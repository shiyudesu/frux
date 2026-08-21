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
	expectedMarkdown, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "recommendation-offline", "expected", "kuairec.md"))
	if err != nil || !bytes.Equal(firstMarkdown, expectedMarkdown) {
		t.Fatalf("public Markdown snapshot mismatch: %v", err)
	}
	directory := t.TempDir()
	jsonPath := filepath.Join(directory, "report.json")
	markdownPath := filepath.Join(directory, "report.md")
	if err := PublishPublicReport(jsonPath, markdownPath, report, false); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{jsonPath, markdownPath} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("path=%s mode=%v", path, info.Mode())
		}
	}
	if err := PublishPublicReport(jsonPath, markdownPath, report, false); err == nil {
		t.Fatal("expected overwrite rejection")
	}
	if err := PublishPublicReport(jsonPath, markdownPath, report, true); err != nil {
		t.Fatal(err)
	}
}

func TestReplayAndGoldenReportsAreDeterministicAndContainNoCandidateIdentity(t *testing.T) {
	replayRoot := filepath.Join("..", "..", "..", "testdata", "recommendation-offline", "replay-v1")
	bundle, err := LoadReplayBundle(filepath.Join(replayRoot, "bundle.json"))
	if err != nil {
		t.Fatal(err)
	}
	baseline, err := LoadNamedPolicy(filepath.Join(replayRoot, "baseline.json"))
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := LoadNamedPolicy(filepath.Join(replayRoot, "candidate.json"))
	if err != nil {
		t.Fatal(err)
	}
	replayEvaluation, err := applicationofflineevaluation.EvaluateReplay(bundle.Bundle, baseline, []applicationofflineevaluation.NamedPolicy{candidate}, []int{1, 2}, false)
	if err != nil {
		t.Fatal(err)
	}
	replayReport, err := NewReplayReport(bundle.SHA256, baseline, []applicationofflineevaluation.NamedPolicy{candidate}, replayEvaluation)
	if err != nil {
		t.Fatal(err)
	}
	firstReplayJSON, firstReplayMarkdown, err := RenderReplayReport(replayReport)
	if err != nil {
		t.Fatal(err)
	}
	secondReplayJSON, secondReplayMarkdown, _ := RenderReplayReport(replayReport)
	if !bytes.Equal(firstReplayJSON, secondReplayJSON) || !bytes.Equal(firstReplayMarkdown, secondReplayMarkdown) ||
		bytes.Contains(firstReplayJSON, []byte(`"video_id"`)) || bytes.Contains(firstReplayJSON, []byte("author-a")) {
		t.Fatal("replay report is nondeterministic or leaked candidate identity")
	}
	expectedReplay, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "recommendation-offline", "expected", "replay.md"))
	if err != nil || !bytes.Equal(firstReplayMarkdown, expectedReplay) {
		t.Fatalf("replay Markdown snapshot mismatch: %v", err)
	}
	goldenPath := filepath.Join("..", "..", "..", "testdata", "recommendation-offline", "golden-v1.json")
	goldenBundle, err := LoadGoldenBundle(goldenPath)
	if err != nil {
		t.Fatal(err)
	}
	goldenEvaluation, err := applicationofflineevaluation.EvaluateGolden(goldenBundle.Bundle, []int{1, 3})
	if err != nil {
		t.Fatal(err)
	}
	goldenReport, err := NewGoldenReport(goldenBundle.SHA256, goldenEvaluation)
	if err != nil {
		t.Fatal(err)
	}
	firstGoldenJSON, firstGoldenMarkdown, err := RenderGoldenReport(goldenReport)
	if err != nil {
		t.Fatal(err)
	}
	secondGoldenJSON, secondGoldenMarkdown, _ := RenderGoldenReport(goldenReport)
	if !bytes.Equal(firstGoldenJSON, secondGoldenJSON) || !bytes.Equal(firstGoldenMarkdown, secondGoldenMarkdown) ||
		bytes.Contains(firstGoldenJSON, []byte("candidate-a")) || bytes.Contains(firstGoldenJSON, []byte(`"judgments"`)) {
		t.Fatal("Golden report is nondeterministic or leaked annotations")
	}
	expectedGolden, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "recommendation-offline", "expected", "golden.md"))
	if err != nil || !bytes.Equal(firstGoldenMarkdown, expectedGolden) {
		t.Fatalf("Golden Markdown snapshot mismatch: %v", err)
	}
}

func TestMicroLensMarkdownSnapshot(t *testing.T) {
	root := filepath.Join("..", "..", "..", "testdata", "recommendation-offline", "microlens-canonical-v1")
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
	_, markdown, err := RenderPublicReport(report)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "recommendation-offline", "expected", "microlens.md"))
	if err != nil || !bytes.Equal(markdown, expected) {
		t.Fatalf("MicroLens Markdown snapshot mismatch: %v", err)
	}
}
