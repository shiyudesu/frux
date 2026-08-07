package infrareview

import (
	"context"
	"errors"
	"strings"
	"time"

	domainreview "github.com/shiyudesu/frux/internal/domain/review"
	domainvideo "github.com/shiyudesu/frux/internal/domain/video"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (r *Repository) LoadModerationSubject(
	ctx context.Context,
	job *domainreview.ModerationJob,
) (*domainreview.ModerationSubject, error) {
	if job == nil {
		return nil, domainreview.ErrInvalidModerationJob
	}
	var row struct {
		CaseID          int64
		VideoID         int64
		ReviewVersion   int
		Title           string
		Description     string
		PolicyVersion   int
		MediaURL        string
		SourceObjectKey string
	}
	err := r.db.WithContext(ctx).Table("review_case AS rc").
		Select(`rc.id AS case_id, v.id AS video_id, v.review_version, rc.policy_version,
			v.title, v.description, v.media_url,
			COALESCE(ma.object_key, '') AS source_object_key`).
		Joins("JOIN video v ON v.id = rc.video_id").
		Joins("LEFT JOIN media_asset ma ON ma.id = v.media_asset_id").
		Where(`rc.id = ? AND rc.video_id = ? AND rc.review_version = ?
			AND rc.status = ? AND v.review_version = ? AND v.status = ?`,
			job.CaseID, job.VideoID, job.ReviewVersion,
			domainreview.CaseStatusOpen, job.ReviewVersion, domainvideo.StatusPendingReview).
		Take(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domainreview.ErrModerationJobStale
		}
		return nil, err
	}
	sourceKey := strings.TrimSpace(row.SourceObjectKey)
	if sourceKey == "" {
		mediaURL := strings.TrimSpace(row.MediaURL)
		if strings.HasPrefix(mediaURL, "/uploads/") {
			sourceKey = strings.TrimPrefix(mediaURL, "/uploads/")
		}
	}
	return &domainreview.ModerationSubject{
		CaseID: row.CaseID, VideoID: row.VideoID, ReviewVersion: row.ReviewVersion,
		Title: row.Title, Description: row.Description, PolicyVersion: row.PolicyVersion,
		SourceObjectKey: sourceKey,
	}, nil
}

func (r *Repository) ModerationResultAccepted(
	ctx context.Context,
	resultID string,
) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&ResultModel{}).
		Where("result_id = ?", strings.TrimSpace(resultID)).
		Count(&count).Error
	return count > 0, err
}

func (r *Repository) LoadModerationProcessingResult(
	ctx context.Context,
	resultID string,
) (*domainreview.ProcessingResult, error) {
	var receipt ResultModel
	if err := r.db.WithContext(ctx).
		Where("result_id = ?", strings.TrimSpace(resultID)).
		Order("created_at ASC").Order("id ASC").
		Take(&receipt).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domainreview.ErrReviewCaseNotFound
		}
		return nil, err
	}
	return loadProcessingResult(r.db.WithContext(ctx), receipt, true)
}

func (r *Repository) ClaimModerationJobs(
	ctx context.Context,
	leaseOwner string,
	limit int,
	leaseTTL time.Duration,
) ([]*domainreview.ModerationJob, error) {
	leaseOwner = strings.TrimSpace(leaseOwner)
	if r == nil || r.db == nil || leaseOwner == "" ||
		len(leaseOwner) > domainreview.MaxModerationLeaseOwnerLength ||
		limit <= 0 || leaseTTL <= 0 {
		return nil, domainreview.ErrInvalidModerationJob
	}
	if limit > 100 {
		limit = 100
	}
	jobs := make([]*domainreview.ModerationJob, 0, limit)
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now, err := databaseNow(tx)
		if err != nil {
			return err
		}
		var models []ModerationJobModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where(`(
				(status IN ? AND available_at <= ?)
				OR (status = ? AND lease_until <= ?)
			)`,
				[]string{domainreview.ModerationJobPending, domainreview.ModerationJobRetryWait},
				now, domainreview.ModerationJobLeased, now).
			Order("available_at ASC").Order("created_at ASC").Order("id ASC").
			Limit(limit).Find(&models).Error; err != nil {
			return err
		}
		leaseUntil := now.Add(leaseTTL)
		for index := range models {
			models[index].Status = domainreview.ModerationJobLeased
			models[index].Attempts++
			models[index].LeaseOwner = leaseOwner
			models[index].LeaseUntil = &leaseUntil
			models[index].UpdatedAt = now
			if err := tx.Model(&ModerationJobModel{}).
				Where("id = ?", models[index].ID).
				Updates(map[string]any{
					"status":      domainreview.ModerationJobLeased,
					"attempts":    gorm.Expr("attempts + 1"),
					"lease_owner": leaseOwner, "lease_until": leaseUntil,
					"updated_at": now,
				}).Error; err != nil {
				return err
			}
			jobs = append(jobs, restoreModerationJob(models[index]))
		}
		return nil
	})
	return jobs, err
}

