package applicationvideo

import (
	"context"
	"errors"
	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
	domainvideo "github.com/shiyudesu/frux/internal/domain/video"
	"strings"
	"time"
)

var ErrLoadVideoFailed = errors.New("failed to load video")
var ErrSaveVideoFailed = errors.New("failed to save video")
var ErrUpdateVideoFailed = errors.New("failed to update video")

type Service struct {
	repo             domainvideo.Repository
	publisher        PublishedEventPublisher
	cacheInvalidator VideoCacheInvalidator
	localAssets      LocalAssetOwnership
	mediaAssets      MediaAssetReader
	mediaDelivery    MediaDeliveryResolver
	mediaCleanup     MediaCleanupScheduler
}

type PublishedEventPublisher interface {
	PublishVideoPublished(ctx context.Context, event *PublishedEvent) error
}

type VideoCacheInvalidator interface {
	InvalidateVideo(ctx context.Context, videoID int64) error
}

type LocalAssetOwnership interface {
	ValidateLocalAssetOwner(ctx context.Context, ownerID int64, assetURL, kind string) error
}

type MediaAssetReader interface {
	FindAssetByID(ctx context.Context, assetID int64) (*domainmedia.MediaAsset, error)
}

type MediaDeliveryResolver interface {
	ResolveVideo(ctx context.Context, videoID, mediaAssetID, coverAssetID int64) (*domainmedia.ResolvedDelivery, error)
	ProtectVideo(ctx context.Context, videoID, mediaAssetID, coverAssetID int64) error
	HasPublicVideo(ctx context.Context, videoID, mediaAssetID, coverAssetID int64) (bool, error)
}

type MediaCleanupScheduler interface {
	ScheduleMediaCleanup(ctx context.Context, mediaAssetID, coverAssetID int64) error
}

type Option func(*Service)

// CreateResult 同时表达“返回哪个视频”和“这次请求是否真的创建了新记录”。
type CreateResult struct {
	Video   *domainvideo.Video
	Created bool
}

func New(repo domainvideo.Repository, options ...Option) *Service {
	service := &Service{repo: repo}
	for _, option := range options {
		option(service)
	}
	return service
}

func WithPublishedEventPublisher(publisher PublishedEventPublisher) Option {
	return func(s *Service) {
		s.publisher = publisher
	}
}

func WithVideoCacheInvalidator(invalidator VideoCacheInvalidator) Option {
	return func(s *Service) {
		s.cacheInvalidator = invalidator
	}
}

func WithLocalAssetOwnership(ownership LocalAssetOwnership) Option {
	return func(s *Service) {
		s.localAssets = ownership
	}
}

func WithMediaAssets(assets MediaAssetReader) Option {
	return func(s *Service) {
		s.mediaAssets = assets
	}
}

func WithMediaDelivery(resolver MediaDeliveryResolver) Option {
	return func(s *Service) {
		s.mediaDelivery = resolver
	}
}

func WithMediaCleanup(scheduler MediaCleanupScheduler) Option {
	return func(s *Service) {
		s.mediaCleanup = scheduler
	}
}

