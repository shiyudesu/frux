package infravideo

import (
	"testing"

	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
	domainvideo "github.com/shiyudesu/frux/internal/domain/video"
)

func TestContentWorkDeltas(t *testing.T) {
	tests := []struct {
		name                              string
		oldStatus, newStatus              int
		oldVisibility, newVisibility      string
		oldMediaStatus, newMediaStatus    string
		wantPublicDelta, wantPrivateDelta int
	}{
		{
			name:      "delete published public work",
			oldStatus: domainvideo.StatusPublished, newStatus: domainvideo.StatusDeleted,
			oldVisibility: domainvideo.VisibilityPublic, newVisibility: domainvideo.VisibilityPublic,
			oldMediaStatus: domainmedia.MediaStatusReady, newMediaStatus: domainmedia.MediaStatusReady,
			wantPublicDelta: -1,
		},
		{
			name:      "repeat deleted state",
			oldStatus: domainvideo.StatusDeleted, newStatus: domainvideo.StatusDeleted,
			oldVisibility: domainvideo.VisibilityPublic, newVisibility: domainvideo.VisibilityPublic,
			oldMediaStatus: domainmedia.MediaStatusReady, newMediaStatus: domainmedia.MediaStatusReady,
		},
		{
			name:      "delete pending public work",
			oldStatus: domainvideo.StatusPendingReview, newStatus: domainvideo.StatusDeleted,
			oldVisibility: domainvideo.VisibilityPublic, newVisibility: domainvideo.VisibilityPublic,
			oldMediaStatus: domainmedia.MediaStatusReady, newMediaStatus: domainmedia.MediaStatusReady,
		},
		{
			name:      "delete private work",
			oldStatus: domainvideo.StatusPublished, newStatus: domainvideo.StatusDeleted,
			oldVisibility: domainvideo.VisibilityPrivate, newVisibility: domainvideo.VisibilityPrivate,
			oldMediaStatus: domainmedia.MediaStatusReady, newMediaStatus: domainmedia.MediaStatusReady,
			wantPrivateDelta: -1,
		},
		{
			name:      "make published work private",
			oldStatus: domainvideo.StatusPublished, newStatus: domainvideo.StatusPublished,
			oldVisibility: domainvideo.VisibilityPublic, newVisibility: domainvideo.VisibilityPrivate,
			oldMediaStatus: domainmedia.MediaStatusReady, newMediaStatus: domainmedia.MediaStatusReady,
			wantPublicDelta: -1, wantPrivateDelta: 1,
		},
		{
			name:      "make published work public",
			oldStatus: domainvideo.StatusPublished, newStatus: domainvideo.StatusPublished,
			oldVisibility: domainvideo.VisibilityPrivate, newVisibility: domainvideo.VisibilityPublic,
			oldMediaStatus: domainmedia.MediaStatusReady, newMediaStatus: domainmedia.MediaStatusReady,
			wantPublicDelta: 1, wantPrivateDelta: -1,
		},
		{
			name:      "take published work offline",
			oldStatus: domainvideo.StatusPublished, newStatus: domainvideo.StatusOffline,
			oldVisibility: domainvideo.VisibilityPublic, newVisibility: domainvideo.VisibilityPublic,
			oldMediaStatus: domainmedia.MediaStatusReady, newMediaStatus: domainmedia.MediaStatusReady,
			wantPublicDelta: -1,
		},
		{
			name:      "restore public work",
			oldStatus: domainvideo.StatusOffline, newStatus: domainvideo.StatusPublished,
			oldVisibility: domainvideo.VisibilityPublic, newVisibility: domainvideo.VisibilityPublic,
			oldMediaStatus: domainmedia.MediaStatusReady, newMediaStatus: domainmedia.MediaStatusReady,
			wantPublicDelta: 1,
		},
		{
			name:      "media becomes ready after approval",
			oldStatus: domainvideo.StatusPublished, newStatus: domainvideo.StatusPublished,
			oldVisibility: domainvideo.VisibilityPublic, newVisibility: domainvideo.VisibilityPublic,
			oldMediaStatus: domainmedia.MediaStatusProcessing, newMediaStatus: domainmedia.MediaStatusReady,
			wantPublicDelta: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			publicDelta, privateDelta := ContentWorkDeltas(
				tt.oldStatus, tt.oldVisibility, tt.oldMediaStatus,
				tt.newStatus, tt.newVisibility, tt.newMediaStatus,
			)
			if publicDelta != tt.wantPublicDelta || privateDelta != tt.wantPrivateDelta {
				t.Fatalf(
					"content deltas = (%d, %d), want (%d, %d)",
					publicDelta, privateDelta, tt.wantPublicDelta, tt.wantPrivateDelta,
				)
			}
		})
	}
}
