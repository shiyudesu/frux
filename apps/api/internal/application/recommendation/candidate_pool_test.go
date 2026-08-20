package applicationrecommendation

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	domainrecommendation "github.com/shiyudesu/frux/internal/domain/recommendation"
	inframetrics "github.com/shiyudesu/frux/internal/infra/metrics"
)

type candidatePoolPolicyRepo struct{ created bool }

func (r *candidatePoolPolicyRepo) CreatePolicy(_ context.Context, policy *domainrecommendation.Policy) (*domainrecommendation.Policy, error) {
	r.created = true
	return policy, nil
}
func (*candidatePoolPolicyRepo) ActivatePolicy(context.Context, string, int) (*domainrecommendation.Policy, error) {
	return nil, domainrecommendation.ErrPolicyNotFound
}
func (*candidatePoolPolicyRepo) RollbackPolicy(context.Context, string, int) (*domainrecommendation.Policy, error) {
	return nil, domainrecommendation.ErrPolicyNotFound
}
func (*candidatePoolPolicyRepo) ListEnabledPolicies(context.Context, string) ([]*domainrecommendation.Policy, error) {
	return nil, nil
}
func (*candidatePoolPolicyRepo) ListPolicies(context.Context, string) ([]*domainrecommendation.Policy, error) {
	return nil, nil
}

func TestPolicyServiceObservesOverBoundRecallBudgetRejection(t *testing.T) {
	repo := &candidatePoolPolicyRepo{}
	config := defaultRecommendationPolicyConfiguration()
	config.RecallBudgets[domainrecommendation.RecallProviderFresh]++
	counter := inframetrics.RecommendationPolicyRejectionsTotal.WithLabelValues("pre_rank_pool")
	before := testutil.ToFloat64(counter)
	_, err := NewPolicyService(repo, func() time.Time { return time.Unix(1, 0).UTC() }).Create(context.Background(), PolicyInput{
		Scene: "recommend", Version: 3, Enabled: true, Config: config,
	})
	if !errors.Is(err, domainrecommendation.ErrInvalidPolicyBound) {
		t.Fatalf("over-bound policy error = %v, want %v", err, domainrecommendation.ErrInvalidPolicyBound)
	}
	if repo.created {
		t.Fatal("invalid over-bound policy reached persistence")
	}
	if delta := testutil.ToFloat64(counter) - before; delta != 1 {
		t.Fatalf("pre-rank pool rejection metric delta = %v, want 1", delta)
	}
}

