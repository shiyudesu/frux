package infraexposure

import (
	domainexposure "GCFeed/internal/domain/exposure"
	domainvideo "GCFeed/internal/domain/video"
	infrapersistence "GCFeed/internal/infra/persistence"
	infravideo "GCFeed/internal/infra/persistence/video"
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Repository struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// SaveViewEvent 写入观看行为，并在 exposed 事件时 upsert 用户视频曝光聚合。
func (r *Repository) SaveViewEvent(ctx context.Context, event *domainexposure.ViewEvent) (*domainexposure.ViewEvent, *domainexposure.Exposure, error) {
	var eventModel ViewEventModel
	var exposureModel ExposureModel
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := ensureReadableVideo(tx, event.UserID, event.VideoID); err != nil {
			return err
		}

		eventModel = ViewEventModel{
			UserID:    event.UserID,
			VideoID:   event.VideoID,
			Scene:     event.Scene,
			RequestID: stringPtr(event.RequestID),
			EventType: event.EventType,
			WatchMs:   event.WatchMs,
			Completed: event.Completed,
		}
		if err := tx.Create(&eventModel).Error; err != nil {
			return err
		}

		if event.CountsAsHistory() {
			if err := upsertViewHistory(tx, eventModel); err != nil {
				return err
			}
		}

		if !event.CountsAsExposure() {
			return nil
		}

		exposureModel = ExposureModel{
			UserID:         event.UserID,
			VideoID:        event.VideoID,
			FirstExposedAt: eventModel.CreatedAt,
			LastExposedAt:  eventModel.CreatedAt,
			ExposureCount:  1,
			LastScene:      event.Scene,
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{
				{Name: "user_id"},
				{Name: "video_id"},
			},
			DoUpdates: clause.Assignments(map[string]any{
				"last_exposed_at": gorm.Expr("EXCLUDED.last_exposed_at"),
				"exposure_count":  gorm.Expr("exposures.exposure_count + 1"),
				"last_scene":      gorm.Expr("EXCLUDED.last_scene"),
				"updated_at":      gorm.Expr("EXCLUDED.updated_at"),
			}),
		}).Create(&exposureModel).Error; err != nil {
			return err
		}

		return tx.Where("user_id = ? AND video_id = ?", event.UserID, event.VideoID).Take(&exposureModel).Error
	})
	if err != nil {
		return nil, nil, mapExposureError(err)
	}

	savedEvent := restoreViewEvent(eventModel)
	var exposure *domainexposure.Exposure
	if event.CountsAsExposure() {
		exposure = restoreExposure(exposureModel)
	}
	return savedEvent, exposure, nil
}

func upsertViewHistory(tx *gorm.DB, event ViewEventModel) error {
	const newerEvent = "(video_view_history.last_watched_at, video_view_history.last_event_id) < (EXCLUDED.last_watched_at, EXCLUDED.last_event_id)"
	history := ViewHistoryModel{
		UserID: event.UserID, VideoID: event.VideoID,
		LastScene: event.Scene, LastEventType: event.EventType,
		LastWatchMs: event.WatchMs, Completed: event.Completed,
		FirstWatchedAt: event.CreatedAt, LastWatchedAt: event.CreatedAt,
		LastEventID: event.ID,
	}
	return tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "user_id"}, {Name: "video_id"}},
		DoUpdates: clause.Assignments(map[string]any{
			"last_scene":       gorm.Expr("CASE WHEN " + newerEvent + " THEN EXCLUDED.last_scene ELSE video_view_history.last_scene END"),
			"last_event_type":  gorm.Expr("CASE WHEN " + newerEvent + " THEN EXCLUDED.last_event_type ELSE video_view_history.last_event_type END"),
			"last_watch_ms":    gorm.Expr("CASE WHEN " + newerEvent + " THEN EXCLUDED.last_watch_ms ELSE video_view_history.last_watch_ms END"),
			"completed":        gorm.Expr("CASE WHEN " + newerEvent + " THEN EXCLUDED.completed ELSE video_view_history.completed END"),
			"first_watched_at": gorm.Expr("LEAST(video_view_history.first_watched_at, EXCLUDED.first_watched_at)"),
			"last_watched_at":  gorm.Expr("CASE WHEN " + newerEvent + " THEN EXCLUDED.last_watched_at ELSE video_view_history.last_watched_at END"),
			"last_event_id":    gorm.Expr("CASE WHEN " + newerEvent + " THEN EXCLUDED.last_event_id ELSE video_view_history.last_event_id END"),
			"updated_at":       gorm.Expr("CASE WHEN " + newerEvent + " THEN EXCLUDED.updated_at ELSE video_view_history.updated_at END"),
		}),
	}).Create(&history).Error
}

func ensureReadableVideo(tx *gorm.DB, userID, videoID int64) error {
	var video infravideo.VideoModel
	err := tx.Select("id").
		Where("id = ? AND status = ? AND (visibility = ? OR author_id = ?)", videoID, domainvideo.StatusPublished, domainvideo.VisibilityPublic, userID).
		Take(&video).
		Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return domainexposure.ErrVideoNotFound
		}
		return err
	}
	return nil
}

