package infrarelation

import (
	domainrelation "GCFeed/internal/domain/relation"
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// EnsureFollowProfileOutbox backfills one current-state signal for relations
// that predate transactional profile projection. It deliberately uses one
// set-based statement: loading every follow and issuing per-row inserts makes
// startup migration unbounded for established accounts.
func EnsureFollowProfileOutbox(tx *gorm.DB) error {
	if tx == nil {
		return gorm.ErrInvalidDB
	}
	return tx.Exec(`
		INSERT INTO relation_profile_projection_outbox (
			event_id,
			follow_id,
			user_id,
			target_user_id,
			active,
			occurred_at,
			recommendation_request_id,
			recommendation_video_id,
			available_at,
			attempts,
			last_error,
			created_at,
			updated_at
		)
		SELECT
			'relation:follow:' || f.id::text || ':' || f.status::text || ':' ||
				(
					floor(EXTRACT(EPOCH FROM f.updated_at))::bigint * 1000000000 +
					(EXTRACT(MICROSECONDS FROM f.updated_at)::bigint % 1000000) * 1000
				)::text,
			f.id,
			f.user_id,
			f.target_user_id,
			f.status = ?,
			f.updated_at,
			'',
			0,
			f.updated_at,
			0,
			'',
			CURRENT_TIMESTAMP,
			CURRENT_TIMESTAMP
		FROM user_follow AS f
		ON CONFLICT (event_id) DO NOTHING
	`, domainrelation.FollowStatusActive).Error
}

func (r *Repository) ClaimFollowProfileOutbox(ctx context.Context, limit int, now, leasedUntil time.Time) ([]domainrelation.FollowProjectionOutboxItem, error) {
	if limit <= 0 {
		return []domainrelation.FollowProjectionOutboxItem{}, nil
	}
	items := make([]domainrelation.FollowProjectionOutboxItem, 0, limit)
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var models []FollowProfileOutboxModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("dispatched_at IS NULL AND available_at <= ? AND (leased_until IS NULL OR leased_until <= ?)", now, now).
			Order("available_at ASC").Order("id ASC").Limit(limit).Find(&models).Error; err != nil {
			return err
		}
		for _, model := range models {
			if err := tx.Model(&FollowProfileOutboxModel{}).Where("id = ?", model.ID).Updates(map[string]any{
				"attempts": gorm.Expr("attempts + 1"), "leased_until": leasedUntil, "updated_at": now,
			}).Error; err != nil {
				return err
			}
			items = append(items, domainrelation.FollowProjectionOutboxItem{
				ID: model.ID, Attempts: model.Attempts + 1, EventID: model.EventID, UserID: model.UserID,
				AuthorID: model.TargetUserID, Active: model.Active, OccurredAt: model.OccurredAt,
				RecommendationRequestID: model.RecommendationRequestID, RecommendationVideoID: model.RecommendationVideoID,
			})
		}
		return nil
	})
	return items, err
}

func (r *Repository) MarkFollowProfileOutboxDispatched(ctx context.Context, id int64, dispatchedAt time.Time) error {
	return r.db.WithContext(ctx).Model(&FollowProfileOutboxModel{}).Where("id = ?", id).Updates(map[string]any{
		"dispatched_at": dispatchedAt, "leased_until": nil, "last_error": "", "updated_at": dispatchedAt,
	}).Error
}

func (r *Repository) MarkFollowProfileOutboxFailed(ctx context.Context, id int64, availableAt time.Time, reason string) error {
	return r.db.WithContext(ctx).Model(&FollowProfileOutboxModel{}).Where("id = ?", id).Updates(map[string]any{
		"available_at": availableAt, "leased_until": nil, "last_error": reason, "updated_at": availableAt,
	}).Error
}
