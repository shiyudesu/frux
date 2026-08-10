package applicationvideo

import (
	"context"
	"errors"
	"os"
	"strings"
	"time"
)

const publicationOutboxEventType = "video_published.v1"

var ErrPublicationOutboxLeaseLost = errors.New("video publication outbox lease lost")
var ErrPublicationOutboxInvalidConfiguration = errors.New("invalid video publication outbox configuration")

type PublicationOutboxItem struct {
	Event       *PublishedEvent
	Attempts    int
	AvailableAt time.Time
	LeaseOwner  string
	LeaseUntil  *time.Time
	CreatedAt   time.Time
}

type PublicationOutboxStats struct {
	Pending       int64
	OldestPending *time.Time
}

type PublicationEventStore interface {
	EnsurePublicationEvent(ctx context.Context, event *PublishedEvent, readyAt time.Time) error
	ClaimPublicationEvents(
		ctx context.Context,
		leaseOwner string,
		limit int,
		now time.Time,
		leaseUntil time.Time,
	) ([]*PublicationOutboxItem, error)
	MarkPublicationEventDispatched(
		ctx context.Context,
		eventID string,
		leaseOwner string,
		dispatchedAt time.Time,
	) error
	MarkPublicationEventFailed(
		ctx context.Context,
		eventID string,
		leaseOwner string,
		availableAt time.Time,
		errorClass string,
	) error
	PublicationOutboxStats(ctx context.Context, now time.Time) (PublicationOutboxStats, error)
	ReconcilePublicationEvents(ctx context.Context, limit int, now time.Time) (int, error)
	CleanupPublicationEvents(ctx context.Context, dispatchedBefore time.Time, limit int) (int64, error)
}

type DurablePublicationPublisher struct {
	store PublicationEventStore
	now   func() time.Time
}

func NewDurablePublicationPublisher(store PublicationEventStore) *DurablePublicationPublisher {
	return &DurablePublicationPublisher{
		store: store,
		now:   func() time.Time { return time.Now().UTC() },
	}
}

func (p *DurablePublicationPublisher) PublishVideoPublished(
	ctx context.Context,
	event *PublishedEvent,
) error {
	if p == nil || p.store == nil || event == nil {
		return nil
	}
	return p.store.EnsurePublicationEvent(ctx, event, p.now().UTC())
}

type PublicationOutboxObserver interface {
	ObservePublicationOutbox(pending int64, oldest *time.Time, err error)
	ObservePublicationStats(result string)
	ObservePublicationDispatch(result string)
	ObservePublicationCleanup(result string, deleted int64)
}

type PublicationOutboxDispatcher struct {
	store          PublicationEventStore
	publisher      PublishedEventPublisher
	observer       PublicationOutboxObserver
	owner          string
	batchSize      int
	maxBatches     int
	cleanupBatch   int
	leaseTTL       time.Duration
	pollInterval   time.Duration
	runTimeout     time.Duration
	durableTimeout time.Duration
	statsTimeout   time.Duration
	replayWindow   time.Duration
	now            func() time.Time
}

func NewPublicationOutboxDispatcher(
	store PublicationEventStore,
	publisher PublishedEventPublisher,
	observer PublicationOutboxObserver,
) *PublicationOutboxDispatcher {
	owner, _ := os.Hostname()
	if strings.TrimSpace(owner) == "" {
		owner = "frux-worker"
	}
	return &PublicationOutboxDispatcher{
		store: store, publisher: publisher, observer: observer,
		owner: "video-publication:" + owner, batchSize: 100,
		maxBatches: 5, cleanupBatch: 100,
		leaseTTL: 30 * time.Second, pollInterval: time.Second,
		runTimeout: 10 * time.Second, replayWindow: 30 * 24 * time.Hour,
		durableTimeout: 2 * time.Second, statsTimeout: 2 * time.Second,
		now: func() time.Time { return time.Now().UTC() },
	}
}

func (d *PublicationOutboxDispatcher) Start(ctx context.Context) error {
	if d == nil || d.store == nil || d.publisher == nil ||
		d.batchSize <= 0 || d.maxBatches <= 0 || d.cleanupBatch <= 0 ||
		d.leaseTTL <= 0 || d.pollInterval <= 0 || d.runTimeout <= 0 ||
		d.durableTimeout <= 0 || d.statsTimeout <= 0 || d.replayWindow <= 0 {
		return ErrPublicationOutboxInvalidConfiguration
	}
	go func() {
		_, _ = d.RunOnce(ctx)
		ticker := time.NewTicker(d.pollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_, _ = d.RunOnce(ctx)
			}
		}
	}()
	return nil
}

