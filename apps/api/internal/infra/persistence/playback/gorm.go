package infraplayback

import (
	domainmedia "GCFeed/internal/domain/media"
	domainplayback "GCFeed/internal/domain/playback"
	domainvideo "GCFeed/internal/domain/video"
	inframediastore "GCFeed/internal/infra/media"
	infrapersistence "GCFeed/internal/infra/persistence"
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Repository struct {
	db           *gorm.DB
	mediaCatalog *inframediastore.DeliveryCatalog
}

type Option func(*Repository)

// New 创建播放优化仓储实现。
func New(db *gorm.DB, options ...Option) *Repository {
	repository := &Repository{db: db}
	for _, option := range options {
		option(repository)
	}
	return repository
}

func WithMediaCatalog(catalog *inframediastore.DeliveryCatalog) Option {
	return func(repository *Repository) {
		repository.mediaCatalog = catalog
	}
}

// FindConfig 按端和网络类型读取配置。
func (r *Repository) FindConfig(ctx context.Context, platform string, networkType string) (*domainplayback.Config, error) {
	var model ConfigModel
	err := r.db.WithContext(ctx).
		Where("platform = ? AND network_type = ?", platform, networkType).
		Take(&model).
		Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return restoreConfig(model), nil
}

// ListPreloadVideos 按发布时间读取兼容补充资源；currentVideoID 为空时从最新资源开始。
func (r *Repository) ListPreloadVideos(ctx context.Context, currentVideoID int64, limit int) ([]*domainplayback.PreloadVideo, error) {
	query := r.db.WithContext(ctx).
		Table("video AS v").
		Select("v.id AS video_id, v.media_url, v.cover_url, v.media_asset_id, v.cover_asset_id, v.media_status").
		Where("v.status = ? AND v.visibility = ? AND v.media_status IN ? AND v.published_at IS NOT NULL", domainvideo.StatusPublished, domainvideo.VisibilityPublic, []string{domainmedia.MediaStatusLegacyReady, domainmedia.MediaStatusReady})

	if currentVideoID > 0 {
		current, err := r.findCurrentVideo(ctx, currentVideoID)
		if err != nil {
			return nil, err
		}
		if current != nil {
			query = query.Where(
				"(v.published_at < ? OR (v.published_at = ? AND v.id < ?))",
				current.PublishedAt,
				current.PublishedAt,
				current.VideoID,
			)
		}
	}

	var models []PreloadVideoModel
	err := query.
		Order("v.published_at DESC").
		Order("v.id DESC").
		Limit(limit).
		Scan(&models).
		Error
	if err != nil {
		return nil, err
	}
	if r.mediaCatalog != nil {
		refs := make([]inframediastore.DeliveryRef, 0, len(models))
		for _, model := range models {
			if model.MediaAssetID > 0 {
				refs = append(refs, inframediastore.DeliveryRef{VideoID: model.VideoID, MediaAssetID: model.MediaAssetID, CoverAssetID: model.CoverAssetID})
			}
		}
		deliveries, err := r.mediaCatalog.ResolveBatch(ctx, refs)
		if err != nil {
			return nil, err
		}
		for index := range models {
			if delivery := deliveries[models[index].VideoID]; delivery != nil {
				models[index].MediaURL = delivery.MediaURL
				models[index].CoverURL = delivery.CoverURL
				models[index].PlaybackSources = delivery.PlaybackSources
			}
		}
	}

	items := make([]*domainplayback.PreloadVideo, 0, len(models))
	for _, model := range models {
		item := domainplayback.RestorePreloadVideo(model.VideoID, model.MediaURL, model.CoverURL)
		item.MediaStatus = model.MediaStatus
		item.PlaybackSources = model.PlaybackSources
		items = append(items, item)
	}
	return items, nil
}

// CreateQoSReport 保存播放质量流水，支持 user_id + idempotency_key 幂等。
func (r *Repository) CreateQoSReport(ctx context.Context, report *domainplayback.QoSReport) (*domainplayback.QoSReport, bool, error) {
	model := QoSLogModel{
		UserID:         report.UserID,
		VideoID:        report.VideoID,
		FirstFrameMs:   report.FirstFrameMs,
		StutterCount:   report.StutterCount,
		WatchMs:        report.WatchMs,
		IdempotencyKey: optionalString(report.IdempotencyKey),
	}

	err := r.db.WithContext(ctx).Create(&model).Error
	if err == nil {
		return restoreQoSReport(model), true, nil
	}
	if !infrapersistence.IsDuplicatedKeyError(err) {
		return nil, false, err
	}

	existing, findErr := r.findExistingQoS(ctx, report.UserID, report.IdempotencyKey)
	if findErr != nil {
		return nil, false, findErr
	}
	return restoreQoSReport(existing), false, nil
}

func (r *Repository) findCurrentVideo(ctx context.Context, videoID int64) (*currentVideoModel, error) {
	var model currentVideoModel
	err := r.db.WithContext(ctx).
		Table("video AS v").
		Select("v.id AS video_id, v.published_at").
		Where("v.id = ? AND v.status = ? AND v.visibility = ? AND v.media_status IN ? AND v.published_at IS NOT NULL", videoID, domainvideo.StatusPublished, domainvideo.VisibilityPublic, []string{domainmedia.MediaStatusLegacyReady, domainmedia.MediaStatusReady}).
		Take(&model).
		Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &model, nil
}

func (r *Repository) findExistingQoS(ctx context.Context, userID int64, idempotencyKey string) (QoSLogModel, error) {
	var model QoSLogModel
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if idempotencyKey == "" {
		return model, gorm.ErrRecordNotFound
	}
	err := r.db.WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("user_id = ? AND idempotency_key = ?", userID, idempotencyKey).
		Order("id DESC").
		Take(&model).
		Error
	return model, err
}

type currentVideoModel struct {
	VideoID     int64
	PublishedAt time.Time
}

func restoreConfig(model ConfigModel) *domainplayback.Config {
	return domainplayback.RestoreConfig(model.ID, model.Platform, model.NetworkType, model.PreloadCount, model.BufferMs, model.UpdatedAt)
}

func restoreQoSReport(model QoSLogModel) *domainplayback.QoSReport {
	return domainplayback.RestoreQoSReport(
		model.ID,
		model.UserID,
		model.VideoID,
		model.FirstFrameMs,
		model.StutterCount,
		model.WatchMs,
		stringValue(model.IdempotencyKey),
		model.CreatedAt,
	)
}

func optionalString(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}
