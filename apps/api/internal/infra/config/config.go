package infraconfig

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net"
	"net/netip"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/goccy/go-yaml"
	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
	domainreview "github.com/shiyudesu/frux/internal/domain/review"
)

var ErrEmptyConfigPath = errors.New("config file path is empty")
var ErrReadConfigFailed = errors.New("read config file failed")
var ErrUnmarshalConfigFailed = errors.New("unmarshal config failed")
var ErrInvalidMediaConfig = errors.New("invalid media config")
var ErrInvalidPlaybackConfig = errors.New("invalid playback config")
var ErrInvalidGovernanceConfig = errors.New("invalid governance config")
var ErrInvalidInternalToken = errors.New("invalid internal token")
var ErrInvalidRateLimitConfig = errors.New("invalid rate limit config")
var ErrInvalidKafkaConfig = errors.New("invalid kafka config")
var ErrInvalidJWTConfig = errors.New("invalid jwt config")
var ErrInvalidSecurityConfig = errors.New("invalid security config")
var ErrInvalidModerationConfig = errors.New("invalid moderation config")

const minInternalTokenLength = 32
const maxAdminAccessTTL = 8 * time.Hour
const maxConsumerAccessTTL = 15 * time.Minute
const maxJWTClockLeeway = 2 * time.Minute
const maxMediaProcessingDuration = 24 * time.Hour

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
	if err := normalizeAndValidateSecurityConfig(&cfg.Security, cfg.JWT.Secret); err != nil {
		return nil, err
	}
	if err := normalizeAndValidateMediaConfig(&cfg.Media); err != nil {
		return nil, err
	}
	if err := normalizeAndValidateModerationConfig(&cfg.Moderation); err != nil {
		return nil, err
	}
	if err := validateModerationMediaConfig(&cfg.Moderation, &cfg.Media); err != nil {
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
	if err := normalizeAndValidateKafkaConfig(&cfg.Kafka); err != nil {
		return nil, err
	}
	return cfg, nil
}

