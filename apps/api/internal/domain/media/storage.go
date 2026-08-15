package domainmedia

import (
	"context"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"
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

type PublicMediaObject struct {
	StorageKey string
	VariantID  int64
	Generation string
}

func BuildPublicExposureKey(variant *MediaVariant) (string, error) {
	if variant == nil || variant.ID <= 0 ||
		strings.TrimSpace(variant.ExposureGeneration) == "" ||
		len(variant.ExposureGeneration) > 32 {
		return "", ErrInvalidObjectKey
	}
	filename := path.Base(strings.TrimSpace(variant.ObjectKey))
	if filename == "." || filename == "" || strings.ContainsAny(filename, `/\`) {
		return "", ErrInvalidObjectKey
	}
	key := fmt.Sprintf(
		"media/v3/%s/%d/%s",
		strings.TrimSpace(variant.ExposureGeneration), variant.ID, filename,
	)
	if !ValidObjectKey(key) {
		return "", ErrInvalidObjectKey
	}
	return key, nil
}

func ParsePublicExposureKey(key string) (generation string, variantID int64, filename string, ok bool) {
	parts := strings.Split(strings.TrimSpace(key), "/")
	if len(parts) != 5 || parts[0] != "media" || parts[1] != "v3" ||
		parts[2] == "" || len(parts[2]) > 32 ||
		parts[4] == "" || parts[4] == "." || parts[4] == ".." {
		return "", 0, "", false
	}
	value, err := strconv.ParseInt(parts[3], 10, 64)
	if err != nil || value <= 0 {
		return "", 0, "", false
	}
	return parts[2], value, parts[4], true
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
