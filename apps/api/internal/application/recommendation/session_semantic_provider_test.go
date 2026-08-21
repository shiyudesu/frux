package applicationrecommendation

import (
	"context"
	"errors"
	"math"
	"reflect"
	"testing"
	"time"

	domainembedding "github.com/shiyudesu/frux/internal/domain/embedding"
	domainrecommendation "github.com/shiyudesu/frux/internal/domain/recommendation"
)

type sessionSemanticInterestBuilderStub struct {
	interest *SessionSemanticInterest
	err      error
	request  SessionSemanticBuildRequest
	calls    int
}

func (s *sessionSemanticInterestBuilderStub) Build(
	_ context.Context,
	request SessionSemanticBuildRequest,
) (*SessionSemanticInterest, error) {
	s.calls++
	s.request = request
	return s.interest.Clone(), s.err
}

type sessionSemanticExactIndexStub struct {
	candidates []domainembedding.MultimodalExactCandidate
	err        error
	query      []float64
	exclusions []int64
	limit      int
	calls      int
	wait       bool
}

func (s *sessionSemanticExactIndexStub) ExactMultimodalSearch(
	ctx context.Context,
	_ domainembedding.MultimodalContractIdentity,
	query []float64,
	exclusions []int64,
	limit int,
) ([]domainembedding.MultimodalExactCandidate, error) {
	s.calls++
	s.query = append([]float64(nil), query...)
	s.exclusions = append([]int64(nil), exclusions...)
	s.limit = limit
	if s.wait {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return append([]domainembedding.MultimodalExactCandidate(nil), s.candidates...), s.err
}

func TestSemanticSessionProviderUsesExactConfidenceAndExclusions(t *testing.T) {
	now := time.Date(2026, 8, 21, 10, 0, 0, 0, time.UTC)
	contract := sessionSemanticTestContract(t, "provider-revision")
	policy := sessionSemanticRecommendationPolicy(t, contract, 4, false)
	evidence := sessionSemanticSuccessEvidence(t, contract, 0.5)
	builder := &sessionSemanticInterestBuilderStub{interest: &SessionSemanticInterest{
		Vector: sessionSemanticUnitVector(contract.Dimension, 0, 1), Confidence: 0.5,
		Band:       domainrecommendation.SessionSemanticConfidenceMedium,
		Exclusions: []int64{1, 2}, OutputLimit: 2, Evidence: evidence,
	}}
	exact := &sessionSemanticExactIndexStub{candidates: []domainembedding.MultimodalExactCandidate{
		{VideoID: 10, Similarity: 0.9, PublishedAt: now},
		{VideoID: 11, Similarity: 0.8, PublishedAt: now.Add(-time.Minute)},
		{VideoID: 12, Similarity: 0.7, PublishedAt: now.Add(-2 * time.Minute)},
	}}
	provider, err := NewSemanticSessionProvider(builder, exact, contract)
	if err != nil {
		t.Fatal(err)
	}
	candidates, gotEvidence, err := provider.RecallWithSessionSemanticEvidence(context.Background(), RecallRequest{
		UserID: 7, Scene: "recommend", Context: sessionSemanticContext(t, 1, []int64{2}),
		Budget: 4, Now: now, Policy: policy,
	})
	if err != nil || len(candidates) != 2 || gotEvidence == nil || gotEvidence.Confidence != 0.5 ||
		exact.calls != 1 || exact.limit != 2 || !reflect.DeepEqual(exact.exclusions, []int64{1, 2}) ||
		builder.calls != 1 || builder.request.Policy.ContractKey != contract.Key() {
		t.Fatalf("candidates=%#v evidence=%#v builder=%#v exact=%#v err=%v", candidates, gotEvidence, builder, exact, err)
	}
	if candidates[0].RecallReasons[0].Provider != domainrecommendation.RecallProviderSemanticSession ||
		math.Abs(candidates[0].SourceScores[domainrecommendation.RecallProviderSemanticSession]-0.45) > 1e-9 ||
		math.Abs(candidates[1].SourceScores[domainrecommendation.RecallProviderSemanticSession]-0.4) > 1e-9 {
		t.Fatalf("candidates=%#v", candidates)
	}
}

func TestSemanticSessionProviderHealthyUnavailableAndFailures(t *testing.T) {
	now := time.Date(2026, 8, 21, 10, 0, 0, 0, time.UTC)
	contract := sessionSemanticTestContract(t, "provider-revision")
	policy := sessionSemanticRecommendationPolicy(t, contract, 4, false)
	unavailable, err := domainrecommendation.NewSessionSemanticEvidence(domainrecommendation.SessionSemanticEvidence{
		BuilderVersion: domainrecommendation.SessionSemanticBuilderV1, ContractKey: contract.Key(),
		Result:         domainrecommendation.SessionSemanticResultLowConfidence,
		ConfidenceBand: domainrecommendation.SessionSemanticConfidenceNone,
	})
	if err != nil {
		t.Fatal(err)
	}
	builder := &sessionSemanticInterestBuilderStub{interest: &SessionSemanticInterest{Evidence: unavailable}}
	exact := &sessionSemanticExactIndexStub{}
	provider, _ := NewSemanticSessionProvider(builder, exact, contract)
	candidates, evidence, err := provider.RecallWithSessionSemanticEvidence(context.Background(), RecallRequest{
		UserID: 7, Context: sessionSemanticContext(t, 1, nil), Budget: 4, Now: now, Policy: policy,
	})
	if err != nil || len(candidates) != 0 || evidence.Result != domainrecommendation.SessionSemanticResultLowConfidence || exact.calls != 0 {
		t.Fatalf("candidates=%#v evidence=%#v exact=%d err=%v", candidates, evidence, exact.calls, err)
	}
	infrastructure := errors.New("exact unavailable")
	builder.interest = &SessionSemanticInterest{
		Vector: sessionSemanticUnitVector(contract.Dimension, 0, 1), Confidence: 1,
		Band: domainrecommendation.SessionSemanticConfidenceHigh, OutputLimit: 4,
		Evidence: sessionSemanticSuccessEvidence(t, contract, 1),
	}
	exact.err = infrastructure
	_, evidence, err = provider.RecallWithSessionSemanticEvidence(context.Background(), RecallRequest{
		UserID: 7, Context: sessionSemanticContext(t, 1, nil), Budget: 4, Now: now, Policy: policy,
	})
	if !errors.Is(err, infrastructure) || evidence == nil || evidence.Result != domainrecommendation.SessionSemanticResultUnavailable {
		t.Fatalf("evidence=%#v err=%v", evidence, err)
	}
	mismatch := policy.Clone()
	mismatch.Config.SessionSemantic.ContractKey = sessionSemanticTestContract(t, "other").Key()
	_, evidence, err = provider.RecallWithSessionSemanticEvidence(context.Background(), RecallRequest{
		UserID: 7, Context: sessionSemanticContext(t, 1, nil), Budget: 4, Now: now, Policy: mismatch,
	})
	if err != nil || evidence == nil || evidence.Result != domainrecommendation.SessionSemanticResultContractMismatch {
		t.Fatalf("evidence=%#v err=%v", evidence, err)
	}
}

func TestSessionSemanticTimeoutDegradesOnlyItsProvider(t *testing.T) {
	now := time.Date(2026, 8, 21, 10, 0, 0, 0, time.UTC)
	contract := sessionSemanticTestContract(t, "provider-revision")
	policy := sessionSemanticRecommendationPolicy(t, contract, 4, false)
	policy.Config.RecallBudgets[domainrecommendation.RecallProviderFresh] = 4
	policy.Config.ProviderDeadlinesMS[domainrecommendation.RecallProviderFresh] = 50
	policy.Config.ProviderDeadlinesMS[domainrecommendation.RecallProviderSemanticSession] = 10
	builder := &sessionSemanticInterestBuilderStub{interest: &SessionSemanticInterest{
		Vector: sessionSemanticUnitVector(contract.Dimension, 0, 1), Confidence: 1,
		Band: domainrecommendation.SessionSemanticConfidenceHigh, OutputLimit: 4,
		Evidence: sessionSemanticSuccessEvidence(t, contract, 1),
	}}
	semantic, _ := NewSemanticSessionProvider(builder, &sessionSemanticExactIndexStub{wait: true}, contract)
	fresh := annotateCandidate(recallCandidate(2, 2, 1, now), domainrecommendation.RecallProviderFresh, 1)
	service := New(
		&rankerTestRepo{}, WithNow(func() time.Time { return now }),
		WithRecallProviders(
			semantic,
			providerFunc{name: domainrecommendation.RecallProviderFresh, run: func(context.Context, RecallRequest) ([]*domainrecommendation.Candidate, error) {
				return []*domainrecommendation.Candidate{fresh}, nil
			}},
		),
	)
	execution, err := service.recallCandidates(context.Background(), &domainrecommendation.CandidateRequest{
		UserID: 7, Scene: "recommend", Context: sessionSemanticContext(t, 1, nil),
	}, 8, policy)
	if err != nil || execution == nil || execution.healthy != 1 || len(execution.candidates) != 1 ||
		execution.sessionSemantic == nil || execution.sessionSemantic.Result != domainrecommendation.SessionSemanticResultTimeout {
		t.Fatalf("execution=%#v err=%v", execution, err)
	}
	if len(execution.degraded) != 1 || execution.degraded[0].Provider != domainrecommendation.RecallProviderSemanticSession ||
		execution.degraded[0].Reason != "timeout" {
		t.Fatalf("degraded=%#v", execution.degraded)
	}
}

func TestSessionSemanticEvidencePropagatesThroughRecallAndQuotaUnderfill(t *testing.T) {
	now := time.Date(2026, 8, 21, 10, 0, 0, 0, time.UTC)
	contract := sessionSemanticTestContract(t, "provider-revision")
	policy := sessionSemanticRecommendationPolicy(t, contract, 10, true)
	evidence := sessionSemanticSuccessEvidence(t, contract, 0.3)
	builder := &sessionSemanticInterestBuilderStub{interest: &SessionSemanticInterest{
		Vector: sessionSemanticUnitVector(contract.Dimension, 0, 1), Confidence: 0.3,
		Band:       domainrecommendation.SessionSemanticConfidenceLow,
		Exclusions: []int64{1}, OutputLimit: 1, Evidence: evidence,
	}}
	exact := &sessionSemanticExactIndexStub{candidates: []domainembedding.MultimodalExactCandidate{
		{VideoID: 10, Similarity: 0.9, PublishedAt: now},
	}}
	semantic, _ := NewSemanticSessionProvider(builder, exact, contract)
	freshCandidates := make([]*domainrecommendation.Candidate, 0, 60)
	visible := map[int64]*domainrecommendation.Candidate{}
	for id := int64(10); id < 70; id++ {
		candidate := recallCandidate(id, id, int(id), now.Add(-time.Duration(id)*time.Second))
		visible[id] = candidate
		if id != 10 {
			freshCandidates = append(freshCandidates, annotateCandidate(candidate, domainrecommendation.RecallProviderFresh, float64(100-id)))
		}
	}
	service := New(
		&rankerTestRepo{}, WithNow(func() time.Time { return now }),
		WithCandidateVisibilityFilter(&quotaVisibilityCatalog{visible: visible}),
		WithRecallProviders(
			semantic,
			providerFunc{name: domainrecommendation.RecallProviderFresh, run: func(context.Context, RecallRequest) ([]*domainrecommendation.Candidate, error) {
				return freshCandidates, nil
			}},
		),
	)
	execution, err := service.recallCandidates(context.Background(), &domainrecommendation.CandidateRequest{
		UserID: 7, Scene: "recommend", Context: sessionSemanticContext(t, 1, nil),
	}, 50, policy)
	if err != nil || execution == nil || execution.sessionSemantic == nil || execution.sessionSemantic.Confidence != 0.3 ||
		len(execution.candidates) != 50 {
		t.Fatalf("execution=%#v err=%v", execution, err)
	}
	foundUnderfill := false
	for _, diagnostic := range execution.quotaDiagnostics {
		if diagnostic.Provider == domainrecommendation.RecallProviderSemanticSession &&
			diagnostic.Result == "underfill" && diagnostic.Count == 4 {
			foundUnderfill = true
		}
	}
	if !foundUnderfill {
		t.Fatalf("diagnostics=%#v", execution.quotaDiagnostics)
	}
}

func TestRankerUsesSemanticSimilarityWithoutChangingHashSession(t *testing.T) {
	now := time.Date(2026, 8, 21, 10, 0, 0, 0, time.UTC)
	contract := sessionSemanticTestContract(t, "provider-revision")
	policy := sessionSemanticRecommendationPolicy(t, contract, 10, false)
	policy.Config.FeatureWeights = map[string]float64{
		domainrecommendation.FeatureSemanticSimilarity: 1,
		domainrecommendation.FeatureSessionSimilarity:  0,
	}
	semanticCandidate := annotateCandidate(
		recallCandidate(1, 1, 0, now), domainrecommendation.RecallProviderSemanticSession, 0.8,
	)
	freshCandidate := annotateCandidate(
		recallCandidate(2, 2, 0, now), domainrecommendation.RecallProviderFresh, 1,
	)
	repo := &rankerTestRepo{
		vectors: map[int64][]float64{1: {1, 0}, 2: {0, 1}}, features: emptyRankingFeatures(),
	}
	service := New(repo, WithNow(func() time.Time { return now }))
	ranked, err := service.rankCandidates(context.Background(), 7, nil,
		[]*domainrecommendation.Candidate{freshCandidate, semanticCandidate}, policy,
	)
	if err != nil || len(ranked) != 2 || ranked[0].VideoID != 1 ||
		ranked[0].ScoreComponents[domainrecommendation.FeatureSemanticSimilarity] != 0.8 ||
		ranked[0].ScoreComponents[domainrecommendation.FeatureSessionSimilarity] != 0 {
		t.Fatalf("ranked=%#v err=%v", ranked, err)
	}
}

func sessionSemanticRecommendationPolicy(
	t testing.TB,
	contract domainembedding.MultimodalContractIdentity,
	semanticBudget int,
	quota bool,
) *domainrecommendation.Policy {
	t.Helper()
	config := defaultRecommendationPolicyConfiguration()
	config.FeatureWeights = map[string]float64{
		domainrecommendation.FeatureSemanticSimilarity: 1,
		domainrecommendation.FeatureFreshness:          0.01,
	}
	config.RecallBudgets = map[string]int{domainrecommendation.RecallProviderSemanticSession: semanticBudget}
	config.ProviderDeadlinesMS = map[string]int{domainrecommendation.RecallProviderSemanticSession: 250}
	if quota {
		config.RecallBudgets[domainrecommendation.RecallProviderFresh] = 60
		config.ProviderDeadlinesMS[domainrecommendation.RecallProviderFresh] = 250
		config.PreRankPoolLimit = 50
		config.RecallProviderOrder = []string{
			domainrecommendation.RecallProviderSemanticSession,
			domainrecommendation.RecallProviderFresh,
		}
		config.RecallProviderReservations = map[string]int{
			domainrecommendation.RecallProviderSemanticSession: 5,
			domainrecommendation.RecallProviderFresh:           0,
		}
	}
	config.SessionSemantic = sessionSemanticTestPolicy(contract.Key(), 1)
	policy, err := domainrecommendation.NewPolicy("recommend", 3, false, config, time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	return policy
}

func sessionSemanticSuccessEvidence(
	t testing.TB,
	contract domainembedding.MultimodalContractIdentity,
	confidence float64,
) *domainrecommendation.SessionSemanticEvidence {
	t.Helper()
	evidence, err := domainrecommendation.NewSessionSemanticEvidence(domainrecommendation.SessionSemanticEvidence{
		BuilderVersion: domainrecommendation.SessionSemanticBuilderV1, ContractKey: contract.Key(),
		Result: domainrecommendation.SessionSemanticResultSuccess, Confidence: confidence,
		ConfidenceBand: sessionSemanticConfidenceBand(confidence),
		EligibleCount:  1, PositiveCount: 1, CompatibleCount: 1,
		ExcludedCount: 1, InputDigest: sessionSemanticTestPolicy(contract.Key(), 1).ContractKey,
	})
	if err != nil {
		t.Fatal(err)
	}
	return evidence
}
