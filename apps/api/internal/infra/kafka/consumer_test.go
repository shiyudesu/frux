package infrakafka

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	applicationeventstream "github.com/shiyudesu/frux/internal/application/eventstream"

	"github.com/twmb/franz-go/pkg/kadm"
)

type fakeConsumerSource struct {
	mu            sync.Mutex
	batches       [][]brokerRecord
	pollErrors    []error
	errorDataLoss []bool
	dataLosses    []bool
	commitErr     error
	commits       [][]brokerRecord
	lag           int64
	lagErr        error
	allows        int
	closed        bool
}

func (f *fakeConsumerSource) Poll(ctx context.Context, _ int) ([]brokerRecord, bool, error) {
	f.mu.Lock()
	if len(f.pollErrors) > 0 {
		err := f.pollErrors[0]
		f.pollErrors = f.pollErrors[1:]
		dataLoss := false
		if len(f.errorDataLoss) > 0 {
			dataLoss = f.errorDataLoss[0]
			f.errorDataLoss = f.errorDataLoss[1:]
		}
		f.mu.Unlock()
		return nil, dataLoss, err
	}
	if len(f.batches) > 0 {
		batch := f.batches[0]
		f.batches = f.batches[1:]
		dataLoss := false
		if len(f.dataLosses) > 0 {
			dataLoss = f.dataLosses[0]
			f.dataLosses = f.dataLosses[1:]
		}
		f.mu.Unlock()
		return batch, dataLoss, nil
	}
	f.mu.Unlock()
	<-ctx.Done()
	return nil, false, ctx.Err()
}

func (f *fakeConsumerSource) Commit(_ context.Context, records []brokerRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.commits = append(f.commits, append([]brokerRecord(nil), records...))
	return f.commitErr
}

func (f *fakeConsumerSource) Lag(context.Context, string) (int64, error) {
	return f.lag, f.lagErr
}

func (f *fakeConsumerSource) AllowRebalance() {
	f.mu.Lock()
	f.allows++
	f.mu.Unlock()
}

func (f *fakeConsumerSource) Close(context.Context) error {
	f.mu.Lock()
	f.closed = true
	f.mu.Unlock()
	return nil
}

type handlerFunc func(context.Context, applicationeventstream.Event) (applicationeventstream.Outcome, error)

func (f handlerFunc) Handle(ctx context.Context, event applicationeventstream.Event) (applicationeventstream.Outcome, error) {
	return f(ctx, event)
}

type consumerObserver struct {
	lag      int64
	calls    int
	dataLoss int
	cancel   context.CancelFunc
}

func TestConsumerCancelsBlockedRebalanceBeforeReleasingOwnership(t *testing.T) {
	source := &fakeConsumerSource{}
	rebalance := make(chan struct{}, 1)
	started := make(chan struct{})
	finished := make(chan struct{})
	consumer := testConsumer(source, handlerFunc(func(ctx context.Context, _ applicationeventstream.Event) (applicationeventstream.Outcome, error) {
		close(started)
		<-ctx.Done()
		close(finished)
		return applicationeventstream.OutcomeRetryable, ctx.Err()
	}))
	consumer.rebalance = rebalance
	done := make(chan struct{})
	var batchErr error
	go func() {
		_, batchErr = consumer.processBatch(
			context.Background(),
			[]brokerRecord{probeRecord(t, 0, 0)},
		)
		close(done)
	}()
	<-started
	rebalance <- struct{}{}
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("rebalance did not cancel in-flight handler")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("batch did not drain after rebalance cancellation")
	}
	if !errors.Is(batchErr, ErrRebalanceDrain) {
		t.Fatalf("error = %v, want ErrRebalanceDrain", batchErr)
	}
}

func TestRebalanceDrainRestartsBeforeUndispatchedPartitionsCanBeSkipped(t *testing.T) {
	rebalance := make(chan struct{}, 1)
	started := make(chan struct{})
	var calls atomic.Int32
	consumer := testConsumer(nil, handlerFunc(func(ctx context.Context, _ applicationeventstream.Event) (applicationeventstream.Outcome, error) {
		if calls.Add(1) == 1 {
			close(started)
			<-ctx.Done()
			return applicationeventstream.OutcomeRetryable, ctx.Err()
		}
		return applicationeventstream.OutcomeDurableSuccess, nil
	}))
	consumer.concurrency = 1
	consumer.rebalance = rebalance
	done := make(chan error, 1)
	go func() {
		_, err := consumer.processBatch(context.Background(), []brokerRecord{
			probeRecord(t, 0, 0),
			probeRecord(t, 1, 0),
		})
		done <- err
	}()
	<-started
	rebalance <- struct{}{}
	select {
	case err := <-done:
		if !errors.Is(err, ErrRebalanceDrain) {
			t.Fatalf("error = %v, want ErrRebalanceDrain", err)
		}
	case <-time.After(time.Second):
		t.Fatal("rebalance drain did not finish")
	}
}

