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

func TestVideoLifecycleMessageValidationAndLegacyCompatibility(t *testing.T) {
	message, err := New(7, TypeVideoLifecycle, "视频已发布", "内容", "video-published:9:1")
	if err != nil {
		t.Fatal(err)
	}
	message.WithTargets(9, 0, 0)
	message.WithLifecycle(
		LifecycleStagePublished, LifecycleResultPublic, "", 1, time.Now(),
	)
	if err := message.ValidateTargets(); err != nil {
		t.Fatalf("targets: %v", err)
	}
	if err := message.ValidateLifecycle(); err != nil {
		t.Fatalf("lifecycle: %v", err)
	}
	message.WithLifecycle(
		LifecycleStagePublished, LifecycleResultFailed, "", 1, time.Now(),
	)
	if err := message.ValidateLifecycle(); err != ErrInvalidLifecycle {
		t.Fatalf("invalid lifecycle error = %v", err)
	}
	withoutEvent, err := New(7, TypeVideoLifecycle, "视频已发布", "内容", "")
	if err != nil {
		t.Fatal(err)
	}
	withoutEvent.WithTargets(9, 0, 0)
	withoutEvent.WithLifecycle(
		LifecycleStagePublished, LifecycleResultPublic, "", 1, time.Now(),
	)
	if err := withoutEvent.ValidateLifecycle(); err != ErrInvalidLifecycle {
		t.Fatalf("missing event id error = %v", err)
	}
	if ValidReviewReasonCode("safe") || !ValidReviewReasonCode("other_policy_violation") {
		t.Fatal("review reason registry accepted an unsafe reason")
	}
	legacy := RestoreWithLifecycle(
		1, 7, TypeSystem, "旧审核通知", "内容", "legacy",
		0, "", "", 0, 0, 0, "", "", "", 0, nil,
		false, time.Now(), nil,
	)
	if err := legacy.ValidateLifecycle(); err != nil {
		t.Fatalf("legacy system validation: %v", err)
	}
}