func normalizeAndValidateModerationConfig(cfg *ModerationConfig) error {
	if cfg == nil {
		return ErrInvalidModerationConfig
	}
	cfg.Mode = strings.ToLower(strings.TrimSpace(cfg.Mode))
	if cfg.Mode == "" {
		cfg.Mode = domainreview.ModerationModeDisabled
	}
	if !domainreview.ValidModerationMode(cfg.Mode) {
		return ErrInvalidModerationConfig
	}
	if cfg.ProviderConfigVersion == 0 {
		cfg.ProviderConfigVersion = 1
	}
	if cfg.ProviderConfigVersion < 1 || cfg.ProviderConfigVersion > 1_000_000 {
		return ErrInvalidModerationConfig
	}
	cfg.InputProfileVersion = defaultValue(cfg.InputProfileVersion, "frames-v1")
	if !domainreview.ValidModerationProfileVersion(cfg.InputProfileVersion) {
		return ErrInvalidModerationConfig
	}
	cfg.Endpoint = strings.TrimRight(strings.TrimSpace(cfg.Endpoint), "/")
	cfg.HMACSecret = strings.TrimSpace(cfg.HMACSecret)
	cfg.SamplePresignEndpoint = strings.TrimRight(
		strings.TrimSpace(cfg.SamplePresignEndpoint), "/",
	)
	cfg.Timeout = defaultDuration(cfg.Timeout, "10s")
	cfg.LeaseTTL = defaultDuration(cfg.LeaseTTL, "45s")
	cfg.PollInterval = defaultDuration(cfg.PollInterval, "1s")
	cfg.SampleURLTTL = defaultDuration(cfg.SampleURLTTL, "30s")
	cfg.SampleRetention = defaultDuration(cfg.SampleRetention, "1h")
	if cfg.WorkerConcurrency == 0 {
		cfg.WorkerConcurrency = 2
	}
	if cfg.MaxAttempts == 0 {
		cfg.MaxAttempts = 5
	}
	timeout, err := time.ParseDuration(cfg.Timeout)
	if err != nil || timeout < 500*time.Millisecond || timeout > 30*time.Second {
		return ErrInvalidModerationConfig
	}
	leaseTTL, err := time.ParseDuration(cfg.LeaseTTL)
	if err != nil || leaseTTL < timeout+time.Second || leaseTTL > 5*time.Minute {
		return ErrInvalidModerationConfig
	}
	pollInterval, err := time.ParseDuration(cfg.PollInterval)
	if err != nil || pollInterval < 100*time.Millisecond || pollInterval > time.Minute {
		return ErrInvalidModerationConfig
	}
	sampleURLTTL, err := time.ParseDuration(cfg.SampleURLTTL)
	if err != nil || sampleURLTTL < timeout || sampleURLTTL > 5*time.Minute {
		return ErrInvalidModerationConfig
	}
	sampleRetention, err := time.ParseDuration(cfg.SampleRetention)
	if err != nil || sampleRetention < time.Minute || sampleRetention > 24*time.Hour {
		return ErrInvalidModerationConfig
	}
	if cfg.WorkerConcurrency < 1 || cfg.WorkerConcurrency > 32 ||
		cfg.MaxAttempts < 1 || cfg.MaxAttempts > 10 {
		return ErrInvalidModerationConfig
	}
	if cfg.Mode == domainreview.ModerationModeDisabled &&
		cfg.Endpoint == "" && cfg.HMACSecret == "" {
		return nil
	}
	if cfg.Endpoint == "" || len(cfg.HMACSecret) < 32 || len(cfg.HMACSecret) > 512 {
		return ErrInvalidModerationConfig
	}
	endpoint, err := url.Parse(cfg.Endpoint)
	if err != nil || endpoint.Host == "" || endpoint.User != nil ||
		endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return ErrInvalidModerationConfig
	}
	if endpoint.Scheme == "https" {
		return nil
	}
	if endpoint.Scheme != "http" || !cfg.AllowInsecureLocal || !isLocalEndpoint(endpoint.Hostname()) {
		return ErrInvalidModerationConfig
	}
	return nil
}

func validateModerationMediaConfig(
	moderation *ModerationConfig,
	media *MediaConfig,
) error {
	if moderation == nil || media == nil {
		return ErrInvalidModerationConfig
	}
	if moderation.Mode == domainreview.ModerationModeDisabled {
		return nil
	}
	gateway, err := url.Parse(moderation.Endpoint)
	if err != nil {
		return ErrInvalidModerationConfig
	}
	gatewayLocal := isLocalEndpoint(gateway.Hostname())
	if media.Backend == domainmedia.StorageBackendLocal {
		if !gatewayLocal {
			return ErrInvalidModerationConfig
		}
		return nil
	}
	if media.Backend != domainmedia.StorageBackendS3 ||
		moderation.SamplePresignEndpoint == "" {
		return ErrInvalidModerationConfig
	}
	sampleEndpoint, err := url.Parse(moderation.SamplePresignEndpoint)
	if err != nil || sampleEndpoint.Host == "" || sampleEndpoint.User != nil ||
		sampleEndpoint.RawQuery != "" || sampleEndpoint.Fragment != "" {
		return ErrInvalidModerationConfig
	}
	sampleLocal := isLocalEndpoint(sampleEndpoint.Hostname())
	if sampleEndpoint.Scheme != "https" &&
		(sampleEndpoint.Scheme != "http" || !moderation.AllowInsecureLocal || !sampleLocal) {
		return ErrInvalidModerationConfig
	}
	if !gatewayLocal && sampleLocal {
		return ErrInvalidModerationConfig
	}
	return nil
}

func isLocalEndpoint(host string) bool {
	host = strings.TrimSpace(strings.ToLower(host))
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	address, err := netip.ParseAddr(host)
	return err == nil && address.IsLoopback()
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
	if err := normalizeAndValidateKafkaConfig(&cfg.Kafka); err != nil {
		return err
	}
	if !cfg.Kafka.Enabled || strings.TrimSpace(cfg.Redis.Addr) == "" {
		return ErrInvalidKafkaConfig
	}
	return nil
}

