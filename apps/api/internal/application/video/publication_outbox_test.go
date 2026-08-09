package applicationvideo

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type publicationStoreStub struct {
	items      []*PublicationOutboxItem
	ensured    int
	dispatched int
	failed     int
	stats      PublicationOutboxStats
	statsErr   error
	batches    [][]*PublicationOutboxItem
	claimCalls int
	reconciled int
	cleanup    int64
	cleanupErr error
	cleanupAt  time.Time
	cleanupMax int
}

func (s *publicationStoreStub) EnsurePublicationEvent(
	context.Context, *PublishedEvent, time.Time,
) error {
	s.ensured++
	return nil
}
func (s *publicationStoreStub) ClaimPublicationEvents(
	context.Context, string, int, time.Time, time.Time,
) ([]*PublicationOutboxItem, error) {
	s.claimCalls++
	if len(s.batches) > 0 {
		items := s.batches[0]
		s.batches = s.batches[1:]
		return items, nil
	}
	items := s.items
	s.items = nil
	return items, nil
}
func (s *publicationStoreStub) MarkPublicationEventDispatched(
	context.Context, string, string, time.Time,
) error {
	s.dispatched++
	return nil
}
func (s *publicationStoreStub) MarkPublicationEventFailed(
	context.Context, string, string, time.Time, string,
) error {
	s.failed++
	return nil
}
func (s *publicationStoreStub) PublicationOutboxStats(
	context.Context, time.Time,
) (PublicationOutboxStats, error) {
	return s.stats, s.statsErr
}

type publicationObserverStub struct {
	pending        int64
	oldest         *time.Time
	statsErr       error
	results        []string
	cleanupResult  string
	cleanupDeleted int64
}

func (o *publicationObserverStub) ObservePublicationOutbox(
	pending int64,
	oldest *time.Time,
	err error,
) {
	o.pending = pending
	o.oldest = oldest
	o.statsErr = err
}

func (o *publicationObserverStub) ObservePublicationDispatch(result string) {
	o.results = append(o.results, result)
}
func (o *publicationObserverStub) ObservePublicationCleanup(result string, deleted int64) {
	o.cleanupResult = result
	o.cleanupDeleted = deleted
}
func (s *publicationStoreStub) ReconcilePublicationEvents(
	context.Context, int, time.Time,
) (int, error) {
	s.reconciled++
	return 0, nil
}
func (s *publicationStoreStub) CleanupPublicationEvents(
	_ context.Context, before time.Time, limit int,
) (int64, error) {
	s.cleanupAt = before
	s.cleanupMax = limit
	return s.cleanup, s.cleanupErr
}

