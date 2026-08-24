package rollout

import (
	"errors"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	applicationrecommendation "github.com/shiyudesu/frux/internal/application/recommendation"
	domainembedding "github.com/shiyudesu/frux/internal/domain/embedding"
	multimodalprofile "github.com/shiyudesu/frux/internal/infra/multimodalprofile"
)

type Action string

const (
	ActionPlan     Action = "plan"
	ActionCreate   Action = "create"
	ActionActivate Action = "activate"
	ActionStatus   Action = "status"
	ActionDisable  Action = "disable"
)

const MutationGate = "FRUX_SESSION_SEMANTIC_ROLLOUT_ALLOW_MUTATION"
const FullRolloutGate = "FRUX_SESSION_SEMANTIC_ROLLOUT_ALLOW_FULL"

var ErrInvalidRolloutConfig = errors.New("invalid session semantic rollout configuration")

type Config struct {
	Action            Action
	PostgresDSN       string
	MetricsEndpoint   string
	ShadowReportPath  string
	SourceVersion     int
	TargetVersion     int
	RolloutPercentage int
	AllowFullRollout  bool
	HTTPTimeout       time.Duration
	MaxResponseBytes  int64
	Contract          domainembedding.MultimodalContractIdentity
	ExpectedProfile   string
}

func LoadConfigFromEnv(action Action) (Config, error) {
	if !ValidAction(action) {
		return Config{}, ErrInvalidRolloutConfig
	}
	config := Config{
		Action:           action,
		PostgresDSN:      strings.TrimSpace(os.Getenv("FRUX_SESSION_SEMANTIC_ROLLOUT_POSTGRES_DSN")),
		MetricsEndpoint:  strings.TrimSpace(os.Getenv("FRUX_SESSION_SEMANTIC_ROLLOUT_API_METRICS_ENDPOINT")),
		ShadowReportPath: strings.TrimSpace(os.Getenv("FRUX_SESSION_SEMANTIC_ROLLOUT_SHADOW_REPORT")),
		ExpectedProfile:  strings.TrimSpace(os.Getenv("FRUX_MULTIMODAL_PROFILE")),
	}
	var err error
	if config.SourceVersion, err = rolloutIntEnv("FRUX_SESSION_SEMANTIC_ROLLOUT_SOURCE_VERSION", 2); err != nil {
		return Config{}, err
	}
	if config.TargetVersion, err = rolloutIntEnv("FRUX_SESSION_SEMANTIC_ROLLOUT_TARGET_VERSION", 3); err != nil {
		return Config{}, err
	}
	if config.RolloutPercentage, err = rolloutIntEnv("FRUX_SESSION_SEMANTIC_ROLLOUT_PERCENTAGE", 1); err != nil {
		return Config{}, err
	}
	if config.AllowFullRollout, err = rolloutBoolEnv(FullRolloutGate, false); err != nil {
		return Config{}, err
	}
	if config.HTTPTimeout, err = rolloutDurationEnv("FRUX_SESSION_SEMANTIC_ROLLOUT_HTTP_TIMEOUT", 3*time.Second); err != nil {
		return Config{}, err
	}
	if config.MaxResponseBytes, err = rolloutInt64Env("FRUX_SESSION_SEMANTIC_ROLLOUT_MAX_RESPONSE_BYTES", 2<<20); err != nil {
		return Config{}, err
	}
	if action != ActionDisable {
		profile, resolveErr := multimodalprofile.Resolve(config.ExpectedProfile)
		if resolveErr != nil {
			return Config{}, ErrInvalidRolloutConfig
		}
		config.ExpectedProfile = profile.ID
		config.Contract = profile.Contract
	} else {
		config.ExpectedProfile = ""
		config.SourceVersion = 0
		config.RolloutPercentage = 0
		config.AllowFullRollout = false
	}
	if err := validateConfig(config); err != nil {
		return Config{}, err
	}
	return config, nil
}

func ValidAction(action Action) bool {
	switch action {
	case ActionPlan, ActionCreate, ActionActivate, ActionStatus, ActionDisable:
		return true
	default:
		return false
	}
}

func MutatingAction(action Action) bool {
	switch action {
	case ActionCreate, ActionActivate, ActionDisable:
		return true
	default:
		return false
	}
}

func MutationAllowed(action Action, execute bool, gate string) bool {
	if !MutatingAction(action) {
		return false
	}
	value, err := strconv.ParseBool(strings.TrimSpace(gate))
	return execute && err == nil && value
}

func validateConfig(config Config) error {
	postgresURL, err := url.Parse(config.PostgresDSN)
	if err != nil || (postgresURL.Scheme != "postgres" && postgresURL.Scheme != "postgresql") ||
		postgresURL.Host == "" || strings.TrimSpace(postgresURL.Path) == "" ||
		config.TargetVersion <= 0 || config.TargetVersion > 1_000_000_000 ||
		config.HTTPTimeout < 100*time.Millisecond || config.HTTPTimeout > 30*time.Second ||
		config.MaxResponseBytes < 64<<10 || config.MaxResponseBytes > 8<<20 {
		return ErrInvalidRolloutConfig
	}
	if config.Action == ActionDisable {
		return nil
	}
	if config.SourceVersion <= 0 || config.TargetVersion <= config.SourceVersion ||
		!validConfiguredRolloutPercentage(config.RolloutPercentage, config.AllowFullRollout) ||
		config.ExpectedProfile == "" {
		return ErrInvalidRolloutConfig
	}
	if (config.Action == ActionPlan || config.Action == ActionCreate || config.Action == ActionActivate) &&
		(config.MetricsEndpoint == "" || config.ShadowReportPath == "") {
		return ErrInvalidRolloutConfig
	}
	return nil
}

func validConfiguredRolloutPercentage(percentage int, allowFull bool) bool {
	if percentage >= applicationrecommendation.MinSessionSemanticRolloutPercentage &&
		percentage <= applicationrecommendation.MaxSessionSemanticRolloutPercentage {
		return true
	}
	return allowFull && percentage == applicationrecommendation.FullSessionSemanticRolloutPercentage
}

func rolloutBoolEnv(name string, fallback bool) (bool, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, ErrInvalidRolloutConfig
	}
	return parsed, nil
}

func rolloutIntEnv(name string, fallback int) (int, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, ErrInvalidRolloutConfig
	}
	return parsed, nil
}

func rolloutInt64Env(name string, fallback int64) (int64, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, ErrInvalidRolloutConfig
	}
	return parsed, nil
}

func rolloutDurationEnv(name string, fallback time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, ErrInvalidRolloutConfig
	}
	return parsed, nil
}
