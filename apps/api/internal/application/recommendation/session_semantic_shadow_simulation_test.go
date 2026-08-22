package applicationrecommendation

import (
	"context"
	"testing"
	"time"

	domainrecommendation "github.com/shiyudesu/frux/internal/domain/recommendation"
)

func TestServiceSimulatesSessionSemanticShadowWithoutMutatingActiveCandidates(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	repo := &recallTestRepo{
		vectors:   map[int64][]float64{1: {1, 0}, 2: {0.9, 0.1}, 3: {0.8, 0.2}},
		exposures: map[int64]*domainrecommendation.Exposure{},
	}
	service := New(repo, WithNow(func() time.Time { return now }))
	config := sessionSemanticShadowTestConfig(t)
	config.SimulatedTopK = 3
	shadowPolicy, err := BuildSessionSemanticShadowPolicy(defaultRecommendationPolicy(), config, now)
	if err != nil {
		t.Fatal(err)
	}
	active := []*domainrecommendation.Candidate{
		annotateCandidate(shadowCandidate(1, 10, now), domainrecommendation.RecallProviderFresh, 100),
		annotateCandidate(shadowCandidate(2, 20, now), domainrecommendation.RecallProviderHot, 50),
	}
	semantic := []*domainrecommendation.Candidate{
		annotateCandidate(shadowCandidate(2, 20, now), domainrecommendation.RecallProviderSemanticSession, 0.7),
		annotateCandidate(shadowCandidate(3, 30, now), domainrecommendation.RecallProviderSemanticSession, 0.8),
	}
	result, err := service.SimulateSessionSemanticShadow(context.Background(), SessionSemanticShadowSimulationRequest{
		Request: SessionSemanticShadowRequest{
			UserID: 1, Scene: "recommend", RequestID: "request", ActivePolicy: defaultRecommendationPolicy(),
			ActiveCandidates: active, BaselineState: "providers", Now: now,
		},
		ShadowPolicy: shadowPolicy, SemanticCandidates: semantic, ComparisonLimit: 100, TopK: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Comparison.IntersectionCount != 1 || result.Comparison.UniqueSemanticCount != 1 ||
		result.Comparison.SemanticPoolSurvival != 2 || result.Comparison.SemanticRankSurvival != 2 ||
		len(result.Mixed) != 3 || len(result.Ranked) != 3 {
		t.Fatalf("result=%#v", result)
	}
	if len(active[0].RecallReasons) != 1 || active[0].RankScore != 0 || len(semantic[0].ScoreComponents) != 0 {
		t.Fatal("simulation mutated copied active or semantic candidates")
	}
}
