package applicationsearch

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	domainsearch "github.com/shiyudesu/frux/internal/domain/search"
	"strings"
	"testing"
	"time"
)

type videoIndexStub struct {
	items      []*domainsearch.VideoIndexItem
	lastQuery  string
	lastCursor *domainsearch.VideoCursor
	lastLimit  int
	err        error
}

func (s *videoIndexStub) SearchVideos(_ context.Context, query string, cursor *domainsearch.VideoCursor, limit int) ([]*domainsearch.VideoIndexItem, error) {
	s.lastQuery, s.lastCursor, s.lastLimit = query, cursor, limit
	if s.err != nil {
		return nil, s.err
	}
	return s.items, nil
}

type userIndexStub struct {
	items      []*domainsearch.UserIndexItem
	lastQuery  string
	lastCursor *domainsearch.UserCursor
	lastLimit  int
	err        error
}

func (s *userIndexStub) SearchUsers(_ context.Context, query string, cursor *domainsearch.UserCursor, limit int) ([]*domainsearch.UserIndexItem, error) {
	s.lastQuery, s.lastCursor, s.lastLimit = query, cursor, limit
	if s.err != nil {
		return nil, s.err
	}
	return s.items, nil
}

func TestCursorCodecBindsVersionQueryAndCategory(t *testing.T) {
	now := time.Date(2026, 8, 4, 2, 0, 0, 123000000, time.UTC)
	videoCursor := EncodeVideoCursor("  Cat  ", &domainsearch.VideoCursor{
		Relevance: domainsearch.VideoRelevanceTitleContains, PublishedAt: now, VideoID: 9,
	})
	decoded, err := DecodeVideoCursor(videoCursor, "Cat")
	if err != nil || decoded.VideoID != 9 || decoded.Relevance != domainsearch.VideoRelevanceTitleContains ||
		!decoded.PublishedAt.Equal(now) {
		t.Fatalf("video cursor did not round trip: cursor=%+v err=%v", decoded, err)
	}
	if _, err := DecodeVideoCursor(videoCursor, "Dog"); !errors.Is(err, domainsearch.ErrInvalidCursor) {
		t.Fatalf("cross-query cursor error = %v, want ErrInvalidCursor", err)
	}
	if _, err := DecodeUserCursor(videoCursor, "Cat"); !errors.Is(err, domainsearch.ErrInvalidCursor) {
		t.Fatalf("cross-category cursor error = %v, want ErrInvalidCursor", err)
	}

	content, err := base64.RawURLEncoding.DecodeString(videoCursor)
	if err != nil {
		t.Fatal(err)
	}
	var payload cursorPayload
	if err := json.Unmarshal(content, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Version != videoCursorVersion {
		t.Fatalf("video cursor version = %d, want %d", payload.Version, videoCursorVersion)
	}
	payload.Version++
	content, _ = json.Marshal(payload)
	if _, err := DecodeVideoCursor(base64.RawURLEncoding.EncodeToString(content), "Cat"); !errors.Is(err, domainsearch.ErrInvalidCursor) {
		t.Fatalf("unsupported cursor version error = %v, want ErrInvalidCursor", err)
	}

	userCursor := EncodeUserCursor("Cat", &domainsearch.UserCursor{
		Relevance: domainsearch.UserRelevanceExactNickname, UpdatedAt: now, UserID: 8,
	})
	content, err = base64.RawURLEncoding.DecodeString(userCursor)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(content, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Version != userCursorVersion || payload.Version == videoCursorVersion {
		t.Fatalf("user cursor version = %d, video version = %d", payload.Version, videoCursorVersion)
	}
	payload.Version = videoCursorVersion
	content, _ = json.Marshal(payload)
	if _, err := DecodeUserCursor(base64.RawURLEncoding.EncodeToString(content), "Cat"); !errors.Is(err, domainsearch.ErrInvalidCursor) {
		t.Fatalf("legacy user cursor error = %v, want ErrInvalidCursor", err)
	}
	if decoded, err := DecodeVideoCursor(videoCursor, "Cat"); err != nil || decoded.VideoID != 9 {
		t.Fatalf("video v1 cursor stopped working: cursor=%+v err=%v", decoded, err)
	}
}

func TestSearchVideosUsesStableBoundaryTuple(t *testing.T) {
	publishedAt := time.Date(2026, 8, 4, 2, 30, 0, 0, time.UTC)
	videoIndex := &videoIndexStub{items: []*domainsearch.VideoIndexItem{
		{ID: 3, PublishedAt: publishedAt, Relevance: domainsearch.VideoRelevanceTitlePrefix},
		{ID: 2, PublishedAt: publishedAt, Relevance: domainsearch.VideoRelevanceTitlePrefix},
		{ID: 1, PublishedAt: publishedAt, Relevance: domainsearch.VideoRelevanceTitlePrefix},
	}}
	service := New(videoIndex, &userIndexStub{})
	page, err := service.SearchVideos(context.Background(), Request{Query: "  cat ", Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 || !page.HasMore || page.NextCursor == "" || videoIndex.lastQuery != "cat" || videoIndex.lastLimit != 3 {
		t.Fatalf("unexpected first page: page=%+v query=%q limit=%d", page, videoIndex.lastQuery, videoIndex.lastLimit)
	}
	cursor, err := DecodeVideoCursor(page.NextCursor, "cat")
	if err != nil {
		t.Fatal(err)
	}
	if cursor.Relevance != domainsearch.VideoRelevanceTitlePrefix || !cursor.PublishedAt.Equal(publishedAt) || cursor.VideoID != 2 {
		t.Fatalf("next cursor lost stable tuple: %+v", cursor)
	}

	videoIndex.items = []*domainsearch.VideoIndexItem{{
		ID: 1, PublishedAt: publishedAt, Relevance: domainsearch.VideoRelevanceTitlePrefix,
	}}
	second, err := service.SearchVideos(context.Background(), Request{Query: "cat", Cursor: page.NextCursor, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.HasMore || second.NextCursor != "" || videoIndex.lastCursor.VideoID != 2 {
		t.Fatalf("unexpected second page: page=%+v repository_cursor=%+v", second, videoIndex.lastCursor)
	}
}

func TestSearchUsersHasIndependentCursorAndLimitBounds(t *testing.T) {
	updatedAt := time.Date(2026, 8, 4, 3, 0, 0, 0, time.UTC)
	userIndex := &userIndexStub{items: []*domainsearch.UserIndexItem{
		{ID: 7, Nickname: "cat", UpdatedAt: updatedAt, Relevance: domainsearch.UserRelevanceExactNickname},
		{ID: 6, Nickname: "cat-two", UpdatedAt: updatedAt, Relevance: domainsearch.UserRelevanceNicknamePrefix},
	}}
	service := New(&videoIndexStub{}, userIndex)
	page, err := service.SearchUsers(context.Background(), Request{Query: "cat", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].ID != 7 || !page.HasMore || userIndex.lastLimit != 2 {
		t.Fatalf("unexpected user page: %+v", page)
	}
	if _, err := DecodeVideoCursor(page.NextCursor, "cat"); !errors.Is(err, domainsearch.ErrInvalidCursor) {
		t.Fatalf("user cursor was accepted for videos: %v", err)
	}
	for _, limit := range []int{-1, domainsearch.MaxLimit + 1} {
		if _, err := service.SearchUsers(context.Background(), Request{Query: "cat", Limit: limit}); !errors.Is(err, domainsearch.ErrInvalidLimit) {
			t.Fatalf("limit %d error = %v, want ErrInvalidLimit", limit, err)
		}
	}
	if _, err := service.SearchUsers(context.Background(), Request{Query: strings.Repeat("界", 65), Limit: 1}); !errors.Is(err, domainsearch.ErrQueryTooLong) {
		t.Fatalf("oversized Unicode query error = %v", err)
	}
}