func normalizeAndValidateKafkaConfig(cfg *KafkaConfig) error {
	if cfg == nil {
		return ErrInvalidKafkaConfig
	}
	cfg.Environment = strings.ToLower(strings.TrimSpace(cfg.Environment))
	if cfg.Environment == "" {
		cfg.Environment = "local"
	}
	switch cfg.Environment {
	case "local", "test", "staging", "production":
	default:
		return ErrInvalidKafkaConfig
	}
	cfg.ClientID = defaultValue(cfg.ClientID, "frux")
	cfg.TopicPrefix = strings.TrimSpace(strings.TrimSuffix(cfg.TopicPrefix, "."))
	if !validKafkaName(cfg.ClientID, 128) ||
		(cfg.TopicPrefix != "" && !validKafkaTopicPrefix(cfg.TopicPrefix)) {
		return ErrInvalidKafkaConfig
	}
	if len(cfg.Brokers) > 16 {
		return ErrInvalidKafkaConfig
	}
	seenBrokers := make(map[string]struct{}, len(cfg.Brokers))
	for index, broker := range cfg.Brokers {
		broker = strings.TrimSpace(broker)
		if broker == "" || strings.ContainsAny(broker, "/?#") {
			return ErrInvalidKafkaConfig
		}
		host, port, err := net.SplitHostPort(broker)
		if err != nil || strings.TrimSpace(host) == "" || strings.TrimSpace(port) == "" {
			return ErrInvalidKafkaConfig
		}
		if _, duplicate := seenBrokers[broker]; duplicate {
			return ErrInvalidKafkaConfig
		}
		seenBrokers[broker] = struct{}{}
		cfg.Brokers[index] = broker
	}
	cfg.Authentication.Mechanism = strings.ToLower(strings.TrimSpace(cfg.Authentication.Mechanism))
	if cfg.Authentication.Mechanism == "" {
		cfg.Authentication.Mechanism = "none"
	}
	cfg.Authentication.Username = strings.TrimSpace(cfg.Authentication.Username)
	switch cfg.Authentication.Mechanism {
	case "none":
		if cfg.Authentication.Username != "" || cfg.Authentication.Password != "" {
			return ErrInvalidKafkaConfig
		}
	case "plain", "scram-sha-256", "scram-sha-512":
		if cfg.Authentication.Username == "" || cfg.Authentication.Password == "" ||
			len(cfg.Authentication.Username) > 256 || len(cfg.Authentication.Password) > 1024 {
			return ErrInvalidKafkaConfig
		}
	default:
		return ErrInvalidKafkaConfig
	}
	cfg.TLS.CAFile = strings.TrimSpace(cfg.TLS.CAFile)
	cfg.TLS.CertificateFile = strings.TrimSpace(cfg.TLS.CertificateFile)
	cfg.TLS.PrivateKeyFile = strings.TrimSpace(cfg.TLS.PrivateKeyFile)
	cfg.TLS.ServerName = strings.TrimSpace(cfg.TLS.ServerName)
	if cfg.TLS.InsecureSkipVerify && cfg.Environment != "local" && cfg.Environment != "test" {
		return ErrInvalidKafkaConfig
	}
	if (cfg.TLS.CertificateFile == "") != (cfg.TLS.PrivateKeyFile == "") ||
		(!cfg.TLS.Enabled && (cfg.TLS.CAFile != "" || cfg.TLS.CertificateFile != "" ||
			cfg.TLS.PrivateKeyFile != "" || cfg.TLS.ServerName != "" || cfg.TLS.InsecureSkipVerify)) {
		return ErrInvalidKafkaConfig
	}
	cfg.Timeouts.Dial = defaultDuration(cfg.Timeouts.Dial, "5s")
	cfg.Timeouts.Request = defaultDuration(cfg.Timeouts.Request, "10s")
	cfg.Timeouts.Produce = defaultDuration(cfg.Timeouts.Produce, "10s")
	cfg.Timeouts.Admin = defaultDuration(cfg.Timeouts.Admin, "10s")
	cfg.Timeouts.Shutdown = defaultDuration(cfg.Timeouts.Shutdown, "15s")
	for _, item := range []struct {
		value string
		min   time.Duration
		max   time.Duration
	}{
		{cfg.Timeouts.Dial, 100 * time.Millisecond, 30 * time.Second},
		{cfg.Timeouts.Request, time.Second, time.Minute},
		{cfg.Timeouts.Produce, time.Second, time.Minute},
		{cfg.Timeouts.Admin, time.Second, time.Minute},
		{cfg.Timeouts.Shutdown, time.Second, time.Minute},
	} {
		duration, err := time.ParseDuration(item.value)
		if err != nil || duration < item.min || duration > item.max {
			return ErrInvalidKafkaConfig
		}
	}
	if cfg.Consumer.MaxPollRecords == 0 {
		cfg.Consumer.MaxPollRecords = 100
	}
	if cfg.Consumer.MaxPollBytes == 0 {
		cfg.Consumer.MaxPollBytes = 8 << 20
	}
	if cfg.Consumer.PartitionConcurrency == 0 {
		cfg.Consumer.PartitionConcurrency = 8
	}
	cfg.Consumer.DrainTimeout = defaultDuration(cfg.Consumer.DrainTimeout, "10s")
	cfg.Consumer.AssignmentTimeout = defaultDuration(cfg.Consumer.AssignmentTimeout, "15s")
	drainTimeout, err := time.ParseDuration(cfg.Consumer.DrainTimeout)
	assignmentTimeout, assignmentErr := time.ParseDuration(cfg.Consumer.AssignmentTimeout)
	if err != nil || drainTimeout < time.Second || drainTimeout > time.Minute ||
		assignmentErr != nil || assignmentTimeout < time.Second || assignmentTimeout > time.Minute ||
		cfg.Consumer.MaxPollRecords < 1 || cfg.Consumer.MaxPollRecords > 1000 ||
		cfg.Consumer.MaxPollBytes < 1<<20 || cfg.Consumer.MaxPollBytes > 32<<20 ||
		cfg.Consumer.PartitionConcurrency < 1 || cfg.Consumer.PartitionConcurrency > 64 {
		return ErrInvalidKafkaConfig
	}
	if cfg.ProductionValidation.ReplicationFactor == 0 {
		if cfg.Environment == "local" || cfg.Environment == "test" {
			cfg.ProductionValidation.ReplicationFactor = 1
		} else {
			cfg.ProductionValidation.ReplicationFactor = 3
		}
	}
	if cfg.ProductionValidation.MinInSyncReplicas == 0 {
		if cfg.Environment == "local" || cfg.Environment == "test" {
			cfg.ProductionValidation.MinInSyncReplicas = 1
		} else {
			cfg.ProductionValidation.MinInSyncReplicas = 2
		}
	}
	if cfg.ProductionValidation.ReplicationFactor < 1 ||
		cfg.ProductionValidation.ReplicationFactor > 9 ||
		cfg.ProductionValidation.MinInSyncReplicas < 1 ||
		cfg.ProductionValidation.MinInSyncReplicas > cfg.ProductionValidation.ReplicationFactor {
		return ErrInvalidKafkaConfig
	}
	if !cfg.Enabled {
		return nil
	}
	if len(cfg.Brokers) == 0 {
		return ErrInvalidKafkaConfig
	}
	local := cfg.Environment == "local" || cfg.Environment == "test"
	if cfg.AllowLocalProvisioning && !local {
		return ErrInvalidKafkaConfig
	}
	if !local {
		if cfg.ProductionValidation.ReplicationFactor < 3 ||
			cfg.ProductionValidation.MinInSyncReplicas < 2 ||
			(cfg.ProductionValidation.RequireTLS && !cfg.TLS.Enabled) ||
			(cfg.ProductionValidation.RequireAuthentication &&
				cfg.Authentication.Mechanism == "none") {
			return ErrInvalidKafkaConfig
		}
	}
	if cfg.Environment == "production" &&
		(!cfg.ProductionValidation.RequireTLS ||
			!cfg.ProductionValidation.RequireAuthentication) {
		return ErrInvalidKafkaConfig
	}
	return nil
}

