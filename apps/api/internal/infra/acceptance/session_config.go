package infraacceptance

import (
	"net/url"
	"os"
	"strings"
	"time"

	applicationacceptance "github.com/shiyudesu/frux/internal/application/acceptance"
	multimodalprofile "github.com/shiyudesu/frux/internal/infra/multimodalprofile"
)

const (
	defaultSessionSemanticPollInterval = 250 * time.Millisecond
	defaultSessionSemanticStageTimeout = 30 * time.Second
)

func LoadSessionSemanticConfigFromEnv(
	timeoutOverride time.Duration,
) (applicationacceptance.SessionSemanticConfig, error) {
	apiEndpoint := strings.TrimRight(valueOrDefault("FRUX_SESSION_SEMANTIC_ACCEPTANCE_API_ENDPOINT", defaultAPIEndpoint), "/")
	config := applicationacceptance.SessionSemanticConfig{
		APIEndpoint:            apiEndpoint,
		APIMetricsEndpoint:     strings.TrimSpace(os.Getenv("FRUX_SESSION_SEMANTIC_ACCEPTANCE_API_METRICS_ENDPOINT")),
		AdapterMetricsEndpoint: strings.TrimSpace(os.Getenv("FRUX_SESSION_SEMANTIC_ACCEPTANCE_ADAPTER_METRICS_ENDPOINT")),
		PostgresDSN:            os.Getenv("FRUX_SESSION_SEMANTIC_ACCEPTANCE_POSTGRES_DSN"),
		UserAccount:            strings.TrimSpace(os.Getenv("FRUX_SESSION_SEMANTIC_ACCEPTANCE_USER_ACCOUNT")),
		UserPassword:           os.Getenv("FRUX_SESSION_SEMANTIC_ACCEPTANCE_USER_PASSWORD"),
		ExpectedProfile:        strings.TrimSpace(os.Getenv("FRUX_MULTIMODAL_PROFILE")),
	}
	if config.APIMetricsEndpoint == "" {
		config.APIMetricsEndpoint = apiEndpoint + "/metrics"
	}
	var err error
	if config.PositiveSeedVideoID, err = int64Env("FRUX_SESSION_SEMANTIC_ACCEPTANCE_POSITIVE_VIDEO_ID", 0); err != nil {
		return applicationacceptance.SessionSemanticConfig{}, err
	}
	if config.NegativeSeedVideoID, err = int64Env("FRUX_SESSION_SEMANTIC_ACCEPTANCE_NEGATIVE_VIDEO_ID", 0); err != nil {
		return applicationacceptance.SessionSemanticConfig{}, err
	}
	if config.ExpectedTargetVideoID, err = int64Env("FRUX_SESSION_SEMANTIC_ACCEPTANCE_TARGET_VIDEO_ID", 0); err != nil {
		return applicationacceptance.SessionSemanticConfig{}, err
	}
	if config.PollInterval, err = durationEnv("FRUX_SESSION_SEMANTIC_ACCEPTANCE_POLL_INTERVAL", defaultSessionSemanticPollInterval); err != nil {
		return applicationacceptance.SessionSemanticConfig{}, err
	}
	if config.StageTimeout, err = durationEnv("FRUX_SESSION_SEMANTIC_ACCEPTANCE_STAGE_TIMEOUT", defaultSessionSemanticStageTimeout); err != nil {
		return applicationacceptance.SessionSemanticConfig{}, err
	}
	if config.HTTPTimeout, err = durationEnv("FRUX_SESSION_SEMANTIC_ACCEPTANCE_HTTP_TIMEOUT", defaultHTTPTimeout); err != nil {
		return applicationacceptance.SessionSemanticConfig{}, err
	}
	if timeoutOverride > 0 {
		config.StageTimeout = timeoutOverride
	}
	if config.MaxResponseBytes, err = int64Env("FRUX_SESSION_SEMANTIC_ACCEPTANCE_MAX_RESPONSE_BYTES", defaultMaxResponseBytes); err != nil {
		return applicationacceptance.SessionSemanticConfig{}, err
	}
	if err := validateSessionSemanticConfig(config); err != nil {
		return applicationacceptance.SessionSemanticConfig{}, err
	}
	return config, nil
}

