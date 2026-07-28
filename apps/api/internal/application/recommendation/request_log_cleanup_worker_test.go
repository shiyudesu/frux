package applicationrecommendation

import (
	domainrecommendation "GCFeed/internal/domain/recommendation"
	"context"
	"testing"
	"time"
)

type requestLogCleanupPolicyReaderStub struct {
	policies map[string][]*domainrecommendation.Policy
}

func (s *requestLogCleanupPolicyReaderStub) ListPolicies(_ context.Context, scene string) ([]*domainrecommendation.Policy, error) {
	return s.policies[scene], nil
}

type requestLogCleanupStoreStub struct {
	calls   []requestLogCleanupCall
	deleted []int64
}

type requestLogCleanupCall struct {
	scene   string
	version int
	cutoff  time.Time
	limit   int
}

type servedCandidateEvidenceCleanupStoreStub struct {
	requestLogCleanupStoreStub
	evidenceCalls   []requestLogCleanupCall
	evidenceDeleted []domainrecommendation.ServedCandidateEvidenceCleanupResult
}

func (s *servedCandidateEvidenceCleanupStoreStub) DeleteServedCandidateEvidenceBefore(_ context.Context, cutoff time.Time, limit int) (domainrecommendation.ServedCandidateEvidenceCleanupResult, error) {
	s.evidenceCalls = append(s.evidenceCalls, requestLogCleanupCall{cutoff: cutoff, limit: limit})
	if len(s.evidenceDeleted) > 0 {
		result := s.evidenceDeleted[0]
		s.evidenceDeleted = s.evidenceDeleted[1:]
		return result, nil
	}
	return domainrecommendation.ServedCandidateEvidenceCleanupResult{RequestGroups: 1, CandidateRows: 1}, nil
}

func (s *requestLogCleanupStoreStub) DeleteRequestLogsForPolicyBefore(_ context.Context, scene string, version int, cutoff time.Time, limit int) (int64, error) {
	s.calls = append(s.calls, requestLogCleanupCall{scene: scene, version: version, cutoff: cutoff, limit: limit})
	if len(s.deleted) > 0 {
		deleted := s.deleted[0]
		s.deleted = s.deleted[1:]
		return deleted, nil
	}
	return 1, nil
}

func TestRequestLogCleanupWorkerUsesExactPersistedPolicyRetention(t *testing.T) {
	now := time.Date(2026, 7, 27, 3, 0, 0, 0, time.UTC)
	first, err := domainrecommendation.NewPolicy("recommend", 1, true, requestLogCleanupPolicyConfig(30), now)
	if err != nil {
		t.Fatal(err)
	}

	second, err := domainrecommendation.NewPolicy("recommend", 2, false, requestLogCleanupPolicyConfig(7), now)
	if err != nil {
		t.Fatal(err)
	}
	store := &requestLogCleanupStoreStub{}
	worker := NewRequestLogCleanupWorker(&requestLogCleanupPolicyReaderStub{
		policies: map[string][]*domainrecommendation.Policy{"recommend": {first, second}},
	}, store)
	worker.now = func() time.Time { return now }
	worker.batch = 17

	deleted, err := worker.DispatchOnce(context.Background())
	if err != nil || deleted != 2 || len(store.calls) != 2 {
		t.Fatalf("cleanup deleted=%d calls=%#v err=%v", deleted, store.calls, err)
	}
	if call := store.calls[0]; call.scene != "recommend" || call.version != 1 || !call.cutoff.Equal(now.AddDate(0, 0, -30)) || call.limit != 17 {
		t.Fatalf("first cleanup call = %#v", call)
	}
	if call := store.calls[1]; call.scene != "recommend" || call.version != 2 || !call.cutoff.Equal(now.AddDate(0, 0, -7)) || call.limit != 17 {
		t.Fatalf("second cleanup call = %#v", call)
	}
}

func TestRequestLogCleanupWorkerDrainsMultipleBoundedBatchesPerPolicy(t *testing.T) {
	now := time.Date(2026, 7, 27, 3, 0, 0, 0, time.UTC)
	policy, err := domainrecommendation.NewPolicy("recommend", 1, true, requestLogCleanupPolicyConfig(30), now)
	if err != nil {
		t.Fatal(err)
	}

	store := &requestLogCleanupStoreStub{deleted: []int64{10, 10, 4}}
	worker := NewRequestLogCleanupWorker(&requestLogCleanupPolicyReaderStub{
		policies: map[string][]*domainrecommendation.Policy{"recommend": {policy}},
	}, store)
	worker.now = func() time.Time { return now }
	worker.batch = 10
	worker.maxBatches = 2
	worker.maxRuntime = time.Second

	deleted, err := worker.DispatchOnce(context.Background())
	if err != nil || deleted != 20 || len(store.calls) != 2 {
		t.Fatalf("bounded drain deleted=%d calls=%#v err=%v", deleted, store.calls, err)
	}

	store = &requestLogCleanupStoreStub{deleted: []int64{10, 10, 4}}
	worker.store = store
	worker.maxBatches = 20
	deleted, err = worker.DispatchOnce(context.Background())
	if err != nil || deleted != 24 || len(store.calls) != 3 {
		t.Fatalf("multi-batch drain deleted=%d calls=%#v err=%v", deleted, store.calls, err)
	}
}

