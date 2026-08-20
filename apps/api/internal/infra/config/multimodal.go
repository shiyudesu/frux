package infraconfig

import (
	"errors"
	"slices"
	"strings"
	"time"

	domainembedding "github.com/shiyudesu/frux/internal/domain/embedding"
)

var ErrInvalidMultimodalConfig = errors.New("invalid multimodal config")
var ErrMissingMultimodalDependency = errors.New("missing multimodal runtime dependency")

const (
	defaultMultimodalProviderDeadline = "15s"
	defaultMultimodalExactDeadline    = "500ms"
	defaultMultimodalQueryCacheTTL    = "10m"
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

	cfg.Provider.Deadline = defaultDuration(cfg.Provider.Deadline, defaultMultimodalProviderDeadline)
	if cfg.Provider.AdmissionLimit == 0 {
		cfg.Provider.AdmissionLimit = 2
	}
	providerDeadline, err := time.ParseDuration(cfg.Provider.Deadline)
	if err != nil || providerDeadline < 100*time.Millisecond || providerDeadline > 2*time.Minute ||
		cfg.Provider.AdmissionLimit < 1 || cfg.Provider.AdmissionLimit > 64 {
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
	if cfg.Hybrid.Version != domainembedding.MultimodalHybridMergeVersionV1 ||
		cfg.Hybrid.FallbackMode != domainembedding.MultimodalLexicalFallback {
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
