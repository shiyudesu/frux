package interfaceshttpsearch

import (
	applicationsearch "GCFeed/internal/application/search"
	domainsearch "GCFeed/internal/domain/search"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
)

type handlerVideoIndexStub struct {
	items []*domainsearch.VideoIndexItem
	err   error
}

func (s handlerVideoIndexStub) SearchVideos(context.Context, string, *domainsearch.VideoCursor, int) ([]*domainsearch.VideoIndexItem, error) {
	return s.items, s.err
}

type handlerUserIndexStub struct {
	items []*domainsearch.UserIndexItem
	err   error
}

func (s handlerUserIndexStub) SearchUsers(context.Context, string, *domainsearch.UserCursor, int) ([]*domainsearch.UserIndexItem, error) {
	return s.items, s.err
}

func TestHandlersReturnTypedPagesAndMapErrors(t *testing.T) {
	now := time.Date(2026, 8, 4, 6, 0, 0, 0, time.UTC)
	service := applicationsearch.New(
		handlerVideoIndexStub{items: []*domainsearch.VideoIndexItem{{
			ID: 11, AuthorID: 7, Title: "cat", CoverURL: "https://example.com/cover.jpg",
			Status: 2, Visibility: "public", PublishedAt: now, CreatedAt: now, UpdatedAt: now,
			MediaStatus: "ready",
			Relevance:   domainsearch.VideoRelevanceExactTitle,
		}}},
		handlerUserIndexStub{items: []*domainsearch.UserIndexItem{{
			ID: 7, Account: "creator", Nickname: "Creator", UpdatedAt: now,
			Relevance: domainsearch.UserRelevanceExactAccount,
		}}},
	)
	handler := New(service)
	router := server.New()
	router.GET("/api/search/videos", handler.Videos)
	router.GET("/api/search/users", handler.Users)

	videoResponse := ut.PerformRequest(router.Engine, http.MethodGet, "/api/search/videos?q=cat&limit=1", nil)
	if videoResponse.Code != http.StatusOK {
		t.Fatalf("video status=%d body=%s", videoResponse.Code, videoResponse.Body.String())
	}
	var videoPage videoPageResponse
	if err := json.Unmarshal(videoResponse.Body.Bytes(), &videoPage); err != nil {
		t.Fatal(err)
	}
	if len(videoPage.Items) != 1 || videoPage.Items[0].ID != 11 || videoPage.Items[0].AuthorID != 7 {
		t.Fatalf("unexpected video response: %+v", videoPage)
	}
	if videoPage.Items[0].Status != 2 || videoPage.Items[0].Visibility != "public" ||
		videoPage.Items[0].MediaStatus != "ready" || videoPage.Items[0].CreatedAt.IsZero() {
		t.Fatalf("video response did not reuse public Video fields: %+v", videoPage.Items[0])
	}

	userResponse := ut.PerformRequest(router.Engine, http.MethodGet, "/api/search/users?q=creator", nil)
	if userResponse.Code != http.StatusOK {
		t.Fatalf("user status=%d body=%s", userResponse.Code, userResponse.Body.String())
	}
	var userPage userPageResponse
	if err := json.Unmarshal(userResponse.Body.Bytes(), &userPage); err != nil {
		t.Fatal(err)
	}
	if len(userPage.Items) != 1 || userPage.Items[0].ID != 7 || userPage.Items[0].Account != "creator" {
		t.Fatalf("unexpected user response: %+v", userPage)
	}

	badLimit := ut.PerformRequest(router.Engine, http.MethodGet, "/api/search/videos?q=cat&limit=invalid", nil)
	if badLimit.Code != http.StatusBadRequest {
		t.Fatalf("bad limit status=%d body=%s", badLimit.Code, badLimit.Body.String())
	}
}

func TestHandlerHidesInfrastructureErrors(t *testing.T) {
	service := applicationsearch.New(
		handlerVideoIndexStub{err: errors.New("database details")},
		handlerUserIndexStub{},
	)
	handler := New(service)
	router := server.New()
	router.GET("/api/search/videos", handler.Videos)
	response := ut.PerformRequest(router.Engine, http.MethodGet, "/api/search/videos?q=cat", nil)
	if response.Code != http.StatusInternalServerError || response.Body.String() != `{"error":"搜索服务暂时不可用，请稍后重试"}` {
		t.Fatalf("unexpected infrastructure error response: status=%d body=%s", response.Code, response.Body.String())
	}
}
