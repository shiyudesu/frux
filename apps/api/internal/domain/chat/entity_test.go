package domainchat

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestCanonicalPairAndConversationMembership(t *testing.T) {
	lower, higher, err := CanonicalPair(9, 3)
	if err != nil {
		t.Fatalf("CanonicalPair returned error: %v", err)
	}
	if lower != 3 || higher != 9 {
		t.Fatalf("unexpected pair: %d/%d", lower, higher)
	}
	if _, _, err := CanonicalPair(4, 4); !errors.Is(err, ErrSelfConversation) {
		t.Fatalf("expected self-conversation error, got %v", err)
	}
	conversation, err := NewConversation(9, 3, time.Unix(10, 0))
	if err != nil {
		t.Fatalf("NewConversation returned error: %v", err)
	}
	if !conversation.Contains(9) || conversation.Counterpart(9) != 3 || conversation.Counterpart(8) != 0 {
		t.Fatalf("conversation membership helpers are incorrect")
	}
}

func TestTextMessageNormalizesAndEnforcesLimits(t *testing.T) {
	message, err := NewTextMessage(1, 2, "  hello \n", " key ", time.Unix(10, 0))
	if err != nil {
		t.Fatalf("NewTextMessage returned error: %v", err)
	}
	if message.Text != "hello" || message.IdempotencyKey != "key" || message.Kind != MessageKindText {
		t.Fatalf("message was not normalized: %#v", message)
	}
	if _, err := NewTextMessage(1, 2, " ", "key", time.Time{}); !errors.Is(err, ErrEmptyText) {
		t.Fatalf("expected empty-text error, got %v", err)
	}
	if _, err := NewTextMessage(1, 2, strings.Repeat("x", MaxTextCodePoints+1), "key", time.Time{}); !errors.Is(err, ErrTextTooLong) {
		t.Fatalf("expected text-too-long error, got %v", err)
	}
	if _, err := NewTextMessage(1, 2, "hello", "", time.Time{}); !errors.Is(err, ErrIdempotencyKeyRequired) {
		t.Fatalf("expected idempotency-required error, got %v", err)
	}
}

func TestMessageKindInvariantsAndReadProgressAreMonotonic(t *testing.T) {
	video, err := NewVideoMessage(1, 2, 88, "video-key", time.Unix(10, 0))
	if err != nil {
		t.Fatalf("NewVideoMessage returned error: %v", err)
	}
	if video.Text != "" || video.VideoID != 88 || video.Kind != MessageKindVideo {
		t.Fatalf("unexpected video message: %#v", video)
	}
	member := RestoreMember(1, 2, 10, nil, 4, nil, nil, time.Time{}, time.Time{})
	if member.AdvanceRead(9, time.Unix(20, 0)) {
		t.Fatalf("older read boundary moved forward")
	}
	if member.AdvanceRead(12, time.Unix(20, 0)) == false || member.LastReadMessageID != 12 || member.LastReadAt == nil {
		t.Fatalf("newer read boundary was not accepted")
	}
	member.SetUnreadCount(-1)
	if member.UnreadCount != 0 {
		t.Fatalf("unread count was not clamped")
	}
}

func TestMessageSameSendPayloadIncludesConversationAndKey(t *testing.T) {
	first := RestoreMessage(1, 7, 2, MessageKindText, "hello", 0, "key", nil, time.Unix(1, 0))
	same := RestoreMessage(2, 7, 2, MessageKindText, "hello", 0, "key", nil, time.Unix(2, 0))
	differentConversation := RestoreMessage(3, 8, 2, MessageKindText, "hello", 0, "key", nil, time.Unix(3, 0))
	differentKey := RestoreMessage(4, 7, 2, MessageKindText, "hello", 0, "other", nil, time.Unix(4, 0))
	if !first.SameSendPayload(same) {
		t.Fatal("expected identical send payloads to match")
	}
	if first.SameSendPayload(differentConversation) || first.SameSendPayload(differentKey) {
		t.Fatal("conversation and idempotency key must be part of the payload identity")
	}
}
