package applicationrecommendation

import (
	applicationexposure "GCFeed/internal/application/exposure"
	applicationinteraction "GCFeed/internal/application/interaction"
	domainrecommendation "GCFeed/internal/domain/recommendation"
	domainrelation "GCFeed/internal/domain/relation"
	inframetrics "GCFeed/internal/infra/metrics"
	"context"
	"errors"
	"time"
)

const (
	defaultProfileOutboxBatchSize    = 50
	defaultProfileOutboxPollInterval = time.Second
	defaultProfileOutboxLease        = 30 * time.Second
	maxProfileOutboxRetryDelay       = time.Minute
)

// FeedbackProfileOutboxStore provides the feedback fact queue. Implementations
// must claim and mark rows durably because projection can be retried.
type FeedbackProfileOutboxStore interface {
	ClaimFeedbackProfileOutbox(ctx context.Context, limit int, now, leasedUntil time.Time) ([]domainrecommendation.FeedbackProjectionOutboxItem, error)
	MarkFeedbackProfileOutboxDispatched(ctx context.Context, id int64, dispatchedAt time.Time) error
	MarkFeedbackProfileOutboxFailed(ctx context.Context, id int64, availableAt time.Time, reason string) error
}

// FollowProfileOutboxStore provides the relation-owned follow fact queue.
type FollowProfileOutboxStore interface {
	ClaimFollowProfileOutbox(ctx context.Context, limit int, now, leasedUntil time.Time) ([]domainrelation.FollowProjectionOutboxItem, error)
	MarkFollowProfileOutboxDispatched(ctx context.Context, id int64, dispatchedAt time.Time) error
	MarkFollowProfileOutboxFailed(ctx context.Context, id int64, availableAt time.Time, reason string) error
}

// ActionProfileOutboxStore is owned by interaction and survives RabbitMQ
// publish failures, allowing accepted actions to be projected eventually.
type ActionProfileOutboxStore interface {
	ClaimActionProfileProjections(ctx context.Context, limit int, now, leasedUntil time.Time) ([]applicationinteraction.ActionProfileProjectionItem, error)
	MarkActionProfileProjectionDispatched(ctx context.Context, eventID string, dispatchedAt time.Time) error
	MarkActionProfileProjectionFailed(ctx context.Context, eventID string, availableAt time.Time, reason string) error
}

// BehaviorProfileProjectionItem is a durable behavior fact awaiting both its
// profile projection and recommendation-outcome attribution.
type BehaviorProfileProjectionItem struct {
	EventID           string
	UserID            int64
	VideoID           int64
	Scene             string
	RequestID         string
	EventType         string
	PlaybackSessionID string
	Sequence          int64
	PositionMs        int
	WatchMs           int
	DurationMs        *int
	Completed         bool
	RecordedAt        time.Time
	OccurredAt        time.Time
	Attempts          int
}

// BehaviorProfileOutboxStore leases persisted behavior facts after RabbitMQ
// delivery has been acknowledged. It prevents unavailable embeddings or
// evidence propagation from spinning on the broker.
type BehaviorProfileOutboxStore interface {
	ClaimBehaviorProfileProjections(ctx context.Context, limit int, now, leasedUntil time.Time) ([]BehaviorProfileProjectionItem, error)
	MarkBehaviorProfileProjectionDispatched(ctx context.Context, userID int64, eventID string, dispatchedAt time.Time) error
	MarkBehaviorProfileProjectionFailed(ctx context.Context, userID int64, eventID string, availableAt time.Time, reason string) error
}

// OutcomeAttributionRecorder persists recommendation outcomes only after
// validating durable request membership and optional follow authorship.
type OutcomeAttributionRecorder interface {
	VerifyAndSaveOutcome(ctx context.Context, outcome *domainrecommendation.Outcome, followedTargetUserID int64) (recorded bool, attributed bool, err error)
}

