package infraexposure

import (
	"context"
	"errors"
	"fmt"
	domainexposure "github.com/shiyudesu/frux/internal/domain/exposure"
	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
	domainvideo "github.com/shiyudesu/frux/internal/domain/video"
	infrapersistence "github.com/shiyudesu/frux/internal/infra/persistence"
	infravideo "github.com/shiyudesu/frux/internal/infra/persistence/video"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Repository struct {
	db *gorm.DB
}

const viewHistoryLockNamespace int64 = 0x4756480000000000

func New(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) FindViewEventByIdentity(ctx context.Context, userID int64, eventID string) (*domainexposure.SaveViewEventResult, error) {
	existing, err := r.findEventByIdentity(ctx, userID, eventID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, mapExposureError(err)
	}
	result, err := r.resultForStoredEvent(ctx, existing)
	if result != nil {
		result.Replayed = true
	}
	return result, err
}

// SaveViewEvent writes the immutable fact, projections, and delivery outbox atomically.
func (r *Repository) SaveViewEvent(ctx context.Context, event *domainexposure.ViewEvent) (*domainexposure.SaveViewEventResult, error) {
	if event.EventID != "" {
		existing, err := r.findEventByIdentity(ctx, event.UserID, event.EventID)
		if err == nil {
			return r.replayResult(ctx, event, existing)
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, mapExposureError(err)
		}
	}

	var eventModel ViewEventModel
	var exposureModel ExposureModel
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Event IDs and Kafka partition keys are user-scoped. Serialize before
		// allocating event/outbox IDs so commit order cannot invert that stream.
		if err := lockViewHistoryUser(tx, event.UserID); err != nil {
			return err
		}
		if err := ensureReadableVideo(tx, event.UserID, event.VideoID); err != nil {
			return err
		}

		eventModel = ViewEventModel{
			UserID:            event.UserID,
			VideoID:           event.VideoID,
			Scene:             event.Scene,
			RequestID:         stringPtr(event.RequestID),
			EventType:         event.EventType,
			EventID:           stringPtr(event.EventID),
			PlaybackSessionID: stringPtr(event.PlaybackSessionID),
			Sequence:          int64Ptr(event.Sequence),
			OccurredAt:        event.OccurredAt,
			PositionMs:        event.PositionMs,
			WatchMs:           event.WatchMs,
			DurationMs:        cloneInt(event.DurationMs),
			Completed:         event.Completed,
		}
		if err := tx.Create(&eventModel).Error; err != nil {
			return err
		}
		if eventModel.EventID == nil {
			legacyID := legacyEventID(eventModel.ID)
			if err := tx.Model(&eventModel).Update("event_id", legacyID).Error; err != nil {
				return err
			}
			eventModel.EventID = &legacyID
		}

		if event.CountsAsHistory() {
			blocked, err := viewHistoryDeletionBlocks(tx, eventModel)
			if err != nil {
				return err
			}
			if !blocked {
				err = upsertViewHistory(tx, eventModel)
			}
			if err != nil {
				return err
			}
		}

		if event.CountsAsExposure() {
			exposureModel = ExposureModel{
				UserID: event.UserID, VideoID: event.VideoID,
				FirstExposedAt: eventModel.CreatedAt, LastExposedAt: eventModel.CreatedAt,
				ExposureCount: 1, LastScene: event.Scene,
			}
			if err := tx.Clauses(clause.OnConflict{
				Columns: []clause.Column{{Name: "user_id"}, {Name: "video_id"}},
				DoUpdates: clause.Assignments(map[string]any{
					"last_exposed_at": gorm.Expr("EXCLUDED.last_exposed_at"),
					"exposure_count":  gorm.Expr("exposures.exposure_count + 1"),
					"last_scene":      gorm.Expr("EXCLUDED.last_scene"),
					"updated_at":      gorm.Expr("EXCLUDED.updated_at"),
				}),
			}).Create(&exposureModel).Error; err != nil {
				return err
			}
			if err := tx.Where("user_id = ? AND video_id = ?", event.UserID, event.VideoID).Take(&exposureModel).Error; err != nil {
				return err
			}
			firstExposedAt := exposureModel.FirstExposedAt
			if err := tx.Model(&eventModel).Updates(map[string]any{
				"exposure_first_at":       firstExposedAt,
				"exposure_count_snapshot": exposureModel.ExposureCount,
			}).Error; err != nil {
				return err
			}
			eventModel.ExposureFirstAt = &firstExposedAt
			eventModel.ExposureCount = exposureModel.ExposureCount
		}

		return tx.Create(&ViewEventOutboxModel{
			EventID: stringValue(eventModel.EventID), ViewEventID: eventModel.ID,
			ExposureCount: exposureModel.ExposureCount, AvailableAt: eventModel.CreatedAt,
		}).Error
	})
	if err != nil {
		if event.EventID != "" && infrapersistence.IsDuplicatedKeyError(err) {
			existing, findErr := r.findEventByIdentity(ctx, event.UserID, event.EventID)
			if findErr == nil {
				return r.replayResult(ctx, event, existing)
			}
		}
		return nil, mapExposureError(err)
	}

	savedEvent := restoreViewEvent(eventModel)
	var exposure *domainexposure.Exposure
	if event.CountsAsExposure() {
		exposure = restoreExposure(exposureModel)
	}
	return &domainexposure.SaveViewEventResult{Event: savedEvent, Exposure: exposure}, nil
}

