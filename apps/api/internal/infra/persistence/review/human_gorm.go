package infrareview

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	domainadminaudit "github.com/shiyudesu/frux/internal/domain/adminaudit"
	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
	domainmessage "github.com/shiyudesu/frux/internal/domain/message"
	domainreview "github.com/shiyudesu/frux/internal/domain/review"
	domainvideo "github.com/shiyudesu/frux/internal/domain/video"
	infravideo "github.com/shiyudesu/frux/internal/infra/persistence/video"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func EnsureHumanReviewPriorities(db *gorm.DB) error {
	return db.Model(&CaseModel{}).
		Where("status = ? AND priority < ?", domainreview.CaseStatusPendingHuman, 1).
		Update("priority", 1).
		Error
}

func (r *Repository) ListHumanQueue(ctx context.Context, filter domainreview.HumanQueueFilter) ([]*domainreview.HumanQueueItem, error) {
	if filter.Limit < 1 || filter.Limit > 101 || !domainreview.ValidPriority(filter.MinPriority) ||
		!domainreview.ValidPriority(filter.MaxPriority) || filter.MinPriority > filter.MaxPriority {
		return nil, domainreview.ErrInvalidQueueFilter
	}
	query := r.db.WithContext(ctx).Table("review_case AS rc").
		Select("rc.*, v.author_id, v.title, v.media_url, v.cover_url, clock_timestamp() AS database_now").
		Joins("JOIN video AS v ON v.id = rc.video_id").
		Where(`rc.status = ? AND rc.priority BETWEEN ? AND ? AND
			v.status = ? AND v.review_version = rc.review_version AND
			(rc.assigned_reviewer_id = 0 OR
			 (rc.assigned_reviewer_id > 0 AND rc.lease_expires_at <= clock_timestamp()))`,
			domainreview.CaseStatusPendingHuman, filter.MinPriority, filter.MaxPriority,
			domainvideo.StatusPendingReview)
	if cursor := filter.Cursor; cursor != nil {
		if (cursor.Scope != "" && cursor.Scope != domainreview.HumanQueueScopeAvailable) ||
			!domainreview.ValidPriority(cursor.Priority) || cursor.CaseID <= 0 || cursor.SortTime.IsZero() {
			return nil, domainreview.ErrInvalidQueueCursor
		}

		query = query.Where(
			"(rc.priority < ? OR (rc.priority = ? AND rc.created_at > ?) OR (rc.priority = ? AND rc.created_at = ? AND rc.id > ?))",
			cursor.Priority, cursor.Priority, cursor.SortTime.UTC(),
			cursor.Priority, cursor.SortTime.UTC(), cursor.CaseID,
		)
	}
	type queueRow struct {
		CaseModel
		AuthorID    int64
		Title       string
		MediaURL    string
		CoverURL    string
		DatabaseNow time.Time
	}
	var rows []queueRow
	if err := query.Order("rc.priority DESC").Order("rc.created_at ASC").Order("rc.id ASC").
		Limit(filter.Limit).Scan(&rows).Error; err != nil {
		return nil, err
	}
	items := make([]*domainreview.HumanQueueItem, 0, len(rows))
	for _, row := range rows {
		reviewCase := restoreCase(row.CaseModel)
		if reviewCase.AssignedReviewerID > 0 && reviewCase.LeaseExpiresAt != nil &&
			!row.DatabaseNow.UTC().Before(reviewCase.LeaseExpiresAt.UTC()) {
			reviewCase.AssignedReviewerID = 0
			reviewCase.LeaseTokenHash = ""
			reviewCase.LeaseExpiresAt = nil
		}
		items = append(items, &domainreview.HumanQueueItem{
			Case: reviewCase, Title: strings.TrimSpace(row.Title),
			AuthorID: row.AuthorID, MediaURL: strings.TrimSpace(row.MediaURL),
			CoverURL: strings.TrimSpace(row.CoverURL),
		})
	}
	return items, nil
}

