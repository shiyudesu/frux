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

type Repository struct {
	db                  *gorm.DB
	auditWriter         AuditWriter
	moderationJobConfig *domainreview.ModerationJobConfig
}

type AuditWriter interface {
	AppendInTransaction(ctx context.Context, tx *gorm.DB, fact *domainadminaudit.Fact) error
	RecordCommittedWrite(fact *domainadminaudit.Fact)
}

type Option func(*Repository)

func WithAuditWriter(writer AuditWriter) Option {
	return func(repository *Repository) { repository.auditWriter = writer }
}

func WithModerationJobConfig(config domainreview.ModerationJobConfig) Option {
	return func(repository *Repository) {
		if domainreview.ValidateModerationJobConfig(config) == nil {
			copyConfig := config
			repository.moderationJobConfig = &copyConfig
		}
	}
}

func New(db *gorm.DB, options ...Option) *Repository {
	repository := &Repository{db: db}
	for _, option := range options {
		if option != nil {
			option(repository)
		}
	}
	return repository
}

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
		if video.MediaAssetID == nil && strings.TrimSpace(video.MediaURL) == "" {
			return domainreview.ErrReviewSubjectNotReady
		}
		findErr := tx.Where("video_id = ? AND review_version = ?", video.ID, video.ReviewVersion).Take(&model).Error
		if findErr == nil {
			if err := r.ensureModerationJob(tx, model, time.Now().UTC()); err != nil {
				return err
			}
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
		if err := r.ensureModerationJob(tx, model, reviewCase.CreatedAt); err != nil {
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

func (r *Repository) ensureModerationJob(tx *gorm.DB, reviewCase CaseModel, now time.Time) error {
	if r == nil || r.moderationJobConfig == nil {
		return nil
	}
	job, err := domainreview.NewModerationJob(
		reviewCase.ID, reviewCase.VideoID, reviewCase.ReviewVersion,
		*r.moderationJobConfig, now,
	)
	if err != nil {
		return err
	}
	return tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "case_id"}, {Name: "review_version"}, {Name: "provider_config_version"},
		},
		DoNothing: true,
	}).Create(moderationJobModelFromDomain(job)).Error
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
			legacySource := existing.SourceKind == domainreview.MachineSourceLegacyUnknown ||
				(existing.SourceKind == domainreview.MachineSourceTestSeed &&
					strings.EqualFold(strings.TrimSpace(existing.Provider), "manual-seed"))
			legacyMatch := legacySource &&
				existing.PayloadHash == result.LegacyPayloadHash
			if existing.PayloadHash != result.PayloadHash && !legacyMatch {
				return domainreview.ErrResultIdentityConflict
			}
			var err error
			processed, err = loadProcessingResult(tx, existing, true)
			return err
		}
		if !errors.Is(findErr, gorm.ErrRecordNotFound) {
			return findErr
		}
		if result.ModerationJobID > 0 {
			var moderationJob ModerationJobModel
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where(`id = ? AND result_id = ? AND status = ? AND lease_owner = ?
					AND lease_until > clock_timestamp()`,
					result.ModerationJobID, result.ResultID,
					domainreview.ModerationJobLeased, result.ModerationLeaseOwner).
				Take(&moderationJob).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return domainreview.ErrModerationJobNotOwned
				}
				return err
			}
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
		if video.MediaAssetID == nil && strings.TrimSpace(video.MediaURL) == "" {
			return domainreview.ErrReviewSubjectNotReady
		}
		policy, err := loadPolicyVersion(tx, caseModel.PolicyVersion)
		if err != nil {
			return err
		}
		policyOutcome, priority, rejectionReason, err := policy.RouteWithPriorityAndReason(result.Signals)
		if err != nil {
			return err
		}
		outcome, priority, err := domainreview.RestrictAutomatedOutcome(
			result.RolloutMode, policyOutcome, priority,
		)
		if err != nil {
			return err
		}
		receipt := ResultModel{
			CaseID: caseModel.ID, VideoID: caseModel.VideoID, ReviewVersion: caseModel.ReviewVersion,
			Provider: result.Provider, ResultID: result.ResultID, PayloadHash: result.PayloadHash,
			ModelVersion: result.ModelVersion, SourceKind: result.SourceKind,
			GeneratedAt: result.GeneratedAt, RolloutMode: result.RolloutMode,
			PolicyVersion: result.PolicyVersion,
			Outcome:       outcome, CreatedAt: result.ReceivedAt,
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
				PolicyVersion: result.PolicyVersion, SourceKind: result.SourceKind,
				GeneratedAt: result.GeneratedAt, CreatedAt: result.ReceivedAt,
			}
			if err := tx.Create(&signalModel).Error; err != nil {
				return err
			}
		}
		decision := DecisionModel{
			CaseID: caseModel.ID, ResultReceiptID: receipt.ID, Outcome: outcome,
			PolicyVersion: result.PolicyVersion, RolloutMode: result.RolloutMode,
			CreatedAt: result.ReceivedAt,
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
			caseModel.Priority = priority
		default:
			return domainreview.ErrInvalidDecisionOutcome
		}
		if err := tx.Model(&caseModel).Updates(map[string]any{
			"status": caseModel.Status, "priority": caseModel.Priority,
			"updated_at": caseModel.UpdatedAt, "closed_at": caseModel.ClosedAt,
		}).Error; err != nil {
			return err
		}

		if outcome == domainreview.OutcomeApprove || outcome == domainreview.OutcomeReject {
			transition := domainvideo.LifecycleApprove
			if outcome == domainreview.OutcomeReject {
				transition = domainvideo.LifecycleReject
			}
			current, publicDelta, privateDelta, err := prepareReviewLifecycleTransition(
				video, transition, result.ReceivedAt,
			)
			if err != nil {
				return domainreview.ErrReviewSubjectState
			}
			if err := tx.Model(&video).Updates(map[string]any{
				"status": current.Status, "published_at": current.PublishedAt,
			}).Error; err != nil {
				return err
			}
			video.Status = current.Status
			video.PublishedAt = current.PublishedAt
			if outcome == domainreview.OutcomeReject {
				if err := infravideo.AppendMediaLifecycleTask(
					tx,
					fmt.Sprintf("review-machine-decision:%d:protect", decision.ID),
					video,
					domainmedia.LifecycleActionProtect,
					domainvideo.StatusRejected,
					"",
					result.ReceivedAt,
				); err != nil {
					return err
				}
			}
			if err := infravideo.AdjustContentStat(tx, video.AuthorID, publicDelta, privateDelta, 0); err != nil {
				return err
			}
			reasonCode := ""
			if outcome == domainreview.OutcomeReject {
				reasonCode = rejectionReason
				if !domainmessage.ValidReviewReasonCode(reasonCode) {
					reasonCode = domainreview.ReasonRejectOther
				}
			}
			notification := reviewLifecycleNotification(video, outcome, reasonCode, result.ReceivedAt)
			outbox := NotificationOutboxModel{
				EventID: notification.EventID, RecipientID: video.AuthorID, VideoID: video.ID,
				Outcome: outcome, ReviewVersion: notification.ReviewVersion,
				Stage: notification.Stage, Result: notification.Result,
				ReasonCode: notification.ReasonCode, OccurredAt: &notification.OccurredAt,
				State:       domainreview.NotificationStatePending,
				AvailableAt: result.ReceivedAt, CreatedAt: result.ReceivedAt, UpdatedAt: result.ReceivedAt,
			}
			if err := tx.Create(&outbox).Error; err != nil {
				return err
			}
			if notification.Stage == domainmessage.LifecycleStagePublished {
				if err := infravideo.AppendPublicationHandoff(
					tx, video, result.ReceivedAt, video.MediaAssetID == nil,
				); err != nil {
					return err
				}
			}
		}
		processed = &domainreview.ProcessingResult{
			Case: restoreCase(caseModel),
			Decision: &domainreview.AutomatedDecision{
				ID: decision.ID, CaseID: decision.CaseID, ResultID: result.ResultID,
				Outcome: decision.Outcome, PolicyVersion: decision.PolicyVersion,
				RolloutMode: decision.RolloutMode, CreatedAt: decision.CreatedAt,
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
		Where(`v.status = ? AND v.review_version > 0 AND
			(v.media_asset_id IS NOT NULL OR v.media_url <> '') AND rc.id IS NULL`,
			domainvideo.StatusPendingReview).
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
			Outcome: decision.Outcome, PolicyVersion: decision.PolicyVersion,
			RolloutMode: decision.RolloutMode, CreatedAt: decision.CreatedAt,
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
	return domainreview.RestoreHumanCase(
		model.ID, model.VideoID, model.ReviewVersion, model.Status, model.PolicyVersion,
		model.Priority, model.Version, model.AssignedReviewerID, model.LeaseTokenHash, model.LeaseExpiresAt,
		model.CreatedAt, model.UpdatedAt, model.ClosedAt,
	)
}

func prepareReviewLifecycleTransition(
	video infravideo.VideoModel,
	transition domainvideo.LifecycleTransition,
	at time.Time,
) (*domainvideo.Video, int, int, error) {
	next := &domainvideo.Video{Status: video.Status, PublishedAt: video.PublishedAt}
	if err := next.ApplyLifecycleTransition(transition, at); err != nil {
		return nil, 0, 0, err
	}
	publicDelta, privateDelta := infravideo.ContentWorkDeltas(
		video.Status, video.Visibility, video.MediaStatus,
		next.Status, video.Visibility, video.MediaStatus,
	)
	return next, publicDelta, privateDelta, nil
}

func positiveValue(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}
