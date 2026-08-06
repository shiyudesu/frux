package inframedia

import (
	"context"
	"errors"
	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
	infrapersistence "github.com/shiyudesu/frux/internal/infra/persistence"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Repository struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) CreateAsset(ctx context.Context, asset *domainmedia.MediaAsset) error {
	model := assetModelFromDomain(asset)
	if err := r.db.WithContext(ctx).Create(&model).Error; err != nil {
		if infrapersistence.IsDuplicatedKeyError(err) {
			return domainmedia.ErrUploadSessionConflict
		}
		return err
	}
	applyAssetModel(asset, model)
	return nil
}

func (r *Repository) FindAssetByID(ctx context.Context, assetID int64) (*domainmedia.MediaAsset, error) {
	var model AssetModel
	if err := r.db.WithContext(ctx).Where("id = ?", assetID).Take(&model).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domainmedia.ErrMediaAssetNotFound
		}
		return nil, err
	}
	return assetFromModel(model), nil
}

func (r *Repository) FindAssetsByIDs(ctx context.Context, assetIDs []int64) (map[int64]*domainmedia.MediaAsset, error) {
	result := make(map[int64]*domainmedia.MediaAsset, len(assetIDs))
	if len(assetIDs) == 0 {
		return result, nil
	}
	var models []AssetModel
	if err := r.db.WithContext(ctx).Where("id IN ?", assetIDs).Find(&models).Error; err != nil {
		return nil, err
	}
	for _, model := range models {
		result[model.ID] = assetFromModel(model)
	}
	return result, nil
}

func (r *Repository) FindAssetByObjectKey(ctx context.Context, backend, objectKey string) (*domainmedia.MediaAsset, error) {
	var model AssetModel
	if err := r.db.WithContext(ctx).Where("storage_backend = ? AND object_key = ?", backend, objectKey).Take(&model).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domainmedia.ErrMediaAssetNotFound
		}
		return nil, err
	}
	return assetFromModel(model), nil
}

func (r *Repository) UpdateAsset(ctx context.Context, asset *domainmedia.MediaAsset) error {
	result := r.db.WithContext(ctx).Model(&AssetModel{}).Where("id = ?", asset.ID).Updates(map[string]any{
		"width": asset.Width, "height": asset.Height, "duration_ms": asset.DurationMS,
		"video_codec": asset.VideoCodec, "audio_codec": asset.AudioCodec,
		"state": asset.State, "error_code": asset.ErrorCode, "updated_at": time.Now().UTC(),
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return domainmedia.ErrMediaAssetNotFound
	}
	return nil
}

func (r *Repository) UpsertVariants(ctx context.Context, variants []*domainmedia.MediaVariant) error {
	if len(variants) == 0 {
		return nil
	}
	models := make([]VariantModel, 0, len(variants))
	for _, variant := range variants {
		if variant == nil {
			continue
		}
		models = append(models, variantModelFromDomain(variant))
	}
	if len(models) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "object_key"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"asset_id", "video_id", "profile_version", "source_type", "format", "codec", "audio_codec",
			"width", "height", "bitrate", "quality", "role", "sort_order", "state",
			"checksum_sha256", "size_bytes", "public", "updated_at",
		}),
	}).Create(&models).Error
}

func (r *Repository) ListReadyVariants(ctx context.Context, assetID int64) ([]*domainmedia.MediaVariant, error) {
	var models []VariantModel
	if err := r.db.WithContext(ctx).
		Where("asset_id = ? AND state = ?", assetID, domainmedia.VariantStateReady).
		Order("sort_order ASC").Order("bitrate ASC").Order("id ASC").
		Find(&models).Error; err != nil {
		return nil, err
	}
	return variantsFromModels(models), nil
}

func (r *Repository) ListReadyVariantsByAssetIDs(ctx context.Context, assetIDs []int64) (map[int64][]*domainmedia.MediaVariant, error) {
	result := make(map[int64][]*domainmedia.MediaVariant, len(assetIDs))
	if len(assetIDs) == 0 {
		return result, nil
	}
	var models []VariantModel
	if err := r.db.WithContext(ctx).
		Where("asset_id IN ? AND state = ?", assetIDs, domainmedia.VariantStateReady).
		Order("asset_id ASC").Order("sort_order ASC").Order("bitrate ASC").Order("id ASC").
		Find(&models).Error; err != nil {
		return nil, err
	}
	for _, model := range models {
		result[model.AssetID] = append(result[model.AssetID], variantFromModel(model))
	}
	return result, nil
}