// ProfileOutboxWorker applies profile signals only after their owning HTTP
// transaction has committed. A failed projection leaves its durable signal
// pending for retry.
type ProfileOutboxWorker struct {
	feedbackStore FeedbackProfileOutboxStore
	followStore   FollowProfileOutboxStore
	actionStore   ActionProfileOutboxStore
	behaviorStore BehaviorProfileOutboxStore
	projector     *ProfileProjector
	outcomes      OutcomeAttributionRecorder
	now           func() time.Time
	batchSize     int
	pollInterval  time.Duration
	lease         time.Duration
}

type ProfileOutboxWorkerOption func(*ProfileOutboxWorker)

func NewProfileOutboxWorker(projector *ProfileProjector, feedbackStore FeedbackProfileOutboxStore, followStore FollowProfileOutboxStore, options ...ProfileOutboxWorkerOption) *ProfileOutboxWorker {
	worker := &ProfileOutboxWorker{
		feedbackStore: feedbackStore, followStore: followStore, projector: projector,
		now: func() time.Time { return time.Now().UTC() }, batchSize: defaultProfileOutboxBatchSize,
		pollInterval: defaultProfileOutboxPollInterval, lease: defaultProfileOutboxLease,
	}
	for _, option := range options {
		option(worker)
	}
	return worker
}

func WithProfileOutboxOutcomeRepository(outcomes OutcomeAttributionRecorder) ProfileOutboxWorkerOption {
	return func(worker *ProfileOutboxWorker) {
		worker.outcomes = outcomes
	}
}

func WithActionProfileOutboxStore(store ActionProfileOutboxStore) ProfileOutboxWorkerOption {
	return func(worker *ProfileOutboxWorker) {
		worker.actionStore = store
	}
}

func WithBehaviorProfileOutboxStore(store BehaviorProfileOutboxStore) ProfileOutboxWorkerOption {
	return func(worker *ProfileOutboxWorker) {
		worker.behaviorStore = store
	}
}

func (w *ProfileOutboxWorker) Start(ctx context.Context) error {
	if w == nil || w.projector == nil {
		return nil
	}
	if _, err := w.DispatchOnce(ctx); err != nil {
		inframetrics.ObserveProfileWorker(time.Time{}, false, err)
	}
	go func() {
		ticker := time.NewTicker(w.pollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if _, err := w.DispatchOnce(ctx); err != nil {
					inframetrics.ObserveProfileWorker(time.Time{}, false, err)
				}
			}
		}
	}()
	return nil
}

