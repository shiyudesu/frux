package applicationreview

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	applicationmessage "github.com/shiyudesu/frux/internal/application/message"
	applicationvideo "github.com/shiyudesu/frux/internal/application/video"
	domainmessage "github.com/shiyudesu/frux/internal/domain/message"
	domainreview "github.com/shiyudesu/frux/internal/domain/review"
)

const (
	defaultReviewNotificationBatchSize       = 50
	defaultReviewNotificationPollInterval    = time.Second
	defaultReviewNotificationLease           = 30 * time.Second
	defaultReviewNotificationDeliveryTimeout = 5 * time.Second
	maxReviewNotificationRetryDelay          = time.Minute
)

type ReviewNotificationRepository interface {
	ClaimReviewNotifications(ctx context.Context, leaseOwner string, limit int, now, leaseUntil time.Time) ([]*domainreview.ReviewNotification, error)
	MarkReviewNotificationDelivered(ctx context.Context, eventID, leaseOwner string, deliveredAt time.Time) error
	MarkReviewNotificationFailed(ctx context.Context, eventID, leaseOwner string, availableAt time.Time, reason string, terminal bool) error
}

type ReviewNotificationDelivery struct {
	EventID       string
	RecipientID   int64
	VideoID       int64
	Outcome       string
	MessageType   string
	ReviewVersion int
	Stage         string
	Result        string
	ReasonCode    string
	OccurredAt    time.Time
	Title         string
	Content       string
}

type ReviewNotificationMessageWriter interface {
	WriteReviewNotification(ctx context.Context, notification ReviewNotificationDelivery) error
}

type ReviewNotificationObserver interface {
	ObserveHumanNotification(result string)
}

type ReviewNotificationWorker struct {
	store        ReviewNotificationRepository
	writer       ReviewNotificationMessageWriter
	observer     ReviewNotificationObserver
	owner        string
	now          func() time.Time
	batchSize    int
	pollInterval time.Duration
	lease        time.Duration
}

func NewReviewNotificationWorker(
	store ReviewNotificationRepository,
	writer ReviewNotificationMessageWriter,
	observer ReviewNotificationObserver,
) *ReviewNotificationWorker {
	return &ReviewNotificationWorker{
		store: store, writer: writer, observer: observer,
		owner:        fmt.Sprintf("review-notification-%d", time.Now().UTC().UnixNano()),
		now:          func() time.Time { return time.Now().UTC() },
		batchSize:    defaultReviewNotificationBatchSize,
		pollInterval: defaultReviewNotificationPollInterval,
		lease:        defaultReviewNotificationLease,
	}
}

func (w *ReviewNotificationWorker) Start(ctx context.Context) error {
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

func (w *ReviewNotificationWorker) DispatchOnce(ctx context.Context) (int, error) {
	if w == nil || w.store == nil || w.writer == nil {
		return 0, nil
	}
	processed := 0
	var resultErr error
	for claimed := 0; claimed < w.batchSize; claimed++ {
		now := w.now().UTC()
		items, err := w.store.ClaimReviewNotifications(ctx, w.owner, 1, now, now.Add(w.lease))
		if err != nil {
			w.observeNotification("retry")
			return processed, err
		}
		if len(items) == 0 {
			break
		}
		item := items[0]
		delivery, deliveryErr := reviewNotificationDelivery(item)
		if deliveryErr != nil {
			err = deliveryErr
		} else {
			deliverCtx, cancel := context.WithTimeout(ctx, defaultReviewNotificationDeliveryTimeout)
			err = w.writer.WriteReviewNotification(deliverCtx, delivery)
			cancel()
		}
		if err == nil {
			if markErr := w.store.MarkReviewNotificationDelivered(ctx, item.EventID, w.owner, w.now().UTC()); markErr != nil {
				w.observeNotification("retry")
				return processed, markErr
			}
			processed++
			w.observeNotification("delivered")
			continue
		}
		terminal := isTerminalReviewNotificationError(err)
		failedAt := w.now().UTC()
		if markErr := w.store.MarkReviewNotificationFailed(
			ctx, item.EventID, w.owner, failedAt.Add(reviewNotificationRetryDelay(item.Attempts)),
			err.Error(), terminal,
		); markErr != nil {
			w.observeNotification("retry")
			return processed, markErr
		}
		if terminal {
			w.observeNotification("terminal")
		} else {
			w.observeNotification("retry")
		}
		resultErr = errors.Join(resultErr, err)
	}
	return processed, resultErr
}

func reviewNotificationDelivery(item *domainreview.ReviewNotification) (ReviewNotificationDelivery, error) {
	delivery := ReviewNotificationDelivery{
		EventID: item.EventID, RecipientID: item.RecipientID, VideoID: item.VideoID,
		Outcome: item.Outcome,
	}
	if item.Stage != "" {
		notification := domainmessage.LifecycleNotification{
			EventID: item.EventID, RecipientID: item.RecipientID,
			VideoID: item.VideoID, ReviewVersion: item.ReviewVersion,
			Stage: item.Stage, Result: item.Result, ReasonCode: item.ReasonCode,
			OccurredAt: item.OccurredAt,
		}
		title, content, err := applicationmessage.LifecycleMessageContent(notification)
		if err != nil {
			return delivery, err
		}
		delivery.MessageType = domainmessage.TypeVideoLifecycle
		delivery.ReviewVersion = item.ReviewVersion
		delivery.Stage = item.Stage
		delivery.Result = item.Result
		delivery.ReasonCode = item.ReasonCode
		delivery.OccurredAt = item.OccurredAt
		delivery.Title = title
		delivery.Content = content
		return delivery, nil
	}
	delivery.MessageType = domainmessage.TypeSystem
	if item.Outcome == domainreview.OutcomeApprove {
		delivery.Title = "视频审核通过"
		delivery.Content = "你的视频已通过审核并进入发布流程。"
	} else {
		delivery.Title = "视频审核未通过"
		delivery.Content = "你的视频未通过审核，请检查内容后再试。"
	}
	return delivery, nil
}

func reviewNotificationRetryDelay(attempts int) time.Duration {
	if attempts < 1 {
		attempts = 1
	}
	delay := time.Second << min(attempts-1, 6)
	if delay > maxReviewNotificationRetryDelay {
		return maxReviewNotificationRetryDelay
	}
	return delay
}

func isTerminalReviewNotificationError(err error) bool {
	return errors.Is(err, domainmessage.ErrInvalidUserID) ||
		errors.Is(err, domainmessage.ErrInvalidMessageType) ||
		errors.Is(err, domainmessage.ErrEmptyTitle) ||
		errors.Is(err, domainmessage.ErrTitleTooLong) ||
		errors.Is(err, domainmessage.ErrEmptyContent) ||
		errors.Is(err, domainmessage.ErrContentTooLong) ||
		errors.Is(err, domainmessage.ErrEventIDTooLong) ||
		errors.Is(err, domainmessage.ErrIdempotencyKeyTooLong) ||
		errors.Is(err, domainmessage.ErrInvalidMessageTarget) ||
		errors.Is(err, domainmessage.ErrInvalidLifecycle) ||
		errors.Is(err, applicationvideo.ErrLifecycleNotificationSuperseded)
}

func (w *ReviewNotificationWorker) observeNotification(result string) {
	if w != nil && w.observer != nil {
		w.observer.ObserveHumanNotification(strings.TrimSpace(result))
	}
}