func (r *Repository) ListReadyVariantsByVideoIDs(ctx context.Context, videoIDs []int64) (map[int64][]*domainmedia.MediaVariant, error) {
	result := make(map[int64][]*domainmedia.MediaVariant, len(videoIDs))
	if len(videoIDs) == 0 {
		return result, nil
	}
	var models []VariantModel
	if err := r.db.WithContext(ctx).
		Where("video_id IN ? AND state = ?", videoIDs, domainmedia.VariantStateReady).
		Order("video_id ASC").Order("sort_order ASC").Order("bitrate ASC").Order("id ASC").
		Find(&models).Error; err != nil {
		return nil, err
	}
	for _, model := range models {
		if model.VideoID == nil {
			continue
		}
		result[*model.VideoID] = append(result[*model.VideoID], variantFromModel(model))
	}
	return result, nil
}

func (r *Repository) UpdateVariantPromotion(
	ctx context.Context,
	variantID int64,
	expectedObjectKey string,
	expectedPublic bool,
	objectKey string,
	public bool,
) (bool, error) {
	result := r.db.WithContext(ctx).Model(&VariantModel{}).
		Where("id = ? AND object_key = ? AND public = ?", variantID, expectedObjectKey, expectedPublic).
		Updates(map[string]any{"object_key": objectKey, "public": public, "updated_at": time.Now().UTC()})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected == 1, nil
}

func (r *Repository) UpsertProcessingProfile(ctx context.Context, profile *domainmedia.ProcessingProfile) error {
	model := ProcessingProfileModel{
		Version: profile.Version, Name: profile.Name, ConfigJSON: profile.ConfigJSON, Active: profile.Active,
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "version"}},
		DoUpdates: clause.AssignmentColumns([]string{"name", "config_json", "active", "updated_at"}),
	}).Create(&model).Error
}

func (r *Repository) FindProcessingProfile(ctx context.Context, version string) (*domainmedia.ProcessingProfile, error) {
	var model ProcessingProfileModel
	if err := r.db.WithContext(ctx).Where("version = ?", version).Take(&model).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domainmedia.ErrProcessingProfileNotFound
		}
		return nil, err
	}
	return &domainmedia.ProcessingProfile{
		Version: model.Version, Name: model.Name, ConfigJSON: model.ConfigJSON, Active: model.Active,
		CreatedAt: model.CreatedAt, UpdatedAt: model.UpdatedAt,
	}, nil
}

func (r *Repository) CreateOrGetProcessingJob(ctx context.Context, job *domainmedia.MediaProcessingJob) (*domainmedia.MediaProcessingJob, bool, error) {
	model := processingJobModelFromDomain(job)
	result := r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&model)
	if result.Error != nil {
		return nil, false, result.Error
	}
	created := result.RowsAffected > 0
	if !created {
		if err := r.db.WithContext(ctx).
			Where("asset_id = ? AND profile_version = ?", job.AssetID, job.ProfileVersion).
			Take(&model).Error; err != nil {
			return nil, false, err
		}
	}
	return processingJobFromModel(model), created, nil
}

func (r *Repository) LeaseProcessingJob(ctx context.Context, assetID int64, profileVersion, owner string, now time.Time, leaseUntil time.Time) (*domainmedia.MediaProcessingJob, error) {
	var model ProcessingJobModel
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("asset_id = ? AND profile_version = ? AND state IN ? AND next_attempt_at <= ? AND (lease_until IS NULL OR lease_until < ?)",
				assetID, profileVersion, []string{domainmedia.JobStatePending, domainmedia.JobStateRetryable}, now, now).
			Take(&model).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return domainmedia.ErrProcessingJobNotFound
			}
			return err
		}
		if err := tx.Model(&model).Updates(map[string]any{
			"state": domainmedia.JobStateProcessing, "lease_owner": owner, "lease_until": leaseUntil,
			"attempts": gorm.Expr("attempts + 1"), "updated_at": now,
		}).Error; err != nil {
			return err
		}
		model.State = domainmedia.JobStateProcessing
		model.LeaseOwner = owner
		model.LeaseUntil = &leaseUntil
		model.Attempts++
		return nil
	})
	if err != nil {
		return nil, err
	}
	return processingJobFromModel(model), nil
}