func upsertViewHistory(tx *gorm.DB, event ViewEventModel) error {
	const newerTuple = "(video_view_history.last_occurred_at, video_view_history.last_event_id) < (EXCLUDED.last_occurred_at, EXCLUDED.last_event_id)"
	const sameSessionNewerSequence = "(video_view_history.last_playback_session_id IS NOT NULL AND EXCLUDED.last_playback_session_id IS NOT NULL AND video_view_history.last_playback_session_id = EXCLUDED.last_playback_session_id AND EXCLUDED.last_occurred_at >= video_view_history.last_occurred_at AND COALESCE(EXCLUDED.last_sequence, 0) > COALESCE(video_view_history.last_sequence, 0))"
	const differentSessionNewerTuple = "((video_view_history.last_playback_session_id IS DISTINCT FROM EXCLUDED.last_playback_session_id OR video_view_history.last_playback_session_id IS NULL OR EXCLUDED.last_playback_session_id IS NULL) AND " + newerTuple + ")"
	const newerEvent = "(" + sameSessionNewerSequence + " OR " + differentSessionNewerTuple + ")"
	history := ViewHistoryModel{
		UserID: event.UserID, VideoID: event.VideoID,
		LastScene: event.Scene, LastEventType: event.EventType,
		LastPositionMs: event.PositionMs, LastWatchMs: event.WatchMs, Completed: event.Completed,
		FirstWatchedAt: event.OccurredAt, LastWatchedAt: event.OccurredAt,
		LastOccurredAt: event.OccurredAt, LastEventID: stringValue(event.EventID),
		LastSessionID: event.PlaybackSessionID, LastSequence: event.Sequence,
	}
	return tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "user_id"}, {Name: "video_id"}},
		DoUpdates: clause.Assignments(map[string]any{
			"last_scene":               gorm.Expr("CASE WHEN " + newerEvent + " THEN EXCLUDED.last_scene ELSE video_view_history.last_scene END"),
			"last_event_type":          gorm.Expr("CASE WHEN " + newerEvent + " THEN EXCLUDED.last_event_type ELSE video_view_history.last_event_type END"),
			"last_position_ms":         gorm.Expr("GREATEST(video_view_history.last_position_ms, EXCLUDED.last_position_ms)"),
			"last_watch_ms":            gorm.Expr("GREATEST(video_view_history.last_watch_ms, EXCLUDED.last_watch_ms)"),
			"completed":                gorm.Expr("video_view_history.completed OR EXCLUDED.completed"),
			"first_watched_at":         gorm.Expr("LEAST(video_view_history.first_watched_at, EXCLUDED.first_watched_at)"),
			"last_watched_at":          gorm.Expr("GREATEST(video_view_history.last_watched_at, EXCLUDED.last_watched_at)"),
			"last_occurred_at":         gorm.Expr("CASE WHEN " + newerEvent + " THEN EXCLUDED.last_occurred_at ELSE video_view_history.last_occurred_at END"),
			"last_event_id":            gorm.Expr("CASE WHEN " + newerEvent + " THEN EXCLUDED.last_event_id ELSE video_view_history.last_event_id END"),
			"last_playback_session_id": gorm.Expr("CASE WHEN " + newerEvent + " THEN EXCLUDED.last_playback_session_id ELSE video_view_history.last_playback_session_id END"),
			"last_sequence":            gorm.Expr("CASE WHEN " + newerEvent + " THEN EXCLUDED.last_sequence ELSE video_view_history.last_sequence END"),
			"updated_at":               gorm.Expr("CASE WHEN " + newerEvent + " THEN EXCLUDED.updated_at ELSE video_view_history.updated_at END"),
		}),
	}).Create(&history).Error
}

