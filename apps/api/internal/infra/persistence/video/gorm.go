package infravideo

import (
	"context"
	"errors"
	domainadminaudit "github.com/shiyudesu/frux/internal/domain/adminaudit"
	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
	domainmessage "github.com/shiyudesu/frux/internal/domain/message"
	domainvideo "github.com/shiyudesu/frux/internal/domain/video"
	inframediastore "github.com/shiyudesu/frux/internal/infra/media"
	infrapersistence "github.com/shiyudesu/frux/internal/infra/persistence"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Repository struct {
	db           *gorm.DB
	mediaCatalog *inframediastore.DeliveryCatalog
	auditWriter  AuditWriter
}

type Option func(*Repository)

type AuditWriter interface {
	AppendInTransaction(ctx context.Context, tx *gorm.DB, fact *domainadminaudit.Fact) error
	RecordCommittedWrite(fact *domainadminaudit.Fact)
}

// videoWithStatModel 承接 video 与 video_stat 联表查询结果。
type videoWithStatModel struct {
	ID             int64
	AuthorID       int64
	Title          string
	Description    string
	MediaURL       string
	CoverURL       string
	MediaAssetID   *int64
	CoverAssetID   *int64
	MediaStatus    string
	MediaErrorCode string
	ReviewVersion  int
	Version        int
	Status         int
	Visibility     string
	LikeCount      int
	CommentCount   int
	FavoriteCount  int
	PublishedAt    *time.Time
	IdempotencyKey *string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// New 创建视频仓储实现。
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

func WithAdminAuditWriter(writer AuditWriter) Option {
	return func(repository *Repository) {
		repository.auditWriter = writer
	}
}

// EnsureStats 确保每个视频都有一条统计记录。
func EnsureStats(db *gorm.DB) error {
	return db.Exec(`
		INSERT INTO video_stat (video_id, like_count, comment_count, favorite_count, created_at, updated_at)
		SELECT v.id, 0, 0, 0, NOW(), NOW()
		FROM video AS v
		LEFT JOIN video_stat AS vs ON vs.video_id = v.id
		WHERE vs.video_id IS NULL
	`).Error
}

func BackfillLocalAssets(db *gorm.DB) error {
	return db.Exec(`
		INSERT INTO local_upload_asset (asset_url, owner_id, kind, created_at)
		SELECT asset_url, MIN(author_id), MIN(kind), NOW()
		FROM (
			SELECT media_url AS asset_url, author_id, 'video' AS kind
			FROM video
			WHERE media_url LIKE '/uploads/video/%'
			UNION ALL
			SELECT cover_url AS asset_url, author_id, 'cover' AS kind
			FROM video
			WHERE cover_url LIKE '/uploads/cover/%'
		) AS referenced_assets
		GROUP BY asset_url
		HAVING COUNT(DISTINCT author_id) = 1
		   AND COUNT(DISTINCT kind) = 1
		ON CONFLICT (asset_url) DO NOTHING
	`).Error
}

// Save 在同一事务内写入视频记录和初始统计记录。
func (r *Repository) Save(ctx context.Context, video *domainvideo.Video) error {
	var model VideoModel
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		model = VideoModel{
			AuthorID:       video.AuthorID,
			Title:          video.Title,
			Description:    video.Description,
			MediaURL:       video.MediaURL,
			CoverURL:       video.CoverURL,
			MediaAssetID:   positiveInt64Ptr(video.MediaAssetID),
			CoverAssetID:   positiveInt64Ptr(video.CoverAssetID),
			MediaStatus:    video.MediaStatus,
			MediaErrorCode: video.MediaErrorCode,
			ReviewVersion:  video.ReviewVersion,
			Version:        video.Version,
			Status:         video.Status,
			Visibility:     video.Visibility,
			PublishedAt:    video.PublishedAt,
			IdempotencyKey: idempotencyKeyPtr(video.IdempotencyKey),
		}

		if err := tx.Create(&model).Error; err != nil {
			if infrapersistence.IsDuplicatedKeyError(err) {
				return domainvideo.ErrDuplicateIdempotencyKey
			}
			return err
		}

		// video_stat 独立存储计数，便于互动接口只更新统计表。
		stat := VideoStatModel{
			VideoID:       model.ID,
			LikeCount:     video.LikeCount,
			CommentCount:  video.CommentCount,
			FavoriteCount: video.FavoriteCount,
		}
		if err := tx.Create(&stat).Error; err != nil {
			return err
		}
		publicDelta, privateDelta := contentWorkCounts(video.Status, video.Visibility, video.MediaStatus)
		if err := AdjustContentStat(tx, video.AuthorID, publicDelta, privateDelta, 0, 0); err != nil {
			return err
		}
		if err := AppendLifecycleNotification(tx, domainmessage.LifecycleNotification{
			EventID:     domainmessage.SubmissionEventID(model.ID, model.ReviewVersion),
			RecipientID: model.AuthorID, VideoID: model.ID,
			ReviewVersion: model.ReviewVersion,
			Stage:         domainmessage.LifecycleStageSubmitted,
			Result:        domainmessage.LifecycleResultPending,
			OccurredAt:    model.CreatedAt,
		}); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}

	// 写回数据库生成的 ID 和时间字段，保证返回响应包含完整信息。
	video.ID = model.ID
	video.CreatedAt = model.CreatedAt
	video.UpdatedAt = model.UpdatedAt
	return nil
}

// FindByID 查询公开可见的视频详情，只返回 Published 状态。
func (r *Repository) FindByID(ctx context.Context, id int64) (*domainvideo.Video, error) {
	var model videoWithStatModel
	err := r.db.WithContext(ctx).
		Table("video AS v").
		Select(videoWithStatSelect()).
		Joins("LEFT JOIN video_stat AS vs ON vs.video_id = v.id").
		Where("v.id = ? AND v.status = ? AND v.visibility = ? AND v.media_status IN ?", id, domainvideo.StatusPublished, domainvideo.VisibilityPublic, []string{domainmedia.MediaStatusLegacyReady, domainmedia.MediaStatusReady}).
		Take(&model).
		Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domainvideo.ErrVideoNotFound
		}
		return nil, err
	}
	video := restoreVideo(model)
	if err := r.hydrateMediaDelivery(ctx, []*domainvideo.Video{video}); err != nil {
		return nil, err
	}
	if video.MediaAssetID > 0 && video.MediaURL == "" {
		return nil, domainvideo.ErrVideoNotFound
	}
	return video, nil
}