func TestRequestLogCleanupWorkerSharesBatchAllowanceAcrossBackloggedPolicies(t *testing.T) {
	now := time.Date(2026, 7, 27, 3, 0, 0, 0, time.UTC)
	policies := make([]*domainrecommendation.Policy, 0, 3)
	for _, version := range []int{3, 2, 1} {
		policy, err := domainrecommendation.NewPolicy("recommend", version, version == 3, requestLogCleanupPolicyConfig(30), now)
		if err != nil {
			t.Fatal(err)
		}
		policies = append(policies, policy)
	}
	store := &requestLogCleanupStoreStub{deleted: []int64{10, 10, 10}}
	worker := NewRequestLogCleanupWorker(&requestLogCleanupPolicyReaderStub{
		policies: map[string][]*domainrecommendation.Policy{"recommend": policies},
	}, store)
	worker.now = func() time.Time { return now }
	worker.batch = 10
	worker.maxBatches = 3
	worker.maxRuntime = time.Second

	if deleted, err := worker.DispatchOnce(context.Background()); err != nil || deleted != 30 {
		t.Fatalf("fair cleanup deleted=%d err=%v", deleted, err)
	}
	if len(store.calls) != 3 {
		t.Fatalf("cleanup calls=%#v", store.calls)
	}
	for index, version := range []int{3, 2, 1} {
		if call := store.calls[index]; call.version != version || call.limit != 10 {
			t.Fatalf("cleanup call %d = %#v, want version %d", index, call, version)
		}
	}
}

func TestRequestLogCleanupWorkerRotatesPoliciesWhenAllowanceIsSmaller(t *testing.T) {
	now := time.Date(2026, 7, 27, 3, 0, 0, 0, time.UTC)
	first, err := domainrecommendation.NewPolicy("recommend", 2, true, requestLogCleanupPolicyConfig(30), now)
	if err != nil {
		t.Fatal(err)
	}
	second, err := domainrecommendation.NewPolicy("recommend", 1, false, requestLogCleanupPolicyConfig(30), now)
	if err != nil {
		t.Fatal(err)
	}
	store := &requestLogCleanupStoreStub{deleted: []int64{10, 10}}
	worker := NewRequestLogCleanupWorker(&requestLogCleanupPolicyReaderStub{
		policies: map[string][]*domainrecommendation.Policy{"recommend": {first, second}},
	}, store)
	worker.now = func() time.Time { return now }
	worker.batch = 10
	worker.maxBatches = 1
	worker.maxRuntime = time.Second

	if _, err := worker.DispatchOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := worker.DispatchOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(store.calls) != 2 || store.calls[0].version != 2 || store.calls[1].version != 1 {
		t.Fatalf("rotating cleanup calls=%#v", store.calls)
	}
}

func TestRequestLogCleanupWorkerAlsoBoundsServedCandidateEvidenceCleanup(t *testing.T) {
	now := time.Date(2026, 7, 27, 3, 0, 0, 0, time.UTC)
	policy, err := domainrecommendation.NewPolicy("recommend", 1, true, requestLogCleanupPolicyConfig(30), now)
	if err != nil {
		t.Fatal(err)
	}
	store := &servedCandidateEvidenceCleanupStoreStub{}
	worker := NewRequestLogCleanupWorker(&requestLogCleanupPolicyReaderStub{
		policies: map[string][]*domainrecommendation.Policy{"recommend": {policy}},
	}, store)
	worker.now = func() time.Time { return now }
	worker.batch = 13

	if deleted, err := worker.DispatchOnce(context.Background()); err != nil || deleted != 2 {
		t.Fatalf("cleanup deleted=%d err=%v", deleted, err)
	}
	if len(store.evidenceCalls) != 1 || !store.evidenceCalls[0].cutoff.Equal(domainrecommendation.ServedCandidateEvidenceCleanupCutoff(now)) || store.evidenceCalls[0].limit != 13 {
		t.Fatalf("served-candidate evidence cleanup was not bounded by expiry: %#v", store.evidenceCalls)
	}
}

func TestRequestLogCleanupWorkerDrainsLargeEvidenceRequestGroupsAcrossBatches(t *testing.T) {
	now := time.Date(2026, 7, 27, 3, 0, 0, 0, time.UTC)
	store := &servedCandidateEvidenceCleanupStoreStub{
		evidenceDeleted: []domainrecommendation.ServedCandidateEvidenceCleanupResult{
			{RequestGroups: 2, CandidateRows: 1_200},
			{RequestGroups: 2, CandidateRows: 1_100},
		},
	}
	worker := NewRequestLogCleanupWorker(&requestLogCleanupPolicyReaderStub{}, store)
	worker.now = func() time.Time { return now }
	worker.batch = 2
	worker.maxBatches = 2

	deleted, err := worker.DispatchOnce(context.Background())
	if err != nil || deleted != 2_300 {
		t.Fatalf("large-group cleanup deleted=%d err=%v", deleted, err)
	}
	if len(store.evidenceCalls) != 2 {
		t.Fatalf("evidence cleanup batches=%#v, want two request-group batches", store.evidenceCalls)
	}
	for _, call := range store.evidenceCalls {
		if call.limit != 2 || !call.cutoff.Equal(domainrecommendation.ServedCandidateEvidenceCleanupCutoff(now)) {
			t.Fatalf("unexpected request-group cleanup call: %#v", call)
		}
	}
}

func requestLogCleanupPolicyConfig(retentionDays int) domainrecommendation.PolicyConfiguration {
	return domainrecommendation.PolicyConfiguration{
		FeatureWeights:         map[string]float64{domainrecommendation.FeatureHotness: 1},
		RecallBudgets:          map[string]int{domainrecommendation.RecallProviderFresh: 10},
		ProviderDeadlinesMS:    map[string]int{domainrecommendation.RecallProviderFresh: 100},
		FreshnessHalfLifeHours: 24,
		ExposureWindowHours:    24,
		Diversity:              domainrecommendation.DiversityRules{MaxPerAuthor: 1, MinAuthorGap: 0},
		RolloutPercentage:      100,
		SnapshotTTLSeconds:     60,
		SamplingRatePPM:        0,
		RetentionDays:          retentionDays,
	}
}
