package infrarecommendation

import (
	domainrecommendation "github.com/shiyudesu/frux/internal/domain/recommendation"
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// EnsureFeedbackProfileOutbox restores delivery for feedback facts created
// before the transactional outbox was introduced.
func EnsureFeedbackProfileOutbox(tx *gorm.DB) error {
	return tx.Exec(`
		INSERT INTO recommendation_feedback_profile_outbox
			(feedback_id, available_at, attempts, last_error, created_at, updated_at)
		SELECT id, created_at, 0, '', created_at, created_at
		FROM recommendation_feedback
		ON CONFLICT DO NOTHING
	`).Error
}

func (r *Repository) ClaimFeedbackProfileOutbox(ctx context.Context, limit int, now, leasedUntil time.Time) ([]domainrecommendation.FeedbackProjectionOutboxItem, error) {
	if limit <= 0 {
		return []domainrecommendation.FeedbackProjectionOutboxItem{}, nil
	}
	items := make([]domainrecommendation.FeedbackProjectionOutboxItem, 0, limit)
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var models []FeedbackProfileOutboxModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("dispatched_at IS NULL AND available_at <= ? AND (leased_until IS NULL OR leased_until <= ?)", now, now).
			Order("available_at ASC").Order("id ASC").Limit(limit).Find(&models).Error; err != nil {
			return err
		}
		for _, model := range models {
			var feedback FeedbackModel
			if err := tx.Where("id = ?", model.FeedbackID).Take(&feedback).Error; err != nil {
				return err
			}
			if err := tx.Model(&FeedbackProfileOutboxModel{}).Where("id = ?", model.ID).Updates(map[string]any{
				"attempts": gorm.Expr("attempts + 1"), "leased_until": leasedUntil, "updated_at": now,
			}).Error; err != nil {
				return err
			}
			items = append(items, domainrecommendation.FeedbackProjectionOutboxItem{
				ID: model.ID, Attempts: model.Attempts + 1, Feedback: feedbackFromModel(feedback),
			})
		}
		return nil
	})
	return items, err
}

func (r *Repository) MarkFeedbackProfileOutboxDispatched(ctx context.Context, id int64, dispatchedAt time.Time) error {
	return r.db.WithContext(ctx).Model(&FeedbackProfileOutboxModel{}).Where("id = ?", id).Updates(map[string]any{
		"dispatched_at": dispatchedAt, "leased_until": nil, "last_error": "", "updated_at": dispatchedAt,
	}).Error
}

func (r *Repository) MarkFeedbackProfileOutboxFailed(ctx context.Context, id int64, availableAt time.Time, reason string) error {
	return r.db.WithContext(ctx).Model(&FeedbackProfileOutboxModel{}).Where("id = ?", id).Updates(map[string]any{
		"available_at": availableAt, "leased_until": nil, "last_error": reason, "updated_at": availableAt,
	}).Error
}
