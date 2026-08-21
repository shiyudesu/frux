package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunEvaluatesVersionedFixtureWithoutModelCalls(t *testing.T) {
	fixture := filepath.Join("..", "..", "testdata", "session-semantic-v1.json")
	reportPath := filepath.Join(t.TempDir(), "report.json")
	var output bytes.Buffer
	if err := run([]string{"--fixture", fixture, "--report", reportPath}, &output); err != nil {
		t.Fatal(err)
	}
	var report evaluationReport
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Result != "success" || report.ExternalModelCalls != 0 || len(report.Cases) != 3 {
		t.Fatalf("report=%#v", report)
	}
	for _, item := range report.Cases {
		if !item.Passed {
			t.Fatalf("case=%#v", item)
		}
	}
	info, err := os.Stat(reportPath)
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 || strings.Contains(string(content), `"vectors"`) ||
		strings.Contains(string(content), `"signals"`) || strings.Contains(string(content), `"raw_event"`) {
		t.Fatalf("mode=%o report=%s", info.Mode().Perm(), content)
	}
}
