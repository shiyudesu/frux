package applicationmedia

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"time"

	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
	inframetrics "github.com/shiyudesu/frux/internal/infra/metrics"
)

const maxVideoUploadBytes int64 = 512 << 20
const maxCoverUploadBytes int64 = 20 << 20

var ErrCreateUploadSessionFailed = errors.New("failed to create upload session")
var ErrCompleteUploadSessionFailed = errors.New("failed to complete upload session")
var ErrUploadObjectMismatch = errors.New("uploaded object does not match session")
var ErrUploadFileTypeInvalid = errors.New("unsupported upload file type")
var ErrVideoUploadTooLarge = errors.New("video upload exceeds 512 MiB")
var ErrCoverUploadTooLarge = errors.New("cover upload exceeds 20 MiB")
var ErrDirectUploadUnavailable = errors.New("direct upload is unavailable")
var ErrDispatchProcessingFailed = errors.New("failed to dispatch media processing")

var allowedUploadExtensions = map[string]map[string]struct{}{
	domainmedia.AssetKindVideo: {
		".mp4": {}, ".mov": {}, ".webm": {},
	},
	domainmedia.AssetKindCover: {
		".jpg": {}, ".jpeg": {}, ".png": {}, ".webp": {},
	},
}

var allowedUploadContentTypes = map[string]map[string]struct{}{
	domainmedia.AssetKindVideo: {
		"video/mp4": {}, "video/quicktime": {}, "video/webm": {},
	},
	domainmedia.AssetKindCover: {
		"image/jpeg": {}, "image/png": {}, "image/webp": {},
	},
}

type ProcessingPublisher interface {
	PublishMediaProcessingRequested(ctx context.Context, event *ProcessingRequestedEvent) error
}

type Repository interface {
	CreateUploadSession(ctx context.Context, session *domainmedia.UploadSession) (*domainmedia.UploadSession, bool, error)
	FindUploadSessionByOwnerAndIdempotencyKey(ctx context.Context, ownerID int64, idempotencyKey string) (*domainmedia.UploadSession, error)
	FindUploadSession(ctx context.Context, sessionID string) (*domainmedia.UploadSession, error)
	CompleteUploadSession(ctx context.Context, sessionID string, asset *domainmedia.MediaAsset, completedAt time.Time) (*domainmedia.UploadSession, *domainmedia.MediaAsset, bool, error)
	CreateOrGetProcessingJob(ctx context.Context, job *domainmedia.MediaProcessingJob) (*domainmedia.MediaProcessingJob, bool, error)
	FindAssetByID(ctx context.Context, assetID int64) (*domainmedia.MediaAsset, error)
	UpdateAsset(ctx context.Context, asset *domainmedia.MediaAsset) error
	UpsertVariants(ctx context.Context, variants []*domainmedia.MediaVariant) error
	RenewExpiredUploadSession(ctx context.Context, expiredSessionID string, replacement *domainmedia.UploadSession) (*domainmedia.UploadSession, error)
}

type Service struct {
	repo           Repository
	store          domainmedia.MediaObjectStore
	publisher      ProcessingPublisher
	backend        string
	sessionTTL     time.Duration
	profileVersion string
	maxAttempts    int
	now            func() time.Time
	newID          func() (string, error)
	urlResolver    domainmedia.MediaURLResolver
	signedURLTTL   time.Duration
	authorizer     MediaAssetAuthorizer
}

type Option func(*Service)

type CreateUploadSessionInput struct {
	OwnerID        int64
	Kind           string
	Filename       string
	ContentType    string
	SizeBytes      int64
	ChecksumSHA256 string
	IdempotencyKey string
}

type CreateUploadSessionResult struct {
	Mode             string
	Session          *domainmedia.UploadSession
	UploadRequest    *domainmedia.PresignedRequest
	CompletedAssetID int64
	Replayed         bool
}

type CompleteUploadSessionResult struct {
	Session  *domainmedia.UploadSession
	Asset    *domainmedia.MediaAsset
	Replayed bool
}

