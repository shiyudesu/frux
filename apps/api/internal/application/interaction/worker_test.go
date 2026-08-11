package applicationinteraction

import (
	"context"
	"errors"
	domaininteraction "github.com/shiyudesu/frux/internal/domain/interaction"
	domainrecommendation "github.com/shiyudesu/frux/internal/domain/recommendation"
	"strings"
	"testing"
	"time"
)

type acceptedActionEventRepositoryStub struct {
	err    error
	events []*domaininteraction.AcceptedActionEvent
}

type recommendationOutcomeRecorderStub struct {
	outcomes map[string]*domainrecommendation.Outcome
	err      error
	valid    bool
	pending  bool
	now      time.Time
}

type recommendationActionOutcomeStoreStub struct {
	acceptedActionEventRepositoryStub
	items      []RecommendationActionOutcomeItem
	dispatched map[string]bool
	failed     map[string]time.Time
	attempts   map[string]int
}

func (s *recommendationActionOutcomeStoreStub) ClaimRecommendationActionOutcomes(_ context.Context, _ int, now, _ time.Time) ([]RecommendationActionOutcomeItem, error) {
	items := make([]RecommendationActionOutcomeItem, 0, len(s.items))
	for _, item := range s.items {
		if s.dispatched[item.EventID] || (s.failed != nil && now.Before(s.failed[item.EventID])) {
			continue
		}
		if s.attempts == nil {
			s.attempts = map[string]int{}
		}
		s.attempts[item.EventID]++
		item.Attempts = s.attempts[item.EventID]
		items = append(items, item)
	}
	return items, nil
}

func (s *recommendationActionOutcomeStoreStub) MarkRecommendationActionOutcomeDispatched(_ context.Context, eventID string, _ time.Time) error {
	if s.dispatched == nil {
		s.dispatched = map[string]bool{}
	}
	s.dispatched[eventID] = true
	return nil
}

func (s *recommendationActionOutcomeStoreStub) MarkRecommendationActionOutcomeFailed(_ context.Context, eventID string, availableAt time.Time, _ string) error {
	if s.failed == nil {
		s.failed = map[string]time.Time{}
	}
	s.failed[eventID] = availableAt
	return nil
}

func (r *recommendationOutcomeRecorderStub) VerifyAndSaveOutcome(_ context.Context, outcome *domainrecommendation.Outcome, _ int64) (bool, bool, error) {
	if r.err != nil {
		return false, false, r.err
	}
	if r.pending && domainrecommendation.OutcomeAttributionPending(outcome.RecordedAt, r.now) {
		return false, false, domainrecommendation.ErrOutcomeAttributionPending
	}
	if !r.valid {
		return false, false, nil
	}
	if r.outcomes == nil {
		r.outcomes = map[string]*domainrecommendation.Outcome{}
	}
	if r.outcomes[outcome.ID] != nil {
		return false, true, nil
	}
	r.outcomes[outcome.ID] = outcome
	return true, true, nil
}

func (r *acceptedActionEventRepositoryStub) PersistAcceptedActionEvent(_ context.Context, event *domaininteraction.AcceptedActionEvent) error {
	r.events = append(r.events, event)
	return r.err
}

func TestActionWorkerClassifiesMissingVideoAsTerminal(t *testing.T) {
	repo := &acceptedActionEventRepositoryStub{err: domaininteraction.ErrVideoNotFound}
	worker := NewActionWorker(repo, nil)

	err := worker.HandleActionChanged(context.Background(), validActionChangedEvent())
	if !IsTerminalActionEventError(err) {
		t.Fatalf("missing video should be terminal, got %v", err)
	}
	if len(repo.events) != 1 {
		t.Fatalf("expected one persistence attempt, got %d", len(repo.events))
	}
}

func TestActionWorkerClassifiesInvalidEventAsTerminal(t *testing.T) {
	repo := &acceptedActionEventRepositoryStub{}
	worker := NewActionWorker(repo, nil)
	event := validActionChangedEvent()
	event.EventID = ""

	err := worker.HandleActionChanged(context.Background(), event)
	if !IsTerminalActionEventError(err) {
		t.Fatalf("invalid event should be terminal, got %v", err)
	}
	if len(repo.events) != 0 {
		t.Fatalf("invalid event reached persistence: %+v", repo.events)
	}
}

