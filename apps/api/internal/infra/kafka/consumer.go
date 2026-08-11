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
	ErrConsumerDataLoss       = errors.New("kafka consumer detected data loss")
	ErrConsumerStartupTimeout = errors.New("kafka consumer assignment startup timeout")
	ErrRecoveryPublish        = errors.New("kafka recovery publication failed")
)

type recoveryPublishError struct {
	cause error
}

func (e *recoveryPublishError) Error() string {
	if e == nil || e.cause == nil {
		return ErrRecoveryPublish.Error()
	}
	return ErrRecoveryPublish.Error() + ": " + sanitizeKafkaError(e.cause)
}

func (e *recoveryPublishError) Unwrap() []error {
	if e == nil || e.cause == nil {
		return []error{ErrRecoveryPublish}
	}
	return []error{ErrRecoveryPublish, e.cause}
}

func wrapRecoveryPublishError(err error) error {
	if err == nil {
		return nil
	}
	return &recoveryPublishError{cause: err}
}

type brokerRecord struct {
	Topic      string
	Partition  int32
	Offset     int64
	Timestamp  time.Time
	Key        []byte
	Value      []byte
	Headers    []applicationeventstream.Header
	original   *kgo.Record
	resumed    bool
	generation uint64
}

type consumerSource interface {
	Poll(ctx context.Context, maxRecords int) ([]brokerRecord, bool, error)
	Commit(ctx context.Context, records []brokerRecord) error
	Lag(ctx context.Context, groupName string) (int64, error)
	Pause(topic string, partition int32)
	Resume(topic string, partition int32)
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
	ObserveLag(topic TopicID, group ConsumerGroupID, stage ConsumerStage, lag int64)
	ObserveDataLoss(topic TopicID, group ConsumerGroupID)
}

type ConsumerRecoveryObserver interface {
	ObserveRecoveryPublish(group ConsumerGroupID, destination, result string)
	ObserveRecoveryDelay(group ConsumerGroupID, tier, result string, duration time.Duration)
	ObserveLocalRetry(group ConsumerGroupID, result string)
}

type ConsumerRecoveryProgressObserver interface {
	ObserveRecoveryProgress(group ConsumerGroupID, kind string)
}

type ConsumerOption func(*consumerOptions)

type consumerOptions struct {
	retryOffsetStore RetryOffsetInitializationStore
}

func WithRetryOffsetInitializationStore(
	store RetryOffsetInitializationStore,
) ConsumerOption {
	return func(options *consumerOptions) {
		options.retryOffsetStore = store
	}
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
	consumeTopicID TopicID
	topicName      string
	topicPrefix    string
	groupID        ConsumerGroupID
	groupName      string
	stage          ConsumerStage
	handler        applicationeventstream.Handler
	recovery       *RecoverySpec
	recoveryTier   int
	recoveryWriter recoveryPublisher
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
	partitions     *consumerPartitionLifecycle
	now            func() time.Time
	delayedMu      sync.Mutex
	delayed        map[delayedPartitionKey]delayedPartition
	delayedChanged chan struct{}
}

type delayedPartitionKey struct {
	topic     string
	partition int32
}

type delayedPartition struct {
	records    []brokerRecord
	notBefore  time.Time
	tier       int
	pausedAt   time.Time
	generation uint64
}

type consumerPartitionLifecycle struct {
	mu       sync.Mutex
	cond     *sync.Cond
	consumer *Consumer
	states   map[delayedPartitionKey]*partitionOwnershipState
}

type partitionOwnershipState struct {
	generation uint64
	owned      bool
	revoking   bool
	active     int
}

type partitionOwnershipLease struct {
	lifecycle  *consumerPartitionLifecycle
	key        delayedPartitionKey
	generation uint64
	once       sync.Once
}

type readyDelayedBatch struct {
	records []brokerRecord
	leases  []*partitionOwnershipLease
}

func (l *consumerPartitionLifecycle) bind(consumer *Consumer) {
	if l == nil {
		return
	}
	l.mu.Lock()
	l.consumer = consumer
	l.mu.Unlock()
}

func (l *consumerPartitionLifecycle) assigned(
	partitions map[string][]int32,
) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.ensureLocked()
	for topic, assigned := range partitions {
		for _, partition := range assigned {
			key := delayedPartitionKey{topic: topic, partition: partition}
			state := l.states[key]
			if state == nil {
				state = &partitionOwnershipState{}
				l.states[key] = state
			}
			state.generation++
			state.owned = true
			state.revoking = false
		}
	}
}

