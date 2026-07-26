package inframedia

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	domainmedia "GCFeed/internal/domain/media"
)

func TestLocalStoreLifecycleAndChecksum(t *testing.T) {
	store, err := NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatalf("new local store: %v", err)
	}
	content := []byte("gcfeed media")
	sum := sha256.Sum256(content)
	checksum := hex.EncodeToString(sum[:])
	key := "uploads/9/session/video/source.mp4"

	metadata, err := store.Put(context.Background(), key, bytes.NewReader(content), int64(len(content)), "video/mp4", checksum)
	if err != nil {
		t.Fatalf("put local object: %v", err)
	}
	if metadata.ChecksumSHA256 != checksum || metadata.SizeBytes != int64(len(content)) {
		t.Fatalf("unexpected metadata: %+v", metadata)
	}
	reader, opened, err := store.Open(context.Background(), key)
	if err != nil {
		t.Fatalf("open local object: %v", err)
	}
	defer reader.Close()
	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read local object: %v", err)
	}
	if !bytes.Equal(got, content) || opened.ETag != checksum {
		t.Fatalf("unexpected local object: %q %+v", got, opened)
	}
	if _, err := store.Put(context.Background(), "uploads/9/bad.mp4", bytes.NewReader(content), int64(len(content)), "video/mp4", strings.Repeat("0", 64)); !errors.Is(err, domainmedia.ErrObjectChecksumMismatch) {
		t.Fatalf("expected checksum mismatch, got %v", err)
	}
	if _, err := store.Head(context.Background(), "../escape"); !errors.Is(err, domainmedia.ErrInvalidObjectKey) {
		t.Fatalf("expected invalid key, got %v", err)
	}
	if err := store.Delete(context.Background(), key); err != nil {
		t.Fatalf("delete local object: %v", err)
	}
	if _, err := store.Head(context.Background(), key); !errors.Is(err, domainmedia.ErrObjectNotFound) {
		t.Fatalf("expected deleted object to be missing, got %v", err)
	}
}

func TestURLResolverPublicAndProtectedURLs(t *testing.T) {
	store := &presignStore{}
	resolver, err := NewURLResolver("https://cdn.example.test/media", store)
	if err != nil {
		t.Fatalf("new URL resolver: %v", err)
	}
	publicURL, err := resolver.PublicURL("variants/1/baseline.mp4")
	if err != nil {
		t.Fatalf("resolve public URL: %v", err)
	}
	if publicURL != "https://cdn.example.test/media/variants/1/baseline.mp4" {
		t.Fatalf("unexpected public URL %q", publicURL)
	}
	protectedURL, expiresAt, err := resolver.ProtectedURL(context.Background(), "originals/1/source.mp4", 5*time.Minute)
	if err != nil {
		t.Fatalf("resolve protected URL: %v", err)
	}
	if protectedURL != "https://signed.example.test/originals/1/source.mp4" || expiresAt.IsZero() {
		t.Fatalf("unexpected protected URL %q %v", protectedURL, expiresAt)
	}
}

type presignStore struct{}

func (*presignStore) Put(context.Context, string, io.Reader, int64, string, string) (*domainmedia.ObjectMetadata, error) {
	return nil, nil
}

func (*presignStore) Open(context.Context, string) (io.ReadCloser, *domainmedia.ObjectMetadata, error) {
	return nil, nil, nil
}

func (*presignStore) Head(context.Context, string) (*domainmedia.ObjectMetadata, error) {
	return nil, nil
}

func (*presignStore) Delete(context.Context, string) error {
	return nil
}

func (*presignStore) List(context.Context, string) ([]domainmedia.ObjectMetadata, error) {
	return nil, nil
}

func (*presignStore) PresignPut(context.Context, string, string, string, int64, time.Duration) (*domainmedia.PresignedRequest, error) {
	return nil, nil
}

func (*presignStore) PresignGet(_ context.Context, key string, expiry time.Duration) (*domainmedia.PresignedRequest, error) {
	return &domainmedia.PresignedRequest{
		URL: "https://signed.example.test/" + key, Method: "GET", ExpiresAt: time.Now().UTC().Add(expiry),
	}, nil
}
