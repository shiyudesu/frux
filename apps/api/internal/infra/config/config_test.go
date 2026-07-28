package infraconfig

import (
	"errors"
	"path/filepath"
	"testing"

	domainmedia "GCFeed/internal/domain/media"
)

func TestNormalizeAndValidateMediaConfigDefaultsToLocal(t *testing.T) {
	var cfg MediaConfig
	if err := normalizeAndValidateMediaConfig(&cfg); err != nil {
		t.Fatalf("normalize local media config: %v", err)
	}
	if cfg.Backend != domainmedia.StorageBackendLocal || cfg.LocalRoot != "./uploads" || cfg.PublicBaseURL != "/media" {
		t.Fatalf("unexpected local defaults: %+v", cfg)
	}
}

func TestNormalizeAndValidateMediaConfigRejectsIncompleteS3(t *testing.T) {
	cfg := MediaConfig{Backend: domainmedia.StorageBackendS3, S3: S3Config{Region: "us-east-1"}}
	if err := normalizeAndValidateMediaConfig(&cfg); !errors.Is(err, ErrInvalidMediaConfig) {
		t.Fatalf("expected invalid S3 config, got %v", err)
	}
}

func TestNormalizeAndValidateMediaConfigAcceptsS3CompatibleEndpoint(t *testing.T) {
	cfg := MediaConfig{
		Backend: domainmedia.StorageBackendS3, PublicBaseURL: "http://127.0.0.1:9000/gcfeed-media/",
		S3: S3Config{
			Endpoint: "http://minio:9000/", PresignEndpoint: "http://127.0.0.1:9000/",
			Region: "us-east-1", Bucket: "gcfeed-media", AccessKey: "minio", SecretKey: "secret", UsePathStyle: true,
		},
	}

	if err := normalizeAndValidateMediaConfig(&cfg); err != nil {
		t.Fatalf("normalize S3 media config: %v", err)
	}
	if cfg.S3.Endpoint != "http://minio:9000" || cfg.S3.PresignEndpoint != "http://127.0.0.1:9000" {
		t.Fatalf("unexpected endpoints: %+v", cfg.S3)
	}
}

func TestNormalizeAndValidatePlaybackConfig(t *testing.T) {
	var cfg PlaybackConfig
	if err := normalizeAndValidatePlaybackConfig(&cfg); err != nil {
		t.Fatalf("normalize playback config: %v", err)
	}
	if cfg.Telemetry.Retention != "168h" ||
		cfg.Telemetry.CleanupInterval != "1h" ||
		cfg.Telemetry.CleanupBatchSize != 1000 ||
		cfg.Telemetry.MaxBatchesPerMinute != 60 {
		t.Fatalf("unexpected playback defaults: %+v", cfg.Telemetry)
	}

	cfg.Telemetry.CleanupInterval = "200h"
	if err := normalizeAndValidatePlaybackConfig(&cfg); !errors.Is(err, ErrInvalidPlaybackConfig) {
		t.Fatalf("expected invalid cleanup interval, got %v", err)
	}
}

func TestValidateAPIConfigRequiresStrongInternalTokenWhenEnabled(t *testing.T) {
	validToken := "rT8v0%PzL2kQ7mX4cN9wA6dF1hJ5sB3y"
	tests := []struct {
		name  string
		cfg   Config
		valid bool
	}{
		{name: "disabled routes permit no token", cfg: Config{Internal: InternalConfig{}}, valid: true},
		{name: "empty token", cfg: Config{Internal: InternalConfig{Enabled: true}}, valid: false},
		{name: "placeholder token", cfg: Config{Internal: InternalConfig{Enabled: true, Token: "replace-with-internal-token"}}, valid: false},
		{name: "short token", cfg: Config{Internal: InternalConfig{Enabled: true, Token: "short-token"}}, valid: false},
		{name: "single character class", cfg: Config{Internal: InternalConfig{Enabled: true, Token: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}, valid: false},
		{name: "strong token", cfg: Config{Internal: InternalConfig{Enabled: true, Token: validToken}}, valid: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateAPIConfig(&test.cfg)
			if test.valid && err != nil {
				t.Fatalf("ValidateAPIConfig() error = %v", err)
			}
			if !test.valid && !errors.Is(err, ErrInvalidInternalToken) {
				t.Fatalf("ValidateAPIConfig() error = %v, want ErrInvalidInternalToken", err)
			}
		})
	}
}

func TestLoadConfigExpandsInternalTokenFromEnvironment(t *testing.T) {
	token := "rT8v0%PzL2kQ7mX4cN9wA6dF1hJ5sB3y"
	t.Setenv("GCFEED_INTERNAL_TOKEN", token)

	cfg, err := LoadConfig(filepath.Join("..", "..", "..", "configs", "config.yaml"))
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.Internal.Token != token || !cfg.Internal.Enabled {
		t.Fatalf("internal config = %+v, want enabled environment token", cfg.Internal)
	}
}
