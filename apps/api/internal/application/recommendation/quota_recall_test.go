package applicationrecommendation

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"sync"
	"testing"
	"time"

	domainrecommendation "github.com/shiyudesu/frux/internal/domain/recommendation"
)

type quotaVisibilityCatalog struct {
	visible map[int64]*domainrecommendation.Candidate
	err     error
	calls   int
	ids     []int64
	batches [][]int64
}

func (c *quotaVisibilityCatalog) ListVisibleCandidates(_ context.Context, ids []int64) ([]*domainrecommendation.Candidate, error) {
	c.calls++
	c.ids = append([]int64(nil), ids...)
	c.batches = append(c.batches, append([]int64(nil), ids...))
	if c.err != nil {
		return nil, c.err
	}
	output := make([]*domainrecommendation.Candidate, 0, len(ids))
	for _, id := range ids {
		if candidate := c.visible[id]; candidate != nil {
			output = append(output, candidate.Clone())
		}
	}
	return output, nil
}

func quotaRecallPolicy(t testing.TB, version int, enabled bool, budgets map[string]int, limit int, order []string, reservations map[string]int) *domainrecommendation.Policy {
	t.Helper()
	config := defaultRecommendationPolicyConfiguration()
	config.RecallBudgets = make(map[string]int, len(budgets))
	config.ProviderDeadlinesMS = make(map[string]int, len(budgets))
	for provider, budget := range budgets {
		config.RecallBudgets[provider] = budget
		config.ProviderDeadlinesMS[provider] = 250
	}
	config.PreRankPoolLimit = limit
	config.RecallProviderOrder = append([]string(nil), order...)
	config.RecallProviderReservations = make(map[string]int, len(reservations))
	for provider, reservation := range reservations {
		config.RecallProviderReservations[provider] = reservation
	}
	config.HardSuppressExposures = false
	policy, err := domainrecommendation.NewPolicy("recommend", version, enabled, config, time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	return policy
}

func TestQuotaRecallFiltersFullSupersetBeforeReservation(t *testing.T) {
	now := time.Date(2026, 8, 20, 2, 0, 0, 0, time.UTC)
	fresh := domainrecommendation.RecallProviderFresh
	hot := domainrecommendation.RecallProviderHot
	policy := quotaRecallPolicy(t, 3, true,
		map[string]int{fresh: 50, hot: 50}, 50, []string{fresh, hot}, map[string]int{fresh: 2, hot: 0},
	)
	providerCandidates := map[string][]*domainrecommendation.Candidate{
		fresh: {
			quotaCandidate(1, fresh, 3, now),
			quotaCandidate(2, fresh, 2, now.Add(-time.Minute)),
			quotaCandidate(3, fresh, 1, now.Add(-2*time.Minute)),
		},
		hot: {quotaCandidate(4, hot, 1, now.Add(-3*time.Minute))},
	}
	visibility := &quotaVisibilityCatalog{visible: map[int64]*domainrecommendation.Candidate{
		2: recallCandidate(2, 202, 20, now.Add(-10*time.Minute)),
		3: recallCandidate(3, 203, 30, now.Add(-11*time.Minute)),
		4: recallCandidate(4, 204, 40, now.Add(-12*time.Minute)),
	}}
	service := New(
		&rankerTestRepo{},
		WithNow(func() time.Time { return now }),
		WithCandidateVisibilityFilter(visibility),
		WithRecallProviders(
			providerFunc{name: fresh, run: func(context.Context, RecallRequest) ([]*domainrecommendation.Candidate, error) {
				return providerCandidates[fresh], nil
			}},
			providerFunc{name: hot, run: func(context.Context, RecallRequest) ([]*domainrecommendation.Candidate, error) {
				return providerCandidates[hot], nil
			}},
		),
	)
	execution, err := service.recallCandidates(context.Background(), &domainrecommendation.CandidateRequest{
		UserID: 9, Scene: "recommend",
	}, 500, policy)
	if err != nil {
		t.Fatal(err)
	}
	if ids := candidateIDs(execution.candidates); !reflect.DeepEqual(ids, []int64{2, 3, 4}) {
		t.Fatalf("mixed IDs = %v, want [2 3 4]", ids)
	}
	if visibility.calls != 1 || len(visibility.ids) != 4 {
		t.Fatalf("visibility batch calls=%d ids=%v, want one full four-ID batch", visibility.calls, visibility.ids)
	}
	if got := execution.candidates[0]; got.AuthorID != 202 || got.HotScore != 20 || !got.PublishedAt.Equal(now.Add(-10*time.Minute)) {
		t.Fatalf("current visible facts were not applied: %#v", got)
	}
}

func TestQuotaRecallCompletionOrderDoesNotChangePool(t *testing.T) {
	now := time.Date(2026, 8, 20, 3, 0, 0, 0, time.UTC)
	fresh := domainrecommendation.RecallProviderFresh
	hot := domainrecommendation.RecallProviderHot
	policy := quotaRecallPolicy(t, 4, true,
		map[string]int{fresh: 60, hot: 60}, 50, []string{fresh, hot}, map[string]int{fresh: 10, hot: 10},
	)
	providerCandidates := map[string][]*domainrecommendation.Candidate{fresh: {}, hot: {}}
	visible := map[int64]*domainrecommendation.Candidate{}
	for index := range 60 {
		freshID := int64(index + 1)
		hotID := int64(index + 1001)
		providerCandidates[fresh] = append(providerCandidates[fresh], quotaCandidate(freshID, fresh, float64(60-index), now.Add(-time.Duration(index)*time.Second)))
		providerCandidates[hot] = append(providerCandidates[hot], quotaCandidate(hotID, hot, float64(60-index), now.Add(-time.Duration(index)*time.Second)))
		visible[freshID] = recallCandidate(freshID, freshID, index, now)
		visible[hotID] = recallCandidate(hotID, hotID, index, now)
	}
	run := func(freshDelay, hotDelay time.Duration) []int64 {
		service := New(
			&rankerTestRepo{},
			WithCandidateVisibilityFilter(&quotaVisibilityCatalog{visible: visible}),
			WithRecallProviders(
				providerFunc{name: fresh, run: func(context.Context, RecallRequest) ([]*domainrecommendation.Candidate, error) {
					time.Sleep(freshDelay)
					return providerCandidates[fresh], nil
				}},
				providerFunc{name: hot, run: func(context.Context, RecallRequest) ([]*domainrecommendation.Candidate, error) {
					time.Sleep(hotDelay)
					return providerCandidates[hot], nil
				}},
			),
		)
		execution, err := service.recallCandidates(context.Background(), &domainrecommendation.CandidateRequest{UserID: 9, Scene: "recommend"}, 500, policy)
		if err != nil {
			t.Fatal(err)
		}
		return candidateIDs(execution.candidates)
	}
	first := run(4*time.Millisecond, 0)
	second := run(0, 4*time.Millisecond)
	if !reflect.DeepEqual(first, second) || len(first) != 50 {
		t.Fatalf("completion order changed pool: first=%v second=%v", first, second)
	}
}

func TestQuotaRecallWiderBudgetsSendOnlyMixedPoolToRanker(t *testing.T) {
	now := time.Date(2026, 8, 20, 4, 0, 0, 0, time.UTC)
	fresh := domainrecommendation.RecallProviderFresh
	hot := domainrecommendation.RecallProviderHot
	policy := quotaRecallPolicy(t, 5, true,
		map[string]int{fresh: 400, hot: 400}, 500, []string{fresh, hot}, map[string]int{fresh: 100, hot: 100},
	)
	policy.Config.SamplingRatePPM = domainrecommendation.MaxSamplingRatePPM
	providers := make([]RecallProvider, 0, 2)
	visible := make(map[int64]*domainrecommendation.Candidate, 800)
	vectors := make(map[int64][]float64, 800)
	budgets := map[string]int{}
	var budgetsMu sync.Mutex
	for providerIndex, provider := range []string{fresh, hot} {
		candidates := make([]*domainrecommendation.Candidate, 0, 400)
		for offset := 1; offset <= 400; offset++ {
			id := int64(providerIndex*1000 + offset)
			candidate := recallCandidate(id, id, 800-offset, now.Add(-time.Duration(id)*time.Second))
			candidates = append(candidates, annotateCandidate(candidate, provider, float64(401-offset)))
			visible[id] = candidate
			vectors[id] = []float64{1}
		}
		providerName := provider
		providerCandidates := candidates
		providers = append(providers, providerFunc{name: providerName, run: func(_ context.Context, request RecallRequest) ([]*domainrecommendation.Candidate, error) {
			budgetsMu.Lock()
			budgets[providerName] = request.Budget
			budgetsMu.Unlock()
			return providerCandidates, nil
		}})
	}
	visibility := &quotaVisibilityCatalog{visible: visible}
	repo := &rankerTestRepo{vectors: vectors, features: emptyRankingFeatures(), captureFeatureIDs: true}
	logs := &memoryRequestLogRepository{}
	store := &memorySnapshotStore{}
	signer, err := NewHMACSnapshotCursorSigner("quota-merge-wide-policy-secret")
	if err != nil {
		t.Fatal(err)
	}
	service := New(
		repo,
		WithNow(func() time.Time { return now }),
		WithPolicySelector(rankerPolicySelector{policy: policy}),
		WithCandidateVisibilityFilter(visibility),
		WithRecallProviders(providers...),
		WithRequestLogRepository(logs),
		WithSnapshotPagination(store, signer),
	)
	result, err := service.Recommend(context.Background(), CandidateRequest{
		UserID: 9, Scene: "recommend", RequestID: "quota-wide", Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	budgetsMu.Lock()
	observedBudgets := map[string]int{fresh: budgets[fresh], hot: budgets[hot]}
	budgetsMu.Unlock()
	if len(result.Candidates) != 10 || observedBudgets[fresh] != 400 || observedBudgets[hot] != 400 {
		t.Fatalf("response=%d budgets=%v", len(result.Candidates), budgets)
	}
	repo.featureMu.Lock()
	featureIDs := append([]int64(nil), repo.featureVideoIDs...)
	repo.featureMu.Unlock()
	batchSizes := make([]int, 0, len(visibility.batches))
	for _, batch := range visibility.batches {
		batchSizes = append(batchSizes, len(batch))
	}
	if len(featureIDs) != 500 || !reflect.DeepEqual(batchSizes, []int{800, 500}) {
		t.Fatalf("ranker=%d visibility batches=%v", len(featureIDs), batchSizes)
	}
	sort.Slice(featureIDs, func(i, j int) bool { return featureIDs[i] < featureIDs[j] })
	for index := 1; index < len(featureIDs); index++ {
		if featureIDs[index] == featureIDs[index-1] {
			t.Fatalf("ranker received duplicate ID %d", featureIDs[index])
		}
	}
	loggedCandidates := 0
	if len(logs.logs) == 1 {
		loggedCandidates = len(logs.logs[0].Candidates)
	}
	if len(logs.logs) != 1 || loggedCandidates != 500 || len(store.snapshots) != 1 {
		t.Fatalf("bounded evidence changed: logs=%d logged=%d snapshots=%d", len(logs.logs), loggedCandidates, len(store.snapshots))
	}
	selectedDiagnostic := false
	for _, diagnostic := range logs.logs[0].RecallDiagnostics {
		if diagnostic.Phase == "final" && diagnostic.Provider == "all" && diagnostic.Result == "selected" && diagnostic.Count == 500 {
			selectedDiagnostic = true
		}
	}
	if !selectedDiagnostic || len(logs.logs[0].RecallDiagnostics) > domainrecommendation.MaxRequestLogRecallDiagnostics {
		t.Fatalf("quota diagnostics were missing or unbounded: %#v", logs.logs[0].RecallDiagnostics)
	}
	for _, snapshot := range store.snapshots {
		if len(snapshot.Candidates) != 500 {
			t.Fatalf("snapshot pool = %d, want 500", len(snapshot.Candidates))
		}
	}
}

func TestQuotaRecallVisibilityFailureUsesSafeLoadError(t *testing.T) {
	now := time.Date(2026, 8, 20, 5, 0, 0, 0, time.UTC)
	fresh := domainrecommendation.RecallProviderFresh
	policy := quotaRecallPolicy(t, 6, true,
		map[string]int{fresh: 50}, 50, []string{fresh}, map[string]int{fresh: 10},
	)
	service := New(
		&rankerTestRepo{},
		WithCandidateVisibilityFilter(&quotaVisibilityCatalog{err: errors.New("visibility unavailable")}),
		WithRecallProviders(providerFunc{name: fresh, run: func(context.Context, RecallRequest) ([]*domainrecommendation.Candidate, error) {
			return []*domainrecommendation.Candidate{quotaCandidate(1, fresh, 1, now)}, nil
		}}),
	)
	_, err := service.recallCandidates(context.Background(), &domainrecommendation.CandidateRequest{UserID: 9, Scene: "recommend"}, 500, policy)
	if !errors.Is(err, ErrLoadRecommendationFailed) {
		t.Fatalf("error = %v, want %v", err, ErrLoadRecommendationFailed)
	}
}