func (r *Repository) ListHistory(ctx context.Context, userID int64, cursor *domainexposure.HistoryCursor, limit int) ([]*domainexposure.ViewHistory, error) {
	var models []ViewHistoryModel
	query := r.db.WithContext(ctx).Where("user_id = ?", userID)
	if cursor != nil {
		query = query.Where("(last_watched_at < ? OR (last_watched_at = ? AND video_id < ?))", cursor.LastWatchedAt, cursor.LastWatchedAt, cursor.VideoID)
	}
	if err := query.Order("last_watched_at DESC").Order("video_id DESC").Limit(limit).Find(&models).Error; err != nil {
		return nil, err
	}
	items := make([]*domainexposure.ViewHistory, 0, len(models))
	for _, model := range models {
		items = append(items, restoreViewHistory(model))
	}
	return items, nil
}

func (r *Repository) DeleteHistory(ctx context.Context, userID, videoID int64) error {
	return r.db.WithContext(ctx).Where("user_id = ? AND video_id = ?", userID, videoID).Delete(&ViewHistoryModel{}).Error
}

func (r *Repository) ClearHistory(ctx context.Context, userID int64) error {
	return r.db.WithContext(ctx).Where("user_id = ?", userID).Delete(&ViewHistoryModel{}).Error
}

func restoreViewHistory(model ViewHistoryModel) *domainexposure.ViewHistory {
	return domainexposure.RestoreViewHistory(
		model.UserID, model.VideoID, model.LastScene, model.LastEventType,
		model.LastWatchMs, model.Completed, model.FirstWatchedAt, model.LastWatchedAt,
		model.CreatedAt, model.UpdatedAt,
	)
}

func EnsureViewHistory(db *gorm.DB) error {
	return db.Exec(`
		INSERT INTO video_view_history (
			user_id, video_id, last_scene, last_event_type, last_watch_ms, completed,
			first_watched_at, last_watched_at, last_event_id, created_at, updated_at
		)
		SELECT DISTINCT ON (user_id, video_id)
			user_id, video_id, scene, event_type, watch_ms, completed,
			MIN(created_at) OVER (PARTITION BY user_id, video_id),
			created_at, id, NOW(), NOW()
		FROM video_view_events
		WHERE event_type IN ('play', 'complete', 'skip')
		ORDER BY user_id, video_id, created_at DESC, id DESC
		ON CONFLICT (user_id, video_id) DO UPDATE SET
			first_watched_at = LEAST(video_view_history.first_watched_at, EXCLUDED.first_watched_at),
			last_scene = CASE WHEN
				(video_view_history.last_watched_at, video_view_history.last_event_id)
					< (EXCLUDED.last_watched_at, EXCLUDED.last_event_id)
				THEN EXCLUDED.last_scene ELSE video_view_history.last_scene END,
			last_event_type = CASE WHEN
				(video_view_history.last_watched_at, video_view_history.last_event_id)
					< (EXCLUDED.last_watched_at, EXCLUDED.last_event_id)
				THEN EXCLUDED.last_event_type ELSE video_view_history.last_event_type END,
			last_watch_ms = CASE WHEN
				(video_view_history.last_watched_at, video_view_history.last_event_id)
					< (EXCLUDED.last_watched_at, EXCLUDED.last_event_id)
				THEN EXCLUDED.last_watch_ms ELSE video_view_history.last_watch_ms END,
			completed = CASE WHEN
				(video_view_history.last_watched_at, video_view_history.last_event_id)
					< (EXCLUDED.last_watched_at, EXCLUDED.last_event_id)
				THEN EXCLUDED.completed ELSE video_view_history.completed END,
			last_watched_at = GREATEST(video_view_history.last_watched_at, EXCLUDED.last_watched_at),
			last_event_id = CASE WHEN
				(video_view_history.last_watched_at, video_view_history.last_event_id)
					< (EXCLUDED.last_watched_at, EXCLUDED.last_event_id)
				THEN EXCLUDED.last_event_id ELSE video_view_history.last_event_id END,
			updated_at = CASE WHEN
				(video_view_history.last_watched_at, video_view_history.last_event_id)
					< (EXCLUDED.last_watched_at, EXCLUDED.last_event_id)
				THEN NOW() ELSE video_view_history.updated_at END
	`).Error
}

func restoreViewEvent(model ViewEventModel) *domainexposure.ViewEvent {
	return domainexposure.RestoreViewEvent(
		model.ID,
		model.UserID,
		model.VideoID,
		model.Scene,
		stringValue(model.RequestID),
		model.EventType,
		model.WatchMs,
		model.Completed,
		model.CreatedAt,
	)
}

func restoreExposure(model ExposureModel) *domainexposure.Exposure {
	return domainexposure.RestoreExposure(
		model.ID,
		model.UserID,
		model.VideoID,
		model.FirstExposedAt,
		model.LastExposedAt,
		model.ExposureCount,
		model.LastScene,
		model.CreatedAt,
		model.UpdatedAt,
	)
}

func stringPtr(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func mapExposureError(err error) error {
	if errors.Is(err, domainexposure.ErrVideoNotFound) {
		return err
	}
	if infrapersistence.IsDuplicatedKeyError(err) {
		return err
	}
	return err
}
