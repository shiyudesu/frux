package applicationexposure

import (
	"context"
	"errors"
	"time"
)

const (
	defaultOutboxBatchSize      = 50
	defaultOutboxPollInterval   = time.Second
	defaultOutboxLease          = 30 * time.Second
	defaultOutboxPublishTimeout = 5 * time.Second
	maxOutboxRetryDelay         = time.Minute
)

type ViewEventPublisher interface {
	PublishViewEventRecorded(ctx context.Context, event *ViewEventRecordedEvent) error
}

type OutboxItem struct {
	ID       int64
	Attempts int
	Event    *ViewEventRecordedEvent
}

type OutboxStats struct {
	Pending       int64
	OldestPending time.Time
}

type OutboxStore interface {
	ClaimViewEventOutbox(ctx context.Context, limit int, now, leasedUntil time.Time) ([]OutboxItem, error)
	MarkViewEventOutboxDispatched(ctx context.Context, id int64, dispatchedAt time.Time) error
	MarkViewEventOutboxFailed(ctx context.Context, id int64, availableAt time.Time, reason string) error
	ViewEventOutboxStats(ctx context.Context, now time.Time) (OutboxStats, error)
}

type OutboxObserver func(OutboxStats, error)

type OutboxDispatcher struct {
	store        OutboxStore
	publisher    ViewEventPublisher
	now          func() time.Time
	batchSize    int
	pollInterval time.Duration
	lease        time.Duration
	observer     OutboxObserver
}

type OutboxOption func(*OutboxDispatcher)

func NewOutboxDispatcher(store OutboxStore, publisher ViewEventPublisher, options ...OutboxOption) *OutboxDispatcher {
	dispatcher := &OutboxDispatcher{
		store: store, publisher: publisher, now: func() time.Time { return time.Now().UTC() },
		batchSize: defaultOutboxBatchSize, pollInterval: defaultOutboxPollInterval, lease: defaultOutboxLease,
	}
	for _, option := range options {
		option(dispatcher)
	}
	return dispatcher
}

func WithOutboxNow(now func() time.Time) OutboxOption {
	return func(dispatcher *OutboxDispatcher) {
		if now != nil {
			dispatcher.now = now
		}
	}
}

func WithOutboxObserver(observer OutboxObserver) OutboxOption {
	return func(dispatcher *OutboxDispatcher) {
		dispatcher.observer = observer
	}
}

func WithOutboxPollInterval(interval time.Duration) OutboxOption {
	return func(dispatcher *OutboxDispatcher) {
		if interval > 0 {
			dispatcher.pollInterval = interval
		}
	}
}

func (d *OutboxDispatcher) Start(ctx context.Context) error {
	if d == nil || d.store == nil || d.publisher == nil {
		return nil
	}
	if _, err := d.DispatchOnce(ctx); err != nil {
		d.observe(ctx, err)
	}
	go func() {
		ticker := time.NewTicker(d.pollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_, err := d.DispatchOnce(ctx)
				d.observe(ctx, err)
			}
		}
	}()
	return nil
}

func (d *OutboxDispatcher) DispatchOnce(ctx context.Context) (int, error) {
	now := d.now().UTC()
	items, err := d.store.ClaimViewEventOutbox(ctx, d.batchSize, now, now.Add(d.lease))
	if err != nil {
		return 0, err
	}
	dispatched := 0
	var dispatchErr error
	for _, item := range items {
		publishCtx, cancel := context.WithTimeout(ctx, defaultOutboxPublishTimeout)
		err := d.publisher.PublishViewEventRecorded(publishCtx, item.Event)
		cancel()
		if err != nil {
			next := now.Add(outboxRetryDelay(item.Attempts))
			if markErr := d.store.MarkViewEventOutboxFailed(ctx, item.ID, next, err.Error()); markErr != nil {
				return dispatched, markErr
			}
			dispatchErr = errors.Join(dispatchErr, err)
			continue
		}
		if err := d.store.MarkViewEventOutboxDispatched(ctx, item.ID, d.now().UTC()); err != nil {
			return dispatched, err
		}
		dispatched++
	}
	return dispatched, dispatchErr
}

func (d *OutboxDispatcher) observe(ctx context.Context, dispatchErr error) {
	if d.observer == nil {
		return
	}
	stats, err := d.store.ViewEventOutboxStats(ctx, d.now().UTC())
	if err != nil {
		d.observer(OutboxStats{}, err)
		return
	}
	d.observer(stats, dispatchErr)
}

func outboxRetryDelay(attempts int) time.Duration {
	if attempts < 1 {
		attempts = 1
	}
	delay := time.Second << min(attempts-1, 6)
	if delay > maxOutboxRetryDelay {
		return maxOutboxRetryDelay
	}
	return delay
}
