package infravideo

import (
	applicationvideo "GCFeed/internal/application/video"
	domainmedia "GCFeed/internal/domain/media"
	domainvideo "GCFeed/internal/domain/video"
	infrapersistence "GCFeed/internal/infra/persistence"
	"context"
	"encoding/json"
	"errors"
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
	if filter.Visibility != "" {
		query = query.Where("v.visibility = ?", filter.Visibility)
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
		VideoID      int64
		MediaAssetID *int64
		CoverAssetID *int64
	}
	if err := r.db.WithContext(ctx).Model(&VideoModel{}).
		Select("id AS video_id, media_asset_id, cover_asset_id").
		Where("id IN ?", videoIDs).Scan(&models).Error; err != nil {
		return nil, err
	}
	result := make([]applicationvideo.MediaAssetRef, 0, len(models))
	for _, model := range models {
		ref := applicationvideo.MediaAssetRef{VideoID: model.VideoID}
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
				publicDelta, privateDelta := contentWorkDeltas(video.Status, video.Visibility, video.MediaStatus, newStatus, newVisibility, video.MediaStatus)
				if err := tx.Model(&VideoModel{}).Where("id = ?", video.ID).Updates(updates).Error; err != nil {
					return err
				}
				if err := AdjustContentStat(tx, userID, publicDelta, privateDelta, receivedLikeDelta, 0); err != nil {
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

func (r *Repository) CreateCollection(ctx context.Context, collection *domainvideo.Collection) (*domainvideo.Collection, bool, error) {
	var model CollectionModel
	created := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if collection.IdempotencyKey != "" {
			err := tx.Where("owner_id = ? AND idempotency_key = ?", collection.OwnerID, collection.IdempotencyKey).Take(&model).Error
			if err == nil {
				return nil
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
		}
		model = CollectionModel{
			OwnerID: collection.OwnerID, Title: collection.Title, Description: collection.Description,
			Visibility: collection.Visibility, Status: collection.Status,
			IdempotencyKey: idempotencyKeyPtr(collection.IdempotencyKey),
		}
		result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&model)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			if collection.IdempotencyKey == "" {
				return domainvideo.ErrDuplicateIdempotencyKey
			}
			return tx.Where("owner_id = ? AND idempotency_key = ?", collection.OwnerID, collection.IdempotencyKey).Take(&model).Error
		}
		created = true
		return AdjustContentStat(tx, collection.OwnerID, 0, 0, 0, 1)
	})
	if err != nil {
		return nil, false, err
	}
	return restoreCollection(model), created, nil
}

func (r *Repository) ListCollections(ctx context.Context, ownerID int64, publicOnly bool, cursor *domainvideo.CollectionCursor, limit int) ([]*domainvideo.Collection, error) {
	var models []CollectionModel
	query := r.db.WithContext(ctx).Where("owner_id = ? AND status = ?", ownerID, domainvideo.CollectionStatusActive)
	if publicOnly {
		query = query.Where("visibility = ?", domainvideo.VisibilityPublic)
	}
	if cursor != nil {
		query = query.Where("(updated_at < ? OR (updated_at = ? AND id < ?))", cursor.UpdatedAt, cursor.UpdatedAt, cursor.CollectionID)
	}
	if err := query.Order("updated_at DESC").Order("id DESC").Limit(limit).Find(&models).Error; err != nil {
		return nil, err
	}
	collections := make([]*domainvideo.Collection, 0, len(models))
	collectionIDs := make([]int64, 0, len(models))
	for _, model := range models {
		collections = append(collections, restoreCollection(model))
		collectionIDs = append(collectionIDs, model.ID)
	}
	previewLimit := 0
	if publicOnly {
		previewLimit = domainvideo.MaxPublicCollectionPreviewItems
	}
	itemsByCollection, memberCounts, err := r.listCollectionItemsBatch(ctx, collectionIDs, publicOnly, previewLimit)
	if err != nil {
		return nil, err
	}
	for _, collection := range collections {
		collection.Items = itemsByCollection[collection.ID]
		collection.MemberCount = memberCounts[collection.ID]
	}
	return collections, nil
}

