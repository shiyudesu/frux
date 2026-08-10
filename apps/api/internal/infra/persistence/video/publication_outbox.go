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
		return AppendPublicationHandoff(tx, video, readyAt, true)
	})
}

func AppendPublicationHandoff(
	tx *gorm.DB,
	video VideoModel,
	readyAt time.Time,
	deliveryReady bool,
) error {
	if tx == nil || video.Status != domainvideo.StatusPublished ||
		video.Visibility != domainvideo.VisibilityPublic ||
		!domainmedia.IsPublicReadyStatus(video.MediaStatus) ||
		video.PublishedAt == nil || video.ReviewVersion <= 0 {
		return domainvideo.ErrVideoStateNotAllowed
	}
	if readyAt.IsZero() {
		readyAt = video.PublishedAt.UTC()
	}
	event := &applicationvideo.PublishedEvent{
		EventID:     domainmessage.PublicationEventID(video.ID, video.ReviewVersion),
		VideoID:     video.ID,
		AuthorID:    video.AuthorID,
		Title:       strings.TrimSpace(video.Title),
		Description: strings.TrimSpace(video.Description),
		MediaURL:    strings.TrimSpace(video.MediaURL),
		CoverURL:    strings.TrimSpace(video.CoverURL),
		PublishedAt: video.PublishedAt.UTC(),
		OccurredAt:  video.PublishedAt.UTC(),
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	if err := AppendLifecycleNotificationWithReadiness(tx, domainmessage.LifecycleNotification{
		EventID: event.EventID, RecipientID: video.AuthorID, VideoID: video.ID,
		ReviewVersion: video.ReviewVersion,
		Stage:         domainmessage.LifecycleStagePublished,
		Result:        domainmessage.LifecycleResultPublic,
		OccurredAt:    event.OccurredAt,
	}, deliveryReady); err != nil {
		return err
	}
	fact := &PublicationEventFactModel{
		EventID: event.EventID, VideoID: event.VideoID,
		EventType: publicationOutboxEventType, PayloadJSON: string(payload),
		PublishedAt: event.PublishedAt, OccurredAt: event.OccurredAt,
		CreatedAt: readyAt.UTC(),
	}
	factResult := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(fact)
	if factResult.Error != nil {
		return factResult.Error
	}
	model := &PublicationEventOutboxModel{
		EventID: event.EventID, VideoID: event.VideoID,
		EventType: publicationOutboxEventType, PayloadJSON: string(payload),
		DeliveryReady: deliveryReady, AvailableAt: readyAt.UTC(),
		CreatedAt: readyAt.UTC(), UpdatedAt: readyAt.UTC(),
	}
	if factResult.RowsAffected == 0 {
		return tx.Model(&PublicationEventOutboxModel{}).
			Where("event_id = ? AND dispatched_at IS NULL", event.EventID).
			Updates(map[string]any{
				"payload_json": string(payload),
				"delivery_ready": gorm.Expr(
					"video_publication_event_outbox.delivery_ready OR ?", deliveryReady,
				),
				"available_at": gorm.Expr(
					"CASE WHEN ? THEN ? ELSE video_publication_event_outbox.available_at END",
					deliveryReady, readyAt.UTC(),
				),
				"updated_at": readyAt.UTC(),
			}).Error
	}
	return tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "event_id"}},
		DoUpdates: clause.Assignments(map[string]any{
			"payload_json": gorm.Expr(
				"CASE WHEN video_publication_event_outbox.dispatched_at IS NULL THEN EXCLUDED.payload_json ELSE video_publication_event_outbox.payload_json END",
			),
			"delivery_ready": gorm.Expr(
				"video_publication_event_outbox.delivery_ready OR EXCLUDED.delivery_ready",
			),
			"available_at": gorm.Expr(
				"CASE WHEN EXCLUDED.delivery_ready THEN EXCLUDED.available_at ELSE video_publication_event_outbox.available_at END",
			),
			"updated_at": readyAt.UTC(),
		}),
	}).Create(model).Error
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
			Where(`dispatched_at IS NULL AND delivery_ready = TRUE AND available_at <= ? AND
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
		Where(`dispatched_at < ? AND EXISTS (
			SELECT 1 FROM video_publication_event_fact fact
			WHERE fact.event_id = video_publication_event_outbox.event_id
		)`, dispatchedBefore.UTC()).
		Order("dispatched_at ASC").Limit(limit).Pluck("event_id", &eventIDs).Error; err != nil {
		return 0, err
	}
	if len(eventIDs) == 0 {
		return 0, nil
	}
	result := r.db.WithContext(ctx).
		Where(`event_id IN ? AND dispatched_at < ? AND EXISTS (
			SELECT 1 FROM video_publication_event_fact fact
			WHERE fact.event_id = video_publication_event_outbox.event_id
		)`, eventIDs, dispatchedBefore.UTC()).
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
				SELECT 1 FROM video_notification_outbox lifecycle
				WHERE lifecycle.video_id = video.id
				  AND lifecycle.review_version = video.review_version
			) AND
			NOT EXISTS (
				SELECT 1 FROM video_publication_event_fact fact
				WHERE fact.event_id = CONCAT('video-published:', video.id, ':', video.review_version)
			)`,
			domainvideo.StatusPublished, domainvideo.VisibilityPublic,
			[]string{domainmedia.MediaStatusLegacyReady, domainmedia.MediaStatusReady}).
		Order("published_at ASC").Order("id ASC").Limit(limit).Find(&videos).Error; err != nil {
		return 0, err
	}
	created := 0
	for _, model := range videos {
		before := int64(0)
		if err := r.db.WithContext(ctx).Model(&PublicationEventOutboxModel{}).
			Where("event_id = ?", domainmessage.PublicationEventID(model.ID, model.ReviewVersion)).
			Count(&before).Error; err != nil {
			return created, err
		}
		if err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var current VideoModel
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("id = ?", model.ID).Take(&current).Error; err != nil {
				return err
			}
			ready, err := publicationDeliveryReady(tx, current)
			if err != nil {
				return err
			}
			return AppendPublicationHandoff(tx, current, now, ready)
		}); err != nil {
			return created, err
		}
		if before == 0 {
			created++
		}
	}
	return created, nil
}

func publicationDeliveryReady(tx *gorm.DB, video VideoModel) (bool, error) {
	if video.MediaAssetID == nil || *video.MediaAssetID <= 0 {
		return true, nil
	}
	var count int64
	err := tx.Table("media_variant").
		Where(`asset_id = ? AND role = ? AND state = ? AND public = TRUE`,
			*video.MediaAssetID, domainmedia.VariantRoleBaseline, domainmedia.VariantStateReady).
		Count(&count).Error
	return count > 0, err
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
