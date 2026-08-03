package domaininteraction

import (
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestCommentUnicodeLimitAndFingerprint(t *testing.T) {
	content := strings.Repeat("界", MaxCommentContentLength)
	if utf8.RuneCountInString(content) != MaxCommentContentLength {
		t.Fatal("test content does not contain the expected rune count")
	}
	comment, err := NewRootComment(10, 20, content, "root-key")
	if err != nil {
		t.Fatalf("accept Unicode content at the code-point limit: %v", err)
	}
	if len(comment.Content) <= MaxCommentContentLength {
		t.Fatal("test did not exercise multibyte content")
	}
	if _, err := NewRootComment(10, 20, content+"界", "root-key-2"); !errors.Is(err, ErrCommentContentTooLong) {
		t.Fatalf("expected code-point limit error, got %v", err)
	}

	rootFingerprint := CommentRequestFingerprint(10, 0, 0, " hello ")
	if rootFingerprint != CommentRequestFingerprint(10, 0, 0, "hello") {
		t.Fatal("canonical content trimming changed the request fingerprint")
	}
	if rootFingerprint == CommentRequestFingerprint(10, 11, 12, "hello") {
		t.Fatal("thread target fields were not bound into the request fingerprint")
	}
}

func TestCommentTombstoneProjection(t *testing.T) {
	now := time.Now().UTC()
	comment := RestoreThreadedComment(
		1, 10, 20, "author", "avatar", 0, 0, 0, "", "",
		"secret", CommentStatusSelfDeleted, 2, 3, 19, "", "", true, true, now, now,
	)
	if !comment.EligibleForPublicProjection() {
		t.Fatal("self-deleted root with active replies should remain as a tombstone")
	}
	comment.ApplyPublicProjection()
	if comment.UserID != 0 || comment.UserNickname != "" || comment.Content != "" ||
		comment.Liked || comment.CanDelete || comment.LikeCount != 0 {
		t.Fatalf("tombstone leaked hidden fields: %+v", comment)
	}

	reply := RestoreThreadedComment(
		2, 10, 21, "reply", "", 1, 1, 20, "author", "",
		"hidden", CommentStatusSelfDeleted, 0, 0, 0, "", "", false, false, now, now,
	)
	if reply.EligibleForPublicProjection() {
		t.Fatal("deleted replies must not remain publicly visible")
	}
}
