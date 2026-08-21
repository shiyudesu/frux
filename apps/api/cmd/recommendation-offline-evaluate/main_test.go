package main

import (
	"bytes"
	"context"
	"encoding/json"
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
