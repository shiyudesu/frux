package applicationrecommendation

import (
	"context"
	"errors"
	applicationinteraction "github.com/shiyudesu/frux/internal/application/interaction"
	domaininteraction "github.com/shiyudesu/frux/internal/domain/interaction"
	domainrecommendation "github.com/shiyudesu/frux/internal/domain/recommendation"
	domainrelation "github.com/shiyudesu/frux/internal/domain/relation"
	"strings"
	"testing"
	"time"
)

type feedbackIsolationRepo struct {
	recallTestRepo
	saved *domainrecommendation.Feedback
}

type memoryFollowProfileOutbox struct {
	item       domainrelation.FollowProjectionOutboxItem
	available  time.Time
	leased     time.Time
	dispatched bool
	failures   int
}

func (s *memoryFollowProfileOutbox) ClaimFollowProfileOutbox(_ context.Context, _ int, now, leasedUntil time.Time) ([]domainrelation.FollowProjectionOutboxItem, error) {
	if s.dispatched || now.Before(s.available) || now.Before(s.leased) {
		return []domainrelation.FollowProjectionOutboxItem{}, nil
	}
	s.item.Attempts++
	s.leased = leasedUntil
	item := s.item
	return []domainrelation.FollowProjectionOutboxItem{item}, nil
}

func (s *memoryFollowProfileOutbox) MarkFollowProfileOutboxDispatched(_ context.Context, _ int64, _ time.Time) error {
	s.dispatched = true
	s.leased = time.Time{}
	return nil
}

func (s *memoryFollowProfileOutbox) MarkFollowProfileOutboxFailed(_ context.Context, _ int64, availableAt time.Time, _ string) error {
	s.failures++
	s.available = availableAt
	s.leased = time.Time{}
	return nil
}

type retryingOutcomeRepository struct {
	err              error
	outcomes         map[string]*domainrecommendation.Outcome
	valid            bool
	followVideoOwner int64
	pending          bool
	now              time.Time
}