func (d *PublicationOutboxDispatcher) RunOnce(ctx context.Context) (int, error) {
	if d == nil || d.store == nil || d.publisher == nil {
		return 0, nil
	}
	runCtx, cancel := context.WithTimeout(ctx, d.runTimeout)
	defer cancel()
	durableCtx, durableCancel := context.WithTimeout(
		context.WithoutCancel(ctx), d.runTimeout+d.durableTimeout,
	)
	defer durableCancel()
	now := d.now().UTC()
	_, reconcileErr := d.store.ReconcilePublicationEvents(runCtx, d.batchSize, now)
	processed := 0
	var dispatchErr error
	for batch := 0; batch < d.maxBatches && runCtx.Err() == nil; batch++ {
		batchProcessed, claimed, err := d.runBatch(runCtx, durableCtx)
		processed += batchProcessed
		dispatchErr = errors.Join(dispatchErr, err)
		if err != nil || claimed < d.batchSize {
			break
		}
	}
	deleted, cleanupErr := d.store.CleanupPublicationEvents(
		runCtx, now.Add(-d.replayWindow), d.cleanupBatch,
	)
	d.observeCleanup(cleanupErr, deleted)
	combined := errors.Join(reconcileErr, dispatchErr, cleanupErr)
	d.observeStats(ctx)
	return processed, combined
}

func (d *PublicationOutboxDispatcher) runBatch(
	ctx context.Context,
	durableCtx context.Context,
) (int, int, error) {
	now := d.now().UTC()
	items, err := d.store.ClaimPublicationEvents(
		ctx, d.owner, d.batchSize, now, now.Add(d.leaseTTL),
	)
	if err != nil {
		return 0, 0, err
	}
	processed := 0
	var dispatchErr error
	for _, item := range items {
		if item == nil || item.Event == nil {
			continue
		}
		err := d.publisher.PublishVideoPublished(ctx, item.Event)
		if err == nil {
			err = d.store.MarkPublicationEventDispatched(
				durableCtx, item.Event.EventID, d.owner, d.now().UTC(),
			)
		}
		if err == nil {
			processed++
			d.observeDispatch("success")
			continue
		}
		class := publicationErrorClass(err)
		retryAt := d.now().UTC().Add(publicationRetryDelay(item.Attempts))
		markErr := d.store.MarkPublicationEventFailed(
			durableCtx, item.Event.EventID, d.owner, retryAt, class,
		)
		dispatchErr = errors.Join(dispatchErr, err, markErr)
		d.observeDispatch(class)
		if ctx.Err() != nil {
			break
		}
	}
	return processed, len(items), dispatchErr
}

func (d *PublicationOutboxDispatcher) observeStats(ctx context.Context) {
	if d == nil || d.observer == nil || d.store == nil {
		return
	}
	statsCtx, cancel := context.WithTimeout(
		context.WithoutCancel(ctx), d.statsTimeout,
	)
	defer cancel()
	stats, err := d.store.PublicationOutboxStats(statsCtx, d.now().UTC())
	d.observer.ObservePublicationOutbox(
		stats.Pending,
		stats.OldestPending,
		err,
	)
	result := "success"
	if err != nil {
		result = "failure"
	}
	d.observer.ObservePublicationStats(result)
}

func (d *PublicationOutboxDispatcher) observeDispatch(result string) {
	if d != nil && d.observer != nil {
		d.observer.ObservePublicationDispatch(result)
	}
}

func (d *PublicationOutboxDispatcher) observeCleanup(err error, deleted int64) {
	if d == nil || d.observer == nil {
		return
	}
	result := "success"
	if err != nil {
		result = "failure"
	}
	d.observer.ObservePublicationCleanup(result, deleted)
}

func publicationRetryDelay(attempts int) time.Duration {
	if attempts < 1 {
		attempts = 1
	}
	delay := time.Second << min(attempts-1, 8)
	if delay > 5*time.Minute {
		return 5 * time.Minute
	}
	return delay
}

func publicationErrorClass(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	return "transport"
}