func (r *Repository) GetCollection(ctx context.Context, collectionID int64) (*domainvideo.Collection, error) {
	var model CollectionModel
	if err := r.db.WithContext(ctx).Where("id = ?", collectionID).Take(&model).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domainvideo.ErrCollectionNotFound
		}
		return nil, err
	}
	return restoreCollection(model), nil
}

func (r *Repository) UpdateCollection(ctx context.Context, collection *domainvideo.Collection, update domainvideo.CollectionUpdate) error {
	columns := map[string]any{}
	if update.Title != nil {
		columns["title"] = collection.Title
	}
	if update.Description != nil {
		columns["description"] = collection.Description
	}
	if update.Visibility != nil {
		columns["visibility"] = collection.Visibility
	}
	if len(columns) == 0 {
		return nil
	}
	result := r.db.WithContext(ctx).Model(&CollectionModel{}).
		Where("id = ? AND owner_id = ? AND status = ?", collection.ID, collection.OwnerID, domainvideo.CollectionStatusActive).
		Updates(columns)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return domainvideo.ErrCollectionNotFound
	}
	return nil
}

func (r *Repository) DeleteCollection(ctx context.Context, collection *domainvideo.Collection) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&CollectionModel{}).
			Where("id = ? AND owner_id = ? AND status = ?", collection.ID, collection.OwnerID, domainvideo.CollectionStatusActive).
			Update("status", domainvideo.CollectionStatusDeleted)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return domainvideo.ErrCollectionNotFound
		}
		return AdjustContentStat(tx, collection.OwnerID, 0, 0, 0, -1)
	})
}

func (r *Repository) SetCollectionItem(ctx context.Context, ownerID, collectionID, videoID int64, active bool) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var collection CollectionModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND owner_id = ? AND status = ?", collectionID, ownerID, domainvideo.CollectionStatusActive).
			Take(&collection).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return domainvideo.ErrCollectionPermissionDenied
			}
			return err
		}
		if !active {
			result := tx.Where("collection_id = ? AND video_id = ?", collectionID, videoID).Delete(&CollectionItemModel{})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected == 0 {
				return nil
			}
			return touchCollection(tx, collectionID)
		}
		var video VideoModel
		if err := tx.Where("id = ? AND author_id = ? AND status <> ?", videoID, ownerID, domainvideo.StatusDeleted).Take(&video).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return domainvideo.ErrVideoPermissionDenied
			}
			return err
		}
		var maxPosition int
		if err := tx.Model(&CollectionItemModel{}).Where("collection_id = ?", collectionID).Select("COALESCE(MAX(position), 0)").Scan(&maxPosition).Error; err != nil {
			return err
		}
		item := CollectionItemModel{CollectionID: collectionID, VideoID: videoID, Position: maxPosition + 1}
		result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&item)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return nil
		}
		return touchCollection(tx, collectionID)
	})
}

func touchCollection(tx *gorm.DB, collectionID int64) error {
	return tx.Model(&CollectionModel{}).
		Where("id = ?", collectionID).
		UpdateColumn("updated_at", gorm.Expr("clock_timestamp()")).
		Error
}

func (r *Repository) ListCollectionItems(ctx context.Context, collectionID int64, publicOnly bool) ([]*domainvideo.CollectionItem, error) {
	itemsByCollection, _, err := r.listCollectionItemsBatch(ctx, []int64{collectionID}, publicOnly, 0)
	if err != nil {
		return nil, err
	}
	return itemsByCollection[collectionID], nil
}

type collectionMembershipRow struct {
	CollectionID int64
	VideoID      int64
	Position     int
	CreatedAt    time.Time
	MemberCount  int
	PreviewRank  int
}

