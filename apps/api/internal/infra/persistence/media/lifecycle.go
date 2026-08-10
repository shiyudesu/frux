package inframedia

import (
	"context"
	"strings"
	"time"

	domainmedia "github.com/shiyudesu/frux/internal/domain/media"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func AppendVideoLifecycleTask(tx *gorm.DB, task *domainmedia.VideoLifecycleTask) error {
	if tx == nil || task == nil {
		return domainmedia.ErrInvalidLifecycleTask
	}
	model := videoLifecycleTaskModelFromDomain(task)
	return tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "dedupe_key"}},
		DoNothing: true,
	}).Create(&model).Error
}

func (r *Repository) ClaimVideoLifecycleTasks(
	ctx context.Context,
	owner string,
	now, leaseUntil time.Time,
	limit int,
) ([]*domainmedia.VideoLifecycleTask, error) {
	owner = strings.TrimSpace(owner)
	if owner == "" || limit <= 0 {
		return []*domainmedia.VideoLifecycleTask{}, nil
	}
	var models []VideoLifecycleTaskModel
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where(`next_attempt_at <= ? AND (
				state IN (?, ?) OR (state = ? AND lease_until <= ?)
			)`,
				now,
				domainmedia.JobStatePending,
				domainmedia.JobStateRetryable,
				domainmedia.JobStateProcessing,
				now,
			).
			Order("next_attempt_at ASC").Order("id ASC").Limit(limit).
			Find(&models).Error; err != nil {
			return err
		}
		if len(models) == 0 {
			return nil
		}
		ids := make([]int64, 0, len(models))
		for _, model := range models {
			ids = append(ids, model.ID)
		}
		if err := tx.Model(&VideoLifecycleTaskModel{}).Where("id IN ?", ids).
			Updates(map[string]any{
				"state":       domainmedia.JobStateProcessing,
				"attempts":    gorm.Expr("attempts + 1"),
				"lease_owner": owner,
				"lease_until": leaseUntil,
				"updated_at":  now,
			}).Error; err != nil {
			return err
		}
		return tx.Where("id IN ?", ids).Order("id ASC").Find(&models).Error
	})
	if err != nil {
		return nil, err
	}
	tasks := make([]*domainmedia.VideoLifecycleTask, 0, len(models))
	for _, model := range models {
		tasks = append(tasks, videoLifecycleTaskFromModel(model))
	}
	return tasks, nil
}

func (r *Repository) UpdateVideoLifecycleTaskOwned(
	ctx context.Context,
	task *domainmedia.VideoLifecycleTask,
	owner string,
) error {
	if task == nil || task.ID <= 0 || strings.TrimSpace(owner) == "" {
		return domainmedia.ErrInvalidLifecycleTask
	}
	result := r.db.WithContext(ctx).Model(&VideoLifecycleTaskModel{}).
		Where(`id = ? AND state = ? AND lease_owner = ?
			AND lease_until > clock_timestamp()`,
			task.ID, domainmedia.JobStateProcessing, strings.TrimSpace(owner)).
		Updates(map[string]any{
			"state":           task.State,
			"error_code":      task.ErrorCode,
			"next_attempt_at": task.NextAttemptAt,
			"lease_owner":     "",
			"lease_until":     nil,
			"completed_at":    task.CompletedAt,
			"updated_at":      gorm.Expr("clock_timestamp()"),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return domainmedia.ErrLifecycleTaskLeaseLost
	}
	return nil
}

func (r *Repository) VideoLifecycleBacklog(
	ctx context.Context,
) (int64, *time.Time, error) {
	var row struct {
		Count  int64
		Oldest *time.Time
	}
	err := r.db.WithContext(ctx).Model(&VideoLifecycleTaskModel{}).
		Select("COUNT(*) AS count, MIN(created_at) AS oldest").
		Where("state IN ?", []string{
			domainmedia.JobStatePending,
			domainmedia.JobStateProcessing,
			domainmedia.JobStateRetryable,
		}).
		Scan(&row).Error
	return row.Count, row.Oldest, err
}

func videoLifecycleTaskModelFromDomain(task *domainmedia.VideoLifecycleTask) VideoLifecycleTaskModel {
	return VideoLifecycleTaskModel{
		ID: task.ID, DedupeKey: task.DedupeKey, VideoID: task.VideoID,
		MediaAssetID: task.MediaAssetID, CoverAssetID: task.CoverAssetID,
		Action: task.Action, RequiredStatus: task.RequiredStatus,
		RequiredVisibility: task.RequiredVisibility, State: task.State,
		Attempts: task.Attempts, MaxAttempts: task.MaxAttempts,
		ErrorCode: task.ErrorCode, LeaseOwner: task.LeaseOwner,
		LeaseUntil: task.LeaseUntil, NextAttemptAt: task.NextAttemptAt,
		CompletedAt: task.CompletedAt,
	}
}

func videoLifecycleTaskFromModel(model VideoLifecycleTaskModel) *domainmedia.VideoLifecycleTask {
	return &domainmedia.VideoLifecycleTask{
		ID: model.ID, DedupeKey: model.DedupeKey, VideoID: model.VideoID,
		MediaAssetID: model.MediaAssetID, CoverAssetID: model.CoverAssetID,
		Action: model.Action, RequiredStatus: model.RequiredStatus,
		RequiredVisibility: model.RequiredVisibility, State: model.State,
		Attempts: model.Attempts, MaxAttempts: model.MaxAttempts,
		ErrorCode: model.ErrorCode, LeaseOwner: model.LeaseOwner,
		LeaseUntil: model.LeaseUntil, NextAttemptAt: model.NextAttemptAt,
		CreatedAt: model.CreatedAt, UpdatedAt: model.UpdatedAt,
		CompletedAt: model.CompletedAt,
	}
}