// FindByIDAnyStatus 查询任意状态视频，供作者删除等内部流程使用。
func (r *Repository) FindByIDAnyStatus(ctx context.Context, id int64) (*domainvideo.Video, error) {
	var model videoWithStatModel
	err := r.db.WithContext(ctx).
		Table("video AS v").
		Select(videoWithStatSelect()).
		Joins("LEFT JOIN video_stat AS vs ON vs.video_id = v.id").
		Where("v.id = ?", id).
		Take(&model).
		Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domainvideo.ErrVideoNotFound
		}
		return nil, err
	}
	video := restoreVideo(model)
	if err := r.hydrateMediaDelivery(ctx, []*domainvideo.Video{video}); err != nil {
		return nil, err
	}
	return video, nil
}

// FindByAuthorAndIdempotencyKey 根据作者和幂等键查找已创建视频。
func (r *Repository) FindByAuthorAndIdempotencyKey(ctx context.Context, authorID int64, key string) (*domainvideo.Video, error) {
	if key == "" {
		return nil, domainvideo.ErrVideoNotFound
	}

	var model videoWithStatModel
	err := r.db.WithContext(ctx).
		Table("video AS v").
		Select(videoWithStatSelect()).
		Joins("LEFT JOIN video_stat AS vs ON vs.video_id = v.id").
		Where("v.author_id = ? AND v.idempotency_key = ?", authorID, key).
		Take(&model).
		Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domainvideo.ErrVideoNotFound
		}
		return nil, err
	}
	video := restoreVideo(model)
	if err := r.hydrateMediaDelivery(ctx, []*domainvideo.Video{video}); err != nil {
		return nil, err
	}
	return video, nil
}

// ListByAuthor 按发布时间倒序返回作者已发布视频。
func (r *Repository) ListByAuthor(ctx context.Context, authorID int64, limit, offset int) ([]*domainvideo.Video, error) {
	result := make([]*domainvideo.Video, 0, limit)
	resolvedSeen := 0
	rawOffset := 0
	batchSize := limit * 2
	if batchSize < 40 {
		batchSize = 40
	}
	for len(result) < limit {
		var models []videoWithStatModel
		err := r.db.WithContext(ctx).
			Table("video AS v").
			Select(videoWithStatSelect()).
			Joins("LEFT JOIN video_stat AS vs ON vs.video_id = v.id").
			Where("v.author_id = ? AND v.status = ? AND v.visibility = ? AND v.media_status IN ?", authorID, domainvideo.StatusPublished, domainvideo.VisibilityPublic, []string{domainmedia.MediaStatusLegacyReady, domainmedia.MediaStatusReady}).
			Order("v.published_at DESC").
			Order("v.id DESC").
			Limit(batchSize).
			Offset(rawOffset).
			Scan(&models).
			Error
		if err != nil {
			return nil, err
		}
		if len(models) == 0 {
			break
		}
		rawOffset += len(models)
		videos := make([]*domainvideo.Video, 0, len(models))
		for _, model := range models {
			videos = append(videos, restoreVideo(model))
		}
		if err := r.hydrateMediaDelivery(ctx, videos); err != nil {
			return nil, err
		}
		for _, video := range filterResolvedPublicVideos(videos) {
			if resolvedSeen < offset {
				resolvedSeen++
				continue
			}
			result = append(result, video)
			if len(result) == limit {
				break
			}
		}
		if len(models) < batchSize {
			break
		}
	}
	return result, nil
}

