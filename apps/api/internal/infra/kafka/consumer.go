package infrakafka

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	applicationeventstream "github.com/shiyudesu/frux/internal/application/eventstream"
	infraconfig "github.com/shiyudesu/frux/internal/infra/config"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
)

var (
	ErrConsumerSession  = errors.New("kafka consumer session failed")
	ErrCommitUncertain  = errors.New("kafka offset commit uncertain")
	ErrShutdownDeadline = errors.New("kafka consumer shutdown deadline exceeded")
	ErrRebalanceDrain   = errors.New("kafka consumer drained for rebalance")
)

type brokerRecord struct {
	Topic     string
	Partition int32
	Offset    int64
	Timestamp time.Time
	Key       []byte
	Value     []byte
	Headers   []applicationeventstream.Header
	original  *kgo.Record
}

type consumerSource interface {
	Poll(ctx context.Context, maxRecords int) ([]brokerRecord, bool, error)
	Commit(ctx context.Context, records []brokerRecord) error
	Lag(ctx context.Context, groupName string) (int64, error)
	AllowRebalance()
	Close()
}

type franzConsumerSource struct {
	client *kgo.Client
	admin  *kadm.Client
}

type ConsumerObserver interface {
	ObserveConsume(topic TopicID, group ConsumerGroupID, outcome string, duration time.Duration, delay time.Duration)
	ObserveCommit(topic TopicID, group ConsumerGroupID, result string)
	ObserveRebalance(group ConsumerGroupID, result string)
	ObserveContract(topic TopicID, group ConsumerGroupID, code ContractFailureCode)
	ObserveLag(topic TopicID, group ConsumerGroupID, lag int64)
	ObserveDataLoss(topic TopicID, group ConsumerGroupID)
}

type Consumer struct {
	source         consumerSource
	topicID        TopicID
	topicName      string
	groupID        ConsumerGroupID
	groupName      string
	handler        applicationeventstream.Handler
	maxPollRecords int
	concurrency    int
	drainTimeout   time.Duration
	commitTimeout  time.Duration
	observer       ConsumerObserver
	lagSampleEvery time.Duration
	lastLagSample  time.Time
	rebalance      <-chan struct{}
}

func NewConsumer(
	ctx context.Context,
	cfg infraconfig.KafkaConfig,
	groupID ConsumerGroupID,
	handler applicationeventstream.Handler,
	observer ConsumerObserver,
) (*Consumer, error) {
	if !cfg.Enabled {
		return nil, ErrKafkaDisabled
	}
	if handler == nil {
		return nil, fmt.Errorf("%w: handler is required", ErrConsumerSession)
	}
	groupSpec, err := ConsumerGroup(groupID)
	if err != nil {
		return nil, err
	}
	topicSpec, err := Topic(groupSpec.Topic)
	if err != nil {
		return nil, err
	}
	if !groupAllowed(topicSpec, groupID) {
		return nil, fmt.Errorf("%w: group is not registered for topic", ErrConsumerSession)
	}
	topicName, err := TopicName(cfg.TopicPrefix, groupSpec.Topic)
	if err != nil {
		return nil, err
	}
	groupName, err := GroupName(cfg.TopicPrefix, groupID)
	if err != nil {
		return nil, err
	}
	if err := validateGroupHandler(groupSpec, groupName, handler); err != nil {
		return nil, err
	}
	options, err := clientOptions(cfg)
	if err != nil {
		return nil, err
	}
	drainTimeout, err := time.ParseDuration(cfg.Consumer.DrainTimeout)
	if err != nil {
		return nil, err
	}
	commitTimeout, err := time.ParseDuration(cfg.Timeouts.Request)
	if err != nil {
		return nil, err
	}
	rebalanceTimeout := drainTimeout + commitTimeout + 10*time.Second
	if rebalanceTimeout < time.Minute {
		rebalanceTimeout = time.Minute
	}
	rebalanceRequested := make(chan struct{}, 1)
	options = append(options,
		kgo.ConsumerGroup(groupName),
		kgo.ConsumeTopics(topicName),
		kgo.DisableAutoCommit(),
		kgo.Balancers(kgo.CooperativeStickyBalancer()),
		kgo.BlockRebalanceOnPoll(),
		kgo.RebalanceTimeout(rebalanceTimeout),
		kgo.FetchMaxBytes(int32(cfg.Consumer.MaxPollBytes)),
		kgo.OnPartitionsCallbackBlocked(func(context.Context, *kgo.Client) {
			select {
			case rebalanceRequested <- struct{}{}:
			default:
			}
		}),
		kgo.OnPartitionsAssigned(func(_ context.Context, _ *kgo.Client, partitions map[string][]int32) {
			if observer != nil && len(partitions) > 0 {
				observer.ObserveRebalance(groupID, "assigned")
			}
		}),
		kgo.OnPartitionsRevoked(func(_ context.Context, _ *kgo.Client, partitions map[string][]int32) {
			if observer != nil && len(partitions) > 0 {
				observer.ObserveRebalance(groupID, "revoked")
			}
		}),
		kgo.OnPartitionsLost(func(_ context.Context, _ *kgo.Client, partitions map[string][]int32) {
			if observer != nil && len(partitions) > 0 {
				observer.ObserveRebalance(groupID, "lost")
			}
		}),
	)
	client, err := kgo.NewClient(options...)
	if err != nil {
		return nil, fmt.Errorf("%w: initialize group client", ErrConsumerSession)
	}
	dialTimeout, err := time.ParseDuration(cfg.Timeouts.Dial)
	if err != nil {
		client.CloseAllowingRebalance()
		return nil, err
	}
	pingContext, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()
	if err := client.Ping(pingContext); err != nil {
		client.CloseAllowingRebalance()
		return nil, fmt.Errorf("%w: broker ping", ErrKafkaUnavailable)
	}
	return &Consumer{
		source:  &franzConsumerSource{client: client, admin: kadm.NewClient(client)},
		topicID: groupSpec.Topic, topicName: topicName,
		groupID: groupID, groupName: groupName, handler: handler,
		maxPollRecords: cfg.Consumer.MaxPollRecords,
		concurrency:    cfg.Consumer.PartitionConcurrency,
		drainTimeout:   drainTimeout, commitTimeout: commitTimeout,
		observer: observer, lagSampleEvery: 15 * time.Second,
		rebalance: rebalanceRequested,
	}, nil
}

