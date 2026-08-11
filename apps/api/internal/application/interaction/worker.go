package applicationinteraction

import (
	"context"
	"errors"
	"fmt"
	domaininteraction "github.com/shiyudesu/frux/internal/domain/interaction"
	domainrecommendation "github.com/shiyudesu/frux/internal/domain/recommendation"
	inframetrics "github.com/shiyudesu/frux/internal/infra/metrics"
	"time"
)

var ErrTerminalActionEvent = errors.New("terminal action event")

const (
	defaultActionOutcomeBatchSize = 50
	defaultActionOutcomeInterval  = time.Second
	defaultActionOutcomeLease     = 30 * time.Second
	maxActionOutcomeRetryDelay    = time.Minute
)

type ActionEventConsumer interface {
	ConsumeActionChanged(ctx context.Context, handler func(context.Context, *ActionChangedEvent) error) error
}

type ActionWorker struct {
	repo     domaininteraction.AcceptedActionEventRepository
	consumer ActionEventConsumer
	outcomes RecommendationOutcomeRecorder
	now      func() time.Time
	observer ActionConsumerObserver
}

type ActionPersistenceOutcome string

const (
	ActionPersistenceApplied    ActionPersistenceOutcome = "applied"
	ActionPersistenceDuplicate  ActionPersistenceOutcome = "duplicate"
	ActionPersistenceSuperseded ActionPersistenceOutcome = "superseded"
)

type ActionPersistenceOutcomeRepository interface {
	PersistAcceptedActionEventWithOutcome(
		context.Context,
		*domaininteraction.AcceptedActionEvent,
	) (ActionPersistenceOutcome, error)
}

type ActionConsumerObserver interface {
	ObserveActionConsumption(result string)
}

type RecommendationOutcomeRecorder interface {
	// VerifyAndSaveOutcome verifies durable request membership before recording
	// client-supplied recommendation attribution. followedTargetUserID is only
	// set for follow outcomes.
	VerifyAndSaveOutcome(ctx context.Context, outcome *domainrecommendation.Outcome, followedTargetUserID int64) (recorded bool, attributed bool, err error)
}

type RecommendationActionOutcomeItem struct {
	EventID                 string
	UserID                  int64
	VideoID                 int64
	ActionType              string
	Active                  bool
	RecommendationRequestID string
	OccurredAt              time.Time
	Attempts                int
}

// ActionProfileProjectionItem is a durable, idempotent input for the
// recommendation profile projector. It intentionally mirrors the accepted
// action receipt rather than the transient transport delivery.
type ActionProfileProjectionItem struct {
	EventID        string
	UserID         int64
	VideoID        int64
	ActionType     string
	Active         bool
	IdempotencyKey string
	Version        int64
	OccurredAt     time.Time
	Attempts       int
}

type RecommendationActionOutcomeStore interface {
	ClaimRecommendationActionOutcomes(ctx context.Context, limit int, now, leasedUntil time.Time) ([]RecommendationActionOutcomeItem, error)
	MarkRecommendationActionOutcomeDispatched(ctx context.Context, eventID string, dispatchedAt time.Time) error
	MarkRecommendationActionOutcomeFailed(ctx context.Context, eventID string, availableAt time.Time, reason string) error
}

type ActionWorkerOption func(*ActionWorker)

func WithRecommendationOutcomeRecorder(outcomes RecommendationOutcomeRecorder) ActionWorkerOption {
	return func(worker *ActionWorker) {
		worker.outcomes = outcomes
	}
}

func WithActionConsumerObserver(observer ActionConsumerObserver) ActionWorkerOption {
	return func(worker *ActionWorker) {
		worker.observer = observer
	}
}

func NewActionWorker(repo domaininteraction.AcceptedActionEventRepository, consumer ActionEventConsumer, options ...ActionWorkerOption) *ActionWorker {
	worker := &ActionWorker{
		repo:     repo,
		consumer: consumer,
		now:      func() time.Time { return time.Now().UTC() },
	}
	for _, option := range options {
		option(worker)
	}
	return worker
}

func (w *ActionWorker) Start(ctx context.Context) error {
	if w == nil {
		return nil
	}
	if w.consumer != nil {
		if err := w.consumer.ConsumeActionChanged(ctx, w.HandleActionChanged); err != nil {
			return err
		}
	}
	if _, ok := w.repo.(RecommendationActionOutcomeStore); ok && w.outcomes != nil {
		go w.dispatchRecommendationOutcomes(ctx)
	}
	return nil
}