type ProtectedAssetAccess struct {
	URL       string
	ExpiresAt time.Time
}

type MediaAssetAuthorizer interface {
	AuthorizeMediaAsset(ctx context.Context, assetID, ownerID int64) (referenced, allowed bool, err error)
}

type protectedVariantReader interface {
	ListReadyVariants(ctx context.Context, assetID int64) ([]*domainmedia.MediaVariant, error)
}

func New(repo Repository, store domainmedia.MediaObjectStore, backend string, sessionTTL time.Duration, profileVersion string, maxAttempts int, options ...Option) *Service {
	service := &Service{
		repo: repo, store: store, backend: strings.ToLower(strings.TrimSpace(backend)),
		sessionTTL: sessionTTL, profileVersion: strings.TrimSpace(profileVersion), maxAttempts: maxAttempts,
		now: func() time.Time { return time.Now().UTC() }, newID: randomID,
	}
	for _, option := range options {
		option(service)
	}
	return service
}

func WithProcessingPublisher(publisher ProcessingPublisher) Option {
	return func(service *Service) {
		service.publisher = publisher
	}
}

func WithNow(now func() time.Time) Option {
	return func(service *Service) {
		if now != nil {
			service.now = now
		}
	}
}

func WithIDGenerator(generator func() (string, error)) Option {
	return func(service *Service) {
		if generator != nil {
			service.newID = generator
		}
	}
}

func WithURLResolver(resolver domainmedia.MediaURLResolver, signedURLTTL time.Duration) Option {
	return func(service *Service) {
		service.urlResolver = resolver
		service.signedURLTTL = signedURLTTL
	}
}

func WithMediaAssetAuthorizer(authorizer MediaAssetAuthorizer) Option {
	return func(service *Service) {
		service.authorizer = authorizer
	}
}

