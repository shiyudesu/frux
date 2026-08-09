package infravideo

import (
	"context"
	"strings"
	"time"

	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
	domainmessage "github.com/shiyudesu/frux/internal/domain/message"
	domainvideo "github.com/shiyudesu/frux/internal/domain/video"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func AppendLifecycleNotification(
	tx *gorm.DB,
	notification domainmessage.LifecycleNotification,
) error {
	return AppendLifecycleNotificationWithReadiness(tx, notification, true)
}

func AppendLifecycleNotificationWithReadiness(
	tx *gorm.DB,
	notification domainmessage.LifecycleNotification,
	deliveryReady bool,
) error {
	if tx == nil ||
		notification.RecipientID <= 0 ||
		notification.EventID == "" ||
		len(notification.EventID) > domainmessage.MaxEventIDLength ||
		notification.OccurredAt.IsZero() {
		return domainmessage.ErrInvalidLifecycle
	}

	if err := domainmessage.ValidateLifecycle(
		notification.Stage,
		notification.Result,
		notification.ReasonCode,
		notification.VideoID,
	); err != nil {
		return err
	}
	model := NotificationOutboxModel{
		EventID: notification.EventID, RecipientID: notification.RecipientID,
		VideoID: notification.VideoID, ReviewVersion: notification.ReviewVersion,
		Stage: notification.Stage, Result: notification.Result,
		ReasonCode: notification.ReasonCode, OccurredAt: notification.OccurredAt,
		DeliveryReady: deliveryReady,
		State:         domainmessage.LifecycleOutboxPending,
		AvailableAt:   notification.OccurredAt,
		CreatedAt:     notification.OccurredAt, UpdatedAt: notification.OccurredAt,
	}
	return tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "event_id"}},
		DoUpdates: clause.Assignments(map[string]any{
			"delivery_ready": gorm.Expr(
				"video_notification_outbox.delivery_ready OR EXCLUDED.delivery_ready",
			),
			"updated_at": notification.OccurredAt,
		}),
	}).Create(&model).Error
}

func LifecyclePublicationTracked(
	tx *gorm.DB,
	videoID int64,
	reviewVersion int,
) (bool, error) {
	if tx == nil || videoID <= 0 || reviewVersion <= 0 {
		return false, domainmessage.ErrInvalidLifecycle
	}
	var count int64
	err := tx.Model(&NotificationOutboxModel{}).
		Where(`video_id = ? AND review_version = ? AND
			(stage = ? OR event_id = ?)`,
			videoID, reviewVersion,
			domainmessage.LifecycleStageSubmitted,
			domainmessage.PublicationEventID(videoID, reviewVersion)).
		Count(&count).Error
	return count > 0, err
}

func (r *Repository) ClaimLifecycleNotifications(
	ctx context.Context,
	leaseOwner string,
	limit int,
	now time.Time,
	leaseUntil time.Time,
) ([]*domainmessage.LifecycleOutboxItem, error) {
	leaseOwner = strings.TrimSpace(leaseOwner)
	if leaseOwner == "" || limit <= 0 {
		return []*domainmessage.LifecycleOutboxItem{}, nil
	}
	items := make([]*domainmessage.LifecycleOutboxItem, 0, limit)
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var models []NotificationOutboxModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where(`state = ? AND available_at <= ? AND
				(lease_until IS NULL OR lease_until <= ?)`,
				domainmessage.LifecycleOutboxPending, now, now).
			Order("available_at ASC").Order("created_at ASC").Order("event_id ASC").
			Limit(limit).Find(&models).Error; err != nil {
			return err
		}

		for index := range models {
			models[index].Attempts++
			models[index].LeaseOwner = leaseOwner
			models[index].LeaseUntil = &leaseUntil
			models[index].UpdatedAt = now
			if err := tx.Model(&NotificationOutboxModel{}).
				Where("event_id = ? AND state = ?", models[index].EventID, domainmessage.LifecycleOutboxPending).
				Updates(map[string]any{
					"attempts":    gorm.Expr("attempts + 1"),
					"lease_owner": leaseOwner, "lease_until": leaseUntil, "updated_at": now,
				}).Error; err != nil {
				return err
			}
			items = append(items, restoreLifecycleOutbox(models[index]))
		}
		return nil
	})
	return items, err
}

func (r *Repository) LifecyclePublicationReady(
	ctx context.Context,
	eventID string,
) (bool, error) {
	var ready bool
	err := r.db.WithContext(ctx).Model(&NotificationOutboxModel{}).
		Select("delivery_ready").
		Where("event_id = ? AND stage = ?",
			strings.TrimSpace(eventID), domainmessage.LifecycleStagePublished).
		Scan(&ready).Error
	return ready, err
}

func (r *Repository) LifecyclePublicationTracked(
	ctx context.Context,
	eventID string,
) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&NotificationOutboxModel{}).
		Where("event_id = ? AND stage = ?",
			strings.TrimSpace(eventID), domainmessage.LifecycleStagePublished).
		Count(&count).Error
	return count > 0, err
}