func (s *Service) CreateWithAssets(ctx context.Context, authorID int64, title, description string, mediaAssetID, coverAssetID int64, idempotencyKey string) (*CreateResult, error) {
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if idempotencyKey != "" {
		existing, err := s.repo.FindByAuthorAndIdempotencyKey(ctx, authorID, idempotencyKey)
		if err == nil {
			return s.ensureReadyAssetPublication(ctx, existing, false)
		}
		if !errors.Is(err, domainvideo.ErrVideoNotFound) {
			return nil, ErrLoadVideoFailed
		}
	}
	if s.mediaAssets == nil {
		return nil, domainmedia.ErrMediaAssetNotFound
	}
	videoAsset, err := s.mediaAssets.FindAssetByID(ctx, mediaAssetID)
	if err != nil {
		return nil, err
	}
	coverAsset, err := s.mediaAssets.FindAssetByID(ctx, coverAssetID)
	if err != nil {
		return nil, err
	}
	if videoAsset.OwnerID != authorID || coverAsset.OwnerID != authorID {
		return nil, domainvideo.ErrVideoPermissionDenied
	}
	if videoAsset.Kind != domainmedia.AssetKindVideo || coverAsset.Kind != domainmedia.AssetKindCover ||
		coverAsset.State != domainmedia.AssetStateReady || videoAsset.State == domainmedia.AssetStateDeleted {
		return nil, domainvideo.ErrVideoStateNotAllowed
	}
	video, err := domainvideo.NewProcessing(authorID, title, description, mediaAssetID, coverAssetID, idempotencyKey)
	if err != nil {
		return nil, err
	}
	if err := s.repo.Save(ctx, video); err != nil {
		if idempotencyKey != "" && errors.Is(err, domainvideo.ErrDuplicateIdempotencyKey) {
			existing, loadErr := s.repo.FindByAuthorAndIdempotencyKey(ctx, authorID, idempotencyKey)
			if loadErr == nil {
				return s.ensureReadyAssetPublication(ctx, existing, false)
			}
		}
		return nil, ErrSaveVideoFailed
	}
	if videoAsset.State == domainmedia.AssetStateReady {
		return s.ensureReadyAssetPublication(ctx, video, true)
	}
	return &CreateResult{Video: video, Created: true}, nil
}

func (s *Service) ensureReadyAssetPublication(ctx context.Context, video *domainvideo.Video, created bool) (*CreateResult, error) {
	if video == nil || video.MediaAssetID <= 0 || s.mediaDelivery == nil {
		return &CreateResult{Video: video, Created: created}, nil
	}
	if s.mediaAssets == nil {
		return nil, domainmedia.ErrMediaAssetNotFound
	}
	asset, err := s.mediaAssets.FindAssetByID(ctx, video.MediaAssetID)
	if err != nil {
		return nil, err
	}
	if asset.State != domainmedia.AssetStateReady {
		return &CreateResult{Video: video, Created: created}, nil
	}
	projectionRepo, ok := s.repo.(MediaProjectionRepository)
	if !ok {
		return nil, ErrUpdateVideoFailed
	}
	publication := NewMediaPublicationService(projectionRepo, s.mediaDelivery, s.publisher, s.cacheInvalidator)
	if err := publication.MediaReady(ctx, video.MediaAssetID); err != nil {
		return nil, err
	}
	updated, err := s.repo.FindByIDAnyStatus(ctx, video.ID)
	if err != nil {
		return nil, ErrLoadVideoFailed
	}
	return &CreateResult{Video: updated, Created: created}, nil
}

// CreatePublished 创建已发布视频；Idempotency-Key 命中时返回已有视频。
func (s *Service) CreatePublished(ctx context.Context, authorID int64, title, description, mediaURL, coverURL, idempotencyKey string) (*CreateResult, error) {
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if len(idempotencyKey) > domainvideo.MaxIdempotencyKeyLength {
		return nil, domainvideo.ErrIdempotencyKeyTooLong
	}

	if idempotencyKey != "" {
		// 客户端重试同一次创建请求时，先通过作者和幂等键找回原视频。
		existing, err := s.repo.FindByAuthorAndIdempotencyKey(ctx, authorID, idempotencyKey)
		if err == nil {
			return &CreateResult{Video: existing, Created: false}, nil
		}
		if !errors.Is(err, domainvideo.ErrVideoNotFound) {
			return nil, ErrLoadVideoFailed
		}
	}

	video, err := domainvideo.NewPublished(authorID, title, description, mediaURL, coverURL, idempotencyKey)
	if err != nil {
		return nil, err
	}
	if err := validatePublishAsset(ctx, s.localAssets, authorID, video.MediaURL, domainvideo.LocalAssetKindVideo); err != nil {
		return nil, err
	}
	if err := validatePublishAsset(ctx, s.localAssets, authorID, video.CoverURL, domainvideo.LocalAssetKindCover); err != nil {
		return nil, err
	}

	if err := s.repo.Save(ctx, video); err != nil {
		if idempotencyKey != "" && errors.Is(err, domainvideo.ErrDuplicateIdempotencyKey) {
			// 并发创建可能先查不到、后插入冲突；冲突后再查一次即可返回一致结果。
			existing, loadErr := s.repo.FindByAuthorAndIdempotencyKey(ctx, authorID, idempotencyKey)
			if loadErr == nil {
				return &CreateResult{Video: existing, Created: false}, nil
			}
			return nil, ErrLoadVideoFailed
		}
		return nil, ErrSaveVideoFailed
	}
	return &CreateResult{Video: video, Created: true}, nil
}