func (r *Repository) ListHumanAssigned(ctx context.Context, filter domainreview.HumanQueueFilter) ([]*domainreview.HumanQueueItem, error) {
	if filter.ReviewerID <= 0 || filter.Limit < 1 || filter.Limit > 101 ||
		!domainreview.ValidPriority(filter.MinPriority) || !domainreview.ValidPriority(filter.MaxPriority) ||
		filter.MinPriority > filter.MaxPriority {
		return nil, domainreview.ErrInvalidQueueFilter
	}
	query := r.db.WithContext(ctx).Table("review_case AS rc").
		Select("rc.*, v.author_id, v.title, v.media_url, v.cover_url, statement_timestamp() AS snapshot_at").
		Joins("JOIN video AS v ON v.id = rc.video_id").
		Where(`rc.status = ? AND rc.priority BETWEEN ? AND ? AND
			rc.assigned_reviewer_id = ? AND rc.lease_expires_at > clock_timestamp() AND
			v.status = ? AND v.review_version = rc.review_version`,
			domainreview.CaseStatusPendingHuman, filter.MinPriority, filter.MaxPriority,
			filter.ReviewerID, domainvideo.StatusPendingReview)
	if cursor := filter.Cursor; cursor != nil {
		if cursor.Scope != domainreview.HumanQueueScopeMine ||
			!domainreview.ValidPriority(cursor.Priority) || cursor.CaseID <= 0 ||
			cursor.SortTime.IsZero() || cursor.SnapshotAt.IsZero() {
			return nil, domainreview.ErrInvalidQueueCursor
		}
		query = query.Where("rc.updated_at <= ?", cursor.SnapshotAt.UTC()).Where(
			`(rc.lease_expires_at > ? OR
			 (rc.lease_expires_at = ? AND rc.priority < ?) OR
			 (rc.lease_expires_at = ? AND rc.priority = ? AND rc.id > ?))`,
			cursor.SortTime.UTC(), cursor.SortTime.UTC(), cursor.Priority,
			cursor.SortTime.UTC(), cursor.Priority, cursor.CaseID,
		)
		if len(cursor.SeenCaseIDs) > 0 {
			query = query.Where("rc.id NOT IN ?", cursor.SeenCaseIDs)
		}
	}
	type assignedRow struct {
		CaseModel
		AuthorID   int64
		Title      string
		MediaURL   string
		CoverURL   string
		SnapshotAt time.Time
	}
	var rows []assignedRow
	if err := query.Order("rc.lease_expires_at ASC").Order("rc.priority DESC").Order("rc.id ASC").
		Limit(filter.Limit).Scan(&rows).Error; err != nil {
		return nil, err
	}
	items := make([]*domainreview.HumanQueueItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, &domainreview.HumanQueueItem{
			Case: restoreCase(row.CaseModel), Title: strings.TrimSpace(row.Title),
			AuthorID: row.AuthorID, MediaURL: strings.TrimSpace(row.MediaURL),
			CoverURL: strings.TrimSpace(row.CoverURL), SnapshotAt: row.SnapshotAt.UTC(),
		})
	}
	return items, nil
}

func (r *Repository) ListHumanRecent(ctx context.Context, filter domainreview.HumanQueueFilter) ([]*domainreview.HumanQueueItem, error) {
	if filter.ReviewerID <= 0 || filter.Limit < 1 || filter.Limit > 101 ||
		!domainreview.ValidPriority(filter.MinPriority) || !domainreview.ValidPriority(filter.MaxPriority) ||
		filter.MinPriority > filter.MaxPriority {
		return nil, domainreview.ErrInvalidQueueFilter
	}
	query := r.db.WithContext(ctx).Table("review_human_decision AS hd").
		Select("rc.*, v.author_id, v.title, v.media_url, v.cover_url, hd.created_at AS decided_at").
		Joins("JOIN review_case AS rc ON rc.id = hd.case_id").
		Joins("JOIN video AS v ON v.id = rc.video_id").
		Where(`hd.reviewer_id = ? AND rc.priority BETWEEN ? AND ? AND
			hd.created_at >= clock_timestamp() - interval '30 days'`,
			filter.ReviewerID, filter.MinPriority, filter.MaxPriority)
	if cursor := filter.Cursor; cursor != nil {
		if cursor.Scope != domainreview.HumanQueueScopeRecent ||
			cursor.CaseID <= 0 || cursor.SortTime.IsZero() {
			return nil, domainreview.ErrInvalidQueueCursor
		}
		query = query.Where(
			"(hd.created_at < ? OR (hd.created_at = ? AND rc.id < ?))",
			cursor.SortTime.UTC(), cursor.SortTime.UTC(), cursor.CaseID,
		)
	}
	type recentRow struct {
		CaseModel
		AuthorID  int64
		Title     string
		MediaURL  string
		CoverURL  string
		DecidedAt time.Time
	}
	var rows []recentRow
	if err := query.Order("hd.created_at DESC").Order("rc.id DESC").
		Limit(filter.Limit).Scan(&rows).Error; err != nil {
		return nil, err
	}
	items := make([]*domainreview.HumanQueueItem, 0, len(rows))
	for _, row := range rows {
		reviewCase := restoreCase(row.CaseModel)
		decidedAt := row.DecidedAt.UTC()
		reviewCase.ClosedAt = &decidedAt
		items = append(items, &domainreview.HumanQueueItem{
			Case: reviewCase, Title: strings.TrimSpace(row.Title),
			AuthorID: row.AuthorID, MediaURL: strings.TrimSpace(row.MediaURL),
			CoverURL: strings.TrimSpace(row.CoverURL),
		})
	}
	return items, nil
}

