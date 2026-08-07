package infravideo

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	domainadminaudit "github.com/shiyudesu/frux/internal/domain/adminaudit"
	domainmessage "github.com/shiyudesu/frux/internal/domain/message"
	domainvideo "github.com/shiyudesu/frux/internal/domain/video"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (r *Repository) ListAdminVideos(
	ctx context.Context,
	filter domainvideo.AdminVideoQuery,
) ([]*domainvideo.Video, error) {
	filter, err := domainvideo.NormalizeAdminVideoQuery(filter)
	if err != nil {
		return nil, err
	}
	var models []videoWithStatModel
	query := r.db.WithContext(ctx).
		Table("video AS v").
		Select(videoWithStatSelect()).
		Joins("LEFT JOIN video_stat AS vs ON vs.video_id = v.id").
		Where("v.status <> ?", domainvideo.StatusDeleted)
	if filter.Status != 0 {
		query = query.Where("v.status = ?", filter.Status)
	}
	if filter.AuthorID > 0 {
		query = query.Where("v.author_id = ?", filter.AuthorID)
	}
	if filter.VideoID > 0 {
		query = query.Where("v.id = ?", filter.VideoID)
	}
	if filter.Keyword != "" {
		pattern := likePattern(filter.Keyword)
		query = query.Where(
			"(v.title ILIKE ? ESCAPE '\\' OR v.description ILIKE ? ESCAPE '\\')",
			pattern,
			pattern,
		)
	}
	if filter.CreatedFrom != nil {
		query = query.Where("v.created_at >= ? AND v.created_at <= ?", *filter.CreatedFrom, *filter.CreatedTo)
	}
	if filter.Cursor != nil {
		query = query.Where(
			"(v.created_at < ? OR (v.created_at = ? AND v.id < ?))",
			filter.Cursor.CreatedAt.UTC(),
			filter.Cursor.CreatedAt.UTC(),
			filter.Cursor.VideoID,
		)
	}
	if err := query.Order("v.created_at DESC").Order("v.id DESC").
		Limit(filter.Limit).Scan(&models).Error; err != nil {
		return nil, err
	}
	videos := make([]*domainvideo.Video, 0, len(models))
	for _, model := range models {
		videos = append(videos, restoreVideo(model))
	}
	return videos, nil
}

func (r *Repository) CommitAdminTransition(
	ctx context.Context,
	raw domainvideo.AdminTransitionCommand,
	auditFact *domainadminaudit.Fact,
) (*domainvideo.AdminTransitionResult, error) {
	command, err := domainvideo.NormalizeAdminTransition(raw)
	if err != nil {
		return nil, err
	}
	if r.auditWriter == nil || auditFact == nil {
		return nil, domainadminaudit.ErrAuditWriteFailed
	}
	var result *domainvideo.AdminTransitionResult
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current VideoModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", command.VideoID).Take(&current).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return domainvideo.ErrVideoNotFound
			}
			return err
		}
		if current.Version != command.ExpectedVersion {
			return domainvideo.ErrVideoVersionConflict
		}
		video := &domainvideo.Video{Status: current.Status, PublishedAt: current.PublishedAt}
		if err := video.ApplyLifecycleTransition(command.Transition, command.OccurredAt); err != nil {
			return err
		}
		if video.Status == current.Status {
			return domainvideo.ErrVideoVersionConflict
		}
		previousStatus := current.Status
		previousVersion := current.Version
		current.Status = video.Status
		current.PublishedAt = video.PublishedAt
		current.Version++
		current.UpdatedAt = command.OccurredAt
		if err := tx.Model(&current).Updates(map[string]any{
			"status": current.Status, "published_at": current.PublishedAt,
			"version": current.Version, "updated_at": current.UpdatedAt,
		}).Error; err != nil {
			return err
		}
		publicDelta, privateDelta := contentWorkDeltas(
			previousStatus, current.Visibility, current.MediaStatus,
			current.Status, current.Visibility, current.MediaStatus,
		)
		if err := AdjustContentStat(tx, current.AuthorID, publicDelta, privateDelta, 0, 0); err != nil {
			return err
		}
		action := &EnforcementActionModel{
			VideoID: current.ID, ActorID: command.ActorID, Action: string(command.Transition),
			ReasonCode: command.ReasonCode, Note: command.Note,
			PreviousStatus: previousStatus, NewStatus: current.Status,
			PreviousVersion: previousVersion, NewVersion: current.Version,
			CreatedAt: command.OccurredAt,
		}
		if err := tx.Create(action).Error; err != nil {
			return err
		}
		if err := r.auditWriter.AppendInTransaction(ctx, tx, auditFact); err != nil {
			return err
		}
		intent := &AdminTransitionIntentModel{
			EventID: fmt.Sprintf("video-admin-transition:%d", action.ID),
			VideoID: current.ID, State: domainvideo.AdminIntentStatePending,
			AvailableAt: command.OccurredAt,
			CreatedAt:   command.OccurredAt, UpdatedAt: command.OccurredAt,
		}
		if err := tx.Create(intent).Error; err != nil {
			return err
		}
		notification := domainmessage.LifecycleNotification{
			RecipientID: current.AuthorID, VideoID: current.ID,
			ReviewVersion: current.ReviewVersion, OccurredAt: command.OccurredAt,
		}
		switch command.Transition {
		case domainvideo.LifecycleTakeOffline:
			notification.EventID = domainmessage.EnforcementEventID(current.ID, action.ID)
			notification.Stage = domainmessage.LifecycleStageEnforcement
			notification.Result = domainmessage.LifecycleResultTakenDown
			notification.ReasonCode = command.ReasonCode
		case domainvideo.LifecycleRestore:
			notification.EventID = domainmessage.RestorationEventID(current.ID, action.ID)
			notification.Stage = domainmessage.LifecycleStageRestoration
			notification.Result = domainmessage.LifecycleResultRestored
			notification.ReasonCode = "compliance_restored"
		}
		if notification.EventID != "" {
			if err := AppendLifecycleNotification(tx, notification); err != nil {
				return err
			}
		}
		domainResult := restoreVideo(videoWithStatModel{
			ID: current.ID, AuthorID: current.AuthorID, Title: current.Title,
			Description: current.Description, MediaURL: current.MediaURL, CoverURL: current.CoverURL,
			MediaAssetID: current.MediaAssetID, CoverAssetID: current.CoverAssetID,
			MediaStatus: current.MediaStatus, MediaErrorCode: current.MediaErrorCode,
			ReviewVersion: current.ReviewVersion, Version: current.Version,
			Status: current.Status, Visibility: current.Visibility, PublishedAt: current.PublishedAt,
			IdempotencyKey: current.IdempotencyKey, CreatedAt: current.CreatedAt, UpdatedAt: current.UpdatedAt,
		})
		result = &domainvideo.AdminTransitionResult{
			Video: domainResult, PreviousStatus: previousStatus,
		}
		return nil
	})
	if err == nil {
		r.auditWriter.RecordCommittedWrite(auditFact)
	}
	return result, err
}