func (s *Service) publishCreatedVideo(ctx context.Context, video *domainvideo.Video) error {
	if s.publisher == nil {
		return nil
	}
	event := NewPublishedEvent(video)
	if event == nil {
		return nil
	}
	return s.publisher.PublishVideoPublished(ctx, event)
}

// Get 只返回已发布视频，删除或下线的视频在公开详情里表现为找不到。
func (s *Service) Get(ctx context.Context, videoID int64) (*domainvideo.Video, error) {
	if videoID <= 0 {
		return nil, domainvideo.ErrInvalidVideoID
	}

	video, err := s.repo.FindByID(ctx, videoID)
	if err != nil {
		if errors.Is(err, domainvideo.ErrVideoNotFound) {
			return nil, domainvideo.ErrVideoNotFound
		}
		return nil, ErrLoadVideoFailed
	}
	return video, nil
}

// ListByAuthor 查询某个作者公开发布的视频列表，使用 offset 分页。
func (s *Service) ListByAuthor(ctx context.Context, authorID int64, limit, offset int) ([]*domainvideo.Video, error) {
	if authorID <= 0 {
		return nil, domainvideo.ErrInvalidAuthorID
	}

	if limit <= 0 {
		return nil, domainvideo.ErrInvalidLimit
	}
	if offset < 0 {
		return nil, domainvideo.ErrInvalidOffset
	}
	if limit > 100 {
		// 后端限制最大页大小，避免一次请求拉取过多数据。
		limit = 100
	}

	videos, err := s.repo.ListByAuthor(ctx, authorID, limit, offset)
	if err != nil {
		return nil, ErrLoadVideoFailed
	}
	return videos, nil
}

func (s *Service) ListMine(ctx context.Context, authorID int64, limit, offset int) ([]*domainvideo.Video, error) {
	if authorID <= 0 {
		return nil, domainvideo.ErrInvalidAuthorID
	}
	if limit <= 0 {
		return nil, domainvideo.ErrInvalidLimit
	}
	if offset < 0 {
		return nil, domainvideo.ErrInvalidOffset
	}
	if limit > 100 {
		limit = 100
	}
	videos, err := s.repo.ListByOwner(ctx, authorID, limit, offset)
	if err != nil {
		return nil, ErrLoadVideoFailed
	}
	return videos, nil
}

// Delete 执行视频软删除，只有作者本人可以删除自己的视频。
func (s *Service) Delete(ctx context.Context, authorID, videoID int64) error {
	if authorID <= 0 {
		return domainvideo.ErrInvalidAuthorID
	}
	if videoID <= 0 {
		return domainvideo.ErrInvalidVideoID
	}

	video, err := s.repo.FindByIDAnyStatus(ctx, videoID)
	if err != nil {
		if errors.Is(err, domainvideo.ErrVideoNotFound) {
			return domainvideo.ErrVideoNotFound
		}
		return ErrLoadVideoFailed
	}
	// 软删除接口保持幂等：已经删除的视频再次删除仍然返回成功。
	alreadyDeleted := video.Status == domainvideo.StatusDeleted
	if err := video.DeleteBy(authorID); err != nil {
		return err
	}
	if alreadyDeleted {
		protectErr := s.protectVideoDelivery(ctx, video)
		var cleanupErr error
		if s.mediaCleanup != nil {
			cleanupErr = s.mediaCleanup.ScheduleMediaCleanup(ctx, video.MediaAssetID, video.CoverAssetID)
		}
		return errors.Join(protectErr, cleanupErr)
	}
	if _, err := s.repo.UpdateStatus(ctx, video); err != nil {
		if errors.Is(err, domainvideo.ErrVideoNotFound) {
			return domainvideo.ErrVideoNotFound
		}
		return ErrUpdateVideoFailed
	}
	if s.cacheInvalidator != nil {
		_ = s.cacheInvalidator.InvalidateVideo(ctx, video.ID)
	}
	protectErr := s.protectVideoDelivery(ctx, video)
	var cleanupErr error
	if s.mediaCleanup != nil {
		cleanupErr = s.mediaCleanup.ScheduleMediaCleanup(ctx, video.MediaAssetID, video.CoverAssetID)
	}
	return errors.Join(protectErr, cleanupErr)
}

