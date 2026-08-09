package applicationvideo

import (
	"context"
	"errors"
	"time"

	domainmessage "github.com/shiyudesu/frux/internal/domain/message"
	domainvideo "github.com/shiyudesu/frux/internal/domain/video"
)

var ErrLifecycleNotificationSuperseded = errors.New("video lifecycle notification is superseded")

type LifecyclePublicationRepository interface {
	FindByIDAnyStatus(ctx context.Context, videoID int64) (*domainvideo.Video, error)
	LifecyclePublicationReady(ctx context.Context, eventID string) (bool, error)
	MarkLifecyclePublicationReady(ctx context.Context, eventID string, readyAt time.Time) error
}

type LifecycleMediaPublisher interface {
	MediaReady(ctx context.Context, assetID int64) error
}

type PublicationRecoveryService struct {
	repository LifecyclePublicationRepository
	media      LifecycleMediaPublisher
	publisher  PublishedEventPublisher
}

func NewPublicationRecoveryService(
	repository LifecyclePublicationRepository,
	media LifecycleMediaPublisher,
	publisher PublishedEventPublisher,
) *PublicationRecoveryService {
	return &PublicationRecoveryService{
		repository: repository, media: media, publisher: publisher,
	}
}

func (s *PublicationRecoveryService) EnsurePublication(
	ctx context.Context,
	notification domainmessage.LifecycleNotification,
) error {
	if notification.Stage != domainmessage.LifecycleStagePublished {
		return nil
	}
	if s == nil || s.repository == nil {
		return ErrLifecycleNotificationNotReady
	}
	video, err := s.repository.FindByIDAnyStatus(ctx, notification.VideoID)
	if err != nil {
		if errors.Is(err, domainvideo.ErrVideoNotFound) {
			return ErrLifecycleNotificationSuperseded
		}
		return err
	}
	if video == nil || video.Status == domainvideo.StatusDeleted ||
		video.ReviewVersion != notification.ReviewVersion {
		return ErrLifecycleNotificationSuperseded
	}
	if video.MediaAssetID > 0 {
		if s.media == nil {
			return ErrLifecycleNotificationNotReady
		}
		if err := s.media.MediaReady(ctx, video.MediaAssetID); err != nil {
			return err
		}
		return nil
	}
	if !video.IsPubliclyReadable() {
		return ErrLifecycleNotificationNotReady
	}
	event := NewPublishedEvent(video)
	if event == nil {
		return ErrLifecycleNotificationNotReady
	}
	if store, ok := s.repository.(PublicationEventStore); ok {
		return store.EnsurePublicationEvent(ctx, event, time.Now().UTC())
	}
	if s.publisher != nil {
		if err := s.publisher.PublishVideoPublished(ctx, event); err != nil {
			return err
		}
	}
	return s.repository.MarkLifecyclePublicationReady(
		ctx, event.EventID, time.Now().UTC(),
	)
}