func (w *ProfileOutboxWorker) DispatchOnce(ctx context.Context) (int, error) {
	if w == nil || w.projector == nil {
		return 0, nil
	}
	now := w.now().UTC()
	dispatched := 0
	var dispatchErr error
	if w.feedbackStore != nil {
		items, err := w.feedbackStore.ClaimFeedbackProfileOutbox(ctx, w.batchSize, now, now.Add(w.lease))
		if err != nil {
			return dispatched, err
		}
		for _, item := range items {
			applied, err := w.projector.ApplyFeedback(ctx, item.Feedback)
			if err != nil {
				inframetrics.ObserveProfileWorker(feedbackOccurredAt(item.Feedback), false, err)
				next := now.Add(profileOutboxRetryDelay(item.Attempts))
				if markErr := w.feedbackStore.MarkFeedbackProfileOutboxFailed(ctx, item.ID, next, err.Error()); markErr != nil {
					return dispatched, markErr
				}
				dispatchErr = errors.Join(dispatchErr, err)
				continue
			}
			inframetrics.ObserveProfileWorker(feedbackOccurredAt(item.Feedback), !applied, nil)
			if err := w.feedbackStore.MarkFeedbackProfileOutboxDispatched(ctx, item.ID, w.now().UTC()); err != nil {
				return dispatched, err
			}
			dispatched++
		}
	}
	if w.followStore != nil {
		items, err := w.followStore.ClaimFollowProfileOutbox(ctx, w.batchSize, now, now.Add(w.lease))
		if err != nil {
			return dispatched, err
		}
		for _, item := range items {
			applied, err := w.projector.ApplyFollow(ctx, item.EventID, item.UserID, item.AuthorID, item.Active, item.OccurredAt)
			if err != nil {
				inframetrics.ObserveProfileWorker(item.OccurredAt, false, err)
				next := now.Add(profileOutboxRetryDelay(item.Attempts))
				if markErr := w.followStore.MarkFollowProfileOutboxFailed(ctx, item.ID, next, err.Error()); markErr != nil {
					return dispatched, markErr
				}
				dispatchErr = errors.Join(dispatchErr, err)
				continue
			}
			if item.Active && item.RecommendationRequestID != "" && w.outcomes != nil {
				outcome, outcomeErr := domainrecommendation.NewOutcome(
					domainrecommendation.OutcomeID("follow", item.EventID),
					item.RecommendationRequestID,
					item.UserID,
					item.RecommendationVideoID,
					"follow",
					item.OccurredAt,
				)
				if outcomeErr == nil {
					var recorded bool
					recorded, attributed, saveErr := w.outcomes.VerifyAndSaveOutcome(ctx, outcome, item.AuthorID)
					outcomeErr = saveErr
					if outcomeErr == nil && !attributed {
						inframetrics.ObserveRecommendationInvalidAttribution(outcome.OutcomeType)
					} else if outcomeErr == nil && recorded {
						inframetrics.ObserveRecommendationOutcome(outcome.OutcomeType)
					}
				}
				if outcomeErr != nil {
					if isTerminalOutcomeValidationError(outcomeErr) {
						inframetrics.ObserveRecommendationInvalidAttribution("unknown")
					} else {
						inframetrics.ObserveProfileWorker(item.OccurredAt, false, outcomeErr)
						next := now.Add(profileOutboxRetryDelay(item.Attempts))
						if markErr := w.followStore.MarkFollowProfileOutboxFailed(ctx, item.ID, next, outcomeErr.Error()); markErr != nil {
							return dispatched, markErr
						}
						dispatchErr = errors.Join(dispatchErr, outcomeErr)
						continue
					}
				}
			}
			inframetrics.ObserveProfileWorker(item.OccurredAt, !applied, nil)
			if err := w.followStore.MarkFollowProfileOutboxDispatched(ctx, item.ID, w.now().UTC()); err != nil {
				return dispatched, err
			}
			dispatched++
		}
	}
	if w.actionStore != nil {
		items, err := w.actionStore.ClaimActionProfileProjections(ctx, w.batchSize, now, now.Add(w.lease))
		if err != nil {
			return dispatched, err
		}
		for _, item := range items {
			event := &applicationinteraction.ActionChangedEvent{
				EventID: item.EventID, UserID: item.UserID, VideoID: item.VideoID, ActionType: item.ActionType,
				Active: item.Active, IdempotencyKey: item.IdempotencyKey, Version: item.Version, OccurredAt: item.OccurredAt,
			}
			applied, err := w.projector.ApplyAction(ctx, event)
			if err != nil {
				inframetrics.ObserveProfileWorker(item.OccurredAt, false, err)
				next := now.Add(profileOutboxRetryDelay(item.Attempts))
				if markErr := w.actionStore.MarkActionProfileProjectionFailed(ctx, item.EventID, next, err.Error()); markErr != nil {
					return dispatched, markErr
				}
				dispatchErr = errors.Join(dispatchErr, err)
				continue
			}
			inframetrics.ObserveProfileWorker(item.OccurredAt, !applied, nil)
			if err := w.actionStore.MarkActionProfileProjectionDispatched(ctx, item.EventID, w.now().UTC()); err != nil {
				return dispatched, err
			}
			dispatched++
		}
	}
	if w.behaviorStore != nil {
		items, err := w.behaviorStore.ClaimBehaviorProfileProjections(ctx, w.batchSize, now, now.Add(w.lease))
		if err != nil {
			return dispatched, err
		}
		for _, item := range items {
			outcomeErr := w.recordViewOutcome(ctx, item)
			if outcomeErr != nil && !isTerminalOutcomeValidationError(outcomeErr) {
				inframetrics.ObserveProfileWorker(item.OccurredAt, false, outcomeErr)
				next := now.Add(profileOutboxRetryDelay(item.Attempts))
				if markErr := w.behaviorStore.MarkBehaviorProfileProjectionFailed(ctx, item.UserID, item.EventID, next, outcomeErr.Error()); markErr != nil {
					return dispatched, markErr
				}
				dispatchErr = errors.Join(dispatchErr, outcomeErr)
				continue
			}
			if outcomeErr != nil {
				inframetrics.ObserveRecommendationInvalidAttribution("unknown")
			}
			event := &applicationexposure.ViewEventRecordedEvent{
				EventID: item.EventID, UserID: item.UserID, VideoID: item.VideoID, Scene: item.Scene,
				RequestID: item.RequestID, EventType: item.EventType, PlaybackSessionID: item.PlaybackSessionID,
				Sequence: item.Sequence, PositionMs: item.PositionMs, WatchMs: item.WatchMs,
				DurationMs: item.DurationMs, Completed: item.Completed, RecordedAt: item.RecordedAt, OccurredAt: item.OccurredAt,
			}
			applied, err := w.projector.ApplyView(ctx, event)
			if err != nil {
				inframetrics.ObserveProfileWorker(item.OccurredAt, false, err)
				next := now.Add(profileOutboxRetryDelay(item.Attempts))
				if markErr := w.behaviorStore.MarkBehaviorProfileProjectionFailed(ctx, item.UserID, item.EventID, next, err.Error()); markErr != nil {
					return dispatched, markErr
				}
				dispatchErr = errors.Join(dispatchErr, err)
				continue
			}
			inframetrics.ObserveProfileWorker(item.OccurredAt, !applied, nil)
			if err := w.behaviorStore.MarkBehaviorProfileProjectionDispatched(ctx, item.UserID, item.EventID, w.now().UTC()); err != nil {
				return dispatched, err
			}

			dispatched++
		}
	}
	return dispatched, dispatchErr
}