func (r *Repository) LeaseProcessingJobs(ctx context.Context, owner string, now time.Time, leaseUntil time.Time, limit int) ([]*domainmedia.MediaProcessingJob, error) {
	if limit <= 0 {
		return []*domainmedia.MediaProcessingJob{}, nil
	}
	var models []ProcessingJobModel
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("state IN ? AND next_attempt_at <= ? AND (lease_until IS NULL OR lease_until < ?)",
				[]string{domainmedia.JobStatePending, domainmedia.JobStateRetryable}, now, now).
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
		if err := tx.Model(&ProcessingJobModel{}).Where("id IN ?", ids).Updates(map[string]any{
			"state": domainmedia.JobStateProcessing, "lease_owner": owner, "lease_until": leaseUntil,
			"attempts": gorm.Expr("attempts + 1"), "updated_at": now,
		}).Error; err != nil {
			return err
		}
		return tx.Where("id IN ?", ids).Order("id ASC").Find(&models).Error
	})
	if err != nil {
		return nil, err
	}
	result := make([]*domainmedia.MediaProcessingJob, 0, len(models))
	for _, model := range models {
		result = append(result, processingJobFromModel(model))
	}
	return result, nil
}

func (r *Repository) UpdateProcessingJob(ctx context.Context, job *domainmedia.MediaProcessingJob) error {
	result := r.db.WithContext(ctx).Model(&ProcessingJobModel{}).Where("id = ?", job.ID).Updates(map[string]any{
		"state": job.State, "attempts": job.Attempts, "max_attempts": job.MaxAttempts,
		"error_code": job.ErrorCode, "error_message": job.ErrorMessage,
		"lease_owner": job.LeaseOwner, "lease_until": job.LeaseUntil,
		"next_attempt_at": job.NextAttemptAt, "completed_at": job.CompletedAt,
		"updated_at": time.Now().UTC(),
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return domainmedia.ErrProcessingJobNotFound
	}
	return nil
}

func (r *Repository) UpdateProcessingJobOwned(ctx context.Context, job *domainmedia.MediaProcessingJob, leaseOwner string) error {
	result := r.db.WithContext(ctx).Model(&ProcessingJobModel{}).
		Where("id = ? AND state = ? AND lease_owner = ?", job.ID, domainmedia.JobStateProcessing, leaseOwner).
		Updates(map[string]any{
			"state": job.State, "attempts": job.Attempts, "max_attempts": job.MaxAttempts,
			"error_code": job.ErrorCode, "error_message": job.ErrorMessage,
			"lease_owner": job.LeaseOwner, "lease_until": job.LeaseUntil,
			"next_attempt_at": job.NextAttemptAt, "completed_at": job.CompletedAt,
			"updated_at": time.Now().UTC(),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return domainmedia.ErrProcessingJobNotFound
	}
	return nil
}

func (r *Repository) ExtendProcessingLease(ctx context.Context, jobID int64, leaseOwner string, leaseUntil time.Time) error {
	result := r.db.WithContext(ctx).Model(&ProcessingJobModel{}).
		Where("id = ? AND state = ? AND lease_owner = ?", jobID, domainmedia.JobStateProcessing, leaseOwner).
		Updates(map[string]any{"lease_until": leaseUntil, "updated_at": time.Now().UTC()})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return domainmedia.ErrProcessingJobNotFound
	}
	return nil
}

func (r *Repository) ReleaseExpiredProcessingLeases(ctx context.Context, now time.Time) (int64, error) {
	result := r.db.WithContext(ctx).Model(&ProcessingJobModel{}).
		Where("state = ? AND lease_until < ?", domainmedia.JobStateProcessing, now).
		Updates(map[string]any{
			"state": domainmedia.JobStateRetryable, "lease_owner": "", "lease_until": nil,
			"next_attempt_at": now, "updated_at": now,
		})
	return result.RowsAffected, result.Error
}

func (r *Repository) ListAssetsForReconciliation(ctx context.Context, limit int) ([]*domainmedia.MediaAsset, error) {
	if limit <= 0 {
		return []*domainmedia.MediaAsset{}, nil
	}
	var models []AssetModel
	if err := r.db.WithContext(ctx).
		Where("state IN ?", []string{
			domainmedia.AssetStateUploaded, domainmedia.AssetStateProcessing, domainmedia.AssetStateReady,
		}).
		Order("last_reconciled_at ASC NULLS FIRST").Order("id ASC").Limit(limit).Find(&models).Error; err != nil {
		return nil, err
	}

	result := make([]*domainmedia.MediaAsset, 0, len(models))
	for _, model := range models {
		result = append(result, assetFromModel(model))
	}
	return result, nil
}

func (r *Repository) MarkAssetReconciled(ctx context.Context, assetID int64, reconciledAt time.Time) error {
	return r.db.WithContext(ctx).Model(&AssetModel{}).Where("id = ?", assetID).
		Update("last_reconciled_at", reconciledAt).Error
}

func (r *Repository) FindProcessingJobByAsset(ctx context.Context, assetID int64) (*domainmedia.MediaProcessingJob, error) {
	var model ProcessingJobModel
	if err := r.db.WithContext(ctx).Where("asset_id = ?", assetID).Order("id DESC").Take(&model).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domainmedia.ErrProcessingJobNotFound
		}
		return nil, err
	}
	return processingJobFromModel(model), nil
}

