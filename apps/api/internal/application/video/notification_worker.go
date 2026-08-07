package applicationvideo

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	applicationmessage "github.com/shiyudesu/frux/internal/application/message"
	domainmessage "github.com/shiyudesu/frux/internal/domain/message"
)

const (
	defaultLifecycleNotificationBatchSize = 50
	defaultLifecycleNotificationPoll      = time.Second
	defaultLifecycleNotificationLease     = 30 * time.Second
	defaultLifecycleNotificationTimeout   = 5 * time.Second
	maxLifecycleNotificationRetry         = time.Minute
)

var ErrLifecycleNotificationNotReady = errors.New("video lifecycle notification target is not publicly ready")

type LifecycleNotificationRepository interface {
	ClaimLifecycleNotifications(ctx context.Context, leaseOwner string, limit int, now, leaseUntil time.Time) ([]*domainmessage.LifecycleOutboxItem, error)
	MarkLifecycleNotificationDelivered(ctx context.Context, eventID, leaseOwner string, deliveredAt time.Time) error
	MarkLifecycleNotificationFailed(ctx context.Context, eventID, leaseOwner string, availableAt time.Time, reason string, terminal bool) error
}

type LifecycleNotificationWriter interface {
	WriteLifecycleNotification(ctx context.Context, notification domainmessage.LifecycleNotification, title, content string) error
}

type LifecycleNotificationObserver interface {
	ObserveLifecycleNotification(result string)
}

type LifecycleNotificationWorker struct {
	store        LifecycleNotificationRepository
	writer       LifecycleNotificationWriter
	observer     LifecycleNotificationObserver
	owner        string
	now          func() time.Time
	batchSize    int
	pollInterval time.Duration
	lease        time.Duration
}

func NewLifecycleNotificationWorker(
	store LifecycleNotificationRepository,
	writer LifecycleNotificationWriter,
	observer LifecycleNotificationObserver,
) *LifecycleNotificationWorker {
	return &LifecycleNotificationWorker{
		store: store, writer: writer, observer: observer,
		owner:        fmt.Sprintf("video-notification-%d", time.Now().UTC().UnixNano()),
		now:          func() time.Time { return time.Now().UTC() },
		batchSize:    defaultLifecycleNotificationBatchSize,
		pollInterval: defaultLifecycleNotificationPoll,
		lease:        defaultLifecycleNotificationLease,
	}
}

func (w *LifecycleNotificationWorker) Start(ctx context.Context) error {
	if w == nil || w.store == nil || w.writer == nil {
		return nil
	}
	_, _ = w.DispatchOnce(ctx)
	go func() {
		ticker := time.NewTicker(w.pollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_, _ = w.DispatchOnce(ctx)
			}
		}
	}()
	return nil
}

func (w *LifecycleNotificationWorker) DispatchOnce(ctx context.Context) (int, error) {
	if w == nil || w.store == nil || w.writer == nil {
		return 0, nil
	}
	if reconciler, ok := w.store.(interface {
		ReconcileLifecyclePublicationNotifications(context.Context, int) (int, error)
	}); ok {
		if _, err := reconciler.ReconcileLifecyclePublicationNotifications(ctx, w.batchSize); err != nil {
			w.observe("retry")
			return 0, err
		}
	}
	processed := 0
	var resultErr error
	for claimed := 0; claimed < w.batchSize; claimed++ {
		now := w.now().UTC()
		items, err := w.store.ClaimLifecycleNotifications(ctx, w.owner, 1, now, now.Add(w.lease))
		if err != nil {
			w.observe("retry")
			return processed, err
		}
		if len(items) == 0 {
			break
		}
		item := items[0]
		title, content, err := applicationmessage.LifecycleMessageContent(item.LifecycleNotification)
		if err == nil {
			deliverCtx, cancel := context.WithTimeout(ctx, defaultLifecycleNotificationTimeout)
			err = w.writer.WriteLifecycleNotification(
				deliverCtx, item.LifecycleNotification, title, content,
			)
			cancel()
		}
		if err == nil {
			if markErr := w.store.MarkLifecycleNotificationDelivered(
				ctx, item.EventID, w.owner, w.now().UTC(),
			); markErr != nil {
				w.observe("retry")
				return processed, markErr
			}
			processed++
			w.observe("delivered")
			continue
		}
		terminal := isTerminalLifecycleNotificationError(err)
		failedAt := w.now().UTC()
		if markErr := w.store.MarkLifecycleNotificationFailed(
			ctx, item.EventID, w.owner,
			failedAt.Add(lifecycleNotificationRetryDelay(item.Attempts)),
			err.Error(), terminal,
		); markErr != nil {
			w.observe("retry")
			return processed, markErr
		}
		if terminal {
			w.observe("terminal")
		} else {
			w.observe("retry")
		}
		resultErr = errors.Join(resultErr, err)
	}
	return processed, resultErr
}

func lifecycleNotificationRetryDelay(attempts int) time.Duration {
	if attempts < 1 {
		attempts = 1
	}
	delay := time.Second << min(attempts-1, 6)
	if delay > maxLifecycleNotificationRetry {
		return maxLifecycleNotificationRetry
	}
	return delay
}

func isTerminalLifecycleNotificationError(err error) bool {
	return errors.Is(err, domainmessage.ErrInvalidUserID) ||
		errors.Is(err, domainmessage.ErrInvalidMessageType) ||
		errors.Is(err, domainmessage.ErrInvalidMessageTarget) ||
		errors.Is(err, domainmessage.ErrInvalidLifecycle) ||
		errors.Is(err, domainmessage.ErrEmptyTitle) ||
		errors.Is(err, domainmessage.ErrTitleTooLong) ||
		errors.Is(err, domainmessage.ErrEmptyContent) ||
		errors.Is(err, domainmessage.ErrContentTooLong) ||
		errors.Is(err, domainmessage.ErrEventIDTooLong) ||
		errors.Is(err, domainmessage.ErrIdempotencyKeyTooLong) ||
		errors.Is(err, ErrLifecycleNotificationSuperseded)
}

func (w *LifecycleNotificationWorker) observe(result string) {
	if w != nil && w.observer != nil {
		w.observer.ObserveLifecycleNotification(strings.TrimSpace(result))
	}
}
