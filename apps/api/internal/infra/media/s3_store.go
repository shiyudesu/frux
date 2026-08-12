package inframedia

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"strings"
	"time"

	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
	infraconfig "github.com/shiyudesu/frux/internal/infra/config"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

type S3Store struct {
	bucket  string
	client  *s3.Client
	presign *s3.PresignClient
}

func NewS3Store(ctx context.Context, cfg infraconfig.S3Config) (*S3Store, error) {
	options := []func(*awsconfig.LoadOptions) error{awsconfig.WithRegion(strings.TrimSpace(cfg.Region))}
	if strings.TrimSpace(cfg.AccessKey) != "" {
		options = append(options, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, ""),
		))
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, options...)
	if err != nil {
		return nil, err
	}
	client := s3.NewFromConfig(awsCfg, func(options *s3.Options) {
		options.UsePathStyle = cfg.UsePathStyle
		if endpoint := strings.TrimRight(strings.TrimSpace(cfg.Endpoint), "/"); endpoint != "" {
			options.BaseEndpoint = aws.String(endpoint)
		}
	})
	presignClient := client
	if endpoint := strings.TrimRight(strings.TrimSpace(cfg.PresignEndpoint), "/"); endpoint != "" && endpoint != strings.TrimRight(strings.TrimSpace(cfg.Endpoint), "/") {
		presignClient = s3.NewFromConfig(awsCfg, func(options *s3.Options) {
			options.UsePathStyle = cfg.UsePathStyle
			options.BaseEndpoint = aws.String(endpoint)
		})
	}
	store := &S3Store{
		bucket:  strings.TrimSpace(cfg.Bucket),
		client:  client,
		presign: s3.NewPresignClient(presignClient),
	}
	if store.bucket == "" {
		return nil, infraconfig.ErrInvalidMediaConfig
	}
	if cfg.AutoCreateBucket {
		if err := store.ensureBucket(ctx); err != nil {
			return nil, err
		}
	}
	return store, nil
}

func (s *S3Store) Put(ctx context.Context, key string, body io.Reader, sizeBytes int64, contentType, checksumSHA256 string) (*domainmedia.ObjectMetadata, error) {
	if !domainmedia.ValidObjectKey(key) {
		return nil, domainmedia.ErrInvalidObjectKey
	}
	checksum, err := checksumBase64(checksumSHA256)
	if err != nil {
		return nil, err
	}
	seekableBody, cleanup, err := seekableUploadBody(body, sizeBytes)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	_, err = s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.bucket), Key: aws.String(key), Body: seekableBody,
		ContentLength: aws.Int64(sizeBytes), ContentType: aws.String(contentType),
		ChecksumSHA256: aws.String(checksum),
		CacheControl:   aws.String(cacheControlForObjectKey(key)),
		Metadata:       map[string]string{"sha256": strings.ToLower(checksumSHA256)},
	})
	if err != nil {
		return nil, err
	}

	return s.Head(ctx, key)
}

func seekableUploadBody(body io.Reader, sizeBytes int64) (io.ReadSeeker, func(), error) {
	if seeker, ok := body.(io.ReadSeeker); ok {
		if _, err := seeker.Seek(0, io.SeekStart); err != nil {
			return nil, func() {}, err
		}
		return seeker, func() {}, nil
	}
	temp, err := os.CreateTemp("", "frux-s3-upload-*")
	if err != nil {
		return nil, func() {}, err
	}
	cleanup := func() {
		_ = temp.Close()
		_ = os.Remove(temp.Name())
	}
	written, err := io.Copy(temp, io.LimitReader(body, sizeBytes+1))
	if err != nil {
		cleanup()
		return nil, func() {}, err
	}
	if written != sizeBytes {
		cleanup()
		return nil, func() {}, domainmedia.ErrInvalidSize
	}
	if _, err := temp.Seek(0, io.SeekStart); err != nil {
		cleanup()
		return nil, func() {}, err
	}
	return temp, cleanup, nil
}

func (s *S3Store) Open(ctx context.Context, key string) (io.ReadCloser, *domainmedia.ObjectMetadata, error) {
	if !domainmedia.ValidObjectKey(key) {
		return nil, nil, domainmedia.ErrInvalidObjectKey
	}
	output, err := s.client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	if err != nil {
		return nil, nil, mapS3Error(err)
	}
	metadata := metadataFromS3(key, output.ContentType, output.ContentLength, output.ETag, output.LastModified, output.Metadata)
	return output.Body, metadata, nil
}

func (s *S3Store) Head(ctx context.Context, key string) (*domainmedia.ObjectMetadata, error) {
	if !domainmedia.ValidObjectKey(key) {
		return nil, domainmedia.ErrInvalidObjectKey
	}
	output, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	if err != nil {
		return nil, mapS3Error(err)
	}
	return metadataFromS3(key, output.ContentType, output.ContentLength, output.ETag, output.LastModified, output.Metadata), nil
}