func (r *Repository) ListByOwner(ctx context.Context, authorID int64, limit, offset int) ([]*domainvideo.Video, error) {
	var models []videoWithStatModel
	err := r.db.WithContext(ctx).
		Table("video AS v").
		Select(videoWithStatSelect()).
		Joins("LEFT JOIN video_stat AS vs ON vs.video_id = v.id").
		Where("v.author_id = ? AND v.status <> ?", authorID, domainvideo.StatusDeleted).
		Order("v.created_at DESC").
		Order("v.id DESC").
		Limit(limit).
		Offset(offset).
		Scan(&models).
		Error
	if err != nil {
		return nil, err
	}
	videos := make([]*domainvideo.Video, 0, len(models))
	for _, model := range models {
		videos = append(videos, restoreVideo(model))
	}
	if err := r.hydrateMediaDelivery(ctx, videos); err != nil {
		return nil, err
	}
	return videos, nil
}

// UpdateStatus 在同一事务中更新生命周期和首次发布时间。
func (r *Repository) UpdateStatus(ctx context.Context, video *domainvideo.Video) (bool, error) {
	applied := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current VideoModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", video.ID).Take(&current).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return domainvideo.ErrVideoNotFound
			}

			return err
		}
		if current.Status == video.Status {
			video.PublishedAt = current.PublishedAt
			return nil
		}
		if video.Status != domainvideo.StatusDeleted ||
			!domainvideo.ValidLifecycleTransition(current.Status, video.Status) {
			return domainvideo.ErrVideoStateNotAllowed
		}
		previousStatus := current.Status
		publishedAt := current.PublishedAt
		if err := tx.Model(&current).Updates(map[string]any{
			"status":       video.Status,
			"published_at": publishedAt,
		}).Error; err != nil {
			return err
		}
		publicDelta, privateDelta := contentWorkDeltas(previousStatus, current.Visibility, current.MediaStatus, video.Status, current.Visibility, current.MediaStatus)
		receivedLikeDelta := 0
		if previousStatus != domainvideo.StatusDeleted && video.Status == domainvideo.StatusDeleted {
			var stat VideoStatModel
			if err := tx.Where("video_id = ?", current.ID).Take(&stat).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			receivedLikeDelta = -stat.LikeCount
		}
		if err := AdjustContentStat(tx, current.AuthorID, publicDelta, privateDelta, receivedLikeDelta, 0); err != nil {
			return err
		}
		video.PublishedAt = publishedAt
		applied = true
		return nil
	})
	return applied, err
}

func (r *Repository) ApplyLifecycleTransition(
	ctx context.Context,
	videoID int64,
	transition domainvideo.LifecycleTransition,
	at time.Time,
) (bool, error) {
	applied := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current VideoModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", videoID).Take(&current).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return domainvideo.ErrVideoNotFound
			}
			return err
		}
		video := &domainvideo.Video{Status: current.Status, PublishedAt: current.PublishedAt}
		if err := video.ApplyLifecycleTransition(transition, at); err != nil {
			return err
		}
		if video.Status == current.Status {
			return nil
		}
		if err := tx.Model(&current).Updates(map[string]any{
			"status":       video.Status,
			"published_at": video.PublishedAt,
		}).Error; err != nil {
			return err
		}
		publicDelta, privateDelta := contentWorkDeltas(
			current.Status,
			current.Visibility,
			current.MediaStatus,
			video.Status,
			current.Visibility,
			current.MediaStatus,
		)
		if err := AdjustContentStat(tx, current.AuthorID, publicDelta, privateDelta, 0, 0); err != nil {
			return err
		}
		current.Status = video.Status
		current.PublishedAt = video.PublishedAt
		if current.Status == domainvideo.StatusPublished &&
			current.Visibility == domainvideo.VisibilityPublic &&
			domainmedia.IsPublicReadyStatus(current.MediaStatus) {
			if err := AppendPublicationHandoff(
				tx, current, at, current.MediaAssetID == nil,
			); err != nil {
				return err
			}
		}
		applied = true
		return nil
	})
	return applied, err
}