func (s *Service) CreateUploadSession(ctx context.Context, input CreateUploadSessionInput) (*CreateUploadSessionResult, error) {
	if s.backend == domainmedia.StorageBackendLocal {
		return &CreateUploadSessionResult{Mode: "multipart"}, nil
	}
	if s.backend != domainmedia.StorageBackendS3 || s.repo == nil || s.store == nil {
		return nil, ErrDirectUploadUnavailable
	}
	if err := validateUploadIntentBase(input); err != nil {
		return nil, err
	}
	fingerprint, err := uploadFingerprint(input)
	if err != nil {
		return nil, ErrCreateUploadSessionFailed
	}
	now := s.now()
	if idempotencyKey := strings.TrimSpace(input.IdempotencyKey); idempotencyKey != "" {
		stored, findErr := s.repo.FindUploadSessionByOwnerAndIdempotencyKey(ctx, input.OwnerID, idempotencyKey)
		switch {
		case findErr == nil:
			if stored.RequestFingerprint != fingerprint {
				return nil, domainmedia.ErrUploadSessionConflict
			}
			if stored.State == domainmedia.UploadSessionStateCompleted {
				return &CreateUploadSessionResult{
					Mode: "direct", Session: stored, CompletedAssetID: stored.CompletedAssetID, Replayed: true,
				}, nil
			}
			if stored.State == domainmedia.UploadSessionStatePending && stored.ExpiresAt.After(now) {
				request, requestErr := s.store.PresignPut(
					ctx, stored.ObjectKey, stored.ContentType, stored.ChecksumSHA256,
					stored.SizeBytes, stored.ExpiresAt.Sub(now),
				)
				if requestErr != nil {
					return nil, ErrCreateUploadSessionFailed
				}
				return &CreateUploadSessionResult{
					Mode: "direct", Session: stored, UploadRequest: request, Replayed: true,
				}, nil
			}
		case !errors.Is(findErr, domainmedia.ErrUploadSessionNotFound):
			return nil, findErr
		}
	}
	if err := validateUploadFileConstraints(input); err != nil {
		return nil, err
	}
	sessionID, err := s.newID()
	if err != nil {
		return nil, ErrCreateUploadSessionFailed
	}
	objectKey, err := domainmedia.BuildUploadObjectKey(input.OwnerID, sessionID, input.Kind, filepath.Ext(strings.TrimSpace(input.Filename)))
	if err != nil {
		return nil, err
	}
	session, err := domainmedia.NewUploadSession(
		sessionID, input.OwnerID, input.Kind, s.backend, objectKey, input.ContentType,
		input.SizeBytes, input.ChecksumSHA256, input.IdempotencyKey, fingerprint, now.Add(s.sessionTTL), now,
	)
	if err != nil {
		return nil, err
	}
	stored, created, err := s.repo.CreateUploadSession(ctx, session)
	if err != nil {
		return nil, err
	}
	if stored.State == domainmedia.UploadSessionStateExpired ||
		(stored.State == domainmedia.UploadSessionStatePending && !stored.ExpiresAt.After(now)) {
		replacementID, idErr := s.newID()
		if idErr != nil {
			return nil, ErrCreateUploadSessionFailed
		}
		replacementKey, keyErr := domainmedia.BuildUploadObjectKey(input.OwnerID, replacementID, input.Kind, filepath.Ext(strings.TrimSpace(input.Filename)))
		if keyErr != nil {
			return nil, keyErr
		}
		replacement, createErr := domainmedia.NewUploadSession(
			replacementID, input.OwnerID, input.Kind, s.backend, replacementKey, input.ContentType,
			input.SizeBytes, input.ChecksumSHA256, input.IdempotencyKey, fingerprint, now.Add(s.sessionTTL), now,
		)
		if createErr != nil {
			return nil, createErr
		}
		oldKey := stored.ObjectKey
		stored, err = s.repo.RenewExpiredUploadSession(ctx, stored.ID, replacement)
		if err != nil {
			return nil, err
		}
		_ = s.store.Delete(ctx, oldKey)
		created = true
	}
	if stored.State == domainmedia.UploadSessionStateCompleted {
		return &CreateUploadSessionResult{
			Mode: "direct", Session: stored, CompletedAssetID: stored.CompletedAssetID, Replayed: true,
		}, nil
	}
	request, err := s.store.PresignPut(ctx, stored.ObjectKey, stored.ContentType, stored.ChecksumSHA256, stored.SizeBytes, stored.ExpiresAt.Sub(now))
	if err != nil {
		return nil, ErrCreateUploadSessionFailed
	}
	return &CreateUploadSessionResult{
		Mode: "direct", Session: stored, UploadRequest: request, Replayed: !created,
	}, nil
}