func (s *S3Store) Delete(ctx context.Context, key string) error {
	if !domainmedia.ValidObjectKey(key) {
		return domainmedia.ErrInvalidObjectKey
	}
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	return err
}

func (s *S3Store) List(ctx context.Context, prefix string) ([]domainmedia.ObjectMetadata, error) {
	prefix = strings.TrimSpace(strings.TrimPrefix(prefix, "/"))
	if prefix != "" && !domainmedia.ValidObjectKey(prefix) {
		return nil, domainmedia.ErrInvalidObjectKey
	}
	paginator := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(s.bucket), Prefix: aws.String(prefix),
	})
	result := []domainmedia.ObjectMetadata{}
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, object := range page.Contents {
			result = append(result, domainmedia.ObjectMetadata{
				Key: aws.ToString(object.Key), SizeBytes: aws.ToInt64(object.Size),
				ETag: strings.Trim(aws.ToString(object.ETag), `"`), LastModified: aws.ToTime(object.LastModified).UTC(),
			})
		}
	}
	return result, nil
}

func (s *S3Store) PresignPut(ctx context.Context, key, contentType, checksumSHA256 string, sizeBytes int64, expiry time.Duration) (*domainmedia.PresignedRequest, error) {
	if !domainmedia.ValidObjectKey(key) || sizeBytes <= 0 {
		return nil, domainmedia.ErrInvalidObjectKey
	}
	if expiry <= 0 {
		return nil, domainmedia.ErrInvalidPresignExpiry
	}
	checksum, err := checksumBase64(checksumSHA256)
	if err != nil {
		return nil, err
	}
	output, err := s.presign.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.bucket), Key: aws.String(key), ContentLength: aws.Int64(sizeBytes),
		ContentType: aws.String(contentType), ChecksumSHA256: aws.String(checksum),
		CacheControl: aws.String("private, no-store"),
		Metadata:     map[string]string{"sha256": strings.ToLower(checksumSHA256)},
	}, func(options *s3.PresignOptions) {
		options.Expires = expiry
	})
	if err != nil {
		return nil, err
	}
	headers := map[string]string{
		"Content-Type": contentType, "Cache-Control": "private, no-store",
		"x-amz-checksum-sha256": checksum, "x-amz-meta-sha256": strings.ToLower(checksumSHA256),
	}
	return &domainmedia.PresignedRequest{
		URL: output.URL, Method: output.Method, Headers: headers, ExpiresAt: time.Now().UTC().Add(expiry),
	}, nil
}

func cacheControlForObjectKey(key string) string {
	if strings.HasPrefix(key, "media/") {
		return "public, max-age=60, must-revalidate"
	}
	return "private, no-store"
}

func (s *S3Store) PresignGet(ctx context.Context, key string, expiry time.Duration) (*domainmedia.PresignedRequest, error) {
	if !domainmedia.ValidObjectKey(key) {
		return nil, domainmedia.ErrInvalidObjectKey
	}
	if expiry <= 0 {
		return nil, domainmedia.ErrInvalidPresignExpiry
	}
	output, err := s.presign.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket), Key: aws.String(key),
		ResponseCacheControl: aws.String("private, no-store"),
	}, func(options *s3.PresignOptions) {
		options.Expires = expiry
	})
	if err != nil {
		return nil, err
	}
	return &domainmedia.PresignedRequest{
		URL: output.URL, Method: output.Method, Headers: map[string]string{}, ExpiresAt: time.Now().UTC().Add(expiry),
	}, nil
}

func (s *S3Store) ensureBucket(ctx context.Context) error {
	if _, err := s.client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(s.bucket)}); err == nil {
		return nil
	}
	_, err := s.client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(s.bucket)})
	return err
}

func checksumBase64(checksum string) (string, error) {
	raw, err := hex.DecodeString(strings.TrimSpace(checksum))
	if err != nil || len(raw) != 32 {
		return "", domainmedia.ErrInvalidChecksum
	}
	return base64.StdEncoding.EncodeToString(raw), nil
}

func metadataFromS3(key string, contentType *string, size *int64, etag *string, modified *time.Time, values map[string]string) *domainmedia.ObjectMetadata {
	return &domainmedia.ObjectMetadata{
		Key: key, ContentType: aws.ToString(contentType), SizeBytes: aws.ToInt64(size),
		ChecksumSHA256: strings.ToLower(values["sha256"]), ETag: strings.Trim(aws.ToString(etag), `"`),
		LastModified: aws.ToTime(modified).UTC(),
	}
}

func mapS3Error(err error) error {
	var noSuchKey *s3types.NoSuchKey
	if errors.As(err, &noSuchKey) {
		return domainmedia.ErrObjectNotFound
	}
	var notFound *s3types.NotFound
	if errors.As(err, &notFound) {
		return domainmedia.ErrObjectNotFound
	}
	var apiError smithy.APIError
	if errors.As(err, &apiError) && (apiError.ErrorCode() == "NoSuchKey" || apiError.ErrorCode() == "NotFound") {
		return domainmedia.ErrObjectNotFound
	}
	return err
}
