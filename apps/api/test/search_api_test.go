package test

import (
	applicationsearch "GCFeed/internal/application/search"
	domainmedia "GCFeed/internal/domain/media"
	domainsearch "GCFeed/internal/domain/search"
	interfaceshttpsearch "GCFeed/internal/interfaces/http/search"
	"context"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/hertz/pkg/app/server"
)

type searchVideoFixture struct {
	item     domainsearch.VideoIndexItem
	readable bool
}

type searchUserFixture struct {
	item   domainsearch.UserIndexItem
	active bool
}

type memorySearchIndex struct {
	videos []searchVideoFixture
	users  []searchUserFixture
}

func (m *memorySearchIndex) SearchVideos(_ context.Context, query string, cursor *domainsearch.VideoCursor, limit int) ([]*domainsearch.VideoIndexItem, error) {
	items := make([]*domainsearch.VideoIndexItem, 0)
	for _, fixture := range m.videos {
		if !fixture.readable {
			continue
		}
		item := fixture.item
		item.Relevance = videoFixtureRelevance(item, query)
		if item.Relevance == 0 || !videoAfterCursor(item, cursor) {
			continue
		}
		cloned := item
		items = append(items, &cloned)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Relevance != items[j].Relevance {
			return items[i].Relevance < items[j].Relevance
		}
		if !items[i].PublishedAt.Equal(items[j].PublishedAt) {
			return items[i].PublishedAt.After(items[j].PublishedAt)
		}
		return items[i].ID > items[j].ID
	})
	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (m *memorySearchIndex) SearchUsers(_ context.Context, query string, cursor *domainsearch.UserCursor, limit int) ([]*domainsearch.UserIndexItem, error) {
	items := make([]*domainsearch.UserIndexItem, 0)
	for _, fixture := range m.users {
		if !fixture.active {
			continue
		}
		item := fixture.item
		item.Relevance = userFixtureRelevance(item, query)
		if item.Relevance == 0 || !userAfterCursor(item, cursor) {
			continue
		}
		cloned := item
		items = append(items, &cloned)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Relevance != items[j].Relevance {
			return items[i].Relevance < items[j].Relevance
		}
		if !items[i].UpdatedAt.Equal(items[j].UpdatedAt) {
			return items[i].UpdatedAt.After(items[j].UpdatedAt)
		}
		return items[i].ID > items[j].ID
	})
	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

type searchVideoAPIResponse struct {
	ID              int64                        `json:"id"`
	AuthorID        int64                        `json:"author_id"`
	Title           string                       `json:"title"`
	CoverURL        string                       `json:"cover_url"`
	Status          int                          `json:"status"`
	Visibility      string                       `json:"visibility"`
	PublishedAt     time.Time                    `json:"published_at"`
	CreatedAt       time.Time                    `json:"created_at"`
	UpdatedAt       time.Time                    `json:"updated_at"`
	MediaStatus     string                       `json:"media_status"`
	PlaybackSources []domainmedia.PlaybackSource `json:"playback_sources"`
}

type searchUserAPIResponse struct {
	ID        int64  `json:"id"`
	Account   string `json:"account"`
	Nickname  string `json:"nickname"`
	AvatarURL string `json:"avatar_url"`
}

type searchVideoPageAPIResponse struct {
	Items      []searchVideoAPIResponse `json:"items"`
	NextCursor string                   `json:"next_cursor"`
	HasMore    bool                     `json:"has_more"`
}

type searchUserPageAPIResponse struct {
	Items      []searchUserAPIResponse `json:"items"`
	NextCursor string                  `json:"next_cursor"`
	HasMore    bool                    `json:"has_more"`
}

