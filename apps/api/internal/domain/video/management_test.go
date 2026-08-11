package domainvideo

import (
	"testing"
	"time"
)

func TestVisibilityTransitionPreservesLifecycle(t *testing.T) {
	video, err := NewPublished(7, "title", "description", "media", "cover", "")
	if err != nil {
		t.Fatalf("new video: %v", err)
	}
	if err := video.Approve(time.Now().UTC()); err != nil {
		t.Fatalf("approve video: %v", err)
	}
	publishedAt := *video.PublishedAt
	if err := video.SetVisibilityBy(7, VisibilityPrivate); err != nil {
		t.Fatalf("make private: %v", err)
	}
	if video.Status != StatusPublished || video.Visibility != VisibilityPrivate || !video.PublishedAt.Equal(publishedAt) {
		t.Fatalf("visibility changed lifecycle fields: %+v", video)
	}
	if video.IsPubliclyReadable() {
		t.Fatal("private video reported publicly readable")
	}
	if err := video.SetVisibilityBy(8, VisibilityPublic); err != ErrVideoPermissionDenied {
		t.Fatalf("expected permission error, got %v", err)
	}
}

func TestRestoreVisibilityDefaultsPublic(t *testing.T) {
	now := time.Now()
	video := RestoreVideoWithVisibility(1, 2, "title", "", "media", "cover", StatusPublished, "", 0, 0, 0, &now, now, now, "")
	if video.Visibility != VisibilityPublic {
		t.Fatalf("expected public migration default, got %q", video.Visibility)
	}
}
