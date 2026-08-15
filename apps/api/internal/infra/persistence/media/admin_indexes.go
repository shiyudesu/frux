package inframedia

import "gorm.io/gorm"

func EnsureAdminProcessingIndexes(db *gorm.DB) error {
	return db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_media_processing_job_active
		ON media_processing_job (state, processing_step, next_attempt_at, id)
		WHERE state IN ('pending', 'processing', 'retryable');

		CREATE INDEX IF NOT EXISTS idx_media_processing_job_terminal_history
		ON media_processing_job (completed_at DESC, id DESC)
		WHERE state IN ('completed', 'failed');
	`).Error
}
