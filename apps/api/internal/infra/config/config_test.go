package infraconfig

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	domainembedding "github.com/shiyudesu/frux/internal/domain/embedding"
	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
	multimodalprofile "github.com/shiyudesu/frux/internal/infra/multimodalprofile"
)

func TestNormalizeAndValidateMediaConfigDefaultsToLocal(t *testing.T) {
	var cfg MediaConfig
	if err := normalizeAndValidateMediaConfig(&cfg); err != nil {
		t.Fatalf("normalize local media config: %v", err)
	}
	if cfg.Backend != domainmedia.StorageBackendLocal ||
		cfg.LocalRoot != "./uploads" ||
		cfg.PublicBaseURL != "/media" ||
		cfg.Processing.ProfileVersion != "v2" ||
		cfg.Processing.MaxDuration != "180m" ||
		cfg.Processing.CommandTimeout != "360m" ||
		cfg.Processing.FFmpegPreset != "veryfast" {
		t.Fatalf("unexpected local defaults: %+v", cfg)
	}
}

func TestNormalizeAndValidateMediaConfigRejectsUnsafeProcessingPolicy(t *testing.T) {
	tests := []MediaProcessingConfig{
		{MaxDuration: "180m", CommandTimeout: "60m", FFmpegPreset: "veryfast"},
		{MaxDuration: "25h", CommandTimeout: "25h", FFmpegPreset: "veryfast"},
		{MaxDuration: "180m", CommandTimeout: "360m", FFmpegPreset: "invalid"},
	}
	for _, processing := range tests {
		cfg := MediaConfig{Processing: processing}
		if err := normalizeAndValidateMediaConfig(&cfg); !errors.Is(err, ErrInvalidMediaConfig) {
			t.Fatalf("processing=%+v error=%v", processing, err)
		}
	}
}

func TestNormalizeAndValidateMediaConfigRejectsIncompleteS3(t *testing.T) {
	cfg := MediaConfig{
		Backend: domainmedia.StorageBackendS3,
		S3:      S3Config{Region: "us-east-1"},
	}
	if err := normalizeAndValidateMediaConfig(&cfg); !errors.Is(err, ErrInvalidMediaConfig) {
		t.Fatalf("expected invalid S3 config, got %v", err)
	}
}

func TestNormalizeAndValidateMediaConfigAcceptsS3CompatibleEndpoint(t *testing.T) {
	cfg := MediaConfig{
		Backend:       domainmedia.StorageBackendS3,
		PublicBaseURL: "http://127.0.0.1:9000/frux-media/",
		S3: S3Config{
			Endpoint: "http://minio:9000/", PresignEndpoint: "http://127.0.0.1:9000/",
			Region: "us-east-1", Bucket: "frux-media",
			AccessKey: "minio", SecretKey: "secret", UsePathStyle: true,
		},
	}
	if err := normalizeAndValidateMediaConfig(&cfg); err != nil {
		t.Fatalf("normalize S3 media config: %v", err)
	}
	if cfg.S3.Endpoint != "http://minio:9000" ||
		cfg.S3.PresignEndpoint != "http://127.0.0.1:9000" {
		t.Fatalf("unexpected endpoints: %+v", cfg.S3)
	}
}

func TestNormalizeAndValidateMediaConfigPreservesDefaultS3EndpointSupport(t *testing.T) {
	cfg := MediaConfig{
		Backend:       domainmedia.StorageBackendS3,
		PublicBaseURL: "/media",
		S3: S3Config{
			Region: "us-east-1",
			Bucket: "frux-media",
		},
	}
	if err := normalizeAndValidateMediaConfig(&cfg); err != nil {
		t.Fatalf("normalize default S3 endpoint: %v", err)
	}
	if cfg.S3.Endpoint != "" || cfg.S3.PresignEndpoint != "" {
		t.Fatalf("unexpected default endpoints: %+v", cfg.S3)
	}
}