func (c *Consumer) Run(ctx context.Context) error {
	if c == nil || c.source == nil || c.handler == nil {
		return ErrConsumerSession
	}
	defer c.source.Close()
	for {
		if ctx.Err() != nil {
			return nil
		}
		c.sampleLag(ctx, false)
		records, dataLoss, err := c.source.Poll(ctx, c.maxPollRecords)
		if dataLoss {
			c.observeDataLoss()
		}
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil
			}
			return fmt.Errorf("%w: poll", ErrConsumerSession)
		}
		if len(records) == 0 {
			c.source.AllowRebalance()
			c.clearRebalanceRequest()
			if ctx.Err() != nil {
				return nil
			}
			continue
		}
		eligible, processErr := c.processBatch(ctx, records)
		if len(eligible) > 0 {
			commitContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), c.commitTimeout)
			err = c.source.Commit(commitContext, eligible)
			cancel()
			if err != nil {
				c.observeCommit("uncertain")
				c.source.AllowRebalance()
				c.sampleLag(ctx, true)
				return fmt.Errorf("%w: %s", ErrCommitUncertain, sanitizeKafkaError(err))
			}
			c.observeCommit("success")
		}
		c.source.AllowRebalance()
		c.clearRebalanceRequest()
		if processErr != nil {
			c.sampleLag(ctx, true)
			if errors.Is(processErr, ErrShutdownDeadline) {
				return processErr
			}
			return fmt.Errorf("%w: handler", ErrConsumerSession)
		}
		if ctx.Err() != nil {
			return nil
		}
	}
}

func validateGroupHandler(
	group ConsumerGroupSpec,
	resolvedGroup string,
	handler applicationeventstream.Handler,
) error {
	shadowHandler, shadow := handler.(applicationeventstream.ShadowOnlyHandler)
	if shadow != group.Shadow {
		return fmt.Errorf("%w: shadow handler mismatch", ErrConsumerSession)
	}
	if shadow && shadowHandler.ExpectedGroup() != resolvedGroup {
		return fmt.Errorf("%w: shadow group identity mismatch", ErrConsumerSession)
	}
	return nil
}

