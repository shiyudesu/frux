package applicationrecommendation

import (
	applicationexposure "github.com/shiyudesu/frux/internal/application/exposure"
	applicationinteraction "github.com/shiyudesu/frux/internal/application/interaction"
	domainrecommendation "github.com/shiyudesu/frux/internal/domain/recommendation"
	"context"
	"errors"
	"math"
	"testing"
	"time"
)

type projectionRepo struct {
	feature   ProfileFeature
	events    []*domainrecommendation.ProfileEvent
	loadCalls int
}

type idempotentProjectionRepo struct {
	projectionRepo
	hashes map[string]string
}

type materializingProjectionRepo struct {
	projectionRepo
	profile *domainrecommendation.UserInterestProfile
	hashes  map[string]string
}

func (r *materializingProjectionRepo) ApplyProfileEvent(_ context.Context, event *domainrecommendation.ProfileEvent) (*domainrecommendation.UserInterestProfile, bool, error) {
	if r.hashes == nil {
		r.hashes = map[string]string{}
	}
	if existing, ok := r.hashes[event.SourceEventID]; ok {
		if existing != event.PayloadHash {
			return nil, false, domainrecommendation.ErrProfileEventConflict
		}
		return r.profile.Clone(), false, nil
	}
	r.hashes[event.SourceEventID] = event.PayloadHash
	if r.profile == nil {
		r.profile = domainrecommendation.EmptyUserInterestProfile(event.UserID, event.OccurredAt)
	}
	updated, err := r.profile.ApplyWithDecay(event, event.Decay)
	if err != nil {
		return nil, false, err
	}
	r.profile = updated
	return updated.Clone(), true, nil
}

func (r *idempotentProjectionRepo) ApplyProfileEvent(_ context.Context, event *domainrecommendation.ProfileEvent) (*domainrecommendation.UserInterestProfile, bool, error) {
	if r.hashes == nil {
		r.hashes = map[string]string{}
	}
	if existing, ok := r.hashes[event.SourceEventID]; ok {
		if existing != event.PayloadHash {
			return nil, false, domainrecommendation.ErrProfileEventConflict
		}
		return domainrecommendation.EmptyUserInterestProfile(event.UserID, event.OccurredAt), false, nil
	}
	r.hashes[event.SourceEventID] = event.PayloadHash
	r.events = append(r.events, event)
	return domainrecommendation.EmptyUserInterestProfile(event.UserID, event.OccurredAt), true, nil
}

func (r *projectionRepo) LoadProfileFeature(context.Context, int64) (ProfileFeature, bool, error) {
	r.loadCalls++
	return r.feature, true, nil
}
func (r *projectionRepo) ApplyProfileEvent(_ context.Context, event *domainrecommendation.ProfileEvent) (*domainrecommendation.UserInterestProfile, bool, error) {
	r.events = append(r.events, event)
	return domainrecommendation.EmptyUserInterestProfile(event.UserID, event.OccurredAt), true, nil
}
func TestProfileProjectorPreservesWeightsAndDecays(t *testing.T) {
	now := time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC)
	repo := &projectionRepo{feature: ProfileFeature{Vector: []float64{1, 2}, AuthorID: 9}}
	projector := NewProfileProjector(repo, WithProfileProjectionNow(func() time.Time { return now }), WithProfileWeighting(ProfileWeighting{LongTermHalfLife: 48 * time.Hour, RecentHalfLife: 24 * time.Hour, ProgressWeight: 2, MaxSignalWeight: 5}))
	duration := 1000
	applied, err := projector.ApplyView(context.Background(), &applicationexposure.ViewEventRecordedEvent{EventID: "progress", UserID: 1, VideoID: 2, EventType: "progress", PositionMs: 600, DurationMs: &duration, OccurredAt: now.Add(-24 * time.Hour)})
	if err != nil || !applied {
		t.Fatalf("progress applied=%v err=%v", applied, err)
	}
	if got := repo.events[0].RecentVector[0]; got < 1.99 || got > 2.01 {
		t.Fatalf("raw recent weight=%v, want 2", got)
	}
	if got := repo.events[0].LongTermVector[0]; got < 1.99 || got > 2.01 {
		t.Fatalf("raw long-term weight=%v, want 2", got)
	}
}
func TestProfileProjectorMakesEarlySkipNegative(t *testing.T) {
	now := time.Now().UTC()
	repo := &projectionRepo{feature: ProfileFeature{Vector: []float64{1}, AuthorID: 9}}
	p := NewProfileProjector(repo, WithProfileProjectionNow(func() time.Time { return now }))
	duration := 1000
	_, err := p.ApplyView(context.Background(), &applicationexposure.ViewEventRecordedEvent{EventID: "skip", UserID: 1, VideoID: 2, EventType: "skip", PositionMs: 100, DurationMs: &duration, OccurredAt: now})
	if err != nil || len(repo.events) != 1 || repo.events[0].NegativeTopicVector[0] <= 0 {
		t.Fatalf("early skip event=%#v err=%v", repo.events, err)
	}
}