func (r *Repository) ListByMediaAssetID(ctx context.Context, mediaAssetID int64) ([]*domainvideo.Video, error) {
	if mediaAssetID <= 0 {
		return []*domainvideo.Video{}, nil
	}
	var models []videoWithStatModel
	if err := r.db.WithContext(ctx).
		Table("video AS v").Select(videoWithStatSelect()).
		Joins("LEFT JOIN video_stat AS vs ON vs.video_id = v.id").
		Where("v.media_asset_id = ? AND v.status <> ?", mediaAssetID, domainvideo.StatusDeleted).
		Order("v.id ASC").Scan(&models).Error; err != nil {
		return nil, err
	}
	videos := make([]*domainvideo.Video, 0, len(models))
	for _, model := range models {
		videos = append(videos, restoreVideo(model))
	}
	return videos, nil
}

func (r *Repository) UpdateMediaProjection(ctx context.Context, video *domainvideo.Video) (bool, error) {
	eligible := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current VideoModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", video.ID).Take(&current).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return domainvideo.ErrVideoNotFound
			}
			return err
		}
		previousMediaStatus := current.MediaStatus
		eligible = current.Status == domainvideo.StatusPublished &&
			current.Visibility == domainvideo.VisibilityPublic &&
			domainmedia.IsPublicReadyStatus(video.MediaStatus)
		if !eligible {
			video.MediaURL = ""
			video.CoverURL = ""
			video.PlaybackSources = nil
		}
		publicDelta, privateDelta := contentWorkDeltas(
			current.Status, current.Visibility, current.MediaStatus,
			current.Status, current.Visibility, video.MediaStatus,
		)
		now := time.Now().UTC()
		if err := tx.Model(&current).Updates(map[string]any{
			"media_url": video.MediaURL, "cover_url": video.CoverURL,
			"media_status": video.MediaStatus, "media_error_code": video.MediaErrorCode,
			"updated_at": now,
		}).Error; err != nil {
			return err
		}
		if err := AdjustContentStat(tx, current.AuthorID, publicDelta, privateDelta, 0, 0); err != nil {
			return err
		}
		if previousMediaStatus != domainmedia.MediaStatusFailed &&
			video.MediaStatus == domainmedia.MediaStatusFailed &&
			current.Status != domainvideo.StatusRejected &&
			current.Status != domainvideo.StatusDeleted {
			profileVersion := strings.TrimSpace(video.MediaProfileVersion)
			if profileVersion == "" {
				profileVersion = "current"
			}
			if err := AppendLifecycleNotification(tx, domainmessage.LifecycleNotification{
				EventID: domainmessage.MediaFailureEventID(
					current.ID, pointerID(current.MediaAssetID), profileVersion,
				),
				RecipientID: current.AuthorID, VideoID: current.ID,
				ReviewVersion: current.ReviewVersion,
				Stage:         domainmessage.LifecycleStageMediaProcessing,
				Result:        domainmessage.LifecycleResultFailed,
				ReasonCode:    domainmessage.LifecycleReasonMediaProcessingFailed,
				OccurredAt:    now,
			}); err != nil {
				return err
			}
		}
		if eligible {
			current.MediaURL = video.MediaURL
			current.CoverURL = video.CoverURL
			current.MediaStatus = video.MediaStatus
			current.MediaErrorCode = video.MediaErrorCode
			if err := AppendPublicationHandoff(tx, current, now, true); err != nil {
				return err
			}
		}
		return nil
	})
	return eligible, err
}

func (r *Repository) AuthorizeMediaAsset(ctx context.Context, assetID, ownerID int64) (referenced, allowed bool, err error) {
	var references []struct {
		AuthorID int64
		Status   int
	}

	if err := r.db.WithContext(ctx).Model(&VideoModel{}).
		Select("author_id, status").
		Where("media_asset_id = ? OR cover_asset_id = ?", assetID, assetID).
		Scan(&references).Error; err != nil {
		return false, false, err
	}
	for _, reference := range references {
		referenced = true
		if reference.AuthorID == ownerID && reference.Status != domainvideo.StatusDeleted {
			allowed = true
		}
	}
	return referenced, allowed, nil
}