func (r *Repository) MarkLifecyclePublicationReady(
	ctx context.Context,
	eventID string,
	readyAt time.Time,
) error {
	result := r.db.WithContext(ctx).Model(&NotificationOutboxModel{}).
		Where("event_id = ? AND stage = ?",
			strings.TrimSpace(eventID), domainmessage.LifecycleStagePublished).
		Updates(map[string]any{
			"delivery_ready": true,
			"available_at":   readyAt,
			"updated_at":     readyAt,
		})
	if result.Error != nil {
		return result.Error
	}
	return nil
}

func (r *Repository) MarkLifecycleNotificationDelivered(
	ctx context.Context,
	eventID string,
	leaseOwner string,
	deliveredAt time.Time,
) error {
	return r.db.WithContext(ctx).Model(&NotificationOutboxModel{}).
		Where("event_id = ? AND state = ? AND lease_owner = ?",
			strings.TrimSpace(eventID), domainmessage.LifecycleOutboxPending, strings.TrimSpace(leaseOwner)).
		Updates(map[string]any{
			"state":        domainmessage.LifecycleOutboxDelivered,
			"delivered_at": deliveredAt, "lease_owner": "", "lease_until": nil,
			"last_error": "", "updated_at": deliveredAt,
		}).Error
}

func (r *Repository) MarkLifecycleNotificationFailed(
	ctx context.Context,
	eventID string,
	leaseOwner string,
	availableAt time.Time,
	reason string,
	terminal bool,
) error {
	reason = strings.TrimSpace(reason)
	if len(reason) > 1024 {
		reason = reason[:1024]
	}
	state := domainmessage.LifecycleOutboxPending
	if terminal {
		state = domainmessage.LifecycleOutboxTerminal
	}
	return r.db.WithContext(ctx).Model(&NotificationOutboxModel{}).
		Where("event_id = ? AND state = ? AND lease_owner = ?",
			strings.TrimSpace(eventID), domainmessage.LifecycleOutboxPending, strings.TrimSpace(leaseOwner)).
		Updates(map[string]any{
			"state": state, "available_at": availableAt, "lease_owner": "",
			"lease_until": nil, "last_error": reason, "updated_at": time.Now().UTC(),
		}).Error
}

func (r *Repository) ReconcileLifecyclePublicationNotifications(
	ctx context.Context,
	limit int,
) (int, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	var videos []VideoModel
	if err := r.db.WithContext(ctx).Table("video AS v").
		Select("v.*").
		Where(`v.status = ? AND v.visibility = ? AND v.media_status IN ? AND
			EXISTS (
				SELECT 1 FROM video_notification_outbox lifecycle
				WHERE lifecycle.video_id = v.id
				  AND lifecycle.review_version = v.review_version
			) AND
			(NOT EXISTS (
				SELECT 1 FROM video_notification_outbox published
				WHERE published.event_id = CONCAT('video-published:', v.id, ':', v.review_version)
			) OR NOT EXISTS (
				SELECT 1 FROM video_publication_event_outbox publication
				WHERE publication.event_id = CONCAT('video-published:', v.id, ':', v.review_version)
			))`,
			domainvideo.StatusPublished, domainvideo.VisibilityPublic,
			[]string{domainmedia.MediaStatusLegacyReady, domainmedia.MediaStatusReady}).
		Order("v.updated_at ASC").Order("v.id ASC").Limit(limit).Scan(&videos).Error; err != nil {
		return 0, err
	}
	created := 0
	for _, video := range videos {
		err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var current VideoModel
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("id = ?", video.ID).Take(&current).Error; err != nil {
				return err
			}
			ready, err := publicationDeliveryReady(tx, current)
			if err != nil {
				return err
			}
			if err := AppendPublicationHandoff(tx, current, time.Now().UTC(), ready); err != nil {
				return err
			}
			created++
			return nil
		})
		if err != nil {
			return created, err
		}
	}
	return created, nil
}

func restoreLifecycleOutbox(model NotificationOutboxModel) *domainmessage.LifecycleOutboxItem {
	return &domainmessage.LifecycleOutboxItem{
		LifecycleNotification: domainmessage.LifecycleNotification{
			EventID: model.EventID, RecipientID: model.RecipientID,
			VideoID: model.VideoID, ReviewVersion: model.ReviewVersion,
			Stage: model.Stage, Result: model.Result, ReasonCode: model.ReasonCode,
			OccurredAt: model.OccurredAt,
		},
		State: model.State, Attempts: model.Attempts, AvailableAt: model.AvailableAt,
		LeaseOwner: model.LeaseOwner, LeaseUntil: model.LeaseUntil,
		LastError: model.LastError, DeliveredAt: model.DeliveredAt,
		CreatedAt: model.CreatedAt, UpdatedAt: model.UpdatedAt,
	}
}

func pointerID(value *int64) int64 {
	if value == nil || *value <= 0 {
		return 0
	}
	return *value
}
