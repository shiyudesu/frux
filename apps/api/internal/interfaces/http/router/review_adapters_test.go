package interfaceshttprouter

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	applicationvideo "github.com/shiyudesu/frux/internal/application/video"
	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
	domainreview "github.com/shiyudesu/frux/internal/domain/review"
	domainvideo "github.com/shiyudesu/frux/internal/domain/video"
)

type reviewPreviewRepositoryStub struct {
	assets   map[int64]*domainmedia.MediaAsset
	variants map[int64][]*domainmedia.MediaVariant
}

func (r reviewPreviewRepositoryStub) FindAssetByID(_ context.Context, assetID int64) (*domainmedia.MediaAsset, error) {
	asset := r.assets[assetID]
	if asset == nil {
		return nil, domainmedia.ErrMediaAssetNotFound
	}
	return asset, nil
}

func (r reviewPreviewRepositoryStub) ListReadyVariants(_ context.Context, assetID int64) ([]*domainmedia.MediaVariant, error) {
	return r.variants[assetID], nil
}

type reviewPreviewResolverStub struct {
	keys []string
}

func (*reviewPreviewResolverStub) PublicURL(string) (string, error) {
	return "", nil
}

func (r *reviewPreviewResolverStub) ProtectedURL(
	_ context.Context,
	objectKey string,
	expiry time.Duration,
) (string, time.Time, error) {
	r.keys = append(r.keys, objectKey)
	return "https://signed.example.test/" + objectKey, time.Now().UTC().Add(expiry), nil
}

type reviewPreviewLocalSignerStub struct {
	keys []string
}

func (s *reviewPreviewLocalSignerStub) Sign(
	objectKey string,
	expiry time.Duration,
) (string, time.Time, error) {
	s.keys = append(s.keys, objectKey)
	return "/review-media/" + objectKey, time.Now().UTC().Add(expiry), nil
}

func TestReviewPreviewProviderUsesProtectedReadyVariants(t *testing.T) {
	resolver := &reviewPreviewResolverStub{}
	provider := reviewPreviewProvider{
		repository: reviewPreviewRepositoryStub{
			assets: map[int64]*domainmedia.MediaAsset{
				1: {ID: 1, StorageBackend: domainmedia.StorageBackendS3, ObjectKey: "uploads/1/source.mp4", State: domainmedia.AssetStateReady},
				2: {ID: 2, StorageBackend: domainmedia.StorageBackendS3, ObjectKey: "uploads/2/source.jpg", State: domainmedia.AssetStateReady},
			},
			variants: map[int64][]*domainmedia.MediaVariant{
				1: {{AssetID: 1, Role: domainmedia.VariantRoleBaseline, ObjectKey: "processed/1/baseline.mp4"}},
				2: {{AssetID: 2, Role: domainmedia.VariantRoleCover, ObjectKey: "processed/2/cover.jpg"}},
			},
		},
		resolver: resolver,
	}
	access, err := provider.ResolveHumanPreview(context.Background(), domainreview.ReviewSubject{
		MediaAssetID: 1, CoverAssetID: 2,
	}, 5*time.Minute)
	if err != nil {
		t.Fatalf("resolve preview: %v", err)
	}
	if access.MediaURL != "https://signed.example.test/processed/1/baseline.mp4" ||
		access.CoverURL != "https://signed.example.test/processed/2/cover.jpg" ||
		fmt.Sprint(resolver.keys) != "[processed/1/baseline.mp4 processed/2/cover.jpg]" {
		t.Fatalf("preview access = %#v keys=%v", access, resolver.keys)
	}
}

func TestReviewPreviewProviderSignsLegacyLocalAssets(t *testing.T) {
	signer := &reviewPreviewLocalSignerStub{}
	provider := reviewPreviewProvider{localSigner: signer}
	access, err := provider.ResolveHumanPreview(context.Background(), domainreview.ReviewSubject{
		MediaURL: "/uploads/video/source.mp4",
		CoverURL: "/uploads/cover/source.jpg",
	}, 5*time.Minute)
	if err != nil {
		t.Fatalf("resolve local preview: %v", err)
	}

	if access.MediaURL != "/review-media/video/source.mp4" ||
		access.CoverURL != "/review-media/cover/source.jpg" ||
		fmt.Sprint(signer.keys) != "[video/source.mp4 cover/source.jpg]" {
		t.Fatalf("local preview = %#v keys=%v", access, signer.keys)
	}
}

func TestReviewPreviewProviderRejectsUnprotectedExternalURL(t *testing.T) {
	provider := reviewPreviewProvider{}
	if _, err := provider.ResolveHumanPreview(context.Background(), domainreview.ReviewSubject{
		MediaURL: "https://public.example.test/source.mp4",
	}, 5*time.Minute); !errors.Is(err, domainreview.ErrReviewPreviewUnavailable) {
		t.Fatalf("external preview error = %v", err)
	}
}

