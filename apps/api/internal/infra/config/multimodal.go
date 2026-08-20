package infraconfig

import (
	"encoding/base64"
	"errors"
	"net/url"
	"slices"
	"strings"
	"time"

	domainembedding "github.com/shiyudesu/frux/internal/domain/embedding"
)

var ErrInvalidMultimodalConfig = errors.New("invalid multimodal config")
var ErrMissingMultimodalDependency = errors.New("missing multimodal runtime dependency")

const (
	MultimodalProviderProtocolV1      = "frux-multimodal-v1"
	defaultMultimodalProviderDeadline = "15s"
	defaultMultimodalStartupTimeout   = "3s"
	defaultMultimodalExactDeadline    = "500ms"
	defaultMultimodalQueryCacheTTL    = "10m"
	defaultMultimodalMaxRequestBytes  = 24 << 20
	defaultMultimodalMaxResponseBytes = 2 << 20
	maxMultimodalRequestBytes         = 96 << 20
	maxMultimodalResponseBytes        = 8 << 20
	maxMultimodalImageBytes           = 20 * 1024 * 1024
	maxMultimodalTotalImageBytes      = 64 * 1024 * 1024
)

type MultimodalRuntimeDependencies struct {
	ProviderContract *domainembedding.MultimodalContractIdentity
	QueryCache       bool
	ExactRetrieval   bool
}