func (r *Repository) ResetProcessingJob(ctx context.Context, assetID int64, profileVersion string, now time.Time) error {
	result := r.db.WithContext(ctx).Model(&ProcessingJobModel{}).
		Where("asset_id = ? AND profile_version = ?", assetID, profileVersion).
		Updates(map[string]any{
			"state": domainmedia.JobStateRetryable, "lease_owner": "", "lease_until": nil,
			"next_attempt_at": now, "completed_at": nil, "error_code": "", "error_message": "",
			"updated_at": now,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return domainmedia.ErrProcessingJobNotFound
	}
	return nil
}

func (r *Repository) ListKnownObjectKeys(ctx context.Context, prefix string) (map[string]struct{}, error) {
	result := map[string]struct{}{}
	pattern := strings.TrimSpace(prefix) + "%"
	var assetKeys []string
	if err := r.db.WithContext(ctx).Model(&AssetModel{}).Where("object_key LIKE ?", pattern).Pluck("object_key", &assetKeys).Error; err != nil {
		return nil, err
	}
	var variantKeys []string
	if err := r.db.WithContext(ctx).Model(&VariantModel{}).Where("object_key LIKE ?", pattern).Pluck("object_key", &variantKeys).Error; err != nil {
		return nil, err
	}
	for _, key := range append(assetKeys, variantKeys...) {
		result[key] = struct{}{}
	}
	return result, nil
}

func (r *Repository) CreateUploadSession(ctx context.Context, session *domainmedia.UploadSession) (*domainmedia.UploadSession, bool, error) {
	model := uploadSessionModelFromDomain(session)
	result := r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&model)
	if result.Error != nil {
		return nil, false, result.Error
	}
	created := result.RowsAffected > 0
	if !created {
		query := r.db.WithContext(ctx)
		if session.IdempotencyKey != "" {
			query = query.Where("owner_id = ? AND idempotency_key = ?", session.OwnerID, session.IdempotencyKey)
		} else {
			query = query.Where("id = ?", session.ID)
		}
		if err := query.Take(&model).Error; err != nil {
			return nil, false, err
		}
		if model.RequestFingerprint != session.RequestFingerprint {
			return nil, false, domainmedia.ErrUploadSessionConflict
		}
	}
	return uploadSessionFromModel(model), created, nil
}

func (r *Repository) FindUploadSession(ctx context.Context, sessionID string) (*domainmedia.UploadSession, error) {
	var model UploadSessionModel
	if err := r.db.WithContext(ctx).Where("id = ?", sessionID).Take(&model).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domainmedia.ErrUploadSessionNotFound
		}
		return nil, err
	}
	return uploadSessionFromModel(model), nil
}

func (r *Repository) RenewExpiredUploadSession(ctx context.Context, expiredSessionID string, replacement *domainmedia.UploadSession) (*domainmedia.UploadSession, error) {
	var model UploadSessionModel
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var expired UploadSessionModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", expiredSessionID).Take(&expired).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return domainmedia.ErrUploadSessionNotFound
			}
			return err
		}
		if expired.State != domainmedia.UploadSessionStateExpired &&
			!(expired.State == domainmedia.UploadSessionStatePending && !expired.ExpiresAt.After(time.Now().UTC())) {
			return domainmedia.ErrUploadSessionConflict
		}
		if err := tx.Delete(&expired).Error; err != nil {
			return err
		}
		model = uploadSessionModelFromDomain(replacement)
		return tx.Create(&model).Error
	})
	if err != nil {
		return nil, err
	}
	return uploadSessionFromModel(model), nil
}