func (r *Repository) ClaimAdminTransitionIntents(
	ctx context.Context,
	leaseOwner string,
	limit int,
	now, leaseUntil time.Time,
) ([]*domainvideo.AdminTransitionIntent, error) {
	leaseOwner = strings.TrimSpace(leaseOwner)
	if leaseOwner == "" || limit <= 0 {
		return []*domainvideo.AdminTransitionIntent{}, nil
	}
	items := make([]*domainvideo.AdminTransitionIntent, 0, limit)
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var models []AdminTransitionIntentModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where(
				"state = ? AND available_at <= ? AND (lease_until IS NULL OR lease_until <= ?)",
				domainvideo.AdminIntentStatePending, now, now,
			).
			Order("available_at ASC").Order("created_at ASC").Order("id ASC").
			Limit(limit).Find(&models).Error; err != nil {
			return err
		}
		for index := range models {
			models[index].Attempts++
			if err := tx.Model(&AdminTransitionIntentModel{}).
				Where("id = ? AND state = ?", models[index].ID, domainvideo.AdminIntentStatePending).
				Updates(map[string]any{
					"attempts": gorm.Expr("attempts + 1"), "lease_owner": leaseOwner,
					"lease_until": leaseUntil, "updated_at": now,
				}).Error; err != nil {
				return err
			}
			items = append(items, &domainvideo.AdminTransitionIntent{
				ID: models[index].ID, EventID: models[index].EventID,
				VideoID: models[index].VideoID, Attempts: models[index].Attempts,
			})
		}
		return nil
	})
	return items, err
}

func (r *Repository) MarkAdminTransitionIntentDelivered(
	ctx context.Context,
	intentID int64,
	leaseOwner string,
	deliveredAt time.Time,
) error {
	result := r.db.WithContext(ctx).Model(&AdminTransitionIntentModel{}).
		Where(
			"id = ? AND state = ? AND lease_owner = ?",
			intentID, domainvideo.AdminIntentStatePending, strings.TrimSpace(leaseOwner),
		).
		Updates(map[string]any{
			"state":        domainvideo.AdminIntentStateDelivered,
			"delivered_at": deliveredAt.UTC(),
			"lease_owner":  "",
			"lease_until":  nil,
			"last_error":   "",
			"updated_at":   deliveredAt.UTC(),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *Repository) MarkAdminTransitionIntentFailed(
	ctx context.Context,
	intentID int64,
	leaseOwner string,
	availableAt time.Time,
	reason string,
) error {
	reason = strings.TrimSpace(reason)
	runes := []rune(reason)
	if len(runes) > 1024 {
		reason = string(runes[:1024])
	}
	result := r.db.WithContext(ctx).Model(&AdminTransitionIntentModel{}).
		Where(
			"id = ? AND state = ? AND lease_owner = ?",
			intentID, domainvideo.AdminIntentStatePending, strings.TrimSpace(leaseOwner),
		).
		Updates(map[string]any{
			"available_at": availableAt.UTC(), "lease_owner": "",
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

var _ domainvideo.AdminRepository = (*Repository)(nil)