func TestActionWorkerLeavesTransientPersistenceFailureRetryable(t *testing.T) {
	transient := errors.New("database unavailable")
	repo := &acceptedActionEventRepositoryStub{err: transient}
	worker := NewActionWorker(repo, nil)

	err := worker.HandleActionChanged(context.Background(), validActionChangedEvent())
	if !errors.Is(err, transient) {
		t.Fatalf("expected transient error, got %v", err)
	}
	if IsTerminalActionEventError(err) {
		t.Fatalf("transient error was classified terminal: %v", err)
	}
}

func TestActionWorkerAcknowledgesRepositoryNoOp(t *testing.T) {
	repo := &acceptedActionEventRepositoryStub{}
	worker := NewActionWorker(repo, nil)

	if err := worker.HandleActionChanged(context.Background(), validActionChangedEvent()); err != nil {
		t.Fatalf("duplicate or stale repository no-op should be acknowledged: %v", err)
	}
}

func TestActionWorkerClassifiesEventConflictAsTerminal(t *testing.T) {
	repo := &acceptedActionEventRepositoryStub{err: domaininteraction.ErrActionEventConflict}
	worker := NewActionWorker(repo, nil)

	err := worker.HandleActionChanged(context.Background(), validActionChangedEvent())
	if !IsTerminalActionEventError(err) {
		t.Fatalf("event conflict should be terminal, got %v", err)
	}
}

func TestActionWorkerRetriesOutcomeProjectionWithoutRejectingDurableAction(t *testing.T) {
	repo := &acceptedActionEventRepositoryStub{}
	outcomes := &recommendationOutcomeRecorderStub{err: errors.New("outcome storage unavailable"), valid: true}
	worker := NewActionWorker(repo, nil, WithRecommendationOutcomeRecorder(outcomes))
	event := validActionChangedEvent()
	event.RecommendationRequestID = "recommendation-request"

	err := worker.HandleActionChanged(context.Background(), event)
	if err == nil || IsTerminalActionEventError(err) {
		t.Fatalf("outcome failure should be retryable after the action fact commits: %v", err)
	}

	if len(repo.events) != 1 {
		t.Fatalf("durable action was not committed before outcome failure: %#v", repo.events)
	}
	outcomes.err = nil
	if err := worker.HandleActionChanged(context.Background(), event); err != nil {
		t.Fatalf("outcome retry failed: %v", err)
	}
	if outcome := outcomes.outcomes["action:event-1"]; outcome == nil || outcome.RequestID != "recommendation-request" || outcome.OutcomeType != "like" {
		t.Fatalf("saved outcome = %#v", outcome)
	}
}

func TestActionWorkerAcknowledgesDurableActionBeforePendingAttribution(t *testing.T) {
	now := time.Date(2026, 7, 27, 2, 0, 0, 0, time.UTC)
	repo := &recommendationActionOutcomeStoreStub{items: []RecommendationActionOutcomeItem{{
		EventID: "pending-after-handoff", UserID: 7, VideoID: 11, ActionType: domaininteraction.ActionTypeLike,
		Active: true, RecommendationRequestID: "recommendation-request", OccurredAt: now,
	}}}
	outcomes := &recommendationOutcomeRecorderStub{pending: true, now: now}
	worker := NewActionWorker(repo, nil, WithRecommendationOutcomeRecorder(outcomes))
	worker.now = func() time.Time { return now }
	event := validActionChangedEvent()
	event.EventID = "pending-after-handoff"
	event.RecommendationRequestID = "recommendation-request"
	event.OccurredAt = now

	if err := worker.HandleActionChanged(context.Background(), event); err != nil {
		t.Fatalf("durably handed-off action should acknowledge delivery: %v", err)
	}
	if len(repo.events) != 1 || len(outcomes.outcomes) != 0 || len(repo.failed) != 0 {
		t.Fatalf("MQ path performed attribution instead of handing it off: events=%#v outcomes=%#v failed=%#v", repo.events, outcomes.outcomes, repo.failed)
	}
	if dispatched, err := worker.DispatchRecommendationOutcomesOnce(context.Background()); !errors.Is(err, domainrecommendation.ErrOutcomeAttributionPending) || dispatched != 0 {
		t.Fatalf("outbox did not retain pending attribution: dispatched=%d err=%v", dispatched, err)
	}
	if got := repo.failed["pending-after-handoff"]; !got.Equal(now.Add(time.Second)) {
		t.Fatalf("pending attribution retry = %v, want exponential first delay", got)
	}
}

