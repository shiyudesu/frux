package interfaceshttprouter

import (
	"context"
	"errors"

	applicationmedia "github.com/shiyudesu/frux/internal/application/media"
	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
	domainvideo "github.com/shiyudesu/frux/internal/domain/video"
)

type mediaAdminVideoReader interface {
	FindAdminVideosByMediaAssetIDs(
		ctx context.Context,
		assetIDs []int64,
	) (map[int64]domainvideo.AdminMediaRef, error)
	FindByIDAnyStatus(ctx context.Context, videoID int64) (*domainvideo.Video, error)
}

type mediaAdminVideoCatalog struct {
	reader mediaAdminVideoReader
}

func (c mediaAdminVideoCatalog) FindAdminProcessingVideosByAssetIDs(
	ctx context.Context,
	assetIDs []int64,
) (map[int64]applicationmedia.AdminProcessingVideo, error) {
	videos, err := c.reader.FindAdminVideosByMediaAssetIDs(ctx, assetIDs)
	if err != nil {
		return nil, err
	}
	result := make(map[int64]applicationmedia.AdminProcessingVideo, len(videos))
	for assetID, video := range videos {
		result[assetID] = applicationmedia.AdminProcessingVideo{
			VideoID: video.VideoID, AssetID: assetID,
			AuthorID: video.AuthorID, Title: video.Title,
		}
	}
	return result, nil
}

func (c mediaAdminVideoCatalog) FindAdminProcessingVideo(
	ctx context.Context,
	videoID int64,
) (*applicationmedia.AdminProcessingVideo, error) {
	video, err := c.reader.FindByIDAnyStatus(ctx, videoID)
	if err != nil {
		if errors.Is(err, domainvideo.ErrVideoNotFound) {
			return nil, domainmedia.ErrInvalidProcessingAdminQuery
		}
		return nil, err
	}
	if video.MediaAssetID <= 0 {
		return nil, domainmedia.ErrInvalidProcessingAdminQuery
	}
	return &applicationmedia.AdminProcessingVideo{
		VideoID: video.ID, AssetID: video.MediaAssetID,
		AuthorID: video.AuthorID, Title: video.Title,
	}, nil
}
