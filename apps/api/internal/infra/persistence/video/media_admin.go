package infravideo

import (
	"context"

	domainvideo "github.com/shiyudesu/frux/internal/domain/video"
)

func (r *Repository) FindAdminVideosByMediaAssetIDs(
	ctx context.Context,
	assetIDs []int64,
) (map[int64]domainvideo.AdminMediaRef, error) {
	result := make(map[int64]domainvideo.AdminMediaRef, len(assetIDs))
	if len(assetIDs) == 0 {
		return result, nil
	}
	var models []VideoModel
	if err := r.db.WithContext(ctx).
		Where("media_asset_id IN ?", assetIDs).
		Find(&models).Error; err != nil {
		return nil, err
	}
	for _, model := range models {
		if model.MediaAssetID == nil || *model.MediaAssetID <= 0 {
			continue
		}
		result[*model.MediaAssetID] = domainvideo.AdminMediaRef{
			VideoID: model.ID, AssetID: *model.MediaAssetID,
			AuthorID: model.AuthorID, Title: model.Title,
		}
	}
	return result, nil
}
