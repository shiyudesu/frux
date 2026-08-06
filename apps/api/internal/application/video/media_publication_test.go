package applicationvideo

import (
	"context"
	"errors"
	"testing"
	"time"

	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
	domainvideo "github.com/shiyudesu/frux/internal/domain/video"
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
	delivery := &mediaDeliveryResolverStub{}
	service := NewMediaPublicationService(repo, delivery, nil, nil)
	if err := service.MediaFailed(context.Background(), 21, "probe_invalid"); err != nil {
		t.Fatalf("publish media failure: %v", err)
	}
	if video.MediaStatus != domainmedia.MediaStatusFailed || video.MediaErrorCode != "probe_invalid" ||
		video.MediaURL != "" || video.CoverURL != "" || delivery.protectCalls != 1 {
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

	if video.MediaStatus != domainmedia.MediaStatusProcessing || video.MediaErrorCode != "publication_event_failed" ||
		video.MediaURL != "" || video.CoverURL != "" || delivery.protectCalls != 1 {
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

func TestMediaPublicationKeepsPendingAndRejectedVariantsProtected(t *testing.T) {
	for _, status := range []int{domainvideo.StatusPendingReview, domainvideo.StatusRejected} {
		t.Run(domainvideoStatusName(status), func(t *testing.T) {
			video := domainvideo.RestoreVideoWithMedia(
				int64(100+status), 7, "protected video", "", "stale-public-url", "stale-cover",
				status, domainvideo.VisibilityPublic, 0, 0, 0, nil,
				time.Now().UTC(), time.Now().UTC(), "",
				int64(200+status), domainmedia.MediaStatusProcessing, "", []domainmedia.PlaybackSource{{
					Type: domainmedia.SourceTypeMP4, URL: "stale-source",
				}}, int64(300+status),
			)
			repo := &mediaProjectionRepositoryStub{videos: []*domainvideo.Video{video}}
			delivery := &mediaDeliveryResolverStub{delivery: &domainmedia.ResolvedDelivery{
				MediaURL: "https://cdn.example.test/should-not-publish.mp4",
			}}
			publisher := &publishedEventPublisherStub{}
			service := NewMediaPublicationService(repo, delivery, publisher, nil)

			if err := service.MediaReady(context.Background(), video.MediaAssetID); err != nil {
				t.Fatalf("record media readiness: %v", err)
			}

			if video.MediaStatus != domainmedia.MediaStatusReady || video.MediaURL != "" ||
				video.CoverURL != "" || len(video.PlaybackSources) != 0 {
				t.Fatalf("protected lifecycle leaked delivery: %+v", video)
			}
			if delivery.calls != 0 || publisher.events != 0 || repo.updates != 1 {
				t.Fatalf("unexpected side effects: delivery=%d events=%d updates=%d", delivery.calls, publisher.events, repo.updates)
			}
		})
	}
}

func TestMediaPublicationCompensatesWhenEligibilityChangesDuringPromotion(t *testing.T) {
	publishedAt := time.Now().UTC()
	video := domainvideo.RestoreVideoWithMedia(
		501, 7, "racing video", "", "", "",
		domainvideo.StatusPublished, domainvideo.VisibilityPublic,
		0, 0, 0, &publishedAt, publishedAt, publishedAt, "",
		601, domainmedia.MediaStatusProcessing, "", nil, 602,
	)
	ineligible := false
	repo := &mediaProjectionRepositoryStub{
		videos:           []*domainvideo.Video{video},
		eligibleOverride: &ineligible,
	}

	delivery := &mediaDeliveryResolverStub{delivery: &domainmedia.ResolvedDelivery{
		MediaURL: "https://cdn.example.test/racing.mp4",
	}}
	service := NewMediaPublicationService(repo, delivery, nil, nil)
	if err := service.MediaReady(context.Background(), video.MediaAssetID); err != nil {
		t.Fatalf("media ready race: %v", err)
	}
	if delivery.calls != 1 || delivery.protectCalls != 1 || video.MediaURL != "" {
		t.Fatalf("ineligible promoted media was not compensated: video=%+v delivery=%+v", video, delivery)
	}
}

func TestMediaPublicationDoesNotTrustStaleStoredURL(t *testing.T) {
	publishedAt := time.Now().UTC()
	video := domainvideo.RestoreVideoWithMedia(
		701, 7, "stale URL", "", "https://cdn.example.test/stale.mp4", "https://cdn.example.test/stale.jpg",
		domainvideo.StatusPublished, domainvideo.VisibilityPublic,
		0, 0, 0, &publishedAt, publishedAt, publishedAt, "",
		801, domainmedia.MediaStatusReady, "", []domainmedia.PlaybackSource{{
			Type: domainmedia.SourceTypeMP4, URL: "https://cdn.example.test/stale.mp4",
		}}, 802,
	)
	repo := &mediaProjectionRepositoryStub{videos: []*domainvideo.Video{video}}
	delivery := &mediaDeliveryResolverStub{delivery: &domainmedia.ResolvedDelivery{
		MediaURL: "https://cdn.example.test/fresh.mp4",
		CoverURL: "https://cdn.example.test/fresh.jpg",
	}, public: false}
	service := NewMediaPublicationService(repo, delivery, nil, nil)
	if err := service.MediaReady(context.Background(), video.MediaAssetID); err != nil {
		t.Fatalf("reconcile stale stored URL: %v", err)
	}
	if delivery.calls != 1 || video.MediaURL != "https://cdn.example.test/fresh.mp4" {
		t.Fatalf("stale stored URL skipped canonical exposure check: video=%+v calls=%d", video, delivery.calls)
	}
}

type mediaProjectionRepositoryStub struct {
	videos           []*domainvideo.Video
	updates          int
	eligibleOverride *bool
}

func (r *mediaProjectionRepositoryStub) ListByMediaAssetID(context.Context, int64) ([]*domainvideo.Video, error) {
	return r.videos, nil
}

func (r *mediaProjectionRepositoryStub) UpdateMediaProjection(_ context.Context, video *domainvideo.Video) (bool, error) {
	r.updates++
	eligible := video.IsPubliclyReadable()
	if r.eligibleOverride != nil {
		eligible = *r.eligibleOverride
	}
	if !eligible {
		video.MediaURL = ""
		video.CoverURL = ""
		video.PlaybackSources = nil
	}
	return eligible, nil
}

type mediaDeliveryResolverStub struct {
	delivery     *domainmedia.ResolvedDelivery
	calls        int
	protectCalls int
	public       bool
}

func (r *mediaDeliveryResolverStub) ResolveVideo(context.Context, int64, int64, int64) (*domainmedia.ResolvedDelivery, error) {
	r.calls++
	r.public = true
	return r.delivery, nil
}

func (r *mediaDeliveryResolverStub) ProtectVideo(context.Context, int64, int64, int64) error {
	r.protectCalls++
	r.public = false
	return nil
}

func (r *mediaDeliveryResolverStub) HasPublicVideo(context.Context, int64, int64, int64) (bool, error) {
	return r.public, nil
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

func domainvideoStatusName(status int) string {
	switch status {
	case domainvideo.StatusPendingReview:
		return "pending"
	case domainvideo.StatusRejected:
		return "rejected"
	default:
		return "unknown"
	}
}
