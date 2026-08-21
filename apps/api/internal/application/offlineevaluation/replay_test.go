package applicationofflineevaluation

import (
	"testing"
	"time"

	domainrecommendation "github.com/shiyudesu/frux/internal/domain/recommendation"
)

func TestEvaluateReplayRequiresParityAndSuppressesNonReplayableComparison(t *testing.T) {
	baselineConfig, err := domainrecommendation.ValidatePolicyConfiguration(domainrecommendation.InitialRecommendationPolicyConfiguration())
	if err != nil {
		t.Fatal(err)
	}
	candidateConfig := cloneReplayPolicyConfig(baselineConfig)
	candidateConfig.FeatureWeights[domainrecommendation.FeatureContentSimilarity] = 0.1
	candidateConfig.FeatureWeights[domainrecommendation.FeatureHotness] = 0.9
	baseline := NamedPolicy{Name: "baseline", NormalizedSHA256: "baseline-hash", Config: baselineConfig}
	candidate := NamedPolicy{Name: "candidate", NormalizedSHA256: "candidate-hash", Config: candidateConfig}
	bundle := replayTestBundle()
	report, err := EvaluateReplay(bundle, baseline, []NamedPolicy{candidate}, []int{1, 2}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !report.BaselineParity || !report.ComparativeAvailable || len(report.Candidates) != 1 ||
		report.Candidates[0].MeanAbsoluteRankShift <= 0 || report.Candidates[0].TopK[0].Overlap != 0 {
		t.Fatalf("report=%#v", report)
	}
	nonReplayable := candidate
	nonReplayable.Name = "rollout-change"
	nonReplayable.NormalizedSHA256 = "rollout-hash"
	nonReplayable.Config.RolloutPercentage = 5
	if _, err := EvaluateReplay(bundle, baseline, []NamedPolicy{nonReplayable}, []int{1}, false); err == nil {
		t.Fatal("expected non-replayable rejection")
	}
	diagnostic, err := EvaluateReplay(bundle, baseline, []NamedPolicy{nonReplayable}, []int{1}, true)
	if err != nil || diagnostic.ComparativeAvailable || len(diagnostic.Candidates) != 0 ||
		len(diagnostic.Differences[nonReplayable.Name]) == 0 {
		t.Fatalf("diagnostic=%#v err=%v", diagnostic, err)
	}
}

func TestEvaluateReplayRejectsBaselineMismatchAndDuplicatePolicyHash(t *testing.T) {
	config, _ := domainrecommendation.ValidatePolicyConfiguration(domainrecommendation.InitialRecommendationPolicyConfiguration())
	baseline := NamedPolicy{Name: "baseline", NormalizedSHA256: "same", Config: config}
	candidate := NamedPolicy{Name: "candidate", NormalizedSHA256: "same", Config: config}
	if _, err := EvaluateReplay(replayTestBundle(), baseline, []NamedPolicy{candidate}, []int{1}, false); err == nil {
		t.Fatal("expected duplicate policy hash rejection")
	}
	bundle := replayTestBundle()
	bundle.Cases[0].ExpectedOrder = []int64{2, 1, 3}
	candidate.NormalizedSHA256 = "other"
	candidate.Config.FeatureWeights[domainrecommendation.FeatureHotness] = 0.21
	if _, err := EvaluateReplay(bundle, baseline, []NamedPolicy{candidate}, []int{1}, false); err == nil {
		t.Fatal("expected parity rejection")
	}
}

func replayTestBundle() ReplayBundle {
	base := time.Unix(100, 0).UTC()
	return ReplayBundle{
		Version: ReplayVersion, Scope: ReplayScopeFull,
		Cases: []ReplayCase{{
			Name: "canonical", ExpectedOrder: []int64{1, 2, 3},
			Candidates: []ReplayCandidate{
				{VideoID: 1, AuthorKey: "author-a", PublishedAt: base, RecallProviders: []string{"fresh"}, ScoreComponents: replayComponents(1, 0)},
				{VideoID: 2, AuthorKey: "author-b", PublishedAt: base.Add(time.Second), RecallProviders: []string{"hot"}, ScoreComponents: replayComponents(0, 1)},
				{VideoID: 3, AuthorKey: "author-c", PublishedAt: base.Add(2 * time.Second), RecallProviders: []string{"fresh"}, ScoreComponents: replayComponents(0, 0)},
			},
		}},
	}
}

func replayComponents(content, hot float64) map[string]float64 {
	return map[string]float64{
		domainrecommendation.FeatureContentSimilarity:  content,
		domainrecommendation.FeatureSessionSimilarity:  0,
		domainrecommendation.FeatureSemanticSimilarity: 0,
		domainrecommendation.FeatureHotness:            hot,
		domainrecommendation.FeatureFreshness:          0,
		domainrecommendation.FeatureAuthorAffinity:     0,
		domainrecommendation.FeatureFollowRelation:     0,
		domainrecommendation.FeatureNegativePenalty:    0,
		domainrecommendation.FeatureExposurePenalty:    0,
	}
}

func cloneReplayPolicyConfig(config domainrecommendation.PolicyConfiguration) domainrecommendation.PolicyConfiguration {
	cloned, err := domainrecommendation.ValidatePolicyConfiguration(config)
	if err != nil {
		panic(err)
	}
	return cloned
}
