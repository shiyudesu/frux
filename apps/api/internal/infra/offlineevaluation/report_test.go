package infraofflineevaluation

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	applicationofflineevaluation "github.com/shiyudesu/frux/internal/application/offlineevaluation"
	domainofflineevaluation "github.com/shiyudesu/frux/internal/domain/offlineevaluation"
)

func TestPublicReportIsDeterministicRestrictedAndRedacted(t *testing.T) {
	root := filepath.Join("..", "..", "..", "testdata", "recommendation-offline", "kuairec-v2")
	loaded, err := LoadManifest(root, "manifest.json", DefaultManifestLimits())
	if err != nil {
		t.Fatal(err)
	}
	dataset, err := LoadDataset(loaded, DefaultDatasetLimits())
	if err != nil {
		t.Fatal(err)
	}
	evaluation, err := applicationofflineevaluation.EvaluatePublicDataset(dataset, domainofflineevaluation.DefaultCaseProfile(), []int{1, 3}, 10)
	if err != nil {
		t.Fatal(err)
	}
	report, err := NewPublicReport(loaded.Evidence, evaluation, nil)
	if err != nil {
		t.Fatal(err)
	}
	firstJSON, firstMarkdown, err := RenderPublicReport(report)
	if err != nil {
		t.Fatal(err)
	}
	secondJSON, secondMarkdown, err := RenderPublicReport(report)
	if err != nil || !bytes.Equal(firstJSON, secondJSON) || !bytes.Equal(firstMarkdown, secondMarkdown) {
		t.Fatal("report rendering is not deterministic")
	}
	for _, forbidden := range [][]byte{[]byte(root), []byte("kuairec:user:"), []byte("kuairec:item:"), []byte("values")} {
		if bytes.Contains(firstJSON, forbidden) || bytes.Contains(firstMarkdown, forbidden) {
			t.Fatalf("report leaked %q", forbidden)
		}
	}
	directory := t.TempDir()
	jsonPath := filepath.Join(directory, "report.json")
	markdownPath := filepath.Join(directory, "report.md")
	if err := PublishPublicReport(jsonPath, markdownPath, report, false); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{jsonPath, markdownPath} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("path=%s mode=%v err=%v", path, info.Mode(), err)
		}
	}
	if err := PublishPublicReport(jsonPath, markdownPath, report, false); err == nil {
		t.Fatal("expected overwrite rejection")
	}
	if err := PublishPublicReport(jsonPath, markdownPath, report, true); err != nil {
		t.Fatal(err)
	}
}
