package infralibrary

import (
	domainlibrary "GCFeed/internal/domain/library"
	"context"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Repository struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) SetWatchLater(ctx context.Context, fact *domainlibrary.WatchLater) (*domainlibrary.WatchLater, error) {
	model := WatchLaterModel{UserID: fact.UserID, VideoID: fact.VideoID, Status: fact.Status}
	if err := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "user_id"}, {Name: "video_id"}},
		DoUpdates: clause.Assignments(map[string]any{
			"status":     gorm.Expr("EXCLUDED.status"),
			"updated_at": gorm.Expr("CASE WHEN user_watch_later.status = EXCLUDED.status THEN user_watch_later.updated_at ELSE EXCLUDED.updated_at END"),
		}),
	}).Create(&model).Error; err != nil {
		return nil, err
	}
	if err := r.db.WithContext(ctx).Where("user_id = ? AND video_id = ?", fact.UserID, fact.VideoID).Take(&model).Error; err != nil {
		return nil, err
	}
	return domainlibrary.RestoreWatchLater(model.UserID, model.VideoID, model.Status, model.CreatedAt, model.UpdatedAt), nil
}

func (r *Repository) ListWatchLater(ctx context.Context, userID int64, cursor *domainlibrary.Cursor, limit int) ([]domainlibrary.VideoCandidate, error) {
	var models []WatchLaterModel
	query := r.db.WithContext(ctx).Where("user_id = ? AND status = ?", userID, domainlibrary.WatchLaterStatusActive)
	if cursor != nil {
		query = query.Where("(updated_at < ? OR (updated_at = ? AND video_id < ?))", cursor.UpdatedAt, cursor.UpdatedAt, cursor.VideoID)
	}
	if err := query.Order("updated_at DESC").Order("video_id DESC").Limit(limit).Find(&models).Error; err != nil {
		return nil, err
	}
	items := make([]domainlibrary.VideoCandidate, 0, len(models))
	for _, model := range models {
		items = append(items, domainlibrary.VideoCandidate{VideoID: model.VideoID, UpdatedAt: model.UpdatedAt})
	}
	return items, nil
}
