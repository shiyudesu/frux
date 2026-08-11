package interfaceshttplibrary

import (
	"context"
	"encoding/json"
	applicationlibrary "github.com/shiyudesu/frux/internal/application/library"
	domainlibrary "github.com/shiyudesu/frux/internal/domain/library"
	"net/http"
	"testing"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
)

func TestHistoryResponseIncludesPositionAndEffectiveWatch(t *testing.T) {
	now := time.Date(2026, 7, 25, 10, 0, 0, 0, time.UTC)
	page := &applicationlibrary.Page{Items: []*domainlibrary.VideoItem{{
		Video: &domainlibrary.VideoCard{
			ID: 1001, AuthorID: 9, AuthorNickname: "author", AuthorAvatarURL: "avatar",
			Title: "video", Status: 2, Visibility: "public", Liked: true, Favorited: false,
		},
		UpdatedAt: now,
		History: &domainlibrary.HistoryCandidate{
			VideoID: 1001, UpdatedAt: now, LastScene: "timeline", LastEventType: "progress",
			LastPositionMs: 20_000, LastWatchMs: 18_000,
		},
	}}}
	h := server.New(server.WithDisablePrintRoute(true))
	handler := &Handler{}
	h.GET("/history", func(_ context.Context, c *app.RequestContext) {
		handler.writePage(c, page, nil)
	})

	response := ut.PerformRequest(h.Engine, http.MethodGet, "/history", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}
	var body struct {
		Items []struct {
			Video struct {
				ID              int64  `json:"id"`
				AuthorNickname  string `json:"author_nickname"`
				AuthorAvatarURL string `json:"author_avatar_url"`
				Liked           bool   `json:"liked"`
				Favorited       bool   `json:"favorited"`
				Title           string `json:"title"`
			} `json:"video"`
			History historyMetadataResponse `json:"history"`
		} `json:"items"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Items) != 1 || body.Items[0].History.LastPositionMs != 20_000 || body.Items[0].History.EffectiveWatchMs != 18_000 {
		t.Fatalf("unexpected history metadata: %+v", body.Items)
	}
	video := body.Items[0].Video
	if video.ID != 1001 || video.Title != "video" || video.AuthorNickname != "author" ||
		video.AuthorAvatarURL != "avatar" || !video.Liked || video.Favorited {
		t.Fatalf("unexpected additive queue fields: %+v", video)
	}
}