func (*consumerObserver) ObserveConsume(TopicID, ConsumerGroupID, string, time.Duration, time.Duration) {
}
func (*consumerObserver) ObserveCommit(TopicID, ConsumerGroupID, string) {}
func (*consumerObserver) ObserveRebalance(ConsumerGroupID, string)       {}
func (*consumerObserver) ObserveContract(TopicID, ConsumerGroupID, ContractFailureCode) {
}
func (o *consumerObserver) ObserveLag(_ TopicID, _ ConsumerGroupID, lag int64) {
	o.lag = lag
	o.calls++
	if o.cancel != nil {
		o.cancel()
	}
}
func (o *consumerObserver) ObserveDataLoss(TopicID, ConsumerGroupID) {
	o.dataLoss++
}

func TestConsumerContinuesAfterRecoveredDataLoss(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	source := &fakeConsumerSource{
		batches:    [][]brokerRecord{{probeRecord(t, 0, 3)}},
		dataLosses: []bool{true},
	}
	observer := &consumerObserver{}
	consumer := testConsumer(source, handlerFunc(func(context.Context, applicationeventstream.Event) (applicationeventstream.Outcome, error) {
		cancel()
		return applicationeventstream.OutcomeDurableSuccess, nil
	}))
	consumer.observer = observer
	if err := consumer.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if observer.dataLoss != 1 || len(source.commits) != 1 {
		t.Fatalf("dataLoss=%d commits=%d", observer.dataLoss, len(source.commits))
	}
}

func TestConsumerReleasesRebalanceGateAfterDataLossOnlyPoll(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	source := &fakeConsumerSource{
		batches:    [][]brokerRecord{{}, {probeRecord(t, 0, 4)}},
		dataLosses: []bool{true, false},
	}
	observer := &consumerObserver{}
	consumer := testConsumer(source, handlerFunc(func(context.Context, applicationeventstream.Event) (applicationeventstream.Outcome, error) {
		cancel()
		return applicationeventstream.OutcomeDurableSuccess, nil
	}))
	consumer.observer = observer
	if err := consumer.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if observer.dataLoss != 1 || source.allows != 2 {
		t.Fatalf("dataLoss=%d allows=%d", observer.dataLoss, source.allows)
	}
}

func TestConsumerObservesDataLossAlongsideFatalFetchError(t *testing.T) {
	source := &fakeConsumerSource{
		pollErrors:    []error{errors.New("authorization failed")},
		errorDataLoss: []bool{true},
	}
	observer := &consumerObserver{}
	consumer := testConsumer(source, handlerFunc(func(context.Context, applicationeventstream.Event) (applicationeventstream.Outcome, error) {
		return applicationeventstream.OutcomeDurableSuccess, nil
	}))
	consumer.observer = observer
	err := consumer.Run(context.Background())
	if !errors.Is(err, ErrConsumerSession) || observer.dataLoss != 1 {
		t.Fatalf("error=%v dataLoss=%d", err, observer.dataLoss)
	}
}