type publicationPublisherStub struct {
	err     error
	calls   int
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (p *publicationPublisherStub) PublishVideoPublished(
	ctx context.Context, _ *PublishedEvent,
) error {
	p.calls++
	if p.started != nil {
		p.once.Do(func() { close(p.started) })
	}
	if p.release != nil {
		select {
		case <-p.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return p.err
}

func TestDurablePublicationBoundaryAndDispatcher(t *testing.T) {
	now := time.Now().UTC()
	event := &PublishedEvent{
		EventID: "video-published:1:1", VideoID: 1, AuthorID: 2,
		PublishedAt: now, OccurredAt: now,
	}

	store := &publicationStoreStub{items: []*PublicationOutboxItem{{
		Event: event, Attempts: 1,
	}}}
	durable := NewDurablePublicationPublisher(store)
	if err := durable.PublishVideoPublished(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if store.ensured != 1 {
		t.Fatalf("ensured = %d", store.ensured)
	}
	publisher := &publicationPublisherStub{}
	dispatcher := NewPublicationOutboxDispatcher(store, publisher, nil)
	dispatcher.now = func() time.Time { return now }
	if processed, err := dispatcher.RunOnce(context.Background()); err != nil || processed != 1 {
		t.Fatalf("processed=%d err=%v", processed, err)
	}
	if publisher.calls != 1 || store.dispatched != 1 {
		t.Fatalf("calls=%d dispatched=%d", publisher.calls, store.dispatched)
	}

	store.items = []*PublicationOutboxItem{{Event: event, Attempts: 2}}
	publisher.err = errors.New("kafka unavailable")
	if _, err := dispatcher.RunOnce(context.Background()); err == nil {
		t.Fatal("transport failure was hidden")
	}
	if store.failed != 1 {
		t.Fatalf("failed marks = %d", store.failed)
	}
	store.items = []*PublicationOutboxItem{{Event: event, Attempts: 3}}
	publisher.err = nil
	if processed, err := dispatcher.RunOnce(context.Background()); err != nil || processed != 1 {
		t.Fatalf("recovery processed=%d err=%v", processed, err)
	}
	if store.dispatched != 2 {
		t.Fatalf("recovery dispatched = %d", store.dispatched)
	}
}

func TestPublicationOutboxStatsRemainObservableDuringTransportFailure(t *testing.T) {
	now := time.Now().UTC()
	oldest := now.Add(-10 * time.Minute)
	event := &PublishedEvent{
		EventID: "video-published:2:1", VideoID: 2, AuthorID: 3,
		PublishedAt: now, OccurredAt: now,
	}
	store := &publicationStoreStub{
		items: []*PublicationOutboxItem{{Event: event, Attempts: 1}},
		stats: PublicationOutboxStats{Pending: 7, OldestPending: &oldest},
	}
	observer := &publicationObserverStub{}
	dispatcher := NewPublicationOutboxDispatcher(
		store,
		&publicationPublisherStub{err: errors.New("transport unavailable")},
		observer,
	)
	dispatcher.now = func() time.Time { return now }
	if _, err := dispatcher.RunOnce(context.Background()); err == nil {
		t.Fatal("transport failure was hidden")
	}
	if observer.statsErr != nil || observer.pending != 7 ||
		observer.oldest == nil || !observer.oldest.Equal(oldest) {
		t.Fatalf(
			"stats pending=%d oldest=%v err=%v",
			observer.pending, observer.oldest, observer.statsErr,
		)
	}
	if len(observer.results) != 1 || observer.results[0] != "transport" {
		t.Fatalf("dispatch results = %v", observer.results)
	}
}

func TestPublicationDispatcherStartDoesNotWaitForTransport(t *testing.T) {
	for _, transport := range []string{"kafka", "rabbit"} {
		t.Run(transport, func(t *testing.T) {
			now := time.Now().UTC()
			event := &PublishedEvent{
				EventID: "video-published:3:1", VideoID: 3, AuthorID: 4,
				PublishedAt: now, OccurredAt: now,
			}
			store := &publicationStoreStub{items: []*PublicationOutboxItem{{Event: event}}}
			publisher := &publicationPublisherStub{
				err:     errors.New(transport + " unavailable"),
				started: make(chan struct{}), release: make(chan struct{}),
			}
			dispatcher := NewPublicationOutboxDispatcher(store, publisher, nil)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			startedAt := time.Now()
			if err := dispatcher.Start(ctx); err != nil {
				t.Fatalf("start failed during outage: %v", err)
			}
			if time.Since(startedAt) > 100*time.Millisecond {
				t.Fatal("startup synchronously waited for publication transport")
			}
			unrelatedStarted := false
			unrelatedStarted = true
			if !unrelatedStarted {
				t.Fatal("unrelated worker did not start")
			}
			select {
			case <-publisher.started:
			case <-time.After(time.Second):
				t.Fatal("asynchronous initial dispatch did not run")
			}
			close(publisher.release)
		})
	}
}

func TestPublicationDispatcherRunIsAggregateBounded(t *testing.T) {
	now := time.Now().UTC()
	makeBatch := func(offset int64) []*PublicationOutboxItem {
		items := make([]*PublicationOutboxItem, 0, 2)
		for index := int64(0); index < 2; index++ {
			items = append(items, &PublicationOutboxItem{Event: &PublishedEvent{
				EventID: "video-published:" + time.Unix(offset+index, 0).UTC().Format("150405"),
				VideoID: offset + index + 1, AuthorID: 1,
				PublishedAt: now, OccurredAt: now,
			}})
		}
		return items
	}
	store := &publicationStoreStub{batches: [][]*PublicationOutboxItem{
		makeBatch(10), makeBatch(20), makeBatch(30), makeBatch(40),
	}}
	dispatcher := NewPublicationOutboxDispatcher(store, &publicationPublisherStub{}, nil)
	dispatcher.batchSize = 2
	dispatcher.maxBatches = 3
	dispatcher.now = func() time.Time { return now }
	processed, err := dispatcher.RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if processed != 6 || store.claimCalls != 3 || len(store.batches) != 1 {
		t.Fatalf(
			"processed=%d claim_calls=%d remaining_batches=%d",
			processed, store.claimCalls, len(store.batches),
		)
	}
}

func TestPublicationDispatcherRunHasAggregateDeadline(t *testing.T) {
	now := time.Now().UTC()
	store := &publicationStoreStub{
		items: []*PublicationOutboxItem{
			{Event: &PublishedEvent{
				EventID: "video-published:deadline:1", VideoID: 50, AuthorID: 1,
				PublishedAt: now, OccurredAt: now,
			}},
		},
	}
	dispatcher := NewPublicationOutboxDispatcher(
		store,
		&publicationPublisherStub{release: make(chan struct{})},
		nil,
	)
	dispatcher.runTimeout = 20 * time.Millisecond
	started := time.Now()
	if _, err := dispatcher.RunOnce(context.Background()); !errors.Is(
		err, context.DeadlineExceeded,
	) {
		t.Fatalf("run error = %v", err)
	}
	if elapsed := time.Since(started); elapsed > 200*time.Millisecond {
		t.Fatalf("aggregate run exceeded bound: %v", elapsed)
	}
}

func TestPublicationDispatcherRunsBoundedCleanup(t *testing.T) {
	now := time.Now().UTC()
	store := &publicationStoreStub{cleanup: 7}
	observer := &publicationObserverStub{}
	dispatcher := NewPublicationOutboxDispatcher(
		store, &publicationPublisherStub{}, observer,
	)
	dispatcher.now = func() time.Time { return now }
	if _, err := dispatcher.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !store.cleanupAt.Equal(now.Add(-30*24*time.Hour)) || store.cleanupMax != 100 {
		t.Fatalf("cleanup cutoff=%v limit=%d", store.cleanupAt, store.cleanupMax)
	}
	if observer.cleanupResult != "success" || observer.cleanupDeleted != 7 {
		t.Fatalf(
			"cleanup result=%s deleted=%d",
			observer.cleanupResult, observer.cleanupDeleted,
		)
	}
}

func TestPublicationDispatcherRejectsInvalidConstruction(t *testing.T) {
	if err := NewPublicationOutboxDispatcher(nil, nil, nil).Start(context.Background()); !errors.Is(
		err, ErrPublicationOutboxInvalidConfiguration,
	) {
		t.Fatalf("invalid start error = %v", err)
	}
}