func TestActionWorkerDispatchesOutcomeFromDurableActionReceipt(t *testing.T) {
	repo := &recommendationActionOutcomeStoreStub{items: []RecommendationActionOutcomeItem{{
		EventID: "durable-action", UserID: 7, VideoID: 11, ActionType: domaininteraction.ActionTypeFavorite,
		Active: true, RecommendationRequestID: "recommendation-request", OccurredAt: time.Now().UTC(),
	}}}
	outcomes := &recommendationOutcomeRecorderStub{valid: true}
	worker := NewActionWorker(repo, nil, WithRecommendationOutcomeRecorder(outcomes))

	if dispatched, err := worker.DispatchRecommendationOutcomesOnce(context.Background()); err != nil || dispatched != 1 {
		t.Fatalf("dispatch durable outcome = %d, %v", dispatched, err)
	}
	if !repo.dispatched["durable-action"] || outcomes.outcomes["action:durable-action"] == nil {
		t.Fatalf("durable action outcome was not marked and recorded: %#v %#v", repo.dispatched, outcomes.outcomes)
	}
}

func TestActionWorkerBoundsActionOutcomeIDAndDoesNotRetryInvalidOutcome(t *testing.T) {
	now := time.Date(2026, 7, 27, 2, 0, 0, 0, time.UTC)
	eventID := strings.Repeat("a", 128)
	store := &recommendationActionOutcomeStoreStub{items: []RecommendationActionOutcomeItem{{
		EventID: eventID, UserID: 7, VideoID: 11, ActionType: domaininteraction.ActionTypeFavorite,
		Active: true, RecommendationRequestID: "recommendation-request", OccurredAt: now,
	}, {
		EventID: "invalid-outcome", UserID: 7, VideoID: 11, ActionType: "invalid",
		Active: true, RecommendationRequestID: "recommendation-request", OccurredAt: now,
	}}}
	outcomes := &recommendationOutcomeRecorderStub{valid: true}
	worker := NewActionWorker(store, nil, WithRecommendationOutcomeRecorder(outcomes))
	worker.now = func() time.Time { return now }

	if dispatched, err := worker.DispatchRecommendationOutcomesOnce(context.Background()); err != nil || dispatched != 2 {
		t.Fatalf("bounded/invalid outcome dispatch = %d, %v", dispatched, err)
	}
	outcomeID := domainrecommendation.OutcomeID("action", eventID)
	if len(outcomeID) > 128 || outcomes.outcomes[outcomeID] == nil {
		t.Fatalf("bounded action outcome = %#v", outcomes.outcomes)
	}
	if !store.dispatched["invalid-outcome"] || len(store.failed) != 0 {
		t.Fatalf("deterministic invalid outcome poisoned retry outbox: dispatched=%#v failed=%#v", store.dispatched, store.failed)
	}
}

