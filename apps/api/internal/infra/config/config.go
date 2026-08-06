package infraconfig

import (
	"errors"
	"net/netip"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/goccy/go-yaml"
	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
)

var ErrEmptyConfigPath = errors.New("config file path is empty")
var ErrReadConfigFailed = errors.New("read config file failed")
var ErrUnmarshalConfigFailed = errors.New("unmarshal config failed")
var ErrInvalidMediaConfig = errors.New("invalid media config")
var ErrInvalidPlaybackConfig = errors.New("invalid playback config")
var ErrInvalidGovernanceConfig = errors.New("invalid governance config")
var ErrInvalidInternalToken = errors.New("invalid internal token")
var ErrInvalidRateLimitConfig = errors.New("invalid rate limit config")
var ErrInvalidRabbitMQConfig = errors.New("invalid rabbitmq config")
var ErrInvalidJWTConfig = errors.New("invalid jwt config")

const minInternalTokenLength = 32
const maxAdminAccessTTL = 8 * time.Hour

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
	if err := normalizeAndValidateJWTConfig(&cfg.JWT); err != nil {
		return nil, err
	}
	if err := normalizeAndValidateMediaConfig(&cfg.Media); err != nil {
		return nil, err
	}
	if err := normalizeAndValidatePlaybackConfig(&cfg.Playback); err != nil {
		return nil, err
	}
	if err := normalizeAndValidateGovernanceConfig(&cfg.Governance); err != nil {
		return nil, err
	}
	if err := normalizeAndValidateRateLimitConfig(&cfg.RateLimit); err != nil {
		return nil, err
	}

	return cfg, nil
}

func normalizeAndValidateGovernanceConfig(cfg *GovernanceConfig) error {
	if cfg == nil {
		return ErrInvalidGovernanceConfig
	}
	cfg.PollInterval = defaultDuration(cfg.PollInterval, "5s")
	cfg.PollTimeout = defaultDuration(cfg.PollTimeout, "2s")
	interval, err := time.ParseDuration(cfg.PollInterval)
	if err != nil || interval < time.Second || interval > time.Minute {
		return ErrInvalidGovernanceConfig
	}
	timeout, err := time.ParseDuration(cfg.PollTimeout)
	if err != nil || timeout < 100*time.Millisecond || timeout > interval {
		return ErrInvalidGovernanceConfig
	}
	return nil
}

// ValidateAPIConfig validates settings that protect API routes. Internal
// endpoints are opt-in so a worker-only deployment can leave their token unset.
func ValidateAPIConfig(cfg *Config) error {
	if cfg == nil {
		return ErrInvalidInternalToken
	}
	if err := normalizeAndValidateInternalConfig(&cfg.Internal); err != nil {
		return err
	}
	if err := normalizeAndValidateRateLimitConfig(&cfg.RateLimit); err != nil {
		return err
	}
	return normalizeAndValidateRabbitMQConfig(&cfg.RabbitMQ)
}

func normalizeAndValidateJWTConfig(cfg *JWTConfig) error {
	if cfg == nil {
		return ErrInvalidJWTConfig
	}
	cfg.Secret = strings.TrimSpace(cfg.Secret)
	cfg.AccessTTL = defaultDuration(cfg.AccessTTL, "15m")
	cfg.AdminAccessTTL = defaultDuration(cfg.AdminAccessTTL, "30m")
	if cfg.Secret == "" {
		return ErrInvalidJWTConfig
	}
	accessTTL, err := time.ParseDuration(cfg.AccessTTL)
	if err != nil || accessTTL <= 0 {
		return ErrInvalidJWTConfig
	}
	adminTTL, err := time.ParseDuration(cfg.AdminAccessTTL)
	if err != nil || adminTTL < 5*time.Minute || adminTTL > maxAdminAccessTTL {
		return ErrInvalidJWTConfig
	}
	return nil
}