func TestShadowGroupsRequireShadowOnlyHandlers(t *testing.T) {
	shadowGroup, err := ConsumerGroup(GroupBackboneProbeShadow)
	if err != nil {
		t.Fatal(err)
	}
	generic := handlerFunc(func(context.Context, applicationeventstream.Event) (applicationeventstream.Outcome, error) {
		return applicationeventstream.OutcomeDurableSuccess, nil
	})
	if err := validateGroupHandler(
		shadowGroup,
		"frux.platform.backbone_probe.shadow.v1",
		generic,
	); err == nil {
		t.Fatal("shadow group accepted generic handler")
	}
	shadow, err := applicationeventstream.NewShadowHandler(
		"frux.platform.backbone_probe.shadow.v1",
		time.Hour,
		nil,
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateGroupHandler(
		shadowGroup,
		"frux.platform.backbone_probe.shadow.v1",
		shadow,
	); err != nil {
		t.Fatalf("shadow handler rejected: %v", err)
	}
	if err := validateGroupHandler(
		shadowGroup,
		"test.frux.platform.backbone_probe.shadow.v1",
		shadow,
	); err == nil {
		t.Fatal("shadow handler accepted mismatched resolved group")
	}
	activeGroup, err := ConsumerGroup(GroupBackboneProbeActive)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateGroupHandler(
		activeGroup,
		"frux.platform.backbone_probe.active.v1",
		shadow,
	); err == nil {
		t.Fatal("active group accepted shadow-only handler")
	}
}

func TestConsumerForcesLagSampleOnHandlerFailure(t *testing.T) {
	source := &fakeConsumerSource{
		batches: [][]brokerRecord{{probeRecord(t, 0, 3)}},
		lag:     9,
	}
	observer := &consumerObserver{}
	consumer := testConsumer(source, handlerFunc(func(context.Context, applicationeventstream.Event) (applicationeventstream.Outcome, error) {
		return applicationeventstream.OutcomeRetryable, errors.New("database unavailable")
	}))
	consumer.observer = observer
	consumer.lagSampleEvery = time.Hour
	err := consumer.Run(context.Background())
	if !errors.Is(err, ErrConsumerSession) {
		t.Fatalf("error = %v", err)
	}
	if observer.lag != 9 || observer.calls != 2 {
		t.Fatalf("lag=%d calls=%d, want lag 9 and initial+failure samples", observer.lag, observer.calls)
	}
}

func TestTotalGroupLagRejectsPartialPartitionFailures(t *testing.T) {
	lag := kadm.GroupLag{
		"frux.platform.backbone_probe.v1": {
			0: {Lag: 4},
			1: {Lag: -1, Err: errors.New("offset unavailable")},
		},
	}
	if _, err := totalGroupLag(lag); !errors.Is(err, ErrKafkaUnavailable) {
		t.Fatalf("error = %v, want ErrKafkaUnavailable", err)
	}
	delete(lag["frux.platform.backbone_probe.v1"], 1)
	total, err := totalGroupLag(lag)
	if err != nil || total != 4 {
		t.Fatalf("total=%d error=%v", total, err)
	}
}

func TestConsumerPreservesPartitionOrderingWithBoundedWorkers(t *testing.T) {
	var mu sync.Mutex
	seen := map[int32][]int64{}
	consumer := testConsumer(nil, handlerFunc(func(_ context.Context, event applicationeventstream.Event) (applicationeventstream.Outcome, error) {
		mu.Lock()
		seen[event.Metadata.Partition] = append(seen[event.Metadata.Partition], event.Metadata.Offset)
		mu.Unlock()
		return applicationeventstream.OutcomeDurableSuccess, nil
	}))
	consumer.concurrency = 2
	eligible, err := consumer.processBatch(context.Background(), []brokerRecord{
		probeRecord(t, 0, 0), probeRecord(t, 1, 0),
		probeRecord(t, 0, 1), probeRecord(t, 1, 1),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(eligible) != 2 ||
		len(seen[0]) != 2 || seen[0][0] != 0 || seen[0][1] != 1 ||
		len(seen[1]) != 2 || seen[1][0] != 0 || seen[1][1] != 1 {
		t.Fatalf("eligible=%+v seen=%+v", eligible, seen)
	}
}

func TestConsumerCommitsOnlyDurableAndTerminalOutcomes(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	source := &fakeConsumerSource{batches: [][]brokerRecord{{
		probeRecord(t, 0, 0), malformedRecord(0, 1), probeRecord(t, 0, 2),
	}}}
	var calls atomic.Int32
	consumer := testConsumer(source, handlerFunc(func(_ context.Context, _ applicationeventstream.Event) (applicationeventstream.Outcome, error) {
		if calls.Add(1) == 2 {
			cancel()
		}
		return applicationeventstream.OutcomeDurableSuccess, nil
	}))
	if err := consumer.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if len(source.commits) != 1 || len(source.commits[0]) != 1 ||
		source.commits[0][0].Offset != 2 || source.allows != 1 {
		t.Fatalf("commits=%+v allows=%d", source.commits, source.allows)
	}
}

func TestConsumerStopsSessionOnCommitUncertainty(t *testing.T) {
	source := &fakeConsumerSource{
		batches:   [][]brokerRecord{{probeRecord(t, 0, 0)}},
		commitErr: errors.New("coordinator unavailable"),
	}
	consumer := testConsumer(source, handlerFunc(func(context.Context, applicationeventstream.Event) (applicationeventstream.Outcome, error) {
		return applicationeventstream.OutcomeDurableSuccess, nil
	}))
	err := consumer.Run(context.Background())
	if !errors.Is(err, ErrCommitUncertain) || source.allows != 1 || !source.closed {
		t.Fatalf("error=%v allows=%d closed=%v", err, source.allows, source.closed)
	}
}

func TestSupervisorRedeliversAfterCommitFailureAndRestarts(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var sessions atomic.Int32
	var deliveries atomic.Int32
	supervisor := Supervisor{
		MinBackoff: time.Millisecond, MaxBackoff: time.Millisecond,
		NewConsumer: func(context.Context) (*Consumer, error) {
			session := sessions.Add(1)
			source := &fakeConsumerSource{batches: [][]brokerRecord{{probeRecord(t, 0, 7)}}}
			if session == 1 {
				source.commitErr = errors.New("commit response lost")
			}
			return testConsumer(source, handlerFunc(func(context.Context, applicationeventstream.Event) (applicationeventstream.Outcome, error) {
				if deliveries.Add(1) == 2 {
					cancel()
				}
				return applicationeventstream.OutcomeDurableSuccess, nil
			})), nil
		},
	}
	if err := supervisor.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if sessions.Load() < 2 || deliveries.Load() != 2 {
		t.Fatalf("sessions=%d deliveries=%d", sessions.Load(), deliveries.Load())
	}
}

func TestConsumerCancellationAndShutdownDeadline(t *testing.T) {
	t.Run("poll cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		source := &fakeConsumerSource{}
		consumer := testConsumer(source, handlerFunc(func(context.Context, applicationeventstream.Event) (applicationeventstream.Outcome, error) {
			t.Fatal("handler should not run")
			return "", nil
		}))
		if err := consumer.Run(ctx); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("bounded drain", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		source := &fakeConsumerSource{batches: [][]brokerRecord{{probeRecord(t, 0, 0)}}}
		started := make(chan struct{})
		release := make(chan struct{})
		consumer := testConsumer(source, handlerFunc(func(context.Context, applicationeventstream.Event) (applicationeventstream.Outcome, error) {
			close(started)
			<-release
			return applicationeventstream.OutcomeRetryable, errors.New("released after shutdown")
		}))
		consumer.drainTimeout = 5 * time.Millisecond
		done := make(chan error, 1)
		go func() { done <- consumer.Run(ctx) }()
		<-started
		cancel()
		select {
		case err := <-done:
			t.Fatalf("consumer released partition while handler was active: %v", err)
		case <-time.After(20 * time.Millisecond):
		}
		close(release)
		select {
		case err := <-done:
			if !errors.Is(err, ErrShutdownDeadline) {
				t.Fatalf("error = %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("consumer did not finish after handler returned")
		}
	})
}

func TestConsumerReportsCommittedLag(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	source := &fakeConsumerSource{
		batches: [][]brokerRecord{{probeRecord(t, 0, 3)}},
		lag:     7,
	}
	observer := &consumerObserver{}
	consumer := testConsumer(source, handlerFunc(func(context.Context, applicationeventstream.Event) (applicationeventstream.Outcome, error) {
		cancel()
		return applicationeventstream.OutcomeDurableSuccess, nil
	}))
	consumer.observer = observer
	consumer.lagSampleEvery = 0
	if err := consumer.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if observer.lag != 7 {
		t.Fatalf("lag = %d, want 7", observer.lag)
	}
}

func testConsumer(source consumerSource, handler applicationeventstream.Handler) *Consumer {
	return &Consumer{
		source: source, topicID: TopicBackboneProbe,
		topicName: "frux.platform.backbone_probe.v1",
		groupID:   GroupBackboneProbeActive,
		groupName: "frux.platform.backbone_probe.active.v1",
		handler:   handler, maxPollRecords: 100, concurrency: 2,
		drainTimeout: 50 * time.Millisecond, commitTimeout: time.Second,
		closeTimeout: time.Second,
	}
}

func probeRecord(t *testing.T, partition int32, offset int64) brokerRecord {
	t.Helper()
	now := time.Now().UTC()
	key := []byte("probe:one")
	value, err := EncodeEvent(TopicBackboneProbe, key, EventMetadata{
		EventID: "event-" + time.Now().Format("150405.000000000"),
		Type:    EventTypeBackboneProbe, SchemaVersion: 1,
		OccurredAt: now.Add(-time.Second), ProducedAt: now,
		Producer: ProducerPlatformWorker,
	}, BackboneProbePayload{ProbeID: "one", Source: "test"})
	if err != nil {
		t.Fatal(err)
	}
	return brokerRecord{
		Topic:     "frux.platform.backbone_probe.v1",
		Partition: partition, Offset: offset, Timestamp: now,
		Key: key, Value: value,
	}
}

func malformedRecord(partition int32, offset int64) brokerRecord {
	return brokerRecord{
		Topic:     "frux.platform.backbone_probe.v1",
		Partition: partition, Offset: offset, Timestamp: time.Now().UTC(),
		Key: []byte("probe:one"), Value: []byte("{"),
	}
}
