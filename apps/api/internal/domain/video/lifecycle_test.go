package domainvideo

import (
	"testing"
	"time"

	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
)

func TestVideoLifecycleTransitions(t *testing.T) {
	approvedAt := time.Date(2026, 8, 5, 11, 0, 0, 0, time.UTC)
	tests := []struct {
		name      string
		status    int
		published *time.Time
		apply     func(*Video) error
		want      int
		wantError bool
	}{
		{name: "approve pending", status: StatusPendingReview, apply: func(video *Video) error { return video.Approve(approvedAt) }, want: StatusPublished},
		{name: "approve published idempotent", status: StatusPublished, published: &approvedAt, apply: func(video *Video) error { return video.Approve(approvedAt.Add(time.Hour)) }, want: StatusPublished},
		{name: "approve rejected rejected", status: StatusRejected, apply: func(video *Video) error { return video.Approve(approvedAt) }, want: StatusRejected, wantError: true},
		{name: "reject pending", status: StatusPendingReview, apply: func(video *Video) error { return video.Reject() }, want: StatusRejected},
		{name: "reject rejected idempotent", status: StatusRejected, apply: func(video *Video) error { return video.Reject() }, want: StatusRejected},
		{name: "take published offline", status: StatusPublished, published: &approvedAt, apply: func(video *Video) error { return video.TakeOffline() }, want: StatusOffline},
		{name: "take offline idempotent", status: StatusOffline, published: &approvedAt, apply: func(video *Video) error { return video.TakeOffline() }, want: StatusOffline},
		{name: "restore offline", status: StatusOffline, published: &approvedAt, apply: func(video *Video) error { return video.Restore() }, want: StatusPublished},
		{name: "restore published idempotent", status: StatusPublished, published: &approvedAt, apply: func(video *Video) error { return video.Restore() }, want: StatusPublished},
		{name: "restore offline without approval", status: StatusOffline, apply: func(video *Video) error { return video.Restore() }, want: StatusOffline, wantError: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			video := &Video{Status: tt.status, PublishedAt: tt.published}
			err := tt.apply(video)
			if (err != nil) != tt.wantError {
				t.Fatalf("transition error = %v, wantError=%v", err, tt.wantError)
			}
			if video.Status != tt.want {
				t.Fatalf("status = %d, want %d", video.Status, tt.want)
			}
			if tt.published != nil && (video.PublishedAt == nil || !video.PublishedAt.Equal(*tt.published)) {
				t.Fatalf("published_at changed: got=%v want=%v", video.PublishedAt, tt.published)
			}
			if tt.name == "approve pending" && (video.PublishedAt == nil || !video.PublishedAt.Equal(approvedAt)) {
				t.Fatalf("approval did not set published_at: %v", video.PublishedAt)
			}
		})
	}
}

func TestDeletedVideoRejectsEveryLifecycleTransition(t *testing.T) {
	approvedAt := time.Now().UTC()
	for name, apply := range map[string]func(*Video) error{
		"approve":      func(video *Video) error { return video.Approve(approvedAt) },
		"reject":       func(video *Video) error { return video.Reject() },
		"take offline": func(video *Video) error { return video.TakeOffline() },
		"restore":      func(video *Video) error { return video.Restore() },
	} {
		t.Run(name, func(t *testing.T) {
			video := &Video{Status: StatusDeleted, PublishedAt: &approvedAt}
			if err := apply(video); err != ErrVideoStateNotAllowed {
				t.Fatalf("error = %v, want ErrVideoStateNotAllowed", err)
			}
			if video.Status != StatusDeleted {
				t.Fatalf("deleted status changed: %d", video.Status)
			}
		})
	}
}

func TestPublicEligibilityRequiresAllIndependentGates(t *testing.T) {
	video := &Video{
		Status: StatusPendingReview, Visibility: VisibilityPublic,
		MediaStatus: domainmedia.MediaStatusReady,
	}
	if video.IsPubliclyReadable() {
		t.Fatal("pending video bypassed review gate")
	}
	video.Status = StatusPublished
	video.MediaStatus = domainmedia.MediaStatusProcessing
	if video.IsPubliclyReadable() {
		t.Fatal("processing video bypassed media gate")
	}
	video.MediaStatus = domainmedia.MediaStatusReady
	video.Visibility = VisibilityPrivate
	if video.IsPubliclyReadable() {
		t.Fatal("private video bypassed visibility gate")
	}
	video.Visibility = VisibilityPublic
	if !video.IsPubliclyReadable() {
		t.Fatal("fully eligible video was not readable")
	}
}

func TestNewVideosStartPendingReviewWithoutPublicationTime(t *testing.T) {
	processing, err := NewProcessing(7, "processing", "", 11, 12, "")
	if err != nil {
		t.Fatalf("NewProcessing() error = %v", err)
	}
	legacy, err := NewPublished(7, "legacy", "", "media", "cover", "")
	if err != nil {
		t.Fatalf("NewPublished() error = %v", err)
	}
	for _, video := range []*Video{processing, legacy} {
		if video.Status != StatusPendingReview || video.PublishedAt != nil || video.IsPubliclyReadable() {
			t.Fatalf("unexpected new video lifecycle: %+v", video)
		}
	}
}

func TestRestoreVideoNormalizesOnlyUnknownLifecycleValues(t *testing.T) {
	now := time.Now().UTC()
	for _, status := range []int{
		StatusDraft, StatusPublished, StatusOffline, StatusDeleted, StatusPendingReview, StatusRejected,
	} {
		video := RestoreVideoWithVisibility(1, 2, "title", "", "media", "cover", status, "", 0, 0, 0, nil, now, now, "")
		if video.Status != status {
			t.Fatalf("known status %d restored as %d", status, video.Status)
		}
	}
	if video := RestoreVideoWithVisibility(1, 2, "title", "", "media", "cover", 0, "", 0, 0, 0, &now, now, now, ""); video.Status != StatusPublished {
		t.Fatalf("legacy zero status restored as %d", video.Status)
	}
	if video := RestoreVideoWithVisibility(1, 2, "title", "", "media", "cover", 99, "", 0, 0, 0, nil, now, now, ""); video.Status != StatusDraft {
		t.Fatalf("unknown status restored as %d", video.Status)
	}
}
