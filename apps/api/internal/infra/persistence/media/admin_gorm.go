package inframedia

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	domainadminaudit "github.com/shiyudesu/frux/internal/domain/adminaudit"
	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
	infrapersistence "github.com/shiyudesu/frux/internal/infra/persistence"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type retryResultModel struct {
	JobID          int64     `json:"job_id"`
	AssetID        int64     `json:"asset_id"`
	ProfileVersion string    `json:"profile_version"`
	State          string    `json:"state"`
	Attempts       int       `json:"attempts"`
	MaxAttempts    int       `json:"max_attempts"`
	NextAttemptAt  time.Time `json:"next_attempt_at"`
	OccurredAt     time.Time `json:"occurred_at"`
}

func (r *Repository) SummarizeAdminProcessing(
	ctx context.Context,
) (*domainmedia.AdminProcessingSummary, error) {
	var rows []struct {
		State string
		Count int64
	}
	if err := r.db.WithContext(ctx).Model(&ProcessingJobModel{}).
		Select("state, COUNT(*) AS count").Group("state").Scan(&rows).Error; err != nil {
		return nil, err
	}
	summary := &domainmedia.AdminProcessingSummary{}
	for _, row := range rows {
		switch row.State {
		case domainmedia.JobStatePending, domainmedia.JobStateRetryable:
			summary.Waiting += row.Count
		case domainmedia.JobStateProcessing:
			summary.Processing += row.Count
		case domainmedia.JobStateFailed:
			summary.Failed += row.Count
		case domainmedia.JobStateCompleted:
			summary.Completed += row.Count
		}
	}
	if summary.Waiting > 0 {
		var oldest struct {
			Value *time.Time
		}
		if err := r.db.WithContext(ctx).Model(&ProcessingJobModel{}).
			Where("state IN ?", []string{
				domainmedia.JobStatePending, domainmedia.JobStateRetryable,
			}).
			Select("MIN(created_at) AS value").Scan(&oldest).Error; err != nil {
			return nil, err
		}
		summary.OldestWaitingAt = oldest.Value
	}
	return summary, nil
}

func (r *Repository) ListActiveAdminProcessing(
	ctx context.Context,
	limit int,
) ([]*domainmedia.MediaProcessingJob, error) {
	if limit <= 0 {
		return []*domainmedia.MediaProcessingJob{}, nil
	}
	if limit > 100 {
		limit = 100
	}
	var models []ProcessingJobModel
	err := r.db.WithContext(ctx).
		Where("state IN ?", []string{
			domainmedia.JobStatePending,
			domainmedia.JobStateProcessing,
			domainmedia.JobStateRetryable,
		}).
		Order(`CASE state
			WHEN 'processing' THEN 0
			WHEN 'retryable' THEN 1
			ELSE 2 END`).
		Order("created_at ASC").Order("id ASC").
		Limit(limit).Find(&models).Error
	if err != nil {
		return nil, err
	}
	return processingJobsFromModels(models), nil
}

func (r *Repository) ListAdminProcessingHistory(
	ctx context.Context,
	raw domainmedia.AdminProcessingHistoryQuery,
) ([]*domainmedia.MediaProcessingJob, error) {
	query, err := domainmedia.NormalizeAdminProcessingHistoryQuery(raw)
	if err != nil {
		return nil, err
	}
	db := r.db.WithContext(ctx).Where(
		"state IN ? AND completed_at IS NOT NULL",
		[]string{domainmedia.JobStateCompleted, domainmedia.JobStateFailed},
	)
	if query.State != "" {
		db = db.Where("state = ?", query.State)
	}
	if query.Step != "" {
		db = db.Where("processing_step = ?", query.Step)
	}
	if query.ErrorCode != "" {
		db = db.Where("error_code = ?", query.ErrorCode)
	}
	if query.AssetID > 0 {
		db = db.Where("asset_id = ?", query.AssetID)
	}
	if query.CompletedFrom != nil {
		db = db.Where("completed_at BETWEEN ? AND ?", query.CompletedFrom, query.CompletedTo)
	}
	if query.Cursor != nil {
		db = db.Where(
			"(completed_at < ? OR (completed_at = ? AND id < ?))",
			query.Cursor.CompletedAt, query.Cursor.CompletedAt, query.Cursor.JobID,
		)
	}
	var models []ProcessingJobModel
	if err := db.Order("completed_at DESC").Order("id DESC").
		Limit(query.Limit).Find(&models).Error; err != nil {
		return nil, err
	}
	return processingJobsFromModels(models), nil
}

