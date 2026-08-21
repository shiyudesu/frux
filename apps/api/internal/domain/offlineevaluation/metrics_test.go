package domainofflineevaluation

import "testing"

func TestAggregateBaselineMetricsUsesExplicitDenominators(t *testing.T) {
	dataset := testDataset()
	build, err := BuildCases(dataset, DefaultCaseProfile(), 10)
	if err != nil {
		t.Fatal(err)
	}
	ranking := Rank(dataset, build.Cases[0], DefaultCaseProfile(), BaselineMultimodalSession)
	metrics, err := AggregateBaselineMetrics(dataset, build.Cases, []Ranking{ranking}, []int{1, 3})
	if err != nil {
		t.Fatal(err)
	}
	if metrics.CasesAvailable != 1 || metrics.TopK[0].Recall.Value == nil || *metrics.TopK[0].Recall.Value != 1 ||
		metrics.MRR.Value == nil || *metrics.MRR.Value != 1 || metrics.CatalogCoverage.Denominator != int64(len(dataset.Items)) ||
		metrics.Work.CandidateScores == 0 || metrics.Work.VectorComponents == 0 || metrics.Work.Comparisons == 0 {
		t.Fatalf("metrics=%#v", metrics)
	}
}

func TestAggregateBaselineMetricsReportsUnavailableMetadataAndCases(t *testing.T) {
	dataset := testDataset()
	build, err := BuildCases(dataset, DefaultCaseProfile(), 10)
	if err != nil {
		t.Fatal(err)
	}
	unavailable := Ranking{Baseline: BaselineImage, Reason: ExclusionMissingFeature}
	metrics, err := AggregateBaselineMetrics(dataset, build.Cases, []Ranking{unavailable}, []int{1})
	if err != nil {
		t.Fatal(err)
	}
	if metrics.CasesAvailable != 0 || metrics.TopK[0].Recall.Availability != AvailabilityUnavailable ||
		metrics.Exclusions[ExclusionMissingFeature] != 1 {
		t.Fatalf("metrics=%#v", metrics)
	}
	for key, item := range dataset.Items {
		item.AuthorKey = ""
		dataset.Items[key] = item
	}
	ranking := Rank(dataset, build.Cases[0], DefaultCaseProfile(), BaselinePopularity)
	metrics, err = AggregateBaselineMetrics(dataset, build.Cases, []Ranking{ranking}, []int{1})
	if err != nil || metrics.Author.Coverage.Availability != AvailabilityUnavailable {
		t.Fatalf("metrics=%#v err=%v", metrics, err)
	}
}
