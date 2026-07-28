package infrainteraction

import (
	"time"

	"gorm.io/gorm"
)

// EnsureActionProfileProjectionOutbox makes action receipts that predate the
// durable profile queue available to the worker. Projection uses event IDs for
// idempotency, so replaying these accepted facts is safe.
func EnsureActionProfileProjectionOutbox(tx *gorm.DB) error {
	if tx == nil {
		return nil
	}
	return tx.Model(&ActionEventModel{}).
		Where("profile_projection_available_at IS NULL").
		Update("profile_projection_available_at", time.Now().UTC()).
		Error
}

// EnsureRecommendationActionOutcomeOutbox makes existing accepted actions
// eligible for bounded outcome-attribution retries after the outbox gained an
// explicit availability timestamp.
func EnsureRecommendationActionOutcomeOutbox(tx *gorm.DB) error {
	if tx == nil {
		return nil
	}
	return tx.Model(&ActionEventModel{}).
		Where("recommendation_outcome_available_at IS NULL").
		Update("recommendation_outcome_available_at", gorm.Expr("processed_at")).
		Error
}
