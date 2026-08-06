package infrareview

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
	domainreview "github.com/shiyudesu/frux/internal/domain/review"
	domainvideo "github.com/shiyudesu/frux/internal/domain/video"
	infravideo "github.com/shiyudesu/frux/internal/infra/persistence/video"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Repository struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Repository { return &Repository{db: db} }

func (r *Repository) CreateOrGetCase(ctx context.Context, videoID int64) (*domainreview.ReviewCase, bool, error) {
	if videoID <= 0 {
		return nil, false, domainreview.ErrInvalidVideoID
	}
	var model CaseModel
	created := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var video infravideo.VideoModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", videoID).Take(&video).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return domainvideo.ErrVideoNotFound
			}
			return err
		}
		if video.ReviewVersion <= 0 {
			return domainreview.ErrInvalidReviewVersion
		}
		if video.Status != domainvideo.StatusPendingReview {
			return domainreview.ErrReviewSubjectState
		}
		if !domainmedia.IsPublicReadyStatus(video.MediaStatus) {
			return domainreview.ErrReviewSubjectNotReady
		}
		findErr := tx.Where("video_id = ? AND review_version = ?", video.ID, video.ReviewVersion).Take(&model).Error
		if findErr == nil {
			return nil
		}
		if !errors.Is(findErr, gorm.ErrRecordNotFound) {
			return findErr
		}
		policy, err := loadActivePolicy(tx)
		if err != nil {
			return err
		}
		reviewCase, err := domainreview.NewCase(video.ID, video.ReviewVersion, policy.Version, time.Now().UTC())
		if err != nil {
			return err
		}
		model = CaseModel{
			VideoID: reviewCase.VideoID, ReviewVersion: reviewCase.ReviewVersion,
			Status: reviewCase.Status, PolicyVersion: reviewCase.PolicyVersion,
			CreatedAt: reviewCase.CreatedAt, UpdatedAt: reviewCase.UpdatedAt,
		}
		if err := tx.Create(&model).Error; err != nil {
			return err
		}
		created = true
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return restoreCase(model), created, nil
}