func (c *Consumer) processBatch(
	ctx context.Context,
	records []brokerRecord,
) ([]brokerRecord, error) {
	partitionRecords := make(map[int32][]brokerRecord)
	for _, record := range records {
		partitionRecords[record.Partition] = append(partitionRecords[record.Partition], record)
	}
	partitions := make([]int, 0, len(partitionRecords))
	for partition := range partitionRecords {
		partitions = append(partitions, int(partition))
	}
	sort.Ints(partitions)
	processContext, cancelProcess := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelProcess()
	jobs := make(chan []brokerRecord)
	results := make(chan result, len(partitions))
	workerCount := c.concurrency
	if workerCount > len(partitions) {
		workerCount = len(partitions)
	}
	var workers sync.WaitGroup
	for range workerCount {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for batch := range jobs {
				results <- c.processPartition(processContext, batch)
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, partition := range partitions {
			select {
			case jobs <- partitionRecords[int32(partition)]:
			case <-processContext.Done():
				return
			}
		}
	}()
	go func() {
		workers.Wait()
		close(results)
	}()
	eligible := make([]brokerRecord, 0, len(partitions))
	var processErr error
	drainExpired := false
	rebalanceDrain := false
	contextDone := ctx.Done()
	rebalanceRequested := c.rebalance
	var drainTimer *time.Timer
	var drainDeadline <-chan time.Time
	defer func() {
		if drainTimer != nil {
			drainTimer.Stop()
		}
	}()
	for {
		select {
		case item, open := <-results:
			if !open {
				if drainExpired {
					return eligible, ErrShutdownDeadline
				}
				if rebalanceDrain {
					return eligible, ErrRebalanceDrain
				}
				return eligible, processErr
			}
			if item.eligible != nil {
				eligible = append(eligible, *item.eligible)
			}
			if item.err != nil && processErr == nil {
				processErr = item.err
			}
		case <-contextDone:
			contextDone = nil
			drainTimer = time.NewTimer(c.drainTimeout)
			drainDeadline = drainTimer.C
		case <-rebalanceRequested:
			rebalanceRequested = nil
			rebalanceDrain = true
			cancelProcess()
		case <-drainDeadline:
			cancelProcess()
			drainDeadline = nil
			drainExpired = true
		}
	}
}

func (c *Consumer) clearRebalanceRequest() {
	if c == nil || c.rebalance == nil {
		return
	}
	select {
	case <-c.rebalance:
	default:
	}
}

func (c *Consumer) processPartition(
	ctx context.Context,
	records []brokerRecord,
) result {
	var lastEligible *brokerRecord
	for index := range records {
		record := records[index]
		started := time.Now()
		decoded, err := DecodeEvent(c.topicID, record.Key, record.Value, time.Now().UTC())
		if err != nil {
			var contract *ContractError
			if errors.As(err, &contract) && contract.Terminal() {
				c.observeContract(contract.Code)
				c.observeConsume("terminal_contract", started, record.Timestamp)
				copy := record
				lastEligible = &copy
				continue
			}
			c.observeConsume("retryable", started, record.Timestamp)
			return result{eligible: lastEligible, err: err}
		}
		outcome, handleErr := c.handler.Handle(ctx, applicationeventstream.Event{
			Metadata: applicationeventstream.RecordMetadata{
				Topic: record.Topic, Group: c.groupName,
				Partition: record.Partition, Offset: record.Offset,
				Timestamp: record.Timestamp, Key: append([]byte(nil), record.Key...),
				Headers: cloneHeaders(record.Headers),
			},
			EventID: decoded.Envelope.EventID, EventType: string(decoded.Envelope.EventType),
			SchemaVersion: decoded.Envelope.SchemaVersion,
			OccurredAt:    decoded.Envelope.OccurredAt, ProducedAt: decoded.Envelope.ProducedAt,
			Producer:      string(decoded.Envelope.Producer),
			CorrelationID: decoded.Envelope.CorrelationID,
			Payload:       decoded.Payload,
		})
		if !applicationeventstream.ValidOutcome(outcome) {
			c.observeConsume("retryable", started, record.Timestamp)
			return result{eligible: lastEligible, err: applicationeventstream.ErrInvalidOutcome}
		}
		c.observeConsume(string(outcome), started, record.Timestamp)
		if applicationeventstream.CommitEligible(outcome) {
			copy := record
			lastEligible = &copy
			continue
		}
		if handleErr == nil {
			handleErr = ErrConsumerSession
		}
		return result{eligible: lastEligible, err: handleErr}
	}
	return result{eligible: lastEligible}
}

type result struct {
	eligible *brokerRecord
	err      error
}

func (c *Consumer) observeConsume(outcome string, started time.Time, timestamp time.Time) {
	if c.observer == nil {
		return
	}
	delay := time.Since(timestamp)
	if delay < 0 {
		delay = 0
	}
	c.observer.ObserveConsume(c.topicID, c.groupID, outcome, time.Since(started), delay)
}

func (c *Consumer) observeCommit(result string) {
	if c.observer != nil {
		c.observer.ObserveCommit(c.topicID, c.groupID, result)
	}
}

func (c *Consumer) observeContract(code ContractFailureCode) {
	if c.observer != nil {
		c.observer.ObserveContract(c.topicID, c.groupID, code)
	}
}

func (c *Consumer) observeDataLoss() {
	if c.observer != nil {
		c.observer.ObserveDataLoss(c.topicID, c.groupID)
	}
}

func (c *Consumer) sampleLag(ctx context.Context, force bool) {
	if c.observer == nil || c.source == nil {
		return
	}
	now := time.Now()
	if !force && c.lagSampleEvery > 0 && now.Sub(c.lastLagSample) < c.lagSampleEvery {
		return
	}
	c.lastLagSample = now
	lagContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), c.commitTimeout)
	defer cancel()
	lag, err := c.source.Lag(lagContext, c.groupName)
	if err != nil {
		return
	}
	if lag < 0 {
		lag = 0
	}
	c.observer.ObserveLag(c.topicID, c.groupID, lag)
}

