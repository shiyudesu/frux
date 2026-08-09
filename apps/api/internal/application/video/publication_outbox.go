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
	ObservePublicationDispatch(result string)
}

type PublicationOutboxDispatcher struct {
	store        PublicationEventStore
	publisher    PublishedEventPublisher
	observer     PublicationOutboxObserver
	owner        string
	batchSize    int
	leaseTTL     time.Duration
	pollInterval time.Duration
	now          func() time.Time
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
		leaseTTL: 30 * time.Second, pollInterval: time.Second,
		now: func() time.Time { return time.Now().UTC() },
	}
}

func (d *PublicationOutboxDispatcher) Start(ctx context.Context) error {
	if d == nil || d.store == nil || d.publisher == nil {
		return nil
	}
	if _, err := d.RunOnce(ctx); err != nil {
		return err
	}
	go func() {
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
	now := d.now().UTC()
	_, reconcileErr := d.store.ReconcilePublicationEvents(ctx, d.batchSize, now)
	items, err := d.store.ClaimPublicationEvents(
		ctx, d.owner, d.batchSize, now, now.Add(d.leaseTTL),
	)
	if err != nil {
		d.observeStats(ctx)
		return 0, errors.Join(reconcileErr, err)
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
				ctx, item.Event.EventID, d.owner, d.now().UTC(),
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
			ctx, item.Event.EventID, d.owner, retryAt, class,
		)
		dispatchErr = errors.Join(dispatchErr, err, markErr)
		d.observeDispatch(class)
	}
	combined := errors.Join(reconcileErr, dispatchErr)
	d.observeStats(ctx)
	return processed, combined
}

func (d *PublicationOutboxDispatcher) observeStats(ctx context.Context) {
	if d == nil || d.observer == nil || d.store == nil {
		return
	}
	stats, err := d.store.PublicationOutboxStats(ctx, d.now().UTC())
	d.observer.ObservePublicationOutbox(
		stats.Pending,
		stats.OldestPending,
		err,
	)
}

func (d *PublicationOutboxDispatcher) observeDispatch(result string) {
	if d != nil && d.observer != nil {
		d.observer.ObservePublicationDispatch(result)
	}
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