func (r *Repository) CompleteUploadSession(ctx context.Context, sessionID string, asset *domainmedia.MediaAsset, completedAt time.Time) (*domainmedia.UploadSession, *domainmedia.MediaAsset, bool, error) {
	var sessionModel UploadSessionModel
	var assetModel AssetModel
	replayed := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", sessionID).Take(&sessionModel).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return domainmedia.ErrUploadSessionNotFound
			}
			return err
		}
		if sessionModel.State == domainmedia.UploadSessionStateCompleted && sessionModel.CompletedAssetID != nil {
			replayed = true
			return tx.Where("id = ?", *sessionModel.CompletedAssetID).Take(&assetModel).Error
		}
		if sessionModel.State != domainmedia.UploadSessionStatePending {
			return domainmedia.ErrUploadSessionConflict
		}
		assetModel = assetModelFromDomain(asset)
		if err := tx.Create(&assetModel).Error; err != nil {
			if !infrapersistence.IsDuplicatedKeyError(err) {
				return err
			}
			if err := tx.Where("storage_backend = ? AND object_key = ?", asset.StorageBackend, asset.ObjectKey).Take(&assetModel).Error; err != nil {
				return err
			}
			if assetModel.OwnerID != asset.OwnerID || assetModel.ChecksumSHA256 != asset.ChecksumSHA256 || assetModel.SizeBytes != asset.SizeBytes {
				return domainmedia.ErrUploadSessionConflict
			}
		}
		if err := tx.Model(&sessionModel).Updates(map[string]any{
			"state":              domainmedia.UploadSessionStateCompleted,
			"completed_asset_id": assetModel.ID,
			"completed_at":       completedAt,
			"updated_at":         completedAt,
		}).Error; err != nil {
			return err
		}
		sessionModel.State = domainmedia.UploadSessionStateCompleted
		sessionModel.CompletedAssetID = &assetModel.ID
		sessionModel.CompletedAt = &completedAt
		return nil
	})
	if err != nil {
		return nil, nil, false, err
	}
	assetDomain := assetFromModel(assetModel)
	return uploadSessionFromModel(sessionModel), assetDomain, replayed, nil
}

func (r *Repository) ExpireUploadSessions(ctx context.Context, now time.Time, limit int) ([]*domainmedia.UploadSession, error) {
	var models []UploadSessionModel
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("state = ? AND expires_at <= ?", domainmedia.UploadSessionStatePending, now).
			Order("expires_at ASC").Limit(limit).Find(&models).Error; err != nil {
			return err
		}
		if len(models) == 0 {
			return nil
		}
		ids := make([]string, 0, len(models))
		for _, model := range models {
			ids = append(ids, model.ID)
		}
		return tx.Model(&UploadSessionModel{}).Where("id IN ?", ids).
			Updates(map[string]any{"state": domainmedia.UploadSessionStateExpired, "updated_at": now}).Error
	})
	if err != nil {
		return nil, err
	}
	result := make([]*domainmedia.UploadSession, 0, len(models))
	for _, model := range models {
		model.State = domainmedia.UploadSessionStateExpired
		result = append(result, uploadSessionFromModel(model))
	}
	return result, nil
}

