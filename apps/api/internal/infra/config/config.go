package infraconfig

import (
	"errors"
	"os"
	"strings"
	"time"

	domainmedia "GCFeed/internal/domain/media"
	"github.com/goccy/go-yaml"
)

var ErrEmptyConfigPath = errors.New("config file path is empty")
var ErrReadConfigFailed = errors.New("read config file failed")
var ErrUnmarshalConfigFailed = errors.New("unmarshal config failed")
var ErrInvalidMediaConfig = errors.New("invalid media config")
var ErrInvalidPlaybackConfig = errors.New("invalid playback config")
var ErrInvalidInternalToken = errors.New("invalid internal token")

const minInternalTokenLength = 32

// LoadConfig 读取 YAML 配置文件，并反序列化为应用启动配置。
func LoadConfig(path string) (*Config, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, ErrEmptyConfigPath
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return nil, ErrReadConfigFailed
	}
	cfg := &Config{}
	if err := yaml.Unmarshal([]byte(os.ExpandEnv(string(content))), cfg); err != nil {
		return nil, ErrUnmarshalConfigFailed
	}
	if err := ValidateAPIConfig(cfg); err != nil {
		return nil, err
	}
	if err := normalizeAndValidateMediaConfig(&cfg.Media); err != nil {
		return nil, err
	}
	if err := normalizeAndValidatePlaybackConfig(&cfg.Playback); err != nil {
		return nil, err
	}

	return cfg, nil
}

// ValidateAPIConfig validates settings that protect API routes. Internal
// endpoints are opt-in so a worker-only deployment can leave their token unset.
func ValidateAPIConfig(cfg *Config) error {
	if cfg == nil {
		return ErrInvalidInternalToken
	}
	return normalizeAndValidateInternalConfig(&cfg.Internal)
}

func normalizeAndValidateInternalConfig(cfg *InternalConfig) error {
	if cfg == nil {
		return ErrInvalidInternalToken
	}
	cfg.Token = strings.TrimSpace(cfg.Token)
	if !cfg.Enabled {
		return nil
	}
	if strings.EqualFold(cfg.Token, "replace-with-internal-token") || !strongInternalToken(cfg.Token) {
		return ErrInvalidInternalToken
	}
	return nil
}

func strongInternalToken(token string) bool {
	if len(token) < minInternalTokenLength {
		return false
	}
	classes := 0
	var lower, upper, digit, other bool
	for _, character := range token {
		switch {
		case character >= 'a' && character <= 'z':
			lower = true
		case character >= 'A' && character <= 'Z':
			upper = true
		case character >= '0' && character <= '9':
			digit = true
		default:
			other = true
		}
	}
	for _, present := range []bool{lower, upper, digit, other} {
		if present {
			classes++
		}
	}
	return classes >= 3
}

func normalizeAndValidatePlaybackConfig(cfg *PlaybackConfig) error {
	cfg.Telemetry.Retention = defaultDuration(cfg.Telemetry.Retention, "168h")
	cfg.Telemetry.CleanupInterval = defaultDuration(cfg.Telemetry.CleanupInterval, "1h")
	if cfg.Telemetry.CleanupBatchSize <= 0 {
		cfg.Telemetry.CleanupBatchSize = 1000
	}
	if cfg.Telemetry.MaxBatchesPerMinute <= 0 {
		cfg.Telemetry.MaxBatchesPerMinute = 60
	}
	retention, err := time.ParseDuration(cfg.Telemetry.Retention)
	if err != nil || retention <= 0 {
		return ErrInvalidPlaybackConfig
	}
	cleanupInterval, err := time.ParseDuration(cfg.Telemetry.CleanupInterval)
	if err != nil || cleanupInterval <= 0 || cleanupInterval > retention {
		return ErrInvalidPlaybackConfig
	}
	if cfg.Telemetry.CleanupBatchSize > 10_000 || cfg.Telemetry.MaxBatchesPerMinute > 10_000 {
		return ErrInvalidPlaybackConfig
	}
	return nil
}

func normalizeAndValidateMediaConfig(cfg *MediaConfig) error {
	cfg.Backend = strings.ToLower(strings.TrimSpace(cfg.Backend))
	if cfg.Backend == "" {
		cfg.Backend = domainmedia.StorageBackendLocal
	}
	if !domainmedia.ValidStorageBackend(cfg.Backend) {
		return ErrInvalidMediaConfig
	}
	cfg.LocalRoot = strings.TrimSpace(cfg.LocalRoot)
	if cfg.LocalRoot == "" {
		cfg.LocalRoot = "./uploads"
	}
	cfg.PublicBaseURL = strings.TrimRight(strings.TrimSpace(cfg.PublicBaseURL), "/")
	if cfg.PublicBaseURL == "" && cfg.Backend == domainmedia.StorageBackendLocal {
		cfg.PublicBaseURL = "/media"
	}
	cfg.SignedURLTTL = defaultDuration(cfg.SignedURLTTL, "15m")
	cfg.UploadSessionTTL = defaultDuration(cfg.UploadSessionTTL, "15m")
	cfg.Processing.ProfileVersion = strings.TrimSpace(cfg.Processing.ProfileVersion)
	if cfg.Processing.ProfileVersion == "" {
		cfg.Processing.ProfileVersion = "v1"
	}
	if cfg.Processing.MaxAttempts <= 0 {
		cfg.Processing.MaxAttempts = 5
	}
	if cfg.Processing.WorkerConcurrency <= 0 {
		cfg.Processing.WorkerConcurrency = 1
	}
	cfg.Processing.LeaseTTL = defaultDuration(cfg.Processing.LeaseTTL, "10m")
	cfg.Processing.CleanupDelay = defaultDuration(cfg.Processing.CleanupDelay, "24h")
	for _, value := range []string{cfg.SignedURLTTL, cfg.UploadSessionTTL, cfg.Processing.LeaseTTL, cfg.Processing.CleanupDelay} {
		duration, err := time.ParseDuration(value)
		if err != nil || duration <= 0 {
			return ErrInvalidMediaConfig
		}
	}
	if cfg.Backend != domainmedia.StorageBackendS3 {
		return nil
	}
	cfg.S3.Endpoint = strings.TrimRight(strings.TrimSpace(cfg.S3.Endpoint), "/")
	cfg.S3.PresignEndpoint = strings.TrimRight(strings.TrimSpace(cfg.S3.PresignEndpoint), "/")
	if cfg.S3.PresignEndpoint == "" {
		cfg.S3.PresignEndpoint = cfg.S3.Endpoint
	}
	cfg.S3.Region = strings.TrimSpace(cfg.S3.Region)
	cfg.S3.Bucket = strings.TrimSpace(cfg.S3.Bucket)
	cfg.S3.AccessKey = strings.TrimSpace(cfg.S3.AccessKey)
	cfg.S3.SecretKey = strings.TrimSpace(cfg.S3.SecretKey)
	if cfg.S3.Region == "" || cfg.S3.Bucket == "" || cfg.PublicBaseURL == "" {
		return ErrInvalidMediaConfig
	}
	if (cfg.S3.AccessKey == "") != (cfg.S3.SecretKey == "") {
		return ErrInvalidMediaConfig
	}
	return nil
}

func defaultDuration(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}