func (s *Service) CompleteUploadSession(ctx context.Context, ownerID int64, sessionID string) (*CompleteUploadSessionResult, error) {
	if ownerID <= 0 {
		return nil, domainmedia.ErrInvalidOwnerID
	}
	session, err := s.repo.FindUploadSession(ctx, strings.TrimSpace(sessionID))
	if err != nil {
		return nil, err
	}
	if session.OwnerID != ownerID {
		return nil, domainmedia.ErrUploadSessionConflict
	}
	now := s.now()
	if session.State == domainmedia.UploadSessionStatePending && !session.ExpiresAt.After(now) {
		return nil, domainmedia.ErrUploadSessionExpired
	}
	metadata, err := s.store.Head(ctx, session.ObjectKey)
	if err != nil {
		if errors.Is(err, domainmedia.ErrObjectNotFound) {
			return nil, ErrUploadObjectMismatch
		}
		return nil, ErrCompleteUploadSessionFailed
	}
	if metadata.SizeBytes != session.SizeBytes ||
		!strings.EqualFold(metadata.ChecksumSHA256, session.ChecksumSHA256) ||
		(metadata.ContentType != "" && !sameContentType(metadata.ContentType, session.ContentType)) {
		return nil, ErrUploadObjectMismatch
	}
	asset, err := domainmedia.NewMediaAsset(
		session.OwnerID, session.Kind, session.StorageBackend, session.ObjectKey,
		session.ContentType, session.SizeBytes, session.ChecksumSHA256,
	)
	if err != nil {
		return nil, err
	}
	completed, storedAsset, replayed, err := s.repo.CompleteUploadSession(ctx, session.ID, asset, now)
	if err != nil {
		return nil, err
	}
	if session.Kind == domainmedia.AssetKindCover {
		if err := s.publishCover(ctx, storedAsset); err != nil {
			return nil, ErrCompleteUploadSessionFailed
		}
	} else if session.Kind == domainmedia.AssetKindVideo {
		job, err := domainmedia.NewProcessingJob(storedAsset.ID, s.profileVersion, s.maxAttempts, now)
		if err != nil {
			return nil, err
		}
		if _, _, err := s.repo.CreateOrGetProcessingJob(ctx, job); err != nil {
			return nil, ErrCompleteUploadSessionFailed
		}
		if s.publisher != nil {
			if err := s.publisher.PublishMediaProcessingRequested(ctx, NewProcessingRequestedEvent(storedAsset.ID, s.profileVersion, now)); err != nil {
				inframetrics.ObserveMediaWakeup("publish_failed")
			}
		}
	}
	return &CompleteUploadSessionResult{Session: completed, Asset: storedAsset, Replayed: replayed}, nil
}

func (s *Service) publishCover(ctx context.Context, asset *domainmedia.MediaAsset) error {
	if asset == nil {
		return domainmedia.ErrMediaAssetNotFound
	}
	extension := strings.ToLower(filepath.Ext(asset.ObjectKey))
	if extension == "" || len(extension) > 8 {
		extension = ".jpg"
	}
	if err := s.repo.UpsertVariants(ctx, []*domainmedia.MediaVariant{{
		AssetID: asset.ID, ProfileVersion: s.profileVersion, SourceType: domainmedia.SourceTypeImage,
		Format: strings.TrimPrefix(extension, "."), ObjectKey: asset.ObjectKey, Role: domainmedia.VariantRoleCover,
		SortOrder: 5, State: domainmedia.VariantStateReady, ChecksumSHA256: asset.ChecksumSHA256,
		SizeBytes: asset.SizeBytes, Public: false,
	}}); err != nil {
		return err
	}
	asset.State = domainmedia.AssetStateReady
	asset.ErrorCode = ""
	return s.repo.UpdateAsset(ctx, asset)
}

func (s *Service) GetProtectedAssetAccess(ctx context.Context, ownerID, assetID int64) (*ProtectedAssetAccess, error) {
	if ownerID <= 0 {
		return nil, domainmedia.ErrInvalidOwnerID
	}
	if assetID <= 0 {
		return nil, domainmedia.ErrInvalidAssetID
	}
	if s.urlResolver == nil || s.signedURLTTL <= 0 {
		return nil, ErrDirectUploadUnavailable
	}
	asset, err := s.repo.FindAssetByID(ctx, assetID)
	if err != nil {
		return nil, err
	}
	if asset.OwnerID != ownerID {
		return nil, domainmedia.ErrMediaAssetPermissionDenied
	}
	if asset.State == domainmedia.AssetStateDeleted {
		return nil, domainmedia.ErrMediaAssetNotFound
	}
	if s.authorizer != nil {
		referenced, allowed, err := s.authorizer.AuthorizeMediaAsset(ctx, assetID, ownerID)
		if err != nil {
			return nil, err
		}
		if referenced && !allowed {
			return nil, domainmedia.ErrMediaAssetPermissionDenied
		}
	}
	objectKey := asset.ObjectKey
	if asset.State == domainmedia.AssetStateReady {
		if variants, ok := s.repo.(protectedVariantReader); ok {
			ready, err := variants.ListReadyVariants(ctx, asset.ID)
			if err != nil {
				return nil, err
			}
			if previewKey := protectedPreviewVariantKey(asset.Kind, ready); previewKey != "" {
				objectKey = previewKey
			}
		}
	}
	resolvedURL, expiresAt, err := s.urlResolver.ProtectedURL(ctx, objectKey, s.signedURLTTL)
	if err != nil {
		return nil, err
	}
	return &ProtectedAssetAccess{URL: resolvedURL, ExpiresAt: expiresAt}, nil
}