func (r *Repository) CreateCleanupTasks(ctx context.Context, tasks []*domainmedia.CleanupTask) error {
	if len(tasks) == 0 {
		return nil
	}
	models := make([]CleanupTaskModel, 0, len(tasks))
	for _, task := range tasks {
		if task != nil {
			models = append(models, cleanupTaskModelFromDomain(task))
		}
	}
	if len(models) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "storage_backend"}, {Name: "object_key"}},
		DoUpdates: clause.Assignments(map[string]any{
			"state": gorm.Expr(
				"CASE WHEN media_cleanup_task.state IN (?, ?) THEN ? ELSE media_cleanup_task.state END",
				domainmedia.CleanupStateCompleted, domainmedia.CleanupStateFailed, domainmedia.CleanupStatePending,
			),
			"attempts": gorm.Expr(
				"CASE WHEN media_cleanup_task.state IN (?, ?) THEN 0 ELSE media_cleanup_task.attempts END",
				domainmedia.CleanupStateCompleted, domainmedia.CleanupStateFailed,
			),
			"not_before": gorm.Expr(
				"CASE WHEN media_cleanup_task.state IN (?, ?) THEN EXCLUDED.not_before ELSE media_cleanup_task.not_before END",
				domainmedia.CleanupStateCompleted, domainmedia.CleanupStateFailed,
			),
			"error_message": gorm.Expr(
				"CASE WHEN media_cleanup_task.state IN (?, ?) THEN '' ELSE media_cleanup_task.error_message END",
				domainmedia.CleanupStateCompleted, domainmedia.CleanupStateFailed,
			),
			"completed_at": gorm.Expr(
				"CASE WHEN media_cleanup_task.state IN (?, ?) THEN NULL ELSE media_cleanup_task.completed_at END",
				domainmedia.CleanupStateCompleted, domainmedia.CleanupStateFailed,
			),
			"updated_at": time.Now().UTC(),
		}),
	}).Create(&models).Error
}

func (r *Repository) LeaseCleanupTasks(ctx context.Context, owner string, now time.Time, leaseUntil time.Time, limit int) ([]*domainmedia.CleanupTask, error) {
	if limit <= 0 {
		return []*domainmedia.CleanupTask{}, nil
	}
	var models []CleanupTaskModel
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("state = ? AND not_before <= ? AND (lease_until IS NULL OR lease_until < ?)", domainmedia.CleanupStatePending, now, now).
			Order("not_before ASC").Order("id ASC").Limit(limit).Find(&models).Error; err != nil {
			return err
		}
		if len(models) == 0 {
			return nil
		}
		ids := make([]int64, 0, len(models))
		for _, model := range models {
			ids = append(ids, model.ID)
		}
		if err := tx.Model(&CleanupTaskModel{}).Where("id IN ?", ids).Updates(map[string]any{
			"state": domainmedia.CleanupStateProcessing, "lease_owner": owner, "lease_until": leaseUntil,
			"attempts": gorm.Expr("attempts + 1"), "updated_at": now,
		}).Error; err != nil {
			return err
		}
		return tx.Where("id IN ?", ids).Order("id ASC").Find(&models).Error
	})
	if err != nil {
		return nil, err
	}
	result := make([]*domainmedia.CleanupTask, 0, len(models))
	for _, model := range models {
		result = append(result, cleanupTaskFromModel(model))
	}
	return result, nil
}

func (r *Repository) UpdateCleanupTask(ctx context.Context, task *domainmedia.CleanupTask) error {
	result := r.db.WithContext(ctx).Model(&CleanupTaskModel{}).Where("id = ?", task.ID).Updates(map[string]any{
		"state": task.State, "attempts": task.Attempts, "max_attempts": task.MaxAttempts,
		"error_message": task.ErrorMessage, "not_before": task.NotBefore,
		"lease_owner": task.LeaseOwner, "lease_until": task.LeaseUntil,
		"completed_at": task.CompletedAt, "updated_at": time.Now().UTC(),
	})
	if result.Error != nil {
		return result.Error
	}

	if result.RowsAffected == 0 {
		return domainmedia.ErrCleanupTaskNotFound
	}
	return nil
}

func (r *Repository) ListIncompletePublicCleanupTasks(
	ctx context.Context,
	assetIDs []int64,
) ([]*domainmedia.CleanupTask, error) {
	if len(assetIDs) == 0 {
		return []*domainmedia.CleanupTask{}, nil
	}
	var models []CleanupTaskModel
	if err := r.db.WithContext(ctx).
		Where(
			"asset_id IN ? AND object_key LIKE ? AND state <> ?",
			assetIDs,
			"media/%",
			domainmedia.CleanupStateCompleted,
		).
		Order("id ASC").
		Find(&models).Error; err != nil {
		return nil, err
	}
	tasks := make([]*domainmedia.CleanupTask, 0, len(models))
	for _, model := range models {
		tasks = append(tasks, cleanupTaskFromModel(model))
	}
	return tasks, nil
}

