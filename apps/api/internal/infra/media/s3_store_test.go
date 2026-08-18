package inframedia

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"

	infraconfig "github.com/shiyudesu/frux/internal/infra/config"
)

func TestS3StoreUsesPublicPresignEndpoint(t *testing.T) {
	store, err := NewS3Store(context.Background(), infraconfig.S3Config{
		Endpoint:        "http://minio:9000",
		PresignEndpoint: "https://s3.frux.example.com:18443",
		Region:          "us-east-1",
		Bucket:          "frux-media",
		AccessKey:       "frux-app",
		SecretKey:       "application-secret",
		UsePathStyle:    true,
	})
	if err != nil {
		t.Fatal(err)
	}

	request, err := store.PresignPut(
		context.Background(),
		"uploads/video/test.mp4",
		"video/mp4",
		strings.Repeat("a", 64),
		128,
		15*time.Minute,
	)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(request.URL)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Scheme != "https" ||
		parsed.Host != "s3.frux.example.com:18443" ||
		parsed.Path != "/frux-media/uploads/video/test.mp4" {
		t.Fatalf("presigned URL = %q", request.URL)
	}
	if request.Headers["Content-Type"] != "video/mp4" ||
		request.Headers["Cache-Control"] != "private, no-store" ||
		request.Headers["x-amz-meta-sha256"] != strings.Repeat("a", 64) {
		t.Fatalf("presigned headers = %+v", request.Headers)
	}
}
