package domainmedia

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	AssetKindVideo = "video"
	AssetKindCover = "cover"

	StorageBackendLocal = "local"
	StorageBackendS3    = "s3"

	AssetStatePending    = "pending"
	AssetStateUploaded   = "uploaded"
	AssetStateProcessing = "processing"
	AssetStateReady      = "ready"
	AssetStateFailed     = "failed"
	AssetStateDeleted    = "deleted"

	VariantStatePending = "pending"
	VariantStateReady   = "ready"
	VariantStateFailed  = "failed"

	JobStatePending    = "pending"
	JobStateProcessing = "processing"
	JobStateRetryable  = "retryable"
	JobStateCompleted  = "completed"
	JobStateFailed     = "failed"

	UploadSessionStatePending   = "pending"
	UploadSessionStateCompleted = "completed"
	UploadSessionStateExpired   = "expired"

	CleanupStatePending    = "pending"
	CleanupStateProcessing = "processing"
	CleanupStateCompleted  = "completed"
	CleanupStateFailed     = "failed"

	MediaStatusLegacyReady = "legacy_ready"
	MediaStatusPending     = "pending"
	MediaStatusProcessing  = "processing"
	MediaStatusReady       = "ready"
	MediaStatusFailed      = "failed"

	SourceTypeMP4   = "mp4"
	SourceTypeDASH  = "dash"
	SourceTypeImage = "image"

	VariantRoleOriginal  = "original"
	VariantRoleBaseline  = "baseline"
	VariantRoleRendition = "rendition"
	VariantRoleManifest  = "manifest"
	VariantRoleSegment   = "segment"
	VariantRoleCover     = "cover"

	MaxObjectKeyLength      = 1024
	MaxContentTypeLength    = 128
	MaxChecksumLength       = 64
	MaxIdempotencyKeyLength = 128
)

type MediaAsset struct {
	ID               int64
	OwnerID          int64
	Kind             string
	StorageBackend   string
	ObjectKey        string
	ContentType      string
	SizeBytes        int64
	ChecksumSHA256   string
	Width            int
	Height           int
	DurationMS       int64
	VideoCodec       string
	AudioCodec       string
	State            string
	ErrorCode        string
	CreatedAt        time.Time
	UpdatedAt        time.Time
	LastReconciledAt *time.Time
}

