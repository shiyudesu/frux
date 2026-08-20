package applicationrecommendation

import (
	"context"
	"errors"
	"fmt"
	applicationexposure "github.com/shiyudesu/frux/internal/application/exposure"
	domainrecommendation "github.com/shiyudesu/frux/internal/domain/recommendation"
	"math"
	"sync"
	"testing"
	"time"
)

type rankerTestRepo struct {
	vectors           map[int64][]float64
	interest          []float64
	features          *domainrecommendation.RankingFeatures
	featureMu         sync.Mutex
	captureFeatureIDs bool
	featureVideoIDs   []int64
	pool              []*domainrecommendation.Candidate
	servedEvidence    []*domainrecommendation.ServedCandidateEvidence
	evidenceByVideo   map[string]time.Time
	feedback          map[string]*domainrecommendation.Feedback
	outcomes          map[string]*domainrecommendation.Outcome
	evidenceMu        sync.Mutex
	captureEvidence   bool
	evidenceErr       error
}

func (r *rankerTestRepo) ListCandidatePool(context.Context, int64, int) ([]*domainrecommendation.Candidate, error) {
	return r.pool, nil
}

type rankerPolicySelector struct {
	policy *domainrecommendation.Policy
	err    error
}

type exposureWindowRepo struct {
	*rankerTestRepo
	since time.Time
}

func (r *exposureWindowRepo) ListRecentExposures(_ context.Context, _ int64, _ []int64, since time.Time) ([]*domainrecommendation.Exposure, error) {
	r.since = since
	return nil, nil
}

func (s rankerPolicySelector) Select(context.Context, string, int64, string) (*domainrecommendation.Policy, error) {
	return s.policy, s.err
}
func (r *rankerTestRepo) LoadUserInterestVector(context.Context, int64) ([]float64, bool, error) {
	return r.interest, len(r.interest) > 0, nil
}
func (r *rankerTestRepo) LoadVideoVectors(_ context.Context, ids []int64) (map[int64][]float64, error) {
	result := make(map[int64][]float64, len(ids))
	for _, id := range ids {
		result[id] = append([]float64(nil), r.vectors[id]...)
	}
	return result, nil
}
func (r *rankerTestRepo) ListRecentExposures(context.Context, int64, []int64, time.Time) ([]*domainrecommendation.Exposure, error) {
	return nil, nil
}
func (r *rankerTestRepo) SaveExposures(context.Context, []*domainrecommendation.ExposureWrite) ([]*domainrecommendation.Exposure, error) {
	return nil, nil
}
func (r *rankerTestRepo) FindFeedbackByUserAndIdempotencyKey(_ context.Context, userID int64, idempotencyKey string) (*domainrecommendation.Feedback, error) {
	r.evidenceMu.Lock()
	defer r.evidenceMu.Unlock()
	if feedback := r.feedback[fmt.Sprintf("%d:%s", userID, idempotencyKey)]; feedback != nil {
		copy := *feedback
		return &copy, nil
	}
	return nil, domainrecommendation.ErrFeedbackNotFound
}
func (r *rankerTestRepo) SaveFeedback(_ context.Context, feedback *domainrecommendation.Feedback) (*domainrecommendation.Feedback, bool, error) {
	r.evidenceMu.Lock()
	defer r.evidenceMu.Unlock()
	if r.feedback == nil {
		r.feedback = map[string]*domainrecommendation.Feedback{}
	}
	key := fmt.Sprintf("%d:%s", feedback.UserID, feedback.IdempotencyKey)
	if existing := r.feedback[key]; existing != nil {
		copy := *existing
		return &copy, true, nil
	}
	copy := *feedback
	copy.ID = int64(len(r.feedback) + 1)
	r.feedback[key] = &copy
	return &copy, false, nil
}
func (r *rankerTestRepo) SaveServedCandidateEvidence(_ context.Context, evidence *domainrecommendation.ServedCandidateEvidence) (bool, error) {
	if r.evidenceErr != nil {
		return false, r.evidenceErr
	}
	if !r.captureEvidence {
		return false, nil
	}
	r.evidenceMu.Lock()
	defer r.evidenceMu.Unlock()
	r.servedEvidence = append(r.servedEvidence, evidence)
	if r.evidenceByVideo == nil {
		r.evidenceByVideo = map[string]time.Time{}
	}
	for _, candidate := range evidence.Candidates {
		r.evidenceByVideo[evidenceKey(evidence.UserID, evidence.RequestID, candidate.VideoID)] = evidence.ExpiresAt
	}
	return false, nil
}
func (r *rankerTestRepo) AppendServedCandidateEvidence(ctx context.Context, evidence *domainrecommendation.ServedCandidateEvidence) (bool, error) {
	return r.SaveServedCandidateEvidence(ctx, evidence)
}
func (r *rankerTestRepo) HasServedCandidateEvidence(_ context.Context, userID int64, requestID string, videoID int64, now time.Time) (bool, error) {
	r.evidenceMu.Lock()
	defer r.evidenceMu.Unlock()
	return r.evidenceByVideo[evidenceKey(userID, requestID, videoID)].After(now), nil
}
func (*rankerTestRepo) DeleteServedCandidateEvidenceBefore(context.Context, time.Time, int) (domainrecommendation.ServedCandidateEvidenceCleanupResult, error) {
	return domainrecommendation.ServedCandidateEvidenceCleanupResult{}, nil
}
func (r *rankerTestRepo) LoadRankingFeatures(_ context.Context, _ int64, videoIDs []int64, _ time.Time, _ time.Time) (*domainrecommendation.RankingFeatures, error) {
	if r.captureFeatureIDs {
		r.featureMu.Lock()
		r.featureVideoIDs = append([]int64(nil), videoIDs...)
		r.featureMu.Unlock()
	}
	return r.features, nil
}

