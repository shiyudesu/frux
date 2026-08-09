package infrakafka

import (
	"context"
	"errors"
	"fmt"
	rand "math/rand/v2"
	"sort"
	"sync"
	"time"

	applicationeventstream "github.com/shiyudesu/frux/internal/application/eventstream"
	infraconfig "github.com/shiyudesu/frux/internal/infra/config"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
)

var (
	ErrConsumerSession        = errors.New("kafka consumer session failed")
	ErrConsumerConfiguration  = errors.New("kafka consumer configuration invalid")
	ErrCommitUncertain        = errors.New("kafka offset commit uncertain")
	ErrShutdownDeadline       = errors.New("kafka consumer shutdown deadline exceeded")
	ErrRebalanceDrain         = errors.New("kafka consumer drained for rebalance")
	ErrConsumerStartupTimeout = errors.New("kafka consumer assignment startup timeout")
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
	Close(ctx context.Context) error
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

type assignmentReadiness struct {
	ready chan struct{}
	once  sync.Once
}

func newAssignmentReadiness() *assignmentReadiness {
	return &assignmentReadiness{ready: make(chan struct{})}
}

func (r *assignmentReadiness) assigned(partitions map[string][]int32) {
	if r == nil {
		return
	}
	hasPartitions := false
	for _, assigned := range partitions {
		if len(assigned) > 0 {
			hasPartitions = true
			break
		}
	}
	if !hasPartitions {
		return
	}
	r.once.Do(func() {
		close(r.ready)
	})
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
	closeTimeout   time.Duration
	observer       ConsumerObserver
	lagSampleEvery time.Duration
	lastLagSample  time.Time
	rebalance      <-chan struct{}
	assignment     *assignmentReadiness
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
		return nil, fmt.Errorf("%w: handler is required", ErrConsumerConfiguration)
	}
	groupSpec, err := ConsumerGroup(groupID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrConsumerConfiguration, err)
	}
	topicSpec, err := Topic(groupSpec.Topic)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrConsumerConfiguration, err)
	}
	if !groupAllowed(topicSpec, groupID) {
		return nil, fmt.Errorf("%w: group is not registered for topic", ErrConsumerConfiguration)
	}
	topicName, err := TopicName(cfg.TopicPrefix, groupSpec.Topic)
	if err != nil {
		return nil, err
	}
	groupName, err := ResolvedGroupName(cfg.TopicPrefix, cfg.ShadowDeployment, groupID)
	if err != nil {
		return nil, err
	}
	if err := validateGroupHandler(groupSpec, groupName, handler); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrConsumerConfiguration, err)
	}
	options, err := clientOptions(cfg)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrConsumerConfiguration, err)
	}
	drainTimeout, err := time.ParseDuration(cfg.Consumer.DrainTimeout)
	if err != nil {
		return nil, fmt.Errorf("%w: drain timeout", ErrConsumerConfiguration)
	}
	commitTimeout, err := time.ParseDuration(cfg.Timeouts.Request)
	if err != nil {
		return nil, fmt.Errorf("%w: request timeout", ErrConsumerConfiguration)
	}
	closeTimeout, err := time.ParseDuration(cfg.Timeouts.Shutdown)
	if err != nil {
		return nil, fmt.Errorf("%w: shutdown timeout", ErrConsumerConfiguration)
	}
	rebalanceTimeout := drainTimeout + commitTimeout + 10*time.Second
	if rebalanceTimeout < time.Minute {
		rebalanceTimeout = time.Minute
	}
	rebalanceRequested := make(chan struct{}, 1)
	assignment := newAssignmentReadiness()
	options = append(options,
		kgo.ConsumerGroup(groupName),
		kgo.ConsumeTopics(topicName),
		kgo.ConsumeResetOffset(kgo.NoResetOffset()),
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
			if len(partitions) == 0 {
				return
			}
			assignment.assigned(partitions)
			if observer != nil {
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
		return nil, fmt.Errorf("%w: initialize group client", ErrConsumerConfiguration)
	}
	dialTimeout, err := time.ParseDuration(cfg.Timeouts.Dial)
	if err != nil {
		client.CloseAllowingRebalance()
		return nil, fmt.Errorf("%w: dial timeout", ErrConsumerConfiguration)
	}
	pingContext, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()
	if err := client.Ping(pingContext); err != nil {
		client.CloseAllowingRebalance()
		return nil, fmt.Errorf("%w: broker ping: %w", ErrKafkaUnavailable, err)
	}
	return &Consumer{
		source:  &franzConsumerSource{client: client, admin: kadm.NewClient(client)},
		topicID: groupSpec.Topic, topicName: topicName,
		groupID: groupID, groupName: groupName, handler: handler,
		maxPollRecords: cfg.Consumer.MaxPollRecords,
		concurrency:    cfg.Consumer.PartitionConcurrency,
		drainTimeout:   drainTimeout, commitTimeout: commitTimeout,
		closeTimeout: closeTimeout,
		observer:     observer, lagSampleEvery: 15 * time.Second,
		rebalance: rebalanceRequested, assignment: assignment,
	}, nil
}

func (c *Consumer) AssignmentReady() <-chan struct{} {
	if c == nil || c.assignment == nil {
		return make(chan struct{})
	}
	return c.assignment.ready
}