func validKafkaName(value string, max int) bool {
	if value == "" || len(value) > max {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}

func validKafkaTopicPrefix(value string) bool {
	if value == "" || len(value) > 64 || !lowerAlphaNumeric(value[0]) {
		return false
	}
	last := value[len(value)-1]
	if !lowerAlphaNumeric(last) {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '-' {
			continue
		}
		return false
	}
	return true
}

func lowerAlphaNumeric(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= '0' && value <= '9'
}

func normalizeAndValidateJWTConfig(cfg *JWTConfig) error {
	if cfg == nil {
		return ErrInvalidJWTConfig
	}

	cfg.Secret = strings.TrimSpace(cfg.Secret)
	cfg.Issuer = defaultValue(cfg.Issuer, "frux")
	cfg.AccessTTL = defaultDuration(cfg.AccessTTL, "5m")
	cfg.AdminAccessTTL = defaultDuration(cfg.AdminAccessTTL, "30m")
	cfg.ClockLeeway = defaultDuration(cfg.ClockLeeway, "30s")
	cfg.LegacySecret = strings.TrimSpace(cfg.LegacySecret)
	cfg.LegacyIssuedUntil = strings.TrimSpace(cfg.LegacyIssuedUntil)
	cfg.LegacyAcceptUntil = strings.TrimSpace(cfg.LegacyAcceptUntil)
	accessTTL, err := time.ParseDuration(cfg.AccessTTL)
	if err != nil || accessTTL <= 0 || accessTTL > maxConsumerAccessTTL {
		return ErrInvalidJWTConfig
	}
	adminTTL, err := time.ParseDuration(cfg.AdminAccessTTL)
	if err != nil || adminTTL < 5*time.Minute || adminTTL > maxAdminAccessTTL {
		return ErrInvalidJWTConfig
	}
	clockLeeway, err := time.ParseDuration(cfg.ClockLeeway)
	if err != nil || clockLeeway < 0 || clockLeeway > maxJWTClockLeeway {
		return ErrInvalidJWTConfig
	}
	if len(cfg.Consumer.Keys) == 0 || len(cfg.Admin.Keys) == 0 {
		if cfg.Secret == "" {
			return ErrInvalidJWTConfig
		}
		cfg.Consumer = JWTKeyRingConfig{
			ActiveKeyID: "consumer-v1",
			Keys: []JWTKeyConfig{{
				ID: "consumer-v1", Secret: deriveJWTSecret(cfg.Secret, "consumer"),
			}},
		}
		cfg.Admin = JWTKeyRingConfig{
			ActiveKeyID: "admin-v1",
			Keys: []JWTKeyConfig{{
				ID: "admin-v1", Secret: deriveJWTSecret(cfg.Secret, "admin"),
			}},
		}
	}
	if err := validateJWTKeyRing(cfg.Consumer); err != nil {
		return err
	}
	if err := validateJWTKeyRing(cfg.Admin); err != nil {
		return err
	}
	if cfg.Consumer.ActiveKeyID == cfg.Admin.ActiveKeyID {
		return ErrInvalidJWTConfig
	}
	if cfg.LegacySecret != "" {
		issuedUntil, issuedErr := time.Parse(
			time.RFC3339, cfg.LegacyIssuedUntil,
		)
		deadline, err := time.Parse(time.RFC3339, cfg.LegacyAcceptUntil)
		if issuedErr != nil || err != nil ||
			deadline.Before(
				issuedUntil.Add(maxDuration(accessTTL, adminTTL)+clockLeeway),
			) {
			return ErrInvalidJWTConfig
		}
	} else if cfg.LegacyIssuedUntil != "" || cfg.LegacyAcceptUntil != "" {
		return ErrInvalidJWTConfig
	}
	return nil
}

func normalizeAndValidateSecurityConfig(
	cfg *SecurityConfig,
	legacyJWTSecret string,
) error {
	if cfg == nil {
		return ErrInvalidSecurityConfig
	}
	cfg.HMACSecret = strings.TrimSpace(cfg.HMACSecret)
	if cfg.HMACSecret == "" && strings.TrimSpace(legacyJWTSecret) != "" {
		cfg.HMACSecret = deriveJWTSecret(legacyJWTSecret, "application-hmac")
	}
	if len(cfg.HMACSecret) < 32 {
		return ErrInvalidSecurityConfig
	}
	return nil
}

func validateJWTKeyRing(ring JWTKeyRingConfig) error {
	ring.ActiveKeyID = strings.TrimSpace(ring.ActiveKeyID)
	if ring.ActiveKeyID == "" || len(ring.Keys) == 0 {
		return ErrInvalidJWTConfig
	}
	seen := make(map[string]struct{}, len(ring.Keys))
	activeFound := false
	for _, key := range ring.Keys {
		id := strings.TrimSpace(key.ID)
		secret := strings.TrimSpace(key.Secret)
		if id == "" || len(secret) < 16 {
			return ErrInvalidJWTConfig
		}
		if _, exists := seen[id]; exists {
			return ErrInvalidJWTConfig
		}
		seen[id] = struct{}{}
		if id == ring.ActiveKeyID {
			activeFound = true
		}
	}
	if !activeFound {
		return ErrInvalidJWTConfig
	}
	return nil
}

func deriveJWTSecret(secret, purpose string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(purpose) + ":" + strings.TrimSpace(secret)))
	return hex.EncodeToString(sum[:])
}