func (r *Repository) findEventByIdentity(ctx context.Context, userID int64, eventID string) (ViewEventModel, error) {
	var model ViewEventModel
	err := r.db.WithContext(ctx).
		Where("user_id = ? AND event_id = ?", userID, eventID).
		Take(&model).
		Error
	return model, err
}

func (r *Repository) replayResult(ctx context.Context, incoming *domainexposure.ViewEvent, existing ViewEventModel) (*domainexposure.SaveViewEventResult, error) {
	saved := restoreViewEvent(existing)
	if !saved.SameNormalizedPayload(incoming) {
		return nil, domainexposure.ErrEventIDConflict
	}
	result, err := r.resultForStoredEvent(ctx, existing)
	if result != nil {
		result.Replayed = true
	}
	return result, err
}

func (r *Repository) resultForStoredEvent(ctx context.Context, existing ViewEventModel) (*domainexposure.SaveViewEventResult, error) {
	saved := restoreViewEvent(existing)
	var exposure *domainexposure.Exposure
	if saved.CountsAsExposure() {
		if existing.ExposureFirstAt != nil && existing.ExposureCount > 0 {
			exposure = domainexposure.RestoreExposure(
				0,
				saved.UserID,
				saved.VideoID,
				existing.ExposureFirstAt.UTC(),
				existing.CreatedAt.UTC(),
				existing.ExposureCount,
				saved.Scene,
				existing.ExposureFirstAt.UTC(),
				existing.CreatedAt.UTC(),
			)
		} else {
			var model ExposureModel
			if err := r.db.WithContext(ctx).Where("user_id = ? AND video_id = ?", saved.UserID, saved.VideoID).Take(&model).Error; err != nil {
				return nil, err
			}
			exposure = restoreExposure(model)
		}
	}
	return &domainexposure.SaveViewEventResult{Event: saved, Exposure: exposure}, nil
}

func ensureReadableVideo(tx *gorm.DB, userID, videoID int64) error {
	var video infravideo.VideoModel
	err := tx.Select("id").
		Where("id = ? AND status = ? AND media_status IN ? AND (visibility = ? OR author_id = ?)", videoID, domainvideo.StatusPublished, []string{domainmedia.MediaStatusLegacyReady, domainmedia.MediaStatusReady}, domainvideo.VisibilityPublic, userID).
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
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockViewHistoryUser(tx, userID); err != nil {
			return err
		}
		if err := upsertViewHistoryDeletion(tx, userID, videoID, time.Now().UTC()); err != nil {
			return err
		}
		return tx.Where("user_id = ? AND video_id = ?", userID, videoID).Delete(&ViewHistoryModel{}).Error
	})
}

func (r *Repository) ClearHistory(ctx context.Context, userID int64) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockViewHistoryUser(tx, userID); err != nil {
			return err
		}
		if err := upsertViewHistoryDeletion(tx, userID, 0, time.Now().UTC()); err != nil {
			return err
		}
		return tx.Where("user_id = ?", userID).Delete(&ViewHistoryModel{}).Error
	})
}

func lockViewHistoryUser(tx *gorm.DB, userID int64) error {
	return tx.Exec("SELECT pg_advisory_xact_lock(?)", viewHistoryLockNamespace+userID).Error
}

func viewHistoryDeletionBlocks(tx *gorm.DB, event ViewEventModel) (bool, error) {
	var result struct {
		Latest *time.Time
	}
	err := tx.Model(&ViewHistoryDeletionModel{}).
		Select("MAX(deleted_at) AS latest").
		Where("user_id = ? AND video_id IN ?", event.UserID, []int64{0, event.VideoID}).
		Scan(&result).
		Error
	if err != nil || result.Latest == nil {
		return false, err
	}
	return !event.OccurredAt.After(result.Latest.UTC().Add(domainexposure.MaxFutureOccurrenceSkew)), nil
}

