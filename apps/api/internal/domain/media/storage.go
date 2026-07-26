package domainmedia

import (
	"context"
	"io"
	"time"
)

type ObjectMetadata struct {
	Key            string
	ContentType    string
	SizeBytes      int64
	ChecksumSHA256 string
	ETag           string
	LastModified   time.Time
}

type PresignedRequest struct {
	URL       string
	Method    string
	Headers   map[string]string
	ExpiresAt time.Time
}

type ResolvedDelivery struct {
	MediaURL        string
	CoverURL        string
	PlaybackSources []PlaybackSource
}

type MediaObjectStore interface {
	Put(ctx context.Context, key string, body io.Reader, sizeBytes int64, contentType, checksumSHA256 string) (*ObjectMetadata, error)
	Open(ctx context.Context, key string) (io.ReadCloser, *ObjectMetadata, error)
	Head(ctx context.Context, key string) (*ObjectMetadata, error)
	Delete(ctx context.Context, key string) error
	List(ctx context.Context, prefix string) ([]ObjectMetadata, error)
	PresignPut(ctx context.Context, key, contentType, checksumSHA256 string, sizeBytes int64, expiry time.Duration) (*PresignedRequest, error)
	PresignGet(ctx context.Context, key string, expiry time.Duration) (*PresignedRequest, error)
}

type MediaURLResolver interface {
	PublicURL(objectKey string) (string, error)
	ProtectedURL(ctx context.Context, objectKey string, expiry time.Duration) (string, time.Time, error)
}