func normalizeAndValidateRabbitMQConfig(cfg *RabbitMQConfig) error {
	if cfg == nil {
		return ErrInvalidRabbitMQConfig
	}
	cfg.URL = strings.TrimSpace(cfg.URL)
	if cfg.URL == "" {
		return nil
	}
	parsed, err := url.Parse(cfg.URL)
	if err != nil || (parsed.Scheme != "amqp" && parsed.Scheme != "amqps") || parsed.Host == "" {
		return ErrInvalidRabbitMQConfig
	}
	cfg.ManagementURL = strings.TrimRight(strings.TrimSpace(cfg.ManagementURL), "/")
	cfg.ManagementUsername = strings.TrimSpace(cfg.ManagementUsername)
	cfg.ManagementPassword = strings.TrimSpace(cfg.ManagementPassword)
	cfg.ManagementTimeout = defaultDuration(cfg.ManagementTimeout, "2s")
	if _, err := time.ParseDuration(cfg.ManagementTimeout); err != nil {
		return ErrInvalidRabbitMQConfig
	}
	dead := &cfg.DeadLetter
	dead.VersionSuffix = defaultValue(dead.VersionSuffix, ".q2")
	dead.ExchangeSuffix = defaultValue(dead.ExchangeSuffix, ".dlx.q2")
	dead.QueueSuffix = defaultValue(dead.QueueSuffix, ".dlq.q2")
	dead.ReplayTimeout = defaultDuration(dead.ReplayTimeout, "5s")
	if dead.DeliveryLimit == 0 {
		dead.DeliveryLimit = 5
	}
	if dead.SourceMaxLength == 0 {
		dead.SourceMaxLength = 100_000
	}
	if dead.DeadLetterMaxLength == 0 {
		dead.DeadLetterMaxLength = 10_000
	}
	if dead.PreviewLimit == 0 {
		dead.PreviewLimit = 20
	}
	if !dead.Enabled {
		return nil
	}
	if dead.DeliveryLimit < 1 || dead.DeliveryLimit > 100 ||
		dead.SourceMaxLength < 1 || dead.DeadLetterMaxLength < 1 ||
		dead.PreviewLimit < 1 || dead.PreviewLimit > 100 {
		return ErrInvalidRabbitMQConfig
	}
	if _, err := time.ParseDuration(dead.ReplayTimeout); err != nil {
		return ErrInvalidRabbitMQConfig
	}
	if cfg.ManagementURL != "" {
		management, err := url.Parse(cfg.ManagementURL)
		if err != nil || (management.Scheme != "http" && management.Scheme != "https") ||
			management.Host == "" || cfg.ManagementUsername == "" || cfg.ManagementPassword == "" {
			return ErrInvalidRabbitMQConfig
		}
	}
	for _, mode := range []string{
		dead.ActionChangedMode, dead.VideoPublishedMode, dead.VideoEmbeddingMode,
		dead.ViewEventRecordedMode, dead.MediaProcessingMode,
	} {
		switch strings.ToLower(strings.TrimSpace(mode)) {
		case "", "legacy", "dual", "new":
		default:
			return ErrInvalidRabbitMQConfig
		}
	}
	return nil
}

func normalizeAndValidateRateLimitConfig(cfg *RateLimitConfig) error {
	if cfg == nil {
		return ErrInvalidRateLimitConfig
	}
	if cfg.MaxEntries == 0 {
		cfg.MaxEntries = 10_000
	}
	cfg.IdleTTL = defaultDuration(cfg.IdleTTL, "10m")
	cfg.RedisTimeout = defaultDuration(cfg.RedisTimeout, "75ms")
	if cfg.MaxEntries < 100 || cfg.MaxEntries > 1_000_000 {
		return ErrInvalidRateLimitConfig
	}
	idleTTL, err := time.ParseDuration(cfg.IdleTTL)
	if err != nil || idleTTL < time.Minute || idleTTL > 24*time.Hour {
		return ErrInvalidRateLimitConfig
	}
	timeout, err := time.ParseDuration(cfg.RedisTimeout)
	if err != nil || timeout < 10*time.Millisecond || timeout > 500*time.Millisecond {
		return ErrInvalidRateLimitConfig
	}
	normalized := make([]string, 0, len(cfg.TrustedProxies))
	seen := make(map[string]struct{}, len(cfg.TrustedProxies))
	for _, raw := range cfg.TrustedProxies {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(raw))
		if err != nil {
			return ErrInvalidRateLimitConfig
		}
		value := prefix.Masked().String()
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		normalized = append(normalized, value)
	}
	cfg.TrustedProxies = normalized
	return nil
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

func defaultValue(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}