func (r *Repository) AuthorizePublicMediaObject(ctx context.Context, objectKey string) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Table("media_variant AS mv").
		Joins(`
			JOIN video AS v
				ON v.id = mv.video_id
				OR v.media_asset_id = mv.asset_id
				OR v.cover_asset_id = mv.asset_id
		`).
		Where(
			"mv.object_key = ? AND mv.public = ? AND v.status = ? AND v.visibility = ? AND v.media_status IN ?",
			objectKey,
			true,
			domainvideo.StatusPublished,
			domainvideo.VisibilityPublic,
			[]string{domainmedia.MediaStatusLegacyReady, domainmedia.MediaStatusReady},
		).
		Distinct("v.id").
		Count(&count).
		Error
	return count > 0, err
}

// restoreVideo 把联表查询结果转换成领域视频对象。
func restoreVideo(model videoWithStatModel) *domainvideo.Video {
	return restoreVideoWithSources(model, nil)
}

func restoreVideoWithSources(model videoWithStatModel, sources []domainmedia.PlaybackSource) *domainvideo.Video {
	assetID := int64(0)
	if model.MediaAssetID != nil {
		assetID = *model.MediaAssetID
	}
	coverAssetID := int64(0)
	if model.CoverAssetID != nil {
		coverAssetID = *model.CoverAssetID
	}
	video := domainvideo.RestoreVideoWithReviewVersion(
		model.ID,
		model.AuthorID,
		model.Title,
		model.Description,
		model.MediaURL,
		model.CoverURL,
		model.Status,
		model.Visibility,
		model.LikeCount,
		model.CommentCount,
		model.FavoriteCount,
		model.PublishedAt,
		model.CreatedAt,
		model.UpdatedAt,
		idempotencyKeyValue(model.IdempotencyKey),
		assetID,
		model.MediaStatus,
		model.MediaErrorCode,
		sources,
		coverAssetID,
		model.ReviewVersion,
	)
	if model.Version > 0 {
		video.Version = model.Version
	}
	return video
}

// videoWithStatSelect 统一视频详情查询字段，避免多个查询写重复 SQL 字段列表。
func videoWithStatSelect() string {
	return "v.id, v.author_id, v.title, v.description, v.media_url, v.cover_url, v.media_asset_id, v.cover_asset_id, v.media_status, v.media_error_code, v.review_version, v.version, v.status, v.visibility, COALESCE(vs.like_count, 0) AS like_count, COALESCE(vs.comment_count, 0) AS comment_count, COALESCE(vs.favorite_count, 0) AS favorite_count, v.published_at, v.idempotency_key, v.created_at, v.updated_at"
}

// idempotencyKeyPtr 将空幂等键存为 NULL，配合唯一索引允许普通创建多次执行。
func idempotencyKeyPtr(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

// idempotencyKeyValue 将数据库可空字段还原成领域层字符串。
func idempotencyKeyValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func positiveInt64Ptr(value int64) *int64 {
	if value <= 0 {
		return nil
	}
	return &value
}

func (r *Repository) hydrateMediaDelivery(ctx context.Context, videos []*domainvideo.Video) error {
	if r.mediaCatalog == nil || len(videos) == 0 {
		return nil
	}

	refs := make([]inframediastore.DeliveryRef, 0, len(videos))
	for _, video := range videos {
		if video == nil || video.MediaAssetID <= 0 || !domainmedia.IsPublicReadyStatus(video.MediaStatus) {
			continue
		}
		if video.Status != domainvideo.StatusPublished || video.Visibility != domainvideo.VisibilityPublic {
			video.MediaURL = ""
			video.CoverURL = ""
			video.PlaybackSources = nil
			continue
		}
		refs = append(refs, inframediastore.DeliveryRef{
			VideoID: video.ID, MediaAssetID: video.MediaAssetID, CoverAssetID: video.CoverAssetID,
		})
	}
	deliveries, err := r.mediaCatalog.ResolveBatch(ctx, refs)
	if err != nil {
		return err
	}
	for _, video := range videos {
		if delivery := deliveries[video.ID]; delivery != nil {
			video.MediaURL = delivery.MediaURL
			video.CoverURL = delivery.CoverURL
			video.PlaybackSources = delivery.PlaybackSources
		} else if video.MediaAssetID > 0 {
			video.MediaURL = ""
			video.CoverURL = ""
			video.PlaybackSources = nil
		}
	}
	return nil
}

func filterResolvedPublicVideos(videos []*domainvideo.Video) []*domainvideo.Video {
	filtered := make([]*domainvideo.Video, 0, len(videos))
	for _, video := range videos {
		if video == nil || (video.MediaAssetID > 0 && video.MediaURL == "") {
			continue
		}
		filtered = append(filtered, video)
	}
	return filtered
}
