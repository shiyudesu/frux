package infraconfig

import (
	"errors"
	"testing"

	domainembedding "github.com/shiyudesu/frux/internal/domain/embedding"
	multimodalprofile "github.com/shiyudesu/frux/internal/infra/multimodalprofile"
)

func validMultimodalConfig() MultimodalConfig {
	return MultimodalConfig{
		Enabled: true, VideoJobsEnabled: true, QueryEmbeddingEnabled: true,
		HybridSearchEnabled: true, SimilarVideosEnabled: true,
		Provider: MultimodalProviderConfig{
			Endpoint: "https://multimodal.example.com", HMACSecret: "multimodal-provider-secret-value-123",
		},
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
	if cfg.Enabled || cfg.Provider.ProtocolVersion != MultimodalProviderProtocolV1 ||
		cfg.Provider.StartupTimeout != defaultMultimodalStartupTimeout ||
		cfg.Provider.Deadline != defaultMultimodalProviderDeadline ||
		cfg.Provider.AdmissionLimit != 2 || cfg.Jobs.MaxAttempts != 5 || cfg.Jobs.LeaseTTL != "45s" ||
		cfg.Provider.MaxRequestBytes != defaultMultimodalMaxRequestBytes ||
		cfg.Provider.MaxResponseBytes != defaultMultimodalMaxResponseBytes ||
		cfg.Jobs.ShutdownTimeout != "15s" ||
		cfg.MaxVideoTextRunes != 2048 || cfg.Images.MaxCount != 4 ||
		cfg.Query.MaxRunes != 128 || cfg.Query.CacheEntries != 1000 ||
		cfg.Exact.MaxLimit != 100 || cfg.Hybrid.Version != domainembedding.MultimodalHybridMergeVersionV1 ||
		cfg.Hybrid.FallbackMode != domainembedding.MultimodalLexicalFallback ||
		cfg.Hybrid.PoolLimit != 100 || cfg.Hybrid.LexicalReservation != 20 ||
		cfg.Hybrid.SemanticReservation != 20 || cfg.Hybrid.CursorTTL != "15m" ||
		cfg.SessionRecommendationEnabled || cfg.Session.MaxSeeds != 21 || cfg.Session.MaxLookback != "24h" ||
		cfg.SessionShadow.Enabled || cfg.SessionShadow.SamplePPM != 0 || cfg.SessionShadow.Budget != 50 ||
		cfg.SessionShadow.Deadline != "250ms" || cfg.SessionShadow.MaxInFlight != 2 ||
		cfg.SessionShadow.ComparisonLimit != 100 || cfg.SessionShadow.SimulatedPoolLimit != 100 ||
		cfg.SessionShadow.SimulatedTopK != 20 || cfg.SessionShadow.SemanticReservation != 10 ||
		cfg.SessionShadow.SemanticWeight != 0.25 || cfg.SessionShadow.ShutdownTimeout != "2s" {
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

func TestNormalizeAndValidateMultimodalConfigResolvesSelectedProfile(t *testing.T) {
	cfg := validMultimodalConfig()
	cfg.Profile = multimodalprofile.TongyiFlashStableProfile
	cfg.Contract = MultimodalContractConfig{}
	if err := normalizeAndValidateMultimodalConfig(&cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Contract.ProviderAlias != multimodalprofile.TongyiProviderAlias ||
		cfg.Contract.RevisionAlias != "stable-independent-mean-v1" ||
		cfg.Contract.FusionPolicy != domainembedding.MultimodalNormalizedMeanFusionV1 {
		t.Fatalf("resolved config=%#v", cfg)
	}
}

func TestNormalizeAndValidateMultimodalConfigRejectsUnknownProfile(t *testing.T) {
	cfg := validMultimodalConfig()
	cfg.Profile = "tongyi-embedding-vision-plus"
	if err := normalizeAndValidateMultimodalConfig(&cfg); !errors.Is(err, ErrInvalidMultimodalConfig) {
		t.Fatalf("error=%v", err)
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
		{name: "weak startup timeout", mutate: func(c *MultimodalConfig) { c.Provider.StartupTimeout = "10ms" }},
		{name: "unbounded admission", mutate: func(c *MultimodalConfig) { c.Provider.AdmissionLimit = 65 }},
		{name: "unknown protocol", mutate: func(c *MultimodalConfig) { c.Provider.ProtocolVersion = "v2" }},
		{name: "missing endpoint", mutate: func(c *MultimodalConfig) { c.Provider.Endpoint = "" }},
		{name: "missing secret", mutate: func(c *MultimodalConfig) { c.Provider.HMACSecret = "" }},
		{name: "short secret", mutate: func(c *MultimodalConfig) { c.Provider.HMACSecret = "short" }},
		{name: "remote http", mutate: func(c *MultimodalConfig) {
			c.Provider.Endpoint = "http://multimodal.example.com"
			c.Provider.AllowInsecureLocal = true
		}},
		{name: "request too small", mutate: func(c *MultimodalConfig) { c.Provider.MaxRequestBytes = 1 << 20 }},
		{name: "response too small", mutate: func(c *MultimodalConfig) { c.Provider.MaxResponseBytes = 1024 }},
		{name: "unbounded video text", mutate: func(c *MultimodalConfig) { c.MaxVideoTextRunes = 8193 }},
		{name: "short lease", mutate: func(c *MultimodalConfig) { c.Jobs.LeaseTTL = "1s" }},
		{name: "slow heartbeat", mutate: func(c *MultimodalConfig) { c.Jobs.HeartbeatInterval = "30s" }},
		{name: "invalid retry range", mutate: func(c *MultimodalConfig) { c.Jobs.RetryBase = "20m" }},
		{name: "invalid shutdown", mutate: func(c *MultimodalConfig) { c.Jobs.ShutdownTimeout = "100ms" }},
		{name: "unbounded image count", mutate: func(c *MultimodalConfig) { c.Images.MaxCount = 17 }},
		{name: "invalid image type", mutate: func(c *MultimodalConfig) { c.Images.AllowedMIMETypes = []string{"image/svg+xml"} }},
		{name: "weak query cache", mutate: func(c *MultimodalConfig) { c.Query.CacheTTL = "100ms" }},
		{name: "unbounded exact limit", mutate: func(c *MultimodalConfig) { c.Exact.MaxLimit = 501 }},
		{name: "unbounded session seeds", mutate: func(c *MultimodalConfig) { c.Session.MaxSeeds = 22 }},
		{name: "unbounded session lookback", mutate: func(c *MultimodalConfig) { c.Session.MaxLookback = "25h" }},
		{name: "shadow without session runtime", mutate: func(c *MultimodalConfig) {
			c.SessionShadow.Enabled = true
			c.SessionRecommendationEnabled = false
		}},
		{name: "shadow sample ppm", mutate: func(c *MultimodalConfig) { c.SessionShadow.SamplePPM = 1_000_001 }},
		{name: "shadow budget", mutate: func(c *MultimodalConfig) { c.SessionShadow.Budget = 101 }},
		{name: "shadow deadline", mutate: func(c *MultimodalConfig) { c.SessionShadow.Deadline = "1s" }},
		{name: "shadow capacity", mutate: func(c *MultimodalConfig) { c.SessionShadow.MaxInFlight = 17 }},
		{name: "shadow comparison", mutate: func(c *MultimodalConfig) { c.SessionShadow.ComparisonLimit = 501 }},
		{name: "shadow pool", mutate: func(c *MultimodalConfig) { c.SessionShadow.SimulatedPoolLimit = 49 }},
		{name: "shadow top k", mutate: func(c *MultimodalConfig) { c.SessionShadow.SimulatedTopK = 101 }},
		{name: "shadow reservation", mutate: func(c *MultimodalConfig) { c.SessionShadow.SemanticReservation = 51 }},
		{name: "shadow weight", mutate: func(c *MultimodalConfig) { c.SessionShadow.SemanticWeight = -0.1 }},
		{name: "shadow shutdown", mutate: func(c *MultimodalConfig) { c.SessionShadow.ShutdownTimeout = "10ms" }},
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

func TestSessionSemanticShadowConfigRequiresCompleteSessionRuntime(t *testing.T) {
	cfg := validMultimodalConfig()
	cfg.VideoJobsEnabled = false
	cfg.QueryEmbeddingEnabled = false
	cfg.HybridSearchEnabled = false
	cfg.SimilarVideosEnabled = false
	cfg.SessionRecommendationEnabled = true
	cfg.SessionShadow.Enabled = true
	cfg.SessionShadow.SamplePPM = 10_000
	cfg.Provider = MultimodalProviderConfig{}
	if err := normalizeAndValidateMultimodalConfig(&cfg); err != nil {
		t.Fatal(err)
	}
	if !cfg.SessionShadow.Enabled || cfg.SessionShadow.SamplePPM != 10_000 {
		t.Fatalf("shadow config=%#v", cfg.SessionShadow)
	}
	ready := MultimodalRuntimeDependencies{ExactRetrieval: true, SessionRecommendation: true}
	if err := ValidateMultimodalAPIRuntime(cfg, ready); err != nil {
		t.Fatalf("shadow runtime rejected: %v", err)
	}
}

func TestApplyMultimodalEnvironmentOverridesIsStrictAndDefaultPreserving(t *testing.T) {
	for _, test := range []struct {
		name        string
		parent      string
		videoJobs   string
		session     string
		full        string
		wantParent  bool
		wantVideo   bool
		wantSession bool
		wantFull    bool
		wantErr     bool
	}{
		{name: "blank preserves yaml", parent: " ", videoJobs: "", session: "", full: " ", wantParent: false, wantSession: false},
		{name: "enable video and development full runtime", parent: "true", videoJobs: "true", session: "TRUE", full: "true", wantParent: true, wantVideo: true, wantSession: true, wantFull: true},
		{name: "explicit disable", parent: "false", videoJobs: "false", session: "false", full: "false", wantParent: false, wantSession: false},
		{name: "invalid parent", parent: "enabled", session: "true", wantErr: true},
		{name: "invalid video jobs", parent: "true", videoJobs: "yes", session: "true", wantErr: true},
		{name: "invalid session", parent: "true", session: "yes", wantErr: true},
		{name: "invalid full rollout", parent: "true", session: "true", full: "yes", wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("FRUX_MULTIMODAL_ENABLED", test.parent)
			t.Setenv("FRUX_MULTIMODAL_VIDEO_JOBS_ENABLED", test.videoJobs)
			t.Setenv("FRUX_MULTIMODAL_SESSION_RECOMMENDATION_ENABLED", test.session)
			t.Setenv("FRUX_MULTIMODAL_SESSION_DEVELOPMENT_FULL_ROLLOUT_ENABLED", test.full)
			cfg := MultimodalConfig{}
			err := applyMultimodalEnvironmentOverrides(&cfg)
			if (err != nil) != test.wantErr {
				t.Fatalf("error=%v", err)
			}
			if err == nil && (cfg.Enabled != test.wantParent || cfg.VideoJobsEnabled != test.wantVideo ||
				cfg.SessionRecommendationEnabled != test.wantSession ||
				cfg.Session.DevelopmentFullRolloutEnabled != test.wantFull) {
				t.Fatalf("config=%#v", cfg)
			}
		})
	}
}

func TestNormalizeAndValidateMultimodalConfigAllowsExactLocalComposeProvider(t *testing.T) {
	cfg := validMultimodalConfig()
	cfg.Provider.Endpoint = "http://multimodal-provider:8099"
	cfg.Provider.AllowInsecureLocal = true
	if err := normalizeAndValidateMultimodalConfig(&cfg); err != nil {
		t.Fatal(err)
	}
	for _, endpoint := range []string{
		"http://adapter:8099",
		"http://multimodal-provider.example.com:8099",
	} {
		candidate := validMultimodalConfig()
		candidate.Provider.Endpoint = endpoint
		candidate.Provider.AllowInsecureLocal = true
		if err := normalizeAndValidateMultimodalConfig(&candidate); !errors.Is(err, ErrInvalidMultimodalConfig) {
			t.Fatalf("endpoint=%q error=%v", endpoint, err)
		}
	}
}

func TestSessionOnlyEnvironmentOverrideNeedsNoProvider(t *testing.T) {
	t.Setenv("FRUX_MULTIMODAL_ENABLED", "true")
	t.Setenv("FRUX_MULTIMODAL_SESSION_RECOMMENDATION_ENABLED", "true")
	t.Setenv("FRUX_MULTIMODAL_SESSION_DEVELOPMENT_FULL_ROLLOUT_ENABLED", "true")
	cfg := validMultimodalConfig()
	cfg.Enabled = false
	cfg.VideoJobsEnabled = false
	cfg.QueryEmbeddingEnabled = false
	cfg.HybridSearchEnabled = false
	cfg.SimilarVideosEnabled = false
	cfg.SessionRecommendationEnabled = false
	cfg.Provider = MultimodalProviderConfig{}
	if err := applyMultimodalEnvironmentOverrides(&cfg); err != nil {
		t.Fatal(err)
	}
	if err := normalizeAndValidateMultimodalConfig(&cfg); err != nil {
		t.Fatal(err)
	}
	if !cfg.Session.DevelopmentFullRolloutEnabled {
		t.Fatal("development full rollout override was not applied")
	}
	if err := ValidateMultimodalAPIRuntime(cfg, MultimodalRuntimeDependencies{
		ExactRetrieval: true, SessionRecommendation: true,
	}); err != nil {
		t.Fatalf("session-only runtime required Provider: %v", err)
	}
}

func TestDevelopmentFullRolloutRequiresSessionRuntime(t *testing.T) {
	cfg := validMultimodalConfig()
	cfg.Session.DevelopmentFullRolloutEnabled = true
	cfg.SessionRecommendationEnabled = false
	if err := normalizeAndValidateMultimodalConfig(&cfg); !errors.Is(err, ErrInvalidMultimodalConfig) {
		t.Fatalf("error=%v", err)
	}
}

func TestSessionSemanticRuntimeRequiresExactButNotProvider(t *testing.T) {
	cfg := validMultimodalConfig()
	cfg.VideoJobsEnabled = false
	cfg.QueryEmbeddingEnabled = false
	cfg.HybridSearchEnabled = false
	cfg.SimilarVideosEnabled = false
	cfg.SessionRecommendationEnabled = true
	cfg.Provider = MultimodalProviderConfig{}
	if err := normalizeAndValidateMultimodalConfig(&cfg); err != nil {
		t.Fatal(err)
	}
	ready := MultimodalRuntimeDependencies{ExactRetrieval: true, SessionRecommendation: true}
	if err := ValidateMultimodalRuntime(cfg, ready); err != nil {
		t.Fatalf("session-only runtime required provider: %v", err)
	}
	if err := ValidateMultimodalAPIRuntime(cfg, ready); err != nil {
		t.Fatalf("session-only API required provider: %v", err)
	}
	for _, dependencies := range []MultimodalRuntimeDependencies{
		{},
		{ExactRetrieval: true},
		{SessionRecommendation: true},
	} {
		if err := ValidateMultimodalAPIRuntime(cfg, dependencies); !errors.Is(err, ErrMissingMultimodalDependency) {
			t.Fatalf("dependencies=%#v error=%v", dependencies, err)
		}
	}
}

func TestNormalizeAndValidateMultimodalConfigAcceptsExplicitLoopbackProvider(t *testing.T) {
	cfg := validMultimodalConfig()
	cfg.Provider.Endpoint = "http://127.0.0.1:8099/"
	cfg.Provider.AllowInsecureLocal = true
	if err := normalizeAndValidateMultimodalConfig(&cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Provider.Endpoint != "http://127.0.0.1:8099" {
		t.Fatalf("endpoint = %q", cfg.Provider.Endpoint)
	}
}

func TestNormalizeAndValidateMultimodalConfigRejectsPartialDisabledProvider(t *testing.T) {
	cfg := MultimodalConfig{Provider: MultimodalProviderConfig{Endpoint: "https://multimodal.example.com"}}
	if err := normalizeAndValidateMultimodalConfig(&cfg); !errors.Is(err, ErrInvalidMultimodalConfig) {
		t.Fatalf("error = %v, want %v", err, ErrInvalidMultimodalConfig)
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

func TestValidateMultimodalRuntimeScopesDependenciesByProcess(t *testing.T) {
	cfg := validMultimodalConfig()
	if err := normalizeAndValidateMultimodalConfig(&cfg); err != nil {
		t.Fatal(err)
	}
	contract, err := cfg.Contract.Identity()
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateMultimodalWorkerRuntime(cfg, nil); !errors.Is(err, ErrMissingMultimodalDependency) {
		t.Fatalf("worker missing provider error=%v", err)
	}
	if err := ValidateMultimodalWorkerRuntime(cfg, &contract); err != nil {
		t.Fatalf("worker rejected matching provider: %v", err)
	}
	if err := ValidateMultimodalAPIRuntime(cfg, MultimodalRuntimeDependencies{ExactRetrieval: true}); !errors.Is(err, ErrMissingMultimodalDependency) {
		t.Fatalf("API missing query dependencies error=%v", err)
	}
	similarOnly := cfg
	similarOnly.VideoJobsEnabled = false
	similarOnly.QueryEmbeddingEnabled = false
	similarOnly.HybridSearchEnabled = false
	if err := ValidateMultimodalAPIRuntime(similarOnly, MultimodalRuntimeDependencies{ExactRetrieval: true}); err != nil {
		t.Fatalf("similar-only API required a provider: %v", err)
	}
}