func (r *Repository) ReleaseExpiredCleanupLeases(ctx context.Context, now time.Time) (int64, error) {
	result := r.db.WithContext(ctx).Model(&CleanupTaskModel{}).
		Where("state = ? AND lease_until < ?", domainmedia.CleanupStateProcessing, now).
		Updates(map[string]any{
			"state": domainmedia.CleanupStatePending, "lease_owner": "", "lease_until": nil,
			"not_before": now, "updated_at": now,
		})
	return result.RowsAffected, result.Error
}

func assetModelFromDomain(asset *domainmedia.MediaAsset) AssetModel {
	return AssetModel{
		ID: asset.ID, OwnerID: asset.OwnerID, Kind: asset.Kind, StorageBackend: asset.StorageBackend,
		ObjectKey: asset.ObjectKey, ContentType: asset.ContentType, SizeBytes: asset.SizeBytes,
		ChecksumSHA256: asset.ChecksumSHA256, Width: asset.Width, Height: asset.Height,
		DurationMS: asset.DurationMS, VideoCodec: asset.VideoCodec, AudioCodec: asset.AudioCodec,
		State: asset.State, ErrorCode: asset.ErrorCode,
	}
}

func applyAssetModel(asset *domainmedia.MediaAsset, model AssetModel) {
	asset.ID = model.ID
	asset.CreatedAt = model.CreatedAt
	asset.UpdatedAt = model.UpdatedAt
}

func assetFromModel(model AssetModel) *domainmedia.MediaAsset {
	return &domainmedia.MediaAsset{
		ID: model.ID, OwnerID: model.OwnerID, Kind: model.Kind, StorageBackend: model.StorageBackend,
		ObjectKey: model.ObjectKey, ContentType: model.ContentType, SizeBytes: model.SizeBytes,
		ChecksumSHA256: model.ChecksumSHA256, Width: model.Width, Height: model.Height,
		DurationMS: model.DurationMS, VideoCodec: model.VideoCodec, AudioCodec: model.AudioCodec,
		State: model.State, ErrorCode: model.ErrorCode, CreatedAt: model.CreatedAt, UpdatedAt: model.UpdatedAt,
		LastReconciledAt: model.LastReconciledAt,
	}
}

func variantModelFromDomain(variant *domainmedia.MediaVariant) VariantModel {
	var videoID *int64
	if variant.VideoID > 0 {
		value := variant.VideoID
		videoID = &value
	}
	return VariantModel{
		ID: variant.ID, AssetID: variant.AssetID, VideoID: videoID, ProfileVersion: variant.ProfileVersion,
		SourceType: variant.SourceType, Format: variant.Format, Codec: variant.Codec, AudioCodec: variant.AudioCodec,
		Width: variant.Width, Height: variant.Height, Bitrate: variant.Bitrate, Quality: variant.Quality,
		ObjectKey: variant.ObjectKey, Role: variant.Role, SortOrder: variant.SortOrder, State: variant.State,
		ChecksumSHA256: variant.ChecksumSHA256, SizeBytes: variant.SizeBytes, Public: variant.Public,
	}
}

func variantFromModel(model VariantModel) *domainmedia.MediaVariant {
	videoID := int64(0)
	if model.VideoID != nil {
		videoID = *model.VideoID
	}
	return &domainmedia.MediaVariant{
		ID: model.ID, AssetID: model.AssetID, VideoID: videoID, ProfileVersion: model.ProfileVersion,
		SourceType: model.SourceType, Format: model.Format, Codec: model.Codec, AudioCodec: model.AudioCodec,
		Width: model.Width, Height: model.Height, Bitrate: model.Bitrate, Quality: model.Quality,
		ObjectKey: model.ObjectKey, Role: model.Role, SortOrder: model.SortOrder, State: model.State,
		ChecksumSHA256: model.ChecksumSHA256, SizeBytes: model.SizeBytes, Public: model.Public,
		CreatedAt: model.CreatedAt, UpdatedAt: model.UpdatedAt,
	}
}

func variantsFromModels(models []VariantModel) []*domainmedia.MediaVariant {
	result := make([]*domainmedia.MediaVariant, 0, len(models))
	for _, model := range models {
		result = append(result, variantFromModel(model))
	}
	return result
}

