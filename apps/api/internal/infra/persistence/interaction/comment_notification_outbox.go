package infrainteraction

import (
	"context"
	domaininteraction "github.com/shiyudesu/frux/internal/domain/interaction"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func createCommentNotificationOutbox(tx *gorm.DB, notification *domaininteraction.CommentNotification) error {
	if notification == nil || notification.RecipientID == notification.ActorID {
		return nil
	}
	model := CommentNotificationOutboxModel{
		EventID: notification.EventID, RecipientID: notification.RecipientID, ActorID: notification.ActorID,
		MessageType: notification.MessageType, Title: notification.Title, Content: notification.Content,
		VideoID: notification.VideoID, RootCommentID: notification.RootCommentID, CommentID: notification.CommentID,
		State: domaininteraction.CommentNotificationStatePending, AvailableAt: notification.AvailableAt,
	}
	return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&model).Error
}

func (r *Repository) ClaimCommentNotifications(ctx context.Context, leaseOwner string, limit int, now time.Time, leaseUntil time.Time) ([]*domaininteraction.CommentNotification, error) {
	leaseOwner = strings.TrimSpace(leaseOwner)
	if limit <= 0 || leaseOwner == "" {
		return []*domaininteraction.CommentNotification{}, nil
	}
	items := make([]*domaininteraction.CommentNotification, 0, limit)
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var models []CommentNotificationOutboxModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("state = ? AND available_at <= ? AND (lease_until IS NULL OR lease_until <= ?)",
				domaininteraction.CommentNotificationStatePending, now, now).
			Order("available_at ASC").Order("created_at ASC").Order("event_id ASC").
			Limit(limit).Find(&models).Error; err != nil {
			return err
		}
		if len(models) == 0 {
			return nil
		}
		eventIDs := make([]string, 0, len(models))
		for index := range models {
			eventIDs = append(eventIDs, models[index].EventID)
			models[index].Attempts++
			models[index].LeaseOwner = leaseOwner
			models[index].LeaseUntil = &leaseUntil
			models[index].UpdatedAt = now
		}
		if err := tx.Model(&CommentNotificationOutboxModel{}).
			Where("event_id IN ? AND state = ?", eventIDs, domaininteraction.CommentNotificationStatePending).
			Updates(map[string]any{
				"attempts":    gorm.Expr("attempts + 1"),
				"lease_owner": leaseOwner,
				"lease_until": leaseUntil,
				"updated_at":  now,
			}).Error; err != nil {
			return err
		}
		for _, model := range models {
			items = append(items, restoreCommentNotification(model))
		}
		return nil
	})
	return items, err
}

func (r *Repository) MarkCommentNotificationDelivered(ctx context.Context, eventID string, leaseOwner string, deliveredAt time.Time) error {
	return r.db.WithContext(ctx).Model(&CommentNotificationOutboxModel{}).
		Where("event_id = ? AND state = ? AND lease_owner = ?",
			strings.TrimSpace(eventID), domaininteraction.CommentNotificationStatePending, strings.TrimSpace(leaseOwner)).
		Updates(map[string]any{
			"state": domaininteraction.CommentNotificationStateDelivered, "delivered_at": deliveredAt,
			"lease_owner": "", "lease_until": nil, "last_error": "", "updated_at": deliveredAt,
		}).Error
}

func (r *Repository) MarkCommentNotificationFailed(ctx context.Context, eventID string, leaseOwner string, availableAt time.Time, reason string, terminal bool) error {
	reason = strings.TrimSpace(reason)
	if len(reason) > 1024 {
		reason = reason[:1024]
	}
	state := domaininteraction.CommentNotificationStatePending
	if terminal {
		state = domaininteraction.CommentNotificationStateTerminal
	}
	return r.db.WithContext(ctx).Model(&CommentNotificationOutboxModel{}).
		Where("event_id = ? AND state = ? AND lease_owner = ?",
			strings.TrimSpace(eventID), domaininteraction.CommentNotificationStatePending, strings.TrimSpace(leaseOwner)).
		Updates(map[string]any{
			"state": state, "available_at": availableAt, "lease_owner": "", "lease_until": nil,
			"last_error": reason, "updated_at": time.Now().UTC(),
		}).Error
}

func restoreCommentNotification(model CommentNotificationOutboxModel) *domaininteraction.CommentNotification {
	return domaininteraction.RestoreCommentNotification(
		model.EventID, model.RecipientID, model.ActorID, model.MessageType, model.Title, model.Content,
		model.VideoID, model.RootCommentID, model.CommentID, model.State, model.Attempts,
		model.AvailableAt, model.LeaseOwner, model.LeaseUntil, model.LastError, model.DeliveredAt,
		model.CreatedAt, model.UpdatedAt,
	)
}

var _ domaininteraction.CommentNotificationOutboxRepository = (*Repository)(nil)