func (r *Repository) HumanQueueStats(ctx context.Context, minPriority, maxPriority int) (int, time.Time, error) {
	if !domainreview.ValidPriority(minPriority) || !domainreview.ValidPriority(maxPriority) ||
		minPriority > maxPriority {
		return 0, time.Time{}, domainreview.ErrInvalidQueueFilter
	}
	var row struct {
		Available int
		Oldest    *time.Time
	}
	err := r.db.WithContext(ctx).Table("review_case AS rc").
		Select("COUNT(*) AS available, MIN(rc.created_at) AS oldest").
		Joins("JOIN video AS v ON v.id = rc.video_id").
		Where(`rc.status = ? AND rc.priority BETWEEN ? AND ? AND
			v.status = ? AND v.review_version = rc.review_version AND
			(rc.assigned_reviewer_id = 0 OR
			 (rc.assigned_reviewer_id > 0 AND rc.lease_expires_at <= clock_timestamp()))`,
			domainreview.CaseStatusPendingHuman, minPriority, maxPriority,
			domainvideo.StatusPendingReview).
		Scan(&row).Error
	if err != nil {
		return 0, time.Time{}, err
	}
	if row.Oldest == nil {
		return row.Available, time.Time{}, nil
	}
	return row.Available, row.Oldest.UTC(), nil
}

func (r *Repository) GetHumanCaseDetail(ctx context.Context, caseID int64) (*domainreview.HumanCaseDetail, error) {
	if caseID <= 0 {
		return nil, domainreview.ErrInvalidCaseID
	}
	if _, err := r.RecoverExpiredHumanLeases(ctx, 100); err != nil {
		return nil, err
	}
	var caseModel CaseModel
	if err := r.db.WithContext(ctx).Where("id = ?", caseID).Take(&caseModel).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domainreview.ErrReviewCaseNotFound
		}
		return nil, err
	}
	var video infravideo.VideoModel
	if err := r.db.WithContext(ctx).Where("id = ?", caseModel.VideoID).Take(&video).Error; err != nil {
		return nil, err
	}
	detail := &domainreview.HumanCaseDetail{
		Case: restoreCase(caseModel),
		Subject: domainreview.ReviewSubject{
			VideoID: video.ID, AuthorID: video.AuthorID, Title: strings.TrimSpace(video.Title),
			Description: strings.TrimSpace(video.Description), MediaURL: strings.TrimSpace(video.MediaURL),
			CoverURL: strings.TrimSpace(video.CoverURL), ReviewVersion: video.ReviewVersion,
			PreviewAllowed: video.Status != domainvideo.StatusDeleted,
			MediaAssetID:   positiveValue(video.MediaAssetID), CoverAssetID: positiveValue(video.CoverAssetID),
		},
	}
	if err := r.loadHumanHistory(ctx, detail); err != nil {
		return nil, err
	}
	return detail, nil
}

func (r *Repository) ClaimHumanCase(
	ctx context.Context,
	caseID, reviewerID int64,
	tokenHash string,
	expectedVersion int,
	duration time.Duration,
) (*domainreview.ReviewCase, error) {
	var claimed *domainreview.ReviewCase
	var subjectErr error
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var model CaseModel
		if err := lockCase(tx, caseID, &model); err != nil {
			return err
		}
		var video infravideo.VideoModel
		videoErr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", model.VideoID).Take(&video).Error
		if videoErr != nil && !errors.Is(videoErr, gorm.ErrRecordNotFound) {
			return videoErr
		}
		now, err := databaseNow(tx)
		if err != nil {
			return err
		}
		if errors.Is(videoErr, gorm.ErrRecordNotFound) ||
			video.Status != domainvideo.StatusPendingReview {
			if err := retireHumanCase(
				tx, &model, reviewerID, domainreview.CaseStatusCancelled,
				domainreview.AssignmentEventCancelled, now,
			); err != nil {
				return err
			}
			subjectErr = domainreview.ErrReviewSubjectState
			return nil
		}
		if video.ReviewVersion != model.ReviewVersion {
			if err := retireHumanCase(
				tx, &model, reviewerID, domainreview.CaseStatusSuperseded,
				domainreview.AssignmentEventSuperseded, now,
			); err != nil {
				return err
			}
			subjectErr = domainreview.ErrReviewSubjectStale
			return nil
		}
		if model.AssignedReviewerID > 0 && model.LeaseExpiresAt != nil && !now.Before(model.LeaseExpiresAt.UTC()) {
			if expectedVersion <= 0 || model.Version != expectedVersion {
				return domainreview.ErrReviewCaseVersion
			}
			if err := expireLease(tx, &model, now); err != nil {
				return err
			}
			expectedVersion = model.Version
		}
		reviewCase := restoreCase(model)
		if err := reviewCase.Claim(reviewerID, tokenHash, expectedVersion, now, duration); err != nil {
			return err
		}
		if err := updateCaseLease(tx, reviewCase); err != nil {
			return err
		}
		if err := appendAssignment(tx, reviewCase, reviewerID, domainreview.AssignmentEventClaimed, reviewCase.LeaseExpiresAt, now); err != nil {
			return err
		}
		claimed = reviewCase
		return nil
	})
	if err == nil && subjectErr != nil {
		return nil, subjectErr
	}
	return claimed, err
}

