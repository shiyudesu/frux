package applicationinteraction

import (
	domaininteraction "GCFeed/internal/domain/interaction"
	"errors"
	"testing"
	"time"
)

func TestRootCursorRejectsCrossSortReuse(t *testing.T) {
	cursor := encodeCommentCursor(&domaininteraction.CommentCursor{
		Version:   domaininteraction.CommentCursorVersion,
		Sort:      domaininteraction.CommentSortLatest,
		CreatedAt: time.Now().UTC(),
		CommentID: 10,
	})
	if _, err := parseRootCommentCursor(cursor, domaininteraction.CommentSortHot); !errors.Is(err, domaininteraction.ErrInvalidCursor) {
		t.Fatalf("expected cross-sort cursor rejection, got %v", err)
	}
	if parsed, err := parseRootCommentCursor(cursor, domaininteraction.CommentSortLatest); err != nil ||
		parsed.Sort != domaininteraction.CommentSortLatest || parsed.CommentID != 10 {
		t.Fatalf("latest cursor did not round trip: parsed=%+v err=%v", parsed, err)
	}
}
