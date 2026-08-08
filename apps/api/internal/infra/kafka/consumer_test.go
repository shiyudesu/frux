package infrakafka

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	applicationeventstream "github.com/shiyudesu/frux/internal/application/eventstream"
)

type fakeConsumerSource struct {
	mu        sync.Mutex
	batches   [][]brokerRecord
	commitErr error
	commits   [][]brokerRecord
	allows    int
	closed    bool
}

func (f *fakeConsumerSource) Poll(ctx context.Context, _ int) ([]brokerRecord, error) {
	f.mu.Lock()
	if len(f.batches) > 0 {
		batch := f.batches[0]
		f.batches = f.batches[1:]
		f.mu.Unlock()
		return batch, nil
	}
	f.mu.Unlock()
	<-ctx.Done()
	return nil, ctx.Err()
}

func (f *fakeConsumerSource) Commit(_ context.Context, records []brokerRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.commits = append(f.commits, append([]brokerRecord(nil), records...))
	return f.commitErr
}

func (f *fakeConsumerSource) AllowRebalance() {
	f.mu.Lock()
	f.allows++
	f.mu.Unlock()
}

func (f *fakeConsumerSource) Close() {
	f.mu.Lock()
	f.closed = true
	f.mu.Unlock()
}

type handlerFunc func(context.Context, applicationeventstream.Event) (applicationeventstream.Outcome, error)

func (f handlerFunc) Handle(ctx context.Context, event applicationeventstream.Event) (applicationeventstream.Outcome, error) {
	return f(ctx, event)
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
		consumer := testConsumer(source, handlerFunc(func(ctx context.Context, _ applicationeventstream.Event) (applicationeventstream.Outcome, error) {
			close(started)
			<-ctx.Done()
			return applicationeventstream.OutcomeRetryable, ctx.Err()
		}))
		consumer.drainTimeout = 5 * time.Millisecond
		done := make(chan error, 1)
		go func() { done <- consumer.Run(ctx) }()
		<-started
		cancel()
		select {
		case err := <-done:
			if !errors.Is(err, ErrShutdownDeadline) {
				t.Fatalf("error = %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("consumer exceeded shutdown deadline")
		}
	})
}

func testConsumer(source consumerSource, handler applicationeventstream.Handler) *Consumer {
	return &Consumer{
		source: source, topicID: TopicBackboneProbe,
		topicName: "frux.platform.backbone_probe.v1",
		groupID:   GroupBackboneProbeActive,
		groupName: "frux.platform.backbone_probe.active.v1",
		handler:   handler, maxPollRecords: 100, concurrency: 2,
		drainTimeout: 50 * time.Millisecond, commitTimeout: time.Second,
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