func (r *Repository) listCollectionItemsBatch(ctx context.Context, collectionIDs []int64, publicOnly bool, previewLimit int) (map[int64][]*domainvideo.CollectionItem, map[int64]int, error) {
	itemsByCollection := make(map[int64][]*domainvideo.CollectionItem, len(collectionIDs))
	memberCounts := make(map[int64]int, len(collectionIDs))
	if len(collectionIDs) == 0 {
		return itemsByCollection, memberCounts, nil
	}

	membershipQuery := r.db.WithContext(ctx).
		Table("video_collection_item AS i").
		Select(`
			i.collection_id,
			i.video_id,
			i.position,
			i.created_at,
			COUNT(*) OVER (PARTITION BY i.collection_id) AS member_count,
			ROW_NUMBER() OVER (
				PARTITION BY i.collection_id
				ORDER BY i.position ASC, i.video_id ASC
			) AS preview_rank
		`).
		Joins("JOIN video AS member_video ON member_video.id = i.video_id").
		Where("i.collection_id IN ?", collectionIDs)
	if publicOnly {
		membershipQuery = membershipQuery.Where(
			"member_video.status = ? AND member_video.visibility = ? AND member_video.media_status IN ?",
			domainvideo.StatusPublished,
			domainvideo.VisibilityPublic,
			[]string{domainmedia.MediaStatusLegacyReady, domainmedia.MediaStatusReady},
		)
	} else {
		membershipQuery = membershipQuery.Where("member_video.status <> ?", domainvideo.StatusDeleted)
	}

	var memberships []collectionMembershipRow
	rankedQuery := r.db.WithContext(ctx).Table("(?) AS ranked_items", membershipQuery)
	if previewLimit > 0 {
		rankedQuery = rankedQuery.Where("preview_rank <= ?", previewLimit)
	}
	if err := rankedQuery.
		Order("collection_id ASC").
		Order("position ASC").
		Order("video_id ASC").
		Scan(&memberships).Error; err != nil {
		return nil, nil, err
	}

	videoIDSet := make(map[int64]struct{}, len(memberships))
	videoIDs := make([]int64, 0, len(memberships))
	for _, membership := range memberships {
		memberCounts[membership.CollectionID] = membership.MemberCount
		if _, exists := videoIDSet[membership.VideoID]; exists {
			continue
		}
		videoIDSet[membership.VideoID] = struct{}{}
		videoIDs = append(videoIDs, membership.VideoID)
	}

	var models []videoWithStatModel
	query := r.db.WithContext(ctx).Table("video AS v").Select(videoWithStatSelect()).
		Joins("LEFT JOIN video_stat AS vs ON vs.video_id = v.id").Where("v.id IN ?", videoIDs)
	if publicOnly {
		query = query.Where("v.status = ? AND v.visibility = ? AND v.media_status IN ?", domainvideo.StatusPublished, domainvideo.VisibilityPublic, []string{domainmedia.MediaStatusLegacyReady, domainmedia.MediaStatusReady})
	} else {
		query = query.Where("v.status <> ?", domainvideo.StatusDeleted)
	}
	if len(videoIDs) > 0 {
		if err := query.Scan(&models).Error; err != nil {
			return nil, nil, err
		}
	}
	videos := make(map[int64]*domainvideo.Video, len(models))
	videoList := make([]*domainvideo.Video, 0, len(models))
	for _, model := range models {
		video := restoreVideo(model)
		videos[video.ID] = video
		videoList = append(videoList, video)
	}
	if err := r.hydrateMediaDelivery(ctx, videoList); err != nil {
		return nil, nil, err
	}
	for _, membership := range memberships {
		video := videos[membership.VideoID]
		if video == nil {
			continue
		}
		itemsByCollection[membership.CollectionID] = append(itemsByCollection[membership.CollectionID], &domainvideo.CollectionItem{
			CollectionID: membership.CollectionID, VideoID: membership.VideoID, Position: membership.Position,
			CreatedAt: membership.CreatedAt, Video: video,
		})
	}
	return itemsByCollection, memberCounts, nil
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

func restoreCollection(model CollectionModel) *domainvideo.Collection {
	return domainvideo.RestoreCollection(
		model.ID, model.OwnerID, model.Title, model.Description, model.Visibility,
		model.Status, idempotencyKeyValue(model.IdempotencyKey), model.CreatedAt, model.UpdatedAt,
	)
}

func likePattern(value string) string {
	value = strings.TrimSpace(value)
	value = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(value)
	return "%" + value + "%"
}