func (w *ActionWorker) HandleActionChanged(ctx context.Context, event *ActionChangedEvent) (resultErr error) {
	start := time.Now()
	defer func() {
		inframetrics.ObserveWorkerJob("interaction_action_changed", time.Since(start), resultErr)
	}()

	if event == nil {
		return terminalActionEvent(domaininteraction.ErrInvalidActionEvent)
	}
	accepted, err := acceptedActionEvent(event)
	if err != nil {
		return terminalActionEvent(err)
	}
	outcome := ActionPersistenceApplied
	var persistErr error
	if outcomeRepo, ok := w.repo.(ActionPersistenceOutcomeRepository); ok {
		outcome, persistErr = outcomeRepo.PersistAcceptedActionEventWithOutcome(ctx, accepted)
	} else {
		persistErr = w.repo.PersistAcceptedActionEvent(ctx, accepted)
	}
	if persistErr != nil {
		err := persistErr
		if errors.Is(err, domaininteraction.ErrVideoNotFound) ||
			errors.Is(err, domaininteraction.ErrActionEventConflict) {
			w.observeActionConsumption("terminal")
			return terminalActionEvent(err)
		}
		w.observeActionConsumption("retryable")
		return err
	}
	w.observeActionConsumption(string(outcome))
	// Production repositories create the outcome handoff in the same
	// transaction as the accepted action. Acknowledge the transport after that
	// transaction commits; the leased outbox owns delayed attribution retries.
	if _, durable := w.repo.(RecommendationActionOutcomeStore); durable {
		return nil
	}
	item := RecommendationActionOutcomeItem{
		EventID: event.EventID, UserID: event.UserID, VideoID: event.VideoID, ActionType: event.ActionType,
		Active: event.Active, RecommendationRequestID: event.RecommendationRequestID, OccurredAt: event.OccurredAt,
	}
	if err := w.recordRecommendationOutcome(ctx, item); err != nil {
		return err
	}
	return nil
}

func (w *ActionWorker) observeActionConsumption(result string) {
	if w != nil && w.observer != nil {
		w.observer.ObserveActionConsumption(result)
	}
}

func (w *ActionWorker) dispatchRecommendationOutcomes(ctx context.Context) {
	ticker := time.NewTicker(defaultActionOutcomeInterval)
	defer ticker.Stop()
	for {
		if _, err := w.DispatchRecommendationOutcomesOnce(ctx); err != nil {
			inframetrics.ObserveWorkerJob("recommendation_action_outcomes", 0, err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (w *ActionWorker) DispatchRecommendationOutcomesOnce(ctx context.Context) (int, error) {
	if w == nil || w.outcomes == nil {
		return 0, nil
	}
	store, ok := w.repo.(RecommendationActionOutcomeStore)
	if !ok {
		return 0, nil
	}
	now := w.now().UTC()
	items, err := store.ClaimRecommendationActionOutcomes(ctx, defaultActionOutcomeBatchSize, now, now.Add(defaultActionOutcomeLease))
	if err != nil {
		return 0, err
	}
	dispatched := 0
	var dispatchErr error
	for _, item := range items {
		if err := w.recordRecommendationOutcome(ctx, item); err != nil {
			if IsTerminalActionEventError(err) {
				if markErr := store.MarkRecommendationActionOutcomeDispatched(ctx, item.EventID, w.now().UTC()); markErr != nil {
					return dispatched, markErr
				}
				dispatched++
				continue
			}
			next := now.Add(actionOutcomeRetryDelay(item.Attempts))
			if markErr := store.MarkRecommendationActionOutcomeFailed(ctx, item.EventID, next, err.Error()); markErr != nil {
				return dispatched, markErr
			}
			dispatchErr = errors.Join(dispatchErr, err)
			continue
		}
		dispatched++
	}
	return dispatched, dispatchErr
}

func (w *ActionWorker) recordRecommendationOutcome(ctx context.Context, item RecommendationActionOutcomeItem) error {
	if !item.Active || item.RecommendationRequestID == "" || w.outcomes == nil {
		return nil
	}
	outcome, err := domainrecommendation.NewOutcome(
		domainrecommendation.OutcomeID("action", item.EventID),
		item.RecommendationRequestID,
		item.UserID,
		item.VideoID,
		item.ActionType,
		item.OccurredAt,
	)
	if err != nil {
		return terminalActionEvent(err)
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
	if store, ok := w.repo.(RecommendationActionOutcomeStore); ok {
		return store.MarkRecommendationActionOutcomeDispatched(ctx, item.EventID, w.now().UTC())
	}
	return nil
}

func actionOutcomeRetryDelay(attempts int) time.Duration {
	if attempts < 1 {
		attempts = 1
	}
	delay := time.Second << min(attempts-1, 6)
	if delay > maxActionOutcomeRetryDelay {
		return maxActionOutcomeRetryDelay
	}
	return delay
}

func IsTerminalActionEventError(err error) bool {
	return errors.Is(err, ErrTerminalActionEvent)
}

func terminalActionEvent(err error) error {
	return fmt.Errorf("%w: %w", ErrTerminalActionEvent, err)
}

func acceptedActionEvent(event *ActionChangedEvent) (*domaininteraction.AcceptedActionEvent, error) {
	if event == nil {
		return nil, domaininteraction.ErrInvalidActionEvent
	}
	return domaininteraction.NewAcceptedActionEventWithRecommendation(
		event.EventID,
		event.UserID,
		event.VideoID,
		event.ActionType,
		event.Active,
		event.IdempotencyKey,
		event.RecommendationRequestID,
		event.Version,
		event.OccurredAt,
	)
}
