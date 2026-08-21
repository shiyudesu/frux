package infraacceptance

import (
	"errors"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	applicationacceptance "github.com/shiyudesu/frux/internal/application/acceptance"
)

const (
	defaultAPIEndpoint           = "http://127.0.0.1:8080"
	defaultAdapterEndpoint       = "http://127.0.0.1:8099"
	defaultWorkerMetricsEndpoint = "http://127.0.0.1:9091/metrics"
	defaultPollInterval          = 500 * time.Millisecond
	defaultStageTimeout          = 2 * time.Minute
	defaultHTTPTimeout           = 20 * time.Second
	defaultMaxResponseBytes      = 2 << 20
	maxVideoFixtureBytes         = 512 << 20
	maxCoverFixtureBytes         = 20 << 20
)

var ErrInvalidAcceptanceConfig = errors.New("invalid multimodal acceptance config")

type ConfigError struct {
	Field string
}

func (e *ConfigError) Error() string {
	if e == nil || e.Field == "" {
		return ErrInvalidAcceptanceConfig.Error()
	}
	return e.Field + ": " + ErrInvalidAcceptanceConfig.Error()
}

func (e *ConfigError) Unwrap() error { return ErrInvalidAcceptanceConfig }

func LoadConfigFromEnv(queryOverride string, timeoutOverride time.Duration) (applicationacceptance.Config, error) {
	apiEndpoint := valueOrDefault("FRUX_ACCEPTANCE_API_ENDPOINT", defaultAPIEndpoint)
	adapterEndpoint := strings.TrimSpace(os.Getenv("FRUX_ACCEPTANCE_ADAPTER_ENDPOINT"))
	if adapterEndpoint == "" {
		adapterEndpoint = strings.TrimSpace(os.Getenv("FRUX_MULTIMODAL_ENDPOINT"))
	}
	if adapterEndpoint == "" {
		adapterEndpoint = defaultAdapterEndpoint
	}
	query := strings.TrimSpace(queryOverride)
	if query == "" {
		query = valueOrDefault("FRUX_ACCEPTANCE_QUERY", "雨夜城市")
	}
	config := applicationacceptance.Config{
		APIEndpoint:           strings.TrimRight(apiEndpoint, "/"),
		AdapterEndpoint:       strings.TrimRight(adapterEndpoint, "/"),
		APIMetricsEndpoint:    strings.TrimRight(apiEndpoint, "/") + "/metrics",
		WorkerMetricsEndpoint: valueOrDefault("FRUX_ACCEPTANCE_WORKER_METRICS_ENDPOINT", defaultWorkerMetricsEndpoint),
		PostgresDSN:           os.Getenv("FRUX_ACCEPTANCE_POSTGRES_DSN"),
		UserAccount:           strings.TrimSpace(os.Getenv("FRUX_ACCEPTANCE_USER_ACCOUNT")),
		UserPassword:          os.Getenv("FRUX_ACCEPTANCE_USER_PASSWORD"),
		AdminAccount:          strings.TrimSpace(os.Getenv("FRUX_ACCEPTANCE_ADMIN_ACCOUNT")),
		AdminPassword:         os.Getenv("FRUX_ACCEPTANCE_ADMIN_PASSWORD"),
		VideoFixturePath:      strings.TrimSpace(os.Getenv("FRUX_ACCEPTANCE_VIDEO_FIXTURE")),
		CoverFixturePath:      strings.TrimSpace(os.Getenv("FRUX_ACCEPTANCE_COVER_FIXTURE")),
		ExpectedProfile:       strings.TrimSpace(os.Getenv("FRUX_MULTIMODAL_PROFILE")),
		Query:                 query,
	}
	var err error
	if config.PollInterval, err = durationEnv("FRUX_ACCEPTANCE_POLL_INTERVAL", defaultPollInterval); err != nil {
		return applicationacceptance.Config{}, err
	}
	if config.StageTimeout, err = durationEnv("FRUX_ACCEPTANCE_STAGE_TIMEOUT", defaultStageTimeout); err != nil {
		return applicationacceptance.Config{}, err
	}
	if config.HTTPTimeout, err = durationEnv("FRUX_ACCEPTANCE_HTTP_TIMEOUT", defaultHTTPTimeout); err != nil {
		return applicationacceptance.Config{}, err
	}
	if timeoutOverride > 0 {
		config.StageTimeout = timeoutOverride
	}
	if config.MaxResponseBytes, err = int64Env("FRUX_ACCEPTANCE_MAX_RESPONSE_BYTES", defaultMaxResponseBytes); err != nil {
		return applicationacceptance.Config{}, err
	}
	if err := validateConfig(config); err != nil {
		return applicationacceptance.Config{}, err
	}
	return config, nil
}

