package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	domainofflineevaluation "github.com/shiyudesu/frux/internal/domain/offlineevaluation"
	infraofflineevaluation "github.com/shiyudesu/frux/internal/infra/offlineevaluation"
)

func TestCommandDefaultsToValidationWithoutRuntimeCalls(t *testing.T) {
	root := filepath.Join("..", "..", "testdata", "recommendation-offline", "kuairec-v2")
	var output bytes.Buffer
	if err := run([]string{"public-dataset", "--root", root}, &output, nil); err != nil {
		t.Fatal(err)
	}
	var report validationReport
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Mode != modeValidation || report.Result != "validated" || report.ExternalModelCalls != 0 ||
		report.Manifest == nil || report.Manifest.Dataset != "kuairec" || len(report.Baselines) != 7 {
		t.Fatalf("report=%#v", report)
	}
	if bytes.Contains(output.Bytes(), []byte(root)) {
		t.Fatalf("report leaked root: %s", output.Bytes())
	}
}

func TestCommandExecutesReplayAndGoldenTracks(t *testing.T) {
	root := filepath.Join("..", "..", "testdata", "recommendation-offline")
	for _, test := range []struct {
		name      string
		arguments []string
		track     string
	}{
		{name: "replay", track: "production_replay", arguments: []string{
			"replay", "--input", filepath.Join(root, "replay-v1", "bundle.json"),
			"--baseline", filepath.Join(root, "replay-v1", "baseline.json"),
			"--candidate", filepath.Join(root, "replay-v1", "candidate.json"), "--k", "1,2",
		}},
		{name: "golden", track: "human_golden", arguments: []string{
			"golden", "--input", filepath.Join(root, "golden-v1.json"), "--k", "1,3",
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			jsonPath := filepath.Join(directory, "report.json")
			markdownPath := filepath.Join(directory, "report.md")
			arguments := append(append([]string(nil), test.arguments...), "--evaluate", "--output-json", jsonPath, "--output-markdown", markdownPath)
			var output bytes.Buffer
			if err := run(arguments, &output, nil); err != nil {
				t.Fatal(err)
			}
			var envelope struct {
				Track              string `json:"track"`
				Result             string `json:"result"`
				ExternalModelCalls int    `json:"external_model_calls"`
			}
			if err := json.Unmarshal(output.Bytes(), &envelope); err != nil || envelope.Track != test.track ||
				envelope.Result != "success" || envelope.ExternalModelCalls != 0 {
				t.Fatalf("envelope=%#v err=%v", envelope, err)
			}
			for _, path := range []string{jsonPath, markdownPath} {
				info, err := os.Stat(path)
				if err != nil {
					t.Fatal(err)
				}
				if info.Mode().Perm() != 0o600 {
					t.Fatalf("mode=%o", info.Mode().Perm())
				}
			}
		})
	}
}

func TestCommandRequiresExecutorAndOutputPairForEvaluation(t *testing.T) {
	root := filepath.Join("..", "..", "testdata", "recommendation-offline", "kuairec-v2")
	arguments := []string{
		"public-dataset", "--root", root, "--evaluate",
		"--output-json", filepath.Join(t.TempDir(), "report.json"),
		"--output-markdown", filepath.Join(t.TempDir(), "report.md"),
	}
	if err := run(arguments, &bytes.Buffer{}, nil); err == nil {
		t.Fatal("expected unavailable executor")
	}
	var output bytes.Buffer
	err := run(arguments, &output, func(
		_ context.Context,
		_ commandOptions,
		_ *infraofflineevaluation.LoadedManifest,
	) (infraofflineevaluation.PublicReport, error) {
		return infraofflineevaluation.PublicReport{
			Version: domainofflineevaluation.ReportVersion, Kind: domainofflineevaluation.ReportKind,
			Track: domainofflineevaluation.TrackPublicDataset, Result: "success",
		}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var report infraofflineevaluation.PublicReport
	if err := json.Unmarshal(output.Bytes(), &report); err != nil || report.Result != "success" {
		t.Fatalf("report=%#v err=%v", report, err)
	}
}

func TestCommandRejectsUnsafeAndDuplicateK(t *testing.T) {
	for _, arguments := range [][]string{
		{"public-dataset", "--root", "data", "--manifest", "../manifest.json"},
		{"public-dataset", "--root", "data", "--k", "5,5"},
		{"replay"},
		{"unknown"},
	} {
		if _, err := parseOptions(arguments); err == nil {
			t.Fatalf("accepted arguments=%v", arguments)
		}
	}
}