func TestSearchAPIFlow(t *testing.T) {
	router := newSearchRouter()

	firstResponse := performJSONRequest(router, http.MethodGet, "/api/search/videos?q=%20CAT%20&limit=2", "", "")
	requireStatus(t, firstResponse, http.StatusOK)
	var first searchVideoPageAPIResponse
	decodeJSON(t, firstResponse, &first)
	if got := searchVideoResponseIDs(first.Items); len(got) != 2 || got[0] != 105 || got[1] != 104 ||
		!first.HasMore || first.NextCursor == "" {
		t.Fatalf("unexpected anonymous video first page: %+v", first)
	}
	if first.Items[0].AuthorID != 42 || first.Items[0].CoverURL == "" ||
		first.Items[0].Status != 2 || first.Items[0].Visibility != "public" ||
		first.Items[0].MediaStatus != "ready" || first.Items[0].PublishedAt.IsZero() ||
		first.Items[0].CreatedAt.IsZero() || first.Items[0].UpdatedAt.IsZero() ||
		len(first.Items[0].PlaybackSources) != 1 {
		t.Fatalf("video result lacks navigation/display fields: %+v", first.Items[0])
	}

	secondResponse := performJSONRequest(
		router,
		http.MethodGet,
		"/api/search/videos?q=CAT&limit=2&cursor="+url.QueryEscape(first.NextCursor),
		"",
		"",
	)
	requireStatus(t, secondResponse, http.StatusOK)
	var second searchVideoPageAPIResponse
	decodeJSON(t, secondResponse, &second)
	if got := searchVideoResponseIDs(second.Items); len(got) != 2 || got[0] != 103 || got[1] != 102 ||
		second.HasMore || second.NextCursor != "" {
		t.Fatalf("unexpected video second page: %+v", second)
	}

	userResponse := performJSONRequest(router, http.MethodGet, "/api/search/users?q=ALICE&limit=2", "", "")
	requireStatus(t, userResponse, http.StatusOK)
	var users searchUserPageAPIResponse
	decodeJSON(t, userResponse, &users)
	if got := searchUserResponseIDs(users.Items); len(got) != 2 || got[0] != 205 || got[1] != 204 ||
		!users.HasMore || users.NextCursor == "" {
		t.Fatalf("unexpected anonymous user page: %+v", users)
	}
	if users.Items[0].ID <= 0 || users.Items[0].Account == "" || users.Items[0].Nickname == "" {
		t.Fatalf("user result lacks public navigation fields: %+v", users.Items[0])
	}

	for _, path := range []string{
		"/api/search/videos",
		"/api/search/videos?q=cat&limit=0",
		"/api/search/users?q=alice&limit=51",
		"/api/search/users?q=" + url.QueryEscape(strings.Repeat("界", 65)),
	} {
		response := performJSONRequest(router, http.MethodGet, path, "", "")
		requireStatus(t, response, http.StatusBadRequest)
	}

	wrongQuery := performJSONRequest(
		router,
		http.MethodGet,
		"/api/search/videos?q=dog&limit=2&cursor="+url.QueryEscape(first.NextCursor),
		"",
		"",
	)
	requireStatus(t, wrongQuery, http.StatusBadRequest)
	wrongCategory := performJSONRequest(
		router,
		http.MethodGet,
		"/api/search/users?q=CAT&limit=2&cursor="+url.QueryEscape(first.NextCursor),
		"",
		"",
	)
	requireStatus(t, wrongCategory, http.StatusBadRequest)

	allVideosResponse := performJSONRequest(router, http.MethodGet, "/api/search/videos?q=cat&limit=50", "", "")
	requireStatus(t, allVideosResponse, http.StatusOK)
	var allVideos searchVideoPageAPIResponse
	decodeJSON(t, allVideosResponse, &allVideos)
	for _, id := range searchVideoResponseIDs(allVideos.Items) {
		if id == 999 || id == 998 {
			t.Fatalf("unreadable video %d leaked into search", id)
		}
	}
	allUsersResponse := performJSONRequest(router, http.MethodGet, "/api/search/users?q=alice&limit=50", "", "")
	requireStatus(t, allUsersResponse, http.StatusOK)
	var allUsers searchUserPageAPIResponse
	decodeJSON(t, allUsersResponse, &allUsers)
	for _, id := range searchUserResponseIDs(allUsers.Items) {
		if id == 999 {
			t.Fatal("inactive user leaked into search")
		}
	}
}