type MediaVariant struct {
	ID             int64
	AssetID        int64
	VideoID        int64
	ProfileVersion string
	SourceType     string
	Format         string
	Codec          string
	AudioCodec     string
	Width          int
	Height         int
	Bitrate        int
	Quality        string
	ObjectKey      string
	Role           string
	SortOrder      int
	State          string
	ChecksumSHA256 string
	SizeBytes      int64
	Public         bool
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type ProcessingProfile struct {
	Version    string
	Name       string
	ConfigJSON string
	Active     bool
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type MediaProcessingJob struct {
	ID             int64
	AssetID        int64
	ProfileVersion string
	State          string
	Attempts       int
	MaxAttempts    int
	ErrorCode      string
	ErrorMessage   string
	LeaseOwner     string
	LeaseUntil     *time.Time
	NextAttemptAt  time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
	CompletedAt    *time.Time
}

type UploadSession struct {
	ID                 string
	OwnerID            int64
	Kind               string
	StorageBackend     string
	ObjectKey          string
	ContentType        string
	SizeBytes          int64
	ChecksumSHA256     string
	State              string
	IdempotencyKey     string
	RequestFingerprint string
	ExpiresAt          time.Time
	CompletedAssetID   int64
	CompletedAt        *time.Time
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type CleanupTask struct {
	ID             int64
	AssetID        int64
	StorageBackend string
	ObjectKey      string
	State          string
	Attempts       int
	MaxAttempts    int
	ErrorMessage   string
	NotBefore      time.Time
	LeaseOwner     string
	LeaseUntil     *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
	CompletedAt    *time.Time
}

type PlaybackSource struct {
	Type       string `json:"type"`
	URL        string `json:"url"`
	Codec      string `json:"codec,omitempty"`
	AudioCodec string `json:"audio_codec,omitempty"`
	Width      int    `json:"width,omitempty"`
	Height     int    `json:"height,omitempty"`
	Bitrate    int    `json:"bitrate,omitempty"`
	Quality    string `json:"quality,omitempty"`
	Role       string `json:"role,omitempty"`
	SortOrder  int    `json:"-"`
}

func NewMediaAsset(ownerID int64, kind, backend, objectKey, contentType string, sizeBytes int64, checksum string) (*MediaAsset, error) {
	kind = normalizeKind(kind)
	backend = normalizeBackend(backend)
	objectKey = strings.TrimSpace(objectKey)
	contentType = strings.ToLower(strings.TrimSpace(contentType))
	checksum = strings.ToLower(strings.TrimSpace(checksum))
	if ownerID <= 0 {
		return nil, ErrInvalidOwnerID
	}
	if !ValidAssetKind(kind) {
		return nil, ErrInvalidAssetKind
	}
	if !ValidStorageBackend(backend) {
		return nil, ErrInvalidStorageBackend
	}
	if !ValidObjectKey(objectKey) {
		return nil, ErrInvalidObjectKey
	}
	if contentType == "" || len(contentType) > MaxContentTypeLength {
		return nil, ErrInvalidContentType
	}
	if sizeBytes <= 0 {
		return nil, ErrInvalidSize
	}
	if !validSHA256(checksum) {
		return nil, ErrInvalidChecksum
	}
	return &MediaAsset{
		OwnerID: ownerID, Kind: kind, StorageBackend: backend, ObjectKey: objectKey,
		ContentType: contentType, SizeBytes: sizeBytes, ChecksumSHA256: checksum, State: AssetStateUploaded,
	}, nil
}

func BuildUploadObjectKey(ownerID int64, sessionID, kind, extension string) (string, error) {
	sessionID = strings.TrimSpace(sessionID)
	kind = normalizeKind(kind)
	extension = strings.ToLower(strings.TrimSpace(extension))
	if ownerID <= 0 {
		return "", ErrInvalidOwnerID
	}
	if sessionID == "" || !ValidAssetKind(kind) {
		return "", ErrInvalidObjectKey
	}
	if extension == "" {
		extension = ".bin"
	}
	if !strings.HasPrefix(extension, ".") {
		extension = "." + extension
	}
	if len(extension) > 16 || strings.ContainsAny(extension, `/\`) {
		return "", ErrInvalidObjectKey
	}
	key := fmt.Sprintf("uploads/%d/%s/%s/source%s", ownerID, sessionID, kind, extension)
	if !ValidObjectKey(key) {
		return "", ErrInvalidObjectKey
	}
	return key, nil
}

func NewUploadSession(id string, ownerID int64, kind, backend, objectKey, contentType string, sizeBytes int64, checksum, idempotencyKey, fingerprint string, expiresAt, now time.Time) (*UploadSession, error) {
	id = strings.TrimSpace(id)
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	fingerprint = strings.TrimSpace(fingerprint)
	if id == "" || ownerID <= 0 || len(idempotencyKey) > MaxIdempotencyKeyLength || fingerprint == "" || !expiresAt.After(now) {
		return nil, ErrInvalidUploadSession
	}
	asset, err := NewMediaAsset(ownerID, kind, backend, objectKey, contentType, sizeBytes, checksum)
	if err != nil {
		return nil, err
	}
	return &UploadSession{
		ID: id, OwnerID: ownerID, Kind: asset.Kind, StorageBackend: asset.StorageBackend,
		ObjectKey: asset.ObjectKey, ContentType: asset.ContentType, SizeBytes: asset.SizeBytes,
		ChecksumSHA256: asset.ChecksumSHA256, State: UploadSessionStatePending,
		IdempotencyKey: idempotencyKey, RequestFingerprint: fingerprint, ExpiresAt: expiresAt,
	}, nil
}

func NewProcessingJob(assetID int64, profileVersion string, maxAttempts int, now time.Time) (*MediaProcessingJob, error) {
	profileVersion = strings.TrimSpace(profileVersion)
	if assetID <= 0 || profileVersion == "" || maxAttempts <= 0 {
		return nil, ErrInvalidProfile
	}
	return &MediaProcessingJob{
		AssetID: assetID, ProfileVersion: profileVersion, State: JobStatePending,
		MaxAttempts: maxAttempts, NextAttemptAt: now,
	}, nil
}

func NewCleanupTask(assetID int64, backend, objectKey string, notBefore time.Time, maxAttempts int) (*CleanupTask, error) {
	backend = normalizeBackend(backend)
	objectKey = strings.TrimSpace(objectKey)
	if assetID < 0 || !ValidStorageBackend(backend) || !ValidObjectKey(objectKey) || maxAttempts <= 0 {
		return nil, ErrInvalidObjectKey
	}
	return &CleanupTask{
		AssetID: assetID, StorageBackend: backend, ObjectKey: objectKey,
		State: CleanupStatePending, NotBefore: notBefore, MaxAttempts: maxAttempts,
	}, nil
}

func SortPlaybackSources(sources []PlaybackSource) []PlaybackSource {
	result := append([]PlaybackSource(nil), sources...)
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].SortOrder != result[j].SortOrder {
			return result[i].SortOrder < result[j].SortOrder
		}
		if result[i].Type != result[j].Type {
			return result[i].Type < result[j].Type
		}
		if result[i].Bitrate != result[j].Bitrate {
			return result[i].Bitrate < result[j].Bitrate
		}
		return result[i].URL < result[j].URL
	})
	return result
}

func ValidAssetKind(value string) bool {
	switch normalizeKind(value) {
	case AssetKindVideo, AssetKindCover:
		return true
	default:
		return false
	}
}

func ValidStorageBackend(value string) bool {
	switch normalizeBackend(value) {
	case StorageBackendLocal, StorageBackendS3:
		return true
	default:
		return false
	}
}

func ValidMediaStatus(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case MediaStatusLegacyReady, MediaStatusPending, MediaStatusProcessing, MediaStatusReady, MediaStatusFailed:
		return true
	default:
		return false
	}
}

func ValidObjectKey(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > MaxObjectKeyLength || strings.HasPrefix(value, "/") || strings.Contains(value, `\`) {
		return false
	}
	cleaned := filepath.ToSlash(filepath.Clean(value))
	return cleaned == value && cleaned != "." && !strings.HasPrefix(cleaned, "../") && !strings.Contains(cleaned, "/../")
}

func IsPublicReadyStatus(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	return value == "" || value == MediaStatusLegacyReady || value == MediaStatusReady
}

func normalizeKind(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func normalizeBackend(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func validSHA256(value string) bool {
	if len(value) != MaxChecksumLength {
		return false
	}
	for _, char := range value {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}