func upsertViewHistoryDeletion(tx *gorm.DB, userID, videoID int64, deletedAt time.Time) error {
	return tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "user_id"}, {Name: "video_id"}},
		DoUpdates: clause.Assignments(map[string]any{
			"deleted_at": gorm.Expr("GREATEST(video_view_history_deletion.deleted_at, EXCLUDED.deleted_at)"),
		}),
	}).Create(&ViewHistoryDeletionModel{
		UserID: userID, VideoID: videoID, DeletedAt: deletedAt,
	}).Error
}

func restoreViewHistory(model ViewHistoryModel) *domainexposure.ViewHistory {
	return domainexposure.RestoreViewHistory(
		model.UserID, model.VideoID, model.LastScene, model.LastEventType,
		model.LastPositionMs, model.LastWatchMs, model.Completed,
		model.FirstWatchedAt, model.LastWatchedAt, model.LastOccurredAt,
		model.LastEventID, stringValue(model.LastSessionID), int64Value(model.LastSequence),
		model.CreatedAt, model.UpdatedAt,
	)
}

func EnsureViewHistory(db *gorm.DB) error {
	return db.Exec(`
		WITH history_events AS (
			SELECT *
			FROM video_view_events
			WHERE event_type IN ('play', 'progress', 'complete', 'skip')
		),
		session_latest AS (
			SELECT DISTINCT ON (
				user_id,
				video_id,
				CASE
					WHEN playback_session_id IS NULL THEN 'legacy:' || event_id
					ELSE 'session:' || playback_session_id
				END
			) *
			FROM history_events
			ORDER BY
				user_id,
				video_id,
				CASE
					WHEN playback_session_id IS NULL THEN 'legacy:' || event_id
					ELSE 'session:' || playback_session_id
				END,
				occurred_at DESC,
				sequence DESC NULLS LAST,
				event_id DESC
		),
		latest AS (
			SELECT DISTINCT ON (user_id, video_id) *
			FROM session_latest
			ORDER BY user_id, video_id, occurred_at DESC, event_id DESC
		),
		aggregates AS (
			SELECT
				user_id,
				video_id,
				MIN(occurred_at) AS first_watched_at,
				MAX(position_ms) AS max_position_ms,
				MAX(watch_ms) AS max_watch_ms,
				BOOL_OR(completed) AS ever_completed
			FROM history_events
			GROUP BY user_id, video_id
		)
		INSERT INTO video_view_history (
			user_id, video_id, last_scene, last_event_type, last_position_ms, last_watch_ms, completed,
			first_watched_at, last_watched_at, last_occurred_at, last_event_id,
			last_playback_session_id, last_sequence, created_at, updated_at
		)
		SELECT
			latest.user_id, latest.video_id, latest.scene, latest.event_type,
			aggregates.max_position_ms, aggregates.max_watch_ms, aggregates.ever_completed,
			aggregates.first_watched_at,
			latest.occurred_at, latest.occurred_at, latest.event_id,
			latest.playback_session_id, latest.sequence, NOW(), NOW()
		FROM latest
		JOIN aggregates USING (user_id, video_id)
		ON CONFLICT (user_id, video_id) DO UPDATE SET
			first_watched_at = LEAST(video_view_history.first_watched_at, EXCLUDED.first_watched_at),
			last_scene = CASE WHEN
				(
					video_view_history.last_playback_session_id IS NOT NULL
					AND EXCLUDED.last_playback_session_id IS NOT NULL
					AND video_view_history.last_playback_session_id = EXCLUDED.last_playback_session_id
					AND EXCLUDED.last_occurred_at >= video_view_history.last_occurred_at
					AND COALESCE(EXCLUDED.last_sequence, 0) > COALESCE(video_view_history.last_sequence, 0)
				) OR (
					(
						video_view_history.last_playback_session_id IS DISTINCT FROM EXCLUDED.last_playback_session_id
						OR video_view_history.last_playback_session_id IS NULL
						OR EXCLUDED.last_playback_session_id IS NULL
					)
					AND (video_view_history.last_occurred_at, video_view_history.last_event_id)
						< (EXCLUDED.last_occurred_at, EXCLUDED.last_event_id)
				)
				THEN EXCLUDED.last_scene ELSE video_view_history.last_scene END,
			last_event_type = CASE WHEN
				(
					video_view_history.last_playback_session_id IS NOT NULL
					AND EXCLUDED.last_playback_session_id IS NOT NULL
					AND video_view_history.last_playback_session_id = EXCLUDED.last_playback_session_id
					AND EXCLUDED.last_occurred_at >= video_view_history.last_occurred_at
					AND COALESCE(EXCLUDED.last_sequence, 0) > COALESCE(video_view_history.last_sequence, 0)
				) OR (
					(
						video_view_history.last_playback_session_id IS DISTINCT FROM EXCLUDED.last_playback_session_id
						OR video_view_history.last_playback_session_id IS NULL
						OR EXCLUDED.last_playback_session_id IS NULL
					)
					AND (video_view_history.last_occurred_at, video_view_history.last_event_id)
						< (EXCLUDED.last_occurred_at, EXCLUDED.last_event_id)
				)
				THEN EXCLUDED.last_event_type ELSE video_view_history.last_event_type END,
			last_position_ms = GREATEST(video_view_history.last_position_ms, EXCLUDED.last_position_ms),
			last_watch_ms = GREATEST(video_view_history.last_watch_ms, EXCLUDED.last_watch_ms),
			completed = video_view_history.completed OR EXCLUDED.completed,
			last_watched_at = GREATEST(video_view_history.last_watched_at, EXCLUDED.last_watched_at),
			last_occurred_at = CASE WHEN
				video_view_history.last_playback_session_id = EXCLUDED.last_playback_session_id
					AND EXCLUDED.last_occurred_at >= video_view_history.last_occurred_at
					AND COALESCE(EXCLUDED.last_sequence, 0) > COALESCE(video_view_history.last_sequence, 0)
				THEN EXCLUDED.last_occurred_at
				ELSE GREATEST(video_view_history.last_occurred_at, EXCLUDED.last_occurred_at)
			END,
			last_event_id = CASE WHEN
				(
					video_view_history.last_playback_session_id = EXCLUDED.last_playback_session_id
					AND EXCLUDED.last_occurred_at >= video_view_history.last_occurred_at
					AND COALESCE(EXCLUDED.last_sequence, 0) > COALESCE(video_view_history.last_sequence, 0)
				) OR (
					(
						video_view_history.last_playback_session_id IS DISTINCT FROM EXCLUDED.last_playback_session_id
						OR video_view_history.last_playback_session_id IS NULL
						OR EXCLUDED.last_playback_session_id IS NULL
					)
					AND (video_view_history.last_occurred_at, video_view_history.last_event_id)
						< (EXCLUDED.last_occurred_at, EXCLUDED.last_event_id)
				)
				THEN EXCLUDED.last_event_id ELSE video_view_history.last_event_id END,
			last_playback_session_id = CASE WHEN
				(
					video_view_history.last_playback_session_id = EXCLUDED.last_playback_session_id
					AND EXCLUDED.last_occurred_at >= video_view_history.last_occurred_at
					AND COALESCE(EXCLUDED.last_sequence, 0) > COALESCE(video_view_history.last_sequence, 0)
				) OR (
					(
						video_view_history.last_playback_session_id IS DISTINCT FROM EXCLUDED.last_playback_session_id
						OR video_view_history.last_playback_session_id IS NULL
						OR EXCLUDED.last_playback_session_id IS NULL
					)
					AND (video_view_history.last_occurred_at, video_view_history.last_event_id)
						< (EXCLUDED.last_occurred_at, EXCLUDED.last_event_id)
				)
				THEN EXCLUDED.last_playback_session_id ELSE video_view_history.last_playback_session_id END,
			last_sequence = CASE WHEN
				(
					video_view_history.last_playback_session_id = EXCLUDED.last_playback_session_id
					AND EXCLUDED.last_occurred_at >= video_view_history.last_occurred_at
					AND COALESCE(EXCLUDED.last_sequence, 0) > COALESCE(video_view_history.last_sequence, 0)
				) OR (
					(
						video_view_history.last_playback_session_id IS DISTINCT FROM EXCLUDED.last_playback_session_id
						OR video_view_history.last_playback_session_id IS NULL
						OR EXCLUDED.last_playback_session_id IS NULL
					)
					AND (video_view_history.last_occurred_at, video_view_history.last_event_id)
						< (EXCLUDED.last_occurred_at, EXCLUDED.last_event_id)
				)
				THEN EXCLUDED.last_sequence ELSE video_view_history.last_sequence END,
			updated_at = CASE WHEN
				(
					video_view_history.last_playback_session_id = EXCLUDED.last_playback_session_id
					AND EXCLUDED.last_occurred_at >= video_view_history.last_occurred_at
					AND COALESCE(EXCLUDED.last_sequence, 0) > COALESCE(video_view_history.last_sequence, 0)
				) OR (
					(
						video_view_history.last_playback_session_id IS DISTINCT FROM EXCLUDED.last_playback_session_id
						OR video_view_history.last_playback_session_id IS NULL
						OR EXCLUDED.last_playback_session_id IS NULL
					)
					AND (video_view_history.last_occurred_at, video_view_history.last_event_id)
						< (EXCLUDED.last_occurred_at, EXCLUDED.last_event_id)
				)
				THEN NOW() ELSE video_view_history.updated_at END
	`).Error
}