func validateConfig(config applicationacceptance.Config) error {
	for field, value := range map[string]string{
		"FRUX_ACCEPTANCE_POSTGRES_DSN":   config.PostgresDSN,
		"FRUX_ACCEPTANCE_USER_ACCOUNT":   config.UserAccount,
		"FRUX_ACCEPTANCE_USER_PASSWORD":  config.UserPassword,
		"FRUX_ACCEPTANCE_ADMIN_ACCOUNT":  config.AdminAccount,
		"FRUX_ACCEPTANCE_ADMIN_PASSWORD": config.AdminPassword,
		"FRUX_ACCEPTANCE_VIDEO_FIXTURE":  config.VideoFixturePath,
		"FRUX_ACCEPTANCE_COVER_FIXTURE":  config.CoverFixturePath,
		"FRUX_MULTIMODAL_PROFILE":        config.ExpectedProfile,
	} {
		if strings.TrimSpace(value) == "" {
			return &ConfigError{Field: field}
		}
	}
	for field, endpoint := range map[string]string{
		"FRUX_ACCEPTANCE_API_ENDPOINT":            config.APIEndpoint,
		"FRUX_ACCEPTANCE_ADAPTER_ENDPOINT":        config.AdapterEndpoint,
		"FRUX_ACCEPTANCE_WORKER_METRICS_ENDPOINT": config.WorkerMetricsEndpoint,
	} {
		if err := validateEndpoint(endpoint); err != nil {
			return &ConfigError{Field: field}
		}
	}
	postgresURL, err := url.Parse(strings.TrimSpace(config.PostgresDSN))
	if err != nil || (postgresURL.Scheme != "postgres" && postgresURL.Scheme != "postgresql") ||
		postgresURL.Host == "" || strings.TrimSpace(postgresURL.Path) == "" {
		return &ConfigError{Field: "FRUX_ACCEPTANCE_POSTGRES_DSN"}
	}
	if config.PollInterval < 100*time.Millisecond || config.PollInterval > 10*time.Second {
		return &ConfigError{Field: "FRUX_ACCEPTANCE_POLL_INTERVAL"}
	}
	if config.StageTimeout < 5*time.Second || config.StageTimeout > 30*time.Minute {
		return &ConfigError{Field: "FRUX_ACCEPTANCE_STAGE_TIMEOUT"}
	}
	if config.HTTPTimeout < time.Second || config.HTTPTimeout > 2*time.Minute ||
		config.HTTPTimeout > config.StageTimeout {
		return &ConfigError{Field: "FRUX_ACCEPTANCE_HTTP_TIMEOUT"}
	}
	if config.MaxResponseBytes < 64<<10 || config.MaxResponseBytes > 8<<20 {
		return &ConfigError{Field: "FRUX_ACCEPTANCE_MAX_RESPONSE_BYTES"}
	}
	if len([]rune(config.Query)) == 0 || len([]rune(config.Query)) > 512 {
		return &ConfigError{Field: "FRUX_ACCEPTANCE_QUERY"}
	}
	if err := validateFixture(config.VideoFixturePath, maxVideoFixtureBytes, []string{".mp4", ".mov", ".webm"}); err != nil {
		return &ConfigError{Field: "FRUX_ACCEPTANCE_VIDEO_FIXTURE"}
	}
	if err := validateFixture(config.CoverFixturePath, maxCoverFixtureBytes, []string{".jpg", ".jpeg", ".png", ".webp"}); err != nil {
		return &ConfigError{Field: "FRUX_ACCEPTANCE_COVER_FIXTURE"}
	}
	return nil
}

func validateEndpoint(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return ErrInvalidAcceptanceConfig
	}
	if parsed.Scheme == "https" {
		return nil
	}
	if parsed.Scheme != "http" || !localHost(parsed.Hostname()) {
		return ErrInvalidAcceptanceConfig
	}
	return nil
}

func localHost(host string) bool {
	host = strings.TrimSpace(strings.ToLower(host))
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	if address, err := netip.ParseAddr(host); err == nil {
		return address.IsLoopback()
	}
	return false
}

func validateFixture(path string, maxBytes int64, extensions []string) error {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxBytes {
		return ErrInvalidAcceptanceConfig
	}
	extension := strings.ToLower(filepath.Ext(path))
	for _, allowed := range extensions {
		if extension == allowed {
			return nil
		}
	}
	return ErrInvalidAcceptanceConfig
}

func valueOrDefault(name, fallback string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	return value
}

func durationEnv(name string, fallback time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		return 0, &ConfigError{Field: name}
	}
	return value, nil
}

func int64Env(name string, fallback int64) (int64, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, &ConfigError{Field: name}
	}
	return value, nil
}