func (r *Repository) ResumeHumanLease(
	ctx context.Context,
	caseID, reviewerID int64,
	tokenHash string,
	expectedVersion int,
	duration time.Duration,
) (*domainreview.ReviewCase, error) {
	var resumed *domainreview.ReviewCase
	var subjectErr error
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var model CaseModel
		if err := lockCase(tx, caseID, &model); err != nil {
			return err
		}
		var video infravideo.VideoModel
		videoErr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", model.VideoID).Take(&video).Error
		if videoErr != nil && !errors.Is(videoErr, gorm.ErrRecordNotFound) {
			return videoErr
		}
		now, err := databaseNow(tx)
		if err != nil {
			return err
		}
		if errors.Is(videoErr, gorm.ErrRecordNotFound) ||
			video.Status != domainvideo.StatusPendingReview {
			if err := retireHumanCase(
				tx, &model, reviewerID, domainreview.CaseStatusCancelled,
				domainreview.AssignmentEventCancelled, now,
			); err != nil {
				return err
			}
			subjectErr = domainreview.ErrReviewSubjectState
			return nil
		}
		if video.ReviewVersion != model.ReviewVersion {
			if err := retireHumanCase(
				tx, &model, reviewerID, domainreview.CaseStatusSuperseded,
				domainreview.AssignmentEventSuperseded, now,
			); err != nil {
				return err
			}
			subjectErr = domainreview.ErrReviewSubjectStale
			return nil
		}
		reviewCase := restoreCase(model)
		if err := reviewCase.Resume(reviewerID, tokenHash, expectedVersion, now, duration); err != nil {
			return err
		}
		if err := updateCaseLease(tx, reviewCase); err != nil {
			return err
		}
		if err := appendAssignment(
			tx, reviewCase, reviewerID, domainreview.AssignmentEventResumed, reviewCase.LeaseExpiresAt, now,
		); err != nil {
			return err
		}
		resumed = reviewCase
		return nil
	})
	if err == nil && subjectErr != nil {
		return nil, subjectErr
	}
	return resumed, err
}

func (r *Repository) RenewHumanLease(
	ctx context.Context,
	caseID, reviewerID int64,
	tokenHash string,
	expectedVersion int,
	duration time.Duration,
) (*domainreview.ReviewCase, error) {
	var renewed *domainreview.ReviewCase
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var model CaseModel
		if err := lockCase(tx, caseID, &model); err != nil {
			return err
		}
		now, err := databaseNow(tx)
		if err != nil {
			return err
		}
		reviewCase := restoreCase(model)
		if err := reviewCase.Renew(reviewerID, tokenHash, expectedVersion, now, duration); err != nil {
			return err
		}
		if err := updateCaseLease(tx, reviewCase); err != nil {
			return err
		}
		if err := appendAssignment(tx, reviewCase, reviewerID, domainreview.AssignmentEventRenewed, reviewCase.LeaseExpiresAt, now); err != nil {
			return err
		}
		renewed = reviewCase
		return nil
	})
	return renewed, err
}

func (r *Repository) ReleaseHumanLease(
	ctx context.Context,
	caseID, reviewerID int64,
	tokenHash string,
	expectedVersion int,
) (*domainreview.ReviewCase, error) {
	var released *domainreview.ReviewCase
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var model CaseModel
		if err := lockCase(tx, caseID, &model); err != nil {
			return err
		}
		now, err := databaseNow(tx)
		if err != nil {
			return err
		}
		reviewCase := restoreCase(model)
		if err := reviewCase.Release(reviewerID, tokenHash, expectedVersion, now); err != nil {
			return err
		}
		if err := updateCaseLease(tx, reviewCase); err != nil {
			return err
		}
		if err := appendAssignment(tx, reviewCase, reviewerID, domainreview.AssignmentEventReleased, nil, now); err != nil {
			return err
		}
		released = reviewCase
		return nil
	})
	return released, err
}

func (r *Repository) RecoverExpiredHumanLeases(ctx context.Context, limit int) (int, error) {
	if limit <= 0 {
		return 0, nil
	}
	recovered := 0
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var models []CaseModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("status = ? AND assigned_reviewer_id > 0 AND lease_expires_at <= clock_timestamp()",
				domainreview.CaseStatusPendingHuman).
			Order("lease_expires_at ASC").Order("id ASC").Limit(limit).Find(&models).Error; err != nil {
			return err
		}
		now, err := databaseNow(tx)
		if err != nil {
			return err
		}
		for index := range models {
			if err := expireLease(tx, &models[index], now); err != nil {
				return err
			}
			recovered++
		}
		return nil
	})
	return recovered, err
}