func (r *rankerTestRepo) GetFeedbackVideo(_ context.Context, videoID int64) (*FeedbackVideo, error) {
	for _, candidate := range r.pool {
		if candidate != nil && candidate.VideoID == videoID {
			return &FeedbackVideo{VideoID: videoID, AuthorID: candidate.AuthorID}, nil
		}
	}
	return nil, domainrecommendation.ErrVideoNotFound
}

func (*rankerTestRepo) ApplyBehaviorEvent(context.Context, *applicationexposure.ViewEventRecordedEvent) (bool, error) {
	return true, nil
}

func (r *rankerTestRepo) VerifyAndSaveOutcome(ctx context.Context, outcome *domainrecommendation.Outcome, _ int64) (bool, bool, error) {
	valid, err := r.HasServedCandidateEvidence(ctx, outcome.UserID, outcome.RequestID, outcome.VideoID, outcome.RecordedAt)
	if err != nil || !valid {
		return false, false, err
	}
	r.evidenceMu.Lock()
	defer r.evidenceMu.Unlock()
	if r.outcomes == nil {
		r.outcomes = map[string]*domainrecommendation.Outcome{}
	}
	if r.outcomes[outcome.ID] != nil {
		return false, true, nil
	}
	r.outcomes[outcome.ID] = outcome
	return true, true, nil
}

func evidenceKey(userID int64, requestID string, videoID int64) string {
	return fmt.Sprintf("%d:%s:%d", userID, requestID, videoID)
}

