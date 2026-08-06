package applicationvideo

import (
	"context"
	"errors"
	"fmt"
	"time"

	domainvideo "github.com/shiyudesu/frux/internal/domain/video"
)

const (
	defaultAdminIntentBatchSize       = 50
	defaultAdminIntentPollInterval    = time.Second
	defaultAdminIntentLease           = 30 * time.Second
	defaultAdminIntentDeliveryTimeout = 10 * time.Second
	maxAdminIntentRetryDelay          = time.Minute
)

var ErrAdminIntentWorkerUnavailable = errors.New("admin transition intent worker dependencies are unavailable")

type AdminTransitionIntentStore interface {
	ClaimAdminTransitionIntents(
		ctx context.Context,
		leaseOwner string,
		limit int,
		now, leaseUntil time.Time,
	) ([]*domainvideo.AdminTransitionIntent, error)
	MarkAdminTransitionIntentDelivered(
		ctx context.Context,
		intentID int64,
		leaseOwner string,
		deliveredAt time.Time,
	) error
	MarkAdminTransitionIntentFailed(
		ctx context.Context,
		intentID int64,
		leaseOwner string,
		availableAt time.Time,
		reason string,
	) error
}

type AdminTransitionVideoReader interface {
	FindByIDAnyStatus(ctx context.Context, videoID int64) (*domainvideo.Video, error)
}

type AdminTransitionSideEffects interface {
	ApplyAdminTransition(ctx context.Context, video *domainvideo.Video) error
}

type AdminTransitionIntentWorker struct {
	store           AdminTransitionIntentStore
	videos          AdminTransitionVideoReader
	cache           VideoCacheInvalidator
	sideEffects     AdminTransitionSideEffects
	owner           string
	now             func() time.Time
	batchSize       int
	pollInterval    time.Duration
	lease           time.Duration
	deliveryTimeout time.Duration
}

func NewAdminTransitionIntentWorker(
	store AdminTransitionIntentStore,
	videos AdminTransitionVideoReader,
	cache VideoCacheInvalidator,
	sideEffects AdminTransitionSideEffects,
) *AdminTransitionIntentWorker {
	return &AdminTransitionIntentWorker{
		store: store, videos: videos, cache: cache, sideEffects: sideEffects,
		owner:           fmt.Sprintf("video-admin-intent-%d", time.Now().UTC().UnixNano()),
		now:             func() time.Time { return time.Now().UTC() },
		batchSize:       defaultAdminIntentBatchSize,
		pollInterval:    defaultAdminIntentPollInterval,
		lease:           defaultAdminIntentLease,
		deliveryTimeout: defaultAdminIntentDeliveryTimeout,
	}
}

func (w *AdminTransitionIntentWorker) Start(ctx context.Context) error {
	if !w.ready() {
		return ErrAdminIntentWorkerUnavailable
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

func (w *AdminTransitionIntentWorker) DispatchOnce(ctx context.Context) (int, error) {
	if !w.ready() {
		return 0, ErrAdminIntentWorkerUnavailable
	}
	now := w.now().UTC()
	intents, err := w.store.ClaimAdminTransitionIntents(
		ctx, w.owner, w.batchSize, now, now.Add(w.lease),
	)
	if err != nil {
		return 0, err
	}
	delivered := 0
	var dispatchErr error
	for _, intent := range intents {
		if intent == nil {
			continue
		}
		deliverCtx, cancel := context.WithTimeout(ctx, w.deliveryTimeout)
		err := w.deliver(deliverCtx, intent)
		cancel()
		if err == nil {
			if markErr := w.store.MarkAdminTransitionIntentDelivered(
				ctx, intent.ID, w.owner, w.now().UTC(),
			); markErr != nil {
				return delivered, markErr
			}
			delivered++
			continue
		}
		failedAt := w.now().UTC()
		if markErr := w.store.MarkAdminTransitionIntentFailed(
			ctx,
			intent.ID,
			w.owner,
			failedAt.Add(adminIntentRetryDelay(intent.Attempts)),
			err.Error(),
		); markErr != nil {
			return delivered, markErr
		}
		dispatchErr = errors.Join(dispatchErr, err)
	}
	return delivered, dispatchErr
}

func (w *AdminTransitionIntentWorker) ready() bool {
	return w != nil && w.store != nil && w.videos != nil &&
		w.cache != nil && w.sideEffects != nil
}

func (w *AdminTransitionIntentWorker) deliver(
	ctx context.Context,
	intent *domainvideo.AdminTransitionIntent,
) error {
	cacheErr := w.cache.InvalidateVideo(ctx, intent.VideoID)
	video, err := w.videos.FindByIDAnyStatus(ctx, intent.VideoID)
	if errors.Is(err, domainvideo.ErrVideoNotFound) {
		return cacheErr
	}
	if err != nil {
		return errors.Join(cacheErr, err)
	}
	return errors.Join(cacheErr, w.sideEffects.ApplyAdminTransition(ctx, video))
}

func adminIntentRetryDelay(attempts int) time.Duration {
	if attempts < 1 {
		attempts = 1
	}
	delay := time.Second << min(attempts-1, 6)
	if delay > maxAdminIntentRetryDelay {
		return maxAdminIntentRetryDelay
	}
	return delay
}
