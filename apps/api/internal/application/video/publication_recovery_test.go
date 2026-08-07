package applicationvideo

import (
	"context"
	"errors"
	"testing"
	"time"

	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
	domainmessage "github.com/shiyudesu/frux/internal/domain/message"
	domainvideo "github.com/shiyudesu/frux/internal/domain/video"
)

type publicationRecoveryRepositoryStub struct {
	video *domainvideo.Video
	ready bool
	marks int
}

func (s *publicationRecoveryRepositoryStub) FindByIDAnyStatus(
	context.Context, int64,
) (*domainvideo.Video, error) {
	if s.video == nil {
		return nil, domainvideo.ErrVideoNotFound
	}
	return s.video, nil
}

func (s *publicationRecoveryRepositoryStub) LifecyclePublicationReady(
	context.Context, string,
) (bool, error) {
	return s.ready, nil
}

func (s *publicationRecoveryRepositoryStub) MarkLifecyclePublicationReady(
	context.Context, string, time.Time,
) error {
	s.ready = true
	s.marks++
	return nil
}

type publicationRecoveryMediaStub struct {
	calls int
	repo  *publicationRecoveryRepositoryStub
}

func (s *publicationRecoveryMediaStub) MediaReady(context.Context, int64) error {
	s.calls++
	s.repo.ready = true
	return nil
}

func TestPublicationRecoveryHandlesProductionLegacyAndSupersededFacts(t *testing.T) {
	now := time.Now().UTC()
	notification := domainmessage.LifecycleNotification{
		EventID:     domainmessage.PublicationEventID(9, 1),
		RecipientID: 7, VideoID: 9, ReviewVersion: 1,
		Stage:  domainmessage.LifecycleStagePublished,
		Result: domainmessage.LifecycleResultPublic, OccurredAt: now,
	}
	productionRepo := &publicationRecoveryRepositoryStub{video: &domainvideo.Video{
		ID: 9, ReviewVersion: 1, MediaAssetID: 41,
		Status: domainvideo.StatusPublished, Visibility: domainvideo.VisibilityPublic,
		MediaStatus: domainmedia.MediaStatusReady,
	}}
	media := &publicationRecoveryMediaStub{repo: productionRepo}
	production := NewPublicationRecoveryService(productionRepo, media, nil)
	if err := production.EnsurePublication(context.Background(), notification); err != nil {
		t.Fatal(err)
	}
	if media.calls != 1 || !productionRepo.ready {
		t.Fatalf("production recovery calls=%d ready=%v", media.calls, productionRepo.ready)
	}

	publishedAt := now
	legacyRepo := &publicationRecoveryRepositoryStub{video: &domainvideo.Video{
		ID: 9, ReviewVersion: 1, Status: domainvideo.StatusPublished,
		Visibility:  domainvideo.VisibilityPublic,
		MediaStatus: domainmedia.MediaStatusLegacyReady,
		MediaURL:    "https://example.com/video.mp4", PublishedAt: &publishedAt,
	}}
	publisher := &publishedEventPublisherStub{}
	legacy := NewPublicationRecoveryService(legacyRepo, nil, publisher)
	if err := legacy.EnsurePublication(context.Background(), notification); err != nil {
		t.Fatal(err)
	}
	if publisher.events != 1 || legacyRepo.marks != 1 {
		t.Fatalf("legacy recovery events=%d marks=%d", publisher.events, legacyRepo.marks)
	}

	legacyRepo.video.ReviewVersion = 2
	legacyRepo.ready = false
	if err := legacy.EnsurePublication(
		context.Background(), notification,
	); !errors.Is(err, ErrLifecycleNotificationSuperseded) {
		t.Fatalf("superseded error = %v", err)
	}
}