func (r *Repository) CommitHumanDecision(
	ctx context.Context,
	decision *domainreview.HumanDecision,
	tokenHash string,
	auditFact *domainadminaudit.Fact,
) (*domainreview.HumanDecisionResult, error) {
	if decision == nil {
		return nil, domainreview.ErrInvalidDecisionOutcome
	}
	if r.auditWriter == nil || auditFact == nil {
		return nil, domainreview.ErrReviewAuditUnavailable
	}
	var result *domainreview.HumanDecisionResult
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(
			"SELECT pg_advisory_xact_lock(hashtextextended(?, 0))",
			fmt.Sprintf("%d|%d|%s", decision.CaseID, decision.ReviewerID, decision.IdempotencyKeyHash),
		).Error; err != nil {
			return err
		}
		var receipt HumanDecisionIdempotencyModel
		findErr := tx.Where(
			"case_id = ? AND reviewer_id = ? AND idempotency_key_hash = ?",
			decision.CaseID, decision.ReviewerID, decision.IdempotencyKeyHash,
		).Take(&receipt).Error
		if findErr == nil {
			if receipt.PayloadHash != decision.PayloadHash {
				return domainreview.ErrDecisionIdentityConflict
			}
			loaded, err := loadCommittedHumanDecision(tx, receipt.DecisionID)
			if err != nil {
				return err
			}
			loaded.Duplicate = true
			result = loaded
			return nil
		}
		if !errors.Is(findErr, gorm.ErrRecordNotFound) {
			return findErr
		}

		var caseModel CaseModel
		if err := lockCase(tx, decision.CaseID, &caseModel); err != nil {
			return err
		}
		var video infravideo.VideoModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", caseModel.VideoID).Take(&video).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return domainvideo.ErrVideoNotFound
			}
			return err
		}
		now, err := databaseNow(tx)
		if err != nil {
			return err
		}
		reviewCase := restoreCase(caseModel)
		if err := reviewCase.ValidateDecision(
			decision.ReviewerID, tokenHash, decision.CaseVersion, decision.ReviewVersion, now,
		); err != nil {
			return err
		}
		if video.Status != domainvideo.StatusPendingReview {
			return domainreview.ErrReviewSubjectState
		}
		if video.ReviewVersion != reviewCase.ReviewVersion {
			return domainreview.ErrReviewSubjectStale
		}

		humanModel := HumanDecisionModel{
			CaseID: decision.CaseID, ReviewerID: decision.ReviewerID, Outcome: decision.Outcome,
			ReasonCode: decision.ReasonCode, Note: decision.Note, ReviewVersion: decision.ReviewVersion,
			CaseVersion: decision.CaseVersion, CreatedAt: now,
		}
		if err := tx.Create(&humanModel).Error; err != nil {
			return err
		}
		current := &domainvideo.Video{Status: video.Status, PublishedAt: video.PublishedAt}
		transition := domainvideo.LifecycleApprove
		nextCaseStatus := domainreview.CaseStatusApproved
		if decision.Outcome == domainreview.OutcomeReject {
			transition = domainvideo.LifecycleReject
			nextCaseStatus = domainreview.CaseStatusRejected
		}
		if err := current.ApplyLifecycleTransition(transition, now); err != nil {
			return domainreview.ErrReviewSubjectState
		}
		if err := tx.Model(&video).Updates(map[string]any{
			"status": current.Status, "published_at": current.PublishedAt, "updated_at": now,
		}).Error; err != nil {
			return err
		}
		publicDelta, privateDelta := reviewContentWorkDeltas(video, current.Status)
		if err := infravideo.AdjustContentStat(tx, video.AuthorID, publicDelta, privateDelta, 0, 0); err != nil {
			return err
		}
		reviewCase.Status = nextCaseStatus
		reviewCase.Version++
		reviewCase.UpdatedAt = now
		reviewCase.ClosedAt = &now
		reviewCase.AssignedReviewerID = 0
		reviewCase.LeaseTokenHash = ""
		reviewCase.LeaseExpiresAt = nil
		if err := tx.Model(&caseModel).Updates(map[string]any{
			"status": reviewCase.Status, "version": reviewCase.Version,
			"assigned_reviewer_id": 0, "lease_token_hash": "", "lease_expires_at": nil,
			"updated_at": now, "closed_at": now,
		}).Error; err != nil {
			return err
		}
		if err := appendAssignment(tx, reviewCase, decision.ReviewerID, domainreview.AssignmentEventDecided, nil, now); err != nil {
			return err
		}
		if err := r.auditWriter.AppendInTransaction(ctx, tx, auditFact); err != nil {
			return err
		}
		notification := reviewLifecycleNotification(
			video, decision.Outcome, decision.ReasonCode, now,
		)
		outbox := NotificationOutboxModel{
			EventID: notification.EventID, RecipientID: video.AuthorID, VideoID: video.ID,
			Outcome: decision.Outcome, State: domainreview.NotificationStatePending,
			ReviewVersion: notification.ReviewVersion,
			Stage:         notification.Stage, Result: notification.Result,
			ReasonCode: notification.ReasonCode, OccurredAt: &notification.OccurredAt,
			AvailableAt: now, CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.Create(&outbox).Error; err != nil {
			return err
		}
		if notification.Stage == domainmessage.LifecycleStagePublished {
			if err := infravideo.AppendLifecycleNotificationWithReadiness(
				tx, notification, false,
			); err != nil {
				return err
			}
		}
		receipt = HumanDecisionIdempotencyModel{
			CaseID: decision.CaseID, ReviewerID: decision.ReviewerID,
			IdempotencyKeyHash: decision.IdempotencyKeyHash, PayloadHash: decision.PayloadHash,
			DecisionID: humanModel.ID, CreatedAt: now,
		}
		if err := tx.Create(&receipt).Error; err != nil {
			return err
		}
		committed := restoreHumanDecision(humanModel)
		committed.IdempotencyKeyHash = decision.IdempotencyKeyHash
		committed.PayloadHash = decision.PayloadHash
		result = &domainreview.HumanDecisionResult{
			Case: reviewCase, Decision: committed, ApplySideEffects: true,
			MediaAssetID: positiveValue(video.MediaAssetID), CoverAssetID: positiveValue(video.CoverAssetID),
		}
		return nil
	})
	if err == nil && result != nil && !result.Duplicate {
		r.auditWriter.RecordCommittedWrite(auditFact)
	}
	return result, err
}

