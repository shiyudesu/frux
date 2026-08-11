package applicationrecommendation

import (
	"context"
	"errors"
	applicationexposure "github.com/shiyudesu/frux/internal/application/exposure"
	domainrecommendation "github.com/shiyudesu/frux/internal/domain/recommendation"
	"strconv"
	"strings"
	"testing"
	"time"
)

type retryingBehaviorProjectionRepo struct {
	stored       map[string]bool
	profileCalls int
	failures     int
	outcomes     map[string]*domainrecommendation.Outcome
	evidence     map[string]bool
}

type windowedBehaviorOutcomeRepo struct {
	retryingBehaviorProjectionRepo
	servedAt  time.Time
	expiresAt time.Time
}

func (r *windowedBehaviorOutcomeRepo) VerifyAndSaveOutcome(_ context.Context, outcome *domainrecommendation.Outcome, _ int64) (bool, bool, error) {
	if outcome == nil || outcome.RecordedAt.Before(r.servedAt) || !outcome.RecordedAt.Before(r.expiresAt) {
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

func (r *retryingBehaviorProjectionRepo) ApplyBehaviorEvent(_ context.Context, event *applicationexposure.ViewEventRecordedEvent) (bool, error) {
	if r.stored == nil {
		r.stored = map[string]bool{}
	}
	key := behaviorEventKey(event.UserID, event.EventID)
	if r.stored[key] {
		return false, nil
	}
	r.stored[key] = true
	return true, nil
}

func (*retryingBehaviorProjectionRepo) LoadProfileFeature(context.Context, int64) (ProfileFeature, bool, error) {
	return ProfileFeature{Vector: []float64{1}, AuthorID: 9}, true, nil
}

func (r *retryingBehaviorProjectionRepo) ApplyProfileEvent(_ context.Context, event *domainrecommendation.ProfileEvent) (*domainrecommendation.UserInterestProfile, bool, error) {
	r.profileCalls++
	if r.failures > 0 {
		r.failures--
		return nil, false, errors.New("profile storage unavailable")
	}
	return domainrecommendation.EmptyUserInterestProfile(event.UserID, event.OccurredAt), true, nil
}

func (r *retryingBehaviorProjectionRepo) VerifyAndSaveOutcome(_ context.Context, outcome *domainrecommendation.Outcome, _ int64) (bool, bool, error) {
	if !r.evidence[outcome.RequestID+":"+strconv.FormatInt(outcome.VideoID, 10)] {
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

func TestBehaviorWorkerRetriesProjectionAfterBehaviorFactWasStored(t *testing.T) {
	now := time.Date(2026, 7, 27, 4, 0, 0, 0, time.UTC)
	repo := &retryingBehaviorProjectionRepo{failures: 1}
	worker := NewBehaviorEventWorker(repo, nil)
	duration := 1_000
	event := &applicationexposure.ViewEventRecordedEvent{
		EventID: "view-1", UserID: 7, VideoID: 11, EventType: "progress",
		PositionMs: 800, WatchMs: 800, DurationMs: &duration, OccurredAt: now,
	}
	if err := worker.Handle(context.Background(), event); err == nil {
		t.Fatal("first projection failure should request redelivery")
	}
	if err := worker.Handle(context.Background(), event); err != nil {
		t.Fatalf("redelivery did not retry projection after duplicate fact: %v", err)
	}
	if len(repo.stored) != 1 || repo.profileCalls != 2 {
		t.Fatalf("behavior/profile attempts = stored:%d calls:%d", len(repo.stored), repo.profileCalls)
	}
}

func TestBehaviorWorkerRecordsOutcomesOnlyForNormalizedRecommendViews(t *testing.T) {
	now := time.Date(2026, 7, 27, 5, 0, 0, 0, time.UTC)
	repo := &retryingBehaviorProjectionRepo{evidence: map[string]bool{"request-2:12": true}}
	worker := NewBehaviorEventWorker(repo, nil)

	for _, event := range []*applicationexposure.ViewEventRecordedEvent{
		{
			EventID: "timeline-view", UserID: 7, VideoID: 11, Scene: " timeline ", RequestID: "request-1",
			EventType: "complete", Completed: true, OccurredAt: now,
		},
		{
			EventID: "recommend-view", UserID: 7, VideoID: 12, Scene: " ReCoMmEnD ", RequestID: "request-2",
			EventType: "complete", Completed: true, OccurredAt: now,
		},
	} {
		if err := worker.Handle(context.Background(), event); err != nil {
			t.Fatalf("handle %s: %v", event.EventID, err)
		}
	}

	if len(repo.outcomes) != 1 || repo.outcomes[domainrecommendation.ViewOutcomeID(7, "recommend-view")] == nil {
		t.Fatalf("outcomes = %#v, want only normalized recommend view", repo.outcomes)
	}
	if repo.profileCalls != 2 {
		t.Fatalf("profile projection should retain supported non-recommend scenes, calls=%d", repo.profileCalls)
	}
}

func TestBehaviorWorkerDoesNotAttributeForgedViewRequestID(t *testing.T) {
	now := time.Date(2026, 7, 27, 5, 0, 0, 0, time.UTC)
	repo := &retryingBehaviorProjectionRepo{evidence: map[string]bool{}}
	worker := NewBehaviorEventWorker(repo, nil)

	if err := worker.Handle(context.Background(), &applicationexposure.ViewEventRecordedEvent{
		EventID: "forged-view", UserID: 7, VideoID: 12, Scene: "recommend", RequestID: "forged-request",
		EventType: "complete", Completed: true, OccurredAt: now,
	}); err != nil {
		t.Fatalf("forged view should remain a behavior fact without an attributed outcome: %v", err)
	}
	if len(repo.outcomes) != 0 {
		t.Fatalf("forged view request was attributed: %#v", repo.outcomes)
	}
}

func TestBehaviorWorkerUsesServerRecordedAtForViewAttribution(t *testing.T) {
	servedAt := time.Date(2026, 7, 27, 5, 0, 0, 0, time.UTC)
	repo := &windowedBehaviorOutcomeRepo{
		servedAt: servedAt, expiresAt: servedAt.Add(time.Minute),
	}
	worker := NewBehaviorEventWorker(repo, nil)

	tests := []struct {
		name       string
		eventID    string
		occurredAt time.Time
		recordedAt time.Time
		wantSaved  bool
	}{
		{
			name:       "bounded skewed client occurrence is accepted when server receipt is in window",
			eventID:    "skewed-but-served",
			occurredAt: servedAt.Add(-23 * time.Hour),
			recordedAt: servedAt.Add(30 * time.Second),
			wantSaved:  true,
		},
		{
			name:       "in-window client occurrence is rejected when server receipt is late",
			eventID:    "late-server-receipt",
			occurredAt: servedAt.Add(30 * time.Second),
			recordedAt: servedAt.Add(time.Minute),
			wantSaved:  false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := worker.Handle(context.Background(), &applicationexposure.ViewEventRecordedEvent{
				EventID: test.eventID, UserID: 7, VideoID: 12, Scene: "recommend", RequestID: "request-window",
				EventType: "complete", Completed: true, OccurredAt: test.occurredAt, RecordedAt: test.recordedAt,
			}); err != nil {
				t.Fatal(err)
			}
			outcome := repo.outcomes[domainrecommendation.ViewOutcomeID(7, test.eventID)]
			if (outcome != nil) != test.wantSaved {
				t.Fatalf("outcome = %#v, want saved=%v", outcome, test.wantSaved)
			}
			if outcome != nil && (!outcome.OccurredAt.Equal(test.occurredAt) || !outcome.RecordedAt.Equal(test.recordedAt)) {
				t.Fatalf("outcome lost distinct occurrence/receipt timestamps: %#v", outcome)
			}
		})
	}
}

func TestBehaviorWorkerBoundsViewOutcomeIDAndReplaysIt(t *testing.T) {
	now := time.Date(2026, 7, 27, 5, 0, 0, 0, time.UTC)
	eventID := strings.Repeat("v", 128)
	repo := &retryingBehaviorProjectionRepo{evidence: map[string]bool{"request-2:12": true}}
	worker := NewBehaviorEventWorker(repo, nil)
	event := &applicationexposure.ViewEventRecordedEvent{
		EventID: eventID, UserID: 7, VideoID: 12, Scene: "recommend", RequestID: "request-2",
		EventType: "complete", Completed: true, OccurredAt: now,
	}
	if err := worker.Handle(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if err := worker.Handle(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	outcomeID := domainrecommendation.ViewOutcomeID(event.UserID, eventID)
	if len(outcomeID) > 128 || len(repo.outcomes) != 1 || repo.outcomes[outcomeID] == nil {
		t.Fatalf("bounded replayed view outcome = %#v", repo.outcomes)
	}
}

func TestBehaviorWorkerScopesSharedViewEventIDsByUser(t *testing.T) {
	now := time.Date(2026, 7, 27, 5, 0, 0, 0, time.UTC)
	repo := &retryingBehaviorProjectionRepo{evidence: map[string]bool{"request-2:12": true}}
	worker := NewBehaviorEventWorker(repo, nil)

	for _, userID := range []int64{7, 8} {
		if err := worker.Handle(context.Background(), &applicationexposure.ViewEventRecordedEvent{
			EventID: "shared-client-view", UserID: userID, VideoID: 12, Scene: "recommend", RequestID: "request-2",
			EventType: "complete", Completed: true, OccurredAt: now,
		}); err != nil {
			t.Fatalf("handle user %d: %v", userID, err)
		}
	}

	first := domainrecommendation.ViewOutcomeID(7, "shared-client-view")
	second := domainrecommendation.ViewOutcomeID(8, "shared-client-view")
	if first == second || len(first) > 128 || len(second) > 128 ||
		repo.outcomes[first] == nil || repo.outcomes[second] == nil || len(repo.stored) != 2 {
		t.Fatalf("shared view event identity was not user scoped: outcomes=%#v stored=%#v", repo.outcomes, repo.stored)
	}
}

type durableBehaviorOutboxRepo struct {
	retryingBehaviorProjectionRepo
	item       BehaviorProfileProjectionItem
	available  time.Time
	dispatched bool
	failed     time.Time
}

func (r *durableBehaviorOutboxRepo) ClaimBehaviorProfileProjections(_ context.Context, _ int, now, _ time.Time) ([]BehaviorProfileProjectionItem, error) {
	if r.dispatched || now.Before(r.available) {
		return []BehaviorProfileProjectionItem{}, nil
	}
	r.item.Attempts++
	return []BehaviorProfileProjectionItem{r.item}, nil
}

func (r *durableBehaviorOutboxRepo) MarkBehaviorProfileProjectionDispatched(_ context.Context, _ int64, _ string, _ time.Time) error {
	r.dispatched = true
	return nil
}

func (r *durableBehaviorOutboxRepo) MarkBehaviorProfileProjectionFailed(_ context.Context, _ int64, _ string, availableAt time.Time, _ string) error {
	r.failed = availableAt
	r.available = availableAt
	return nil
}

func TestBehaviorWorkerAcknowledgesDurableHandoffBeforeProfileProjection(t *testing.T) {
	now := time.Date(2026, 7, 27, 6, 0, 0, 0, time.UTC)
	repo := &durableBehaviorOutboxRepo{
		retryingBehaviorProjectionRepo: retryingBehaviorProjectionRepo{failures: 1},
		item: BehaviorProfileProjectionItem{
			EventID: "durable-view", UserID: 7, VideoID: 11, EventType: "progress",
			PositionMs: 800, WatchMs: 800, DurationMs: intPtr(1_000), OccurredAt: now,
		},
		available: now,
	}
	worker := NewBehaviorEventWorker(repo, nil)
	event := &applicationexposure.ViewEventRecordedEvent{
		EventID: "durable-view", UserID: 7, VideoID: 11, EventType: "progress",
		PositionMs: 800, WatchMs: 800, DurationMs: intPtr(1_000), OccurredAt: now,
	}
	if err := worker.Handle(context.Background(), event); err != nil {
		t.Fatalf("durable behavior handoff should acknowledge MQ: %v", err)
	}
	if repo.profileCalls != 0 || len(repo.stored) != 1 {
		t.Fatalf("MQ path projected before handing off: calls=%d stored=%#v", repo.profileCalls, repo.stored)
	}

	profileWorker := NewProfileOutboxWorker(
		NewProfileProjector(repo, WithProfileProjectionNow(func() time.Time { return now })),
		nil,
		nil,
		WithBehaviorProfileOutboxStore(repo),
	)
	current := now
	profileWorker.now = func() time.Time { return current }
	if dispatched, err := profileWorker.DispatchOnce(context.Background()); err == nil || dispatched != 0 {
		t.Fatalf("first durable projection dispatched=%d err=%v", dispatched, err)
	}
	if !repo.failed.Equal(now.Add(time.Second)) {
		t.Fatalf("durable behavior retry was not backoff-scheduled: %v", repo.failed)
	}
	current = current.Add(time.Second)
	if dispatched, err := profileWorker.DispatchOnce(context.Background()); err != nil || dispatched != 1 || !repo.dispatched {
		t.Fatalf("durable behavior retry dispatched=%d err=%v", dispatched, err)
	}
}

func TestProfileOutboxRecordsRecommendOutcomeBeforeMissingEmbeddingRetry(t *testing.T) {
	now := time.Date(2026, 7, 27, 7, 0, 0, 0, time.UTC)
	store := &durableBehaviorOutboxRepo{
		item: BehaviorProfileProjectionItem{
			EventID: "recommend-view-without-embedding", UserID: 7, VideoID: 11,
			Scene: domainrecommendation.RecommendationRequestLogScene, RequestID: "recommendation-request",
			EventType: "complete", Completed: true, RecordedAt: now, OccurredAt: now,
		},
		available: now,
	}
	features := &delayedProfileFeatureRepo{}
	outcomes := &retryingOutcomeRepository{valid: true}
	worker := NewProfileOutboxWorker(
		NewProfileProjector(features, WithProfileProjectionNow(func() time.Time { return now })),
		nil,
		nil,
		WithBehaviorProfileOutboxStore(store),
		WithProfileOutboxOutcomeRepository(outcomes),
	)
	current := now
	worker.now = func() time.Time { return current }

	if dispatched, err := worker.DispatchOnce(context.Background()); !errors.Is(err, domainrecommendation.ErrProfileFeatureUnavailable) || dispatched != 0 {
		t.Fatalf("missing embedding dispatch=%d err=%v", dispatched, err)
	}
	outcomeID := domainrecommendation.ViewOutcomeID(7, "recommend-view-without-embedding")
	if outcomes.outcomes[outcomeID] == nil {
		t.Fatalf("successful view outcome was delayed by unavailable embedding: %#v", outcomes.outcomes)
	}
	if store.dispatched || !store.failed.Equal(now.Add(time.Second)) {
		t.Fatalf("profile projection was not retained with backoff: %#v", store)
	}

	current = current.Add(time.Second)
	features.available = true
	if dispatched, err := worker.DispatchOnce(context.Background()); err != nil || dispatched != 1 || !store.dispatched {
		t.Fatalf("embedding-ready retry dispatched=%d err=%v store=%#v", dispatched, err, store)
	}
	if len(outcomes.outcomes) != 1 {
		t.Fatalf("idempotent outcome retry created duplicates: %#v", outcomes.outcomes)
	}
}

func intPtr(value int) *int {
	return &value
}

func behaviorEventKey(userID int64, eventID string) string {
	return strconv.FormatInt(userID, 10) + ":" + eventID
}