// RepairExistingViewHistoryAggregates fixes monotonic progress/completion only for
// projections that still exist, so user-deleted history is never recreated.
func RepairExistingViewHistoryAggregates(db *gorm.DB) error {
	return db.Exec(`
		WITH aggregates AS (
			SELECT
				user_id,
				video_id,
				MAX(position_ms) AS max_position_ms,
				MAX(watch_ms) AS max_watch_ms,
				BOOL_OR(completed) AS ever_completed
			FROM video_view_events
			WHERE event_type IN ('play', 'progress', 'complete', 'skip')
			GROUP BY user_id, video_id
		)
		UPDATE video_view_history AS history
		SET last_position_ms = GREATEST(history.last_position_ms, aggregates.max_position_ms),
			last_watch_ms = GREATEST(history.last_watch_ms, aggregates.max_watch_ms),
			completed = history.completed OR aggregates.ever_completed,
			updated_at = CASE
				WHEN history.last_position_ms < aggregates.max_position_ms
					OR history.last_watch_ms < aggregates.max_watch_ms
					OR (NOT history.completed AND aggregates.ever_completed)
				THEN NOW()
				ELSE history.updated_at
			END
		FROM aggregates
		WHERE history.user_id = aggregates.user_id
			AND history.video_id = aggregates.video_id
	`).Error
}

