package shadowevaluation

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGoldenFixtureProducesDeterministicReport(t *testing.T) {
	fixture, checksum, err := Load(goldenFixturePath())
	if err != nil {
		t.Fatal(err)
	}
	report, err := Evaluate(fixture, checksum, 5)
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != "complete" || report.ExternalModelCalls != 0 ||
		report.Cases.Total != 6 || report.Cases.Available != 5 || report.Cases.Labeled != 5 ||
		report.CandidateMetrics.UniqueSemantic != 5 || report.RelevanceMetrics.SimulatedNDCG.Samples != 5 {
		t.Fatalf("report=%#v", report)
	}
	directory := t.TempDir()
	jsonPath := filepath.Join(directory, "report.json")
	markdownPath := filepath.Join(directory, "report.md")
	if err := Write(report, jsonPath, markdownPath); err != nil {
		t.Fatal(err)
	}
	firstJSON, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatal(err)
	}
	firstMarkdown, err := os.ReadFile(markdownPath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(firstJSON, []byte(`"v20"`)) || bytes.Contains(firstMarkdown, []byte("v20")) {
		t.Fatal("aggregate reports exposed fixture candidate identities")
	}
	if err := Write(report, jsonPath, markdownPath); err != nil {
		t.Fatal(err)
	}
	secondJSON, _ := os.ReadFile(jsonPath)
	secondMarkdown, _ := os.ReadFile(markdownPath)
	if !bytes.Equal(firstJSON, secondJSON) || !bytes.Equal(firstMarkdown, secondMarkdown) {
		t.Fatal("identical evaluation did not produce byte-identical reports")
	}
	for _, path := range []string{jsonPath, markdownPath} {
		info, statErr := os.Stat(path)
		if statErr != nil {
			t.Fatal(statErr)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode=%o", path, info.Mode().Perm())
		}
	}
}

func TestEvaluationReportsInsufficientEvidenceAsInconclusive(t *testing.T) {
	fixture, checksum, err := Load(goldenFixturePath())
	if err != nil {
		t.Fatal(err)
	}
	report, err := Evaluate(fixture, checksum, 6)
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != "inconclusive" || len(report.Warnings) != 2 {
		t.Fatalf("report=%#v", report)
	}
}

func TestFixtureValidationRejectsMalformedUnsafeDuplicateAndOversizedInputs(t *testing.T) {
	fixture, _, err := Load(goldenFixturePath())
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		mutate func(*Fixture)
	}{
		{name: "unsafe path", mutate: func(value *Fixture) { value.Provenance.SourcePath = "../secret.json" }},
		{name: "duplicate case", mutate: func(value *Fixture) { value.Cases[1].ID = value.Cases[0].ID }},
		{name: "duplicate candidate", mutate: func(value *Fixture) {
			value.Cases[0].Active = append(value.Cases[0].Active, value.Cases[0].Active[0])
		}},
		{name: "ranked outside mixed", mutate: func(value *Fixture) {
			value.Cases[0].Ranked[0].ID = "outside"
		}},
		{name: "inconsistent author", mutate: func(value *Fixture) {
			value.Cases[0].Semantic[0].Author = "different-author"
		}},
		{name: "unavailable with rows", mutate: func(value *Fixture) {
			value.Cases[len(value.Cases)-1].Active = []Candidate{{ID: "x", Author: "a"}}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cloned := cloneFixture(t, fixture)
			test.mutate(cloned)
			path := writeFixture(t, cloned)
			if _, _, loadErr := Load(path); !errors.Is(loadErr, ErrInvalidFixture) {
				t.Fatalf("error=%v", loadErr)
			}
		})
	}
	unknown := filepath.Join(t.TempDir(), "unknown.json")
	payload, _ := os.ReadFile(goldenFixturePath())
	payload = bytes.Replace(payload, []byte(`"schema":`), []byte(`"unknown":true,"schema":`), 1)
	if err := os.WriteFile(unknown, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Load(unknown); !errors.Is(err, ErrInvalidFixture) {
		t.Fatalf("unknown field error=%v", err)
	}
	oversized := filepath.Join(t.TempDir(), "oversized.json")
	if err := os.WriteFile(oversized, bytes.Repeat([]byte("x"), maxFixtureBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Load(oversized); !errors.Is(err, ErrInvalidFixture) {
		t.Fatalf("oversized error=%v", err)
	}
}

func TestWriteRejectsConflictingOutputs(t *testing.T) {
	fixture, checksum, err := Load(goldenFixturePath())
	if err != nil {
		t.Fatal(err)
	}
	report, err := Evaluate(fixture, checksum, 5)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "same")
	if err := Write(report, path, path); !errors.Is(err, ErrInvalidOutput) {
		t.Fatalf("error=%v", err)
	}
}

func goldenFixturePath() string {
	return filepath.Join("..", "..", "..", "testdata", "session-semantic-shadow", "golden-v1.json")
}

func cloneFixture(t testing.TB, fixture *Fixture) *Fixture {
	t.Helper()
	payload, err := json.Marshal(fixture)
	if err != nil {
		t.Fatal(err)
	}
	var cloned Fixture
	if err := json.Unmarshal(payload, &cloned); err != nil {
		t.Fatal(err)
	}
	return &cloned
}

func writeFixture(t testing.TB, fixture *Fixture) string {
	t.Helper()
	payload, err := json.Marshal(fixture)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), strings.ReplaceAll(fixture.Provenance.Name, "/", "-")+".json")
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
