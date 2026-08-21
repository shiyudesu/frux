package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	applicationacceptance "github.com/shiyudesu/frux/internal/application/acceptance"
)

func TestRunDefaultsToNonBillableValidation(t *testing.T) {
	setCommandAcceptanceEnvironment(t)
	var output bytes.Buffer
	if err := run(nil, &output); err != nil {
		t.Fatal(err)
	}
	var report applicationacceptance.Report
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Mode != applicationacceptance.ModeValidation || report.Result != applicationacceptance.ResultSuccess ||
		report.PlannedModelCalls != 3 {
		t.Fatalf("report=%#v", report)
	}
}

func TestRunRequiresBothBillableGates(t *testing.T) {
	setCommandAcceptanceEnvironment(t)
	var output bytes.Buffer
	if err := run([]string{"--execute"}, &output); err != nil {
		t.Fatal(err)
	}
	var report applicationacceptance.Report
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Mode != applicationacceptance.ModeValidation {
		t.Fatalf("report=%#v", report)
	}
}

func TestLoadEnvironmentBeforeBillableDecision(t *testing.T) {
	root := t.TempDir()
	workingDirectory := filepath.Join(root, "apps", "api")
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(workingDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(root, "apps", ".env.acceptance"),
		[]byte(applicationacceptance.BillableAcknowledgementEnvironment+"=true\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	oldWorkingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(workingDirectory); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWorkingDirectory) })
	oldValue, existed := os.LookupEnv(applicationacceptance.BillableAcknowledgementEnvironment)
	if err := os.Unsetenv(applicationacceptance.BillableAcknowledgementEnvironment); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if existed {
			_ = os.Setenv(applicationacceptance.BillableAcknowledgementEnvironment, oldValue)
		} else {
			_ = os.Unsetenv(applicationacceptance.BillableAcknowledgementEnvironment)
		}
	})

	decision, err := loadEnvironmentAndDecide(true)
	if err != nil {
		t.Fatal(err)
	}
	if !decision.Confirmed || decision.Mode != applicationacceptance.ModeExecution {
		t.Fatalf("decision=%#v", decision)
	}
}

func TestRunWritesRestrictedReportWithoutSecrets(t *testing.T) {
	setCommandAcceptanceEnvironment(t)
	path := filepath.Join(t.TempDir(), "report.json")
	var output bytes.Buffer
	if err := run([]string{"--report", path}, &output); err != nil {
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
	if info.Mode().Perm() != 0o600 || bytes.Contains(content, []byte("secret-value")) {
		t.Fatalf("mode=%o content=%s", info.Mode().Perm(), content)
	}
}

func setCommandAcceptanceEnvironment(t testing.TB) {
	t.Helper()
	directory := t.TempDir()
	videoPath := filepath.Join(directory, "fixture.mp4")
	coverPath := filepath.Join(directory, "cover.jpg")
	if err := os.WriteFile(videoPath, []byte("video"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(coverPath, []byte("cover"), 0o600); err != nil {
		t.Fatal(err)
	}
	values := map[string]string{
		"FRUX_ACCEPTANCE_POSTGRES_DSN":                           "postgres://frux:secret-value@127.0.0.1:5432/frux?sslmode=disable",
		"FRUX_ACCEPTANCE_USER_ACCOUNT":                           "user",
		"FRUX_ACCEPTANCE_USER_PASSWORD":                          "user-secret-value",
		"FRUX_ACCEPTANCE_ADMIN_ACCOUNT":                          "admin",
		"FRUX_ACCEPTANCE_ADMIN_PASSWORD":                         "admin-secret-value",
		"FRUX_ACCEPTANCE_VIDEO_FIXTURE":                          videoPath,
		"FRUX_ACCEPTANCE_COVER_FIXTURE":                          coverPath,
		"FRUX_MULTIMODAL_PROFILE":                                "tongyi-embedding-vision-flash-2026-03-06",
		applicationacceptance.BillableAcknowledgementEnvironment: "",
	}
	for name, value := range values {
		t.Setenv(name, value)
	}
	for _, name := range []string{"FRUX_ACCEPTANCE_QUERY", "FRUX_MULTIMODAL_ENDPOINT"} {
		t.Setenv(name, "")
	}
	if strings.Contains(os.Getenv("FRUX_ACCEPTANCE_POSTGRES_DSN"), "\n") {
		t.Fatal("invalid test DSN")
	}
}