func TestPolicyRecallRanksCompleteFiveProviderPool(t *testing.T) {
	now := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	providerNames := []string{
		domainrecommendation.RecallProviderFresh,
		domainrecommendation.RecallProviderHot,
		domainrecommendation.RecallProviderContentSimilarity,
		domainrecommendation.RecallProviderFollowedAuthor,
		domainrecommendation.RecallProviderSessionContinuation,
	}
	providers := make([]RecallProvider, 0, len(providerNames))
	visible := make(map[int64]*domainrecommendation.Candidate, domainrecommendation.MaxPolicyPreRankCandidates)
	vectors := make(map[int64][]float64, domainrecommendation.MaxPolicyPreRankCandidates)
	for providerIndex, providerName := range providerNames {
		candidates := make([]*domainrecommendation.Candidate, 0, 100)
		for offset := 1; offset <= 100; offset++ {
			id := int64(providerIndex*100 + offset)
			candidate := recallCandidate(id, id, offset, now.Add(-time.Duration(id)*time.Minute))
			candidates = append(candidates, annotateCandidate(candidate, providerName, float64(101-offset)))
			visible[id] = candidate
			vectors[id] = []float64{1, 0}
		}
		providerCandidates := candidates
		providers = append(providers, providerFunc{name: providerName, run: func(context.Context, RecallRequest) ([]*domainrecommendation.Candidate, error) {
			return providerCandidates, nil
		}})
	}

	repo := &rankerTestRepo{
		vectors: vectors, features: emptyRankingFeatures(), captureFeatureIDs: true,
	}
	policy := rankerPolicy(t, 1, defaultRecommendationPolicyConfiguration().FeatureWeights)
	policy.Config.SamplingRatePPM = domainrecommendation.MaxSamplingRatePPM
	logs := &memoryRequestLogRepository{}
	store := &memorySnapshotStore{}
	signer, err := NewHMACSnapshotCursorSigner("complete-policy-pool-secret")
	if err != nil {
		t.Fatal(err)
	}
	service := New(
		repo,
		WithNow(func() time.Time { return now }),
		WithPolicySelector(rankerPolicySelector{policy: policy}),
		WithCandidateVisibilityFilter(visibilityCatalog{visible: visible}),
		WithRecallProviders(providers...),
		WithSnapshotPagination(store, signer),
		WithRequestLogRepository(logs),
	)

	result, err := service.Recommend(context.Background(), CandidateRequest{
		UserID: 9, Scene: "recommend", RequestID: "complete-500", Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Candidates) != 10 {
		t.Fatalf("response candidates = %d, want 10", len(result.Candidates))
	}
	repo.featureMu.Lock()
	featureCount := len(repo.featureVideoIDs)
	repo.featureMu.Unlock()
	if featureCount != domainrecommendation.MaxPolicyPreRankCandidates {
		t.Fatalf("feature pool = %d, want %d", featureCount, domainrecommendation.MaxPolicyPreRankCandidates)
	}
	if len(logs.logs) != 1 || len(logs.logs[0].Candidates) != domainrecommendation.MaxPolicyPreRankCandidates {
		t.Fatalf("request log did not retain complete pool: %#v", logs.logs)
	}
	if len(store.snapshots) != 1 {
		t.Fatalf("snapshot count = %d, want 1", len(store.snapshots))
	}
	for _, snapshot := range store.snapshots {
		if len(snapshot.Candidates) != domainrecommendation.MaxPolicyPreRankCandidates {
			t.Fatalf("snapshot pool = %d, want %d", len(snapshot.Candidates), domainrecommendation.MaxPolicyPreRankCandidates)
		}
	}
}

func TestOlderCandidateOutsideFormerPrefixCanWinPolicyRanking(t *testing.T) {
	now := time.Date(2026, 8, 20, 1, 0, 0, 0, time.UTC)
	candidates := make([]*domainrecommendation.Candidate, 0, 100)
	visible := make(map[int64]*domainrecommendation.Candidate, 100)
	vectors := make(map[int64][]float64, 100)
	for id := int64(1); id <= 100; id++ {
		publishedAt := now.Add(-time.Duration(id) * time.Minute)
		if id == 1 {
			publishedAt = now.Add(-30 * 24 * time.Hour)
		}
		candidate := recallCandidate(id, id, 0, publishedAt)
		candidates = append(candidates, annotateCandidate(candidate, domainrecommendation.RecallProviderFresh, float64(101-id)))
		visible[id] = candidate
		vectors[id] = []float64{0, 1}
	}
	vectors[1] = []float64{1, 0}
	repo := &rankerTestRepo{
		vectors: vectors, interest: []float64{1, 0}, features: emptyRankingFeatures(), captureFeatureIDs: true,
	}
	policy := rankerPolicy(t, 2, map[string]float64{domainrecommendation.FeatureContentSimilarity: 1})
	service := New(
		repo,
		WithNow(func() time.Time { return now }),
		WithPolicySelector(rankerPolicySelector{policy: policy}),
		WithCandidateVisibilityFilter(visibilityCatalog{visible: visible}),
		WithRecallProviders(providerFunc{name: domainrecommendation.RecallProviderFresh, run: func(context.Context, RecallRequest) ([]*domainrecommendation.Candidate, error) {
			return candidates, nil
		}}),
	)

	result, err := service.Recommend(context.Background(), CandidateRequest{
		UserID: 7, Scene: "recommend", RequestID: "older-winner", Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Candidates) == 0 || result.Candidates[0].VideoID != 1 {
		t.Fatalf("older similarity winner was lost: %#v", result.Candidates)
	}
	repo.featureMu.Lock()
	featureCount := len(repo.featureVideoIDs)
	repo.featureMu.Unlock()
	if featureCount != 100 {
		t.Fatalf("feature pool = %d, want 100", featureCount)
	}
}

func TestPolicyRecallFinalOrderIgnoresProviderCompletionOrder(t *testing.T) {
	now := time.Date(2026, 8, 20, 2, 0, 0, 0, time.UTC)
	current := map[int64]*domainrecommendation.Candidate{
		1: recallCandidate(1, 1, 5, now.Add(-time.Minute)),
		2: recallCandidate(2, 2, 20, now.Add(-2*time.Minute)),
		3: recallCandidate(3, 3, 10, now.Add(-3*time.Minute)),
		4: recallCandidate(4, 4, 15, now.Add(-4*time.Minute)),
		5: recallCandidate(5, 5, 1, now.Add(-5*time.Minute)),
	}
	fresh := []*domainrecommendation.Candidate{
		annotateCandidate(current[1], domainrecommendation.RecallProviderFresh, 5),
		annotateCandidate(current[3], domainrecommendation.RecallProviderFresh, 4),
		annotateCandidate(current[5], domainrecommendation.RecallProviderFresh, 3),
	}
	hot := []*domainrecommendation.Candidate{
		annotateCandidate(current[2], domainrecommendation.RecallProviderHot, 20),
		annotateCandidate(current[3], domainrecommendation.RecallProviderHot, 10),
		annotateCandidate(current[4], domainrecommendation.RecallProviderHot, 15),
	}
	run := func(freshDelay, hotDelay time.Duration) ([]int64, []string) {
		repo := &rankerTestRepo{
			vectors:  map[int64][]float64{1: {1}, 2: {1}, 3: {1}, 4: {1}, 5: {1}},
			features: emptyRankingFeatures(),
		}
		policy := rankerPolicy(t, 3, map[string]float64{
			domainrecommendation.FeatureHotness:   1,
			domainrecommendation.FeatureFreshness: 0.1,
		})
		service := New(
			repo,
			WithNow(func() time.Time { return now }),
			WithPolicySelector(rankerPolicySelector{policy: policy}),
			WithCandidateVisibilityFilter(visibilityCatalog{visible: current}),
			WithRecallProviders(
				providerFunc{name: domainrecommendation.RecallProviderFresh, run: func(context.Context, RecallRequest) ([]*domainrecommendation.Candidate, error) {
					time.Sleep(freshDelay)
					return fresh, nil
				}},
				providerFunc{name: domainrecommendation.RecallProviderHot, run: func(context.Context, RecallRequest) ([]*domainrecommendation.Candidate, error) {
					time.Sleep(hotDelay)
					return hot, nil
				}},
			),
		)
		result, err := service.Recommend(context.Background(), CandidateRequest{
			UserID: 3, Scene: "recommend", RequestID: "completion-order", Limit: 10,
		})
		if err != nil {
			t.Fatal(err)
		}
		ids := candidateIDs(result.Candidates)
		reasons := []string{}
		for _, candidate := range result.Candidates {
			if candidate.VideoID == 3 {
				for _, reason := range candidate.RecallReasons {
					reasons = append(reasons, reason.Provider)
				}
			}
		}
		return ids, reasons
	}

	firstIDs, firstReasons := run(5*time.Millisecond, 0)
	secondIDs, secondReasons := run(0, 5*time.Millisecond)
	if !reflect.DeepEqual(firstIDs, secondIDs) || !reflect.DeepEqual(firstReasons, secondReasons) {
		t.Fatalf("provider completion order changed result: ids %v/%v reasons %v/%v", firstIDs, secondIDs, firstReasons, secondReasons)
	}
	if !reflect.DeepEqual(firstReasons, []string{domainrecommendation.RecallProviderFresh, domainrecommendation.RecallProviderHot}) {
		t.Fatalf("duplicate reasons were not canonical: %v", firstReasons)
	}
}
