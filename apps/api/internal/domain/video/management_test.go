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

func TestCollectionInvariants(t *testing.T) {
	collection, err := NewCollection(7, "  series  ", " description ", "", "key")
	if err != nil {
		t.Fatalf("new collection: %v", err)
	}
	if collection.Title != "series" || collection.Visibility != VisibilityPrivate {
		t.Fatalf("unexpected collection defaults: %+v", collection)
	}
	public := VisibilityPublic
	if err := collection.UpdateBy(7, CollectionUpdate{Visibility: &public}); err != nil {
		t.Fatalf("update collection: %v", err)
	}
	if collection.Visibility != VisibilityPublic {
		t.Fatalf("unexpected visibility: %s", collection.Visibility)
	}
	if err := collection.DeleteBy(8); err != ErrCollectionPermissionDenied {
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