func (r *Repository) ClaimReviewNotifications(
	ctx context.Context,
	leaseOwner string,
	limit int,
	now, leaseUntil time.Time,
) ([]*domainreview.ReviewNotification, error) {
	leaseOwner = strings.TrimSpace(leaseOwner)
	if leaseOwner == "" || limit <= 0 {
		return []*domainreview.ReviewNotification{}, nil
	}
	items := make([]*domainreview.ReviewNotification, 0, limit)
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var models []NotificationOutboxModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("state = ? AND available_at <= ? AND (lease_until IS NULL OR lease_until <= ?)",
				domainreview.NotificationStatePending, now, now).
			Order("available_at ASC").Order("created_at ASC").Order("event_id ASC").
			Limit(limit).Find(&models).Error; err != nil {
			return err
		}
		for index := range models {
			models[index].Attempts++
			models[index].LeaseOwner = leaseOwner
			models[index].LeaseUntil = &leaseUntil
			models[index].UpdatedAt = now
			if err := tx.Model(&NotificationOutboxModel{}).Where("event_id = ?", models[index].EventID).
				Updates(map[string]any{
					"attempts": gorm.Expr("attempts + 1"), "lease_owner": leaseOwner,
					"lease_until": leaseUntil, "updated_at": now,
				}).Error; err != nil {
				return err
			}
			items = append(items, restoreNotification(models[index]))
		}
		return nil
	})
	return items, err
}

func (r *Repository) MarkReviewNotificationDelivered(ctx context.Context, eventID, leaseOwner string, deliveredAt time.Time) error {
	return r.db.WithContext(ctx).Model(&NotificationOutboxModel{}).
		Where("event_id = ? AND state = ? AND lease_owner = ?",
			strings.TrimSpace(eventID), domainreview.NotificationStatePending, strings.TrimSpace(leaseOwner)).
		Updates(map[string]any{
			"state": domainreview.NotificationStateDelivered, "delivered_at": deliveredAt,
			"lease_owner": "", "lease_until": nil, "last_error": "", "updated_at": deliveredAt,
		}).Error
}

func (r *Repository) MarkReviewNotificationFailed(
	ctx context.Context,
	eventID, leaseOwner string,
	availableAt time.Time,
	reason string,
	terminal bool,
) error {
	reason = strings.TrimSpace(reason)
	if len(reason) > 1024 {
		reason = reason[:1024]
	}
	state := domainreview.NotificationStatePending
	if terminal {
		state = domainreview.NotificationStateTerminal
	}
	return r.db.WithContext(ctx).Model(&NotificationOutboxModel{}).
		Where("event_id = ? AND state = ? AND lease_owner = ?",
			strings.TrimSpace(eventID), domainreview.NotificationStatePending, strings.TrimSpace(leaseOwner)).
		Updates(map[string]any{
			"state": state, "available_at": availableAt, "lease_owner": "",
			"lease_until": nil, "last_error": reason, "updated_at": time.Now().UTC(),
		}).Error
}

