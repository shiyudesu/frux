package migration

import (
	"errors"
	"time"

	infraaccount "GCFeed/internal/infra/persistence/account"
	infraembedding "GCFeed/internal/infra/persistence/embedding"
	infraexposure "GCFeed/internal/infra/persistence/exposure"
	infrafeed "GCFeed/internal/infra/persistence/feed"
	infrainteraction "GCFeed/internal/infra/persistence/interaction"
	infralibrary "GCFeed/internal/infra/persistence/library"
	inframedia "GCFeed/internal/infra/persistence/media"
	inframessage "GCFeed/internal/infra/persistence/message"
	infraplayback "GCFeed/internal/infra/persistence/playback"
	infrarecommendation "GCFeed/internal/infra/persistence/recommendation"
	infrarelation "GCFeed/internal/infra/persistence/relation"
	infravideo "GCFeed/internal/infra/persistence/video"

	"gorm.io/gorm"
)

const advisoryLockKey int64 = 0x474346656564
const viewHistoryBackfillKey = "20260724_video_view_history_backfill_v1"
const viewEventEnvelopeBackfillKey = "20260725_view_event_envelope_backfill_v1"
const viewHistoryAggregateRepairKey = "20260725_video_view_history_aggregate_repair_v2"
const recommendationBehaviorBackfillKey = "20260725_recommendation_behavior_backfill_v1"
const recommendationFeedbackProfileOutboxBackfillKey = "20260727_recommendation_feedback_profile_outbox_v1"
const relationProfileOutboxBackfillKey = "20260727_relation_profile_outbox_v1"
const interactionActionProfileOutboxBackfillKey = "20260727_interaction_action_profile_outbox_v1"
const interactionActionOutcomeOutboxBackfillKey = "20260727_interaction_action_outcome_outbox_v1"
const threadedCommentBackfillKey = "20260803_threaded_comment_backfill_v1"

type markerModel struct {
	Key       string    `gorm:"column:key;size:128;primaryKey"`
	AppliedAt time.Time `gorm:"column:applied_at;not null;autoCreateTime"`
}

func (markerModel) TableName() string {
	return "app_migration"
}

func AutoMigrate(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", advisoryLockKey).Error; err != nil {
			return err
		}
		if err := prepareExposureSchema(tx); err != nil {
			return err
		}
		if err := prepareRecommendationRequestLogSchema(tx); err != nil {
			return err
		}
		if err := tx.AutoMigrate(
			&infraaccount.UserModel{},
			&infraaccount.ProfileSettingModel{},
			&infraembedding.VideoEmbeddingModel{},
			&infravideo.VideoModel{},
			&inframedia.AssetModel{},
			&inframedia.VariantModel{},
			&inframedia.ProcessingProfileModel{},
			&inframedia.ProcessingJobModel{},
			&inframedia.UploadSessionModel{},
			&inframedia.CleanupTaskModel{},
			&infravideo.LocalAssetModel{},
			&infravideo.VideoStatModel{},
			&infravideo.UserContentStatModel{},
			&infravideo.CollectionModel{},
			&infravideo.CollectionItemModel{},
			&infravideo.BatchOperationModel{},
			&infrafeed.InboxModel{},
			&infraexposure.ViewEventModel{},
			&infraexposure.ExposureModel{},
			&infraexposure.ViewHistoryModel{},
			&infraexposure.ViewHistoryDeletionModel{},
			&infraexposure.ViewEventOutboxModel{},
			&infrarecommendation.BehaviorEventModel{},
			&infrarecommendation.PolicyModel{},
			&infrarecommendation.UserInterestProfileModel{},
			&infrarecommendation.AppliedProfileEventModel{},
			&infrarecommendation.FeedbackModel{},
			&infrarecommendation.FeedbackProfileOutboxModel{},
			&infrarecommendation.RequestLogModel{},
			&infrarecommendation.ServedCandidateEvidenceModel{},
			&infrarecommendation.OutcomeModel{},
			&infrainteraction.ActionModel{},
			&infrainteraction.ActionEventModel{},
			&infrainteraction.ActionIdempotencyReceiptModel{},
			&infrainteraction.CommentModel{},
			&infrainteraction.CommentLikeModel{},
			&infrainteraction.CommentLikeIdempotencyReceiptModel{},
			&infrainteraction.CommentNotificationOutboxModel{},
			&inframessage.MessageModel{},
			&infraplayback.ConfigModel{},
			&infraplayback.QoSLogModel{},
			&infraplayback.TelemetryBatchModel{},
			&infraplayback.TelemetryEventModel{},
			&infrarelation.FollowModel{},
			&infrarelation.RelationStatModel{},
			&infrarelation.FollowProfileOutboxModel{},
			&infralibrary.WatchLaterModel{},
			&markerModel{},
		); err != nil {
			return err
		}
		if err := infravideo.EnsureStats(tx); err != nil {
			return err
		}
		if err := tx.Model(&infravideo.VideoModel{}).Where("visibility IS NULL OR visibility = ''").Update("visibility", "public").Error; err != nil {
			return err
		}
		if err := infravideo.BackfillLocalAssets(tx); err != nil {
			return err
		}
		if err := backfillLegacyMediaStatus(tx); err != nil {
			return err
		}
		if err := infraaccount.EnsureProfileSettings(tx); err != nil {
			return err
		}
		if err := infrainteraction.BackfillActionEventOrder(tx); err != nil {
			return err
		}
		if err := runOnce(tx, threadedCommentBackfillKey, infrainteraction.BackfillThreadedComments); err != nil {
			return err
		}
		if err := infrainteraction.ReconcileCommentCounters(tx); err != nil {
			return err
		}
		if err := infravideo.ReconcileContentStats(tx); err != nil {
			return err
		}
		if err := runOnce(tx, viewEventEnvelopeBackfillKey, infraexposure.EnsureViewEventEnvelope); err != nil {
			return err
		}
		if err := runOnce(tx, viewHistoryBackfillKey, infraexposure.EnsureViewHistory); err != nil {
			return err
		}
		if err := runOnce(tx, viewHistoryAggregateRepairKey, infraexposure.RepairExistingViewHistoryAggregates); err != nil {
			return err
		}
		if err := runOnce(tx, recommendationBehaviorBackfillKey, infrarecommendation.EnsureBehaviorEvents); err != nil {
			return err
		}
		if err := runOnce(tx, recommendationFeedbackProfileOutboxBackfillKey, infrarecommendation.EnsureFeedbackProfileOutbox); err != nil {
			return err
		}
		if err := runOnce(tx, relationProfileOutboxBackfillKey, infrarelation.EnsureFollowProfileOutbox); err != nil {
			return err
		}
		if err := runOnce(tx, interactionActionProfileOutboxBackfillKey, infrainteraction.EnsureActionProfileProjectionOutbox); err != nil {
			return err
		}
		if err := runOnce(tx, interactionActionOutcomeOutboxBackfillKey, infrainteraction.EnsureRecommendationActionOutcomeOutbox); err != nil {
			return err
		}
		if err := infrarecommendation.EnsureInitialPolicies(tx); err != nil {
			return err
		}
		return infrafeed.EnsureTimelineIndex(tx)
	})
}

