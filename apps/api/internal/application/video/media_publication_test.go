package applicationvideo

import (
	"context"
	"errors"
	"testing"
	"time"

	domainmedia "GCFeed/internal/domain/media"
	domainvideo "GCFeed/internal/domain/video"
)

func TestMediaPublicationProjectsCompatibilityURLsAndPublishes(t *testing.T) {
	video := domainvideo.RestoreVideoWithMedia(
		31, 7, "ready video", "", "", "", domainvideo.StatusPublished, domainvideo.VisibilityPublic,
		0, 0, 0, timePointer(time.Now().UTC()), time.Now().UTC(), time.Now().UTC(), "",
		11, domainmedia.MediaStatusProcessing, "", nil, 12,
	)
	repo := &mediaProjectionRepositoryStub{videos: []*domainvideo.Video{video}}
	delivery := &mediaDeliveryResolverStub{delivery: &domainmedia.ResolvedDelivery{
		MediaURL: "https://cdn.example.test/baseline.mp4",
		CoverURL: "https://cdn.example.test/cover.jpg",
		PlaybackSources: []domainmedia.PlaybackSource{{
			Type: domainmedia.SourceTypeMP4, URL: "https://cdn.example.test/baseline.mp4",
			Role: domainmedia.VariantRoleBaseline,
		}},
	}}
	publisher := &publishedEventPublisherStub{}
	cache := &videoCacheInvalidatorStub{}
	service := NewMediaPublicationService(repo, delivery, publisher, cache)

	if err := service.MediaReady(context.Background(), 11); err != nil {
		t.Fatalf("publish media readiness: %v", err)
	}
	if video.MediaStatus != domainmedia.MediaStatusReady ||
		video.MediaURL != delivery.delivery.MediaURL ||
		video.CoverURL != delivery.delivery.CoverURL ||
		len(video.PlaybackSources) != 1 {
		t.Fatalf("unexpected media projection: %+v", video)
	}
	if repo.updates != 1 || publisher.events != 1 || cache.invalidations != 1 {
		t.Fatalf("unexpected side effects: updates=%d events=%d invalidations=%d", repo.updates, publisher.events, cache.invalidations)
	}
}

func TestMediaPublicationExposesFailureToOwner(t *testing.T) {
	video := domainvideo.RestoreVideoWithMedia(
		32, 7, "failed video", "", "", "", domainvideo.StatusPublished, domainvideo.VisibilityPublic,
		0, 0, 0, timePointer(time.Now().UTC()), time.Now().UTC(), time.Now().UTC(), "",
		21, domainmedia.MediaStatusProcessing, "", nil, 22,
	)
	repo := &mediaProjectionRepositoryStub{videos: []*domainvideo.Video{video}}
	service := NewMediaPublicationService(repo, nil, nil, nil)
	if err := service.MediaFailed(context.Background(), 21, "probe_invalid"); err != nil {
		t.Fatalf("publish media failure: %v", err)
	}
	if video.MediaStatus != domainmedia.MediaStatusFailed || video.MediaErrorCode != "probe_invalid" {
		t.Fatalf("unexpected failed media projection: %+v", video)
	}
}

func TestMediaPublicationRetriesEventAfterPublishFailure(t *testing.T) {
	video := domainvideo.RestoreVideoWithMedia(
		33, 7, "retry event", "", "", "", domainvideo.StatusPublished, domainvideo.VisibilityPublic,
		0, 0, 0, timePointer(time.Now().UTC()), time.Now().UTC(), time.Now().UTC(), "",
		41, domainmedia.MediaStatusProcessing, "", nil, 42,
	)
	repo := &mediaProjectionRepositoryStub{videos: []*domainvideo.Video{video}}
	delivery := &mediaDeliveryResolverStub{delivery: &domainmedia.ResolvedDelivery{
		MediaURL: "https://cdn.example.test/baseline.mp4", CoverURL: "https://cdn.example.test/cover.jpg",
	}}
	publisher := &publishedEventPublisherStub{err: errors.New("publish failed")}
	service := NewMediaPublicationService(repo, delivery, publisher, nil)
	if err := service.MediaReady(context.Background(), 41); err == nil {
		t.Fatal("expected publish failure")
	}
	if video.MediaStatus != domainmedia.MediaStatusProcessing || video.MediaErrorCode != "publication_event_failed" {
		t.Fatalf("expected readiness rollback, got %+v", video)
	}
	publisher.err = nil
	if err := service.MediaReady(context.Background(), 41); err != nil {
		t.Fatalf("retry publication: %v", err)
	}
	if video.MediaStatus != domainmedia.MediaStatusReady || publisher.events != 1 {
		t.Fatalf("expected successful retry, video=%+v events=%d", video, publisher.events)
	}
}

type mediaProjectionRepositoryStub struct {
	videos  []*domainvideo.Video
	updates int
}

func (r *mediaProjectionRepositoryStub) ListByMediaAssetID(context.Context, int64) ([]*domainvideo.Video, error) {
	return r.videos, nil
}

func (r *mediaProjectionRepositoryStub) UpdateMediaProjection(context.Context, *domainvideo.Video) error {
	r.updates++
	return nil
}

type mediaDeliveryResolverStub struct {
	delivery *domainmedia.ResolvedDelivery
}

func (r *mediaDeliveryResolverStub) ResolveVideo(context.Context, int64, int64, int64) (*domainmedia.ResolvedDelivery, error) {
	return r.delivery, nil
}

type publishedEventPublisherStub struct {
	events int
	err    error
}

func (p *publishedEventPublisherStub) PublishVideoPublished(context.Context, *PublishedEvent) error {
	if p.err != nil {
		return p.err
	}
	p.events++
	return nil
}

type videoCacheInvalidatorStub struct {
	invalidations int
}

func (c *videoCacheInvalidatorStub) InvalidateVideo(context.Context, int64) error {
	c.invalidations++
	return nil
}

func timePointer(value time.Time) *time.Time {
	return &value
}