func (r *retryingOutcomeRepository) VerifyAndSaveOutcome(_ context.Context, outcome *domainrecommendation.Outcome, followedTargetUserID int64) (bool, bool, error) {
	if r.err != nil {
		return false, false, r.err
	}
	if r.pending && domainrecommendation.OutcomeAttributionPending(outcome.RecordedAt, r.now) {
		return false, false, domainrecommendation.ErrOutcomeAttributionPending
	}
	if !r.valid {
		return false, false, nil
	}
	if outcome.OutcomeType == "follow" && r.followVideoOwner > 0 && r.followVideoOwner != followedTargetUserID {
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

func (r *feedbackIsolationRepo) SaveFeedback(_ context.Context, feedback *domainrecommendation.Feedback) (*domainrecommendation.Feedback, bool, error) {
	saved := *feedback
	saved.ID = 7
	r.saved = &saved
	return &saved, false, nil
}

func (*feedbackIsolationRepo) GetFeedbackVideo(context.Context, int64) (*FeedbackVideo, error) {
	return &FeedbackVideo{VideoID: 9, AuthorID: 7}, nil
}
func (*feedbackIsolationRepo) HasServedCandidateEvidence(context.Context, int64, string, int64, time.Time) (bool, error) {
	return true, nil
}

// These methods make the repository a projection repository whose projection
// would fail if the request path ever attempted to call it.
func (r *feedbackIsolationRepo) LoadProfileFeature(context.Context, int64) (ProfileFeature, bool, error) {
	return ProfileFeature{}, false, errors.New("projection unavailable")
}
func (r *feedbackIsolationRepo) ApplyProfileEvent(context.Context, *domainrecommendation.ProfileEvent) (*domainrecommendation.UserInterestProfile, bool, error) {
	return nil, false, errors.New("projection unavailable")
}

func TestSubmitFeedbackDoesNotFailAfterDurableSaveWhenProjectionIsUnavailable(t *testing.T) {
	repo := &feedbackIsolationRepo{}
	service := New(repo, WithNow(func() time.Time { return time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC) }))

	result, err := service.SubmitFeedback(context.Background(), FeedbackInput{
		UserID: 42, VideoID: 9, RequestID: "request-1",
		FeedbackType: domainrecommendation.FeedbackTypeNotInterested, IdempotencyKey: "feedback-1",
	})
	if err != nil || result == nil || result.Feedback == nil || result.Feedback.ID != 7 {
		t.Fatalf("durably saved feedback was made failure-shaped: result=%#v err=%v", result, err)
	}
}

type retryingProfileRepo struct {
	feature  ProfileFeature
	failures int
	eventIDs []string
}

type delayedProfileFeatureRepo struct {
	available bool
	eventIDs  []string
}

func (r *delayedProfileFeatureRepo) LoadProfileFeature(context.Context, int64) (ProfileFeature, bool, error) {
	if !r.available {
		return ProfileFeature{}, false, nil
	}
	return ProfileFeature{Vector: []float64{1}, AuthorID: 3}, true, nil
}

func (r *delayedProfileFeatureRepo) ApplyProfileEvent(_ context.Context, event *domainrecommendation.ProfileEvent) (*domainrecommendation.UserInterestProfile, bool, error) {
	r.eventIDs = append(r.eventIDs, event.SourceEventID)
	return domainrecommendation.EmptyUserInterestProfile(event.UserID, event.OccurredAt), true, nil
}

func (r *retryingProfileRepo) LoadProfileFeature(context.Context, int64) (ProfileFeature, bool, error) {
	return r.feature, true, nil
}
func (r *retryingProfileRepo) ApplyProfileEvent(_ context.Context, event *domainrecommendation.ProfileEvent) (*domainrecommendation.UserInterestProfile, bool, error) {
	if r.failures > 0 {
		r.failures--
		return nil, false, errors.New("database unavailable")
	}
	r.eventIDs = append(r.eventIDs, event.SourceEventID)
	return domainrecommendation.EmptyUserInterestProfile(event.UserID, event.OccurredAt), true, nil
}

type memoryFeedbackProfileOutbox struct {
	item       domainrecommendation.FeedbackProjectionOutboxItem
	available  time.Time
	leased     time.Time
	dispatched bool
	failures   int
}

type memoryActionProfileOutbox struct {
	item       applicationinteraction.ActionProfileProjectionItem
	available  time.Time
	leased     time.Time
	dispatched bool
	failures   int
}

func (s *memoryActionProfileOutbox) ClaimActionProfileProjections(_ context.Context, _ int, now, leasedUntil time.Time) ([]applicationinteraction.ActionProfileProjectionItem, error) {
	if s.dispatched || now.Before(s.available) || now.Before(s.leased) {
		return []applicationinteraction.ActionProfileProjectionItem{}, nil
	}
	s.item.Attempts++
	s.leased = leasedUntil
	return []applicationinteraction.ActionProfileProjectionItem{s.item}, nil
}
func (s *memoryActionProfileOutbox) MarkActionProfileProjectionDispatched(_ context.Context, _ string, _ time.Time) error {
	s.dispatched = true
	s.leased = time.Time{}
	return nil
}
func (s *memoryActionProfileOutbox) MarkActionProfileProjectionFailed(_ context.Context, _ string, availableAt time.Time, _ string) error {
	s.failures++
	s.available = availableAt
	s.leased = time.Time{}
	return nil
}

func (s *memoryFeedbackProfileOutbox) ClaimFeedbackProfileOutbox(_ context.Context, _ int, now, leasedUntil time.Time) ([]domainrecommendation.FeedbackProjectionOutboxItem, error) {
	if s.dispatched || now.Before(s.available) || now.Before(s.leased) {
		return []domainrecommendation.FeedbackProjectionOutboxItem{}, nil
	}
	s.item.Attempts++
	s.leased = leasedUntil
	item := s.item
	return []domainrecommendation.FeedbackProjectionOutboxItem{item}, nil
}
func (s *memoryFeedbackProfileOutbox) MarkFeedbackProfileOutboxDispatched(_ context.Context, _ int64, _ time.Time) error {
	s.dispatched = true
	s.leased = time.Time{}
	return nil
}
func (s *memoryFeedbackProfileOutbox) MarkFeedbackProfileOutboxFailed(_ context.Context, _ int64, availableAt time.Time, _ string) error {
	s.failures++
	s.available = availableAt
	s.leased = time.Time{}
	return nil
}

func TestProfileOutboxWorkerRetriesFeedbackUntilItIsApplied(t *testing.T) {
	now := time.Date(2026, 7, 27, 1, 0, 0, 0, time.UTC)
	feedback := domainrecommendation.RestoreFeedback(7, 42, 9, "request-1", domainrecommendation.FeedbackTypeNotInterested, "feedback-1", now)
	store := &memoryFeedbackProfileOutbox{
		item:      domainrecommendation.FeedbackProjectionOutboxItem{ID: 1, Feedback: feedback},
		available: now,
	}

	repo := &retryingProfileRepo{feature: ProfileFeature{Vector: []float64{1}, AuthorID: 3}, failures: 1}
	worker := NewProfileOutboxWorker(NewProfileProjector(repo, WithProfileProjectionNow(func() time.Time { return now })), store, nil)
	current := now
	worker.now = func() time.Time { return current }

	if dispatched, err := worker.DispatchOnce(context.Background()); err == nil || dispatched != 0 {
		t.Fatalf("first attempt: dispatched=%d err=%v", dispatched, err)
	}
	if store.failures != 1 || store.dispatched {
		t.Fatalf("failed projection was not retained for retry: %#v", store)
	}
	current = current.Add(time.Second)
	if dispatched, err := worker.DispatchOnce(context.Background()); err != nil || dispatched != 1 {
		t.Fatalf("retry: dispatched=%d err=%v", dispatched, err)
	}
	if !store.dispatched || len(repo.eventIDs) != 1 || repo.eventIDs[0] != "feedback:7" {
		t.Fatalf("feedback signal was not eventually projected with its stable ID: store=%#v ids=%v", store, repo.eventIDs)
	}
}

func TestProfileOutboxWorkerDispatchesReduceAuthorWithoutEmbedding(t *testing.T) {
	now := time.Date(2026, 7, 27, 1, 0, 0, 0, time.UTC)
	feedback := domainrecommendation.RestoreFeedback(
		8, 42, 9, "request-1", domainrecommendation.FeedbackTypeReduceAuthor, "feedback-author", now,
	)
	if err := feedback.SetSuppression(domainrecommendation.SuppressionScopeAuthor, 3, now.Add(14*24*time.Hour)); err != nil {
		t.Fatalf("set suppression: %v", err)
	}
	store := &memoryFeedbackProfileOutbox{
		item:      domainrecommendation.FeedbackProjectionOutboxItem{ID: 2, Feedback: feedback},
		available: now,
	}
	repo := &projectionRepo{}
	worker := NewProfileOutboxWorker(
		NewProfileProjector(repo, WithProfileProjectionNow(func() time.Time { return now })),
		store,
		nil,
	)
	worker.now = func() time.Time { return now }

	if dispatched, err := worker.DispatchOnce(context.Background()); err != nil || dispatched != 1 || !store.dispatched {
		t.Fatalf("dispatch reduce-author profile signal: dispatched=%d err=%v store=%#v", dispatched, err, store)
	}
	if repo.loadCalls != 0 || len(repo.events) != 1 || repo.events[0].NegativeAuthorWeights[3] <= 0 {
		t.Fatalf("reduce-author projection unexpectedly required embedding: loads=%d events=%#v", repo.loadCalls, repo.events)
	}
}

func TestProfileOutboxWorkerRetriesFollowOutcomeWithoutLosingFollowFact(t *testing.T) {
	now := time.Date(2026, 7, 27, 1, 0, 0, 0, time.UTC)
	store := &memoryFollowProfileOutbox{
		item: domainrelation.FollowProjectionOutboxItem{
			ID: 1, EventID: "follow-1", UserID: 42, AuthorID: 9, Active: true, OccurredAt: now,
			RecommendationRequestID: "recommendation-request", RecommendationVideoID: 11,
		},
		available: now,
	}

	projection := &retryingProfileRepo{feature: ProfileFeature{Vector: []float64{1}, AuthorID: 9}}
	outcomes := &retryingOutcomeRepository{err: errors.New("outcome storage unavailable"), valid: true}
	worker := NewProfileOutboxWorker(
		NewProfileProjector(projection, WithProfileProjectionNow(func() time.Time { return now })),
		nil,
		store,
		WithProfileOutboxOutcomeRepository(outcomes),
	)
	current := now
	worker.now = func() time.Time { return current }

	if dispatched, err := worker.DispatchOnce(context.Background()); err == nil || dispatched != 0 || store.dispatched || store.failures != 1 {
		t.Fatalf("first follow outcome attempt dispatched=%d err=%v store=%#v", dispatched, err, store)
	}
	outcomes.err = nil
	current = current.Add(time.Second)
	if dispatched, err := worker.DispatchOnce(context.Background()); err != nil || dispatched != 1 || !store.dispatched {
		t.Fatalf("follow outcome retry dispatched=%d err=%v store=%#v", dispatched, err, store)
	}
	if outcome := outcomes.outcomes["follow:follow-1"]; outcome == nil || outcome.RequestID != "recommendation-request" || outcome.VideoID != 11 {
		t.Fatalf("follow outcome = %#v", outcome)
	}
}

func TestProfileOutboxWorkerBoundsFollowOutcomeIDAndSkipsInvalidRetry(t *testing.T) {
	now := time.Date(2026, 7, 27, 1, 0, 0, 0, time.UTC)
	eventID := strings.Repeat("f", 128)
	store := &memoryFollowProfileOutbox{
		item: domainrelation.FollowProjectionOutboxItem{
			ID: 1, EventID: eventID, UserID: 42, AuthorID: 9, Active: true, OccurredAt: now,
			RecommendationRequestID: "recommendation-request", RecommendationVideoID: 11,
		},
		available: now,
	}
	projection := &retryingProfileRepo{feature: ProfileFeature{Vector: []float64{1}, AuthorID: 9}}
	outcomes := &retryingOutcomeRepository{valid: true}
	worker := NewProfileOutboxWorker(
		NewProfileProjector(projection, WithProfileProjectionNow(func() time.Time { return now })),
		nil, store, WithProfileOutboxOutcomeRepository(outcomes),
	)
	worker.now = func() time.Time { return now }
	if dispatched, err := worker.DispatchOnce(context.Background()); err != nil || dispatched != 1 {
		t.Fatalf("bounded follow outcome dispatch = %d, %v", dispatched, err)
	}
	outcomeID := domainrecommendation.OutcomeID("follow", eventID)
	if len(outcomeID) > 128 || outcomes.outcomes[outcomeID] == nil {
		t.Fatalf("bounded follow outcome = %#v", outcomes.outcomes)
	}

	store.dispatched = false
	store.item.ID = 2
	store.item.EventID = "invalid-follow-outcome"
	store.item.RecommendationVideoID = 0
	store.available = now
	if dispatched, err := worker.DispatchOnce(context.Background()); err != nil || dispatched != 1 || !store.dispatched || store.failures != 0 {
		t.Fatalf("invalid follow outcome retried instead of dispatching: dispatched=%d err=%v store=%#v", dispatched, err, store)
	}
}

func TestProfileOutboxWorkerRetriesPendingFollowAttributionAfterProjectingSignal(t *testing.T) {
	now := time.Date(2026, 7, 27, 1, 0, 0, 0, time.UTC)
	store := &memoryFollowProfileOutbox{
		item: domainrelation.FollowProjectionOutboxItem{
			ID: 3, EventID: "late-follow", UserID: 42, AuthorID: 9, Active: true, OccurredAt: now,
			RecommendationRequestID: "recommendation-request", RecommendationVideoID: 11,
		},
		available: now,
	}
	projection := &retryingProfileRepo{feature: ProfileFeature{Vector: []float64{1}, AuthorID: 9}}
	outcomes := &retryingOutcomeRepository{pending: true, now: now}
	worker := NewProfileOutboxWorker(
		NewProfileProjector(projection, WithProfileProjectionNow(func() time.Time { return now })),
		nil,
		store,
		WithProfileOutboxOutcomeRepository(outcomes),
	)
	current := now
	worker.now = func() time.Time { return current }

	if dispatched, err := worker.DispatchOnce(context.Background()); !errors.Is(err, domainrecommendation.ErrOutcomeAttributionPending) || dispatched != 0 {
		t.Fatalf("pending follow attribution dispatch=%d err=%v", dispatched, err)
	}
	if store.dispatched || store.failures != 1 || len(projection.eventIDs) != 1 {
		t.Fatalf("pending attribution lost profile signal or was terminal: store=%#v signals=%v", store, projection.eventIDs)
	}

	current = current.Add(time.Second)
	outcomes.pending = false
	outcomes.valid = true
	if dispatched, err := worker.DispatchOnce(context.Background()); err != nil || dispatched != 1 || !store.dispatched {
		t.Fatalf("late follow evidence dispatch=%d err=%v store=%#v", dispatched, err, store)
	}
	if outcomes.outcomes["follow:late-follow"] == nil {
		t.Fatalf("late follow evidence did not record outcome: %#v", outcomes.outcomes)
	}
}

func TestProfileOutboxWorkerSkipsForgedFollowAttributionWithoutLosingProfileSignal(t *testing.T) {
	now := time.Date(2026, 7, 27, 1, 0, 0, 0, time.UTC)
	store := &memoryFollowProfileOutbox{
		item: domainrelation.FollowProjectionOutboxItem{
			ID: 2, EventID: "forged-follow", UserID: 42, AuthorID: 9, Active: true, OccurredAt: now,
			RecommendationRequestID: "forged-recommendation-request", RecommendationVideoID: 11,
		},
		available: now,
	}
	projection := &retryingProfileRepo{feature: ProfileFeature{Vector: []float64{1}, AuthorID: 9}}
	outcomes := &retryingOutcomeRepository{valid: true, followVideoOwner: 99}
	worker := NewProfileOutboxWorker(
		NewProfileProjector(projection, WithProfileProjectionNow(func() time.Time { return now })),
		nil,
		store,
		WithProfileOutboxOutcomeRepository(outcomes),
	)
	worker.now = func() time.Time { return now }

	if dispatched, err := worker.DispatchOnce(context.Background()); err != nil || dispatched != 1 {
		t.Fatalf("forged follow attribution should not fail accepted follow: dispatched=%d err=%v", dispatched, err)
	}
	if !store.dispatched || store.failures != 0 || len(projection.eventIDs) != 1 {
		t.Fatalf("forged attribution lost follow profile signal: store=%#v profile=%#v", store, projection.eventIDs)
	}
	if len(outcomes.outcomes) != 0 {
		t.Fatalf("forged follow attribution created outcome: %#v", outcomes.outcomes)
	}
}

func TestActionProfileOutboxEventuallyProjectsAcceptedActionAfterPublishFailure(t *testing.T) {
	now := time.Date(2026, 7, 27, 1, 0, 0, 0, time.UTC)
	store := &memoryActionProfileOutbox{
		item: applicationinteraction.ActionProfileProjectionItem{
			EventID: "action-persisted-after-publish-failure", UserID: 42, VideoID: 9,
			ActionType: domaininteraction.ActionTypeLike, Active: true, Version: 1, OccurredAt: now,
		},
		available: now,
	}

	repo := &retryingProfileRepo{feature: ProfileFeature{Vector: []float64{1}, AuthorID: 3}, failures: 1}
	worker := NewProfileOutboxWorker(
		NewProfileProjector(repo, WithProfileProjectionNow(func() time.Time { return now })),
		nil,
		nil,
		WithActionProfileOutboxStore(store),
	)
	current := now
	worker.now = func() time.Time { return current }

	if dispatched, err := worker.DispatchOnce(context.Background()); err == nil || dispatched != 0 || store.failures != 1 {
		t.Fatalf("first durable action projection dispatched=%d err=%v store=%#v", dispatched, err, store)
	}
	current = current.Add(time.Second)
	if dispatched, err := worker.DispatchOnce(context.Background()); err != nil || dispatched != 1 || !store.dispatched {
		t.Fatalf("action projection retry dispatched=%d err=%v store=%#v", dispatched, err, store)
	}
	if len(repo.eventIDs) != 1 || repo.eventIDs[0] != "action-persisted-after-publish-failure" {
		t.Fatalf("accepted action was not projected exactly once: %v", repo.eventIDs)
	}
}

func TestActionProfileOutboxBacksOffUntilEmbeddingIsAvailable(t *testing.T) {
	now := time.Date(2026, 7, 27, 1, 0, 0, 0, time.UTC)
	store := &memoryActionProfileOutbox{
		item: applicationinteraction.ActionProfileProjectionItem{
			EventID: "action-awaiting-embedding", UserID: 42, VideoID: 9,
			ActionType: domaininteraction.ActionTypeLike, Active: true, Version: 1, OccurredAt: now,
		},
		available: now,
	}
	repo := &delayedProfileFeatureRepo{}
	worker := NewProfileOutboxWorker(
		NewProfileProjector(repo, WithProfileProjectionNow(func() time.Time { return now })),
		nil,
		nil,
		WithActionProfileOutboxStore(store),
	)
	current := now
	worker.now = func() time.Time { return current }

	if dispatched, err := worker.DispatchOnce(context.Background()); !errors.Is(err, domainrecommendation.ErrProfileFeatureUnavailable) || dispatched != 0 {
		t.Fatalf("missing embedding dispatch=%d err=%v", dispatched, err)
	}
	if store.failures != 1 || !store.available.Equal(now.Add(time.Second)) || store.dispatched {
		t.Fatalf("missing embedding was not durably backoff-scheduled: %#v", store)
	}
	if dispatched, err := worker.DispatchOnce(context.Background()); err != nil || dispatched != 0 {
		t.Fatalf("backoff window was ignored: dispatched=%d err=%v", dispatched, err)
	}

	current = current.Add(time.Second)
	repo.available = true
	if dispatched, err := worker.DispatchOnce(context.Background()); err != nil || dispatched != 1 || !store.dispatched {
		t.Fatalf("embedding-ready retry dispatched=%d err=%v store=%#v", dispatched, err, store)
	}
}
