package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	multimodalprofile "github.com/shiyudesu/frux/internal/infra/multimodalprofile"
	rollout "github.com/shiyudesu/frux/internal/infra/rollout"
)

func TestRunLoadsBoundedEnvironmentAndWritesReport(t *testing.T) {
	reportPath := filepath.Join(t.TempDir(), "rollout.json")
	setCommandEnvironment(t, reportPath)
	var captured rollout.Config
	var execute bool
	var gate string
	executor := func(_ context.Context, config rollout.Config, executeValue bool, gateValue string) (rollout.OperatorReport, error) {
		captured, execute, gate = config, executeValue, gateValue
		return rollout.OperatorReport{
			Schema: rollout.OperatorReportSchemaV1, ToolVersion: rollout.OperatorToolVersionV1,
			Action: string(config.Action), Mode: "mutation_requested", Result: "success", TargetVersion: config.TargetVersion,
		}, nil
	}
	var output bytes.Buffer
	if err := run([]string{"--action", "create", "--execute"}, &output, executor); err != nil {
		t.Fatal(err)
	}
	if captured.Action != rollout.ActionCreate || captured.TargetVersion != 3 || !execute || gate != "true" || output.Len() == 0 {
		t.Fatalf("config=%#v execute=%v gate=%q output=%q", captured, execute, gate, output.String())
	}
	info, err := os.Stat(reportPath)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode=%v error=%v", info.Mode().Perm(), err)
	}
}

func TestParseOptionsRejectsUnsafeCombinations(t *testing.T) {
	for _, arguments := range [][]string{
		{"--action", "unknown"},
		{"--action", "plan", "--execute"},
		{"--action", "status", "extra"},
	} {
		if _, err := parseOptions(arguments); err == nil {
			t.Fatalf("arguments=%v", arguments)
		}
	}
}

func setCommandEnvironment(t testing.TB, reportPath string) {
	t.Helper()
	t.Setenv("FRUX_SESSION_SEMANTIC_ROLLOUT_POSTGRES_DSN", "postgres://frux:secret@127.0.0.1:5432/frux?sslmode=disable")
	t.Setenv("FRUX_SESSION_SEMANTIC_ROLLOUT_API_METRICS_ENDPOINT", "http://127.0.0.1:8080/metrics")
	t.Setenv("FRUX_SESSION_SEMANTIC_ROLLOUT_SHADOW_REPORT", "/tmp/shadow.json")
	t.Setenv("FRUX_SESSION_SEMANTIC_ROLLOUT_SOURCE_VERSION", "2")
	t.Setenv("FRUX_SESSION_SEMANTIC_ROLLOUT_TARGET_VERSION", "3")
	t.Setenv("FRUX_SESSION_SEMANTIC_ROLLOUT_PERCENTAGE", "1")
	t.Setenv("FRUX_SESSION_SEMANTIC_ROLLOUT_REPORT", reportPath)
	t.Setenv("FRUX_SESSION_SEMANTIC_ROLLOUT_ALLOW_MUTATION", "true")
	t.Setenv("FRUX_MULTIMODAL_PROFILE", multimodalprofile.TongyiFlashSnapshotProfile)
}