func normalizeAndValidateMultimodalConfig(cfg *MultimodalConfig) error {
	if cfg == nil {
		return ErrInvalidMultimodalConfig
	}
	contractFields := multimodalContractFieldCount(cfg.Contract)
	contractConfigured := contractFields == 8
	if contractFields > 0 && !contractConfigured {
		return ErrInvalidMultimodalConfig
	}
	if cfg.Enabled && !contractConfigured {
		return ErrInvalidMultimodalConfig
	}
	if !cfg.Enabled && (cfg.VideoJobsEnabled || cfg.QueryEmbeddingEnabled ||
		cfg.HybridSearchEnabled || cfg.SimilarVideosEnabled) {
		return ErrInvalidMultimodalConfig
	}
	if cfg.Enabled && !cfg.VideoJobsEnabled && !cfg.QueryEmbeddingEnabled &&
		!cfg.HybridSearchEnabled && !cfg.SimilarVideosEnabled {
		return ErrInvalidMultimodalConfig
	}
	if cfg.HybridSearchEnabled && !cfg.QueryEmbeddingEnabled {
		return ErrInvalidMultimodalConfig
	}
	if cfg.HybridSearchEnabled || cfg.SimilarVideosEnabled {
		if !cfg.Enabled {
			return ErrInvalidMultimodalConfig
		}
	}
	if cfg.MaxVideoTextRunes == 0 {
		cfg.MaxVideoTextRunes = 2048
	}
	if cfg.MaxVideoTextRunes < 1 || cfg.MaxVideoTextRunes > 8192 {
		return ErrInvalidMultimodalConfig
	}

	if contractConfigured {
		identity, err := domainembedding.NewMultimodalContractIdentity(
			cfg.Contract.ProviderAlias,
			cfg.Contract.ModelAlias,
			cfg.Contract.RevisionAlias,
			cfg.Contract.Dimension,
			cfg.Contract.TextCanonicalizer,
			cfg.Contract.FrameSamplingPolicy,
			cfg.Contract.ImagePreprocessingPolicy,
			cfg.Contract.FusionPolicy,
		)
		if err != nil ||
			identity.TextCanonicalizer != domainembedding.MultimodalTextCanonicalizerV1 ||
			identity.FrameSamplingPolicy != domainembedding.MultimodalFrameSamplingPolicyV1 ||
			identity.ImagePreprocessingPolicy != domainembedding.MultimodalImagePreprocessingV1 ||
			identity.FusionPolicy != domainembedding.MultimodalFusionPolicyV1 {
			return ErrInvalidMultimodalConfig
		}
		cfg.Contract = MultimodalContractConfig{
			ProviderAlias: identity.ProviderAlias, ModelAlias: identity.ModelAlias,
			RevisionAlias: identity.RevisionAlias, Dimension: identity.Dimension,
			TextCanonicalizer:        identity.TextCanonicalizer,
			FrameSamplingPolicy:      identity.FrameSamplingPolicy,
			ImagePreprocessingPolicy: identity.ImagePreprocessingPolicy,
			FusionPolicy:             identity.FusionPolicy,
		}
	}

	providerFieldsConfigured := strings.TrimSpace(cfg.Provider.Endpoint) != "" ||
		strings.TrimSpace(cfg.Provider.HMACSecret) != ""
	providerRequired := cfg.VideoJobsEnabled || cfg.QueryEmbeddingEnabled || cfg.HybridSearchEnabled
	cfg.Provider.Endpoint = strings.TrimRight(strings.TrimSpace(cfg.Provider.Endpoint), "/")
	cfg.Provider.HMACSecret = strings.TrimSpace(cfg.Provider.HMACSecret)
	cfg.Provider.ProtocolVersion = strings.ToLower(strings.TrimSpace(cfg.Provider.ProtocolVersion))
	if cfg.Provider.ProtocolVersion == "" {
		cfg.Provider.ProtocolVersion = MultimodalProviderProtocolV1
	}
	cfg.Provider.StartupTimeout = defaultDuration(cfg.Provider.StartupTimeout, defaultMultimodalStartupTimeout)
	cfg.Provider.Deadline = defaultDuration(cfg.Provider.Deadline, defaultMultimodalProviderDeadline)
	if cfg.Provider.AdmissionLimit == 0 {
		cfg.Provider.AdmissionLimit = 2
	}
	if cfg.Provider.MaxRequestBytes == 0 {
		cfg.Provider.MaxRequestBytes = defaultMultimodalMaxRequestBytes
	}
	if cfg.Provider.MaxResponseBytes == 0 {
		cfg.Provider.MaxResponseBytes = defaultMultimodalMaxResponseBytes
	}
	startupTimeout, err := time.ParseDuration(cfg.Provider.StartupTimeout)
	if err != nil || startupTimeout < 100*time.Millisecond || startupTimeout > 30*time.Second {
		return ErrInvalidMultimodalConfig
	}
	providerDeadline, err := time.ParseDuration(cfg.Provider.Deadline)
	if err != nil || providerDeadline < 100*time.Millisecond || providerDeadline > 2*time.Minute ||
		cfg.Provider.AdmissionLimit < 1 || cfg.Provider.AdmissionLimit > 64 ||
		cfg.Provider.ProtocolVersion != MultimodalProviderProtocolV1 ||
		cfg.Provider.MaxRequestBytes < 1<<20 || cfg.Provider.MaxRequestBytes > maxMultimodalRequestBytes ||
		cfg.Provider.MaxResponseBytes < 64<<10 || cfg.Provider.MaxResponseBytes > maxMultimodalResponseBytes {
		return ErrInvalidMultimodalConfig
	}
	if providerRequired || providerFieldsConfigured {
		if cfg.Provider.Endpoint == "" || len(cfg.Provider.HMACSecret) < 32 || len(cfg.Provider.HMACSecret) > 512 {
			return ErrInvalidMultimodalConfig
		}
		endpoint, parseErr := url.Parse(cfg.Provider.Endpoint)
		if parseErr != nil || endpoint.Host == "" || endpoint.User != nil ||
			endpoint.RawQuery != "" || endpoint.Fragment != "" {
			return ErrInvalidMultimodalConfig
		}
		if endpoint.Scheme != "https" &&
			(endpoint.Scheme != "http" || !cfg.Provider.AllowInsecureLocal || !isLocalEndpoint(endpoint.Hostname())) {
			return ErrInvalidMultimodalConfig
		}
	}
	if cfg.Jobs.MaxAttempts == 0 {
		cfg.Jobs.MaxAttempts = 5
	}
	cfg.Jobs.LeaseTTL = defaultDuration(cfg.Jobs.LeaseTTL, "45s")
	cfg.Jobs.HeartbeatInterval = defaultDuration(cfg.Jobs.HeartbeatInterval, "10s")
	cfg.Jobs.PollInterval = defaultDuration(cfg.Jobs.PollInterval, "1s")
	cfg.Jobs.RetryBase = defaultDuration(cfg.Jobs.RetryBase, "5s")
	cfg.Jobs.RetryMax = defaultDuration(cfg.Jobs.RetryMax, "15m")
	cfg.Jobs.ShutdownTimeout = defaultDuration(cfg.Jobs.ShutdownTimeout, "15s")
	cfg.Jobs.TerminalRetention = defaultDuration(cfg.Jobs.TerminalRetention, "720h")
	leaseTTL, leaseErr := time.ParseDuration(cfg.Jobs.LeaseTTL)
	heartbeatInterval, heartbeatErr := time.ParseDuration(cfg.Jobs.HeartbeatInterval)
	pollInterval, pollErr := time.ParseDuration(cfg.Jobs.PollInterval)
	retryBase, retryBaseErr := time.ParseDuration(cfg.Jobs.RetryBase)
	retryMax, retryMaxErr := time.ParseDuration(cfg.Jobs.RetryMax)
	shutdownTimeout, shutdownErr := time.ParseDuration(cfg.Jobs.ShutdownTimeout)
	terminalRetention, retentionErr := time.ParseDuration(cfg.Jobs.TerminalRetention)
	if leaseErr != nil || heartbeatErr != nil || pollErr != nil || retryBaseErr != nil ||
		retryMaxErr != nil || shutdownErr != nil || retentionErr != nil ||
		cfg.Jobs.MaxAttempts < 1 || cfg.Jobs.MaxAttempts > 10 ||
		leaseTTL < providerDeadline+time.Second || leaseTTL > 10*time.Minute ||
		heartbeatInterval < time.Second || heartbeatInterval*2 >= leaseTTL ||
		pollInterval < 100*time.Millisecond || pollInterval > time.Minute ||
		retryBase < time.Second || retryMax < retryBase || retryMax > 24*time.Hour ||
		shutdownTimeout < time.Second || shutdownTimeout > time.Minute ||
		terminalRetention < time.Hour || terminalRetention > 90*24*time.Hour {
		return ErrInvalidMultimodalConfig
	}

	if cfg.Images.MaxCount == 0 {
		cfg.Images.MaxCount = 4
	}
	if cfg.Images.MaxBytesEach == 0 {
		cfg.Images.MaxBytesEach = 4 * 1024 * 1024
	}
	if cfg.Images.MaxTotalBytes == 0 {
		cfg.Images.MaxTotalBytes = 12 * 1024 * 1024
	}
	if cfg.Images.MaxPixelsEach == 0 {
		cfg.Images.MaxPixelsEach = 4_000_000
	}
	if len(cfg.Images.AllowedMIMETypes) == 0 {
		cfg.Images.AllowedMIMETypes = []string{"image/jpeg", "image/png", "image/webp"}
	}
	if cfg.Images.MaxCount < 1 || cfg.Images.MaxCount > 16 ||
		cfg.Images.MaxBytesEach < 64*1024 || cfg.Images.MaxBytesEach > maxMultimodalImageBytes ||
		cfg.Images.MaxTotalBytes < cfg.Images.MaxBytesEach || cfg.Images.MaxTotalBytes > maxMultimodalTotalImageBytes ||
		cfg.Images.MaxPixelsEach < 10_000 || cfg.Images.MaxPixelsEach > 16_000_000 {
		return ErrInvalidMultimodalConfig
	}
	minimumEncodedRequestBytes := int64(base64.StdEncoding.EncodedLen(cfg.Images.MaxTotalBytes)) + 64<<10
	if cfg.Provider.MaxRequestBytes < minimumEncodedRequestBytes {
		return ErrInvalidMultimodalConfig
	}
	allowedMIMETypes := make([]string, 0, len(cfg.Images.AllowedMIMETypes))
	for _, mimeType := range cfg.Images.AllowedMIMETypes {
		mimeType = strings.ToLower(strings.TrimSpace(mimeType))
		if !slices.Contains([]string{"image/jpeg", "image/png", "image/webp"}, mimeType) ||
			slices.Contains(allowedMIMETypes, mimeType) {
			return ErrInvalidMultimodalConfig
		}
		allowedMIMETypes = append(allowedMIMETypes, mimeType)
	}
	slices.Sort(allowedMIMETypes)
	if !slices.Contains(allowedMIMETypes, "image/jpeg") {
		return ErrInvalidMultimodalConfig
	}
	cfg.Images.AllowedMIMETypes = allowedMIMETypes

	if cfg.Query.MaxRunes == 0 {
		cfg.Query.MaxRunes = 128
	}
	cfg.Query.CacheTTL = defaultDuration(cfg.Query.CacheTTL, defaultMultimodalQueryCacheTTL)
	if cfg.Query.CacheEntries == 0 {
		cfg.Query.CacheEntries = 1000
	}
	queryCacheTTL, err := time.ParseDuration(cfg.Query.CacheTTL)
	if err != nil || cfg.Query.MaxRunes < 1 || cfg.Query.MaxRunes > 512 ||
		queryCacheTTL < time.Second || queryCacheTTL > 24*time.Hour ||
		cfg.Query.CacheEntries < 1 || cfg.Query.CacheEntries > 100_000 {
		return ErrInvalidMultimodalConfig
	}

	if cfg.Exact.MaxLimit == 0 {
		cfg.Exact.MaxLimit = 100
	}
	cfg.Exact.Deadline = defaultDuration(cfg.Exact.Deadline, defaultMultimodalExactDeadline)
	exactDeadline, err := time.ParseDuration(cfg.Exact.Deadline)
	if err != nil || cfg.Exact.MaxLimit < 1 || cfg.Exact.MaxLimit > 500 ||
		exactDeadline < 10*time.Millisecond || exactDeadline > 10*time.Second {
		return ErrInvalidMultimodalConfig
	}

	cfg.Hybrid.Version = strings.ToLower(strings.TrimSpace(cfg.Hybrid.Version))
	if cfg.Hybrid.Version == "" {
		cfg.Hybrid.Version = domainembedding.MultimodalHybridMergeVersionV1
	}
	cfg.Hybrid.FallbackMode = strings.ToLower(strings.TrimSpace(cfg.Hybrid.FallbackMode))
	if cfg.Hybrid.FallbackMode == "" {
		cfg.Hybrid.FallbackMode = domainembedding.MultimodalLexicalFallback
	}
	if cfg.Hybrid.PoolLimit == 0 {
		cfg.Hybrid.PoolLimit = 100
	}
	if cfg.Hybrid.LexicalReservation == 0 {
		cfg.Hybrid.LexicalReservation = 20
	}
	if cfg.Hybrid.SemanticReservation == 0 {
		cfg.Hybrid.SemanticReservation = 20
	}
	cfg.Hybrid.CursorTTL = defaultDuration(cfg.Hybrid.CursorTTL, "15m")
	cursorTTL, cursorErr := time.ParseDuration(cfg.Hybrid.CursorTTL)
	if cfg.Hybrid.Version != domainembedding.MultimodalHybridMergeVersionV1 ||
		cfg.Hybrid.FallbackMode != domainembedding.MultimodalLexicalFallback ||
		cfg.Hybrid.PoolLimit < 51 || cfg.Hybrid.PoolLimit > 500 ||
		cfg.Hybrid.PoolLimit > cfg.Exact.MaxLimit ||
		cfg.Hybrid.LexicalReservation < 0 || cfg.Hybrid.SemanticReservation < 0 ||
		cfg.Hybrid.LexicalReservation+cfg.Hybrid.SemanticReservation > cfg.Hybrid.PoolLimit ||
		cursorErr != nil || cursorTTL < time.Minute || cursorTTL > 24*time.Hour {
		return ErrInvalidMultimodalConfig
	}
	return nil
}

