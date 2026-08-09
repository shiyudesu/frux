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
	enabled.BaseURL = "http://user:secret@semantic-embedding:8081/path"
	if err := normalizeAndValidateSemanticEmbeddingConfig(
		&enabled,
		&internal,
	); !errors.Is(err, ErrInvalidSemanticEmbeddingConfig) {
		t.Fatalf("invalid URL error = %v", err)
	}
}