func (r *Repository) loadHumanHistory(ctx context.Context, detail *domainreview.HumanCaseDetail) error {
	var signals []SignalModel
	if err := r.db.WithContext(ctx).Where("case_id = ?", detail.Case.ID).
		Order("created_at ASC").Order("id ASC").Find(&signals).Error; err != nil {
		return err
	}
	var receipts []ResultModel
	if err := r.db.WithContext(ctx).Where("case_id = ?", detail.Case.ID).Find(&receipts).Error; err != nil {
		return err
	}
	resultIDs := make(map[int64]string, len(receipts))
	for _, receipt := range receipts {
		resultIDs[receipt.ID] = receipt.ResultID
	}
	for _, signal := range signals {
		var refs []string
		if err := json.Unmarshal([]byte(signal.EvidenceRefsJSON), &refs); err != nil {
			return err
		}
		detail.History.Signals = append(detail.History.Signals, &domainreview.EvidenceSignal{
			ID: signal.ID, ResultID: resultIDs[signal.ResultReceiptID], Label: signal.Label,
			Confidence: signal.Confidence, EvidenceRefs: refs, Provider: signal.Provider,
			ModelVersion: signal.ModelVersion, PolicyVersion: signal.PolicyVersion, CreatedAt: signal.CreatedAt,
		})
	}
	var automated []DecisionModel
	if err := r.db.WithContext(ctx).Where("case_id = ?", detail.Case.ID).
		Order("created_at ASC").Order("id ASC").Find(&automated).Error; err != nil {
		return err
	}
	for _, decision := range automated {
		detail.History.AutomatedDecisions = append(detail.History.AutomatedDecisions, &domainreview.AutomatedDecision{
			ID: decision.ID, CaseID: decision.CaseID, ResultID: resultIDs[decision.ResultReceiptID],
			Outcome: decision.Outcome, PolicyVersion: decision.PolicyVersion, CreatedAt: decision.CreatedAt,
		})
	}
	var assignments []AssignmentModel
	if err := r.db.WithContext(ctx).Where("case_id = ?", detail.Case.ID).
		Order("created_at ASC").Order("id ASC").Find(&assignments).Error; err != nil {
		return err
	}
	for _, assignment := range assignments {
		detail.History.Assignments = append(detail.History.Assignments, restoreAssignment(assignment))
	}
	var human []HumanDecisionModel
	if err := r.db.WithContext(ctx).Where("case_id = ?", detail.Case.ID).
		Order("created_at ASC").Order("id ASC").Find(&human).Error; err != nil {
		return err
	}
	for _, decision := range human {
		detail.History.HumanDecisions = append(detail.History.HumanDecisions, restoreHumanDecision(decision))
	}
	return nil
}

func loadCommittedHumanDecision(tx *gorm.DB, decisionID int64) (*domainreview.HumanDecisionResult, error) {
	var model HumanDecisionModel
	if err := tx.Where("id = ?", decisionID).Take(&model).Error; err != nil {
		return nil, err
	}
	var caseModel CaseModel
	if err := tx.Where("id = ?", model.CaseID).Take(&caseModel).Error; err != nil {
		return nil, err
	}
	var video infravideo.VideoModel
	if err := tx.Where("id = ?", caseModel.VideoID).Take(&video).Error; err != nil {
		return nil, err
	}
	return &domainreview.HumanDecisionResult{
		Case: restoreCase(caseModel), Decision: restoreHumanDecision(model),
		ApplySideEffects: video.ReviewVersion == caseModel.ReviewVersion,
		MediaAssetID:     positiveValue(video.MediaAssetID), CoverAssetID: positiveValue(video.CoverAssetID),
	}, nil
}

func lockCase(tx *gorm.DB, caseID int64, model *CaseModel) error {
	if caseID <= 0 {
		return domainreview.ErrInvalidCaseID
	}
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", caseID).Take(model).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return domainreview.ErrReviewCaseNotFound
		}
		return err
	}
	return nil
}

func databaseNow(tx *gorm.DB) (time.Time, error) {
	var now time.Time
	if err := tx.Raw("SELECT clock_timestamp()").Scan(&now).Error; err != nil {
		return time.Time{}, err
	}
	return now.UTC().Truncate(time.Microsecond), nil
}

func expireLease(tx *gorm.DB, model *CaseModel, now time.Time) error {
	reviewerID := model.AssignedReviewerID
	reviewCase := restoreCase(*model)
	if !reviewCase.Expire(now) {
		return nil
	}
	if err := updateCaseLease(tx, reviewCase); err != nil {
		return err
	}
	if err := appendAssignment(tx, reviewCase, reviewerID, domainreview.AssignmentEventExpired, nil, now); err != nil {
		return err
	}
	model.Version = reviewCase.Version
	model.AssignedReviewerID = 0
	model.LeaseTokenHash = ""
	model.LeaseExpiresAt = nil
	model.UpdatedAt = now
	return nil
}

func updateCaseLease(tx *gorm.DB, reviewCase *domainreview.ReviewCase) error {
	return tx.Model(&CaseModel{}).Where("id = ?", reviewCase.ID).Updates(map[string]any{
		"version": reviewCase.Version, "assigned_reviewer_id": reviewCase.AssignedReviewerID,
		"lease_token_hash": reviewCase.LeaseTokenHash, "lease_expires_at": reviewCase.LeaseExpiresAt,
		"updated_at": reviewCase.UpdatedAt,
	}).Error
}