func validateSessionSemanticConfig(config applicationacceptance.SessionSemanticConfig) error {
	for field, value := range map[string]string{
		"FRUX_SESSION_SEMANTIC_ACCEPTANCE_POSTGRES_DSN":  config.PostgresDSN,
		"FRUX_SESSION_SEMANTIC_ACCEPTANCE_USER_ACCOUNT":  config.UserAccount,
		"FRUX_SESSION_SEMANTIC_ACCEPTANCE_USER_PASSWORD": config.UserPassword,
		"FRUX_MULTIMODAL_PROFILE":                        config.ExpectedProfile,
	} {
		if strings.TrimSpace(value) == "" {
			return &ConfigError{Field: field}
		}
	}
	for field, endpoint := range map[string]string{
		"FRUX_SESSION_SEMANTIC_ACCEPTANCE_API_ENDPOINT":         config.APIEndpoint,
		"FRUX_SESSION_SEMANTIC_ACCEPTANCE_API_METRICS_ENDPOINT": config.APIMetricsEndpoint,
	} {
		if validateEndpoint(endpoint) != nil {
			return &ConfigError{Field: field}
		}
	}
	if config.AdapterMetricsEndpoint != "" && validateEndpoint(config.AdapterMetricsEndpoint) != nil {
		return &ConfigError{Field: "FRUX_SESSION_SEMANTIC_ACCEPTANCE_ADAPTER_METRICS_ENDPOINT"}
	}
	postgresURL, err := url.Parse(strings.TrimSpace(config.PostgresDSN))
	if err != nil || (postgresURL.Scheme != "postgres" && postgresURL.Scheme != "postgresql") ||
		postgresURL.Host == "" || strings.TrimSpace(postgresURL.Path) == "" {
		return &ConfigError{Field: "FRUX_SESSION_SEMANTIC_ACCEPTANCE_POSTGRES_DSN"}
	}
	ids := []int64{config.PositiveSeedVideoID, config.NegativeSeedVideoID, config.ExpectedTargetVideoID}
	seen := map[int64]struct{}{}
	for _, videoID := range ids {
		if videoID <= 0 {
			return &ConfigError{Field: "FRUX_SESSION_SEMANTIC_ACCEPTANCE_VIDEO_IDS"}
		}
		if _, exists := seen[videoID]; exists {
			return &ConfigError{Field: "FRUX_SESSION_SEMANTIC_ACCEPTANCE_VIDEO_IDS"}
		}
		seen[videoID] = struct{}{}
	}
	if _, err := multimodalprofile.Resolve(config.ExpectedProfile); err != nil {
		return &ConfigError{Field: "FRUX_MULTIMODAL_PROFILE"}
	}
	if config.PollInterval < 100*time.Millisecond || config.PollInterval > 5*time.Second {
		return &ConfigError{Field: "FRUX_SESSION_SEMANTIC_ACCEPTANCE_POLL_INTERVAL"}
	}
	if config.StageTimeout < 5*time.Second || config.StageTimeout > 10*time.Minute {
		return &ConfigError{Field: "FRUX_SESSION_SEMANTIC_ACCEPTANCE_STAGE_TIMEOUT"}
	}
	if config.HTTPTimeout < time.Second || config.HTTPTimeout > 2*time.Minute || config.HTTPTimeout > config.StageTimeout {
		return &ConfigError{Field: "FRUX_SESSION_SEMANTIC_ACCEPTANCE_HTTP_TIMEOUT"}
	}
	if config.MaxResponseBytes < 64<<10 || config.MaxResponseBytes > 8<<20 {
		return &ConfigError{Field: "FRUX_SESSION_SEMANTIC_ACCEPTANCE_MAX_RESPONSE_BYTES"}
	}
	return nil
}