func (s *Service) SetOffline(ctx context.Context, videoID int64) error {
	return s.applyLifecycleTransition(ctx, videoID, domainvideo.LifecycleTakeOffline, time.Time{})
}

func (s *Service) RestorePublished(ctx context.Context, videoID int64) error {
	return s.applyLifecycleTransition(ctx, videoID, domainvideo.LifecycleRestore, time.Time{})
}

func (s *Service) Approve(ctx context.Context, videoID int64, approvedAt time.Time) error {
	return s.applyLifecycleTransition(ctx, videoID, domainvideo.LifecycleApprove, approvedAt)
}

func (s *Service) Reject(ctx context.Context, videoID int64) error {
	return s.applyLifecycleTransition(ctx, videoID, domainvideo.LifecycleReject, time.Time{})
}

func (s *Service) applyLifecycleTransition(
	ctx context.Context,
	videoID int64,
	transition domainvideo.LifecycleTransition,
	at time.Time,
) error {
	if videoID <= 0 {
		return domainvideo.ErrInvalidVideoID
	}
	applied, err := s.repo.ApplyLifecycleTransition(ctx, videoID, transition, at)
	if err != nil {
		if errors.Is(err, domainvideo.ErrVideoNotFound) {
			return domainvideo.ErrVideoNotFound
		}
		if errors.Is(err, domainvideo.ErrVideoStateNotAllowed) {
			return err
		}
		return ErrUpdateVideoFailed
	}
	video, err := s.repo.FindByIDAnyStatus(ctx, videoID)
	if err != nil {
		return ErrLoadVideoFailed
	}
	if s.cacheInvalidator != nil && applied {
		_ = s.cacheInvalidator.InvalidateVideo(ctx, video.ID)
	}
	if video.Status == domainvideo.StatusPublished {
		if video.MediaAssetID > 0 && s.mediaDelivery != nil &&
			(domainmedia.IsPublicReadyStatus(video.MediaStatus) ||
				video.MediaErrorCode == "publication_event_failed") {
			if projectionRepo, ok := s.repo.(MediaProjectionRepository); ok {
				publication := NewMediaPublicationService(projectionRepo, s.mediaDelivery, s.publisher, s.cacheInvalidator)
				if err := publication.MediaReady(ctx, video.MediaAssetID); err != nil {
					return err
				}
				return nil
			}
		}
		if video.IsPubliclyReadable() {
			return s.publishCreatedVideo(ctx, video)
		}
	}
	return s.protectVideoDelivery(ctx, video)
}

func (s *Service) protectVideoDelivery(ctx context.Context, video *domainvideo.Video) error {
	if video == nil || video.MediaAssetID <= 0 || s.mediaDelivery == nil {
		return nil
	}
	if err := s.mediaDelivery.ProtectVideo(ctx, video.ID, video.MediaAssetID, video.CoverAssetID); err != nil {
		return err
	}
	video.MediaURL = ""
	video.CoverURL = ""
	video.PlaybackSources = nil
	if projectionRepo, ok := s.repo.(MediaProjectionRepository); ok {
		eligible, err := projectionRepo.UpdateMediaProjection(ctx, video)
		if err != nil {
			return err
		}
		if eligible {
			publication := NewMediaPublicationService(projectionRepo, s.mediaDelivery, s.publisher, s.cacheInvalidator)
			return publication.MediaReady(ctx, video.MediaAssetID)
		}
	}
	if s.cacheInvalidator != nil {
		_ = s.cacheInvalidator.InvalidateVideo(ctx, video.ID)
	}
	return nil
}