func (c *Consumer) Run(ctx context.Context) error {
	if c == nil || c.source == nil || c.handler == nil {
		return ErrConsumerSession
	}
	defer func() {
		closeContext, cancel := context.WithTimeout(context.Background(), c.closeTimeout)
		_ = c.source.Close(closeContext)
		cancel()
	}()
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
			return fmt.Errorf("%w: poll: %w", ErrConsumerSession, err)
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
				return errors.Join(
					ErrCommitUncertain,
					err,
					errors.New(sanitizeKafkaError(err)),
				)
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
			if errors.Is(processErr, ErrRebalanceDrain) {
				return processErr
			}
			return fmt.Errorf("%w: handler: %w", ErrConsumerSession, processErr)
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
		applicationEvent := applicationeventstream.Event{
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
		}
		outcome, handleErr := c.handleWithRequestedRetry(ctx, applicationEvent)
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

func (c *Consumer) handleWithRequestedRetry(
	ctx context.Context,
	event applicationeventstream.Event,
) (applicationeventstream.Outcome, error) {
	for {
		outcome, err := c.handler.Handle(ctx, event)
		if outcome != applicationeventstream.OutcomeRetryable || err == nil {
			return outcome, err
		}
		type retryAfter interface {
			RetryAfter() time.Duration
		}
		var requested retryAfter
		if !errors.As(err, &requested) || requested.RetryAfter() <= 0 {
			return outcome, err
		}
		timer := time.NewTimer(requested.RetryAfter())
		select {
		case <-ctx.Done():
			timer.Stop()
			return outcome, ctx.Err()
		case <-timer.C:
		}
	}
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

func (s *franzConsumerSource) Close(ctx context.Context) error {
	if s == nil || s.client == nil {
		return nil
	}
	s.client.AllowRebalance()
	leaveErr := s.client.LeaveGroupContext(ctx)
	if leaveErr != nil {
		_ = s.client.LeaveGroupContext(nil)
	}
	closed := make(chan struct{})
	go func() {
		s.client.Close()
		close(closed)
	}()
	select {
	case <-closed:
		return leaveErr
	case <-ctx.Done():
		_ = s.client.LeaveGroupContext(nil)
		return ctx.Err()
	}
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

type ConsumerSessionObserver interface {
	ObserveConsumerSession(group ConsumerGroupID, result string)
}

type Supervisor struct {
	NewConsumer ConsumerFactory
	Group       ConsumerGroupID
	Observer    ConsumerSessionObserver
	Ready       chan<- struct{}
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
	var readyOnce sync.Once
	for {
		sessionStarted := time.Now()
		consumer, err := s.NewConsumer(ctx)
		if err == nil {
			sessionDone := make(chan error, 1)
			go func() {
				sessionDone <- consumer.Run(ctx)
			}()
			select {
			case <-consumer.AssignmentReady():
				s.observe("started")
				readyOnce.Do(func() {
					if s.Ready != nil {
						close(s.Ready)
					}
				})
				err = <-sessionDone
			case err = <-sessionDone:
				select {
				case <-consumer.AssignmentReady():
					s.observe("started")
					readyOnce.Do(func() {
						if s.Ready != nil {
							close(s.Ready)
						}
					})
				default:
				}
			}
		}
		if ctx.Err() != nil {
			s.observe("stopped")
			return nil
		}
		if errors.Is(err, ErrRebalanceDrain) {
			s.observe("rebalance_restart")
			backoff = s.MinBackoff
			if backoff <= 0 {
				backoff = 100 * time.Millisecond
			}
			delay := rebalanceRestartDelay()
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil
			case <-timer.C:
			}
			continue
		}

		if !RetryableConsumerError(err) {
			s.observe("fatal_failure")
			return err
		}
		s.observe("retryable_failure")
		if time.Since(sessionStarted) >= maxBackoff {
			backoff = s.MinBackoff
			if backoff <= 0 {
				backoff = 100 * time.Millisecond
			}
		}
		delay := consumerRetryDelay(err, backoff, maxBackoff)
		timer := time.NewTimer(delay)
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

func rebalanceRestartDelay() time.Duration {
	return time.Second + time.Duration(rand.Int64N(int64(2*time.Second)))
}

func RetryableConsumerError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrKafkaDisabled) ||
		errors.Is(err, ErrConsumerConfiguration) ||
		errors.Is(err, ErrInvalidKafkaTLS) ||
		errors.Is(err, ErrUnknownRegistryValue) ||
		errors.Is(err, applicationeventstream.ErrInvalidOutcome) ||
		errors.Is(err, kerr.SaslAuthenticationFailed) ||
		errors.Is(err, kerr.UnsupportedSaslMechanism) ||
		errors.Is(err, kerr.TopicAuthorizationFailed) ||
		errors.Is(err, kerr.GroupAuthorizationFailed) ||
		errors.Is(err, kerr.ClusterAuthorizationFailed) ||
		errors.Is(err, kerr.OffsetOutOfRange) {
		return false
	}
	return true
}

func consumerRetryDelay(err error, backoff, maximum time.Duration) time.Duration {
	type retryAfter interface {
		RetryAfter() time.Duration
	}
	var requested retryAfter
	if errors.As(err, &requested) && requested.RetryAfter() > backoff {
		backoff = requested.RetryAfter()
		if backoff > 30*time.Second {
			backoff = 30 * time.Second
		}
		return backoff
	}
	if maximum > 0 && backoff > maximum {
		return maximum
	}
	return backoff
}

func (s Supervisor) observe(result string) {
	if s.Observer != nil {
		s.Observer.ObserveConsumerSession(s.Group, result)
	}
}