func TestNormalizeAndValidateMediaConfigRejectsInvalidPublicS3Endpoints(t *testing.T) {
	tests := []struct {
		name            string
		publicBaseURL   string
		endpoint        string
		presignEndpoint string
	}{
		{
			name:          "remote insecure public base",
			publicBaseURL: "http://frux.example.com:18443/media",
			endpoint:      "http://minio:9000", presignEndpoint: "https://s3.example.com:18443",
		},
		{
			name:          "remote insecure presign",
			publicBaseURL: "https://frux.example.com:18443/media",
			endpoint:      "http://minio:9000", presignEndpoint: "http://s3.example.com:18443",
		},
		{
			name:          "shared public hostname",
			publicBaseURL: "https://frux.example.com:18443/media",
			endpoint:      "http://minio:9000", presignEndpoint: "https://frux.example.com:18443",
		},
		{
			name:          "invalid public port",
			publicBaseURL: "https://frux.example.com:70000/media",
			endpoint:      "http://minio:9000", presignEndpoint: "https://s3.example.com:18443",
		},
		{
			name:          "runtime endpoint path",
			publicBaseURL: "https://frux.example.com:18443/media",
			endpoint:      "http://minio:9000/storage", presignEndpoint: "https://s3.example.com:18443",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := MediaConfig{
				Backend: domainmedia.StorageBackendS3, PublicBaseURL: test.publicBaseURL,
				S3: S3Config{
					Endpoint: test.endpoint, PresignEndpoint: test.presignEndpoint,
					Region: "us-east-1", Bucket: "frux-media",
					AccessKey: "minio", SecretKey: "secret", UsePathStyle: true,
					RequirePublicHTTPS: true,
				},
			}
			if err := normalizeAndValidateMediaConfig(&cfg); !errors.Is(err, ErrInvalidMediaConfig) {
				t.Fatalf("expected invalid media config, got %v", err)
			}
		})
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

func TestNormalizeAndValidateModerationConfig(t *testing.T) {
	var cfg ModerationConfig
	if err := normalizeAndValidateModerationConfig(&cfg); err != nil {
		t.Fatalf("disabled moderation defaults: %v", err)
	}
	if cfg.Mode != "disabled" ||
		cfg.ProviderConfigVersion != 1 ||
		cfg.InputProfileVersion != "frames-v1" ||
		cfg.WorkerConcurrency != 2 ||
		cfg.MaxAttempts != 5 {
		t.Fatalf("unexpected moderation defaults: %+v", cfg)
	}

	cfg = ModerationConfig{
		Mode: "observe", ProviderConfigVersion: 2,
		Endpoint:   "http://127.0.0.1:9001",
		HMACSecret: strings.Repeat("s", 32), AllowInsecureLocal: true,
	}
	if err := normalizeAndValidateModerationConfig(&cfg); err != nil {
		t.Fatalf("local observe moderation: %v", err)
	}
	localMedia := MediaConfig{Backend: domainmedia.StorageBackendLocal}
	if err := validateModerationMediaConfig(&cfg, &localMedia); err != nil {
		t.Fatalf("local moderation media: %v", err)
	}
	cfg.Endpoint = "http://provider.example.com"
	if err := normalizeAndValidateModerationConfig(&cfg); !errors.Is(err, ErrInvalidModerationConfig) {
		t.Fatalf("insecure remote endpoint error = %v", err)
	}
	cfg.Endpoint = "https://provider.example.com"
	cfg.Mode = "unsafe"
	if err := normalizeAndValidateModerationConfig(&cfg); !errors.Is(err, ErrInvalidModerationConfig) {
		t.Fatalf("invalid rollout mode error = %v", err)
	}

	cfg = ModerationConfig{
		Mode: "observe", ProviderConfigVersion: 2,
		Endpoint:              "https://provider.example.com",
		HMACSecret:            strings.Repeat("s", 32),
		SamplePresignEndpoint: "https://media.example.com",
	}
	if err := normalizeAndValidateModerationConfig(&cfg); err != nil {
		t.Fatal(err)
	}
	s3Media := MediaConfig{Backend: domainmedia.StorageBackendS3}
	if err := validateModerationMediaConfig(&cfg, &s3Media); err != nil {
		t.Fatalf("remote moderation media: %v", err)
	}
	cfg.SamplePresignEndpoint = "http://127.0.0.1:9000"
	if err := validateModerationMediaConfig(&cfg, &s3Media); !errors.Is(err, ErrInvalidModerationConfig) {
		t.Fatalf("unreachable sample endpoint error = %v", err)
	}
}

func TestNormalizeAndValidateJWTConfigBoundsAdminTTL(t *testing.T) {
	cfg := JWTConfig{Secret: "secret", AccessTTL: "15m"}
	if err := normalizeAndValidateJWTConfig(&cfg); err != nil {
		t.Fatalf("default admin TTL: %v", err)
	}
	if cfg.AdminAccessTTL != "30m" {
		t.Fatalf("admin TTL = %q", cfg.AdminAccessTTL)
	}
	cfg.AdminAccessTTL = "9h"
	if err := normalizeAndValidateJWTConfig(&cfg); !errors.Is(err, ErrInvalidJWTConfig) {
		t.Fatalf("oversized admin TTL error = %v", err)
	}
	cfg.AdminAccessTTL = "1m"
	if err := normalizeAndValidateJWTConfig(&cfg); !errors.Is(err, ErrInvalidJWTConfig) {
		t.Fatalf("short admin TTL error = %v", err)
	}
}

func TestNormalizeAndValidateGovernanceConfig(t *testing.T) {
	var cfg GovernanceConfig
	if err := normalizeAndValidateGovernanceConfig(&cfg); err != nil {
		t.Fatalf("normalize governance config: %v", err)
	}
	if cfg.PollInterval != "5s" || cfg.PollTimeout != "2s" {
		t.Fatalf("unexpected governance defaults: %+v", cfg)
	}
	cfg.PollTimeout = "6s"
	if err := normalizeAndValidateGovernanceConfig(&cfg); !errors.Is(err, ErrInvalidGovernanceConfig) {
		t.Fatalf("expected invalid governance timeout, got %v", err)
	}
}

func TestNormalizeAndValidateRateLimitConfig(t *testing.T) {
	var cfg RateLimitConfig
	if err := normalizeAndValidateRateLimitConfig(&cfg); err != nil {
		t.Fatalf("normalize rate limit config: %v", err)
	}
	if cfg.MaxEntries != 10_000 ||
		cfg.IdleTTL != "10m" ||
		cfg.RedisTimeout != "75ms" {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	cfg.TrustedProxies = []string{"not-a-prefix"}
	if err := normalizeAndValidateRateLimitConfig(&cfg); !errors.Is(err, ErrInvalidRateLimitConfig) {
		t.Fatalf("expected invalid trusted proxy, got %v", err)
	}
	cfg = RateLimitConfig{MaxEntries: 99, IdleTTL: "10m", RedisTimeout: "75ms"}
	if err := normalizeAndValidateRateLimitConfig(&cfg); !errors.Is(err, ErrInvalidRateLimitConfig) {
		t.Fatalf("expected invalid capacity, got %v", err)
	}
}

func TestNormalizeAndValidateKafkaConfig(t *testing.T) {
	t.Run("disabled library config still normalizes", func(t *testing.T) {
		var cfg KafkaConfig
		if err := normalizeAndValidateKafkaConfig(&cfg); err != nil {
			t.Fatalf("disabled Kafka config: %v", err)
		}
		if cfg.Environment != "local" ||
			cfg.ClientID != "frux" ||
			cfg.Consumer.AssignmentTimeout != "15s" {
			t.Fatalf("unexpected defaults: %+v", cfg)
		}
	})

	t.Run("local provisioning", func(t *testing.T) {
		cfg := KafkaConfig{
			Enabled: true, Brokers: []string{"localhost:9092"},
			AllowLocalProvisioning: true,
		}
		if err := normalizeAndValidateKafkaConfig(&cfg); err != nil {
			t.Fatalf("local Kafka config: %v", err)
		}
		if cfg.ProductionValidation.ReplicationFactor != 1 ||
			cfg.ProductionValidation.MinInSyncReplicas != 1 {
			t.Fatalf("unexpected local topology: %+v", cfg.ProductionValidation)
		}
	})

	t.Run("authenticated TLS production", func(t *testing.T) {
		cfg := KafkaConfig{
			Enabled: true, Environment: "production",
			Brokers: []string{"broker-1.example.com:9093", "broker-2.example.com:9093"},
			Authentication: KafkaAuthenticationConfig{
				Mechanism: "scram-sha-512", Username: "frux", Password: "secret",
			},
			TLS: KafkaTLSConfig{Enabled: true, ServerName: "kafka.example.com"},
			ProductionValidation: KafkaProductionValidationConfig{
				ReplicationFactor: 3, MinInSyncReplicas: 2,
				RequireAuthentication: true, RequireTLS: true,
			},
		}
		if err := normalizeAndValidateKafkaConfig(&cfg); err != nil {
			t.Fatalf("production Kafka config: %v", err)
		}
	})

	for _, test := range []struct {
		name string
		cfg  KafkaConfig
	}{
		{name: "missing brokers", cfg: KafkaConfig{Enabled: true}},
		{name: "invalid broker", cfg: KafkaConfig{
			Enabled: true, Brokers: []string{"http://localhost:9092"},
		}},
		{name: "credentials without mechanism", cfg: KafkaConfig{
			Authentication: KafkaAuthenticationConfig{Username: "frux", Password: "secret"},
		}},
		{name: "partial mutual TLS", cfg: KafkaConfig{
			TLS: KafkaTLSConfig{Enabled: true, CertificateFile: "client.crt"},
		}},
		{name: "production local provisioning", cfg: KafkaConfig{
			Enabled: true, Environment: "production",
			Brokers: []string{"broker:9092"}, AllowLocalProvisioning: true,
		}},
		{name: "production unsafe replication", cfg: KafkaConfig{
			Enabled: true, Environment: "production", Brokers: []string{"broker:9092"},
			ProductionValidation: KafkaProductionValidationConfig{
				ReplicationFactor: 1, MinInSyncReplicas: 1,
			},
		}},
		{name: "production security requirements disabled", cfg: KafkaConfig{
			Enabled: true, Environment: "production", Brokers: []string{"broker:9093"},
			Authentication: KafkaAuthenticationConfig{
				Mechanism: "scram-sha-256", Username: "frux", Password: "secret",
			},
			TLS: KafkaTLSConfig{Enabled: true},
			ProductionValidation: KafkaProductionValidationConfig{
				ReplicationFactor: 3, MinInSyncReplicas: 2,
			},
		}},
		{name: "unbounded poll", cfg: KafkaConfig{
			Consumer: KafkaConsumerConfig{MaxPollRecords: 1001},
		}},
		{name: "unbounded assignment startup", cfg: KafkaConfig{
			Consumer: KafkaConsumerConfig{AssignmentTimeout: "61s"},
		}},
		{name: "runtime-invalid topic prefix", cfg: KafkaConfig{
			TopicPrefix: "Frux_Test",
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := normalizeAndValidateKafkaConfig(&test.cfg); !errors.Is(err, ErrInvalidKafkaConfig) {
				t.Fatalf("error = %v, want ErrInvalidKafkaConfig", err)
			}
		})
	}
}

func TestValidateAPIConfigRequiresKafkaRedisAndStrongInternalToken(t *testing.T) {
	validToken := "rT8v0%PzL2kQ7mX4cN9wA6dF1hJ5sB3y"
	tests := []struct {
		name  string
		cfg   Config
		valid bool
	}{
		{name: "disabled routes permit no token", cfg: finalRuntimeConfig(InternalConfig{}), valid: true},
		{name: "empty token", cfg: finalRuntimeConfig(InternalConfig{Enabled: true}), valid: false},
		{name: "placeholder token", cfg: finalRuntimeConfig(InternalConfig{
			Enabled: true, Token: "replace-with-internal-token",
		}), valid: false},
		{name: "short token", cfg: finalRuntimeConfig(InternalConfig{
			Enabled: true, Token: "short-token",
		}), valid: false},
		{name: "single character class", cfg: finalRuntimeConfig(InternalConfig{
			Enabled: true, Token: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		}), valid: false},
		{name: "embedded newline", cfg: finalRuntimeConfig(InternalConfig{
			Enabled: true, Token: "rT8v0%PzL2kQ7mX4\ncN9wA6dF1hJ5sB3y",
		}), valid: false},
		{name: "non ascii", cfg: finalRuntimeConfig(InternalConfig{
			Enabled: true, Token: "rT8v0%PzL2kQ7mX4cN9wA6dF1hJ5sB中",
		}), valid: false},
		{name: "strong token", cfg: finalRuntimeConfig(InternalConfig{
			Enabled: true, Token: validToken,
		}), valid: true},
		{name: "Kafka disabled", cfg: Config{
			Redis: RedisConfig{Addr: "localhost:6379"},
		}, valid: false},
		{name: "Redis missing", cfg: Config{
			Kafka: KafkaConfig{Enabled: true, Brokers: []string{"localhost:9092"}},
		}, valid: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateAPIConfig(&test.cfg)
			if test.valid && err != nil {
				t.Fatalf("ValidateAPIConfig() error = %v", err)
			}
			if !test.valid && err == nil {
				t.Fatal("ValidateAPIConfig() accepted invalid runtime")
			}
		})
	}
}

func TestLoadConfigExpandsInternalTokenFromEnvironment(t *testing.T) {
	token := "rT8v0%PzL2kQ7mX4cN9wA6dF1hJ5sB3y"
	t.Setenv("FRUX_INTERNAL_TOKEN", token)

	cfg, err := LoadConfig(filepath.Join("..", "..", "..", "configs", "config.yaml"))
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.Internal.Token != token || !cfg.Internal.Enabled {
		t.Fatalf("internal config = %+v, want enabled environment token", cfg.Internal)
	}
	if cfg.Multimodal.Enabled || cfg.Multimodal.Provider.Deadline != defaultMultimodalProviderDeadline ||
		cfg.Multimodal.Hybrid.FallbackMode != domainembedding.MultimodalLexicalFallback {
		t.Fatalf("multimodal config = %+v, want disabled-safe defaults", cfg.Multimodal)
	}
}

func TestLoadConfigResolvesMultimodalProfileFromEnvironment(t *testing.T) {
	t.Setenv("FRUX_INTERNAL_TOKEN", "rT8v0%PzL2kQ7mX4cN9wA6dF1hJ5sB3y")
	t.Setenv("FRUX_MULTIMODAL_PROFILE", multimodalprofile.TongyiFlashStableProfile)
	cfg, err := LoadConfig(filepath.Join("..", "..", "..", "configs", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Multimodal.Profile != multimodalprofile.TongyiFlashStableProfile ||
		cfg.Multimodal.Contract.RevisionAlias != "stable-independent-mean-v1" ||
		cfg.Multimodal.Contract.FusionPolicy != domainembedding.MultimodalNormalizedMeanFusionV1 {
		t.Fatalf("multimodal config=%#v", cfg.Multimodal)
	}
}

func TestLoadProdConfigUsesMinIOAndSingleKafka(t *testing.T) {
	environment := map[string]string{
		"FRUX_DOMAIN":              "frux.example.com",
		"FRUX_PUBLIC_HTTPS_PORT":   "18443",
		"FRUX_S3_DOMAIN":           "s3.frux.example.com",
		"FRUX_JWT_CONSUMER_SECRET": "prod-consumer-jwt-secret-123456",
		"FRUX_JWT_ADMIN_SECRET":    "prod-admin-jwt-secret-123456789",
		"FRUX_HMAC_SECRET":         "prod-application-hmac-secret-123456",
		"FRUX_INTERNAL_TOKEN":      "rT8v0%PzL2kQ7mX4cN9wA6dF1hJ5sB3y",
		"FRUX_POSTGRES_USER":       "frux",
		"FRUX_POSTGRES_PASSWORD":   "database-secret",
		"FRUX_POSTGRES_DATABASE":   "frux",
		"FRUX_REDIS_PASSWORD":      "redis-secret",
		"FRUX_S3_ACCESS_KEY":       "frux-app",
		"FRUX_S3_SECRET_KEY":       "minio-application-secret",
		"FRUX_S3_BUCKET":           "frux-media",
	}
	for name, value := range environment {
		t.Setenv(name, value)
	}

	cfg, err := LoadConfig(filepath.Join("..", "..", "..", "configs", "config.prod.yaml"))
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.Media.S3.Endpoint != "http://minio:9000" ||
		cfg.Media.S3.PresignEndpoint != "https://s3.frux.example.com:18443" ||
		cfg.Media.S3.Bucket != "frux-media" ||
		!cfg.Media.S3.UsePathStyle ||
		cfg.Media.S3.AutoCreateBucket ||
		!cfg.Media.S3.RequirePublicHTTPS ||
		cfg.Media.Processing.ProfileVersion != "v2" ||
		cfg.Media.Processing.MaxDuration != "180m" ||
		cfg.Media.Processing.CommandTimeout != "360m" ||
		cfg.Media.Processing.FFmpegPreset != "veryfast" ||
		!cfg.Media.Processing.DisableOrphanCleanup {
		t.Fatalf("prod media config = %+v", cfg.Media)
	}
	if cfg.Media.PublicBaseURL != "https://frux.example.com:18443/media" ||
		cfg.Database.Host != "postgres" ||
		cfg.Redis.Addr != "redis:6379" ||
		cfg.Kafka.Environment != "local" ||
		!cfg.Kafka.AllowLocalProvisioning ||
		cfg.Kafka.TLS.Enabled ||
		cfg.Kafka.Authentication.Mechanism != "none" ||
		len(cfg.Kafka.Brokers) != 1 ||
		cfg.Kafka.Brokers[0] != "kafka:9092" {
		t.Fatalf("prod runtime config = %+v", cfg)
	}
}

func TestLoadConfigDoesNotAcceptLegacyInternalTokenEnvironment(t *testing.T) {
	t.Setenv("FRUX_INTERNAL_TOKEN", "")
	t.Setenv("GC"+"FEED_INTERNAL_TOKEN", "rT8v0%PzL2kQ7mX4cN9wA6dF1hJ5sB3y")

	_, err := LoadConfig(filepath.Join("..", "..", "..", "configs", "config.yaml"))
	if !errors.Is(err, ErrInvalidInternalToken) {
		t.Fatalf("LoadConfig() error = %v, want ErrInvalidInternalToken", err)
	}
}

func finalRuntimeConfig(internal InternalConfig) Config {
	return Config{
		Internal: internal,
		Redis:    RedisConfig{Addr: "localhost:6379"},
		Kafka: KafkaConfig{
			Enabled: true,
			Brokers: []string{"localhost:9092"},
		},
	}
}