func EnsureViewEventEnvelope(db *gorm.DB) error {
	if err := db.Exec(`
		UPDATE video_view_events
		SET event_id = 'legacy-' || id::text
		WHERE event_id IS NULL OR event_id = ''
	`).Error; err != nil {
		return err
	}
	if err := db.Exec(`
		UPDATE video_view_events
		SET occurred_at = created_at
		WHERE event_id LIKE 'legacy-%'
			AND playback_session_id IS NULL
	`).Error; err != nil {
		return err
	}
	if err := db.Exec(`
		UPDATE video_view_events
		SET position_ms = watch_ms
		WHERE position_ms = 0 AND watch_ms > 0
	`).Error; err != nil {
		return err
	}
	if err := db.Exec(`
		WITH snapshots AS (
			SELECT
				id,
				MIN(created_at) OVER (PARTITION BY user_id, video_id) AS first_exposed_at,
				COUNT(*) OVER (
					PARTITION BY user_id, video_id
					ORDER BY created_at, id
					ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW
				) AS exposure_count
			FROM video_view_events
			WHERE event_type = 'exposed'
		)
		UPDATE video_view_events AS event
		SET exposure_first_at = snapshots.first_exposed_at,
			exposure_count_snapshot = snapshots.exposure_count
		FROM snapshots
		WHERE event.id = snapshots.id
			AND (event.exposure_first_at IS NULL OR event.exposure_count_snapshot = 0)
	`).Error; err != nil {
		return err
	}
	return db.Exec(`
		UPDATE video_view_history AS history
		SET last_position_ms = GREATEST(history.last_position_ms, event.position_ms),
			last_watch_ms = GREATEST(history.last_watch_ms, event.watch_ms),
			last_occurred_at = event.occurred_at,
			last_watched_at = event.occurred_at,
			last_event_id = event.event_id,
			last_playback_session_id = event.playback_session_id,
			last_sequence = event.sequence,
			updated_at = NOW()
		FROM video_view_events AS event
		WHERE history.user_id = event.user_id
			AND history.video_id = event.video_id
			AND history.last_event_id = event.event_id
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
		stringValue(model.EventID),
		stringValue(model.PlaybackSessionID),
		int64Value(model.Sequence),
		model.OccurredAt,
		model.PositionMs,
		model.WatchMs,
		model.DurationMs,
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

func int64Ptr(value int64) *int64 {
	if value <= 0 {
		return nil
	}
	return &value
}

func int64Value(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func cloneInt(value *int) *int {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func legacyEventID(id int64) string {
	return fmt.Sprintf("legacy-%d", id)
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