func rankerPolicy(t testing.TB, version int, weights map[string]float64) *domainrecommendation.Policy {
	t.Helper()
	config := defaultRecommendationPolicyConfiguration()
	config.FeatureWeights = weights
	config.HardSuppressExposures = false
	policy, err := domainrecommendation.NewPolicy("recommend", version, true, config, time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	return policy
}

func rankerCandidate(id, author int64, hot int, published time.Time, provider string) *domainrecommendation.Candidate {
	candidate := domainrecommendation.RestoreCandidate(id, author, 0, 0, hot, 0, "", published)
	candidate.RecallReasons = []domainrecommendation.RecallReason{{Provider: provider, Score: 1}}
	return candidate
}

func emptyRankingFeatures() *domainrecommendation.RankingFeatures {
	return &domainrecommendation.RankingFeatures{
		FollowedAuthors: map[int64]bool{}, RecentExposures: map[int64]*domainrecommendation.Exposure{},
		NegativeVideos: map[int64]bool{}, NegativeAuthors: map[int64]bool{},
	}
}

func TestRankerPolicyWeightsAndAffinitySignals(t *testing.T) {
	now := time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC)
	features := emptyRankingFeatures()
	features.Profile = domainrecommendation.RestoreUserInterestProfile(1, nil, []float64{1, 0}, map[int64]float64{2: 100}, nil, nil, 1, now)
	features.FollowedAuthors[2] = true
	repo := &rankerTestRepo{
		vectors:  map[int64][]float64{1: {1, 0}, 2: {0, 1}},
		features: features,
	}

	service := New(repo, WithNow(func() time.Time { return now }))
	pool := []*domainrecommendation.Candidate{
		rankerCandidate(1, 1, 100, now, domainrecommendation.RecallProviderHot),
		rankerCandidate(2, 2, 1, now, domainrecommendation.RecallProviderFresh),
	}
	contentPolicy := rankerPolicy(t, 2, map[string]float64{
		domainrecommendation.FeatureContentSimilarity: 1,
		domainrecommendation.FeatureAuthorAffinity:    0,
		domainrecommendation.FeatureFollowRelation:    0,
	})
	ranked, err := service.rankCandidates(context.Background(), 1, nil, pool, contentPolicy)
	if err != nil || ranked[0].VideoID != 1 {
		t.Fatalf("content policy did not select similarity winner: %#v, %v", ranked, err)
	}
	affinityPolicy := rankerPolicy(t, 3, map[string]float64{
		domainrecommendation.FeatureContentSimilarity: 0,
		domainrecommendation.FeatureAuthorAffinity:    1,
		domainrecommendation.FeatureFollowRelation:    1,
	})
	ranked, err = service.rankCandidates(context.Background(), 1, nil, pool, affinityPolicy)
	if err != nil || ranked[0].VideoID != 2 || ranked[0].PolicyVersion != 3 {
		t.Fatalf("affinity/follow policy did not apply: %#v, %v", ranked, err)
	}
}