func processingJobModelFromDomain(job *domainmedia.MediaProcessingJob) ProcessingJobModel {
	return ProcessingJobModel{
		ID: job.ID, AssetID: job.AssetID, ProfileVersion: job.ProfileVersion, State: job.State,
		Attempts: job.Attempts, MaxAttempts: job.MaxAttempts, ErrorCode: job.ErrorCode,
		ErrorMessage: job.ErrorMessage, LeaseOwner: job.LeaseOwner, LeaseUntil: job.LeaseUntil,
		NextAttemptAt: job.NextAttemptAt, CompletedAt: job.CompletedAt,
	}
}

func processingJobFromModel(model ProcessingJobModel) *domainmedia.MediaProcessingJob {
	return &domainmedia.MediaProcessingJob{
		ID: model.ID, AssetID: model.AssetID, ProfileVersion: model.ProfileVersion, State: model.State,
		Attempts: model.Attempts, MaxAttempts: model.MaxAttempts, ErrorCode: model.ErrorCode,
		ErrorMessage: model.ErrorMessage, LeaseOwner: model.LeaseOwner, LeaseUntil: model.LeaseUntil,
		NextAttemptAt: model.NextAttemptAt, CompletedAt: model.CompletedAt,
		CreatedAt: model.CreatedAt, UpdatedAt: model.UpdatedAt,
	}
}

func uploadSessionModelFromDomain(session *domainmedia.UploadSession) UploadSessionModel {
	var completedAssetID *int64
	if session.CompletedAssetID > 0 {
		value := session.CompletedAssetID
		completedAssetID = &value
	}
	return UploadSessionModel{
		ID: session.ID, OwnerID: session.OwnerID, Kind: session.Kind, StorageBackend: session.StorageBackend,
		ObjectKey: session.ObjectKey, ContentType: session.ContentType, SizeBytes: session.SizeBytes,
		ChecksumSHA256: session.ChecksumSHA256, State: session.State,
		IdempotencyKey: nullableString(session.IdempotencyKey), RequestFingerprint: session.RequestFingerprint,
		ExpiresAt: session.ExpiresAt, CompletedAssetID: completedAssetID, CompletedAt: session.CompletedAt,
	}
}

func uploadSessionFromModel(model UploadSessionModel) *domainmedia.UploadSession {
	completedAssetID := int64(0)
	if model.CompletedAssetID != nil {
		completedAssetID = *model.CompletedAssetID
	}
	return &domainmedia.UploadSession{
		ID: model.ID, OwnerID: model.OwnerID, Kind: model.Kind, StorageBackend: model.StorageBackend,
		ObjectKey: model.ObjectKey, ContentType: model.ContentType, SizeBytes: model.SizeBytes,
		ChecksumSHA256: model.ChecksumSHA256, State: model.State,
		IdempotencyKey: stringValue(model.IdempotencyKey), RequestFingerprint: model.RequestFingerprint,
		ExpiresAt: model.ExpiresAt, CompletedAssetID: completedAssetID, CompletedAt: model.CompletedAt,
		CreatedAt: model.CreatedAt, UpdatedAt: model.UpdatedAt,
	}
}

func cleanupTaskModelFromDomain(task *domainmedia.CleanupTask) CleanupTaskModel {
	return CleanupTaskModel{
		ID: task.ID, AssetID: task.AssetID, StorageBackend: task.StorageBackend, ObjectKey: task.ObjectKey,
		State: task.State, Attempts: task.Attempts, MaxAttempts: task.MaxAttempts,
		ErrorMessage: task.ErrorMessage, NotBefore: task.NotBefore,
		LeaseOwner: task.LeaseOwner, LeaseUntil: task.LeaseUntil, CompletedAt: task.CompletedAt,
	}
}

func cleanupTaskFromModel(model CleanupTaskModel) *domainmedia.CleanupTask {
	return &domainmedia.CleanupTask{
		ID: model.ID, AssetID: model.AssetID, StorageBackend: model.StorageBackend, ObjectKey: model.ObjectKey,
		State: model.State, Attempts: model.Attempts, MaxAttempts: model.MaxAttempts,
		ErrorMessage: model.ErrorMessage, NotBefore: model.NotBefore,
		LeaseOwner: model.LeaseOwner, LeaseUntil: model.LeaseUntil, CompletedAt: model.CompletedAt,
		CreatedAt: model.CreatedAt, UpdatedAt: model.UpdatedAt,
	}
}

func nullableString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
