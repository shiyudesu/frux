package infraconfig

import (
	"errors"
	"testing"
)

func TestSemanticEmbeddingConfigurationIsStrictAndOptional(t *testing.T) {
	disabled := SemanticEmbeddingConfig{}
	if err := normalizeAndValidateSemanticEmbeddingConfig(
		&disabled,
		&InternalConfig{},
	); err != nil {
		t.Fatal(err)
	}
	if disabled.MetadataTimeout != "3s" || disabled.RequestTimeout != "17s" {
		t.Fatalf("disabled defaults = %+v", disabled)
	}
	enabled := SemanticEmbeddingConfig{
		Enabled: true, BaseURL: "http://semantic-embedding:8081",
		MetadataTimeout: "3s", RequestTimeout: "17s",
		CoverageInterval: "5m", LeaseTTL: "30s", PollInterval: "1s",
		WorkerConcurrency: 2,
	}
	internal := InternalConfig{
		Enabled: true,
		Token:   "Strong-Internal-Token-For-Semantic-123!",
	}
	if err := normalizeAndValidateSemanticEmbeddingConfig(&enabled, &internal); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		mutate func(*SemanticEmbeddingConfig, *InternalConfig)
	}{
		{name: "URL", mutate: func(cfg *SemanticEmbeddingConfig, _ *InternalConfig) {
			cfg.BaseURL = "http://semantic-embedding:8081/path"
		}},
		{name: "userinfo", mutate: func(cfg *SemanticEmbeddingConfig, _ *InternalConfig) {
			cfg.BaseURL = "http://user@semantic-embedding:8081"
		}},
		{name: "internal disabled", mutate: func(_ *SemanticEmbeddingConfig, internal *InternalConfig) {
			internal.Enabled = false
		}},
		{name: "weak token", mutate: func(_ *SemanticEmbeddingConfig, internal *InternalConfig) {
			internal.Token = "weak"
		}},
		{name: "metadata minimum", mutate: func(cfg *SemanticEmbeddingConfig, _ *InternalConfig) {
			cfg.MetadataTimeout = "499ms"
		}},
		{name: "metadata maximum", mutate: func(cfg *SemanticEmbeddingConfig, _ *InternalConfig) {
			cfg.MetadataTimeout = "5001ms"
		}},
		{name: "request minimum", mutate: func(cfg *SemanticEmbeddingConfig, _ *InternalConfig) {
			cfg.RequestTimeout = "999ms"
		}},
		{name: "request maximum", mutate: func(cfg *SemanticEmbeddingConfig, _ *InternalConfig) {
			cfg.RequestTimeout = "20.001s"
		}},
		{name: "coverage minimum", mutate: func(cfg *SemanticEmbeddingConfig, _ *InternalConfig) {
			cfg.CoverageInterval = "59s"
		}},
		{name: "coverage maximum", mutate: func(cfg *SemanticEmbeddingConfig, _ *InternalConfig) {
			cfg.CoverageInterval = "61m"
		}},
		{name: "concurrency", mutate: func(cfg *SemanticEmbeddingConfig, _ *InternalConfig) {
			cfg.WorkerConcurrency = 3
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := enabled
			auth := internal
			test.mutate(&cfg, &auth)
			if err := normalizeAndValidateSemanticEmbeddingConfig(
				&cfg,
				&auth,
			); !errors.Is(err, ErrInvalidSemanticEmbeddingConfig) {
				t.Fatalf("invalid semantic config error = %v", err)
			}
		})
	}
}