func (r *Repository) ProcessMachineResult(ctx context.Context, result *domainreview.MachineResult) (*domainreview.ProcessingResult, error) {
	if result == nil {
		return nil, domainreview.ErrInvalidSignal
	}
	var processed *domainreview.ProcessingResult
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", result.Provider+"|"+result.ResultID).Error; err != nil {
			return err
		}
		var existing ResultModel
		findErr := tx.Where("provider = ? AND result_id = ?", result.Provider, result.ResultID).Take(&existing).Error
		if findErr == nil {
			if existing.PayloadHash != result.PayloadHash {
				return domainreview.ErrResultIdentityConflict
			}
			var err error
			processed, err = loadProcessingResult(tx, existing, true)
			return err
		}
		if !errors.Is(findErr, gorm.ErrRecordNotFound) {
			return findErr
		}

		var caseModel CaseModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", result.CaseID).Take(&caseModel).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return domainreview.ErrReviewCaseNotFound
			}
			return err
		}
		if caseModel.Status != domainreview.CaseStatusOpen {
			return domainreview.ErrReviewCaseNotOpen
		}
		if caseModel.VideoID != result.VideoID || caseModel.ReviewVersion != result.ReviewVersion ||
			caseModel.PolicyVersion != result.PolicyVersion {
			return domainreview.ErrReviewSubjectStale
		}

		var video infravideo.VideoModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", caseModel.VideoID).Take(&video).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return domainvideo.ErrVideoNotFound
			}
			return err
		}
		if video.Status != domainvideo.StatusPendingReview {
			return domainreview.ErrReviewSubjectState
		}
		if video.ReviewVersion != caseModel.ReviewVersion {
			return domainreview.ErrReviewSubjectStale
		}
		if !domainmedia.IsPublicReadyStatus(video.MediaStatus) {
			return domainreview.ErrReviewSubjectNotReady
		}
		policy, err := loadPolicyVersion(tx, caseModel.PolicyVersion)
		if err != nil {
			return err
		}
		outcome, err := policy.Route(result.Signals)
		if err != nil {
			return err
		}
		receipt := ResultModel{
			CaseID: caseModel.ID, VideoID: caseModel.VideoID, ReviewVersion: caseModel.ReviewVersion,
			Provider: result.Provider, ResultID: result.ResultID, PayloadHash: result.PayloadHash,
			ModelVersion: result.ModelVersion, PolicyVersion: result.PolicyVersion,
			Outcome: outcome, CreatedAt: result.ReceivedAt,
		}
		if err := tx.Create(&receipt).Error; err != nil {
			return err
		}
		for _, signal := range result.Signals {
			evidenceJSON, err := json.Marshal(signal.EvidenceRefs)
			if err != nil {
				return err
			}
			signalModel := SignalModel{
				CaseID: caseModel.ID, ResultReceiptID: receipt.ID, Label: signal.Label,
				Confidence: signal.Confidence, EvidenceRefsJSON: string(evidenceJSON),
				Provider: result.Provider, ModelVersion: result.ModelVersion,
				PolicyVersion: result.PolicyVersion, CreatedAt: result.ReceivedAt,
			}
			if err := tx.Create(&signalModel).Error; err != nil {
				return err
			}
		}
		decision := DecisionModel{
			CaseID: caseModel.ID, ResultReceiptID: receipt.ID, Outcome: outcome,
			PolicyVersion: result.PolicyVersion, CreatedAt: result.ReceivedAt,
		}
		if err := tx.Create(&decision).Error; err != nil {
			return err
		}
		receipt.DecisionID = decision.ID
		if err := tx.Model(&receipt).Update("decision_id", decision.ID).Error; err != nil {
			return err
		}

		caseModel.UpdatedAt = result.ReceivedAt
		switch outcome {
		case domainreview.OutcomeReject:
			caseModel.Status = domainreview.CaseStatusRejected
			caseModel.ClosedAt = &result.ReceivedAt
		case domainreview.OutcomeApprove:
			caseModel.Status = domainreview.CaseStatusApproved
			caseModel.ClosedAt = &result.ReceivedAt
		case domainreview.OutcomeHuman:
			caseModel.Status = domainreview.CaseStatusPendingHuman
		default:
			return domainreview.ErrInvalidDecisionOutcome
		}
		if err := tx.Model(&caseModel).Updates(map[string]any{
			"status": caseModel.Status, "updated_at": caseModel.UpdatedAt, "closed_at": caseModel.ClosedAt,
		}).Error; err != nil {
			return err
		}

		if outcome == domainreview.OutcomeApprove || outcome == domainreview.OutcomeReject {
			current := &domainvideo.Video{Status: video.Status, PublishedAt: video.PublishedAt}
			transition := domainvideo.LifecycleApprove
			if outcome == domainreview.OutcomeReject {
				transition = domainvideo.LifecycleReject
			}
			if err := current.ApplyLifecycleTransition(transition, result.ReceivedAt); err != nil {
				return domainreview.ErrReviewSubjectState
			}
			if err := tx.Model(&video).Updates(map[string]any{
				"status": current.Status, "published_at": current.PublishedAt,
			}).Error; err != nil {
				return err
			}
			publicDelta, privateDelta := reviewContentWorkDeltas(video, current.Status)
			if err := infravideo.AdjustContentStat(tx, video.AuthorID, publicDelta, privateDelta, 0, 0); err != nil {
				return err
			}
		}
		processed = &domainreview.ProcessingResult{
			Case: restoreCase(caseModel),
			Decision: &domainreview.AutomatedDecision{
				ID: decision.ID, CaseID: decision.CaseID, ResultID: result.ResultID,
				Outcome: decision.Outcome, PolicyVersion: decision.PolicyVersion, CreatedAt: decision.CreatedAt,
			},
			ApplySideEffects: true,
			MediaAssetID:     positiveValue(video.MediaAssetID), CoverAssetID: positiveValue(video.CoverAssetID),
		}
		return nil
	})
	return processed, err
}

