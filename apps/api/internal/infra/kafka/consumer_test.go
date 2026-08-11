package infrakafka

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	applicationeventstream "github.com/shiyudesu/frux/internal/application/eventstream"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
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
	pauses        int
	resumes       int
	paused        []int32
	resumed       []int32
	pauseEvents   chan int32
	resumeEvents  chan int32
	onCommit      func()
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
	onCommit := f.onCommit
	f.mu.Unlock()
	if onCommit != nil {
		onCommit()
	}
	f.mu.Lock()
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

func (f *fakeConsumerSource) Pause(_ string, partition int32) {
	f.mu.Lock()
	f.pauses++
	f.paused = append(f.paused, partition)
	events := f.pauseEvents
	f.mu.Unlock()
	if events != nil {
		events <- partition
	}
}

func (f *fakeConsumerSource) Resume(_ string, partition int32) {
	f.mu.Lock()
	f.resumes++
	f.resumed = append(f.resumed, partition)
	events := f.resumeEvents
	f.mu.Unlock()
	if events != nil {
		events <- partition
	}
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

type requestedDelayError struct {
	delay time.Duration
}

func (e requestedDelayError) Error() string             { return "retry later" }
func (e requestedDelayError) RetryAfter() time.Duration { return e.delay }

type publishedRecovery struct {
	destination TopicID
	key         []byte
	value       []byte
	headers     []applicationeventstream.Header
}

type fakeRecoveryPublisher struct {
	mu        sync.Mutex
	records   []publishedRecovery
	err       error
	onPublish func()
	order     *[]string
}

func (p *fakeRecoveryPublisher) PublishRecovery(
	_ context.Context,
	destination TopicID,
	key, value []byte,
	headers []applicationeventstream.Header,
) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.records = append(p.records, publishedRecovery{
		destination: destination,
		key:         append([]byte(nil), key...),
		value:       append([]byte(nil), value...),
		headers:     cloneHeaders(headers),
	})
	if p.order != nil {
		*p.order = append(*p.order, "publish")
	}
	if p.onPublish != nil {
		p.onPublish()
	}
	return p.err
}

type consumerObserver struct {
	lag      int64
	calls    int
	dataLoss int
	progress int
	cancel   context.CancelFunc
}

type sessionObserver struct {
	mu      sync.Mutex
	results []string
}

