package infravideo

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	applicationvideo "github.com/shiyudesu/frux/internal/application/video"
	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
	domainmessage "github.com/shiyudesu/frux/internal/domain/message"
	domainvideo "github.com/shiyudesu/frux/internal/domain/video"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (r *Repository) EnsurePublicationEvent(
	ctx context.Context,
	event *applicationvideo.PublishedEvent,
	readyAt time.Time,
) error {
	if r == nil || r.db == nil || !validPublicationEvent(event) {
		return domainvideo.ErrInvalidVideoID
	}
	if readyAt.IsZero() {
		readyAt = time.Now().UTC()
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var video VideoModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", event.VideoID).Take(&video).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return domainvideo.ErrVideoNotFound
			}
			return err
		}
		if video.Status != domainvideo.StatusPublished ||
			video.Visibility != domainvideo.VisibilityPublic ||
			!domainmedia.IsPublicReadyStatus(video.MediaStatus) ||
			video.PublishedAt == nil ||
			!video.PublishedAt.UTC().Equal(event.PublishedAt.UTC()) ||
			domainmessage.PublicationEventID(video.ID, video.ReviewVersion) != event.EventID {
			return domainvideo.ErrVideoStateNotAllowed
		}
		model := PublicationEventOutboxModel{
			EventID: event.EventID, VideoID: event.VideoID,
			EventType: publicationOutboxEventType, PayloadJSON: string(payload),
			AvailableAt: readyAt.UTC(), CreatedAt: readyAt.UTC(), UpdatedAt: readyAt.UTC(),
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&model).Error; err != nil {
			return err
		}
		return tx.Model(&NotificationOutboxModel{}).
			Where("event_id = ? AND stage = ?", event.EventID, domainmessage.LifecycleStagePublished).
			Updates(map[string]any{
				"delivery_ready": true, "available_at": readyAt.UTC(), "updated_at": readyAt.UTC(),
			}).Error
	})
}

func (r *Repository) ClaimPublicationEvents(
	ctx context.Context,
	leaseOwner string,
	limit int,
	now time.Time,
	leaseUntil time.Time,
) ([]*applicationvideo.PublicationOutboxItem, error) {
	leaseOwner = strings.TrimSpace(leaseOwner)
	if leaseOwner == "" || limit <= 0 {
		return []*applicationvideo.PublicationOutboxItem{}, nil
	}
	items := make([]*applicationvideo.PublicationOutboxItem, 0, limit)
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var models []PublicationEventOutboxModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where(`dispatched_at IS NULL AND available_at <= ? AND
				(lease_until IS NULL OR lease_until <= ?)`, now, now).
			Order("available_at ASC").Order("created_at ASC").Order("event_id ASC").
			Limit(limit).Find(&models).Error; err != nil {
			return err
		}
		for index := range models {
			models[index].Attempts++
			models[index].LeaseOwner = leaseOwner
			models[index].LeaseUntil = &leaseUntil
			if err := tx.Model(&PublicationEventOutboxModel{}).
				Where("event_id = ? AND dispatched_at IS NULL", models[index].EventID).
				Updates(map[string]any{
					"attempts": gorm.Expr("attempts + 1"), "lease_owner": leaseOwner,
					"lease_until": leaseUntil, "updated_at": now,
				}).Error; err != nil {
				return err
			}
			item, err := restorePublicationOutbox(models[index])
			if err != nil {
				return err
			}
			items = append(items, item)
		}
		return nil
	})
	return items, err
}

