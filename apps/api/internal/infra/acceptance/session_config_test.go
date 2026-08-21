package infraacceptance

import (
	"errors"
	"testing"
	"time"

	multimodalprofile "github.com/shiyudesu/frux/internal/infra/multimodalprofile"
)

func TestLoadSessionSemanticConfigFromEnv(t *testing.T) {
	setSessionSemanticConfigEnvironment(t)
	config, err := LoadSessionSemanticConfigFromEnv(45 * time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if config.APIEndpoint != defaultAPIEndpoint || config.APIMetricsEndpoint != defaultAPIEndpoint+"/metrics" ||
		config.PositiveSeedVideoID != 11 || config.NegativeSeedVideoID != 12 || config.ExpectedTargetVideoID != 13 ||
		config.StageTimeout != 45*time.Second {
		t.Fatalf("config=%#v", config)
	}
}

func TestLoadSessionSemanticConfigRejectsDuplicateAndRemoteValues(t *testing.T) {
	setSessionSemanticConfigEnvironment(t)
	t.Setenv("FRUX_SESSION_SEMANTIC_ACCEPTANCE_TARGET_VIDEO_ID", "11")
	if _, err := LoadSessionSemanticConfigFromEnv(0); !errors.Is(err, ErrInvalidAcceptanceConfig) {
		t.Fatalf("duplicate error=%v", err)
	}
	setSessionSemanticConfigEnvironment(t)
	t.Setenv("FRUX_SESSION_SEMANTIC_ACCEPTANCE_API_ENDPOINT", "http://example.com")
	if _, err := LoadSessionSemanticConfigFromEnv(0); !errors.Is(err, ErrInvalidAcceptanceConfig) {
		t.Fatalf("remote error=%v", err)
	}
}

func setSessionSemanticConfigEnvironment(t testing.TB) {
	t.Helper()
	values := map[string]string{
		"FRUX_SESSION_SEMANTIC_ACCEPTANCE_API_ENDPOINT":             "",
		"FRUX_SESSION_SEMANTIC_ACCEPTANCE_API_METRICS_ENDPOINT":     "",
		"FRUX_SESSION_SEMANTIC_ACCEPTANCE_ADAPTER_METRICS_ENDPOINT": "",
		"FRUX_SESSION_SEMANTIC_ACCEPTANCE_POSTGRES_DSN":             "postgres://frux:secret@127.0.0.1:5432/frux?sslmode=disable",
		"FRUX_SESSION_SEMANTIC_ACCEPTANCE_USER_ACCOUNT":             "acceptance-user",
		"FRUX_SESSION_SEMANTIC_ACCEPTANCE_USER_PASSWORD":            "acceptance-secret",
		"FRUX_SESSION_SEMANTIC_ACCEPTANCE_POSITIVE_VIDEO_ID":        "11",
		"FRUX_SESSION_SEMANTIC_ACCEPTANCE_NEGATIVE_VIDEO_ID":        "12",
		"FRUX_SESSION_SEMANTIC_ACCEPTANCE_TARGET_VIDEO_ID":          "13",
		"FRUX_SESSION_SEMANTIC_ACCEPTANCE_POLL_INTERVAL":            "",
		"FRUX_SESSION_SEMANTIC_ACCEPTANCE_STAGE_TIMEOUT":            "",
		"FRUX_SESSION_SEMANTIC_ACCEPTANCE_HTTP_TIMEOUT":             "",
		"FRUX_SESSION_SEMANTIC_ACCEPTANCE_MAX_RESPONSE_BYTES":       "",
		"FRUX_MULTIMODAL_PROFILE":                                   multimodalprofile.TongyiFlashSnapshotProfile,
	}
	for name, value := range values {
		t.Setenv(name, value)
	}
}