func TestActionOutcomeOutboxRetriesPendingAttributionUntilLateEvidenceArrives(t *testing.T) {
	now := time.Date(2026, 7, 27, 2, 0, 0, 0, time.UTC)
	store := &recommendationActionOutcomeStoreStub{items: []RecommendationActionOutcomeItem{{
		EventID: "late-evidence", UserID: 7, VideoID: 11, ActionType: domaininteraction.ActionTypeLike,
		Active: true, RecommendationRequestID: "recommendation-request", OccurredAt: now,
	}}}
	outcomes := &recommendationOutcomeRecorderStub{pending: true, now: now}
	worker := NewActionWorker(store, nil, WithRecommendationOutcomeRecorder(outcomes))
	current := now
	worker.now = func() time.Time { return current }

	if dispatched, err := worker.DispatchRecommendationOutcomesOnce(context.Background()); !errors.Is(err, domainrecommendation.ErrOutcomeAttributionPending) || dispatched != 0 {
		t.Fatalf("pending attribution dispatch=%d err=%v", dispatched, err)
	}
	if got := store.failed["late-evidence"]; !got.Equal(now.Add(time.Second)) || store.dispatched["late-evidence"] {
		t.Fatalf("pending attribution was not backoff-retried: failed=%v dispatched=%v", got, store.dispatched)
	}

	current = current.Add(time.Second)
	outcomes.pending = false
	outcomes.valid = true
	if dispatched, err := worker.DispatchRecommendationOutcomesOnce(context.Background()); err != nil || dispatched != 1 {
		t.Fatalf("late evidence outcome dispatch=%d err=%v", dispatched, err)
	}
	if !store.dispatched["late-evidence"] || outcomes.outcomes["action:late-evidence"] == nil {
		t.Fatalf("late evidence did not become an outcome: dispatched=%#v outcomes=%#v", store.dispatched, outcomes.outcomes)
	}
}

func TestActionOutcomeOutboxTerminatesExpiredForgedAttribution(t *testing.T) {
	now := time.Date(2026, 7, 27, 2, 0, 0, 0, time.UTC)
	store := &recommendationActionOutcomeStoreStub{items: []RecommendationActionOutcomeItem{{
		EventID: "expired-forged", UserID: 7, VideoID: 11, ActionType: domaininteraction.ActionTypeFavorite,
		Active: true, RecommendationRequestID: "forged-recommendation-request",
		OccurredAt: now.Add(-domainrecommendation.OutcomeAttributionEvidenceWindow),
	}}}
	outcomes := &recommendationOutcomeRecorderStub{pending: true, now: now}
	worker := NewActionWorker(store, nil, WithRecommendationOutcomeRecorder(outcomes))
	worker.now = func() time.Time { return now }

	if dispatched, err := worker.DispatchRecommendationOutcomesOnce(context.Background()); err != nil || dispatched != 1 {
		t.Fatalf("expired forged attribution dispatch=%d err=%v", dispatched, err)
	}
	if !store.dispatched["expired-forged"] || len(store.failed) != 0 || len(outcomes.outcomes) != 0 {
		t.Fatalf("expired forged attribution was retried or recorded: dispatched=%#v failed=%#v outcomes=%#v", store.dispatched, store.failed, outcomes.outcomes)
	}
}

func TestActionWorkerSkipsForgedRecommendationAttributionAfterPersistingFacts(t *testing.T) {
	repo := &acceptedActionEventRepositoryStub{}
	outcomes := &recommendationOutcomeRecorderStub{}
	worker := NewActionWorker(repo, nil, WithRecommendationOutcomeRecorder(outcomes))

	for _, actionType := range []string{domaininteraction.ActionTypeLike, domaininteraction.ActionTypeFavorite} {
		event := validActionChangedEvent()
		event.EventID = "forged-" + actionType
		event.ActionType = actionType
		event.RecommendationRequestID = "forged-recommendation-request"
		if err := worker.HandleActionChanged(context.Background(), event); err != nil {
			t.Fatalf("accept %s with forged recommendation header: %v", actionType, err)
		}
	}

	if len(repo.events) != 2 {
		t.Fatalf("forged attribution rolled back accepted interactions: %#v", repo.events)
	}
	if len(outcomes.outcomes) != 0 {
		t.Fatalf("forged attribution created recommendation outcomes: %#v", outcomes.outcomes)
	}
}

func validActionChangedEvent() *ActionChangedEvent {
	return &ActionChangedEvent{
		EventID:        "event-1",
		UserID:         7,
		VideoID:        11,
		ActionType:     domaininteraction.ActionTypeLike,
		Active:         true,
		IdempotencyKey: "request-1",
		Version:        1,
		OccurredAt:     time.Now().UTC(),
	}
}