func (r *Repository) MarkPublicationEventDispatched(
	ctx context.Context,
	eventID string,
	leaseOwner string,
	dispatchedAt time.Time,
) error {
	result := r.db.WithContext(ctx).Model(&PublicationEventOutboxModel{}).
		Where("event_id = ? AND dispatched_at IS NULL AND lease_owner = ?",
			strings.TrimSpace(eventID), strings.TrimSpace(leaseOwner)).
		Updates(map[string]any{
			"dispatched_at": dispatchedAt.UTC(), "lease_owner": "", "lease_until": nil,
			"last_error_class": "", "updated_at": dispatchedAt.UTC(),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return applicationvideo.ErrPublicationOutboxLeaseLost
	}
	return nil
}

func (r *Repository) MarkPublicationEventFailed(
	ctx context.Context,
	eventID string,
	leaseOwner string,
	availableAt time.Time,
	errorClass string,
) error {
	errorClass = strings.TrimSpace(errorClass)
	if len(errorClass) > 32 {
		errorClass = errorClass[:32]
	}
	result := r.db.WithContext(ctx).Model(&PublicationEventOutboxModel{}).
		Where("event_id = ? AND dispatched_at IS NULL AND lease_owner = ?",
			strings.TrimSpace(eventID), strings.TrimSpace(leaseOwner)).
		Updates(map[string]any{
			"available_at": availableAt.UTC(), "lease_owner": "", "lease_until": nil,
			"last_error_class": errorClass, "updated_at": time.Now().UTC(),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return applicationvideo.ErrPublicationOutboxLeaseLost
	}
	return nil
}

func (r *Repository) PublicationOutboxStats(
	ctx context.Context,
	now time.Time,
) (applicationvideo.PublicationOutboxStats, error) {
	var result struct {
		Pending       int64
		OldestPending *time.Time
	}

	err := r.db.WithContext(ctx).Model(&PublicationEventOutboxModel{}).
		Select("COUNT(*) AS pending, MIN(created_at) AS oldest_pending").
		Where("dispatched_at IS NULL").Scan(&result).Error
	return applicationvideo.PublicationOutboxStats{
		Pending: result.Pending, OldestPending: result.OldestPending,
	}, err
}

func (r *Repository) CleanupPublicationEvents(
	ctx context.Context,
	dispatchedBefore time.Time,
	limit int,
) (int64, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	var eventIDs []string
	if err := r.db.WithContext(ctx).Model(&PublicationEventOutboxModel{}).
		Where("dispatched_at < ?", dispatchedBefore.UTC()).
		Order("dispatched_at ASC").Limit(limit).Pluck("event_id", &eventIDs).Error; err != nil {
		return 0, err
	}
	if len(eventIDs) == 0 {
		return 0, nil
	}
	result := r.db.WithContext(ctx).
		Where("event_id IN ? AND dispatched_at < ?", eventIDs, dispatchedBefore.UTC()).
		Delete(&PublicationEventOutboxModel{})
	return result.RowsAffected, result.Error
}

func (r *Repository) ReconcilePublicationEvents(
	ctx context.Context,
	limit int,
	now time.Time,
) (int, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	var videos []VideoModel
	if err := r.db.WithContext(ctx).
		Where(`status = ? AND visibility = ? AND media_status IN ? AND published_at IS NOT NULL AND
			EXISTS (
				SELECT 1 FROM video_notification_outbox notification
				WHERE notification.event_id = CONCAT('video-published:', video.id, ':', video.review_version)
				  AND notification.stage = ?
				  AND (notification.state = ? OR notification.delivery_ready = FALSE)
			) AND
			NOT EXISTS (
				SELECT 1 FROM video_publication_event_outbox outbox
				WHERE outbox.event_id = CONCAT('video-published:', video.id, ':', video.review_version)
			)`,
			domainvideo.StatusPublished, domainvideo.VisibilityPublic,
			[]string{domainmedia.MediaStatusLegacyReady, domainmedia.MediaStatusReady},
			domainmessage.LifecycleStagePublished,
			domainmessage.LifecycleOutboxPending).
		Order("published_at ASC").Order("id ASC").Limit(limit).Find(&videos).Error; err != nil {
		return 0, err
	}
	created := 0
	for _, model := range videos {
		video := restoreVideo(videoWithStatModel{
			ID: model.ID, AuthorID: model.AuthorID, Title: model.Title,
			Description: model.Description, MediaURL: model.MediaURL, CoverURL: model.CoverURL,
			MediaAssetID: model.MediaAssetID, CoverAssetID: model.CoverAssetID,
			MediaStatus: model.MediaStatus, MediaErrorCode: model.MediaErrorCode,
			ReviewVersion: model.ReviewVersion, Version: model.Version, Status: model.Status,
			Visibility: model.Visibility, PublishedAt: model.PublishedAt,
			CreatedAt: model.CreatedAt, UpdatedAt: model.UpdatedAt,
		})
		event := applicationvideo.NewPublishedEvent(video)
		if event == nil {
			continue
		}
		event.OccurredAt = model.PublishedAt.UTC()
		before := int64(0)
		if err := r.db.WithContext(ctx).Model(&PublicationEventOutboxModel{}).
			Where("event_id = ?", event.EventID).Count(&before).Error; err != nil {
			return created, err
		}
		if err := r.EnsurePublicationEvent(ctx, event, now); err != nil {
			return created, err
		}
		if before == 0 {
			created++
		}
	}
	return created, nil
}

func restorePublicationOutbox(
	model PublicationEventOutboxModel,
) (*applicationvideo.PublicationOutboxItem, error) {
	var event applicationvideo.PublishedEvent
	if err := json.Unmarshal([]byte(model.PayloadJSON), &event); err != nil {
		return nil, err
	}
	return &applicationvideo.PublicationOutboxItem{
		Event: &event, Attempts: model.Attempts, AvailableAt: model.AvailableAt,
		LeaseOwner: model.LeaseOwner, LeaseUntil: model.LeaseUntil, CreatedAt: model.CreatedAt,
	}, nil
}

func validPublicationEvent(event *applicationvideo.PublishedEvent) bool {
	return event != nil && event.EventID != "" && len(event.EventID) <= 128 &&
		event.VideoID > 0 && event.AuthorID > 0 &&
		!event.PublishedAt.IsZero() && !event.OccurredAt.IsZero()
}

const publicationOutboxEventType = "video_published.v1"