func (r *Repository) SaveModerationInputManifest(
	ctx context.Context,
	jobID int64,
	leaseOwner string,
	manifestJSON string,
) error {
	manifestJSON = strings.TrimSpace(manifestJSON)
	if jobID <= 0 || manifestJSON == "" {
		return domainreview.ErrInvalidModerationJob
	}
	result := r.db.WithContext(ctx).Model(&ModerationJobModel{}).
		Where("id = ? AND status = ? AND lease_owner = ? AND lease_until > clock_timestamp()",
			jobID, domainreview.ModerationJobLeased, strings.TrimSpace(leaseOwner)).
		Updates(map[string]any{
			"input_manifest_json": manifestJSON,
			"updated_at":          gorm.Expr("clock_timestamp()"),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return domainreview.ErrModerationJobNotOwned
	}
	return nil
}

func (r *Repository) RenewModerationJobLease(
	ctx context.Context,
	jobID int64,
	leaseOwner string,
	leaseTTL time.Duration,
) error {
	if jobID <= 0 || strings.TrimSpace(leaseOwner) == "" || leaseTTL <= 0 {
		return domainreview.ErrInvalidModerationJob
	}
	result := r.db.WithContext(ctx).Model(&ModerationJobModel{}).
		Where(`id = ? AND status = ? AND lease_owner = ?
			AND lease_until > clock_timestamp()`,
			jobID, domainreview.ModerationJobLeased, strings.TrimSpace(leaseOwner)).
		Updates(map[string]any{
			"lease_until": gorm.Expr(
				"clock_timestamp() + (? * interval '1 millisecond')",
				leaseTTL.Milliseconds(),
			),
			"updated_at": gorm.Expr("clock_timestamp()"),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return domainreview.ErrModerationJobNotOwned
	}
	return nil
}

func (r *Repository) MarkModerationJobRetry(
	ctx context.Context,
	jobID int64,
	leaseOwner string,
	availableAt time.Time,
	errorCode string,
) error {
	return r.updateOwnedModerationJob(ctx, jobID, leaseOwner, map[string]any{
		"status": domainreview.ModerationJobRetryWait, "available_at": availableAt,
		"lease_owner": "", "lease_until": nil, "last_error_code": boundedModerationError(errorCode),
	})
}

func (r *Repository) MarkModerationJobSubmitted(
	ctx context.Context,
	jobID int64,
	leaseOwner string,
	submittedAt time.Time,
) error {
	return r.updateOwnedModerationJob(ctx, jobID, leaseOwner, map[string]any{
		"status": domainreview.ModerationJobSubmitted, "submitted_at": submittedAt,
		"lease_owner": "", "lease_until": nil, "last_error_code": "",
	})
}

func (r *Repository) MarkModerationJobTerminal(
	ctx context.Context,
	jobID int64,
	leaseOwner string,
	errorCode string,
) error {
	return r.updateOwnedModerationJob(ctx, jobID, leaseOwner, map[string]any{
		"status":      domainreview.ModerationJobTerminal,
		"lease_owner": "", "lease_until": nil, "last_error_code": boundedModerationError(errorCode),
	})
}

func (r *Repository) CancelModerationJob(
	ctx context.Context,
	jobID int64,
	leaseOwner string,
	reason string,
) error {
	result := r.db.WithContext(ctx).Model(&ModerationJobModel{}).
		Where(`id = ? AND status = ? AND lease_owner = ?
			AND lease_until > clock_timestamp()`,
			jobID, domainreview.ModerationJobLeased, strings.TrimSpace(leaseOwner)).
		Updates(map[string]any{
			"status":       domainreview.ModerationJobCancelled,
			"cancelled_at": gorm.Expr("clock_timestamp()"),
			"lease_owner":  "", "lease_until": nil,
			"last_error_code": boundedModerationError(reason),
			"updated_at":      gorm.Expr("clock_timestamp()"),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return domainreview.ErrModerationJobNotOwned
	}
	return nil
}

func (r *Repository) ModerationJobCurrent(
	ctx context.Context,
	job *domainreview.ModerationJob,
) (bool, error) {
	if job == nil {
		return false, domainreview.ErrInvalidModerationJob
	}
	var count int64
	err := r.db.WithContext(ctx).Table("review_case AS rc").
		Joins("JOIN video AS v ON v.id = rc.video_id").
		Where(`rc.id = ? AND rc.video_id = ? AND rc.review_version = ?
			AND rc.status = ? AND v.review_version = ? AND v.status = ?`,
			job.CaseID, job.VideoID, job.ReviewVersion,
			domainreview.CaseStatusOpen, job.ReviewVersion, domainvideo.StatusPendingReview).
		Count(&count).Error
	return count == 1, err
}

func (r *Repository) ReconcileModerationJobs(
	ctx context.Context,
	config domainreview.ModerationJobConfig,
	limit int,
) (domainreview.ModerationReconciliationStats, error) {
	if err := domainreview.ValidateModerationJobConfig(config); err != nil {
		return domainreview.ModerationReconciliationStats{}, err
	}
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	stats := domainreview.ModerationReconciliationStats{}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now, err := databaseNow(tx)
		if err != nil {
			return err
		}
		recovered := tx.Model(&ModerationJobModel{}).
			Where("status = ? AND lease_until <= ?", domainreview.ModerationJobLeased, now).
			Updates(map[string]any{
				"status":       domainreview.ModerationJobRetryWait,
				"available_at": now, "lease_owner": "", "lease_until": nil,
				"updated_at": now,
			})
		if recovered.Error != nil {
			return recovered.Error
		}
		stats.RecoveredLeases = recovered.RowsAffected

		cancelled := tx.Model(&ModerationJobModel{}).
			Where("status IN ?", []string{
				domainreview.ModerationJobPending, domainreview.ModerationJobRetryWait,
				domainreview.ModerationJobLeased,
			}).
			Where(`NOT EXISTS (
				SELECT 1 FROM review_case rc
				JOIN video v ON v.id = rc.video_id
				WHERE rc.id = review_moderation_job.case_id
				  AND rc.video_id = review_moderation_job.video_id
				  AND rc.review_version = review_moderation_job.review_version
				  AND rc.status = ?
				  AND v.review_version = review_moderation_job.review_version
				  AND v.status = ?
			) AND NOT EXISTS (
				SELECT 1 FROM review_machine_result accepted
				WHERE accepted.result_id = review_moderation_job.result_id
			)`, domainreview.CaseStatusOpen, domainvideo.StatusPendingReview).
			Updates(map[string]any{
				"status":       domainreview.ModerationJobCancelled,
				"cancelled_at": now, "lease_owner": "", "lease_until": nil,
				"last_error_code": "stale_subject", "updated_at": now,
			})
		if cancelled.Error != nil {
			return cancelled.Error
		}
		stats.Cancelled = cancelled.RowsAffected

		var cases []CaseModel
		if err := tx.Table("review_case AS rc").Select("rc.*").
			Joins("JOIN video v ON v.id = rc.video_id").
			Where(`rc.status = ? AND v.status = ? AND v.review_version = rc.review_version
				AND NOT EXISTS (
					SELECT 1 FROM review_moderation_job job
					WHERE job.case_id = rc.id
					  AND job.review_version = rc.review_version
					  AND job.provider_config_version = ?
				)`,
				domainreview.CaseStatusOpen, domainvideo.StatusPendingReview,
				config.ProviderConfigVersion).
			Order("rc.created_at ASC").Order("rc.id ASC").Limit(limit).
			Find(&cases).Error; err != nil {
			return err
		}
		for _, reviewCase := range cases {
			job, err := domainreview.NewModerationJob(
				reviewCase.ID, reviewCase.VideoID, reviewCase.ReviewVersion, config, now,
			)
			if err != nil {
				return err
			}
			result := tx.Clauses(clause.OnConflict{DoNothing: true}).
				Create(moderationJobModelFromDomain(job))
			if result.Error != nil {
				return result.Error
			}
			stats.Created += result.RowsAffected
		}
		return nil
	})
	return stats, err
}

func (r *Repository) updateOwnedModerationJob(
	ctx context.Context,
	jobID int64,
	leaseOwner string,
	updates map[string]any,
) error {
	if jobID <= 0 || strings.TrimSpace(leaseOwner) == "" {
		return domainreview.ErrInvalidModerationJob
	}
	updates["updated_at"] = gorm.Expr("clock_timestamp()")
	result := r.db.WithContext(ctx).Model(&ModerationJobModel{}).
		Where("id = ? AND status = ? AND lease_owner = ? AND lease_until > clock_timestamp()",
			jobID, domainreview.ModerationJobLeased, strings.TrimSpace(leaseOwner)).
		Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return domainreview.ErrModerationJobNotOwned
	}
	return nil
}

