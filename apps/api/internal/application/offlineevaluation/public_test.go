package applicationofflineevaluation

import (
	"path/filepath"
	"testing"

	domainofflineevaluation "github.com/shiyudesu/frux/internal/domain/offlineevaluation"
	infraofflineevaluation "github.com/shiyudesu/frux/internal/infra/offlineevaluation"
)

func TestEvaluatePublicDatasetProducesSeparateBaselineMetrics(t *testing.T) {
	root := filepath.Join("..", "..", "..", "testdata", "recommendation-offline", "kuairec-v2")
	loaded, err := infraofflineevaluation.LoadManifest(root, "manifest.json", infraofflineevaluation.DefaultManifestLimits())
	if err != nil {
		t.Fatal(err)
	}
	dataset, err := infraofflineevaluation.LoadDataset(loaded, infraofflineevaluation.DefaultDatasetLimits())
	if err != nil {
		t.Fatal(err)
	}
	report, err := EvaluatePublicDataset(dataset, domainofflineevaluation.DefaultCaseProfile(), []int{1, 3}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if report.Summary.Dataset != domainofflineevaluation.DatasetKuaiRec || report.Summary.Cases != 2 ||
		len(report.Baselines) != 7 || len(report.Deltas) != 12 {
		t.Fatalf("report=%#v", report)
	}
	for _, baseline := range report.Baselines {
		if baseline.CasesAvailable != 2 || baseline.CatalogCoverage.Value == nil || baseline.Work.CandidateScores == 0 {
			t.Fatalf("baseline=%#v", baseline)
		}
	}
	var session *domainofflineevaluation.BaselineMetrics
	for index := range report.Baselines {
		if report.Baselines[index].Baseline == domainofflineevaluation.BaselineMultimodalSession {
			session = &report.Baselines[index]
			break
		}
	}
	if session == nil || session.TopK[0].HitRate.Value == nil || *session.TopK[0].HitRate.Value != 1 {
		t.Fatalf("session=%#v", session)
	}
}
