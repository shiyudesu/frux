package infravideo

import (
	domainvideo "GCFeed/internal/domain/video"
	infrapersistence "GCFeed/internal/infra/persistence"
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Repository struct {
	db *gorm.DB
}

// videoWithStatModel 承接 video 与 video_stat 联表查询结果。
type videoWithStatModel struct {
	ID             int64
	AuthorID       int64
	Title          string
	Description    string
	MediaURL       string
	CoverURL       string
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
func New(db *gorm.DB) *Repository {
	return &Repository{db: db}
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
		publicDelta, privateDelta := contentWorkCounts(video.Status, video.Visibility)
		if err := AdjustContentStat(tx, video.AuthorID, publicDelta, privateDelta, 0, 0); err != nil {
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
		Where("v.id = ? AND v.status = ? AND v.visibility = ?", id, domainvideo.StatusPublished, domainvideo.VisibilityPublic).
		Take(&model).
		Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domainvideo.ErrVideoNotFound
		}
		return nil, err
	}
	return restoreVideo(model), nil
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
	return restoreVideo(model), nil
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
	return restoreVideo(model), nil
}

// ListByAuthor 按发布时间倒序返回作者已发布视频。
func (r *Repository) ListByAuthor(ctx context.Context, authorID int64, limit, offset int) ([]*domainvideo.Video, error) {
	var models []videoWithStatModel
	err := r.db.WithContext(ctx).
		Table("video AS v").
		Select(videoWithStatSelect()).
		Joins("LEFT JOIN video_stat AS vs ON vs.video_id = v.id").
		Where("v.author_id = ? AND v.status = ? AND v.visibility = ?", authorID, domainvideo.StatusPublished, domainvideo.VisibilityPublic).
		Order("v.published_at DESC").
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
		// 查询模型逐条恢复为领域对象，应用层无需知道数据库联表细节。
		videos = append(videos, restoreVideo(model))
	}
	return videos, nil
}

// UpdateStatus 只更新状态字段，用于软删除。
func (r *Repository) UpdateStatus(ctx context.Context, video *domainvideo.Video) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current VideoModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", video.ID).Take(&current).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return domainvideo.ErrVideoNotFound
			}
			return err
		}
		if current.Status == video.Status {
			return nil
		}
		previousStatus := current.Status
		if err := tx.Model(&current).Update("status", video.Status).Error; err != nil {
			return err
		}
		publicDelta, privateDelta := contentWorkDeltas(previousStatus, current.Visibility, video.Status, current.Visibility)
		receivedLikeDelta := 0
		if previousStatus != domainvideo.StatusDeleted && video.Status == domainvideo.StatusDeleted {
			var stat VideoStatModel
			if err := tx.Where("video_id = ?", current.ID).Take(&stat).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			receivedLikeDelta = -stat.LikeCount
		}
		return AdjustContentStat(tx, current.AuthorID, publicDelta, privateDelta, receivedLikeDelta, 0)
	})
}

// restoreVideo 把联表查询结果转换成领域视频对象。
func restoreVideo(model videoWithStatModel) *domainvideo.Video {
	return domainvideo.RestoreVideoWithVisibility(
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
	)
}

// videoWithStatSelect 统一视频详情查询字段，避免多个查询写重复 SQL 字段列表。
func videoWithStatSelect() string {
	return "v.id, v.author_id, v.title, v.description, v.media_url, v.cover_url, v.status, v.visibility, COALESCE(vs.like_count, 0) AS like_count, COALESCE(vs.comment_count, 0) AS comment_count, COALESCE(vs.favorite_count, 0) AS favorite_count, v.published_at, v.idempotency_key, v.created_at, v.updated_at"
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
