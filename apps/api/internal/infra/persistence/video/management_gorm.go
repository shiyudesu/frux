package infravideo

import (
	"context"
	"encoding/json"
	"errors"
	applicationvideo "github.com/shiyudesu/frux/internal/application/video"
	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
	domainvideo "github.com/shiyudesu/frux/internal/domain/video"
	infrapersistence "github.com/shiyudesu/frux/internal/infra/persistence"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (r *Repository) QueryCreatorVideos(ctx context.Context, filter domainvideo.CreatorVideoFilter) ([]*domainvideo.Video, error) {
	var models []videoWithStatModel
	query := r.db.WithContext(ctx).
		Table("video AS v").
		Select(videoWithStatSelect()).
		Joins("LEFT JOIN video_stat AS vs ON vs.video_id = v.id").
		Where("v.author_id = ? AND v.status <> ?", filter.AuthorID, domainvideo.StatusDeleted)
	if filter.VideoID > 0 {
		query = query.Where("v.id = ?", filter.VideoID)
	}
	if filter.Visibility != "" {
		query = query.Where("v.visibility = ?", filter.Visibility)
	}
	if len(filter.Statuses) > 0 {
		query = query.Where("v.status IN ?", filter.Statuses)
	}
	if filter.Query != "" {
		query = query.Where("v.title ILIKE ? ESCAPE '\\' OR v.description ILIKE ? ESCAPE '\\'", likePattern(filter.Query), likePattern(filter.Query))
	}
	if filter.CreatedFrom != nil {
		query = query.Where("v.created_at >= ?", *filter.CreatedFrom)
	}
	if filter.CreatedTo != nil {
		query = query.Where("v.created_at <= ?", *filter.CreatedTo)
	}
	if filter.Cursor != nil {
		query = query.Where("(v.created_at < ? OR (v.created_at = ? AND v.id < ?))", filter.Cursor.CreatedAt, filter.Cursor.CreatedAt, filter.Cursor.VideoID)
	}
	if err := query.Order("v.created_at DESC").Order("v.id DESC").Limit(filter.Limit).Scan(&models).Error; err != nil {
		return nil, err
	}
	videos := make([]*domainvideo.Video, 0, len(models))
	for _, model := range models {
		videos = append(videos, restoreVideo(model))
	}
	if err := r.hydrateMediaDelivery(ctx, videos); err != nil {
		return nil, err
	}
	return videos, nil
}

func (r *Repository) ListMediaAssetRefs(ctx context.Context, videoIDs []int64) ([]applicationvideo.MediaAssetRef, error) {
	if len(videoIDs) == 0 {
		return []applicationvideo.MediaAssetRef{}, nil
	}
	var models []struct {
		VideoID        int64
		MediaAssetID   *int64
		CoverAssetID   *int64
		Status         int
		Visibility     string
		MediaStatus    string
		MediaErrorCode string
	}
	if err := r.db.WithContext(ctx).Model(&VideoModel{}).
		Select("id AS video_id, media_asset_id, cover_asset_id, status, visibility, media_status, media_error_code").
		Where("id IN ?", videoIDs).Scan(&models).Error; err != nil {
		return nil, err
	}
	result := make([]applicationvideo.MediaAssetRef, 0, len(models))
	for _, model := range models {
		ref := applicationvideo.MediaAssetRef{
			VideoID: model.VideoID, Status: model.Status, Visibility: model.Visibility,
			MediaStatus: model.MediaStatus, MediaErrorCode: model.MediaErrorCode,
		}
		if model.MediaAssetID != nil {
			ref.MediaAssetID = *model.MediaAssetID
		}
		if model.CoverAssetID != nil {
			ref.CoverAssetID = *model.CoverAssetID
		}
		result = append(result, ref)
	}
	return result, nil
}

func (r *Repository) ApplyBatch(ctx context.Context, userID int64, action string, videoIDs []int64, idempotencyKey, fingerprint string) (*domainvideo.BatchOperation, bool, error) {
	var operation BatchOperationModel
	replayed := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := time.Now().UTC()
		findErr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("user_id = ? AND idempotency_key = ?", userID, idempotencyKey).
			Take(&operation).Error
		if findErr == nil {
			if operation.Fingerprint != fingerprint {
				return domainvideo.ErrBatchIdempotencyConflict
			}
			replayed = true
			return nil
		}
		if !errors.Is(findErr, gorm.ErrRecordNotFound) {
			return findErr
		}

		var videos []VideoModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id IN ?", videoIDs).Find(&videos).Error; err != nil {
			return err
		}
		if len(videos) != len(videoIDs) {
			return domainvideo.ErrVideoNotFound
		}
		for _, video := range videos {
			if video.AuthorID != userID {
				return domainvideo.ErrVideoPermissionDenied
			}
			if action != domainvideo.BatchActionDelete && (video.Status == domainvideo.StatusDeleted || video.Status == domainvideo.StatusOffline) {
				return domainvideo.ErrVideoStateNotAllowed
			}
		}

		for _, video := range videos {
			newStatus, newVisibility := video.Status, video.Visibility
			receivedLikeDelta := 0
			updates := map[string]any{}
			switch action {
			case domainvideo.BatchActionMakePublic:
				if video.Visibility != domainvideo.VisibilityPublic {
					newVisibility = domainvideo.VisibilityPublic
					updates["visibility"] = newVisibility
				}
			case domainvideo.BatchActionMakePrivate:
				if video.Visibility != domainvideo.VisibilityPrivate {
					newVisibility = domainvideo.VisibilityPrivate
					updates["visibility"] = newVisibility
				}
			case domainvideo.BatchActionDelete:
				if video.Status != domainvideo.StatusDeleted {
					newStatus = domainvideo.StatusDeleted
					updates["status"] = newStatus
					var stat VideoStatModel
					if err := tx.Where("video_id = ?", video.ID).Take(&stat).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
						return err
					}
					receivedLikeDelta = -stat.LikeCount
				}
			default:
				return domainvideo.ErrInvalidBatchAction
			}
			if len(updates) > 0 {
				publicDelta, privateDelta := ContentWorkDeltas(video.Status, video.Visibility, video.MediaStatus, newStatus, newVisibility, video.MediaStatus)
				if err := tx.Model(&VideoModel{}).Where("id = ?", video.ID).Updates(updates).Error; err != nil {
					return err
				}
				if err := AdjustContentStat(tx, userID, publicDelta, privateDelta, receivedLikeDelta); err != nil {
					return err
				}
				if action == domainvideo.BatchActionMakePublic &&
					video.Status == domainvideo.StatusPublished &&
					domainmedia.IsPublicReadyStatus(video.MediaStatus) {
					video.Visibility = newVisibility
					if err := AppendPublicationHandoff(
						tx, video, time.Now().UTC(), video.MediaAssetID == nil,
					); err != nil {
						return err
					}
				}
			}
			switch action {
			case domainvideo.BatchActionMakePrivate:
				if err := AppendMediaLifecycleTask(
					tx,
					"creator-batch:"+idempotencyKey+":private",
					video,
					domainmedia.LifecycleActionProtect,
					0,
					domainvideo.VisibilityPrivate,
					now,
				); err != nil {
					return err
				}
			case domainvideo.BatchActionDelete:
				if err := AppendMediaLifecycleTask(
					tx,
					"creator-batch:"+idempotencyKey+":delete",
					video,
					domainmedia.LifecycleActionDelete,
					domainvideo.StatusDeleted,
					"",
					now,
				); err != nil {
					return err
				}
			}
		}

		videoIDsJSON, _ := json.Marshal(videoIDs)
		resultJSON, _ := json.Marshal(map[string]any{"action": action, "video_ids": videoIDs})
		operation = BatchOperationModel{
			UserID: userID, IdempotencyKey: idempotencyKey, Fingerprint: fingerprint,
			Action: action, VideoIDsJSON: string(videoIDsJSON), ResultJSON: string(resultJSON),
		}
		if err := tx.Create(&operation).Error; err != nil {
			if infrapersistence.IsDuplicatedKeyError(err) {
				return domainvideo.ErrBatchIdempotencyConflict
			}
			return err
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, domainvideo.ErrBatchIdempotencyConflict) {
			var existing BatchOperationModel
			if loadErr := r.db.WithContext(ctx).Where("user_id = ? AND idempotency_key = ?", userID, idempotencyKey).Take(&existing).Error; loadErr == nil && existing.Fingerprint == fingerprint {
				return batchOperationFromModel(existing), true, nil
			}
		}
		return nil, false, err
	}
	return batchOperationFromModel(operation), replayed, nil
}