func protectedPreviewVariantKey(
	assetKind string,
	variants []*domainmedia.MediaVariant,
) string {
	role := domainmedia.VariantRoleBaseline
	if assetKind == domainmedia.AssetKindCover {
		role = domainmedia.VariantRoleCover
	}
	for _, variant := range variants {
		if variant != nil &&
			variant.State == domainmedia.VariantStateReady &&
			variant.Role == role &&
			strings.TrimSpace(variant.ObjectKey) != "" {
			return variant.ObjectKey
		}
	}
	return ""
}

func validateUploadIntentBase(input CreateUploadSessionInput) error {
	if input.OwnerID <= 0 {
		return domainmedia.ErrInvalidOwnerID
	}
	kind := strings.ToLower(strings.TrimSpace(input.Kind))
	if !domainmedia.ValidAssetKind(kind) {
		return domainmedia.ErrInvalidAssetKind
	}
	filename := strings.TrimSpace(input.Filename)
	if filename == "" {
		return domainmedia.ErrInvalidObjectKey
	}
	if input.SizeBytes <= 0 {
		return domainmedia.ErrInvalidSize
	}
	_, err := domainmedia.NewMediaAsset(
		input.OwnerID, input.Kind, domainmedia.StorageBackendS3,
		"validation/source.bin", input.ContentType, input.SizeBytes, input.ChecksumSHA256,
	)
	return err
}

func validateUploadFileConstraints(input CreateUploadSessionInput) error {
	kind := strings.ToLower(strings.TrimSpace(input.Kind))
	filename := strings.TrimSpace(input.Filename)
	extension := strings.ToLower(filepath.Ext(filepath.Base(filename)))
	if _, ok := allowedUploadExtensions[kind][extension]; !ok {
		return ErrUploadFileTypeInvalid
	}
	contentType := strings.ToLower(strings.TrimSpace(strings.Split(input.ContentType, ";")[0]))
	if _, ok := allowedUploadContentTypes[kind][contentType]; !ok {
		return ErrUploadFileTypeInvalid
	}
	switch kind {
	case domainmedia.AssetKindVideo:
		if input.SizeBytes > maxVideoUploadBytes {
			return ErrVideoUploadTooLarge
		}
	case domainmedia.AssetKindCover:
		if input.SizeBytes > maxCoverUploadBytes {
			return ErrCoverUploadTooLarge
		}
	}
	return nil
}

func uploadFingerprint(input CreateUploadSessionInput) (string, error) {
	payload, err := json.Marshal(struct {
		OwnerID        int64  `json:"owner_id"`
		Kind           string `json:"kind"`
		Filename       string `json:"filename"`
		ContentType    string `json:"content_type"`
		SizeBytes      int64  `json:"size_bytes"`
		ChecksumSHA256 string `json:"checksum_sha256"`
	}{
		OwnerID: input.OwnerID, Kind: strings.ToLower(strings.TrimSpace(input.Kind)),
		Filename: strings.TrimSpace(input.Filename), ContentType: strings.ToLower(strings.TrimSpace(input.ContentType)),
		SizeBytes: input.SizeBytes, ChecksumSHA256: strings.ToLower(strings.TrimSpace(input.ChecksumSHA256)),
	})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

func randomID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func sameContentType(left, right string) bool {
	left = strings.ToLower(strings.TrimSpace(strings.Split(left, ";")[0]))
	right = strings.ToLower(strings.TrimSpace(strings.Split(right, ";")[0]))
	return left == right
}
