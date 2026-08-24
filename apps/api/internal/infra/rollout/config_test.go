package rollout

import (
	"errors"
	"testing"

	multimodalprofile "github.com/shiyudesu/frux/internal/infra/multimodalprofile"
)

func TestLoadConfigFromEnvScopesActionRequirements(t *testing.T) {
	setRolloutConfigEnv(t)
	config, err := LoadConfigFromEnv(ActionPlan)
	if err != nil || config.TargetVersion != 3 || config.SourceVersion != 2 ||
		config.RolloutPercentage != 1 || config.ExpectedProfile != multimodalprofile.TongyiFlashSnapshotProfile {
		t.Fatalf("config=%#v error=%v", config, err)
	}
	t.Setenv("FRUX_MULTIMODAL_PROFILE", "")
	t.Setenv("FRUX_SESSION_SEMANTIC_ROLLOUT_SHADOW_REPORT", "")
	t.Setenv("FRUX_SESSION_SEMANTIC_ROLLOUT_API_METRICS_ENDPOINT", "")
	disable, err := LoadConfigFromEnv(ActionDisable)
	if err != nil || disable.TargetVersion != 3 {
		t.Fatalf("disable=%#v error=%v", disable, err)
	}
	if _, err := LoadConfigFromEnv(ActionPlan); !errors.Is(err, ErrInvalidRolloutConfig) {
		t.Fatalf("plan error=%v", err)
	}
}

func TestLoadConfigFromEnvRejectsBoundsAndMutationNeedsBothGates(t *testing.T) {
	setRolloutConfigEnv(t)
	t.Setenv("FRUX_SESSION_SEMANTIC_ROLLOUT_PERCENTAGE", "6")
	if _, err := LoadConfigFromEnv(ActionCreate); !errors.Is(err, ErrInvalidRolloutConfig) {
		t.Fatalf("error=%v", err)
	}
	if MutationAllowed(ActionCreate, false, "true") || MutationAllowed(ActionCreate, true, "false") ||
		!MutationAllowed(ActionCreate, true, "true") || MutationAllowed(ActionPlan, true, "true") {
		t.Fatal("mutation acknowledgement was not double gated")
	}
}

func TestLoadConfigFromEnvRequiresExplicitFullRolloutAcknowledgement(t *testing.T) {
	setRolloutConfigEnv(t)
	t.Setenv("FRUX_SESSION_SEMANTIC_ROLLOUT_TARGET_VERSION", "4")
	t.Setenv("FRUX_SESSION_SEMANTIC_ROLLOUT_PERCENTAGE", "100")
	if _, err := LoadConfigFromEnv(ActionPlan); !errors.Is(err, ErrInvalidRolloutConfig) {
		t.Fatalf("full rollout without acknowledgement error=%v", err)
	}
	t.Setenv(FullRolloutGate, "true")
	config, err := LoadConfigFromEnv(ActionPlan)
	if err != nil || !config.AllowFullRollout || config.RolloutPercentage != 100 {
		t.Fatalf("config=%#v error=%v", config, err)
	}
	t.Setenv("FRUX_SESSION_SEMANTIC_ROLLOUT_PERCENTAGE", "50")
	if _, err := LoadConfigFromEnv(ActionPlan); !errors.Is(err, ErrInvalidRolloutConfig) {
		t.Fatalf("intermediate rollout error=%v", err)
	}
	t.Setenv(FullRolloutGate, "not-a-bool")
	if _, err := LoadConfigFromEnv(ActionPlan); !errors.Is(err, ErrInvalidRolloutConfig) {
		t.Fatalf("invalid acknowledgement error=%v", err)
	}
}

func setRolloutConfigEnv(t testing.TB) {
	t.Helper()
	t.Setenv("FRUX_SESSION_SEMANTIC_ROLLOUT_POSTGRES_DSN", "postgres://frux:secret@127.0.0.1:5432/frux?sslmode=disable")
	t.Setenv("FRUX_SESSION_SEMANTIC_ROLLOUT_API_METRICS_ENDPOINT", "http://127.0.0.1:8080/metrics")
	t.Setenv("FRUX_SESSION_SEMANTIC_ROLLOUT_SHADOW_REPORT", "/tmp/shadow.json")
	t.Setenv("FRUX_SESSION_SEMANTIC_ROLLOUT_SOURCE_VERSION", "2")
	t.Setenv("FRUX_SESSION_SEMANTIC_ROLLOUT_TARGET_VERSION", "3")
	t.Setenv("FRUX_SESSION_SEMANTIC_ROLLOUT_PERCENTAGE", "1")
	t.Setenv(FullRolloutGate, "false")
	t.Setenv("FRUX_SESSION_SEMANTIC_ROLLOUT_HTTP_TIMEOUT", "3s")
	t.Setenv("FRUX_SESSION_SEMANTIC_ROLLOUT_MAX_RESPONSE_BYTES", "2097152")
	t.Setenv("FRUX_MULTIMODAL_PROFILE", multimodalprofile.TongyiFlashSnapshotProfile)
}