func (o *sessionObserver) ObserveConsumerSession(
	_ ConsumerGroupID,
	_ ConsumerStage,
	result string,
) {
	o.mu.Lock()
	o.results = append(o.results, result)
	o.mu.Unlock()
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
func (o *consumerObserver) ObserveLag(
	_ TopicID,
	_ ConsumerGroupID,
	_ ConsumerStage,
	lag int64,
) {
	o.lag = lag
	o.calls++
	if o.cancel != nil {
		o.cancel()
	}
}
func (o *consumerObserver) ObserveDataLoss(TopicID, ConsumerGroupID) {
	o.dataLoss++
}
func (o *consumerObserver) ObserveRecoveryProgress(ConsumerGroupID, string) {
	o.progress++
}

func TestConsumerFailsBeforeProcessingRecoveredDataLoss(t *testing.T) {
	source := &fakeConsumerSource{
		batches:    [][]brokerRecord{{probeRecord(t, 0, 3)}},
		dataLosses: []bool{true},
	}
	observer := &consumerObserver{}
	var calls atomic.Int32
	consumer := testConsumer(source, handlerFunc(func(context.Context, applicationeventstream.Event) (applicationeventstream.Outcome, error) {
		calls.Add(1)
		return applicationeventstream.OutcomeDurableSuccess, nil
	}))
	consumer.observer = observer
	err := consumer.Run(context.Background())
	if !errors.Is(err, ErrConsumerDataLoss) ||
		observer.dataLoss != 1 || len(source.commits) != 0 ||
		calls.Load() != 0 {
		t.Fatalf(
			"error=%v dataLoss=%d commits=%d calls=%d",
			err,
			observer.dataLoss,
			len(source.commits),
			calls.Load(),
		)
	}
}

func TestConsumerDataLossOnlyPollStopsBeforeRebalanceRelease(t *testing.T) {
	source := &fakeConsumerSource{
		batches:    [][]brokerRecord{{}},
		dataLosses: []bool{true},
	}
	observer := &consumerObserver{}
	consumer := testConsumer(source, handlerFunc(func(context.Context, applicationeventstream.Event) (applicationeventstream.Outcome, error) {
		t.Fatal("handler should not run")
		return applicationeventstream.OutcomeDurableSuccess, nil
	}))
	consumer.observer = observer
	if err := consumer.Run(context.Background()); !errors.Is(err, ErrConsumerDataLoss) {
		t.Fatalf("error = %v", err)
	}
	if observer.dataLoss != 1 || source.allows != 0 {
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
	if !errors.Is(err, ErrConsumerDataLoss) || observer.dataLoss != 1 {
		t.Fatalf("error=%v dataLoss=%d", err, observer.dataLoss)
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

func TestConsumerRetriesRequestedDelayWithoutRestartingSession(t *testing.T) {
	var calls atomic.Int32
	consumer := testConsumer(nil, handlerFunc(func(context.Context, applicationeventstream.Event) (applicationeventstream.Outcome, error) {
		if calls.Add(1) < 3 {
			return applicationeventstream.OutcomeRetryable,
				requestedDelayError{delay: time.Millisecond}
		}
		return applicationeventstream.OutcomeDurableSuccess, nil
	}))
	outcome, err := consumer.handleWithRequestedRetry(
		context.Background(),
		applicationeventstream.Event{},
	)
	if err != nil || outcome != applicationeventstream.OutcomeDurableSuccess ||
		calls.Load() != 3 {
		t.Fatalf("outcome=%s error=%v calls=%d", outcome, err, calls.Load())
	}
}

func TestRetryTopicConsumerPublishesBeforeSourceCommit(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	record := videoRecord(t, 0, 7)
	order := make([]string, 0, 2)
	source := &fakeConsumerSource{batches: [][]brokerRecord{{record}}}
	publisher := &fakeRecoveryPublisher{order: &order, onPublish: cancel}
	consumer := testRecoveryConsumer(
		t,
		source,
		GroupFeedVideoPublishedActive,
		0,
		handlerFunc(func(context.Context, applicationeventstream.Event) (applicationeventstream.Outcome, error) {
			return applicationeventstream.OutcomeRetryable, errors.New("redis unavailable")
		}),
		publisher,
	)
	consumer.source = &orderedConsumerSource{
		fakeConsumerSource: source,
		order:              &order,
	}

	if err := consumer.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if len(order) != 2 || order[0] != "publish" || order[1] != "commit" {
		t.Fatalf("operation order = %v", order)
	}
	if len(publisher.records) != 1 ||
		publisher.records[0].destination != TopicFeedVideoPublishedRetry5s ||
		string(publisher.records[0].key) != string(record.Key) ||
		string(publisher.records[0].value) != string(record.Value) {
		t.Fatalf("published recovery = %+v", publisher.records)
	}
}

type orderedConsumerSource struct {
	*fakeConsumerSource
	order *[]string
}

func (s *orderedConsumerSource) Commit(ctx context.Context, records []brokerRecord) error {
	if s.order != nil {
		*s.order = append(*s.order, "commit")
	}
	return s.fakeConsumerSource.Commit(ctx, records)
}

func TestRetryPublicationFailureLeavesSourceUncommitted(t *testing.T) {
	source := &fakeConsumerSource{batches: [][]brokerRecord{{videoRecord(t, 0, 8)}}}
	publisher := &fakeRecoveryPublisher{err: errors.New("broker rejected")}
	consumer := testRecoveryConsumer(
		t,
		source,
		GroupFeedVideoPublishedActive,
		0,
		handlerFunc(func(context.Context, applicationeventstream.Event) (applicationeventstream.Outcome, error) {
			return applicationeventstream.OutcomeRetryable, errors.New("redis unavailable")
		}),
		publisher,
	)
	err := consumer.Run(context.Background())
	if !errors.Is(err, ErrRecoveryPublish) || len(source.commits) != 0 {
		t.Fatalf("error=%v commits=%+v", err, source.commits)
	}
}

func TestRecoveryPublicationErrorsPreserveClassificationWithoutLeaking(t *testing.T) {
	const secret = "broker=10.0.0.8 password=super-secret"
	tests := []struct {
		name      string
		cause     error
		match     error
		retryable bool
	}{
		{
			name:  "authorization is fatal",
			cause: fmt.Errorf("%s: %w", secret, kerr.TopicAuthorizationFailed),
			match: kerr.TopicAuthorizationFailed,
		},
		{
			name:      "ordinary broker failure is retryable",
			cause:     fmt.Errorf("%s: %w", secret, kerr.BrokerNotAvailable),
			match:     kerr.BrokerNotAvailable,
			retryable: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := &fakeConsumerSource{
				batches: [][]brokerRecord{{videoRecord(t, 0, 108)}},
			}
			consumer := testRecoveryConsumer(
				t,
				source,
				GroupFeedVideoPublishedActive,
				0,
				handlerFunc(func(
					context.Context,
					applicationeventstream.Event,
				) (applicationeventstream.Outcome, error) {
					return applicationeventstream.OutcomeRetryable, errors.New("dependency unavailable")
				}),
				&fakeRecoveryPublisher{err: test.cause},
			)
			err := consumer.Run(context.Background())
			if !errors.Is(err, ErrRecoveryPublish) {
				t.Fatalf("error does not retain recovery sentinel: %v", err)
			}
			if !errors.Is(err, test.match) {
				t.Fatalf("error does not retain Kafka cause %v: %v", test.match, err)
			}
			if got := RetryableConsumerError(err); got != test.retryable {
				t.Fatalf("retryable=%t want %t: %v", got, test.retryable, err)
			}
			displayed := err.Error()
			if len(displayed) > 128 ||
				strings.Contains(displayed, "10.0.0.8") ||
				strings.Contains(displayed, "super-secret") ||
				strings.Contains(displayed, "broker not available") {
				t.Fatalf("unsafe displayed error %q", displayed)
			}
		})
	}
}

func TestDependencyDeadlineRoutesAfterRegisteredRetryExhaustion(t *testing.T) {
	for _, test := range []struct {
		name        string
		tier        int
		record      func(*testing.T) brokerRecord
		destination TopicID
	}{
		{
			name: "source to first retry", tier: 0,
			record: func(t *testing.T) brokerRecord {
				return videoRecord(t, 0, 81)
			},
			destination: TopicFeedVideoPublishedRetry5s,
		},
		{
			name: "final retry to DLQ", tier: 5,
			record: func(t *testing.T) brokerRecord {
				return retryTierRecord(t, 5, time.Now().UTC())
			},
			destination: TopicFeedVideoPublishedDLQ,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			publisher := &fakeRecoveryPublisher{}
			consumer := testRecoveryConsumer(
				t,
				&fakeConsumerSource{},
				GroupFeedVideoPublishedActive,
				test.tier,
				handlerFunc(func(
					context.Context,
					applicationeventstream.Event,
				) (applicationeventstream.Outcome, error) {
					return applicationeventstream.OutcomeRetryable, context.DeadlineExceeded
				}),
				publisher,
			)
			result := consumer.processPartition(
				context.Background(),
				[]brokerRecord{test.record(t)},
			)
			if result.err != nil || result.eligible == nil ||
				len(publisher.records) != 1 ||
				publisher.records[0].destination != test.destination {
				t.Fatalf("result=%+v publications=%+v", result, publisher.records)
			}
		})
	}
}

func TestParentCancellationDoesNotRouteRecovery(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	publisher := &fakeRecoveryPublisher{}
	consumer := testRecoveryConsumer(
		t,
		&fakeConsumerSource{},
		GroupFeedVideoPublishedActive,
		0,
		handlerFunc(func(
			context.Context,
			applicationeventstream.Event,
		) (applicationeventstream.Outcome, error) {
			cancel()
			return applicationeventstream.OutcomeRetryable, context.DeadlineExceeded
		}),
		publisher,
	)
	result := consumer.processPartition(ctx, []brokerRecord{videoRecord(t, 0, 82)})
	if !errors.Is(result.err, context.Canceled) || result.eligible != nil ||
		len(publisher.records) != 0 {
		t.Fatalf("result=%+v publications=%+v", result, publisher.records)
	}
}

func TestInvalidRetryMetadataIsQuarantinedBeforeCommit(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*brokerRecord)
		code   RecoveryMetadataCode
	}{
		{
			name: "malformed",
			mutate: func(record *brokerRecord) {
				record.Headers = []applicationeventstream.Header{{
					Key: RecoveryHeaderKey, Value: []byte(`{"tier":`),
				}}
			},
			code: RecoveryMetadataInvalid,
		},
		{
			name: "inconsistent tier",
			mutate: func(record *brokerRecord) {
				var metadata RecoveryMetadata
				if err := decodeStrict(record.Headers[0].Value, &metadata); err != nil {
					t.Fatal(err)
				}
				metadata.Tier = 2
				encoded, err := json.Marshal(metadata)
				if err != nil {
					t.Fatal(err)
				}
				record.Headers[0].Value = encoded
			},
			code: RecoveryMetadataInvalidAttempt,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			record := retryTierRecord(t, 1, time.Now().UTC())
			test.mutate(&record)
			ctx, cancel := context.WithCancel(context.Background())
			source := &fakeConsumerSource{batches: [][]brokerRecord{{record}}}
			publisher := &fakeRecoveryPublisher{onPublish: cancel}
			var handlerCalls atomic.Int32
			consumer := testRecoveryConsumer(
				t,
				source,
				GroupFeedVideoPublishedActive,
				1,
				handlerFunc(func(
					context.Context,
					applicationeventstream.Event,
				) (applicationeventstream.Outcome, error) {
					handlerCalls.Add(1)
					return applicationeventstream.OutcomeDurableSuccess, nil
				}),
				publisher,
			)
			if err := consumer.Run(ctx); err != nil {
				t.Fatal(err)
			}
			if len(source.commits) != 1 || len(source.commits[0]) != 1 ||
				len(publisher.records) != 1 || handlerCalls.Load() != 0 {
				t.Fatalf(
					"commits=%+v publications=%+v handler_calls=%d",
					source.commits, publisher.records, handlerCalls.Load(),
				)
			}
			published := publisher.records[0]
			if published.destination != TopicFeedVideoPublishedDLQ ||
				!bytes.Equal(published.key, record.Key) ||
				!bytes.Equal(published.value, record.Value) {
				t.Fatalf("quarantine publication=%+v", published)
			}
			metadata, err := DecodeRecoveryQuarantineHeaders(
				"", published.destination, published.headers,
				published.key, published.value,
			)
			if err != nil || metadata.MetadataCode != test.code ||
				metadata.ConsumedTopic != record.Topic ||
				metadata.ConsumedPartition != record.Partition ||
				metadata.ConsumedOffset != record.Offset ||
				metadata.ConsumerGroup != GroupFeedVideoPublishedActive ||
				metadata.FailureClass != FailureRecoveryMetadataInvalid ||
				!metadata.NonReplayable {
				t.Fatalf("metadata=%+v err=%v", metadata, err)
			}
		})
	}
}

func TestInvalidRetryMetadataPublicationFailureLeavesOffsetUncommitted(t *testing.T) {
	record := retryTierRecord(t, 1, time.Now().UTC())
	record.Headers = []applicationeventstream.Header{{
		Key: RecoveryHeaderKey, Value: []byte(`{"obsolete":true}`),
	}}
	source := &fakeConsumerSource{batches: [][]brokerRecord{{record}}}
	publisher := &fakeRecoveryPublisher{err: errors.New("broker rejected")}
	consumer := testRecoveryConsumer(
		t,
		source,
		GroupFeedVideoPublishedActive,
		1,
		handlerFunc(func(
			context.Context,
			applicationeventstream.Event,
		) (applicationeventstream.Outcome, error) {
			t.Fatal("invalid recovery metadata must not reach the handler")
			return "", nil
		}),
		publisher,
	)
	err := consumer.Run(context.Background())
	if !errors.Is(err, ErrRecoveryPublish) || len(source.commits) != 0 ||
		len(publisher.records) != 1 {
		t.Fatalf(
			"error=%v commits=%+v publications=%+v",
			err, source.commits, publisher.records,
		)
	}
}