func ValidateMultimodalRuntime(cfg MultimodalConfig, dependencies MultimodalRuntimeDependencies) error {
	if !cfg.Enabled {
		return nil
	}
	expected, err := cfg.Contract.Identity()
	if err != nil || dependencies.ProviderContract == nil ||
		!expected.Equal(*dependencies.ProviderContract) {
		return ErrMissingMultimodalDependency
	}
	if (cfg.QueryEmbeddingEnabled || cfg.HybridSearchEnabled) && !dependencies.QueryCache {
		return ErrMissingMultimodalDependency
	}
	if (cfg.HybridSearchEnabled || cfg.SimilarVideosEnabled) && !dependencies.ExactRetrieval {
		return ErrMissingMultimodalDependency
	}
	return nil
}

func ValidateMultimodalWorkerRuntime(
	cfg MultimodalConfig,
	providerContract *domainembedding.MultimodalContractIdentity,
) error {
	if !cfg.Enabled || !cfg.VideoJobsEnabled {
		return nil
	}
	expected, err := cfg.Contract.Identity()
	if err != nil || providerContract == nil || !expected.Equal(*providerContract) {
		return ErrMissingMultimodalDependency
	}
	return nil
}

func ValidateMultimodalAPIRuntime(cfg MultimodalConfig, dependencies MultimodalRuntimeDependencies) error {
	if !cfg.Enabled {
		return nil
	}
	if cfg.QueryEmbeddingEnabled || cfg.HybridSearchEnabled {
		expected, err := cfg.Contract.Identity()
		if err != nil || dependencies.ProviderContract == nil ||
			!expected.Equal(*dependencies.ProviderContract) || !dependencies.QueryCache {
			return ErrMissingMultimodalDependency
		}
	}
	if (cfg.HybridSearchEnabled || cfg.SimilarVideosEnabled) && !dependencies.ExactRetrieval {
		return ErrMissingMultimodalDependency
	}
	return nil
}

func (cfg MultimodalContractConfig) Identity() (domainembedding.MultimodalContractIdentity, error) {
	return domainembedding.NewMultimodalContractIdentity(
		cfg.ProviderAlias,
		cfg.ModelAlias,
		cfg.RevisionAlias,
		cfg.Dimension,
		cfg.TextCanonicalizer,
		cfg.FrameSamplingPolicy,
		cfg.ImagePreprocessingPolicy,
		cfg.FusionPolicy,
	)
}

func multimodalContractFieldCount(contract MultimodalContractConfig) int {
	fields := []bool{
		strings.TrimSpace(contract.ProviderAlias) != "",
		strings.TrimSpace(contract.ModelAlias) != "",
		strings.TrimSpace(contract.RevisionAlias) != "",
		contract.Dimension != 0,
		strings.TrimSpace(contract.TextCanonicalizer) != "",
		strings.TrimSpace(contract.FrameSamplingPolicy) != "",
		strings.TrimSpace(contract.ImagePreprocessingPolicy) != "",
		strings.TrimSpace(contract.FusionPolicy) != "",
	}
	configured := 0
	for _, present := range fields {
		if present {
			configured++
		}
	}
	return configured
}
