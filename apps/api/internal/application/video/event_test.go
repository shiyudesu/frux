package applicationvideo

import (
	"testing"
	"time"

	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
	domainvideo "github.com/shiyudesu/frux/internal/domain/video"
)

func TestPublishedEventRequiresCombinedPublicEligibility(t *testing.T) {
	publishedAt := time.Now().UTC()
	video := domainvideo.RestoreVideoWithMedia(
		1, 2, "title", "", "media", "cover",
		domainvideo.StatusPendingReview, domainvideo.VisibilityPublic,
		0, 0, 0, &publishedAt, publishedAt, publishedAt, "",
		0, domainmedia.MediaStatusReady, "", nil, 0,
	)
	if event := NewPublishedEvent(video); event != nil {
		t.Fatalf("pending video produced event: %+v", event)
	}
	video.Status = domainvideo.StatusPublished
	video.MediaStatus = domainmedia.MediaStatusProcessing
	if event := NewPublishedEvent(video); event != nil {
		t.Fatalf("processing video produced event: %+v", event)
	}
	video.MediaStatus = domainmedia.MediaStatusReady
	video.ReviewVersion = 3
	if event := NewPublishedEvent(video); event == nil {
		t.Fatal("eligible video did not produce published event")
	} else if event.EventID != "video-published:1:3" {
		t.Fatalf("event id = %q", event.EventID)
	}
}