func batchOperationFromModel(operation BatchOperationModel) *domainvideo.BatchOperation {
	var ids []int64
	_ = json.Unmarshal([]byte(operation.VideoIDsJSON), &ids)
	return &domainvideo.BatchOperation{
		UserID: operation.UserID, Key: operation.IdempotencyKey, Fingerprint: operation.Fingerprint,
		Action: operation.Action, VideoIDs: ids, ResultJSON: operation.ResultJSON, CreatedAt: operation.CreatedAt,
	}
}

func (r *Repository) BatchGetReadable(ctx context.Context, viewerID int64, videoIDs []int64, publicOnly bool) (map[int64]*domainvideo.Video, error) {
	result := map[int64]*domainvideo.Video{}
	if len(videoIDs) == 0 {
		return result, nil
	}
	var models []videoWithStatModel
	query := r.db.WithContext(ctx).
		Table("video AS v").Select(videoWithStatSelect()).
		Joins("LEFT JOIN video_stat AS vs ON vs.video_id = v.id").
		Where("v.id IN ? AND v.status = ?", videoIDs, domainvideo.StatusPublished)
	if publicOnly {
		query = query.Where("v.visibility = ? AND v.media_status IN ?", domainvideo.VisibilityPublic, []string{domainmedia.MediaStatusLegacyReady, domainmedia.MediaStatusReady})
	} else {
		query = query.Where("v.visibility = ? OR v.author_id = ?", domainvideo.VisibilityPublic, viewerID)
	}
	if err := query.Scan(&models).Error; err != nil {
		return nil, err
	}
	for _, model := range models {
		video := restoreVideo(model)
		result[video.ID] = video
	}
	videoList := make([]*domainvideo.Video, 0, len(result))
	for _, video := range result {
		videoList = append(videoList, video)
	}
	if err := r.hydrateMediaDelivery(ctx, videoList); err != nil {
		return nil, err
	}
	if publicOnly {
		for videoID, video := range result {
			if video.MediaAssetID > 0 && video.MediaURL == "" {
				delete(result, videoID)
			}
		}
	}
	return result, nil
}

