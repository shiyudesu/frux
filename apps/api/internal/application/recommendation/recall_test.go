package applicationrecommendation

import (
	domainrecommendation "github.com/shiyudesu/frux/internal/domain/recommendation"
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

type recallTestRepo struct {
	vectors   map[int64][]float64
	interest  []float64
	exposures map[int64]*domainrecommendation.Exposure
}

func (r *recallTestRepo) ListCandidatePool(context.Context, int64, int) ([]*domainrecommendation.Candidate, error) {
	return nil, nil
}

func (r *recallTestRepo) LoadUserInterestVector(context.Context, int64) ([]float64, bool, error) {
	return append([]float64(nil), r.interest...), len(r.interest) > 0, nil
}

func (r *recallTestRepo) LoadVideoVectors(_ context.Context, ids []int64) (map[int64][]float64, error) {
	output := map[int64][]float64{}
	for _, id := range ids {
		output[id] = append([]float64(nil), r.vectors[id]...)
	}
	return output, nil
}

func (r *recallTestRepo) ListRecentExposures(_ context.Context, _ int64, ids []int64, _ time.Time) ([]*domainrecommendation.Exposure, error) {
	output := make([]*domainrecommendation.Exposure, 0, len(ids))
	for _, id := range ids {
		if exposure := r.exposures[id]; exposure != nil {
			output = append(output, exposure)
		}
	}
	return output, nil
}

func (r *recallTestRepo) SaveExposures(context.Context, []*domainrecommendation.ExposureWrite) ([]*domainrecommendation.Exposure, error) {
	return nil, nil
}

func (*recallTestRepo) FindFeedbackByUserAndIdempotencyKey(context.Context, int64, string) (*domainrecommendation.Feedback, error) {
	return nil, domainrecommendation.ErrFeedbackNotFound
}

func (r *recallTestRepo) SaveFeedback(context.Context, *domainrecommendation.Feedback) (*domainrecommendation.Feedback, bool, error) {
	return nil, false, nil
}

func (*recallTestRepo) SaveServedCandidateEvidence(context.Context, *domainrecommendation.ServedCandidateEvidence) (bool, error) {
	return false, nil
}

func (*recallTestRepo) AppendServedCandidateEvidence(context.Context, *domainrecommendation.ServedCandidateEvidence) (bool, error) {
	return false, nil
}

func (*recallTestRepo) HasServedCandidateEvidence(context.Context, int64, string, int64, time.Time) (bool, error) {
	return false, nil
}

func (*recallTestRepo) DeleteServedCandidateEvidenceBefore(context.Context, time.Time, int) (domainrecommendation.ServedCandidateEvidenceCleanupResult, error) {
	return domainrecommendation.ServedCandidateEvidenceCleanupResult{}, nil
}

func (r *recallTestRepo) LoadVectors(_ context.Context, ids []int64, _ string) (map[int64][]float64, error) {
	return r.LoadVideoVectors(context.Background(), ids)
}

type recallTestCatalog struct {
	fresh     []*domainrecommendation.Candidate
	hot       []*domainrecommendation.Candidate
	authors   []*domainrecommendation.Candidate
	embedding map[string][]*domainrecommendation.Candidate
}

func (c recallTestCatalog) ListFreshCandidates(context.Context, int) ([]*domainrecommendation.Candidate, error) {
	return c.fresh, nil
}

func (c recallTestCatalog) ListHotCandidates(context.Context, int) ([]*domainrecommendation.Candidate, error) {
	return c.hot, nil
}

func (c recallTestCatalog) ListPublicCandidatesByAuthors(context.Context, []int64, int) ([]*domainrecommendation.Candidate, error) {
	return c.authors, nil
}

func (c recallTestCatalog) ListEmbeddingCandidates(_ context.Context, model string, _ int) ([]*domainrecommendation.Candidate, error) {
	return c.embedding[model], nil
}

type staticFollows []int64

func (f staticFollows) ListFollowedAuthorIDs(context.Context, int64, int) ([]int64, error) {
	return append([]int64(nil), f...), nil
}

func recallCandidate(id int64, author int64, hot int, published time.Time) *domainrecommendation.Candidate {
	return domainrecommendation.RestoreCandidate(id, author, 0, 0, hot, 0, "", published)
}

func TestRecallProvidersRespectOrderingAndBounds(t *testing.T) {
	now := time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC)
	catalog := recallTestCatalog{
		fresh:   []*domainrecommendation.Candidate{recallCandidate(3, 1, 0, now), recallCandidate(2, 1, 0, now.Add(-time.Minute)), recallCandidate(1, 1, 0, now.Add(-2*time.Minute))},
		hot:     []*domainrecommendation.Candidate{recallCandidate(2, 1, 20, now), recallCandidate(1, 1, 10, now)},
		authors: []*domainrecommendation.Candidate{recallCandidate(5, 8, 0, now), recallCandidate(4, 8, 0, now.Add(-time.Minute))},
		embedding: map[string][]*domainrecommendation.Candidate{
			"hash-ngram-v1": {recallCandidate(7, 2, 0, now), recallCandidate(6, 2, 0, now.Add(-time.Minute))},
		},
	}
	repo := &recallTestRepo{vectors: map[int64][]float64{6: {0.8, 0.2}, 7: {1, 0}}, interest: []float64{1, 0}, exposures: map[int64]*domainrecommendation.Exposure{}}
	request := RecallRequest{UserID: 9, Budget: 1, Now: now}

	expectedVideoIDs := map[string]int64{
		domainrecommendation.RecallProviderFresh:             3,
		domainrecommendation.RecallProviderHot:               2,
		domainrecommendation.RecallProviderFollowedAuthor:    5,
		domainrecommendation.RecallProviderContentSimilarity: 7,
	}
	for _, provider := range []RecallProvider{
		NewFreshContentProvider(catalog),
		NewHotContentProvider(catalog),
		NewFollowedAuthorProvider(staticFollows{8}, catalog),
		NewContentSimilarityProvider(catalog, repo, repo, "unavailable-model"),
	} {
		candidates, err := provider.Recall(context.Background(), request)
		if err != nil {
			t.Fatalf("%s: %v", provider.Name(), err)
		}
		if len(candidates) != 1 {
			t.Fatalf("%s returned %d candidates, want bounded result", provider.Name(), len(candidates))
		}
		if candidates[0].VideoID != expectedVideoIDs[provider.Name()] {
			t.Fatalf("%s returned video %d, want %d", provider.Name(), candidates[0].VideoID, expectedVideoIDs[provider.Name()])
		}
		if len(candidates[0].RecallReasons) != 1 || candidates[0].RecallReasons[0].Provider != provider.Name() {
			t.Fatalf("%s did not retain recall metadata: %#v", provider.Name(), candidates[0].RecallReasons)
		}
	}
}

