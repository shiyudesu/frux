package applicationvideo

import (
	"context"

	domainmedia "GCFeed/internal/domain/media"
	domainvideo "GCFeed/internal/domain/video"
)

type MediaProjectionRepository interface {
	ListByMediaAssetID(ctx context.Context, mediaAssetID int64) ([]*domainvideo.Video, error)
	UpdateMediaProjection(ctx context.Context, video *domainvideo.Video) error
}

type MediaPublicationService struct {
	repo             MediaProjectionRepository
	delivery         MediaDeliveryResolver
	publisher        PublishedEventPublisher
	cacheInvalidator VideoCacheInvalidator
}

func NewMediaPublicationService(repo MediaProjectionRepository, delivery MediaDeliveryResolver, publisher PublishedEventPublisher, cacheInvalidator VideoCacheInvalidator) *MediaPublicationService {
	return &MediaPublicationService{
		repo: repo, delivery: delivery, publisher: publisher, cacheInvalidator: cacheInvalidator,
	}
}

func (s *MediaPublicationService) MediaReady(ctx context.Context, assetID int64) error {
	if s == nil || s.repo == nil || s.delivery == nil || assetID <= 0 {
		return nil
	}
	videos, err := s.repo.ListByMediaAssetID(ctx, assetID)
	if err != nil {
		return err
	}
	for _, video := range videos {
		if video == nil {
			continue
		}
		if video.Visibility != domainvideo.VisibilityPublic {
			video.MediaURL = ""
			video.CoverURL = ""
			video.PlaybackSources = nil
			video.MediaStatus = domainmedia.MediaStatusReady
			video.MediaErrorCode = ""
			if err := s.repo.UpdateMediaProjection(ctx, video); err != nil {
				return err
			}
			if s.cacheInvalidator != nil {
				_ = s.cacheInvalidator.InvalidateVideo(ctx, video.ID)
			}
			continue
		}
		delivery, err := s.delivery.ResolveVideo(ctx, video.ID, video.MediaAssetID, video.CoverAssetID)
		if err != nil {
			return err
		}
		wasDiscoverable := domainmedia.IsPublicReadyStatus(video.MediaStatus) && video.MediaURL != ""
		video.MediaURL = delivery.MediaURL
		video.CoverURL = delivery.CoverURL
		video.PlaybackSources = delivery.PlaybackSources
		video.MediaStatus = domainmedia.MediaStatusReady
		video.MediaErrorCode = ""
		if err := s.repo.UpdateMediaProjection(ctx, video); err != nil {
			return err
		}
		if s.cacheInvalidator != nil {
			_ = s.cacheInvalidator.InvalidateVideo(ctx, video.ID)
		}
		if !wasDiscoverable && video.IsPubliclyReadable() && s.publisher != nil {
			event := NewPublishedEvent(video)
			if event != nil {
				if err := s.publisher.PublishVideoPublished(ctx, event); err != nil {
					video.MediaStatus = domainmedia.MediaStatusProcessing
					video.MediaErrorCode = "publication_event_failed"
					if rollbackErr := s.repo.UpdateMediaProjection(ctx, video); rollbackErr != nil {
						return rollbackErr
					}
					if s.cacheInvalidator != nil {
						_ = s.cacheInvalidator.InvalidateVideo(ctx, video.ID)
					}
					return err
				}
			}
		}
	}
	return nil
}

func (s *MediaPublicationService) MediaFailed(ctx context.Context, assetID int64, errorCode string) error {
	if s == nil || s.repo == nil || assetID <= 0 {
		return nil
	}
	videos, err := s.repo.ListByMediaAssetID(ctx, assetID)
	if err != nil {
		return err
	}
	for _, video := range videos {
		if video == nil {
			continue
		}
		video.MediaStatus = domainmedia.MediaStatusFailed
		video.MediaErrorCode = errorCode
		video.PlaybackSources = nil
		if err := s.repo.UpdateMediaProjection(ctx, video); err != nil {
			return err
		}
		if s.cacheInvalidator != nil {
			_ = s.cacheInvalidator.InvalidateVideo(ctx, video.ID)
		}
	}
	return nil
}