func backfillLegacyMediaStatus(tx *gorm.DB) error {
	return tx.Model(&infravideo.VideoModel{}).
		Where("media_status IS NULL OR media_status = ''").
		Update("media_status", "legacy_ready").
		Error
}

func prepareExposureSchema(tx *gorm.DB) error {
	return tx.Exec(`
		DO $$
		BEGIN
			IF to_regclass('video_view_history') IS NOT NULL
				AND EXISTS (
					SELECT 1
					FROM information_schema.columns
					WHERE table_schema = current_schema()
						AND table_name = 'video_view_history'
						AND column_name = 'last_event_id'
						AND data_type NOT IN ('character varying', 'text')
				)
			THEN
				ALTER TABLE video_view_history ALTER COLUMN last_event_id DROP DEFAULT;
				ALTER TABLE video_view_history
					ALTER COLUMN last_event_id TYPE varchar(128)
					USING CASE WHEN last_event_id > 0 THEN 'legacy-' || last_event_id::text ELSE '' END;
				ALTER TABLE video_view_history ALTER COLUMN last_event_id SET DEFAULT '';
			END IF;
		END $$;
	`).Error
}

func prepareRecommendationRequestLogSchema(tx *gorm.DB) error {
	return tx.Exec(`
		DO $$
		BEGIN
			IF to_regclass('recommendation_request_log') IS NOT NULL THEN
				IF EXISTS (
					SELECT 1
					FROM pg_constraint
					WHERE conrelid = 'recommendation_request_log'::regclass
						AND conname = 'uk_recommendation_request_log_request'
				) THEN
					ALTER TABLE recommendation_request_log
						DROP CONSTRAINT uk_recommendation_request_log_request;
				END IF;
				IF to_regclass('uk_recommendation_request_log_request') IS NOT NULL THEN
					DROP INDEX uk_recommendation_request_log_request;
				END IF;
			END IF;
		END $$;
	`).Error
}

func runOnce(tx *gorm.DB, key string, apply func(*gorm.DB) error) error {
	var marker markerModel
	err := tx.Where("key = ?", key).Take(&marker).Error
	if err == nil {
		return nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if err := apply(tx); err != nil {
		return err
	}
	return tx.Create(&markerModel{Key: key}).Error
}
