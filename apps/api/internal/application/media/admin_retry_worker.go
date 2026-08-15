package applicationmedia

import (
	"context"
	"errors"
	"fmt"
	"time"

	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
	inframetrics "github.com/shiyudesu/frux/internal/infra/metrics"
)

const (
	defaultRetryNotificationBatch = 50
	defaultRetryNotificationPoll  = time.Second
	defaultRetryNotificationLease = 30 * time.Second
	defaultRetryNotificationCall  = 5 * time.Second
)

type ProcessingRetryNotificationRepository interface {
	ClaimProcessingRetryNotifications(
		ctx context.Context,
		leaseOwner string,
		limit int,
		now, leaseUntil time.Time,
	) ([]*domainmedia.RetryNotificationOutboxItem, error)
	MarkProcessingRetryNotificationDelivered(
		ctx context.Context,
		eventID, leaseOwner string,
		deliveredAt time.Time,
	) error
	MarkProcessingRetryNotificationFailed(
		ctx context.Context,
		eventID, leaseOwner string,
		availableAt time.Time,
		reason string,
		terminal bool,
	) error
	CountPendingProcessingRetryNotifications(ctx context.Context) (int64, error)
}

type ProcessingRetryNotificationWorker struct {
	repository ProcessingRetryNotificationRepository
	notifier   MediaStateNotifier
	owner      string
	now        func() time.Time
}

func NewProcessingRetryNotificationWorker(
	repository ProcessingRetryNotificationRepository,
	notifier MediaStateNotifier,
) *ProcessingRetryNotificationWorker {
	return &ProcessingRetryNotificationWorker{
		repository: repository, notifier: notifier,
		owner: fmt.Sprintf("media-retry-notification-%d", time.Now().UTC().UnixNano()),
		now:   func() time.Time { return time.Now().UTC() },
	}
}

func (w *ProcessingRetryNotificationWorker) Start(ctx context.Context) {
	if w == nil || w.repository == nil || w.notifier == nil {
		return
	}
	go func() {
		ticker := time.NewTicker(defaultRetryNotificationPoll)
		defer ticker.Stop()
		for {
			if _, err := w.DispatchOnce(ctx); err != nil && ctx.Err() != nil {
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func (w *ProcessingRetryNotificationWorker) DispatchOnce(
	ctx context.Context,
) (int, error) {
	if w == nil || w.repository == nil || w.notifier == nil {
		return 0, nil
	}
	processed := 0
	var resultErr error
	if backlog, err := w.repository.CountPendingProcessingRetryNotifications(ctx); err == nil {
		inframetrics.MediaRetryOutboxBacklog.Set(float64(backlog))
	}
	for processed < defaultRetryNotificationBatch {
		now := w.now()
		items, err := w.repository.ClaimProcessingRetryNotifications(
			ctx, w.owner, 1, now, now.Add(defaultRetryNotificationLease),
		)
		if err != nil {
			return processed, err
		}
		if len(items) == 0 {
			break
		}
		item := items[0]
		notifyCtx, cancel := context.WithTimeout(ctx, defaultRetryNotificationCall)
		err = w.notifier.MediaRepairing(notifyCtx, item.AssetID, "")
		cancel()
		if err == nil {
			if err := w.repository.MarkProcessingRetryNotificationDelivered(
				ctx, item.EventID, w.owner, w.now(),
			); err != nil {
				return processed, err
			}
			processed++
			inframetrics.ObserveMediaRetryProjection("delivered")
			continue
		}
		terminal := errors.Is(err, domainmedia.ErrMediaAssetNotFound)
		failedAt := w.now()
		if markErr := w.repository.MarkProcessingRetryNotificationFailed(
			ctx, item.EventID, w.owner,
			failedAt.Add(processingRetryDelay(item.Attempts)),
			err.Error(), terminal,
		); markErr != nil {
			return processed, markErr
		}
		if terminal {
			inframetrics.ObserveMediaRetryProjection("terminal")
		} else {
			inframetrics.ObserveMediaRetryProjection("retry")
		}
		resultErr = errors.Join(resultErr, err)
		processed++
	}
	if backlog, err := w.repository.CountPendingProcessingRetryNotifications(ctx); err == nil {
		inframetrics.MediaRetryOutboxBacklog.Set(float64(backlog))
	}
	return processed, resultErr
}