func (r *Repository) FindProcessingJobByID(
	ctx context.Context,
	jobID int64,
) (*domainmedia.MediaProcessingJob, error) {
	var model ProcessingJobModel
	if err := r.db.WithContext(ctx).Where("id = ?", jobID).Take(&model).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domainmedia.ErrProcessingJobNotFound
		}
		return nil, err
	}
	return processingJobFromModel(model), nil
}

func (r *Repository) CommitAdminProcessingRetry(
	ctx context.Context,
	raw domainmedia.AdminProcessingRetryCommand,
	buildAudit func(domainmedia.ProcessingRetryAuditInput) (*domainadminaudit.Fact, error),
) (*domainmedia.AdminProcessingRetryResult, error) {
	command, err := domainmedia.NormalizeAdminProcessingRetryCommand(raw)
	if err != nil {
		return nil, err
	}
	if r.auditWriter == nil || buildAudit == nil {
		return nil, domainadminaudit.ErrAuditWriteFailed
	}
	fingerprint := command.Fingerprint()
	var result *domainmedia.AdminProcessingRetryResult
	var committedFact *domainadminaudit.Fact
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		receipt, found, err := findProcessingRetryReceipt(
			tx, command.ActorID, command.IdempotencyKey,
		)
		if err != nil {
			return err
		}
		if found {
			if receipt.Fingerprint != fingerprint {
				return domainmedia.ErrProcessingRetryIdempotencyConflict
			}
			result, err = processingRetryResultFromReceipt(*receipt)
			if err == nil {
				result.Replayed = true
			}
			return err
		}

		var job ProcessingJobModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", command.JobID).Take(&job).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return domainmedia.ErrProcessingJobNotFound
			}
			return err
		}
		if job.State != domainmedia.JobStateFailed {
			return domainmedia.ErrProcessingRetryConflict
		}
		var asset AssetModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", job.AssetID).Take(&asset).Error; err != nil {
			return err
		}
		if asset.State == domainmedia.AssetStateDeleted ||
			asset.State == domainmedia.AssetStateReady {
			return domainmedia.ErrProcessingRetryConflict
		}
		var videoCount int64
		if err := tx.Table("video").
			Where("id = ? AND media_asset_id = ?", command.VideoID, asset.ID).
			Count(&videoCount).Error; err != nil {
			return err
		}
		if videoCount != 1 {
			return domainmedia.ErrProcessingRetryConflict
		}
		previousState, previousAttempts := job.State, job.Attempts
		now := command.OccurredAt
		update := tx.Model(&ProcessingJobModel{}).Where(
			"id = ? AND state = ?", job.ID, domainmedia.JobStateFailed,
		).Updates(map[string]any{
			"state":    domainmedia.JobStateRetryable,
			"attempts": 0, "error_code": "", "error_message": "",
			"lease_owner": "", "lease_until": nil,
			"processing_step": domainmedia.ProcessingStepWaiting,
			"progress_bps":    nil, "progress_updated_at": now,
			"next_attempt_at": now, "completed_at": nil, "updated_at": now,
		})
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected != 1 {
			return domainmedia.ErrProcessingRetryConflict
		}
		if err := tx.Model(&AssetModel{}).Where("id = ?", asset.ID).
			Updates(map[string]any{
				"state":      domainmedia.AssetStateUploaded,
				"error_code": "", "updated_at": now,
			}).Error; err != nil {
			return err
		}
		committedFact, err = buildAudit(domainmedia.ProcessingRetryAuditInput{
			AssetID: asset.ID, VideoID: command.VideoID,
			PreviousState: previousState, NewState: domainmedia.JobStateRetryable,
			PreviousAttempts: previousAttempts,
		})
		if err != nil {
			return err
		}
		if err := r.auditWriter.AppendInTransaction(ctx, tx, committedFact); err != nil {
			return err
		}
		outbox := ProcessingRetryOutboxModel{
			EventID: command.EventID(), JobID: job.ID, AssetID: asset.ID,
			State: domainmedia.RetryNotificationPending, AvailableAt: now,
			CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.Create(&outbox).Error; err != nil {
			return err
		}
		job.State = domainmedia.JobStateRetryable
		job.Attempts = 0
		job.ErrorCode, job.ErrorMessage = "", ""
		job.LeaseOwner, job.LeaseUntil = "", nil
		job.ProcessingStep = domainmedia.ProcessingStepWaiting
		job.ProgressBPS = nil
		job.ProgressUpdatedAt = &now
		job.NextAttemptAt, job.CompletedAt, job.UpdatedAt = now, nil, now
		result = &domainmedia.AdminProcessingRetryResult{
			Job: processingJobFromModel(job),
		}
		encoded, err := json.Marshal(retryResultModelFromDomain(result.Job, now))
		if err != nil {
			return err
		}
		receiptModel := ProcessingRetryReceiptModel{
			ActorID: command.ActorID, IdempotencyKey: command.IdempotencyKey,
			Fingerprint: fingerprint, JobID: job.ID, ReasonCode: command.ReasonCode,
			Note: command.Note, ResultJSON: string(encoded), CreatedAt: now,
		}
		if err := tx.Create(&receiptModel).Error; err != nil {
			if infrapersistence.IsDuplicatedKeyError(err) {
				return domainmedia.ErrProcessingRetryIdempotencyConflict
			}
			return err
		}
		return nil
	})
	if err != nil {
		replayed, loadErr := r.loadMatchingProcessingRetry(
			ctx, command.ActorID, command.IdempotencyKey, fingerprint,
		)
		if loadErr == nil && replayed != nil {
			replayed.Replayed = true
			return replayed, nil
		}
		return nil, err
	}
	if committedFact != nil {
		r.auditWriter.RecordCommittedWrite(committedFact)
	}
	return result, nil
}