func moderationJobModelFromDomain(job *domainreview.ModerationJob) *ModerationJobModel {
	return &ModerationJobModel{
		ID: job.ID, CaseID: job.CaseID, VideoID: job.VideoID,
		ReviewVersion: job.ReviewVersion, ProviderConfigVersion: job.ProviderConfigVersion,
		InputProfileVersion: job.InputProfileVersion, RolloutMode: job.RolloutMode,
		Status: job.Status, ResultID: job.ResultID, RequestID: job.RequestID,
		Attempts: job.Attempts, MaxAttempts: job.MaxAttempts,
		AvailableAt: job.AvailableAt, LeaseOwner: job.LeaseOwner, LeaseUntil: job.LeaseUntil,
		InputManifestJSON: job.InputManifestJSON, LastErrorCode: job.LastErrorCode,
		SubmittedAt: job.SubmittedAt, CancelledAt: job.CancelledAt,
		CreatedAt: job.CreatedAt, UpdatedAt: job.UpdatedAt,
	}
}

func restoreModerationJob(model ModerationJobModel) *domainreview.ModerationJob {
	return &domainreview.ModerationJob{
		ID: model.ID, CaseID: model.CaseID, VideoID: model.VideoID,
		ReviewVersion: model.ReviewVersion, ProviderConfigVersion: model.ProviderConfigVersion,
		InputProfileVersion: model.InputProfileVersion, RolloutMode: model.RolloutMode,
		Status: model.Status, ResultID: model.ResultID, RequestID: model.RequestID,
		Attempts: model.Attempts, MaxAttempts: model.MaxAttempts,
		AvailableAt: model.AvailableAt, LeaseOwner: model.LeaseOwner, LeaseUntil: model.LeaseUntil,
		InputManifestJSON: model.InputManifestJSON, LastErrorCode: model.LastErrorCode,
		SubmittedAt: model.SubmittedAt, CancelledAt: model.CancelledAt,
		CreatedAt: model.CreatedAt, UpdatedAt: model.UpdatedAt,
	}
}

func boundedModerationError(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if len(value) > domainreview.MaxModerationErrorCodeLength {
		value = value[:domainreview.MaxModerationErrorCodeLength]
	}
	return value
}
