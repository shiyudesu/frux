package infraexposure

import (
	"context"
	applicationexposure "github.com/shiyudesu/frux/internal/application/exposure"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (r *Repository) ClaimViewEventOutbox(ctx context.Context, limit int, now, leasedUntil time.Time) ([]applicationexposure.OutboxItem, error) {
	if limit <= 0 {
		return []applicationexposure.OutboxItem{}, nil
	}
	items := make([]applicationexposure.OutboxItem, 0, limit)
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var models []ViewEventOutboxModel
		if err := tx.Table("view_event_outbox AS current_outbox").
			Select("current_outbox.*").
			Joins("JOIN video_view_events AS current_event ON current_event.id = current_outbox.view_event_id").
			Clauses(clause.Locking{
				Strength: "UPDATE",
				Table:    clause.Table{Name: "current_outbox"},
				Options:  "SKIP LOCKED",
			}).
			Where("current_outbox.dispatched_at IS NULL AND current_outbox.available_at <= ? AND (current_outbox.leased_until IS NULL OR current_outbox.leased_until < ?)", now, now).
			Where(`NOT EXISTS (
				SELECT 1
				FROM view_event_outbox AS earlier_outbox
				JOIN video_view_events AS earlier_event ON earlier_event.id = earlier_outbox.view_event_id
				WHERE earlier_outbox.dispatched_at IS NULL
				  AND earlier_outbox.id < current_outbox.id
				  AND earlier_event.user_id = current_event.user_id
			)`).
			Order("current_outbox.id ASC").
			Limit(limit).
			Find(&models).Error; err != nil {
			return err
		}
		if len(models) == 0 {
			return nil
		}
		ids := make([]int64, 0, len(models))
		for index := range models {
			ids = append(ids, models[index].ID)
			models[index].Attempts++
		}
		if err := tx.Model(&ViewEventOutboxModel{}).
			Where("id IN ?", ids).
			Updates(map[string]any{"leased_until": leasedUntil, "attempts": gorm.Expr("attempts + 1"), "updated_at": now}).
			Error; err != nil {
			return err
		}
		for _, model := range models {
			var eventModel ViewEventModel
			if err := tx.Where("id = ?", model.ViewEventID).Take(&eventModel).Error; err != nil {
				return err
			}
			event := applicationexposure.NewViewEventRecordedEvent(restoreViewEvent(eventModel), nil)
			if event != nil {
				event.ExposureCount = model.ExposureCount
			}
			items = append(items, applicationexposure.OutboxItem{ID: model.ID, Attempts: model.Attempts, Event: event})
		}
		return nil
	})
	return items, err
}

func (r *Repository) MarkViewEventOutboxDispatched(ctx context.Context, id int64, dispatchedAt time.Time) error {
	return r.db.WithContext(ctx).Model(&ViewEventOutboxModel{}).
		Where("id = ? AND dispatched_at IS NULL", id).
		Updates(map[string]any{
			"dispatched_at": dispatchedAt, "leased_until": nil, "last_error": "", "updated_at": dispatchedAt,
		}).Error
}

func (r *Repository) MarkViewEventOutboxFailed(ctx context.Context, id int64, availableAt time.Time, reason string) error {
	reason = strings.TrimSpace(reason)
	if len(reason) > 512 {
		reason = reason[:512]
	}
	return r.db.WithContext(ctx).Model(&ViewEventOutboxModel{}).
		Where("id = ? AND dispatched_at IS NULL", id).
		Updates(map[string]any{
			"available_at": availableAt, "leased_until": nil, "last_error": reason, "updated_at": time.Now().UTC(),
		}).Error
}

func (r *Repository) ViewEventOutboxStats(ctx context.Context, now time.Time) (applicationexposure.OutboxStats, error) {
	var result struct {
		Pending int64
		Oldest  *time.Time
	}
	err := r.db.WithContext(ctx).Model(&ViewEventOutboxModel{}).
		Select("COUNT(*) AS pending, MIN(created_at) AS oldest").
		Where("dispatched_at IS NULL").
		Scan(&result).Error
	stats := applicationexposure.OutboxStats{Pending: result.Pending}
	if result.Oldest != nil {
		stats.OldestPending = result.Oldest.UTC()
	}
	return stats, err
}
