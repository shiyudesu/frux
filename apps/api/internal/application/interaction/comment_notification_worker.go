package applicationinteraction

import (
	"context"
	"errors"
	"fmt"
	domaininteraction "github.com/shiyudesu/frux/internal/domain/interaction"
	domainmessage "github.com/shiyudesu/frux/internal/domain/message"
	inframetrics "github.com/shiyudesu/frux/internal/infra/metrics"
	"strings"
	"time"
)

const (
	defaultCommentNotificationBatchSize       = 50
	defaultCommentNotificationPollInterval    = time.Second
	defaultCommentNotificationLease           = 30 * time.Second
	defaultCommentNotificationDeliveryTimeout = 5 * time.Second
	maxCommentNotificationRetryDelay          = time.Minute
)

type CommentNotificationDelivery struct {
	EventID        string
	RecipientID    int64
	ActorID        int64
	ActorNickname  string
	ActorAvatarURL string
	MessageType    string
	Title          string
	Content        string
	VideoID        int64
	RootCommentID  int64
	CommentID      int64
}

type CommentNotificationMessageWriter interface {
	WriteCommentNotification(ctx context.Context, notification CommentNotificationDelivery) error
}

type CommentNotificationActorReader interface {
	GetUserProfile(ctx context.Context, userID int64) (*domaininteraction.UserProfile, error)
}

type CommentNotificationWorker struct {
	store        domaininteraction.CommentNotificationOutboxRepository
	actors       CommentNotificationActorReader
	writer       CommentNotificationMessageWriter
	owner        string
	now          func() time.Time
	batchSize    int
	pollInterval time.Duration
	lease        time.Duration
}

type CommentNotificationWorkerOption func(*CommentNotificationWorker)

func NewCommentNotificationWorker(store domaininteraction.CommentNotificationOutboxRepository, actors CommentNotificationActorReader, writer CommentNotificationMessageWriter, options ...CommentNotificationWorkerOption) *CommentNotificationWorker {
	worker := &CommentNotificationWorker{
		store: store, actors: actors, writer: writer,
		owner:     fmt.Sprintf("comment-notification-%d", time.Now().UTC().UnixNano()),
		now:       func() time.Time { return time.Now().UTC() },
		batchSize: defaultCommentNotificationBatchSize, pollInterval: defaultCommentNotificationPollInterval,
		lease: defaultCommentNotificationLease,
	}
	for _, option := range options {
		option(worker)
	}
	return worker
}

func WithCommentNotificationWorkerOwner(owner string) CommentNotificationWorkerOption {
	return func(worker *CommentNotificationWorker) {
		if owner = strings.TrimSpace(owner); owner != "" {
			worker.owner = owner
		}
	}
}

func WithCommentNotificationWorkerNow(now func() time.Time) CommentNotificationWorkerOption {
	return func(worker *CommentNotificationWorker) {
		if now != nil {
			worker.now = now
		}
	}
}

func WithCommentNotificationWorkerPollInterval(interval time.Duration) CommentNotificationWorkerOption {
	return func(worker *CommentNotificationWorker) {
		if interval > 0 {
			worker.pollInterval = interval
		}
	}
}

func (w *CommentNotificationWorker) Start(ctx context.Context) error {
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

func (w *CommentNotificationWorker) DispatchOnce(ctx context.Context) (processed int, resultErr error) {
	start := time.Now()
	defer func() {
		inframetrics.ObserveWorkerJob("comment_notification_outbox", time.Since(start), resultErr)
	}()
	if w == nil || w.store == nil || w.writer == nil {
		return 0, nil
	}
	var dispatchErr error
	for claimed := 0; claimed < w.batchSize; claimed++ {
		claimNow := w.now().UTC()
		items, err := w.store.ClaimCommentNotifications(
			ctx, w.owner, 1, claimNow, claimNow.Add(w.lease),
		)
		if err != nil {
			return processed, err
		}
		if len(items) == 0 {
			break
		}
		item := items[0]
		if item == nil {
			break
		}
		delivery := CommentNotificationDelivery{
			EventID: item.EventID, RecipientID: item.RecipientID, ActorID: item.ActorID,
			MessageType: item.MessageType, Title: item.Title, Content: item.Content,
			VideoID: item.VideoID, RootCommentID: item.RootCommentID, CommentID: item.CommentID,
		}
		deliverCtx, cancel := context.WithTimeout(ctx, defaultCommentNotificationDeliveryTimeout)
		if w.actors != nil {
			if actor, actorErr := w.actors.GetUserProfile(deliverCtx, item.ActorID); actorErr == nil && actor != nil {
				delivery.ActorNickname = actor.Nickname
				delivery.ActorAvatarURL = actor.AvatarURL
			}
		}
		err = w.writer.WriteCommentNotification(deliverCtx, delivery)
		cancel()
		if err == nil {
			if markErr := w.store.MarkCommentNotificationDelivered(ctx, item.EventID, w.owner, w.now().UTC()); markErr != nil {
				return processed, markErr
			}
			processed++
			continue
		}
		terminal := isTerminalCommentNotificationError(err)
		failedAt := w.now().UTC()
		next := failedAt.Add(commentNotificationRetryDelay(item.Attempts))
		if markErr := w.store.MarkCommentNotificationFailed(ctx, item.EventID, w.owner, next, err.Error(), terminal); markErr != nil {
			return processed, markErr
		}
		dispatchErr = errors.Join(dispatchErr, err)
	}
	return processed, dispatchErr
}

func commentNotificationRetryDelay(attempts int) time.Duration {
	if attempts < 1 {
		attempts = 1
	}
	delay := time.Second << min(attempts-1, 6)
	if delay > maxCommentNotificationRetryDelay {
		return maxCommentNotificationRetryDelay
	}
	return delay
}

func isTerminalCommentNotificationError(err error) bool {
	return errors.Is(err, domainmessage.ErrInvalidUserID) ||
		errors.Is(err, domainmessage.ErrInvalidMessageType) ||
		errors.Is(err, domainmessage.ErrEmptyTitle) ||
		errors.Is(err, domainmessage.ErrTitleTooLong) ||
		errors.Is(err, domainmessage.ErrEmptyContent) ||
		errors.Is(err, domainmessage.ErrContentTooLong) ||
		errors.Is(err, domainmessage.ErrEventIDTooLong) ||
		errors.Is(err, domainmessage.ErrIdempotencyKeyTooLong) ||
		errors.Is(err, domainmessage.ErrInvalidMessageTarget)
}