func maxDuration(left, right time.Duration) time.Duration {
	if left > right {
		return left
	}
	return right
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
		if character < 32 || character > 126 {
			return false
		}
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
		cfg.Processing.ProfileVersion = "v2"
	}
	if cfg.Processing.MaxAttempts <= 0 {
		cfg.Processing.MaxAttempts = 5
	}
	if cfg.Processing.WorkerConcurrency <= 0 {
		cfg.Processing.WorkerConcurrency = 1
	}
	cfg.Processing.LeaseTTL = defaultDuration(cfg.Processing.LeaseTTL, "10m")
	cfg.Processing.MaxDuration = defaultDuration(cfg.Processing.MaxDuration, "180m")
	cfg.Processing.CommandTimeout = defaultDuration(cfg.Processing.CommandTimeout, "360m")
	cfg.Processing.FFmpegPreset = strings.ToLower(strings.TrimSpace(cfg.Processing.FFmpegPreset))
	if cfg.Processing.FFmpegPreset == "" {
		cfg.Processing.FFmpegPreset = "veryfast"
	}
	cfg.Processing.CleanupDelay = defaultDuration(cfg.Processing.CleanupDelay, "24h")
	for _, value := range []string{
		cfg.SignedURLTTL,
		cfg.UploadSessionTTL,
		cfg.Processing.LeaseTTL,
		cfg.Processing.MaxDuration,
		cfg.Processing.CommandTimeout,
		cfg.Processing.CleanupDelay,
	} {
		duration, err := time.ParseDuration(value)
		if err != nil || duration <= 0 {
			return ErrInvalidMediaConfig
		}
	}
	maxDuration, _ := time.ParseDuration(cfg.Processing.MaxDuration)
	commandTimeout, _ := time.ParseDuration(cfg.Processing.CommandTimeout)
	if maxDuration > maxMediaProcessingDuration ||
		commandTimeout < maxDuration ||
		commandTimeout > maxMediaProcessingDuration ||
		!validFFmpegPreset(cfg.Processing.FFmpegPreset) {
		return ErrInvalidMediaConfig
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

func validFFmpegPreset(value string) bool {
	switch value {
	case "ultrafast", "superfast", "veryfast", "faster", "fast", "medium", "slow":
		return true
	default:
		return false
	}
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
