package infravideo

import (
	"context"
	"testing"

	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
	domainvideo "github.com/shiyudesu/frux/internal/domain/video"
	inframedia "github.com/shiyudesu/frux/internal/infra/media"
)

func TestHydrateMediaDeliveryClearsPendingAndRejectedPublicURLs(t *testing.T) {
	repository := &Repository{mediaCatalog: inframedia.NewDeliveryCatalog(nil, nil, nil)}
	for _, status := range []int{domainvideo.StatusPendingReview, domainvideo.StatusRejected} {
		video := &domainvideo.Video{
			ID: 1, MediaAssetID: 2, Status: status, Visibility: domainvideo.VisibilityPublic,
			MediaStatus: domainmedia.MediaStatusReady, MediaURL: "stale-media", CoverURL: "stale-cover",
			PlaybackSources: []domainmedia.PlaybackSource{{URL: "stale-source"}},
		}
		if err := repository.hydrateMediaDelivery(context.Background(), []*domainvideo.Video{video}); err != nil {
			t.Fatalf("hydrate status %d: %v", status, err)
		}
		if video.MediaURL != "" || video.CoverURL != "" || len(video.PlaybackSources) != 0 {
			t.Fatalf("status %d retained public delivery: %+v", status, video)
		}
	}
}