func (r *Repository) ClaimProcessingRetryNotifications(
	ctx context.Context,
	leaseOwner string,
	limit int,
	now, leaseUntil time.Time,
) ([]*domainmedia.RetryNotificationOutboxItem, error) {
	leaseOwner = strings.TrimSpace(leaseOwner)
	if leaseOwner == "" || limit <= 0 || !leaseUntil.After(now) {
		return []*domainmedia.RetryNotificationOutboxItem{}, nil
	}
	items := make([]*domainmedia.RetryNotificationOutboxItem, 0, limit)
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var models []ProcessingRetryOutboxModel
		if err := tx.Clauses(clause.Locking{
			Strength: "UPDATE", Options: "SKIP LOCKED",
		}).Where(
			"state = ? AND available_at <= ? AND (lease_until IS NULL OR lease_until <= ?)",
			domainmedia.RetryNotificationPending, now, now,
		).Order("available_at ASC").Order("event_id ASC").
			Limit(limit).Find(&models).Error; err != nil {
			return err
		}
		for _, model := range models {
			update := tx.Model(&ProcessingRetryOutboxModel{}).Where(
				"event_id = ? AND state = ? AND (lease_until IS NULL OR lease_until <= ?)",
				model.EventID, domainmedia.RetryNotificationPending, now,
			).Updates(map[string]any{
				"attempts": gorm.Expr("attempts + 1"), "lease_owner": leaseOwner,
				"lease_until": leaseUntil, "updated_at": now,
			})
			if update.Error != nil {
				return update.Error
			}
			if update.RowsAffected != 1 {
				continue
			}
			model.Attempts++
			model.LeaseOwner, model.LeaseUntil = leaseOwner, &leaseUntil
			items = append(items, retryNotificationFromModel(model))
		}
		return nil
	})
	return items, err
}