func (r *Repository) ListAssetReferences(ctx context.Context, assetURL string) ([]domainvideo.AssetReference, error) {
	var references []domainvideo.AssetReference
	err := r.db.WithContext(ctx).
		Model(&VideoModel{}).
		Select("author_id, status, visibility").
		Where("media_url = ? OR cover_url = ?", assetURL, assetURL).
		Scan(&references).
		Error
	return references, err
}

func (r *Repository) CreateLocalAsset(ctx context.Context, asset *domainvideo.LocalAsset) error {
	if asset == nil || asset.OwnerID <= 0 {
		return domainvideo.ErrInvalidLocalAsset
	}
	model := LocalAssetModel{
		AssetURL: asset.AssetURL,
		OwnerID:  asset.OwnerID,
		Kind:     asset.Kind,
	}
	if err := r.db.WithContext(ctx).Create(&model).Error; err != nil {
		if !infrapersistence.IsDuplicatedKeyError(err) {
			return err
		}
		var existing LocalAssetModel
		if loadErr := r.db.WithContext(ctx).Where("asset_url = ?", asset.AssetURL).Take(&existing).Error; loadErr != nil {
			return loadErr
		}
		if existing.OwnerID != asset.OwnerID || existing.Kind != asset.Kind {
			return domainvideo.ErrLocalAssetOwnershipConflict
		}
		model = existing
	}
	asset.CreatedAt = model.CreatedAt
	return nil
}

func (r *Repository) FindLocalAsset(ctx context.Context, assetURL string) (*domainvideo.LocalAsset, error) {
	var model LocalAssetModel
	if err := r.db.WithContext(ctx).Where("asset_url = ?", assetURL).Take(&model).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domainvideo.ErrLocalAssetNotFound
		}
		return nil, err
	}
	return &domainvideo.LocalAsset{
		AssetURL:  model.AssetURL,
		OwnerID:   model.OwnerID,
		Kind:      model.Kind,
		CreatedAt: model.CreatedAt,
	}, nil
}

func likePattern(value string) string {
	value = strings.TrimSpace(value)
	value = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(value)
	return "%" + value + "%"
}
