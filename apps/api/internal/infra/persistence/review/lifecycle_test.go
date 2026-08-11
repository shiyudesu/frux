package infrareview

import (
	"testing"
	"time"

	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
	domainvideo "github.com/shiyudesu/frux/internal/domain/video"
	infravideo "github.com/shiyudesu/frux/internal/infra/persistence/video"
)

func TestPrepareReviewLifecycleTransitionPreservesOldStateForContentDelta(t *testing.T) {
	at := time.Date(2026, 8, 11, 10, 0, 0, 0, time.UTC)
	video := infravideo.VideoModel{
		Status:      domainvideo.StatusPendingReview,
		Visibility:  domainvideo.VisibilityPublic,
		MediaStatus: domainmedia.MediaStatusReady,
	}

	next, publicDelta, privateDelta, err := prepareReviewLifecycleTransition(
		video, domainvideo.LifecycleApprove, at,
	)
	if err != nil {
		t.Fatal(err)
	}
	if video.Status != domainvideo.StatusPendingReview {
		t.Fatalf("input status changed to %d", video.Status)
	}
	if next.Status != domainvideo.StatusPublished || next.PublishedAt == nil ||
		!next.PublishedAt.Equal(at) {
		t.Fatalf("unexpected approved video: %+v", next)
	}
	if publicDelta != 1 || privateDelta != 0 {
		t.Fatalf("content deltas = (%d, %d), want (1, 0)", publicDelta, privateDelta)
	}
}

func TestPrepareReviewLifecycleTransitionWaitsForReadyMedia(t *testing.T) {
	video := infravideo.VideoModel{
		Status:      domainvideo.StatusPendingReview,
		Visibility:  domainvideo.VisibilityPublic,
		MediaStatus: domainmedia.MediaStatusProcessing,
	}

	_, publicDelta, privateDelta, err := prepareReviewLifecycleTransition(
		video, domainvideo.LifecycleApprove, time.Now().UTC(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if publicDelta != 0 || privateDelta != 0 {
		t.Fatalf("content deltas = (%d, %d), want (0, 0)", publicDelta, privateDelta)
	}
}
