package infraconfig

import (
	"errors"
	"testing"

	domainembedding "github.com/shiyudesu/frux/internal/domain/embedding"
)

func validMultimodalConfig() MultimodalConfig {
	return MultimodalConfig{
		Enabled: true, VideoJobsEnabled: true, QueryEmbeddingEnabled: true,
		HybridSearchEnabled: true, SimilarVideosEnabled: true,
		Contract: MultimodalContractConfig{
			ProviderAlias: "provider", ModelAlias: "model", RevisionAlias: "revision",
			Dimension:                domainembedding.MinMultimodalDimension,
			TextCanonicalizer:        domainembedding.MultimodalTextCanonicalizerV1,
			FrameSamplingPolicy:      domainembedding.MultimodalFrameSamplingPolicyV1,
			ImagePreprocessingPolicy: domainembedding.MultimodalImagePreprocessingV1,
			FusionPolicy:             domainembedding.MultimodalFusionPolicyV1,
		},
	}
}

func TestNormalizeAndValidateMultimodalConfigDisabledDefaults(t *testing.T) {
	var cfg MultimodalConfig
	if err := normalizeAndValidateMultimodalConfig(&cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Enabled || cfg.Provider.Deadline != defaultMultimodalProviderDeadline ||
		cfg.Provider.AdmissionLimit != 2 || cfg.Jobs.MaxAttempts != 5 || cfg.Jobs.LeaseTTL != "45s" ||
		cfg.Jobs.ShutdownTimeout != "15s" ||
		cfg.MaxVideoTextRunes != 2048 || cfg.Images.MaxCount != 4 ||
		cfg.Query.MaxRunes != 128 || cfg.Query.CacheEntries != 1000 ||
		cfg.Exact.MaxLimit != 100 || cfg.Hybrid.Version != domainembedding.MultimodalHybridMergeVersionV1 ||
		cfg.Hybrid.FallbackMode != domainembedding.MultimodalLexicalFallback ||
		cfg.Hybrid.PoolLimit != 100 || cfg.Hybrid.LexicalReservation != 20 ||
		cfg.Hybrid.SemanticReservation != 20 || cfg.Hybrid.CursorTTL != "15m" {
		t.Fatalf("unexpected disabled defaults: %#v", cfg)
	}
	if err := ValidateMultimodalRuntime(cfg, MultimodalRuntimeDependencies{}); err != nil {
		t.Fatalf("disabled runtime required dependencies: %v", err)
	}
}

func TestNormalizeAndValidateMultimodalConfigAcceptsCompleteContract(t *testing.T) {
	cfg := validMultimodalConfig()
	cfg.Contract.ProviderAlias = " Provider-A "
	if err := normalizeAndValidateMultimodalConfig(&cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Contract.ProviderAlias != "provider-a" || cfg.Contract.Dimension != domainembedding.MinMultimodalDimension {
		t.Fatalf("contract was not normalized: %#v", cfg.Contract)
	}
	contract, err := cfg.Contract.Identity()
	if err != nil {
		t.Fatal(err)
	}
	dependencies := MultimodalRuntimeDependencies{ProviderContract: &contract, QueryCache: true, ExactRetrieval: true}
	if err := ValidateMultimodalRuntime(cfg, dependencies); err != nil {
		t.Fatalf("complete runtime rejected: %v", err)
	}
}

func TestNormalizeAndValidateMultimodalConfigRejectsInvalidContractsAndBounds(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*MultimodalConfig)
	}{
		{name: "partial contract", mutate: func(c *MultimodalConfig) { c.Contract.ModelAlias = "" }},
		{name: "unknown text policy", mutate: func(c *MultimodalConfig) { c.Contract.TextCanonicalizer = "unknown" }},
		{name: "unknown frame policy", mutate: func(c *MultimodalConfig) { c.Contract.FrameSamplingPolicy = "unknown" }},
		{name: "unknown preprocessing policy", mutate: func(c *MultimodalConfig) { c.Contract.ImagePreprocessingPolicy = "unknown" }},
		{name: "unknown fusion policy", mutate: func(c *MultimodalConfig) { c.Contract.FusionPolicy = "unknown" }},
		{name: "weak dimension", mutate: func(c *MultimodalConfig) { c.Contract.Dimension = domainembedding.MinMultimodalDimension - 1 }},
		{name: "weak deadline", mutate: func(c *MultimodalConfig) { c.Provider.Deadline = "10ms" }},
		{name: "unbounded admission", mutate: func(c *MultimodalConfig) { c.Provider.AdmissionLimit = 65 }},
		{name: "unbounded video text", mutate: func(c *MultimodalConfig) { c.MaxVideoTextRunes = 8193 }},
		{name: "short lease", mutate: func(c *MultimodalConfig) { c.Jobs.LeaseTTL = "1s" }},
		{name: "slow heartbeat", mutate: func(c *MultimodalConfig) { c.Jobs.HeartbeatInterval = "30s" }},
		{name: "invalid retry range", mutate: func(c *MultimodalConfig) { c.Jobs.RetryBase = "20m" }},
		{name: "invalid shutdown", mutate: func(c *MultimodalConfig) { c.Jobs.ShutdownTimeout = "100ms" }},
		{name: "unbounded image count", mutate: func(c *MultimodalConfig) { c.Images.MaxCount = 17 }},
		{name: "invalid image type", mutate: func(c *MultimodalConfig) { c.Images.AllowedMIMETypes = []string{"image/svg+xml"} }},
		{name: "weak query cache", mutate: func(c *MultimodalConfig) { c.Query.CacheTTL = "100ms" }},
		{name: "unbounded exact limit", mutate: func(c *MultimodalConfig) { c.Exact.MaxLimit = 501 }},
		{name: "unknown hybrid version", mutate: func(c *MultimodalConfig) { c.Hybrid.Version = "unknown" }},
		{name: "fallback disabled", mutate: func(c *MultimodalConfig) { c.Hybrid.FallbackMode = "none" }},
		{name: "hybrid pool below page bound", mutate: func(c *MultimodalConfig) { c.Hybrid.PoolLimit = 50 }},
		{name: "hybrid pool exceeds exact", mutate: func(c *MultimodalConfig) { c.Hybrid.PoolLimit = 101 }},
		{name: "hybrid reservations exceed pool", mutate: func(c *MultimodalConfig) { c.Hybrid.LexicalReservation = 81 }},
		{name: "hybrid without query", mutate: func(c *MultimodalConfig) { c.QueryEmbeddingEnabled = false }},
		{name: "disabled with active feature", mutate: func(c *MultimodalConfig) { c.Enabled = false }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := validMultimodalConfig()
			test.mutate(&cfg)
			if err := normalizeAndValidateMultimodalConfig(&cfg); !errors.Is(err, ErrInvalidMultimodalConfig) {
				t.Fatalf("error = %v, want %v", err, ErrInvalidMultimodalConfig)
			}
		})
	}
}

func TestValidateMultimodalRuntimeRequiresEnabledDependencies(t *testing.T) {
	cfg := validMultimodalConfig()
	if err := normalizeAndValidateMultimodalConfig(&cfg); err != nil {
		t.Fatal(err)
	}
	contract, err := cfg.Contract.Identity()
	if err != nil {
		t.Fatal(err)
	}
	mismatched := contract
	mismatched.Dimension++
	tests := []MultimodalRuntimeDependencies{
		{},
		{ProviderContract: &mismatched, QueryCache: true, ExactRetrieval: true},
		{ProviderContract: &contract},
		{ProviderContract: &contract, QueryCache: true},
	}
	for _, dependencies := range tests {
		if err := ValidateMultimodalRuntime(cfg, dependencies); !errors.Is(err, ErrMissingMultimodalDependency) {
			t.Fatalf("dependencies=%#v error=%v", dependencies, err)
		}
	}
}