func (r *Repository) ListReviewableVideoIDsWithoutCase(ctx context.Context, limit int) ([]int64, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	var ids []int64
	err := r.db.WithContext(ctx).Table("video AS v").
		Select("v.id").
		Joins("LEFT JOIN review_case AS rc ON rc.video_id = v.id AND rc.review_version = v.review_version").
		Where("v.status = ? AND v.review_version > 0 AND v.media_status IN ? AND rc.id IS NULL",
			domainvideo.StatusPendingReview,
			[]string{domainmedia.MediaStatusLegacyReady, domainmedia.MediaStatusReady},
		).
		Order("v.id ASC").Limit(limit).Scan(&ids).Error
	return ids, err
}

func loadProcessingResult(tx *gorm.DB, receipt ResultModel, duplicate bool) (*domainreview.ProcessingResult, error) {
	var caseModel CaseModel
	if err := tx.Where("id = ?", receipt.CaseID).Take(&caseModel).Error; err != nil {
		return nil, err
	}
	var decision DecisionModel
	if err := tx.Where("id = ?", receipt.DecisionID).Take(&decision).Error; err != nil {
		return nil, err
	}
	var video infravideo.VideoModel
	if err := tx.Where("id = ?", receipt.VideoID).Take(&video).Error; err != nil {
		return nil, err
	}
	return &domainreview.ProcessingResult{
		Case: restoreCase(caseModel),
		Decision: &domainreview.AutomatedDecision{
			ID: decision.ID, CaseID: decision.CaseID, ResultID: receipt.ResultID,
			Outcome: decision.Outcome, PolicyVersion: decision.PolicyVersion, CreatedAt: decision.CreatedAt,
		},
		Duplicate:        duplicate,
		ApplySideEffects: video.ReviewVersion == caseModel.ReviewVersion,
		MediaAssetID:     positiveValue(video.MediaAssetID), CoverAssetID: positiveValue(video.CoverAssetID),
	}, nil
}

func loadActivePolicy(tx *gorm.DB) (*domainreview.Policy, error) {
	var model PolicyModel
	if err := tx.Where("enabled = ?", true).Order("version DESC").Take(&model).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domainreview.ErrReviewPolicyNotFound
		}
		return nil, err
	}
	return policyFromModel(model)
}

func loadPolicyVersion(tx *gorm.DB, version int) (*domainreview.Policy, error) {
	var model PolicyModel
	if err := tx.Where("version = ? AND enabled = ?", version, true).Take(&model).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domainreview.ErrReviewPolicyNotFound
		}
		return nil, err
	}
	return policyFromModel(model)
}

func policyFromModel(model PolicyModel) (*domainreview.Policy, error) {
	var config domainreview.PolicyConfiguration
	if err := json.Unmarshal([]byte(model.ConfigJSON), &config); err != nil {
		return nil, err
	}
	policy := domainreview.RestorePolicy(model.ID, model.Version, model.Enabled, config, model.CreatedAt, model.UpdatedAt)
	if policy == nil {
		return nil, domainreview.ErrInvalidPolicy
	}
	return policy, nil
}

func restoreCase(model CaseModel) *domainreview.ReviewCase {
	return domainreview.RestoreCase(
		model.ID, model.VideoID, model.ReviewVersion, model.Status, model.PolicyVersion,
		model.CreatedAt, model.UpdatedAt, model.ClosedAt,
	)
}

func reviewContentWorkDeltas(video infravideo.VideoModel, nextStatus int) (int, int) {
	oldPublic, oldPrivate := reviewContentWorkCounts(video.Status, video.Visibility, video.MediaStatus)
	newPublic, newPrivate := reviewContentWorkCounts(nextStatus, video.Visibility, video.MediaStatus)
	return newPublic - oldPublic, newPrivate - oldPrivate
}

func reviewContentWorkCounts(status int, visibility, mediaStatus string) (int, int) {
	if status == domainvideo.StatusDeleted {
		return 0, 0
	}
	if visibility == domainvideo.VisibilityPrivate {
		return 0, 1
	}
	if status == domainvideo.StatusPublished && domainmedia.IsPublicReadyStatus(mediaStatus) {
		return 1, 0
	}
	return 0, 0
}

func positiveValue(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}