func (r *Repository) MarkProcessingRetryNotificationDelivered(
	ctx context.Context,
	eventID, leaseOwner string,
	deliveredAt time.Time,
) error {
	result := r.db.WithContext(ctx).Model(&ProcessingRetryOutboxModel{}).Where(
		"event_id = ? AND state = ? AND lease_owner = ?",
		strings.TrimSpace(eventID), domainmedia.RetryNotificationPending,
		strings.TrimSpace(leaseOwner),
	).Updates(map[string]any{
		"state":        domainmedia.RetryNotificationDelivered,
		"delivered_at": deliveredAt, "lease_owner": "", "lease_until": nil,
		"last_error": "", "updated_at": deliveredAt,
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *Repository) MarkProcessingRetryNotificationFailed(
	ctx context.Context,
	eventID, leaseOwner string,
	availableAt time.Time,
	reason string,
	terminal bool,
) error {
	state := domainmedia.RetryNotificationPending
	if terminal {
		state = domainmedia.RetryNotificationTerminal
	}
	reason = strings.TrimSpace(reason)
	if len(reason) > 1024 {
		reason = reason[len(reason)-1024:]
	}
	result := r.db.WithContext(ctx).Model(&ProcessingRetryOutboxModel{}).Where(
		"event_id = ? AND state = ? AND lease_owner = ?",
		strings.TrimSpace(eventID), domainmedia.RetryNotificationPending,
		strings.TrimSpace(leaseOwner),
	).Updates(map[string]any{
		"state": state, "available_at": availableAt,
		"lease_owner": "", "lease_until": nil,
		"last_error": reason, "updated_at": time.Now().UTC(),
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *Repository) CountPendingProcessingRetryNotifications(
	ctx context.Context,
) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&ProcessingRetryOutboxModel{}).
		Where("state = ?", domainmedia.RetryNotificationPending).
		Count(&count).Error
	return count, err
}

func findProcessingRetryReceipt(
	tx *gorm.DB,
	actorID int64,
	idempotencyKey string,
) (*ProcessingRetryReceiptModel, bool, error) {
	var receipt ProcessingRetryReceiptModel
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("actor_id = ? AND idempotency_key = ?", actorID, idempotencyKey).
		Take(&receipt).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return &receipt, true, nil
}

func (r *Repository) loadMatchingProcessingRetry(
	ctx context.Context,
	actorID int64,
	idempotencyKey, fingerprint string,
) (*domainmedia.AdminProcessingRetryResult, error) {
	var receipt ProcessingRetryReceiptModel
	if err := r.db.WithContext(ctx).Where(
		"actor_id = ? AND idempotency_key = ?", actorID, idempotencyKey,
	).Take(&receipt).Error; err != nil {
		return nil, err
	}
	if receipt.Fingerprint != fingerprint {
		return nil, domainmedia.ErrProcessingRetryIdempotencyConflict
	}
	return processingRetryResultFromReceipt(receipt)
}

func processingRetryResultFromReceipt(
	receipt ProcessingRetryReceiptModel,
) (*domainmedia.AdminProcessingRetryResult, error) {
	var stored retryResultModel
	if err := json.Unmarshal([]byte(receipt.ResultJSON), &stored); err != nil {
		return nil, err
	}
	progressAt := stored.OccurredAt
	job := &domainmedia.MediaProcessingJob{
		ID: stored.JobID, AssetID: stored.AssetID,
		ProfileVersion: stored.ProfileVersion, State: stored.State,
		Attempts: stored.Attempts, MaxAttempts: stored.MaxAttempts,
		ProcessingStep:    domainmedia.ProcessingStepWaiting,
		ProgressUpdatedAt: &progressAt, NextAttemptAt: stored.NextAttemptAt,
		UpdatedAt: stored.OccurredAt,
	}
	return &domainmedia.AdminProcessingRetryResult{Job: job}, nil
}

func retryResultModelFromDomain(
	job *domainmedia.MediaProcessingJob,
	occurredAt time.Time,
) retryResultModel {
	return retryResultModel{
		JobID: job.ID, AssetID: job.AssetID, ProfileVersion: job.ProfileVersion,
		State: job.State, Attempts: job.Attempts, MaxAttempts: job.MaxAttempts,
		NextAttemptAt: job.NextAttemptAt, OccurredAt: occurredAt,
	}
}

func retryNotificationFromModel(
	model ProcessingRetryOutboxModel,
) *domainmedia.RetryNotificationOutboxItem {
	return &domainmedia.RetryNotificationOutboxItem{
		EventID: model.EventID, JobID: model.JobID, AssetID: model.AssetID,
		State: model.State, Attempts: model.Attempts, AvailableAt: model.AvailableAt,
		LeaseOwner: model.LeaseOwner, LeaseUntil: model.LeaseUntil,
		LastError: model.LastError, DeliveredAt: model.DeliveredAt,
		CreatedAt: model.CreatedAt, UpdatedAt: model.UpdatedAt,
	}
}

func processingJobsFromModels(
	models []ProcessingJobModel,
) []*domainmedia.MediaProcessingJob {
	result := make([]*domainmedia.MediaProcessingJob, 0, len(models))
	for _, model := range models {
		result = append(result, processingJobFromModel(model))
	}
	return result
}