func TestProfileProjectorDelaysRawFollowAndActionSignalsUntilMaterialization(t *testing.T) {
	now := time.Date(2026, 7, 27, 8, 0, 0, 0, time.UTC)
	occurredAt := now.Add(-24 * time.Hour)
	repo := &materializingProjectionRepo{projectionRepo: projectionRepo{feature: ProfileFeature{Vector: []float64{1}, AuthorID: 9}}}
	weights := ProfileWeighting{
		LongTermHalfLife: 48 * time.Hour, RecentHalfLife: 24 * time.Hour,
		LikeWeight: 1, FollowWeight: .75, MaxSignalWeight: 2,
	}
	projector := NewProfileProjector(repo, WithProfileProjectionNow(func() time.Time { return now }), WithProfileWeighting(weights))
	if applied, err := projector.ApplyAction(context.Background(), &applicationinteraction.ActionChangedEvent{
		EventID: "delayed-like", UserID: 7, VideoID: 11, ActionType: "LIKE", Active: true, OccurredAt: occurredAt,
	}); err != nil || !applied {
		t.Fatalf("apply delayed action: applied=%v err=%v", applied, err)
	}
	if applied, err := projector.ApplyFollow(context.Background(), "delayed-follow", 7, 12, true, occurredAt); err != nil || !applied {
		t.Fatalf("apply delayed follow: applied=%v err=%v", applied, err)
	}
	if repo.profile.RecentVector[0] != 1 || repo.profile.AuthorAffinities[9] != 1 || repo.profile.AuthorAffinities[12] != .75 {
		t.Fatalf("delayed source was aged before materialization: %#v", repo.profile)
	}
	decayed := repo.profile.DecayTo(now, domainrecommendation.ProfileDecay{
		LongTermHalfLife: weights.LongTermHalfLife, RecentHalfLife: weights.RecentHalfLife,
	})
	if math.Abs(decayed.RecentVector[0]-.5) > 1e-9 ||
		math.Abs(decayed.AuthorAffinities[9]-.5) > 1e-9 ||
		math.Abs(decayed.AuthorAffinities[12]-.375) > 1e-9 {
		t.Fatalf("delayed follow/action did not decay exactly once at read: %#v", decayed)
	}
}

func TestProfileProjectionIdentityIgnoresRetryDecayButRejectsChangedSourceSignal(t *testing.T) {
	now := time.Date(2026, 7, 27, 4, 0, 0, 0, time.UTC)
	repo := &idempotentProjectionRepo{projectionRepo: projectionRepo{feature: ProfileFeature{Vector: []float64{1}, AuthorID: 9}}}
	duration := 1_000
	event := &applicationexposure.ViewEventRecordedEvent{
		EventID: "view-1", UserID: 7, VideoID: 11, EventType: "progress",
		PositionMs: 800, WatchMs: 800, DurationMs: &duration, OccurredAt: now.Add(-time.Hour),
	}
	first := NewProfileProjector(repo, WithProfileProjectionNow(func() time.Time { return now }))
	if _, err := first.ApplyView(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	firstHash := repo.events[0].PayloadHash
	retry := NewProfileProjector(repo, WithProfileProjectionNow(func() time.Time { return now.Add(12 * time.Hour) }))
	if applied, err := retry.ApplyView(context.Background(), event); err != nil || applied {
		t.Fatalf("retry result applied=%v err=%v", applied, err)
	}
	if repo.hashes[event.EventID] != firstHash {
		t.Fatalf("retry decay changed source identity: %s != %s", repo.hashes[event.EventID], firstHash)
	}

	changed := *event
	changed.PositionMs = 900
	if _, err := retry.ApplyView(context.Background(), &changed); !errors.Is(err, domainrecommendation.ErrProfileEventConflict) {
		t.Fatalf("different immutable source signal error = %v, want profile conflict", err)
	}
}

func TestProfileProjectorUsesDurableReduceAuthorScopeWithoutEmbedding(t *testing.T) {
	now := time.Date(2026, 7, 27, 5, 0, 0, 0, time.UTC)
	repo := &projectionRepo{}
	projector := NewProfileProjector(repo, WithProfileProjectionNow(func() time.Time { return now }))
	feedback := domainrecommendation.RestoreFeedback(
		7, 42, 11, "request-1", domainrecommendation.FeedbackTypeReduceAuthor, "feedback-1", now,
	)
	if err := feedback.SetSuppression(domainrecommendation.SuppressionScopeAuthor, 9, now.Add(14*24*time.Hour)); err != nil {
		t.Fatalf("set suppression: %v", err)
	}

	applied, err := projector.ApplyFeedback(context.Background(), feedback)
	if err != nil || !applied {
		t.Fatalf("apply feedback: applied=%v err=%v", applied, err)
	}
	if len(repo.events) != 1 {
		t.Fatalf("profile events = %#v", repo.events)
	}
	event := repo.events[0]
	if repo.loadCalls != 0 || event.SourceAuthorID != 9 || event.NegativeAuthorWeights[9] <= 0 || len(event.NegativeTopicVector) != 0 {
		t.Fatalf("reduce-author event = %#v", event)
	}
}
