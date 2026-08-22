package applicationrecommendation

import (
	"testing"
	"time"

	domainrecommendation "github.com/shiyudesu/frux/internal/domain/recommendation"
)

func sessionSemanticShadowTestConfig(t testing.TB) SessionSemanticShadowRuntimeConfig {
	t.Helper()
	contract := sessionSemanticTestContract(t, "shadow-revision")
	return SessionSemanticShadowRuntimeConfig{
		Enabled: true, SamplePPM: domainrecommendation.MaxSamplingRatePPM,
		Budget: 25, Deadline: 250 * time.Millisecond, MaxInFlight: 2,
		ComparisonLimit: 100, SimulatedPoolLimit: 100, SimulatedTopK: 10,
		SemanticReservation: 10, SemanticWeight: 0.25, ShutdownTimeout: time.Second,
		Contract: contract, SessionPolicy: sessionSemanticTestPolicy(contract.Key(), 2),
	}
}

func TestSessionSemanticShadowRuntimeConfigValidation(t *testing.T) {
	valid := sessionSemanticShadowTestConfig(t)
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	invalid := valid
	invalid.SessionPolicy = invalid.SessionPolicy.Clone()
	invalid.SessionPolicy.ContractKey = sessionSemanticTestContract(t, "other-revision").Key()
	if err := invalid.Validate(); err != ErrSessionSemanticShadowConfiguration {
		t.Fatalf("error=%v", err)
	}
}

func TestBuildSessionSemanticShadowPolicyDoesNotMutateActivePolicy(t *testing.T) {
	active := defaultRecommendationPolicy()
	before := active.Clone()
	config := sessionSemanticShadowTestConfig(t)
	shadow, err := BuildSessionSemanticShadowPolicy(active, config, time.Unix(10, 0))
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := active.Config.RecallBudgets[domainrecommendation.RecallProviderSemanticSession]; exists ||
		active.Config.SessionSemantic != nil ||
		active.Config.FeatureWeights[domainrecommendation.FeatureSemanticSimilarity] != 0 {
		t.Fatalf("active policy mutated: %#v", active.Config)
	}
	if len(active.Config.RecallBudgets) != len(before.Config.RecallBudgets) {
		t.Fatal("active policy budget map changed")
	}
	if shadow.Config.RecallBudgets[domainrecommendation.RecallProviderSemanticSession] != config.Budget ||
		shadow.Config.ProviderDeadlinesMS[domainrecommendation.RecallProviderSemanticSession] != int(config.Deadline/time.Millisecond) ||
		shadow.Config.FeatureWeights[domainrecommendation.FeatureSemanticSimilarity] != config.SemanticWeight ||
		shadow.Config.SessionSemantic.ContractKey != config.Contract.Key() ||
		shadow.Config.RecallProviderReservations[domainrecommendation.RecallProviderSemanticSession] != config.SemanticReservation {
		t.Fatalf("shadow policy=%#v", shadow.Config)
	}
	if got := shadow.Config.RecallProviderOrder[len(shadow.Config.RecallProviderOrder)-1]; got != domainrecommendation.RecallProviderSemanticSession {
		t.Fatalf("last provider=%q", got)
	}
}

func TestCompareSessionSemanticShadowMetricsAndEmptyDenominators(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	active := []*domainrecommendation.Candidate{
		shadowCandidate(1, 10, now), shadowCandidate(2, 20, now), shadowCandidate(3, 20, now),
	}
	semantic := []*domainrecommendation.Candidate{
		shadowCandidate(2, 20, now), shadowCandidate(4, 30, now), shadowCandidate(5, 40, now),
	}
	mixed := []*domainrecommendation.Candidate{
		shadowCandidate(1, 10, now), shadowCandidate(2, 20, now), shadowCandidate(4, 30, now), shadowCandidate(5, 40, now),
	}
	ranked := []*domainrecommendation.Candidate{
		shadowCandidate(4, 30, now), shadowCandidate(1, 10, now), shadowCandidate(2, 20, now), shadowCandidate(5, 40, now),
	}
	comparison := CompareSessionSemanticShadow(active, semantic, mixed, ranked, 3)
	if !comparison.valid() || comparison.ActiveCount != 3 || comparison.SemanticCount != 3 ||
		comparison.IntersectionCount != 1 || comparison.UniqueSemanticCount != 2 ||
		comparison.SemanticPoolSurvival != 3 || comparison.SemanticRankSurvival != 2 ||
		comparison.UniqueRankSurvival != 1 || comparison.ActiveTopKDisplaced != 1 ||
		comparison.SemanticAuthorCount != 3 || comparison.SimulatedAuthorCount != 3 {
		t.Fatalf("comparison=%#v", comparison)
	}
	if comparison.OverlapRatio != 1.0/3.0 || comparison.UniqueContributionRatio != 2.0/3.0 ||
		comparison.PoolSurvivalRatio != 1 || comparison.UniqueRankSurvivalRatio != 0.5 ||
		comparison.ActiveTopKDisplacedRatio != 1.0/3.0 {
		t.Fatalf("ratios=%#v", comparison)
	}
	empty := CompareSessionSemanticShadow(nil, nil, nil, nil, 10)
	if !empty.valid() || empty.OverlapRatio != 0 || empty.UniqueContributionRatio != 0 ||
		empty.PoolSurvivalRatio != 0 || empty.RankSurvivalRatio != 0 {
		t.Fatalf("empty=%#v", empty)
	}
}

func TestSessionSemanticShadowRequestCloneIsDefensive(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	request := SessionSemanticShadowRequest{
		UserID: 1, Scene: " Recommend ", RequestID: " request ",
		Context: sessionSemanticContext(t, 1, []int64{2}), ActivePolicy: defaultRecommendationPolicy(),
		ActiveCandidates: []*domainrecommendation.Candidate{shadowCandidate(3, 4, now)},
		BaselineState:    "providers", Now: now,
	}
	cloned := request.clone(1)
	request.Context.RecentVideoIDs[0] = 99
	request.ActiveCandidates[0].AuthorID = 99
	request.ActivePolicy.Config.RecallBudgets[domainrecommendation.RecallProviderFresh] = 1
	if !cloned.valid() || cloned.Scene != "recommend" || cloned.RequestID != "request" ||
		cloned.Context.RecentVideoIDs[0] != 2 || cloned.ActiveCandidates[0].AuthorID != 4 ||
		cloned.ActivePolicy.Config.RecallBudgets[domainrecommendation.RecallProviderFresh] == 1 {
		t.Fatalf("clone=%#v", cloned)
	}
}

func shadowCandidate(videoID, authorID int64, publishedAt time.Time) *domainrecommendation.Candidate {
	return domainrecommendation.RestoreCandidate(videoID, authorID, 0, 0, 0, 0, "", publishedAt)
}
