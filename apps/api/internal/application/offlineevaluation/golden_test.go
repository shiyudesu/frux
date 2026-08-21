package applicationofflineevaluation

import "testing"

func TestEvaluateGoldenReportsSemanticDirectionSuppressionAndAgreement(t *testing.T) {
	bundle := goldenTestBundle()
	report, err := EvaluateGolden(bundle, []int{1, 3})
	if err != nil {
		t.Fatal(err)
	}
	if report.Cases != 4 || report.Candidates != 10 || !report.LabelCoverage.Available || len(report.Rankings) != 2 {
		t.Fatalf("report=%#v", report)
	}
	var baseline, semantic *GoldenRankingMetrics
	for index := range report.Rankings {
		switch report.Rankings[index].Name {
		case "baseline":
			baseline = &report.Rankings[index]
		case "semantic":
			semantic = &report.Rankings[index]
		}
	}
	if baseline == nil || semantic == nil || semantic.TopK[0].NDCG.Value != 1 ||
		semantic.DirectionAccuracy.Value != 1 || semantic.SuppressionAccuracy.Value != 1 ||
		baseline.DirectionAccuracy.Value != 0 || baseline.SuppressionAccuracy.Value != 0 {
		t.Fatalf("baseline=%#v semantic=%#v", baseline, semantic)
	}
}

func TestEvaluateGoldenRejectsMissingAdjudicationAndPublicProvenance(t *testing.T) {
	bundle := goldenTestBundle()
	bundle.Cases[1].Candidates[0].Adjudicated = nil
	if _, err := EvaluateGolden(bundle, []int{1}); err == nil {
		t.Fatal("expected missing adjudication rejection")
	}
	bundle = goldenTestBundle()
	bundle.Provenance = "public_dataset"
	if _, err := EvaluateGolden(bundle, []int{1}); err == nil {
		t.Fatal("expected cross-track provenance rejection")
	}
}

func goldenTestBundle() GoldenBundle {
	adjudicated := 3
	return GoldenBundle{
		Version: GoldenVersion, Rubric: GoldenRubric, Provenance: GoldenProvenance,
		Cases: []GoldenCase{
			{Name: "query", Kind: GoldenKindQuery, Candidates: []GoldenCandidate{
				{Key: "a", Judgments: []int{3, 3}}, {Key: "b", Judgments: []int{1, 1}}, {Key: "c", Judgments: []int{0, 0}},
			}, Rankings: []GoldenRanking{{Name: "baseline", Order: []string{"b", "a", "c"}}, {Name: "semantic", Order: []string{"a", "b", "c"}}}},
			{Name: "similar", Kind: GoldenKindSimilar, Candidates: []GoldenCandidate{
				{Key: "a", Judgments: []int{3, 1}, Adjudicated: &adjudicated}, {Key: "b", Judgments: []int{2, 2}}, {Key: "c", Judgments: []int{0, 0}},
			}, Rankings: []GoldenRanking{{Name: "baseline", Order: []string{"b", "a", "c"}}, {Name: "semantic", Order: []string{"a", "b", "c"}}}},
			{Name: "direction", Kind: GoldenKindDirection, ExpectedDirection: "toward", Candidates: []GoldenCandidate{
				{Key: "a", Judgments: []int{3, 3}}, {Key: "b", Judgments: []int{1, 1}},
			}, Rankings: []GoldenRanking{{Name: "baseline", Order: []string{"b", "a"}, Direction: "away"}, {Name: "semantic", Order: []string{"a", "b"}, Direction: "toward"}}},
			{Name: "suppression", Kind: GoldenKindSuppression, SuppressedCandidateKey: "b", SuppressionCutoff: 1, Candidates: []GoldenCandidate{
				{Key: "a", Judgments: []int{3, 3}}, {Key: "b", Judgments: []int{0, 0}},
			}, Rankings: []GoldenRanking{{Name: "baseline", Order: []string{"b", "a"}}, {Name: "semantic", Order: []string{"a", "b"}}}},
		},
	}
}