func TestQuarantinePublicationErrorPreservesAuthorizationCause(t *testing.T) {
	record := retryTierRecord(t, 1, time.Now().UTC())
	record.Headers = []applicationeventstream.Header{{
		Key: RecoveryHeaderKey, Value: []byte(`{"obsolete":true}`),
	}}
	consumer := testRecoveryConsumer(
		t,
		&fakeConsumerSource{batches: [][]brokerRecord{{record}}},
		GroupFeedVideoPublishedActive,
		1,
		handlerFunc(func(
			context.Context,
			applicationeventstream.Event,
		) (applicationeventstream.Outcome, error) {
			t.Fatal("invalid recovery metadata must not reach the handler")
			return "", nil
		}),
		&fakeRecoveryPublisher{
			err: fmt.Errorf(
				"broker=private user=frux credential=hidden: %w",
				kerr.TopicAuthorizationFailed,
			),
		},
	)
	err := consumer.Run(context.Background())
	if !errors.Is(err, ErrRecoveryPublish) ||
		!errors.Is(err, kerr.TopicAuthorizationFailed) ||
		RetryableConsumerError(err) {
		t.Fatalf("quarantine publication error classification = %v", err)
	}
	if displayed := err.Error(); len(displayed) > 128 ||
		strings.Contains(displayed, "private") ||
		strings.Contains(displayed, "frux") ||
		strings.Contains(displayed, "hidden") {
		t.Fatalf("unsafe displayed error %q", displayed)
	}
}