func (w *ProfileOutboxWorker) recordViewOutcome(ctx context.Context, item BehaviorProfileProjectionItem) error {
	if item.RequestID == "" || item.Scene != domainrecommendation.RecommendationRequestLogScene || w.outcomes == nil {
		return nil
	}
	outcome, err := domainrecommendation.NewOutcomeWithRecordedAt(
		domainrecommendation.ViewOutcomeID(item.UserID, item.EventID),
		item.RequestID,
		item.UserID,
		item.VideoID,
		item.EventType,
		item.OccurredAt,
		recordedAtForOutcome(item.RecordedAt),
	)
	if err != nil {
		return err
	}
	recorded, attributed, err := w.outcomes.VerifyAndSaveOutcome(ctx, outcome, 0)
	if err != nil {
		return err
	}
	if !attributed {
		inframetrics.ObserveRecommendationInvalidAttribution(outcome.OutcomeType)
	} else if recorded {
		inframetrics.ObserveRecommendationOutcome(outcome.OutcomeType)
	}
	return nil
}

func isTerminalOutcomeValidationError(err error) bool {
	return errors.Is(err, domainrecommendation.ErrInvalidRequestLog)
}

func feedbackOccurredAt(feedback *domainrecommendation.Feedback) time.Time {
	if feedback == nil {
		return time.Time{}
	}
	return feedback.CreatedAt
}

func profileOutboxRetryDelay(attempts int) time.Duration {
	if attempts < 1 {
		attempts = 1
	}
	delay := time.Second << min(attempts-1, 6)
	if delay > maxProfileOutboxRetryDelay {
		return maxProfileOutboxRetryDelay
	}
	return delay
}
