package infraofflineevaluation

import (
	"path/filepath"
	"testing"

	applicationofflineevaluation "github.com/shiyudesu/frux/internal/application/offlineevaluation"
)

func TestLoadGoldenBundleStrictlyReadsVersionedFixture(t *testing.T) {
	path := filepath.Join("..", "..", "..", "testdata", "recommendation-offline", "golden-v1.json")
	loaded, err := LoadGoldenBundle(path)
	if err != nil || loaded.SHA256 == "" || loaded.Bundle.Version != applicationofflineevaluation.GoldenVersion || len(loaded.Bundle.Cases) != 4 {
		t.Fatalf("loaded=%#v err=%v", loaded, err)
	}
	report, err := applicationofflineevaluation.EvaluateGolden(loaded.Bundle, []int{1, 3})
	if err != nil || len(report.Rankings) != 2 {
		t.Fatalf("report=%#v err=%v", report, err)
	}
}