func TestRetryPublishCommitCrashWindowProducesSafeDuplicate(t *testing.T) {
	record := videoRecord(t, 0, 9)
	publisher := &fakeRecoveryPublisher{}
	firstSource := &fakeConsumerSource{
		batches:   [][]brokerRecord{{record}},
		commitErr: errors.New("commit response lost"),
	}
	first := testRecoveryConsumer(
		t,
		firstSource,
		GroupFeedVideoPublishedActive,
		0,
		handlerFunc(func(context.Context, applicationeventstream.Event) (applicationeventstream.Outcome, error) {
			return applicationeventstream.OutcomeRetryable, errors.New("redis unavailable")
		}),
		publisher,
	)
	if err := first.Run(context.Background()); !errors.Is(err, ErrCommitUncertain) {
		t.Fatalf("first error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	publisher.onPublish = cancel
	secondSource := &fakeConsumerSource{batches: [][]brokerRecord{{record}}}
	second := testRecoveryConsumer(
		t,
		secondSource,
		GroupFeedVideoPublishedActive,
		0,
		handlerFunc(func(context.Context, applicationeventstream.Event) (applicationeventstream.Outcome, error) {
			return applicationeventstream.OutcomeRetryable, errors.New("redis unavailable")
		}),
		publisher,
	)
	if err := second.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if len(publisher.records) != 2 {
		t.Fatalf("publications = %d", len(publisher.records))
	}
	firstMetadata, err := DecodeRecoveryHeaders(
		"",
		publisher.records[0].destination,
		publisher.records[0].headers,
		publisher.records[0].key,
		publisher.records[0].value,
	)
	if err != nil {
		t.Fatal(err)
	}
	secondMetadata, err := DecodeRecoveryHeaders(
		"",
		publisher.records[1].destination,
		publisher.records[1].headers,
		publisher.records[1].key,
		publisher.records[1].value,
	)
	if err != nil {
		t.Fatal(err)
	}
	if firstMetadata.EventID != secondMetadata.EventID ||
		firstMetadata.SourceOffset != secondMetadata.SourceOffset ||
		firstMetadata.PayloadSHA256 != secondMetadata.PayloadSHA256 ||
		string(publisher.records[0].value) != string(publisher.records[1].value) {
		t.Fatalf("duplicate metadata changed: first=%+v second=%+v", firstMetadata, secondMetadata)
	}
}

func TestRetryTierDelayDoesNotBlockOtherPartitionsAndResumesOnce(t *testing.T) {
	notBefore := time.Now().UTC().Add(200 * time.Millisecond)
	delayed := retryTierRecord(t, 1, notBefore)
	delayed.Partition = 0
	readyFirst := retryTierRecord(t, 1, time.Now().UTC())
	readyFirst.Partition = 1
	readyFirst.Offset = 2
	readySecond := retryTierRecord(t, 1, time.Now().UTC())
	readySecond.Partition = 1
	readySecond.Offset = 3
	source := &fakeConsumerSource{
		batches: [][]brokerRecord{{delayed, readyFirst}, {readySecond}},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var mu sync.Mutex
	var partitions []int32
	var handledAt []time.Time
	consumer := testRecoveryConsumer(
		t,
		source,
		GroupFeedVideoPublishedActive,
		1,
		handlerFunc(func(_ context.Context, event applicationeventstream.Event) (applicationeventstream.Outcome, error) {
			mu.Lock()
			partitions = append(partitions, event.Metadata.Partition)
			handledAt = append(handledAt, time.Now())
			mu.Unlock()
			if event.Metadata.Partition == 0 {
				cancel()
			}
			return applicationeventstream.OutcomeDurableSuccess, nil
		}),
		&fakeRecoveryPublisher{},
	)
	started := time.Now()
	if err := consumer.Run(ctx); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(partitions) != 3 ||
		partitions[0] != 1 || partitions[1] != 1 || partitions[2] != 0 {
		t.Fatalf("handled partitions=%v", partitions)
	}
	if handledAt[2].Before(notBefore) {
		t.Fatalf("delayed record handled at %s before %s", handledAt[2], notBefore)
	}
	if source.pauses != 1 || source.resumes != 1 ||
		len(source.paused) != 1 || source.paused[0] != 0 ||
		len(source.resumed) != 1 || source.resumed[0] != 0 {
		t.Fatalf(
			"pauses=%d resumes=%d paused=%v resumed=%v elapsed=%s",
			source.pauses,
			source.resumes,
			source.paused,
			source.resumed,
			time.Since(started),
		)
	}
}

func TestRetryTierDurableHandlingReportsRecoveryProgress(t *testing.T) {
	record := retryTierRecord(t, 1, time.Now().UTC())
	observer := &consumerObserver{}
	consumer := testRecoveryConsumer(
		t,
		&fakeConsumerSource{},
		GroupFeedVideoPublishedActive,
		1,
		handlerFunc(func(context.Context, applicationeventstream.Event) (applicationeventstream.Outcome, error) {
			return applicationeventstream.OutcomeDurableSuccess, nil
		}),
		&fakeRecoveryPublisher{},
	)
	consumer.observer = observer
	result := consumer.processPartition(context.Background(), []brokerRecord{record})
	if result.err != nil || result.eligible == nil || observer.progress != 1 {
		t.Fatalf("result=%+v progress=%d", result, observer.progress)
	}
}

func TestRetryTierPauseIsDiscardedOnCancellation(t *testing.T) {
	record := retryTierRecord(t, 1, time.Now().UTC().Add(time.Second))
	source := &fakeConsumerSource{
		batches:      [][]brokerRecord{{record}},
		pauseEvents:  make(chan int32, 1),
		resumeEvents: make(chan int32, 1),
	}
	consumer := testRecoveryConsumer(
		t,
		source,
		GroupFeedVideoPublishedActive,
		1,
		handlerFunc(func(context.Context, applicationeventstream.Event) (applicationeventstream.Outcome, error) {
			return applicationeventstream.OutcomeDurableSuccess, nil
		}),
		&fakeRecoveryPublisher{},
	)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- consumer.Run(ctx)
	}()
	select {
	case partition := <-source.pauseEvents:
		if partition != 0 {
			t.Fatalf("paused partition=%d", partition)
		}
	case <-time.After(time.Second):
		t.Fatal("partition was not paused")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("consumer did not stop")
	}
	select {
	case partition := <-source.resumeEvents:
		t.Fatalf("session shutdown resumed stale partition %d", partition)
	case <-time.After(20 * time.Millisecond):
	}
	if source.pauses != 1 || source.resumes != 0 {
		t.Fatalf("pauses=%d resumes=%d", source.pauses, source.resumes)
	}
}

func TestRetryTierRevocationDiscardsDelayedOwnership(t *testing.T) {
	now := time.Now().UTC()
	source := &fakeConsumerSource{}
	consumer := testRecoveryConsumer(
		t,
		source,
		GroupFeedVideoPublishedActive,
		1,
		handlerFunc(func(context.Context, applicationeventstream.Event) (applicationeventstream.Outcome, error) {
			t.Fatal("revoked delayed record must not be handled by its old owner")
			return "", nil
		}),
		&fakeRecoveryPublisher{},
	)
	consumer.now = func() time.Time { return now }
	record := retryTierRecord(t, 1, now.Add(time.Minute))
	consumer.delayPartition(result{
		delayed: []brokerRecord{record}, notBefore: now.Add(time.Minute), tier: 1,
	})

	consumer.discardDelayedPartitions(
		map[string][]int32{record.Topic: {record.Partition}},
		"revoked",
	)
	consumer.now = func() time.Time { return now.Add(2 * time.Minute) }
	if ready := consumer.takeReadyDelayed(); len(ready.records) != 0 {
		ready.release()
		t.Fatalf("old owner retained revoked records: %+v", ready.records)
	}
	if len(consumer.delayed) != 0 || source.resumes != 0 {
		t.Fatalf("delayed=%+v resumes=%d", consumer.delayed, source.resumes)
	}
}

func TestRetryTierReassignmentRefetchesDelayedRecord(t *testing.T) {
	now := time.Now().UTC()
	record := retryTierRecord(t, 1, now.Add(time.Minute))
	oldSource := &fakeConsumerSource{}
	oldOwner := testRecoveryConsumer(
		t,
		oldSource,
		GroupFeedVideoPublishedActive,
		1,
		handlerFunc(func(context.Context, applicationeventstream.Event) (applicationeventstream.Outcome, error) {
			t.Fatal("old owner must not handle reassigned delayed record")
			return "", nil
		}),
		&fakeRecoveryPublisher{},
	)
	oldOwner.now = func() time.Time { return now }
	oldOwner.delayPartition(result{
		delayed: []brokerRecord{record}, notBefore: now.Add(time.Minute), tier: 1,
	})
	oldOwner.discardDelayedPartitions(
		map[string][]int32{record.Topic: {record.Partition}},
		"lost",
	)

	var handled atomic.Int32
	newOwner := testRecoveryConsumer(
		t,
		&fakeConsumerSource{},
		GroupFeedVideoPublishedActive,
		1,
		handlerFunc(func(context.Context, applicationeventstream.Event) (applicationeventstream.Outcome, error) {
			handled.Add(1)
			return applicationeventstream.OutcomeDurableSuccess, nil
		}),
		&fakeRecoveryPublisher{},
	)
	newOwner.now = func() time.Time { return now.Add(2 * time.Minute) }
	refetched := record
	refetched.resumed = false
	result := newOwner.processPartition(context.Background(), []brokerRecord{refetched})
	if result.err != nil || result.eligible == nil || handled.Load() != 1 {
		t.Fatalf("result=%+v handled=%d", result, handled.Load())
	}
	if ready := oldOwner.takeReadyDelayed(); len(ready.records) != 0 {
		ready.release()
		t.Fatalf("old owner later returned %+v", ready.records)
	}
}

func TestRetryTierRevocationLeavesOtherPartitionsDelayed(t *testing.T) {
	now := time.Now().UTC()
	source := &fakeConsumerSource{}
	consumer := testRecoveryConsumer(
		t,
		source,
		GroupFeedVideoPublishedActive,
		1,
		handlerFunc(func(context.Context, applicationeventstream.Event) (applicationeventstream.Outcome, error) {
			return applicationeventstream.OutcomeDurableSuccess, nil
		}),
		&fakeRecoveryPublisher{},
	)
	consumer.now = func() time.Time { return now }
	revoked := retryTierRecord(t, 1, now.Add(time.Minute))
	revoked.Partition = 0
	unaffected := retryTierRecord(t, 1, now.Add(2*time.Minute))
	unaffected.Partition = 1
	consumer.delayPartition(result{
		delayed: []brokerRecord{revoked}, notBefore: now.Add(time.Minute), tier: 1,
	})
	consumer.delayPartition(result{
		delayed: []brokerRecord{unaffected}, notBefore: now.Add(2 * time.Minute), tier: 1,
	})

	consumer.discardDelayedPartitions(
		map[string][]int32{revoked.Topic: {revoked.Partition}},
		"revoked",
	)
	consumer.delayedMu.Lock()
	_, revokedPresent := consumer.delayed[delayedPartitionKey{
		topic: revoked.Topic, partition: revoked.Partition,
	}]
	_, unaffectedPresent := consumer.delayed[delayedPartitionKey{
		topic: unaffected.Topic, partition: unaffected.Partition,
	}]
	consumer.delayedMu.Unlock()
	if revokedPresent || !unaffectedPresent {
		t.Fatalf("delayed=%+v", consumer.delayed)
	}
	consumer.now = func() time.Time { return now.Add(3 * time.Minute) }
	ready := consumer.takeReadyDelayed()
	defer ready.release()
	if len(ready.records) != 1 ||
		ready.records[0].Partition != unaffected.Partition ||
		source.resumes != 1 {
		t.Fatalf("ready=%+v resumes=%d", ready.records, source.resumes)
	}
}

func TestRetryTierReadyAtRevokeIsDiscardedWithoutCommit(t *testing.T) {
	now := time.Now().UTC()
	source := &fakeConsumerSource{}
	consumer := testRecoveryConsumer(
		t,
		source,
		GroupFeedVideoPublishedActive,
		1,
		handlerFunc(func(context.Context, applicationeventstream.Event) (applicationeventstream.Outcome, error) {
			t.Fatal("revoked ready record must not be handled")
			return "", nil
		}),
		&fakeRecoveryPublisher{},
	)
	consumer.now = func() time.Time { return now.Add(time.Minute) }
	record := retryTierRecord(t, 1, now)
	consumer.delayPartition(result{
		delayed: []brokerRecord{record}, notBefore: now, tier: 1,
	})

	consumer.partitions.discard(
		map[string][]int32{record.Topic: {record.Partition}},
		"revoked",
	)
	ready := consumer.takeReadyDelayed()
	ready.release()
	if len(ready.records) != 0 || len(source.commits) != 0 {
		t.Fatalf("ready=%+v commits=%+v", ready.records, source.commits)
	}
}

func TestRetryTierRevokeDuringHandlingWaitsForCommit(t *testing.T) {
	now := time.Now().UTC()
	handlerStarted := make(chan struct{})
	releaseHandler := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	source := &fakeConsumerSource{onCommit: cancel}
	consumer := testRecoveryConsumer(
		t,
		source,
		GroupFeedVideoPublishedActive,
		1,
		handlerFunc(func(context.Context, applicationeventstream.Event) (applicationeventstream.Outcome, error) {
			close(handlerStarted)
			<-releaseHandler
			return applicationeventstream.OutcomeDurableSuccess, nil
		}),
		&fakeRecoveryPublisher{},
	)
	consumer.now = func() time.Time { return now.Add(time.Minute) }
	record := retryTierRecord(t, 1, now)
	consumer.delayPartition(result{
		delayed: []brokerRecord{record}, notBefore: now, tier: 1,
	})

	runDone := make(chan error, 1)
	go func() {
		runDone <- consumer.Run(ctx)
	}()
	select {
	case <-handlerStarted:
	case <-time.After(time.Second):
		t.Fatal("delayed handler did not start")
	}
	revokeDone := make(chan struct{})
	go func() {
		consumer.partitions.discard(
			map[string][]int32{record.Topic: {record.Partition}},
			"revoked",
		)
		close(revokeDone)
	}()
	select {
	case <-revokeDone:
		t.Fatal("revocation completed while delayed handler owned the partition")
	case <-time.After(20 * time.Millisecond):
	}
	close(releaseHandler)
	select {
	case err := <-runDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("consumer did not finish")
	}
	select {
	case <-revokeDone:
	case <-time.After(time.Second):
		t.Fatal("revocation did not finish after commit")
	}
	if len(source.commits) != 1 || len(source.commits[0]) != 1 {
		t.Fatalf("commits=%+v", source.commits)
	}
}

func TestRetryTierLossDuringHandlingAbortsCommit(t *testing.T) {
	now := time.Now().UTC()
	handlerStarted := make(chan struct{})
	releaseHandler := make(chan struct{})
	source := &fakeConsumerSource{}
	consumer := testRecoveryConsumer(
		t,
		source,
		GroupFeedVideoPublishedActive,
		1,
		handlerFunc(func(context.Context, applicationeventstream.Event) (applicationeventstream.Outcome, error) {
			close(handlerStarted)
			<-releaseHandler
			return applicationeventstream.OutcomeDurableSuccess, nil
		}),
		&fakeRecoveryPublisher{},
	)
	consumer.now = func() time.Time { return now.Add(time.Minute) }
	record := retryTierRecord(t, 1, now)
	consumer.delayPartition(result{
		delayed: []brokerRecord{record}, notBefore: now, tier: 1,
	})

	runDone := make(chan error, 1)
	go func() {
		runDone <- consumer.Run(context.Background())
	}()
	select {
	case <-handlerStarted:
	case <-time.After(time.Second):
		t.Fatal("delayed handler did not start")
	}
	consumer.partitions.discard(
		map[string][]int32{record.Topic: {record.Partition}},
		"lost",
	)
	close(releaseHandler)
	select {
	case err := <-runDone:
		if !errors.Is(err, ErrRebalanceDrain) {
			t.Fatalf("error=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("consumer did not abort after partition loss")
	}
	if len(source.commits) != 0 {
		t.Fatalf("commits=%+v", source.commits)
	}
}

func TestRetryTierReassignmentInvalidatesOldGeneration(t *testing.T) {
	now := time.Now().UTC()
	source := &fakeConsumerSource{}
	consumer := testRecoveryConsumer(
		t,
		source,
		GroupFeedVideoPublishedActive,
		1,
		handlerFunc(func(context.Context, applicationeventstream.Event) (applicationeventstream.Outcome, error) {
			return applicationeventstream.OutcomeDurableSuccess, nil
		}),
		&fakeRecoveryPublisher{},
	)
	consumer.now = func() time.Time { return now }
	record := retryTierRecord(t, 1, now.Add(time.Minute))
	consumer.assignRecordGenerations([]brokerRecord{record})
	consumer.delayPartition(result{
		delayed: []brokerRecord{record}, notBefore: now.Add(time.Minute), tier: 1,
	})
	partitions := map[string][]int32{record.Topic: {record.Partition}}
	consumer.partitions.discard(partitions, "lost")
	consumer.partitions.assigned(partitions)
	consumer.now = func() time.Time { return now.Add(2 * time.Minute) }

	ready := consumer.takeReadyDelayed()
	ready.release()
	if len(ready.records) != 0 || len(source.commits) != 0 {
		t.Fatalf("old generation ready=%+v commits=%+v", ready.records, source.commits)
	}
}

func TestRetryTierRevokeUnaffectedPartitionDoesNotWait(t *testing.T) {
	now := time.Now().UTC()
	consumer := testRecoveryConsumer(
		t,
		&fakeConsumerSource{},
		GroupFeedVideoPublishedActive,
		1,
		handlerFunc(func(context.Context, applicationeventstream.Event) (applicationeventstream.Outcome, error) {
			return applicationeventstream.OutcomeDurableSuccess, nil
		}),
		&fakeRecoveryPublisher{},
	)
	consumer.now = func() time.Time { return now.Add(time.Minute) }
	active := retryTierRecord(t, 1, now)
	active.Partition = 1
	revoked := retryTierRecord(t, 1, now.Add(2*time.Minute))
	revoked.Partition = 0
	consumer.delayPartition(result{
		delayed: []brokerRecord{active}, notBefore: now, tier: 1,
	})
	consumer.delayPartition(result{
		delayed: []brokerRecord{revoked}, notBefore: now.Add(2 * time.Minute), tier: 1,
	})
	ready := consumer.takeReadyDelayed()
	if len(ready.records) != 1 || ready.records[0].Partition != active.Partition {
		ready.release()
		t.Fatalf("ready=%+v", ready.records)
	}
	revokeDone := make(chan struct{})
	go func() {
		consumer.partitions.discard(
			map[string][]int32{revoked.Topic: {revoked.Partition}},
			"revoked",
		)
		close(revokeDone)
	}()
	select {
	case <-revokeDone:
	case <-time.After(time.Second):
		ready.release()
		t.Fatal("unaffected partition ownership blocked revocation")
	}
	ready.release()
}

func TestRetryTierProgressesToNextTierAndThenDLQ(t *testing.T) {
	t.Run("source contract failure to DLQ", func(t *testing.T) {
		record := videoRecord(t, 0, 10)
		record.Value = []byte("{")
		publisher := &fakeRecoveryPublisher{}
		consumer := testRecoveryConsumer(
			t,
			&fakeConsumerSource{},
			GroupFeedVideoPublishedActive,
			0,
			handlerFunc(func(context.Context, applicationeventstream.Event) (applicationeventstream.Outcome, error) {
				t.Fatal("handler must not receive a terminal contract failure")
				return "", nil
			}),
			publisher,
		)
		result := consumer.processPartition(context.Background(), []brokerRecord{record})
		if result.err != nil || result.eligible == nil ||
			len(publisher.records) != 1 ||
			publisher.records[0].destination != TopicFeedVideoPublishedDLQ {
			t.Fatalf("result=%+v records=%+v", result, publisher.records)
		}
		metadata, err := DecodeRecoveryHeaders(
			"",
			TopicFeedVideoPublishedDLQ,
			publisher.records[0].headers,
			publisher.records[0].key,
			publisher.records[0].value,
		)
		if err != nil {
			t.Fatal(err)
		}
		if metadata.FailureClass != FailureTerminalContract ||
			metadata.SchemaVersion != 0 ||
			!strings.HasPrefix(metadata.EventID, "sha256:") {
			t.Fatalf("metadata = %+v", metadata)
		}
	})

	t.Run("malformed source key reaches owning DLQ unchanged", func(t *testing.T) {
		record := videoRecord(t, 0, 11)
		record.Key = []byte("video:0")
		originalKey := append([]byte(nil), record.Key...)
		originalValue := append([]byte(nil), record.Value...)
		source := &fakeConsumerSource{batches: [][]brokerRecord{{record}}}
		ctx, cancel := context.WithCancel(context.Background())
		publisher := &fakeRecoveryPublisher{onPublish: cancel}
		consumer := testRecoveryConsumer(
			t,
			source,
			GroupFeedVideoPublishedActive,
			0,
			handlerFunc(func(context.Context, applicationeventstream.Event) (applicationeventstream.Outcome, error) {
				t.Fatal("handler must not receive a malformed source key")
				return "", nil
			}),
			publisher,
		)
		if err := consumer.Run(ctx); err != nil {
			t.Fatal(err)
		}
		if len(source.commits) != 1 || len(source.commits[0]) != 1 ||
			len(publisher.records) != 1 ||
			publisher.records[0].destination != TopicFeedVideoPublishedDLQ {
			t.Fatalf("commits=%+v records=%+v", source.commits, publisher.records)
		}
		published := publisher.records[0]
		if !bytes.Equal(published.key, originalKey) ||
			!bytes.Equal(published.value, originalValue) {
			t.Fatal("terminal poison routing changed key or value")
		}
		metadata, err := DecodeRecoveryHeaders(
			"", TopicFeedVideoPublishedDLQ,
			published.headers, published.key, published.value,
		)
		if err != nil || metadata.FailureClass != FailureTerminalContract {
			t.Fatalf("metadata=%+v err=%v", metadata, err)
		}
	})

	t.Run("retryable invalid key cannot bypass validation", func(t *testing.T) {
		record := videoRecord(t, 0, 12)
		record.Key = []byte("video:0")
		publisher := &fakeRecoveryPublisher{}
		consumer := testRecoveryConsumer(
			t,
			&fakeConsumerSource{},
			GroupFeedVideoPublishedActive,
			0,
			handlerFunc(func(context.Context, applicationeventstream.Event) (applicationeventstream.Outcome, error) {
				return applicationeventstream.OutcomeRetryable, errors.New("redis unavailable")
			}),
			publisher,
		)
		err := consumer.routeRecovery(
			context.Background(),
			record,
			nil,
			"event-video-42",
			1,
			FailureLocalRetryExhausted,
		)
		var metadataErr *RecoveryMetadataError
		if !errors.As(err, &metadataErr) ||
			metadataErr.Code != RecoveryMetadataInvalidSource ||
			len(publisher.records) != 0 {
			t.Fatalf("error=%v records=%+v", err, publisher.records)
		}
	})

	t.Run("next tier", func(t *testing.T) {
		record := retryTierRecord(t, 1, time.Now().UTC())
		publisher := &fakeRecoveryPublisher{}
		consumer := testRecoveryConsumer(
			t,
			&fakeConsumerSource{},
			GroupFeedVideoPublishedActive,
			1,
			handlerFunc(func(context.Context, applicationeventstream.Event) (applicationeventstream.Outcome, error) {
				return applicationeventstream.OutcomeRetryable, errors.New("redis unavailable")
			}),
			publisher,
		)
		result := consumer.processPartition(context.Background(), []brokerRecord{record})
		if result.err != nil || result.eligible == nil ||
			len(publisher.records) != 1 ||
			publisher.records[0].destination != TopicFeedVideoPublishedRetry30s {
			t.Fatalf("result=%+v records=%+v", result, publisher.records)
		}
		metadata, err := DecodeRecoveryHeaders(
			"",
			TopicFeedVideoPublishedRetry30s,
			publisher.records[0].headers,
			publisher.records[0].key,
			publisher.records[0].value,
		)
		if err != nil {
			t.Fatal(err)
		}
		if metadata.Attempt != 2 || metadata.Tier != 2 ||
			metadata.SourceOffset != 11 {
			t.Fatalf("metadata = %+v", metadata)
		}
	})

	t.Run("terminal to DLQ", func(t *testing.T) {
		record := retryTierRecord(t, 3, time.Now().UTC())
		publisher := &fakeRecoveryPublisher{}
		consumer := testRecoveryConsumer(
			t,
			&fakeConsumerSource{},
			GroupFeedVideoPublishedActive,
			3,
			handlerFunc(func(context.Context, applicationeventstream.Event) (applicationeventstream.Outcome, error) {
				return applicationeventstream.OutcomeTerminal, errors.New("deleted author")
			}),
			publisher,
		)
		result := consumer.processPartition(context.Background(), []brokerRecord{record})
		if result.err != nil || result.eligible == nil ||
			len(publisher.records) != 1 ||
			publisher.records[0].destination != TopicFeedVideoPublishedDLQ {
			t.Fatalf("result=%+v records=%+v", result, publisher.records)
		}
		metadata, err := DecodeRecoveryHeaders(
			"",
			TopicFeedVideoPublishedDLQ,
			publisher.records[0].headers,
			publisher.records[0].key,
			publisher.records[0].value,
		)
		if err != nil {
			t.Fatal(err)
		}
		if metadata.Tier != 0 || metadata.FailureClass != FailureTerminalDomain {
			t.Fatalf("metadata = %+v", metadata)
		}
	})

	t.Run("final tier exhaustion to DLQ", func(t *testing.T) {
		record := retryTierRecord(t, 5, time.Now().UTC())
		publisher := &fakeRecoveryPublisher{}
		consumer := testRecoveryConsumer(
			t,
			&fakeConsumerSource{},
			GroupFeedVideoPublishedActive,
			5,
			handlerFunc(func(context.Context, applicationeventstream.Event) (applicationeventstream.Outcome, error) {
				return applicationeventstream.OutcomeRetryable, errors.New("redis unavailable")
			}),
			publisher,
		)
		result := consumer.processPartition(context.Background(), []brokerRecord{record})
		if result.err != nil || result.eligible == nil ||
			len(publisher.records) != 1 ||
			publisher.records[0].destination != TopicFeedVideoPublishedDLQ {
			t.Fatalf("result=%+v records=%+v", result, publisher.records)
		}
		metadata, err := DecodeRecoveryHeaders(
			"",
			TopicFeedVideoPublishedDLQ,
			publisher.records[0].headers,
			publisher.records[0].key,
			publisher.records[0].value,
		)
		if err != nil {
			t.Fatal(err)
		}
		if metadata.Attempt != 6 || metadata.Tier != 0 ||
			metadata.FailureClass != FailureLocalRetryExhausted {
			t.Fatalf("metadata = %+v", metadata)
		}
	})
}

func TestBlockAndRetryStopsAfterRegisteredLocalBound(t *testing.T) {
	var calls atomic.Int32
	publisher := &fakeRecoveryPublisher{}
	consumer := testRecoveryConsumer(
		t,
		&fakeConsumerSource{},
		GroupBackboneProbeActive,
		0,
		handlerFunc(func(context.Context, applicationeventstream.Event) (applicationeventstream.Outcome, error) {
			calls.Add(1)
			return applicationeventstream.OutcomeRetryable, errors.New("database unavailable")
		}),
		publisher,
	)
	result := consumer.processPartition(
		context.Background(),
		[]brokerRecord{probeRecord(t, 0, 12)},
	)
	if result.eligible != nil || result.err == nil || calls.Load() != 3 ||
		len(publisher.records) != 0 {
		t.Fatalf(
			"result=%+v calls=%d publications=%d",
			result,
			calls.Load(),
			len(publisher.records),
		)
	}
}

func TestRegisteredLocalRetryHonorsCancellationAndTotalDelay(t *testing.T) {
	t.Run("cancellation", func(t *testing.T) {
		var calls atomic.Int32
		consumer := testRecoveryConsumer(
			t,
			&fakeConsumerSource{},
			GroupPersistActionActive,
			0,
			handlerFunc(func(context.Context, applicationeventstream.Event) (applicationeventstream.Outcome, error) {
				calls.Add(1)
				return applicationeventstream.OutcomeRetryable, errors.New("database unavailable")
			}),
			&fakeRecoveryPublisher{},
		)
		consumer.recovery.LocalRetry = LocalRetrySpec{
			MaxAttempts: 10, InitialDelay: time.Second,
			MaxDelay: time.Second, MaxTotalDelay: 5 * time.Second,
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		outcome, err, exhausted := consumer.handleWithRegisteredRetry(
			ctx,
			applicationeventstream.Event{},
		)
		if outcome != applicationeventstream.OutcomeRetryable ||
			!errors.Is(err, context.Canceled) || exhausted || calls.Load() != 1 {
			t.Fatalf(
				"outcome=%s error=%v exhausted=%t calls=%d",
				outcome,
				err,
				exhausted,
				calls.Load(),
			)
		}
	})

	t.Run("total delay", func(t *testing.T) {
		var calls atomic.Int32
		consumer := testRecoveryConsumer(
			t,
			&fakeConsumerSource{},
			GroupConsumeViewActive,
			0,
			handlerFunc(func(context.Context, applicationeventstream.Event) (applicationeventstream.Outcome, error) {
				calls.Add(1)
				return applicationeventstream.OutcomeRetryable, errors.New("database unavailable")
			}),
			&fakeRecoveryPublisher{},
		)
		consumer.recovery.LocalRetry = LocalRetrySpec{
			MaxAttempts: 10, InitialDelay: time.Millisecond,
			MaxDelay: 2 * time.Millisecond, MaxTotalDelay: time.Millisecond,
		}
		outcome, err, exhausted := consumer.handleWithRegisteredRetry(
			context.Background(),
			applicationeventstream.Event{},
		)
		if outcome != applicationeventstream.OutcomeRetryable ||
			err == nil || !exhausted || calls.Load() != 2 {
			t.Fatalf(
				"outcome=%s error=%v exhausted=%t calls=%d",
				outcome,
				err,
				exhausted,
				calls.Load(),
			)
		}
	})
}

func TestDurableJobCommitsAfterHandoffWithoutKafkaRecoveryRecord(t *testing.T) {
	publisher := &fakeRecoveryPublisher{}
	ctx, cancel := context.WithCancel(context.Background())
	source := &fakeConsumerSource{batches: [][]brokerRecord{{mediaRecord(t, 0, 4)}}}
	laterJobFailure := make(chan struct{})
	laterFailureObserved := make(chan struct{})
	consumer := testRecoveryConsumer(
		t,
		source,
		GroupMediaProcessingActive,
		0,
		handlerFunc(func(context.Context, applicationeventstream.Event) (applicationeventstream.Outcome, error) {
			go func() {
				<-laterJobFailure
				close(laterFailureObserved)
			}()
			cancel()
			return applicationeventstream.OutcomeDurableSuccess, nil
		}),
		publisher,
	)
	if err := consumer.Run(ctx); err != nil {
		t.Fatal(err)
	}
	close(laterJobFailure)
	select {
	case <-laterFailureObserved:
	case <-time.After(time.Second):
		t.Fatal("later durable job failure was not observed")
	}
	if len(source.commits) != 1 || len(source.commits[0]) != 1 ||
		source.commits[0][0].Offset != 4 || len(publisher.records) != 0 {
		t.Fatalf("commits=%+v publications=%d", source.commits, len(publisher.records))
	}
}

func TestRetryTopicRebalanceBeforeHandoffLeavesSourceUncommitted(t *testing.T) {
	rebalance := make(chan struct{}, 1)
	started := make(chan struct{})
	source := &fakeConsumerSource{}
	publisher := &fakeRecoveryPublisher{}
	consumer := testRecoveryConsumer(
		t,
		source,
		GroupFeedVideoPublishedActive,
		0,
		handlerFunc(func(ctx context.Context, _ applicationeventstream.Event) (applicationeventstream.Outcome, error) {
			close(started)
			<-ctx.Done()
			return applicationeventstream.OutcomeRetryable, ctx.Err()
		}),
		publisher,
	)
	consumer.rebalance = rebalance
	done := make(chan struct{})
	var eligible []brokerRecord
	var processErr error
	go func() {
		eligible, processErr = consumer.processBatch(
			context.Background(),
			[]brokerRecord{videoRecord(t, 0, 15)},
		)
		close(done)
	}()
	<-started
	rebalance <- struct{}{}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("recovery batch did not drain after rebalance")
	}
	if !errors.Is(processErr, ErrRebalanceDrain) ||
		len(eligible) != 0 || len(source.commits) != 0 ||
		len(publisher.records) != 0 {
		t.Fatalf(
			"error=%v eligible=%d commits=%d publications=%d",
			processErr,
			len(eligible),
			len(source.commits),
			len(publisher.records),
		)
	}
}

func TestRebalanceRestartDelayIsBounded(t *testing.T) {
	for range 100 {
		delay := rebalanceRestartDelay()
		if delay < time.Second || delay >= 3*time.Second {
			t.Fatalf("delay = %s", delay)
		}
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

func TestCommitAuthorizationFailureRemainsFatal(t *testing.T) {
	err := errors.Join(ErrCommitUncertain, kerr.GroupAuthorizationFailed)
	if RetryableConsumerError(err) {
		t.Fatalf("commit authorization failure was retryable: %v", err)
	}

}

func TestOffsetOutOfRangeIsFatal(t *testing.T) {
	if RetryableConsumerError(kerr.OffsetOutOfRange) {
		t.Fatal("offset out of range was retryable")
	}

}

func TestConsumerDataLossIsFatal(t *testing.T) {
	if RetryableConsumerError(ErrConsumerDataLoss) {
		t.Fatal("consumer data loss was retryable")
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

func TestConsumerAssignmentReadinessRequiresNonEmptyPartitions(t *testing.T) {
	consumer := testConsumer(&fakeConsumerSource{}, handlerFunc(func(
		context.Context,
		applicationeventstream.Event,
	) (applicationeventstream.Outcome, error) {
		return applicationeventstream.OutcomeDurableSuccess, nil
	}))
	consumer.assignment = newAssignmentReadiness()
	consumer.assignment.assigned(nil)
	consumer.assignment.assigned(map[string][]int32{"topic": nil})
	select {
	case <-consumer.AssignmentReady():
		t.Fatal("empty assignment marked consumer ready")
	default:
	}
	consumer.assignment.assigned(map[string][]int32{"topic": {0}})
	select {
	case <-consumer.AssignmentReady():
	case <-time.After(time.Second):
		t.Fatal("non-empty assignment did not mark consumer ready")
	}
}

func TestSupervisorReportsStartedOnlyAfterAssignment(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	observer := &sessionObserver{}
	consumer := testConsumer(&fakeConsumerSource{}, handlerFunc(func(
		context.Context,
		applicationeventstream.Event,
	) (applicationeventstream.Outcome, error) {
		return applicationeventstream.OutcomeDurableSuccess, nil
	}))
	consumer.assignment = newAssignmentReadiness()
	ready := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- (Supervisor{
			Group: GroupBackboneProbeActive, Observer: observer, Ready: ready,
			NewConsumer: func(context.Context) (*Consumer, error) {
				return consumer, nil
			},
		}).Run(ctx)
	}()
	time.Sleep(10 * time.Millisecond)
	observer.mu.Lock()
	if len(observer.results) != 0 {
		t.Fatalf("session healthy before assignment: %v", observer.results)
	}
	observer.mu.Unlock()
	consumer.assignment.assigned(map[string][]int32{"topic": {0}})
	select {
	case <-ready:
	case <-time.After(time.Second):
		t.Fatal("supervisor did not report readiness")
	}
	observer.mu.Lock()
	if len(observer.results) != 1 || observer.results[0] != "started" {
		t.Fatalf("session results=%v", observer.results)
	}
	observer.mu.Unlock()
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestSupervisorReportsFailuresAndStopsOnNonRetryableInitialization(t *testing.T) {
	observer := &sessionObserver{}
	var attempts atomic.Int32
	supervisor := Supervisor{
		Group: GroupPersistActionActive, Observer: observer,
		MinBackoff: time.Millisecond, MaxBackoff: time.Millisecond,
		NewConsumer: func(context.Context) (*Consumer, error) {
			attempts.Add(1)
			return nil, fmt.Errorf("%w: handler mismatch", ErrConsumerConfiguration)
		},
	}
	err := supervisor.Run(context.Background())
	if !errors.Is(err, ErrConsumerConfiguration) || attempts.Load() != 1 {
		t.Fatalf("error=%v attempts=%d", err, attempts.Load())
	}
	if len(observer.results) != 1 || observer.results[0] != "fatal_failure" {
		t.Fatalf("results=%v", observer.results)
	}
}

func TestSupervisorFatalRuntimeBeforeAssignmentIsNotHealthy(t *testing.T) {
	observer := &sessionObserver{}
	supervisor := Supervisor{
		Group: GroupPersistActionActive, Observer: observer,
		NewConsumer: func(context.Context) (*Consumer, error) {
			consumer := testConsumer(
				&fakeConsumerSource{pollErrors: []error{kerr.GroupAuthorizationFailed}},
				handlerFunc(func(
					context.Context,
					applicationeventstream.Event,
				) (applicationeventstream.Outcome, error) {
					return applicationeventstream.OutcomeDurableSuccess, nil
				}),
			)
			consumer.assignment = newAssignmentReadiness()
			return consumer, nil
		},
	}
	err := supervisor.Run(context.Background())
	if !errors.Is(err, kerr.GroupAuthorizationFailed) {
		t.Fatalf("error=%v", err)
	}
	if len(observer.results) != 1 || observer.results[0] != "fatal_failure" {
		t.Fatalf("results=%v", observer.results)
	}
}

func TestConsumerErrorClassificationRejectsAuthorizationFailures(t *testing.T) {
	for _, err := range []error{
		kerr.SaslAuthenticationFailed,
		kerr.TopicAuthorizationFailed,
		kerr.GroupAuthorizationFailed,
	} {
		if RetryableConsumerError(fmt.Errorf("%w: poll: %w", ErrConsumerSession, err)) {
			t.Fatalf("authorization error was retryable: %v", err)
		}
	}
	if !RetryableConsumerError(fmt.Errorf("%w: poll: %w", ErrConsumerSession, kerr.BrokerNotAvailable)) {
		t.Fatal("broker outage was classified as fatal")
	}
	if !RetryableConsumerError(fmt.Errorf(
		"initialize consumer offsets: %w",
		ErrRetryOffsetInitialization,
	)) {
		t.Fatal("temporary offset initialization failure was classified as fatal")
	}
}

func TestSupervisorHonorsRequestedRetryDelay(t *testing.T) {
	delay := consumerRetryDelay(
		fmt.Errorf("%w: %w", ErrConsumerSession, requestedDelayError{delay: time.Second}),
		100*time.Millisecond,
		200*time.Millisecond,
	)
	if delay != time.Second {
		t.Fatalf("delay=%s", delay)
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
	assignment := newAssignmentReadiness()
	assignment.assigned(map[string][]int32{"topic": {0}})
	return &Consumer{
		source: source, topicID: TopicBackboneProbe,
		consumeTopicID: TopicBackboneProbe, stage: ConsumerStageSource,
		topicName: "frux.platform.backbone_probe.v1",
		groupID:   GroupBackboneProbeActive,
		groupName: "frux.platform.backbone_probe.active.v1",
		handler:   handler, maxPollRecords: 100, concurrency: 2,
		drainTimeout: 50 * time.Millisecond, commitTimeout: time.Second,
		closeTimeout:   time.Second,
		assignment:     assignment,
		delayedChanged: make(chan struct{}, 1),
	}
}

func testRecoveryConsumer(
	t *testing.T,
	source consumerSource,
	group ConsumerGroupID,
	tier int,
	handler applicationeventstream.Handler,
	publisher recoveryPublisher,
) *Consumer {
	t.Helper()
	recovery, err := Recovery(group)
	if err != nil {
		t.Fatal(err)
	}
	recovery.LocalRetry = LocalRetrySpec{
		MaxAttempts: 3, InitialDelay: time.Millisecond,
		MaxDelay: time.Millisecond, MaxTotalDelay: 2 * time.Millisecond,
	}
	consumeTopic := recovery.SourceTopic
	stage, err := RecoveryConsumerStage(group, tier)
	if err != nil {
		t.Fatal(err)
	}
	if tier > 0 {
		registered, ok := recovery.RetryTier(tier)
		if !ok {
			t.Fatalf("unknown tier %d", tier)
		}
		consumeTopic = registered.Topic
	}
	topicName, err := TopicName("", consumeTopic)
	if err != nil {
		t.Fatal(err)
	}
	groupName, err := GroupName("", group)
	if err != nil {
		t.Fatal(err)
	}
	if tier > 0 {
		groupName, err = RecoveryConsumerGroupName("", group, tier)
		if err != nil {
			t.Fatal(err)
		}
	}
	assignment := newAssignmentReadiness()
	assignment.assigned(map[string][]int32{topicName: {0}})
	lifecycle := &consumerPartitionLifecycle{}
	consumer := &Consumer{
		source: source, topicID: recovery.SourceTopic, consumeTopicID: consumeTopic,
		topicName: topicName, groupID: group, groupName: groupName, stage: stage,
		handler: handler, recovery: &recovery, recoveryTier: tier,
		recoveryWriter: publisher, maxPollRecords: 100, concurrency: 2,
		drainTimeout: 50 * time.Millisecond, commitTimeout: time.Second,
		closeTimeout: time.Second, assignment: assignment, partitions: lifecycle,
		now:            time.Now,
		delayedChanged: make(chan struct{}, 1),
	}
	lifecycle.bind(consumer)
	lifecycle.assigned(map[string][]int32{
		topicName: {0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12},
	})
	return consumer
}

func videoRecord(t *testing.T, partition int32, offset int64) brokerRecord {
	t.Helper()
	key, value := recoveryBusinessRecord(t)
	topic, err := TopicName("", TopicVideoPublished)
	if err != nil {
		t.Fatal(err)
	}
	return brokerRecord{
		Topic: topic, Partition: partition, Offset: offset,
		Timestamp: time.Now().UTC(), Key: key, Value: value,
	}
}

func retryTierRecord(t *testing.T, tier int, notBefore time.Time) brokerRecord {
	t.Helper()
	key, value := recoveryBusinessRecord(t)
	recovery, err := Recovery(GroupFeedVideoPublishedActive)
	if err != nil {
		t.Fatal(err)
	}
	registered, ok := recovery.RetryTier(tier)
	if !ok {
		t.Fatalf("unknown retry tier %d", tier)
	}
	source, err := TopicName("", TopicVideoPublished)
	if err != nil {
		t.Fatal(err)
	}
	latestFailure := time.Now().UTC().Add(-time.Second)
	if notBefore.Before(latestFailure) {
		latestFailure = notBefore
	}
	metadata := RecoveryMetadata{
		SourceTopic: source, SourcePartition: 2, SourceOffset: 11,
		EventID: "event-video-42", SchemaVersion: 1,
		ConsumerGroup: GroupFeedVideoPublishedActive,
		Attempt:       tier, Tier: tier, FailureClass: FailureLocalRetryExhausted,
		FirstFailureAt:  latestFailure.Add(-time.Second),
		LatestFailureAt: latestFailure, NotBefore: notBefore,
		PayloadSHA256: PayloadSHA256(value),
	}
	headers, err := EncodeRecoveryHeaders("", registered.Topic, metadata, key, value)
	if err != nil {
		t.Fatal(err)
	}
	topic, err := TopicName("", registered.Topic)
	if err != nil {
		t.Fatal(err)
	}
	return brokerRecord{
		Topic: topic, Partition: 0, Offset: int64(tier),
		Timestamp: time.Now().UTC(), Key: key, Value: value, Headers: headers,
	}
}

func mediaRecord(t *testing.T, partition int32, offset int64) brokerRecord {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Millisecond)
	key, err := EncodeKey(KeyKindAssetID, AssetKey{AssetID: 91})
	if err != nil {
		t.Fatal(err)
	}
	value, err := EncodeEvent(
		TopicMediaProcessingRequested,
		key,
		EventMetadata{
			EventID: "event-media-91", Type: EventTypeMediaProcessingRequested,
			SchemaVersion: 1, OccurredAt: now, ProducedAt: now,
			Producer: ProducerMediaAPI,
		},
		MediaProcessingRequestedPayload{
			EventID: "event-media-91", AssetID: 91,
			ProfileVersion: "v1", OccurredAt: now,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	topic, err := TopicName("", TopicMediaProcessingRequested)
	if err != nil {
		t.Fatal(err)
	}
	return brokerRecord{
		Topic: topic, Partition: partition, Offset: offset,
		Timestamp: now, Key: key, Value: value,
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

func TestSourceConsumersIgnoreSiblingLegacyReplayHeaders(t *testing.T) {
	key, value := recoveryBusinessRecord(t)
	now := time.Now().UTC().Truncate(time.Millisecond)
	sourceTopic, err := TopicName("dev", TopicVideoPublished)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name    string
		owner   ConsumerGroupID
		sibling ConsumerGroupID
	}{
		{
			name:  "feed replay does not block embedding",
			owner: GroupFeedVideoPublishedActive, sibling: GroupEmbeddingVideoPublishedActive,
		},
		{
			name:  "embedding replay does not block feed",
			owner: GroupEmbeddingVideoPublishedActive, sibling: GroupFeedVideoPublishedActive,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			metadata := RecoveryMetadata{
				SourceTopic: sourceTopic, SourcePartition: 2, SourceOffset: 91,
				EventID: "event-video-42", SchemaVersion: 1,
				ConsumerGroup: test.owner,
				Attempt:       1, Tier: 0, FailureClass: FailureTerminalDomain,
				FirstFailureAt: now, LatestFailureAt: now, NotBefore: now,
				PayloadSHA256: PayloadSHA256(value),
				ReplayID:      "replay-0123456789abcdef0123456789abcdef",
			}
			headers, err := EncodeRecoveryHeaders(
				"dev", TopicVideoPublished, metadata, key, value,
			)
			if err != nil {
				t.Fatal(err)
			}
			recovery, err := Recovery(test.sibling)
			if err != nil {
				t.Fatal(err)
			}
			consumer := &Consumer{
				topicID: TopicVideoPublished, consumeTopicID: TopicVideoPublished,
				topicPrefix: "dev", groupID: test.sibling,
				recovery: &recovery, recoveryWriter: &fakeRecoveryPublisher{},
				handler: handlerFunc(func(context.Context, applicationeventstream.Event) (applicationeventstream.Outcome, error) {
					return applicationeventstream.OutcomeDurableSuccess, nil
				}),
				now: time.Now,
			}
			record := brokerRecord{
				Topic: sourceTopic, Partition: 4, Offset: 101,
				Key: key, Value: value, Headers: headers,
			}
			result := consumer.processPartition(
				context.Background(), []brokerRecord{record},
			)
			if result.err != nil || result.eligible == nil {
				t.Fatalf("result=%+v", result)
			}
		})
	}
}