func (l *consumerPartitionLifecycle) generation(
	key delayedPartitionKey,
) (uint64, bool) {
	if l == nil {
		return 0, true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.ensureLocked()
	state := l.states[key]
	if state == nil || !state.owned || state.revoking {
		return 0, false
	}
	return state.generation, true
}

func (l *consumerPartitionLifecycle) acquire(
	key delayedPartitionKey,
	generation uint64,
) (*partitionOwnershipLease, bool) {
	if l == nil {
		return &partitionOwnershipLease{}, true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.ensureLocked()
	state := l.states[key]
	if state == nil || !state.owned || state.revoking ||
		state.generation != generation {
		return nil, false
	}
	state.active++
	return &partitionOwnershipLease{
		lifecycle: l, key: key, generation: generation,
	}, true
}

func (l *consumerPartitionLifecycle) discard(
	partitions map[string][]int32,
	result string,
) {
	if l == nil {
		return
	}
	selected := delayedPartitionSelection(partitions)
	l.mu.Lock()
	l.ensureLocked()
	consumer := l.consumer
	keys := make([]delayedPartitionKey, 0)
	force := result == "lost"
	for key, state := range l.states {
		if partitions != nil {
			if _, ok := selected[key]; !ok {
				continue
			}
		}
		if state.owned {
			if force {
				state.owned = false
				state.revoking = false
				state.generation++
				state.active = 0
			} else {
				state.revoking = true
			}
			keys = append(keys, key)
		}
	}
	sort.Slice(keys, func(left, right int) bool {
		if keys[left].topic == keys[right].topic {
			return keys[left].partition < keys[right].partition
		}
		return keys[left].topic < keys[right].topic
	})
	if !force {
		for ownershipActive(l.states, keys) {
			l.cond.Wait()
		}
		for _, key := range keys {
			state := l.states[key]
			state.owned = false
			state.revoking = false
			state.generation++
		}
	}
	l.mu.Unlock()
	if consumer != nil {
		consumer.discardDelayedPartitions(partitions, result)
	}
}

func (l *consumerPartitionLifecycle) ensureLocked() {
	if l.states == nil {
		l.states = make(map[delayedPartitionKey]*partitionOwnershipState)
	}
	if l.cond == nil {
		l.cond = sync.NewCond(&l.mu)
	}
}

func (l *partitionOwnershipLease) valid() bool {
	if l == nil || l.lifecycle == nil {
		return true
	}
	l.lifecycle.mu.Lock()
	defer l.lifecycle.mu.Unlock()
	state := l.lifecycle.states[l.key]
	return state != nil && state.owned &&
		state.generation == l.generation && state.active > 0
}

func (l *partitionOwnershipLease) release() {
	if l == nil || l.lifecycle == nil {
		return
	}
	l.once.Do(func() {
		l.lifecycle.mu.Lock()
		defer l.lifecycle.mu.Unlock()
		state := l.lifecycle.states[l.key]
		if state != nil && state.generation == l.generation &&
			state.active > 0 {
			state.active--
		}
		l.lifecycle.cond.Broadcast()
	})
}

func (b *readyDelayedBatch) valid() bool {
	if b == nil {
		return true
	}
	for _, lease := range b.leases {
		if !lease.valid() {
			return false
		}
	}
	return true
}

func (b *readyDelayedBatch) release() {
	if b == nil {
		return
	}
	for _, lease := range b.leases {
		lease.release()
	}
	b.leases = nil
}

func delayedPartitionSelection(
	partitions map[string][]int32,
) map[delayedPartitionKey]struct{} {
	selected := make(map[delayedPartitionKey]struct{})
	for topic, assigned := range partitions {
		for _, partition := range assigned {
			selected[delayedPartitionKey{topic: topic, partition: partition}] = struct{}{}
		}
	}
	return selected
}

func ownershipActive(
	states map[delayedPartitionKey]*partitionOwnershipState,
	keys []delayedPartitionKey,
) bool {
	for _, key := range keys {
		if state := states[key]; state != nil && state.active > 0 {
			return true
		}
	}
	return false
}

type pollResult struct {
	records  []brokerRecord
	dataLoss bool
	err      error
}

func NewConsumer(
	ctx context.Context,
	cfg infraconfig.KafkaConfig,
	groupID ConsumerGroupID,
	handler applicationeventstream.Handler,
	observer ConsumerObserver,
	options ...ConsumerOption,
) (*Consumer, error) {
	return newConsumer(ctx, cfg, groupID, 0, handler, observer, options...)
}

func NewRetryTierConsumer(
	ctx context.Context,
	cfg infraconfig.KafkaConfig,
	groupID ConsumerGroupID,
	tier int,
	handler applicationeventstream.Handler,
	observer ConsumerObserver,
	options ...ConsumerOption,
) (*Consumer, error) {
	return newConsumer(ctx, cfg, groupID, tier, handler, observer, options...)
}

func newConsumer(
	ctx context.Context,
	cfg infraconfig.KafkaConfig,
	groupID ConsumerGroupID,
	recoveryTier int,
	handler applicationeventstream.Handler,
	observer ConsumerObserver,
	configure ...ConsumerOption,
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
	registered, recoveryErr := Recovery(groupID)
	if recoveryErr != nil || registered.SourceTopic != groupSpec.Topic {
		return nil, fmt.Errorf("%w: recovery policy is not registered", ErrConsumerConfiguration)
	}
	recoverySpec := &registered
	consumeTopicID := groupSpec.Topic
	if recoveryTier > 0 {
		if recoverySpec == nil || recoverySpec.Policy != RecoveryRetryTopics {
			return nil, fmt.Errorf("%w: retry tier group is not registered", ErrConsumerConfiguration)
		}
		tierSpec, ok := recoverySpec.RetryTier(recoveryTier)
		if !ok {
			return nil, fmt.Errorf("%w: retry tier is not registered", ErrConsumerConfiguration)
		}
		consumeTopicID = tierSpec.Topic
	}
	consumeTopicSpec, err := Topic(consumeTopicID)
	if err != nil || !groupAllowed(consumeTopicSpec, groupID) {
		return nil, fmt.Errorf("%w: group is not registered for consumed topic", ErrConsumerConfiguration)
	}
	topicName, err := TopicName(cfg.TopicPrefix, consumeTopicID)
	if err != nil {
		return nil, err
	}
	stage, err := RecoveryConsumerStage(groupID, recoveryTier)
	if err != nil {
		return nil, fmt.Errorf("%w: consumer stage is not registered", ErrConsumerConfiguration)
	}
	var groupName string
	if recoveryTier > 0 {
		groupName, err = RecoveryConsumerGroupName(cfg.TopicPrefix, groupID, recoveryTier)
	} else {
		groupName, err = ResolvedGroupName(cfg.TopicPrefix, groupID)
	}
	if err != nil {
		return nil, err
	}
	if err := validateGroupHandler(groupSpec, groupName, handler); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrConsumerConfiguration, err)
	}
	consumerConfig := consumerOptions{}
	for _, option := range configure {
		if option != nil {
			option(&consumerConfig)
		}
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
	produceTimeout, err := time.ParseDuration(cfg.Timeouts.Produce)
	if err != nil {
		return nil, fmt.Errorf("%w: produce timeout", ErrConsumerConfiguration)
	}
	closeTimeout, err := time.ParseDuration(cfg.Timeouts.Shutdown)
	if err != nil {
		return nil, fmt.Errorf("%w: shutdown timeout", ErrConsumerConfiguration)
	}
	dialTimeout, err := time.ParseDuration(cfg.Timeouts.Dial)
	if err != nil {
		return nil, fmt.Errorf("%w: dial timeout", ErrConsumerConfiguration)
	}
	adminTimeout, err := time.ParseDuration(cfg.Timeouts.Admin)
	if err != nil {
		return nil, fmt.Errorf("%w: admin timeout", ErrConsumerConfiguration)
	}
	rebalanceTimeout := drainTimeout + commitTimeout + 10*time.Second
	if rebalanceTimeout < time.Minute {
		rebalanceTimeout = time.Minute
	}
	rebalanceRequested := make(chan struct{}, 1)
	assignment := newAssignmentReadiness()
	partitionLifecycle := &consumerPartitionLifecycle{}
	resetOffset := kgo.NoResetOffset()
	if consumerConfig.retryOffsetStore != nil {
		resetOffset = resetOffset.AtCommitted()
		adminClient, clientErr := kgo.NewClient(options...)
		if clientErr != nil {
			return nil, fmt.Errorf("%w: initialize admin client", ErrConsumerConfiguration)
		}
		pingContext, cancel := context.WithTimeout(ctx, dialTimeout)
		clientErr = adminClient.Ping(pingContext)
		cancel()
		if clientErr == nil {
			identity, identityErr := NewRetryOffsetInitializationIdentity(
				cfg.Environment,
				cfg.TopicPrefix,
				groupName,
				topicName,
			)
			if identityErr != nil {
				clientErr = identityErr
			}
			initializer := &retryOffsetAdministrator{
				backend:            &franzRetryOffsetBackend{client: kadm.NewClient(adminClient)},
				store:              consumerConfig.retryOffsetStore,
				identity:           identity,
				timeout:            adminTimeout,
				adoptSparseOffsets: recoveryTier == 0,
			}
			if clientErr == nil {
				clientErr = initializer.Initialize(ctx, groupName, topicName)
			}
		}
		adminClient.Close()
		if clientErr != nil {
			return nil, fmt.Errorf("initialize consumer offsets: %w", clientErr)
		}
	} else if recoveryTier > 0 {
		return nil, fmt.Errorf(
			"%w: durable retry offset initialization store is required",
			ErrConsumerConfiguration,
		)
	}
	options = append(options,
		kgo.ConsumerGroup(groupName),
		kgo.ConsumeTopics(topicName),
		kgo.ConsumeResetOffset(resetOffset),
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
			partitionLifecycle.assigned(partitions)
			assignment.assigned(partitions)
			if observer != nil {
				observer.ObserveRebalance(groupID, "assigned")
			}
		}),
		kgo.OnPartitionsRevoked(func(_ context.Context, _ *kgo.Client, partitions map[string][]int32) {
			partitionLifecycle.discard(partitions, "revoked")
			if observer != nil && len(partitions) > 0 {
				observer.ObserveRebalance(groupID, "revoked")
			}
		}),
		kgo.OnPartitionsLost(func(_ context.Context, _ *kgo.Client, partitions map[string][]int32) {
			partitionLifecycle.discard(partitions, "lost")
			if observer != nil && len(partitions) > 0 {
				observer.ObserveRebalance(groupID, "lost")
			}
		}),
	)
	client, err := kgo.NewClient(options...)
	if err != nil {
		return nil, fmt.Errorf("%w: initialize group client", ErrConsumerConfiguration)
	}
	pingContext, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()
	if err := client.Ping(pingContext); err != nil {
		client.CloseAllowingRebalance()
		return nil, fmt.Errorf("%w: broker ping: %w", ErrKafkaUnavailable, err)
	}
	consumer := &Consumer{
		source:  &franzConsumerSource{client: client, admin: kadm.NewClient(client)},
		topicID: groupSpec.Topic, consumeTopicID: consumeTopicID, topicName: topicName,
		topicPrefix: cfg.TopicPrefix,
		groupID:     groupID, groupName: groupName, stage: stage, handler: handler,
		recovery: recoverySpec, recoveryTier: recoveryTier,
		maxPollRecords: cfg.Consumer.MaxPollRecords,
		concurrency:    cfg.Consumer.PartitionConcurrency,
		drainTimeout:   drainTimeout, commitTimeout: commitTimeout,
		closeTimeout: closeTimeout,
		observer:     observer, lagSampleEvery: 15 * time.Second,
		rebalance: rebalanceRequested, assignment: assignment,
		partitions:     partitionLifecycle,
		now:            time.Now,
		delayedChanged: make(chan struct{}, 1),
	}
	partitionLifecycle.bind(consumer)
	if recoverySpec != nil && recoverySpec.Policy == RecoveryRetryTopics {
		consumer.recoveryWriter = &franzRecoveryPublisher{
			producer: client, prefix: cfg.TopicPrefix, timeout: produceTimeout,
		}
	}
	return consumer, nil
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
		if c.partitions != nil {
			c.partitions.discard(nil, "canceled")
		} else {
			c.discardDelayedPartitions(nil, "canceled")
		}
		closeContext, cancel := context.WithTimeout(context.Background(), c.closeTimeout)
		_ = c.source.Close(closeContext)
		cancel()
	}()
	for {
		if ctx.Err() != nil {
			return nil
		}
		c.sampleLag(ctx, false)
		ready := c.takeReadyDelayed()
		records := ready.records
		dataLoss := false
		var err error
		if len(records) == 0 {
			records, dataLoss, err = c.poll(ctx)
			c.assignRecordGenerations(records)
		}
		if dataLoss {
			ready.release()
			c.observeDataLoss()
			return ErrConsumerDataLoss
		}
		if err != nil {
			ready.release()
			if errors.Is(err, ErrRebalanceDrain) {
				c.source.AllowRebalance()
				return err
			}
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				if ctx.Err() != nil {
					return nil
				}
				continue
			}
			return fmt.Errorf("%w: poll: %w", ErrConsumerSession, err)
		}
		if len(records) == 0 {
			ready.release()
			c.source.AllowRebalance()
			c.clearRebalanceRequest()
			if ctx.Err() != nil {
				return nil
			}
			continue
		}
		eligible, processErr := c.processBatch(ctx, records)
		if !ready.valid() {
			ready.release()
			c.source.AllowRebalance()
			return ErrRebalanceDrain
		}
		if len(eligible) > 0 {
			commitContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), c.commitTimeout)
			err = c.source.Commit(commitContext, eligible)
			cancel()
			if err != nil {
				ready.release()
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
		ready.release()
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
	_ ConsumerGroupSpec,
	_ string,
	_ applicationeventstream.Handler,
) error {
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
			if len(item.delayed) > 0 {
				c.delayPartition(item)
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
		recoveryMetadata, err := c.prepareRecoveryRecord(ctx, record)
		if err != nil {
			if c.recoveryTier > 0 && c.routesRecoveryRecords() {
				if quarantineErr := c.quarantineInvalidRecovery(
					ctx, record, err,
				); quarantineErr != nil {
					c.observeConsume(
						"recovery_quarantine_failed", started, record.Timestamp,
					)
					return result{eligible: lastEligible, err: quarantineErr}
				}
				c.observeConsume("routed_quarantine", started, record.Timestamp)
				copy := record
				lastEligible = &copy
				continue
			}
			c.observeConsume("recovery_invalid", started, record.Timestamp)
			return result{eligible: lastEligible, err: err}
		}
		if recoveryMetadata != nil {
			now := time.Now
			if c.now != nil {
				now = c.now
			}
			delay := recoveryMetadata.NotBefore.Sub(now().UTC())
			if delay > 0 {
				return result{
					eligible:  lastEligible,
					delayed:   append([]brokerRecord(nil), records[index:]...),
					notBefore: recoveryMetadata.NotBefore,
					tier:      recoveryMetadata.Tier,
				}
			}
			if !record.resumed {
				c.observeRecoveryDelay(recoveryMetadata.Tier, "ready", 0)
			}
		}
		decoded, err := DecodeEvent(c.topicID, record.Key, record.Value, time.Now().UTC())
		if err != nil {
			var contract *ContractError
			if errors.As(err, &contract) && contract.Terminal() {
				c.observeContract(contract.Code)
				if c.routesRecoveryRecords() {
					eventID, schemaVersion := RecoveryEventIdentity(record.Value)
					if routeErr := c.routeRecovery(
						ctx,
						record,
						recoveryMetadata,
						eventID,
						schemaVersion,
						FailureTerminalContract,
					); routeErr != nil {
						c.observeConsume("recovery_publish_failed", started, record.Timestamp)
						return result{eligible: lastEligible, err: routeErr}
					}
					c.observeConsume("routed_dlq", started, record.Timestamp)
					copy := record
					lastEligible = &copy
					continue
				}
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
		outcome, handleErr, exhausted := c.handleWithRegisteredRetry(ctx, applicationEvent)
		if !applicationeventstream.ValidOutcome(outcome) {
			c.observeConsume("retryable", started, record.Timestamp)
			return result{eligible: lastEligible, err: applicationeventstream.ErrInvalidOutcome}
		}
		switch outcome {
		case applicationeventstream.OutcomeDurableSuccess:
			c.observeConsume(string(outcome), started, record.Timestamp)
			if recoveryMetadata != nil {
				c.observeRecoveryProgress()
			}
			copy := record
			lastEligible = &copy
			continue
		case applicationeventstream.OutcomeTerminal:
			if c.routesRecoveryRecords() {
				if routeErr := c.routeRecovery(
					ctx,
					record,
					recoveryMetadata,
					decoded.Envelope.EventID,
					decoded.Envelope.SchemaVersion,
					FailureTerminalDomain,
				); routeErr != nil {
					c.observeConsume("recovery_publish_failed", started, record.Timestamp)
					return result{eligible: lastEligible, err: routeErr}
				}
				c.observeConsume("routed_dlq", started, record.Timestamp)
			} else {
				c.observeConsume(string(outcome), started, record.Timestamp)
			}
			copy := record
			lastEligible = &copy
			continue
		case applicationeventstream.OutcomeRetryable:
			c.observeConsume("retryable", started, record.Timestamp)
			if lifecycleErr := ctx.Err(); lifecycleErr != nil {
				return result{eligible: lastEligible, err: lifecycleErr}
			}
			if c.routesRecoveryRecords() && exhausted {
				if routeErr := c.routeRecovery(
					ctx,
					record,
					recoveryMetadata,
					decoded.Envelope.EventID,
					decoded.Envelope.SchemaVersion,
					FailureLocalRetryExhausted,
				); routeErr != nil {
					c.observeConsume("recovery_publish_failed", started, record.Timestamp)
					return result{eligible: lastEligible, err: routeErr}
				}
				c.observeConsume("routed_retry", started, record.Timestamp)
				copy := record
				lastEligible = &copy
				continue
			}
		}
		if handleErr == nil {
			handleErr = ErrConsumerSession
		}
		return result{eligible: lastEligible, err: handleErr}
	}
	return result{eligible: lastEligible}
}

func (c *Consumer) prepareRecoveryRecord(
	ctx context.Context,
	record brokerRecord,
) (*RecoveryMetadata, error) {
	if c == nil {
		return nil, nil
	}
	if c.recoveryTier == 0 {
		if !c.routesRecoveryRecords() ||
			!containsRecoveryHeader(record.Headers) {
			return nil, nil
		}
		metadata, err := DecodeRecoveryHeaders(
			c.topicPrefix,
			c.consumeTopicID,
			record.Headers,
			record.Key,
			record.Value,
		)
		if err != nil {
			return nil, err
		}
		if metadata.ConsumerGroup != c.groupID {
			return nil, nil
		}
		if metadata.Tier != 0 || metadata.ReplayID == "" {
			return nil, recoveryMetadataError(RecoveryMetadataInvalidTopic)
		}
		return &metadata, nil
	}
	metadata, err := DecodeRecoveryHeaders(
		c.topicPrefix,
		c.consumeTopicID,
		record.Headers,
		record.Key,
		record.Value,
	)
	if err != nil {
		return nil, err
	}
	if metadata.ConsumerGroup != c.groupID || metadata.Tier != c.recoveryTier {
		return nil, recoveryMetadataError(RecoveryMetadataInvalidTopic)
	}
	return &metadata, nil
}

func containsRecoveryHeader(headers []applicationeventstream.Header) bool {
	for _, header := range headers {
		if header.Key == RecoveryHeaderKey {
			return true
		}
	}
	return false
}

func (c *Consumer) routesRecoveryRecords() bool {
	return c != nil && c.recovery != nil &&
		c.recovery.Policy == RecoveryRetryTopics &&
		c.recoveryWriter != nil
}

func (c *Consumer) routeRecovery(
	ctx context.Context,
	record brokerRecord,
	previous *RecoveryMetadata,
	eventID string,
	schemaVersion int,
	failureClass FailureClass,
) error {
	if !c.routesRecoveryRecords() || !c.recovery.AllowsFailure(failureClass) {
		return ErrConsumerConfiguration
	}
	now := time.Now
	if c.now != nil {
		now = c.now
	}
	failedAt := now().UTC()
	destination := c.recovery.DLQTopic
	nextTier := 0
	if failureClass == FailureLocalRetryExhausted &&
		c.recoveryTier < len(c.recovery.RetryTiers) {
		nextTier = c.recoveryTier + 1
		tier, ok := c.recovery.RetryTier(nextTier)
		if !ok {
			return ErrConsumerConfiguration
		}
		destination = tier.Topic
	}
	sourceTopic, err := TopicName(c.topicPrefix, c.recovery.SourceTopic)
	if err != nil {
		return err
	}
	metadata := RecoveryMetadata{
		SourceTopic: sourceTopic, SourcePartition: record.Partition,
		SourceOffset: record.Offset, EventID: eventID, SchemaVersion: schemaVersion,
		ConsumerGroup: c.groupID, Attempt: 1, Tier: nextTier,
		FailureClass: failureClass, FirstFailureAt: failedAt,
		LatestFailureAt: failedAt, NotBefore: failedAt,
		PayloadSHA256: PayloadSHA256(record.Value),
	}
	if previous != nil {
		metadata.SourceTopic = previous.SourceTopic
		metadata.SourcePartition = previous.SourcePartition
		metadata.SourceOffset = previous.SourceOffset
		metadata.EventID = previous.EventID
		metadata.SchemaVersion = previous.SchemaVersion
		metadata.Attempt = previous.Attempt + 1
		metadata.FirstFailureAt = previous.FirstFailureAt
		metadata.ReplayID = previous.ReplayID
	}
	if nextTier > 0 {
		tier, _ := c.recovery.RetryTier(nextTier)
		metadata.NotBefore = failedAt.Add(tier.Delay)
	}
	var headers []applicationeventstream.Header
	if failureClass == FailureTerminalContract &&
		previous == nil &&
		destination == c.recovery.DLQTopic {
		headers, err = encodeTerminalContractDLQHeaders(
			c.topicPrefix,
			destination,
			metadata,
			record.Key,
			record.Value,
		)
	} else {
		headers, err = EncodeRecoveryHeaders(
			c.topicPrefix,
			destination,
			metadata,
			record.Key,
			record.Value,
		)
	}
	if err != nil {
		return err
	}
	err = c.recoveryWriter.PublishRecovery(
		ctx,
		destination,
		record.Key,
		record.Value,
		headers,
	)
	if err != nil {
		result := "failed"
		if errors.Is(err, ErrProduceUncertain) {
			result = "uncertain"
		}
		c.observeRecoveryPublish(nextTier, result)
		return wrapRecoveryPublishError(err)
	}
	c.observeRecoveryPublish(nextTier, "acknowledged")
	return nil
}

func (c *Consumer) quarantineInvalidRecovery(
	ctx context.Context,
	record brokerRecord,
	cause error,
) error {
	if c == nil || c.recovery == nil || c.recoveryTier < 1 ||
		!c.routesRecoveryRecords() {
		return ErrConsumerConfiguration
	}
	now := time.Now
	if c.now != nil {
		now = c.now
	}
	headers, err := EncodeRecoveryQuarantineHeaders(
		c.topicPrefix,
		c.recovery.DLQTopic,
		RecoveryQuarantineMetadata{
			ConsumedTopic:     record.Topic,
			ConsumedPartition: record.Partition,
			ConsumedOffset:    record.Offset,
			ConsumerGroup:     c.groupID,
			FailureClass:      FailureRecoveryMetadataInvalid,
			MetadataCode:      recoveryMetadataCode(cause),
			QuarantinedAt:     now().UTC(),
			PayloadSHA256:     PayloadSHA256(record.Value),
			KeySHA256:         PayloadSHA256(record.Key),
			NonReplayable:     true,
		},
		record.Key,
		record.Value,
	)
	if err != nil {
		return err
	}
	err = c.recoveryWriter.PublishRecovery(
		ctx,
		c.recovery.DLQTopic,
		record.Key,
		record.Value,
		headers,
	)
	if err != nil {
		result := "failed"
		if errors.Is(err, ErrProduceUncertain) {
			result = "uncertain"
		}
		c.observeRecoveryPublish(0, result)
		return wrapRecoveryPublishError(err)
	}
	c.observeRecoveryPublish(0, "acknowledged")
	return nil
}

func (c *Consumer) handleWithRegisteredRetry(
	ctx context.Context,
	event applicationeventstream.Event,
) (applicationeventstream.Outcome, error, bool) {
	if c == nil || c.recovery == nil {
		outcome, err := c.handleWithRequestedRetry(ctx, event)
		return outcome, err, outcome == applicationeventstream.OutcomeRetryable
	}
	spec := c.recovery.LocalRetry
	delay := spec.InitialDelay
	totalDelay := time.Duration(0)
	for attempt := 1; ; attempt++ {
		outcome, err := c.handler.Handle(ctx, event)
		if outcome != applicationeventstream.OutcomeRetryable ||
			!applicationeventstream.ValidOutcome(outcome) {
			return outcome, err, false
		}
		if err == nil {
			err = ErrConsumerSession
		}
		if attempt >= spec.MaxAttempts {
			return outcome, err, true
		}
		type retryAfter interface {
			RetryAfter() time.Duration
		}
		var requested retryAfter
		wait := delay
		if errors.As(err, &requested) && requested.RetryAfter() > 0 {
			wait = requested.RetryAfter()
		}
		if wait <= 0 || totalDelay+wait > spec.MaxTotalDelay {
			return outcome, err, true
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return outcome, ctx.Err(), false
		case <-timer.C:
		}
		totalDelay += wait
		c.observeLocalRetry("attempted")
		if delay < spec.MaxDelay {
			delay *= 2
			if delay > spec.MaxDelay {
				delay = spec.MaxDelay
			}
		}
	}
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
	eligible  *brokerRecord
	err       error
	delayed   []brokerRecord
	notBefore time.Time
	tier      int
}

func (c *Consumer) delayPartition(item result) {
	if c == nil || c.source == nil || len(item.delayed) == 0 ||
		item.notBefore.IsZero() {
		return
	}
	first := item.delayed[0]
	key := delayedPartitionKey{topic: first.Topic, partition: first.Partition}
	generation := uint64(0)
	if c.partitions != nil {
		var owned bool
		generation, owned = c.partitions.generation(key)
		if !owned || (first.generation != 0 && first.generation != generation) {
			return
		}
	}
	c.delayedMu.Lock()
	defer c.delayedMu.Unlock()
	if c.delayed == nil {
		c.delayed = make(map[delayedPartitionKey]delayedPartition)
	}
	if existing, ok := c.delayed[key]; ok {
		if existing.generation != generation {
			return
		}
		existing.records = append(existing.records, item.delayed...)
		if item.notBefore.Before(existing.notBefore) {
			existing.notBefore = item.notBefore
			existing.tier = item.tier
		}
		c.delayed[key] = existing
		c.signalDelayedChanged()
		return
	}
	pausedAt := time.Now().UTC()
	c.source.Pause(first.Topic, first.Partition)
	c.delayed[key] = delayedPartition{
		records:    append([]brokerRecord(nil), item.delayed...),
		notBefore:  item.notBefore,
		tier:       item.tier,
		pausedAt:   pausedAt,
		generation: generation,
	}
	c.signalDelayedChanged()
}

func (c *Consumer) assignRecordGenerations(records []brokerRecord) {
	if c == nil || c.partitions == nil {
		return
	}
	for index := range records {
		if records[index].generation != 0 {
			continue
		}
		key := delayedPartitionKey{
			topic: records[index].Topic, partition: records[index].Partition,
		}
		generation, owned := c.partitions.generation(key)
		if owned {
			records[index].generation = generation
		}
	}
}

func (c *Consumer) takeReadyDelayed() readyDelayedBatch {
	if c == nil {
		return readyDelayedBatch{}
	}
	now := time.Now().UTC()
	if c.now != nil {
		now = c.now().UTC()
	}
	c.delayedMu.Lock()
	if len(c.delayed) == 0 {
		c.delayedMu.Unlock()
		return readyDelayedBatch{}
	}
	keys := make([]delayedPartitionKey, 0, len(c.delayed))
	for key, delayed := range c.delayed {
		if !delayed.notBefore.After(now) {
			keys = append(keys, key)
		}
	}
	sort.Slice(keys, func(left, right int) bool {
		if keys[left].topic == keys[right].topic {
			return keys[left].partition < keys[right].partition
		}
		return keys[left].topic < keys[right].topic
	})
	ready := make([]struct {
		key     delayedPartitionKey
		delayed delayedPartition
		lease   *partitionOwnershipLease
	}, 0, len(keys))
	for _, key := range keys {
		delayed := c.delayed[key]
		lease, owned := c.partitions.acquire(key, delayed.generation)
		if !owned {
			delete(c.delayed, key)
			continue
		}
		delete(c.delayed, key)
		if !lease.valid() {
			lease.release()
			continue
		}
		ready = append(ready, struct {
			key     delayedPartitionKey
			delayed delayedPartition
			lease   *partitionOwnershipLease
		}{key: key, delayed: delayed, lease: lease})
	}
	if len(ready) > 0 {
		c.signalDelayedChanged()
	}
	c.delayedMu.Unlock()

	batch := readyDelayedBatch{
		records: make([]brokerRecord, 0),
		leases:  make([]*partitionOwnershipLease, 0, len(ready)),
	}
	for _, item := range ready {
		key := item.key
		delayed := item.delayed
		c.source.Resume(key.topic, key.partition)
		duration := time.Since(delayed.pausedAt)
		if duration < 0 {
			duration = 0
		}
		c.observeRecoveryDelay(delayed.tier, "resumed", duration)
		batch.leases = append(batch.leases, item.lease)
		for _, record := range delayed.records {
			record.resumed = true
			batch.records = append(batch.records, record)
		}
	}
	return batch
}

func (c *Consumer) discardDelayedPartitions(
	partitions map[string][]int32,
	result string,
) {
	if c == nil {
		return
	}
	selected := delayedPartitionSelection(partitions)
	c.delayedMu.Lock()
	discarded := make([]delayedPartition, 0)
	for key, delayed := range c.delayed {
		if partitions != nil {
			if _, ok := selected[key]; !ok {
				continue
			}
		}
		discarded = append(discarded, delayed)
		delete(c.delayed, key)
	}
	if len(discarded) > 0 {
		c.signalDelayedChanged()
	}
	c.delayedMu.Unlock()
	for _, delayed := range discarded {
		duration := time.Since(delayed.pausedAt)
		if duration < 0 {
			duration = 0
		}
		c.observeRecoveryDelay(delayed.tier, result, duration)
	}
}

func (c *Consumer) nextDelayedAt() time.Time {
	if c == nil {
		return time.Time{}
	}
	c.delayedMu.Lock()
	defer c.delayedMu.Unlock()
	var earliest time.Time
	for _, delayed := range c.delayed {
		if earliest.IsZero() || delayed.notBefore.Before(earliest) {
			earliest = delayed.notBefore
		}
	}
	return earliest
}

func (c *Consumer) signalDelayedChanged() {
	if c == nil || c.delayedChanged == nil {
		return
	}
	select {
	case c.delayedChanged <- struct{}{}:
	default:
	}
}

func (c *Consumer) poll(ctx context.Context) ([]brokerRecord, bool, error) {
	pollContext, cancelPoll := context.WithCancel(ctx)
	defer cancelPoll()
	done := make(chan pollResult, 1)
	go func() {
		records, dataLoss, err := c.source.Poll(pollContext, c.maxPollRecords)
		done <- pollResult{records: records, dataLoss: dataLoss, err: err}
	}()

	var timer *time.Timer
	var due <-chan time.Time
	if next := c.nextDelayedAt(); !next.IsZero() {
		delay := time.Until(next)
		if c.now != nil {
			delay = next.Sub(c.now().UTC())
		}
		if delay < 0 {
			delay = 0
		}
		timer = time.NewTimer(delay)
		due = timer.C
		defer timer.Stop()
	}

	select {
	case result := <-done:
		return result.records, result.dataLoss, result.err
	case <-due:
		cancelPoll()
		result := <-done
		if len(result.records) > 0 || result.dataLoss ||
			(result.err != nil &&
				!errors.Is(result.err, context.Canceled) &&
				!errors.Is(result.err, context.DeadlineExceeded)) {
			return result.records, result.dataLoss, result.err
		}
		return nil, false, context.DeadlineExceeded
	case <-c.delayedChanged:
		cancelPoll()
		result := <-done
		if len(result.records) > 0 || result.dataLoss ||
			(result.err != nil &&
				!errors.Is(result.err, context.Canceled) &&
				!errors.Is(result.err, context.DeadlineExceeded)) {
			return result.records, result.dataLoss, result.err
		}
		return nil, false, context.DeadlineExceeded
	case <-c.rebalance:
		cancelPoll()
		<-done
		return nil, false, ErrRebalanceDrain
	case <-ctx.Done():
		cancelPoll()
		<-done
		return nil, false, ctx.Err()
	}
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

func (c *Consumer) observeRecoveryPublish(tier int, result string) {
	observer, ok := c.observer.(ConsumerRecoveryObserver)
	if !ok {
		return
	}
	destination := "dlq"
	if c.recovery != nil && tier > 0 {
		if registered, exists := c.recovery.RetryTier(tier); exists {
			destination = "retry_" + registered.Label
		}
	}
	observer.ObserveRecoveryPublish(c.groupID, destination, result)
}

func (c *Consumer) observeRecoveryDelay(tier int, result string, duration time.Duration) {
	observer, ok := c.observer.(ConsumerRecoveryObserver)
	if !ok {
		return
	}
	label := "unknown"
	if c.recovery != nil {
		if registered, exists := c.recovery.RetryTier(tier); exists {
			label = registered.Label
		}
	}
	observer.ObserveRecoveryDelay(c.groupID, label, result, duration)
}

func (c *Consumer) observeLocalRetry(result string) {
	observer, ok := c.observer.(ConsumerRecoveryObserver)
	if ok {
		observer.ObserveLocalRetry(c.groupID, result)
	}
}

func (c *Consumer) observeRecoveryProgress() {
	observer, ok := c.observer.(ConsumerRecoveryProgressObserver)
	if ok {
		observer.ObserveRecoveryProgress(c.groupID, "durable")
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
	c.observer.ObserveLag(c.consumeTopicID, c.groupID, c.stage, lag)
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

func (s *franzConsumerSource) Pause(topic string, partition int32) {
	if s == nil || s.client == nil {
		return
	}
	s.client.PauseFetchPartitions(map[string][]int32{topic: {partition}})
}

func (s *franzConsumerSource) Resume(topic string, partition int32) {
	if s == nil || s.client == nil {
		return
	}
	s.client.ResumeFetchPartitions(map[string][]int32{topic: {partition}})
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
	ObserveConsumerSession(group ConsumerGroupID, stage ConsumerStage, result string)
}

type Supervisor struct {
	NewConsumer ConsumerFactory
	Group       ConsumerGroupID
	Stage       ConsumerStage
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
		errors.Is(err, kerr.OffsetOutOfRange) ||
		errors.Is(err, ErrConsumerDataLoss) {
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
		s.Observer.ObserveConsumerSession(s.Group, s.Stage, result)
	}
}