type providerFunc struct {
	name string
	run  func(context.Context, RecallRequest) ([]*domainrecommendation.Candidate, error)
}

func (p providerFunc) Name() string { return p.name }

func (p providerFunc) Recall(ctx context.Context, request RecallRequest) ([]*domainrecommendation.Candidate, error) {
	return p.run(ctx, request)
}

type visibilityCatalog struct {
	visible map[int64]*domainrecommendation.Candidate
}

func (c visibilityCatalog) ListVisibleCandidates(_ context.Context, ids []int64) ([]*domainrecommendation.Candidate, error) {
	output := make([]*domainrecommendation.Candidate, 0, len(ids))
	for _, id := range ids {
		if candidate := c.visible[id]; candidate != nil {
			output = append(output, candidate.Clone())
		}
	}
	return output, nil
}

func TestRecallExecutionMergesDegradesFiltersAndBounds(t *testing.T) {
	now := time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC)
	candidate1 := recallCandidate(1, 1, 3, now)
	candidate2 := recallCandidate(2, 2, 2, now.Add(-time.Minute))
	candidate3 := recallCandidate(3, 3, 1, now.Add(-2*time.Minute))
	repo := &recallTestRepo{
		vectors:  map[int64][]float64{1: {1, 0}, 2: {1, 0}, 3: {1, 0}},
		interest: []float64{1, 0},
		exposures: map[int64]*domainrecommendation.Exposure{
			2: domainrecommendation.RestoreExposure(1, 9, 2, now, now, 1, "recommend"),
		},
	}
	service := New(
		repo,
		WithNow(func() time.Time { return now }),
		WithCandidateVisibilityFilter(visibilityCatalog{visible: map[int64]*domainrecommendation.Candidate{1: candidate1, 2: candidate2}}),
		WithRecallProviders(
			providerFunc{name: domainrecommendation.RecallProviderFresh, run: func(context.Context, RecallRequest) ([]*domainrecommendation.Candidate, error) {
				return []*domainrecommendation.Candidate{annotateCandidate(candidate1, domainrecommendation.RecallProviderFresh, 1), annotateCandidate(candidate2, domainrecommendation.RecallProviderFresh, 1), annotateCandidate(candidate3, domainrecommendation.RecallProviderFresh, 1)}, nil
			}},
			providerFunc{name: domainrecommendation.RecallProviderHot, run: func(context.Context, RecallRequest) ([]*domainrecommendation.Candidate, error) {
				return []*domainrecommendation.Candidate{annotateCandidate(candidate1, domainrecommendation.RecallProviderHot, 3)}, nil
			}},
			providerFunc{name: domainrecommendation.RecallProviderContentSimilarity, run: func(context.Context, RecallRequest) ([]*domainrecommendation.Candidate, error) {
				time.Sleep(500 * time.Millisecond)
				return nil, errors.New("late provider result")
			}},
		),
	)

	result, err := service.Recommend(context.Background(), CandidateRequest{UserID: 9, Scene: "recommend", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Degraded || len(result.DegradedProviders) != 1 || result.DegradedProviders[0].Reason != "timeout" {
		t.Fatalf("expected timeout degradation, got %#v", result.DegradedProviders)
	}
	if len(result.Candidates) != 1 || result.Candidates[0].VideoID != 1 {
		t.Fatalf("want only visible non-exposed candidate, got %#v", result.Candidates)
	}
	if len(result.Candidates[0].RecallReasons) != 2 || len(result.Candidates[0].SourceScores) != 2 {
		t.Fatalf("duplicate metadata was not merged: %#v %#v", result.Candidates[0].RecallReasons, result.Candidates[0].SourceScores)
	}

	_, err = New(repo, WithRecallProviders(providerFunc{name: domainrecommendation.RecallProviderFresh, run: func(context.Context, RecallRequest) ([]*domainrecommendation.Candidate, error) {
		return nil, errors.New("unavailable")
	}})).Recommend(context.Background(), CandidateRequest{UserID: 9, Scene: "recommend", Limit: 1})
	if !errors.Is(err, ErrLoadRecommendationFailed) {
		t.Fatalf("all failed providers should fail request, got %v", err)
	}
}

func TestSessionContinuationUsesBoundedContextSeeds(t *testing.T) {
	now := time.Now().UTC()
	catalog := recallTestCatalog{embedding: map[string][]*domainrecommendation.Candidate{
		"hash-ngram-v1": {recallCandidate(11, 2, 0, now), recallCandidate(10, 2, 0, now)},
	}}
	repo := &recallTestRepo{vectors: map[int64][]float64{9: {1, 0}, 10: {1, 0}, 11: {0, 1}}, exposures: map[int64]*domainrecommendation.Exposure{}}
	contextValue, err := domainrecommendation.NewRecommendationContext(domainrecommendation.RecommendationContextInput{CurrentVideoID: 9})
	if err != nil {
		t.Fatal(err)
	}

	candidates, err := NewSessionContinuationProvider(catalog, repo).Recall(context.Background(), RecallRequest{Context: contextValue, Budget: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || candidates[0].VideoID != 10 || candidates[0].RecallReasons[0].Provider != domainrecommendation.RecallProviderSessionContinuation {
		t.Fatalf("unexpected session continuation: %#v", candidates)
	}
}

func TestRecallExecutionBoundsTotalPool(t *testing.T) {
	now := time.Now().UTC()
	repo := &recallTestRepo{exposures: map[int64]*domainrecommendation.Exposure{}}
	candidates := []*domainrecommendation.Candidate{
		annotateCandidate(recallCandidate(3, 1, 0, now), domainrecommendation.RecallProviderFresh, 1),
		annotateCandidate(recallCandidate(2, 1, 0, now.Add(-time.Minute)), domainrecommendation.RecallProviderFresh, 1),
		annotateCandidate(recallCandidate(1, 1, 0, now.Add(-2*time.Minute)), domainrecommendation.RecallProviderFresh, 1),
	}
	service := New(
		repo,
		WithCandidateVisibilityFilter(visibilityCatalog{visible: map[int64]*domainrecommendation.Candidate{1: candidates[2], 2: candidates[1], 3: candidates[0]}}),
		WithRecallProviders(providerFunc{name: domainrecommendation.RecallProviderFresh, run: func(context.Context, RecallRequest) ([]*domainrecommendation.Candidate, error) {
			return candidates, nil
		}}),
	)
	request, err := domainrecommendation.NewCandidateRequest(9, "recommend", "", nil, 10, nil)
	if err != nil {
		t.Fatal(err)
	}
	execution, err := service.recallCandidates(context.Background(), request, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(execution.candidates) != 2 || execution.candidates[0].VideoID != 3 || execution.candidates[1].VideoID != 2 {
		t.Fatalf("total pool must be bounded in deterministic order: %#v", execution.candidates)
	}
}

func TestRecallProviderSlotsBoundContextIgnoringCalls(t *testing.T) {
	now := time.Date(2026, 7, 27, 6, 0, 0, 0, time.UTC)
	release := make(chan struct{})
	defer close(release)
	var blockingCalls atomic.Int32
	blocking := providerFunc{
		name: domainrecommendation.RecallProviderFresh,
		run: func(context.Context, RecallRequest) ([]*domainrecommendation.Candidate, error) {
			blockingCalls.Add(1)
			<-release
			return nil, nil
		},
	}
	policy := defaultRecommendationPolicy()
	policy.Config.ProviderDeadlinesMS[domainrecommendation.RecallProviderFresh] = 10
	service := New(
		&recallTestRepo{exposures: map[int64]*domainrecommendation.Exposure{}},
		WithNow(func() time.Time { return now }),
		WithRecallProviderSlots(1),
		WithRecallProviders(blocking),
	)
	request, err := domainrecommendation.NewCandidateRequest(7, "recommend", "slot-bound", nil, 1, nil)
	if err != nil {
		t.Fatal(err)
	}

	first, err := service.recallCandidates(context.Background(), request, 10, policy)
	if !errors.Is(err, ErrLoadRecommendationFailed) || first != nil {
		t.Fatalf("initial timed-out provider should preserve the existing all-provider failure: %#v, %v", first, err)
	}
	if blockingCalls.Load() != 1 {
		t.Fatalf("expected one detached blocking provider call, got %d", blockingCalls.Load())
	}

	for range 5 {
		next, err := service.recallCandidates(context.Background(), request, 10, policy)
		if err != nil || len(next.degraded) != 1 {
			t.Fatalf("capacity-limited retry should degrade without starting another call: %#v, %v", next, err)
		}
		for _, degradation := range next.degraded {
			if degradation.Reason != "capacity" {
				t.Fatalf("retry started work instead of degrading: %#v", next.degraded)
			}
		}
	}
	if blockingCalls.Load() != 1 {
		t.Fatalf("context-ignoring provider escaped the service slot bound: %d calls", blockingCalls.Load())
	}
	result, err := service.Recommend(context.Background(), CandidateRequest{
		UserID: 7, Scene: "recommend", RequestID: "slot-bound-response", Limit: 1,
	})
	if err != nil || !result.Degraded || len(result.DegradedProviders) != 1 ||
		result.DegradedProviders[0].Reason != "capacity" {
		t.Fatalf("slot exhaustion did not produce a degraded fallback response: %#v, %v", result, err)
	}
}
