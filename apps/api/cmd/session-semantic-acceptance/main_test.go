package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	applicationacceptance "github.com/shiyudesu/frux/internal/application/acceptance"
	multimodalprofile "github.com/shiyudesu/frux/internal/infra/multimodalprofile"
)

func TestCommandDefaultsToValidationAndRequiresBothGates(t *testing.T) {
	setSessionCommandEnvironment(t)
	for _, test := range []struct {
		arguments []string
		gate      string
		wantMode  applicationacceptance.Mode
	}{
		{wantMode: applicationacceptance.ModeValidation},
		{arguments: []string{"--execute"}, wantMode: applicationacceptance.ModeValidation},
		{gate: "true", wantMode: applicationacceptance.ModeValidation},
		{arguments: []string{"--execute"}, gate: "true", wantMode: applicationacceptance.ModeExecution},
	} {
		t.Run(string(test.wantMode)+test.gate, func(t *testing.T) {
			t.Setenv(applicationacceptance.SessionSemanticMutationGate, test.gate)
			var output bytes.Buffer
			called := 0
			err := runWithExecutor(test.arguments, &output, func(_ context.Context, _ applicationacceptance.SessionSemanticConfig, report applicationacceptance.SessionSemanticReport) (applicationacceptance.SessionSemanticReport, error) {
				called++
				if report.Mode != test.wantMode {
					t.Fatalf("mode=%s want=%s", report.Mode, test.wantMode)
				}
				report.Result = applicationacceptance.ResultSuccess
				return report, nil
			})
			if err != nil || called != 1 {
				t.Fatalf("called=%d err=%v output=%s", called, err, output.String())
			}
		})
	}
}

func TestCommandWritesRestrictedRedactedReport(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.json")
	report := applicationacceptance.NewSessionSemanticReport("run", applicationacceptance.ModeExecution, time.Now(), true)
	report.Result = applicationacceptance.ResultSuccess
	report.Request = &applicationacceptance.SessionRequestEvidence{RequestID: "bounded-id"}
	if err := emitReport(&bytes.Buffer{}, path, report); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var decoded applicationacceptance.SessionSemanticReport
	if err := json.Unmarshal(content, &decoded); err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 || bytes.Contains(content, []byte("secret-value")) || bytes.Contains(content, []byte("signed-cursor")) {
		t.Fatalf("mode=%o content=%s", info.Mode().Perm(), content)
	}
}

func setSessionCommandEnvironment(t testing.TB) {
	t.Helper()
	values := map[string]string{
		"FRUX_SESSION_SEMANTIC_ACCEPTANCE_POSTGRES_DSN":      "postgres://frux:secret-value@127.0.0.1:5432/frux?sslmode=disable",
		"FRUX_SESSION_SEMANTIC_ACCEPTANCE_USER_ACCOUNT":      "acceptance-user",
		"FRUX_SESSION_SEMANTIC_ACCEPTANCE_USER_PASSWORD":     "secret-value",
		"FRUX_SESSION_SEMANTIC_ACCEPTANCE_POSITIVE_VIDEO_ID": "11",
		"FRUX_SESSION_SEMANTIC_ACCEPTANCE_NEGATIVE_VIDEO_ID": "12",
		"FRUX_SESSION_SEMANTIC_ACCEPTANCE_TARGET_VIDEO_ID":   "13",
		"FRUX_MULTIMODAL_PROFILE":                            multimodalprofile.TongyiFlashSnapshotProfile,
	}
	for name, value := range values {
		t.Setenv(name, value)
	}
	for _, name := range []string{
		"FRUX_SESSION_SEMANTIC_ACCEPTANCE_API_ENDPOINT",
		"FRUX_SESSION_SEMANTIC_ACCEPTANCE_API_METRICS_ENDPOINT",
		"FRUX_SESSION_SEMANTIC_ACCEPTANCE_ADAPTER_METRICS_ENDPOINT",
		"FRUX_SESSION_SEMANTIC_ACCEPTANCE_POLL_INTERVAL",
		"FRUX_SESSION_SEMANTIC_ACCEPTANCE_STAGE_TIMEOUT",
		"FRUX_SESSION_SEMANTIC_ACCEPTANCE_HTTP_TIMEOUT",
		"FRUX_SESSION_SEMANTIC_ACCEPTANCE_MAX_RESPONSE_BYTES",
	} {
		t.Setenv(name, "")
	}
}