func retireHumanCase(
	tx *gorm.DB,
	model *CaseModel,
	reviewerID int64,
	status, event string,
	now time.Time,
) error {
	if model.Status != domainreview.CaseStatusPendingHuman {
		return domainreview.ErrReviewCaseNotHuman
	}
	reviewCase := restoreCase(*model)
	reviewCase.Status = status
	reviewCase.Version++
	reviewCase.AssignedReviewerID = 0
	reviewCase.LeaseTokenHash = ""
	reviewCase.LeaseExpiresAt = nil
	reviewCase.UpdatedAt = now
	reviewCase.ClosedAt = &now
	if err := tx.Model(model).Updates(map[string]any{
		"status": status, "version": reviewCase.Version,
		"assigned_reviewer_id": 0, "lease_token_hash": "", "lease_expires_at": nil,
		"updated_at": now, "closed_at": now,
	}).Error; err != nil {
		return err
	}
	return appendAssignment(tx, reviewCase, reviewerID, event, nil, now)
}

func appendAssignment(
	tx *gorm.DB,
	reviewCase *domainreview.ReviewCase,
	reviewerID int64,
	event string,
	leaseUntil *time.Time,
	now time.Time,
) error {
	if reviewerID <= 0 || !domainreview.ValidAssignmentEvent(event) {
		return domainreview.ErrInvalidReviewerID
	}
	return tx.Create(&AssignmentModel{
		CaseID: reviewCase.ID, ReviewerID: reviewerID, Event: event,
		CaseVersion: reviewCase.Version, LeaseUntil: leaseUntil, CreatedAt: now,
	}).Error
}

func restoreAssignment(model AssignmentModel) *domainreview.ReviewerAssignment {
	return &domainreview.ReviewerAssignment{
		ID: model.ID, CaseID: model.CaseID, ReviewerID: model.ReviewerID,
		Event: model.Event, CaseVersion: model.CaseVersion,
		LeaseUntil: model.LeaseUntil, CreatedAt: model.CreatedAt,
	}
}

func restoreHumanDecision(model HumanDecisionModel) *domainreview.HumanDecision {
	return &domainreview.HumanDecision{
		ID: model.ID, CaseID: model.CaseID, ReviewerID: model.ReviewerID,
		Outcome: model.Outcome, ReasonCode: model.ReasonCode, Note: model.Note,
		ReviewVersion: model.ReviewVersion, CaseVersion: model.CaseVersion,
		CreatedAt: model.CreatedAt,
	}
}

func restoreNotification(model NotificationOutboxModel) *domainreview.ReviewNotification {
	occurredAt := model.CreatedAt
	if model.OccurredAt != nil {
		occurredAt = model.OccurredAt.UTC()
	}
	return &domainreview.ReviewNotification{
		EventID: model.EventID, RecipientID: model.RecipientID, VideoID: model.VideoID,
		Outcome: model.Outcome, State: model.State, Attempts: model.Attempts,
		ReviewVersion: model.ReviewVersion, Stage: model.Stage, Result: model.Result,
		ReasonCode: model.ReasonCode, OccurredAt: occurredAt,
		AvailableAt: model.AvailableAt, LeaseOwner: model.LeaseOwner, LeaseUntil: model.LeaseUntil,
		LastError: model.LastError, DeliveredAt: model.DeliveredAt,
		CreatedAt: model.CreatedAt, UpdatedAt: model.UpdatedAt,
	}
}

func reviewLifecycleNotification(
	video infravideo.VideoModel,
	outcome string,
	reasonCode string,
	occurredAt time.Time,
) domainmessage.LifecycleNotification {
	notification := domainmessage.LifecycleNotification{
		RecipientID: video.AuthorID, VideoID: video.ID,
		ReviewVersion: video.ReviewVersion, OccurredAt: occurredAt,
	}
	if outcome == domainreview.OutcomeReject {
		notification.EventID = domainmessage.ReviewEventID(
			video.ID, video.ReviewVersion, domainmessage.LifecycleResultRejected,
		)
		notification.Stage = domainmessage.LifecycleStageReview
		notification.Result = domainmessage.LifecycleResultRejected
		notification.ReasonCode = reasonCode
		return notification
	}
	if video.Visibility == domainvideo.VisibilityPublic &&
		domainmedia.IsPublicReadyStatus(video.MediaStatus) {
		notification.EventID = domainmessage.PublicationEventID(video.ID, video.ReviewVersion)
		notification.Stage = domainmessage.LifecycleStagePublished
		notification.Result = domainmessage.LifecycleResultPublic
		return notification
	}
	notification.EventID = domainmessage.ReviewEventID(
		video.ID, video.ReviewVersion, domainmessage.LifecycleResultApproved,
	)
	notification.Stage = domainmessage.LifecycleStageReview
	notification.Result = domainmessage.LifecycleResultApproved
	return notification
}