func TestRankerProfileAgeConfidenceLetsFreshSignalsDominate(t *testing.T) {
	now := time.Date(2026, 7, 27, 8, 0, 0, 0, time.UTC)
	profile := domainrecommendation.RestoreUserInterestProfile(
		1, nil, []float64{1, 0}, map[int64]float64{1: 100}, nil, nil, 1, now.Add(-24*time.Hour),
	)
	features := emptyRankingFeatures()
	features.Profile = profile
	repo := &rankerTestRepo{
		vectors:  map[int64][]float64{1: {1, 0}, 2: {0, 1}},
		features: features,
	}
	service := New(repo, WithNow(func() time.Time { return now }))
	policy := rankerPolicy(t, 4, map[string]float64{
		domainrecommendation.FeatureContentSimilarity: 1,
		domainrecommendation.FeatureAuthorAffinity:    1,
		domainrecommendation.FeatureHotness:           .5,
	})
	policy.Config.ProfileRecentHalfLifeHours = 1
	policy.Config.ProfileLongTermHalfLifeHours = 1
	pool := []*domainrecommendation.Candidate{
		rankerCandidate(1, 1, 0, now, domainrecommendation.RecallProviderFresh),
		rankerCandidate(2, 2, 100, now, domainrecommendation.RecallProviderHot),
	}

	stale, err := service.rankCandidates(context.Background(), 1, nil, pool, policy)
	if err != nil || stale[0].VideoID != 2 || stale[1].Similarity >= .001 {
		t.Fatalf("stale profile retained excessive ranking influence: %#v, %v", stale, err)
	}

	freshEvent, err := domainrecommendation.NewProfileEvent(domainrecommendation.ProfileEventInput{
		UserID: 1, SourceEventID: "fresh-interest", EventType: "complete", OccurredAt: now,
		RecentVector: []float64{1, 0}, AuthorAffinities: map[int64]float64{1: 100},
	})
	if err != nil {
		t.Fatal(err)
	}
	freshProfile, err := profile.ApplyWithDecay(freshEvent, domainrecommendation.ProfileDecay{
		LongTermHalfLife: time.Hour, RecentHalfLife: time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	repo.features.Profile = freshProfile
	fresh, err := service.rankCandidates(context.Background(), 1, nil, pool, policy)
	if err != nil || fresh[0].VideoID != 1 || fresh[0].Similarity < .99 {
		t.Fatalf("fresh profile signal did not dominate stale interest: %#v, %v", fresh, err)
	}
}

func TestRecommendSelectsPolicyAndRetainsVersion(t *testing.T) {
	now := time.Now().UTC()
	policy := rankerPolicy(t, 9, map[string]float64{domainrecommendation.FeatureHotness: 1})
	repo := &rankerTestRepo{
		vectors:  map[int64][]float64{1: {1}},
		features: emptyRankingFeatures(),
		pool:     []*domainrecommendation.Candidate{rankerCandidate(1, 1, 1, now, domainrecommendation.RecallProviderHot)},
	}

	service := New(repo, WithNow(func() time.Time { return now }), WithPolicySelector(rankerPolicySelector{policy: policy}))
	result, err := service.Recommend(context.Background(), CandidateRequest{UserID: 1, Scene: "recommend", RequestID: "request", Limit: 1})
	if err != nil || result.PolicyVersion != 9 || result.Candidates[0].PolicyVersion != 9 {
		t.Fatalf("selected policy version was not retained: %#v, %v", result, err)
	}
	service = New(repo, WithNow(func() time.Time { return now }), WithPolicySelector(rankerPolicySelector{err: domainrecommendation.ErrPolicyNotFound}))
	result, err = service.Recommend(context.Background(), CandidateRequest{UserID: 1, Scene: "recommend", RequestID: "request", Limit: 1})
	if err != nil || result.PolicyVersion != 1 {
		t.Fatalf("missing stored policy did not use validated default: %#v, %v", result, err)
	}
	service = New(repo, WithPolicySelector(rankerPolicySelector{err: errors.New("storage unavailable")}))
	if _, err = service.Recommend(context.Background(), CandidateRequest{UserID: 1, Scene: "recommend", Limit: 1}); !errors.Is(err, ErrLoadRecommendationFailed) {
		t.Fatalf("stored-policy lookup error must not use default: %v", err)
	}
}

func TestDecideExposuresUsesSelectedPolicyWindow(t *testing.T) {
	now := time.Date(2026, 7, 27, 5, 0, 0, 0, time.UTC)
	policy := rankerPolicy(t, 12, map[string]float64{domainrecommendation.FeatureHotness: 1})
	policy.Config.ExposureWindowHours = 6
	repo := &exposureWindowRepo{rankerTestRepo: &rankerTestRepo{}}
	service := New(
		repo,
		WithNow(func() time.Time { return now }),
		WithPolicySelector(rankerPolicySelector{policy: policy}),
	)

	result, err := service.DecideExposures(context.Background(), ExposureDecisionInput{
		UserID: 7, Scene: "recommend", RequestID: "policy-window", VideoIDs: []int64{101},
	})
	if err != nil || len(result.Decisions) != 1 || !result.Decisions[0].Allowed {
		t.Fatalf("exposure decision failed: %#v, %v", result, err)
	}
	if !repo.since.Equal(now.Add(-6 * time.Hour)) {
		t.Fatalf("decision ignored selected policy exposure window: got %s want %s", repo.since, now.Add(-6*time.Hour))
	}
}

func TestRecordDeliveredCandidatesReportsEvidencePersistenceFailure(t *testing.T) {
	now := time.Now().UTC()
	repo := &rankerTestRepo{
		vectors:     map[int64][]float64{1: {1}},
		features:    emptyRankingFeatures(),
		pool:        []*domainrecommendation.Candidate{rankerCandidate(1, 1, 1, now, domainrecommendation.RecallProviderHot)},
		evidenceErr: errors.New("storage unavailable"),
	}
	service := New(repo, WithNow(func() time.Time { return now }))

	if err := service.RecordDeliveredCandidates(context.Background(), DeliveredCandidatesInput{
		UserID: 1, RequestID: "request", PolicyVersion: 1, VideoIDs: []int64{1}, ExpiresAt: now.Add(time.Minute),
	}); !errors.Is(err, ErrSaveRecommendationEvidenceFailed) {
		t.Fatalf("delivery persistence failure was not observable: %v", err)
	}
}

func TestRankerNegativeFeedbackExposureAndFallback(t *testing.T) {
	now := time.Now().UTC()
	features := emptyRankingFeatures()
	features.NegativeVideos[1] = true
	features.RecentExposures[1] = domainrecommendation.RestoreExposure(1, 1, 1, now, now, 3, "recommend")
	repo := &rankerTestRepo{vectors: map[int64][]float64{1: {1}, 2: {1}}, features: features}
	service := New(repo, WithNow(func() time.Time { return now }))
	policy := rankerPolicy(t, 2, map[string]float64{
		domainrecommendation.FeatureNegativePenalty: -1,
		domainrecommendation.FeatureExposurePenalty: -1,
	})
	policy.Config.HardSuppressExposures = true
	policy.Config.MinimumFallbackPool = 1
	pool := []*domainrecommendation.Candidate{
		rankerCandidate(1, 1, 0, now, domainrecommendation.RecallProviderFresh),
		rankerCandidate(2, 2, 0, now, domainrecommendation.RecallProviderFresh),
	}
	ranked, err := service.rankCandidates(context.Background(), 1, nil, pool, policy)
	if err != nil || len(ranked) != 1 || ranked[0].VideoID != 2 {
		t.Fatalf("exposed/negative candidate was not suppressed: %#v, %v", ranked, err)
	}
	policy.Config.MinimumFallbackPool = 2
	ranked, err = service.rankCandidates(context.Background(), 1, nil, pool, policy)
	if err != nil || len(ranked) != 2 || ranked[0].VideoID != 2 {
		t.Fatalf("fallback pool did not retain candidates deterministically: %#v, %v", ranked, err)
	}
}

func TestRankerColdStartBoundsAndDeterministicTies(t *testing.T) {
	now := time.Now().UTC()
	repo := &rankerTestRepo{
		vectors:  map[int64][]float64{3: {math.NaN()}, 2: {1}, 1: {1}},
		features: emptyRankingFeatures(),
	}
	service := New(repo, WithNow(func() time.Time { return now }))
	policy := rankerPolicy(t, 2, map[string]float64{
		domainrecommendation.FeatureContentSimilarity: 1,
		domainrecommendation.FeatureHotness:           0,
	})
	pool := []*domainrecommendation.Candidate{
		rankerCandidate(1, 1, 0, now, domainrecommendation.RecallProviderFresh),
		rankerCandidate(3, 3, 0, now, domainrecommendation.RecallProviderFresh),
		rankerCandidate(2, 2, 0, now, domainrecommendation.RecallProviderFresh),
	}
	ranked, err := service.rankCandidates(context.Background(), 1, nil, pool, policy)
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range ranked {
		if math.IsNaN(candidate.RankScore) || math.IsInf(candidate.RankScore, 0) {
			t.Fatalf("invalid rank score: %#v", candidate)
		}
		for name, value := range candidate.ScoreComponents {
			if value < 0 || value > 1 || math.IsNaN(value) || math.IsInf(value, 0) {
				t.Fatalf("%s was not normalized: %v", name, value)
			}
		}
	}
	if ranked[0].VideoID != 3 || ranked[1].VideoID != 2 || ranked[2].VideoID != 1 {
		t.Fatalf("cold-start ties lost published/id ordering: %#v", ranked)
	}
}

func TestRankerDiversityHonorsGapsWithBoundedFallback(t *testing.T) {
	now := time.Now().UTC()
	candidates := []*domainrecommendation.Candidate{
		rankerCandidate(5, 1, 0, now, domainrecommendation.RecallProviderFresh),
		rankerCandidate(4, 1, 0, now.Add(-time.Second), domainrecommendation.RecallProviderFresh),
		rankerCandidate(3, 2, 0, now.Add(-2*time.Second), domainrecommendation.RecallProviderHot),
		rankerCandidate(2, 1, 0, now.Add(-3*time.Second), domainrecommendation.RecallProviderFresh),
		rankerCandidate(1, 3, 0, now.Add(-4*time.Second), domainrecommendation.RecallProviderContentSimilarity),
	}
	diversified := diversifyCandidates(candidates, domainrecommendation.DiversityRules{MaxPerAuthor: 2, MinAuthorGap: 1, MinContentGap: 1})
	if diversified[0].VideoID != 5 || diversified[1].VideoID != 3 || diversified[2].VideoID != 4 {
		t.Fatalf("diversity did not deterministically spread authors/content: %#v", diversified)
	}
	onlyAuthor := diversifyCandidates(candidates[:2], domainrecommendation.DiversityRules{MaxPerAuthor: 1, MinAuthorGap: 1, MinContentGap: 1})
	if len(onlyAuthor) != 2 || onlyAuthor[0].VideoID != 5 || onlyAuthor[1].VideoID != 4 {
		t.Fatalf("infeasible diversity did not use bounded stable fallback: %#v", onlyAuthor)
	}
}