func (s *franzConsumerSource) Poll(ctx context.Context, maxRecords int) ([]brokerRecord, bool, error) {
	fetches := s.client.PollRecords(ctx, maxRecords)
	recoveredDataLoss := false
	var fatalErr error
	for _, fetchErr := range fetches.Errors() {
		var dataLoss *kgo.ErrDataLoss
		if errors.As(fetchErr.Err, &dataLoss) {
			recoveredDataLoss = true
			continue
		}
		if fatalErr == nil {
			fatalErr = fetchErr.Err
		}
	}
	if fatalErr != nil {
		return nil, recoveredDataLoss, fatalErr
	}
	records := make([]brokerRecord, 0, fetches.NumRecords())
	fetches.EachPartition(func(partition kgo.FetchTopicPartition) {
		for _, record := range partition.Records {
			headers := make([]applicationeventstream.Header, 0, len(record.Headers))
			for _, header := range record.Headers {
				headers = append(headers, applicationeventstream.Header{
					Key: header.Key, Value: append([]byte(nil), header.Value...),
				})
			}
			records = append(records, brokerRecord{
				Topic: record.Topic, Partition: record.Partition, Offset: record.Offset,
				Timestamp: record.Timestamp, Key: append([]byte(nil), record.Key...),
				Value: append([]byte(nil), record.Value...), Headers: headers, original: record,
			})
		}
	})
	return records, recoveredDataLoss, nil
}

func (s *franzConsumerSource) Commit(ctx context.Context, records []brokerRecord) error {
	originals := make([]*kgo.Record, 0, len(records))
	for _, record := range records {
		if record.original != nil {
			originals = append(originals, record.original)
		}
	}
	if len(originals) == 0 {
		return nil
	}
	return s.client.CommitRecords(ctx, originals...)
}

func (s *franzConsumerSource) Lag(ctx context.Context, groupName string) (int64, error) {
	if s == nil || s.admin == nil {
		return 0, ErrKafkaUnavailable
	}
	lags, err := s.admin.Lag(ctx, groupName)
	if err != nil {
		return 0, err
	}
	group, exists := lags[groupName]
	if !exists {
		return 0, ErrKafkaUnavailable
	}
	if err := group.Error(); err != nil {
		return 0, err
	}
	return totalGroupLag(group.Lag)
}

func totalGroupLag(lag kadm.GroupLag) (int64, error) {
	var total int64
	for _, partition := range lag.Sorted() {
		if partition.Err != nil || partition.Lag < 0 {
			return 0, fmt.Errorf("%w: incomplete group lag", ErrKafkaUnavailable)
		}
		total += partition.Lag
	}
	return total, nil
}

func (s *franzConsumerSource) AllowRebalance() {
	s.client.AllowRebalance()
}

func (s *franzConsumerSource) Close() {
	s.client.CloseAllowingRebalance()
}

func groupAllowed(topic TopicSpec, group ConsumerGroupID) bool {
	for _, allowed := range topic.AllowedGroups {
		if allowed == group {
			return true
		}
	}
	return false
}

func cloneHeaders(headers []applicationeventstream.Header) []applicationeventstream.Header {
	cloned := make([]applicationeventstream.Header, 0, len(headers))
	for _, header := range headers {
		cloned = append(cloned, applicationeventstream.Header{
			Key: header.Key, Value: append([]byte(nil), header.Value...),
		})
	}
	return cloned
}

type ConsumerFactory func(ctx context.Context) (*Consumer, error)

type Supervisor struct {
	NewConsumer ConsumerFactory
	MinBackoff  time.Duration
	MaxBackoff  time.Duration
}

func (s Supervisor) Run(ctx context.Context) error {
	if s.NewConsumer == nil {
		return ErrConsumerSession
	}
	backoff := s.MinBackoff
	if backoff <= 0 {
		backoff = 100 * time.Millisecond
	}
	maxBackoff := s.MaxBackoff
	if maxBackoff <= 0 {
		maxBackoff = 30 * time.Second
	}
	for {
		consumer, err := s.NewConsumer(ctx)
		if err == nil {
			err = consumer.Run(ctx)
		}
		if ctx.Err() != nil {
			return nil
		}
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
		if backoff < maxBackoff {
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		}
	}
}