func newSearchRouter() *server.Hertz {
	now := time.Date(2026, 8, 4, 7, 0, 0, 0, time.UTC)
	index := &memorySearchIndex{
		videos: []searchVideoFixture{
			{item: searchVideoItem(105, "cat", "exact", now), readable: true},
			{item: searchVideoItem(104, "Cat videos", "prefix", now), readable: true},
			{item: searchVideoItem(103, "My cat clip", "contains", now), readable: true},
			{item: searchVideoItem(102, "Other", "A cat in the description", now), readable: true},
			{item: searchVideoItem(999, "cat", "private", now.Add(time.Hour)), readable: false},
			{item: searchVideoItem(998, "cat ready later", "processing", now.Add(time.Hour)), readable: false},
		},
		users: []searchUserFixture{
			{item: searchUserItem(205, "alice", "Exact", now), active: true},
			{item: searchUserItem(204, "alice-two", "Prefix", now), active: true},
			{item: searchUserItem(203, "other", "Alice Nick", now), active: true},
			{item: searchUserItem(999, "alice-frozen", "Inactive", now.Add(time.Hour)), active: false},
		},
	}
	service := applicationsearch.New(index, index)
	handler := interfaceshttpsearch.New(service)
	router := server.New()
	search := router.Group("/api/search")
	search.GET("/videos", handler.Videos)
	search.GET("/users", handler.Users)
	return router
}

func searchVideoItem(id int64, title, description string, publishedAt time.Time) domainsearch.VideoIndexItem {
	return domainsearch.VideoIndexItem{
		ID: id, AuthorID: 42, Title: title, Description: description,
		MediaURL: "/video.mp4", CoverURL: "/cover.jpg", Status: 2, Visibility: "public",
		PublishedAt: publishedAt, CreatedAt: publishedAt, UpdatedAt: publishedAt,
		MediaStatus:     "ready",
		PlaybackSources: []domainmedia.PlaybackSource{{Type: "mp4", URL: "/video.mp4"}},
	}
}

func searchUserItem(id int64, account, nickname string, updatedAt time.Time) domainsearch.UserIndexItem {
	return domainsearch.UserIndexItem{
		ID: id, Account: account, Nickname: nickname, AvatarURL: "/avatar.jpg", UpdatedAt: updatedAt,
	}
}

func videoFixtureRelevance(item domainsearch.VideoIndexItem, query string) int {
	title, description, query := strings.ToLower(item.Title), strings.ToLower(item.Description), strings.ToLower(query)
	switch {
	case title == query:
		return domainsearch.VideoRelevanceExactTitle
	case strings.HasPrefix(title, query):
		return domainsearch.VideoRelevanceTitlePrefix
	case strings.Contains(title, query):
		return domainsearch.VideoRelevanceTitleContains
	case strings.Contains(description, query):
		return domainsearch.VideoRelevanceDescriptionOnly
	default:
		return 0
	}
}

func userFixtureRelevance(item domainsearch.UserIndexItem, query string) int {
	account, nickname, query := strings.ToLower(item.Account), strings.ToLower(item.Nickname), strings.ToLower(query)
	switch {
	case account == query:
		return domainsearch.UserRelevanceExactAccount
	case strings.HasPrefix(account, query):
		return domainsearch.UserRelevanceAccountPrefix
	case strings.HasPrefix(nickname, query):
		return domainsearch.UserRelevanceNicknamePrefix
	case strings.Contains(account, query):
		return domainsearch.UserRelevanceAccountContains
	case strings.Contains(nickname, query):
		return domainsearch.UserRelevanceNicknameContains
	default:
		return 0
	}
}

func videoAfterCursor(item domainsearch.VideoIndexItem, cursor *domainsearch.VideoCursor) bool {
	if cursor == nil {
		return true
	}
	if item.Relevance != cursor.Relevance {
		return item.Relevance > cursor.Relevance
	}
	if !item.PublishedAt.Equal(cursor.PublishedAt) {
		return item.PublishedAt.Before(cursor.PublishedAt)
	}
	return item.ID < cursor.VideoID
}

func userAfterCursor(item domainsearch.UserIndexItem, cursor *domainsearch.UserCursor) bool {
	if cursor == nil {
		return true
	}
	if item.Relevance != cursor.Relevance {
		return item.Relevance > cursor.Relevance
	}
	if !item.UpdatedAt.Equal(cursor.UpdatedAt) {
		return item.UpdatedAt.Before(cursor.UpdatedAt)
	}
	return item.ID < cursor.UserID
}

func searchVideoResponseIDs(items []searchVideoAPIResponse) []int64 {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func searchUserResponseIDs(items []searchUserAPIResponse) []int64 {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}
