package infraconfig

import (
	"errors"
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
