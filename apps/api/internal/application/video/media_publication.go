package applicationvideo

import (
	"context"
	"errors"
	"time"

	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
	domainmessage "github.com/shiyudesu/frux/internal/domain/message"
	domainvideo "github.com/shiyudesu/frux/internal/domain/video"
)

type MediaProjectionRepository interface {
	ListByMediaAssetID(ctx context.Context, mediaAssetID int64) ([]*domainvideo.Video, error)
	UpdateMediaProjection(ctx context.Context, video *domainvideo.Video) (bool, error)
	LifecyclePublicationTracked(ctx context.Context, eventID string) (bool, error)
	LifecyclePublicationReady(ctx context.Context, eventID string) (bool, error)
	MarkLifecyclePublicationReady(ctx context.Context, eventID string, readyAt time.Time) error
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
		if video.IsPubliclyReadable() && video.MediaURL != "" && video.MediaErrorCode == "" {
			public, err := s.delivery.HasPublicVideo(ctx, video.ID, video.MediaAssetID, video.CoverAssetID)
			if err != nil {
				return err
			}
			if public {
				eventID := domainmessage.PublicationEventID(video.ID, video.ReviewVersion)
				tracked, err := s.repo.LifecyclePublicationTracked(ctx, eventID)
				if err != nil {
					return err
				}
				if !tracked {
					continue
				}
				ready, err := s.repo.LifecyclePublicationReady(ctx, eventID)
				if err != nil {
					return err
				}
				if ready {
					continue
				}
				if err := s.publishAndMarkReady(ctx, video); err != nil {
					return err
				}
				continue
			}
		}
		if video.Visibility != domainvideo.VisibilityPublic || video.Status != domainvideo.StatusPublished {
			video.MediaURL = ""
			video.CoverURL = ""
			video.PlaybackSources = nil
			video.MediaStatus = domainmedia.MediaStatusReady
			video.MediaErrorCode = ""
			eligible, err := s.repo.UpdateMediaProjection(ctx, video)
			if err != nil {
				return err
			}
			if eligible {
				return s.MediaReady(ctx, assetID)
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
		video.MediaURL = delivery.MediaURL
		video.CoverURL = delivery.CoverURL
		video.PlaybackSources = delivery.PlaybackSources
		video.MediaStatus = domainmedia.MediaStatusReady
		video.MediaErrorCode = ""
		eligible, err := s.repo.UpdateMediaProjection(ctx, video)
		if err != nil {
			return err
		}
		if !eligible {
			if err := s.delivery.ProtectVideo(ctx, video.ID, video.MediaAssetID, video.CoverAssetID); err != nil {
				return err
			}
			continue
		}
		if s.cacheInvalidator != nil {
			_ = s.cacheInvalidator.InvalidateVideo(ctx, video.ID)
		}
		if video.IsPubliclyReadable() {
			eventID := domainmessage.PublicationEventID(video.ID, video.ReviewVersion)
			tracked, err := s.repo.LifecyclePublicationTracked(
				ctx, eventID,
			)
			if err != nil {
				return err
			}
			if !tracked {
				continue
			}
			ready, err := s.repo.LifecyclePublicationReady(ctx, eventID)
			if err != nil {
				return err
			}
			if ready {
				continue
			}
			if publishErr := s.publishAndMarkReady(ctx, video); publishErr != nil {
				protectErr := s.delivery.ProtectVideo(ctx, video.ID, video.MediaAssetID, video.CoverAssetID)
				video.MediaStatus = domainmedia.MediaStatusProcessing
				video.MediaErrorCode = "publication_event_failed"
				video.MediaURL = ""
				video.CoverURL = ""
				video.PlaybackSources = nil
				_, rollbackErr := s.repo.UpdateMediaProjection(ctx, video)
				if s.cacheInvalidator != nil {
					_ = s.cacheInvalidator.InvalidateVideo(ctx, video.ID)
				}
				return errors.Join(publishErr, protectErr, rollbackErr)
			}
		}
	}
	return nil
}

func (s *MediaPublicationService) publishAndMarkReady(
	ctx context.Context,
	video *domainvideo.Video,
) error {
	event := NewPublishedEvent(video)
	if event == nil {
		return nil
	}
	if s.publisher != nil {
		if err := s.publisher.PublishVideoPublished(ctx, event); err != nil {
			return err
		}
	}
	return s.repo.MarkLifecyclePublicationReady(ctx, event.EventID, time.Now().UTC())
}

func (s *MediaPublicationService) MediaFailed(ctx context.Context, assetID int64, profileVersion, errorCode string) error {
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
		video.MediaProfileVersion = profileVersion
		video.MediaURL = ""
		video.CoverURL = ""
		video.PlaybackSources = nil
		if _, err := s.repo.UpdateMediaProjection(ctx, video); err != nil {
			return err
		}
		if s.delivery != nil && video.MediaAssetID > 0 {
			if err := s.delivery.ProtectVideo(ctx, video.ID, video.MediaAssetID, video.CoverAssetID); err != nil {
				return err
			}
		}
		if s.cacheInvalidator != nil {
			_ = s.cacheInvalidator.InvalidateVideo(ctx, video.ID)
		}
	}
	return nil
}

func (s *MediaPublicationService) MediaRepairing(ctx context.Context, assetID int64, errorCode string) error {
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
		video.MediaStatus = domainmedia.MediaStatusProcessing
		video.MediaErrorCode = errorCode
		video.MediaURL = ""
		video.CoverURL = ""
		video.PlaybackSources = nil
		if _, err := s.repo.UpdateMediaProjection(ctx, video); err != nil {
			return err
		}
		if s.delivery != nil && video.MediaAssetID > 0 {
			if err := s.delivery.ProtectVideo(
				ctx, video.ID, video.MediaAssetID, video.CoverAssetID,
			); err != nil {
				return err
			}
		}
		if s.cacheInvalidator != nil {
			_ = s.cacheInvalidator.InvalidateVideo(ctx, video.ID)
		}
	}
	return nil
}

func (s *MediaPublicationService) ProtectVideo(ctx context.Context, videoID, mediaAssetID, coverAssetID int64) error {
	if s == nil || s.delivery == nil || mediaAssetID <= 0 {
		return nil
	}
	if err := s.delivery.ProtectVideo(ctx, videoID, mediaAssetID, coverAssetID); err != nil {
		return err
	}
	if s.repo == nil {
		return nil
	}
	videos, err := s.repo.ListByMediaAssetID(ctx, mediaAssetID)
	if err != nil {
		return err
	}
	for _, video := range videos {
		if video == nil || video.ID != videoID {
			continue
		}
		video.MediaURL = ""
		video.CoverURL = ""
		video.PlaybackSources = nil
		eligible, err := s.repo.UpdateMediaProjection(ctx, video)
		if err != nil {
			return err
		}
		if eligible {
			return s.MediaReady(ctx, mediaAssetID)
		}
		if s.cacheInvalidator != nil {
			_ = s.cacheInvalidator.InvalidateVideo(ctx, video.ID)
		}
	}
	return nil
}
