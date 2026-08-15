package infraaccount

import (
	"context"
	"strings"
	"time"

	domainaccount "github.com/shiyudesu/frux/internal/domain/account"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func appendAccountLifecycleNotification(
	tx *gorm.DB,
	userID int64,
	operation domainaccount.AccountManagementOperation,
	reasonCode string,
	authVersion int64,
	occurredAt time.Time,
) error {
	notification, err := domainaccount.NewAccountLifecycleNotification(
		userID, operation, reasonCode, authVersion, occurredAt,
	)
	if err != nil {
		return err
	}
	model := NotificationOutboxModel{
		EventID: notification.EventID, RecipientID: notification.RecipientID,
		Operation: string(notification.Operation), ReasonCode: notification.ReasonCode,
		AuthVersion: notification.AuthVersion, OccurredAt: notification.OccurredAt,
		State:       domainaccount.AccountNotificationPending,
		AvailableAt: notification.OccurredAt,
		CreatedAt:   notification.OccurredAt, UpdatedAt: notification.OccurredAt,
	}
	return tx.Create(&model).Error
}

func (r *Repository) ClaimAccountNotifications(
	ctx context.Context,
	leaseOwner string,
	limit int,
	now time.Time,
	leaseUntil time.Time,
) ([]*domainaccount.AccountNotificationOutboxItem, error) {
	leaseOwner = strings.TrimSpace(leaseOwner)
	if leaseOwner == "" || limit <= 0 || now.IsZero() || !leaseUntil.After(now) {
		return []*domainaccount.AccountNotificationOutboxItem{}, nil
	}
	items := make([]*domainaccount.AccountNotificationOutboxItem, 0, limit)
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var models []NotificationOutboxModel
		if err := tx.Clauses(clause.Locking{
			Strength: "UPDATE", Options: "SKIP LOCKED",
		}).
			Where(
				"state = ? AND available_at <= ? AND (lease_until IS NULL OR lease_until <= ?)",
				domainaccount.AccountNotificationPending, now, now,
			).
			Order("available_at ASC").Order("created_at ASC").Order("event_id ASC").
			Limit(limit).Find(&models).Error; err != nil {
			return err
		}
		for index := range models {
			update := tx.Model(&NotificationOutboxModel{}).
				Where(
					"event_id = ? AND state = ? AND (lease_until IS NULL OR lease_until <= ?)",
					models[index].EventID, domainaccount.AccountNotificationPending, now,
				).
				Updates(map[string]any{
					"attempts": gorm.Expr("attempts + 1"), "lease_owner": leaseOwner,
					"lease_until": leaseUntil, "updated_at": now,
				})
			if update.Error != nil {
				return update.Error
			}
			if update.RowsAffected != 1 {
				continue
			}
			models[index].Attempts++
			models[index].LeaseOwner = leaseOwner
			models[index].LeaseUntil = &leaseUntil
			models[index].UpdatedAt = now
			item, err := restoreAccountNotificationOutbox(models[index])
			if err != nil {
				return err
			}
			items = append(items, item)
		}
		return nil
	})
	return items, err
}

func (r *Repository) MarkAccountNotificationDelivered(
	ctx context.Context,
	eventID string,
	leaseOwner string,
	deliveredAt time.Time,
) error {
	result := r.db.WithContext(ctx).Model(&NotificationOutboxModel{}).
		Where(
			"event_id = ? AND state = ? AND lease_owner = ?",
			strings.TrimSpace(eventID), domainaccount.AccountNotificationPending,
			strings.TrimSpace(leaseOwner),
		).
		Updates(map[string]any{
			"state":        domainaccount.AccountNotificationDelivered,
			"delivered_at": deliveredAt.UTC(), "lease_owner": "", "lease_until": nil,
			"last_error": "", "updated_at": deliveredAt.UTC(),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *Repository) MarkAccountNotificationFailed(
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
	state := domainaccount.AccountNotificationPending
	if terminal {
		state = domainaccount.AccountNotificationTerminal
	}
	result := r.db.WithContext(ctx).Model(&NotificationOutboxModel{}).
		Where(
			"event_id = ? AND state = ? AND lease_owner = ?",
			strings.TrimSpace(eventID), domainaccount.AccountNotificationPending,
			strings.TrimSpace(leaseOwner),
		).
		Updates(map[string]any{
			"state": state, "available_at": availableAt.UTC(), "lease_owner": "",
			"lease_until": nil, "last_error": reason, "updated_at": time.Now().UTC(),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func restoreAccountNotificationOutbox(
	model NotificationOutboxModel,
) (*domainaccount.AccountNotificationOutboxItem, error) {
	return &domainaccount.AccountNotificationOutboxItem{
		AccountLifecycleNotification: domainaccount.AccountLifecycleNotification{
			EventID: model.EventID, RecipientID: model.RecipientID,
			Operation:  domainaccount.AccountManagementOperation(model.Operation),
			ReasonCode: model.ReasonCode, AuthVersion: model.AuthVersion,
			OccurredAt: model.OccurredAt,
		},
		State: model.State, Attempts: model.Attempts,
		AvailableAt: model.AvailableAt, LeaseOwner: model.LeaseOwner,
		LeaseUntil: model.LeaseUntil, LastError: model.LastError,
		DeliveredAt: model.DeliveredAt, CreatedAt: model.CreatedAt,
		UpdatedAt: model.UpdatedAt,
	}, nil
}
