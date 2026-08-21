package infraacceptance

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadConfigFromEnvAcceptsBoundedLocalConfiguration(t *testing.T) {
	setAcceptanceTestEnvironment(t)
	config, err := LoadConfigFromEnv("", 0)
	if err != nil {
		t.Fatal(err)
	}
	if config.APIEndpoint != defaultAPIEndpoint || config.AdapterEndpoint != defaultAdapterEndpoint ||
		config.Query != "雨夜城市" || config.PollInterval != defaultPollInterval ||
		config.StageTimeout != defaultStageTimeout || config.MaxResponseBytes != defaultMaxResponseBytes {
		t.Fatalf("config=%#v", config)
	}
}

func TestLoadConfigFromEnvRejectsMissingSecretWithoutValueLeak(t *testing.T) {
	setAcceptanceTestEnvironment(t)
	t.Setenv("FRUX_ACCEPTANCE_ADMIN_PASSWORD", "")
	_, err := LoadConfigFromEnv("", 0)
	if !errors.Is(err, ErrInvalidAcceptanceConfig) || !strings.Contains(err.Error(), "FRUX_ACCEPTANCE_ADMIN_PASSWORD") {
		t.Fatalf("error=%v", err)
	}
	if strings.Contains(err.Error(), "admin-secret-value") {
		t.Fatalf("secret leaked in error: %v", err)
	}
}

func TestLoadConfigFromEnvRejectsRemoteHTTPAndBounds(t *testing.T) {
	for _, test := range []struct {
		name  string
		value string
	}{
		{name: "FRUX_ACCEPTANCE_API_ENDPOINT", value: "http://example.com"},
		{name: "FRUX_ACCEPTANCE_POLL_INTERVAL", value: "10ms"},
		{name: "FRUX_ACCEPTANCE_STAGE_TIMEOUT", value: "1s"},
		{name: "FRUX_ACCEPTANCE_MAX_RESPONSE_BYTES", value: "1024"},
	} {
		t.Run(test.name, func(t *testing.T) {
			setAcceptanceTestEnvironment(t)
			t.Setenv(test.name, test.value)
			if _, err := LoadConfigFromEnv("", 0); !errors.Is(err, ErrInvalidAcceptanceConfig) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestLoadConfigFromEnvUsesOverrides(t *testing.T) {
	setAcceptanceTestEnvironment(t)
	config, err := LoadConfigFromEnv("query override", 45*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if config.Query != "query override" || config.StageTimeout != 45*time.Second {
		t.Fatalf("config=%#v", config)
	}
}

func setAcceptanceTestEnvironment(t testing.TB) {
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
		"FRUX_ACCEPTANCE_POSTGRES_DSN":   "postgres://frux:secret@127.0.0.1:5432/frux?sslmode=disable",
		"FRUX_ACCEPTANCE_USER_ACCOUNT":   "acceptance-user",
		"FRUX_ACCEPTANCE_USER_PASSWORD":  "user-secret-value",
		"FRUX_ACCEPTANCE_ADMIN_ACCOUNT":  "acceptance-admin",
		"FRUX_ACCEPTANCE_ADMIN_PASSWORD": "admin-secret-value",
		"FRUX_ACCEPTANCE_VIDEO_FIXTURE":  videoPath,
		"FRUX_ACCEPTANCE_COVER_FIXTURE":  coverPath,
		"FRUX_MULTIMODAL_PROFILE":        "tongyi-embedding-vision-flash-2026-03-06",
	}
	for name, value := range values {
		t.Setenv(name, value)
	}
	for _, name := range []string{
		"FRUX_ACCEPTANCE_API_ENDPOINT",
		"FRUX_ACCEPTANCE_ADAPTER_ENDPOINT",
		"FRUX_ACCEPTANCE_WORKER_METRICS_ENDPOINT",
		"FRUX_ACCEPTANCE_QUERY",
		"FRUX_ACCEPTANCE_POLL_INTERVAL",
		"FRUX_ACCEPTANCE_STAGE_TIMEOUT",
		"FRUX_ACCEPTANCE_HTTP_TIMEOUT",
		"FRUX_ACCEPTANCE_MAX_RESPONSE_BYTES",
		"FRUX_MULTIMODAL_ENDPOINT",
	} {
		t.Setenv(name, "")
	}
}