type outcomeVideoReaderStub struct {
	video *domainvideo.Video
}

func (s outcomeVideoReaderStub) FindByIDAnyStatus(context.Context, int64) (*domainvideo.Video, error) {
	return s.video, nil
}

type outcomeMediaPublicationStub struct {
	ready int
}

type outcomePublicationTrackerStub struct {
	video *domainvideo.Video
	ready bool
	marks int
}

func (s *outcomePublicationTrackerStub) FindByIDAnyStatus(
	context.Context, int64,
) (*domainvideo.Video, error) {
	return s.video, nil
}

func (s *outcomePublicationTrackerStub) LifecyclePublicationReady(
	context.Context, string,
) (bool, error) {
	return s.ready, nil
}

func (s *outcomePublicationTrackerStub) MarkLifecyclePublicationReady(
	context.Context, string, time.Time,
) error {
	s.ready = true
	s.marks++
	return nil
}

type outcomePublisherStub struct {
	calls int
}

func (s *outcomePublisherStub) PublishVideoPublished(
	context.Context,
	*applicationvideo.PublishedEvent,
) error {
	s.calls++
	return nil
}

func (s *outcomeMediaPublicationStub) MediaReady(context.Context, int64) error {
	s.ready++
	return nil
}

func (*outcomeMediaPublicationStub) ProtectVideo(context.Context, int64, int64, int64) error {
	return nil
}

func TestReviewOutcomeDefersPublicationUntilMediaIsReady(t *testing.T) {
	publication := &outcomeMediaPublicationStub{}
	applier := reviewOutcomeApplier{
		videoReader: outcomeVideoReaderStub{video: &domainvideo.Video{
			ID: 9, Status: domainvideo.StatusPublished,
			Visibility:  domainvideo.VisibilityPublic,
			MediaStatus: domainmedia.MediaStatusProcessing,
		}},
		mediaPublication: publication,
	}

	err := applier.ApplyReviewOutcome(context.Background(), &domainreview.ProcessingResult{
		Case: &domainreview.ReviewCase{ID: 1, VideoID: 9},
		Decision: &domainreview.AutomatedDecision{
			Outcome: domainreview.OutcomeApprove,
		},
		MediaAssetID: 41,
	})
	if err != nil || publication.ready != 0 {
		t.Fatalf("processing approval err=%v ready=%d", err, publication.ready)
	}
}

func TestReviewOutcomeRetriesFailedPublication(t *testing.T) {
	publication := &outcomeMediaPublicationStub{}
	applier := reviewOutcomeApplier{
		videoReader: outcomeVideoReaderStub{video: &domainvideo.Video{
			ID: 9, Status: domainvideo.StatusPublished,
			Visibility:     domainvideo.VisibilityPublic,
			MediaStatus:    domainmedia.MediaStatusProcessing,
			MediaErrorCode: "publication_event_failed",
		}},
		mediaPublication: publication,
	}

	err := applier.ApplyReviewOutcome(context.Background(), &domainreview.ProcessingResult{
		Case: &domainreview.ReviewCase{ID: 1, VideoID: 9},
		Decision: &domainreview.AutomatedDecision{
			Outcome: domainreview.OutcomeApprove,
		},
		MediaAssetID: 41,
	})
	if err != nil || publication.ready != 1 {
		t.Fatalf("publication retry err=%v ready=%d", err, publication.ready)
	}
}

func TestReviewOutcomePublishesLegacyEventOnlyOnce(t *testing.T) {
	publishedAt := time.Now().UTC()
	tracker := &outcomePublicationTrackerStub{video: &domainvideo.Video{
		ID: 9, ReviewVersion: 1, Status: domainvideo.StatusPublished,
		Visibility:  domainvideo.VisibilityPublic,
		MediaStatus: domainmedia.MediaStatusReady, PublishedAt: &publishedAt,
	}}
	publisher := &outcomePublisherStub{}
	applier := reviewOutcomeApplier{
		videoReader: tracker,
		publisher:   publisher,
	}
	result := &domainreview.ProcessingResult{
		Case:     &domainreview.ReviewCase{ID: 1, VideoID: 9, ReviewVersion: 1},
		Decision: &domainreview.AutomatedDecision{Outcome: domainreview.OutcomeApprove},
	}
	if err := applier.ApplyReviewOutcome(context.Background(), result); err != nil {
		t.Fatal(err)
	}
	if err := applier.ApplyReviewOutcome(context.Background(), result); err != nil {
		t.Fatal(err)
	}
	if publisher.calls != 1 || tracker.marks != 1 {
		t.Fatalf("publisher calls=%d marks=%d", publisher.calls, tracker.marks)
	}
}
