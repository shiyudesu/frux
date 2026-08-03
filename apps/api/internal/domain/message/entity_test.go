package domainmessage

import (
	"testing"
	"time"
)

func TestCommentMessageTypesAndTargets(t *testing.T) {
	for _, messageType := range []string{TypeComment, TypeCommentReply, TypeCommentLike} {
		message, err := New(7, messageType, "title", "content", "event")
		if err != nil {
			t.Fatalf("new %s message: %v", messageType, err)
		}
		message.WithTargets(11, 13, 12)
		if err := message.ValidateTargets(); err != nil {
			t.Fatalf("validate %s targets: %v", messageType, err)
		}
		if message.VideoID != 11 || message.CommentID != 13 || message.RootCommentID != 12 {
			t.Fatalf("unexpected targets for %s: %+v", messageType, message)
		}
	}
}

func TestLegacyRestoredMessageAllowsMissingTargets(t *testing.T) {
	message := Restore(1, 7, TypeComment, "title", "content", "legacy-event", false, time.Now(), nil)
	if message.VideoID != 0 || message.CommentID != 0 || message.RootCommentID != 0 {
		t.Fatalf("legacy message unexpectedly requires targets: %+v", message)
	}
}
