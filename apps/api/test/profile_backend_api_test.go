package test

import (
	"context"
	"encoding/json"
	"fmt"
	applicationlibrary "github.com/shiyudesu/frux/internal/application/library"
	applicationvideo "github.com/shiyudesu/frux/internal/application/video"
	domainlibrary "github.com/shiyudesu/frux/internal/domain/library"
	domainvideo "github.com/shiyudesu/frux/internal/domain/video"
	infrajwt "github.com/shiyudesu/frux/internal/infra/jwt"
	interfaceshttpapierror "github.com/shiyudesu/frux/internal/interfaces/http/apierror"
	interfaceshttplibrary "github.com/shiyudesu/frux/internal/interfaces/http/library"
	interfaceshttpmiddleware "github.com/shiyudesu/frux/internal/interfaces/http/middleware"
	interfaceshttpvideo "github.com/shiyudesu/frux/internal/interfaces/http/video"
	"net/http"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
)

type managementMemoryRepo struct {
	mu         sync.Mutex
	videos     map[int64]*domainvideo.Video
	operations map[string]*domainvideo.BatchOperation
}

func newManagementMemoryRepo() *managementMemoryRepo {
	now := time.Now().UTC()
	return &managementMemoryRepo{
		videos: map[int64]*domainvideo.Video{
			1: domainvideo.RestoreVideoWithVisibility(1, 42, "public work", "", "media-1", "cover-1", domainvideo.StatusPublished, domainvideo.VisibilityPublic, 1, 0, 0, &now, now.Add(-time.Minute), now, ""),
			2: domainvideo.RestoreVideoWithVisibility(2, 42, "private work", "", "media-2", "cover-2", domainvideo.StatusPublished, domainvideo.VisibilityPrivate, 2, 0, 0, &now, now, now, ""),
			3: domainvideo.RestoreVideoWithVisibility(3, 77, "other work", "", "media-3", "cover-3", domainvideo.StatusPublished, domainvideo.VisibilityPublic, 0, 0, 0, &now, now, now, ""),
			4: domainvideo.RestoreVideoWithVisibility(4, 42, "pending work", "", "media-4", "cover-4", domainvideo.StatusPendingReview, domainvideo.VisibilityPublic, 0, 0, 0, nil, now.Add(time.Minute), now, ""),
			5: domainvideo.RestoreVideoWithVisibility(5, 42, "rejected work", "", "media-5", "cover-5", domainvideo.StatusRejected, domainvideo.VisibilityPublic, 0, 0, 0, nil, now.Add(2*time.Minute), now, ""),
		},
		operations: map[string]*domainvideo.BatchOperation{},
	}
}

func (r *managementMemoryRepo) QueryCreatorVideos(_ context.Context, filter domainvideo.CreatorVideoFilter) ([]*domainvideo.Video, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]*domainvideo.Video, 0)
	for _, video := range r.videos {
		if video.AuthorID != filter.AuthorID || video.Status == domainvideo.StatusDeleted {
			continue
		}
		if filter.VideoID > 0 && video.ID != filter.VideoID {
			continue
		}
		if filter.Visibility != "" && video.Visibility != filter.Visibility {
			continue
		}
		if len(filter.Statuses) > 0 && !containsVideoStatus(filter.Statuses, video.Status) {
			continue
		}
		items = append(items, cloneVideo(video))
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID > items[j].ID
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	if len(items) > filter.Limit {
		items = items[:filter.Limit]
	}
	return items, nil
}

func (r *managementMemoryRepo) ApplyBatch(_ context.Context, userID int64, action string, videoIDs []int64, key, fingerprint string) (*domainvideo.BatchOperation, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	operationKey := fmt.Sprintf("%d:%s", userID, key)
	if existing := r.operations[operationKey]; existing != nil {
		if existing.Fingerprint != fingerprint {
			return nil, false, domainvideo.ErrBatchIdempotencyConflict
		}
		cloned := *existing
		cloned.VideoIDs = append([]int64(nil), existing.VideoIDs...)
		return &cloned, true, nil
	}
	for _, id := range videoIDs {
		video := r.videos[id]
		if video == nil {
			return nil, false, domainvideo.ErrVideoNotFound
		}
		if video.AuthorID != userID {
			return nil, false, domainvideo.ErrVideoPermissionDenied
		}
	}
	for _, id := range videoIDs {
		video := r.videos[id]
		switch action {
		case domainvideo.BatchActionMakePublic:
			video.Visibility = domainvideo.VisibilityPublic
		case domainvideo.BatchActionMakePrivate:
			video.Visibility = domainvideo.VisibilityPrivate
		case domainvideo.BatchActionDelete:
			video.Status = domainvideo.StatusDeleted
		}
	}
	operation := &domainvideo.BatchOperation{UserID: userID, Key: key, Fingerprint: fingerprint, Action: action, VideoIDs: append([]int64(nil), videoIDs...)}
	r.operations[operationKey] = operation
	return operation, false, nil
}

func (r *managementMemoryRepo) BatchGetReadable(_ context.Context, viewerID int64, ids []int64, publicOnly bool) (map[int64]*domainvideo.Video, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := map[int64]*domainvideo.Video{}
	for _, id := range ids {
		video := r.videos[id]
		if video == nil || video.Status != domainvideo.StatusPublished {
			continue
		}
		if publicOnly && video.Visibility != domainvideo.VisibilityPublic {
			continue
		}
		if !publicOnly && video.Visibility == domainvideo.VisibilityPrivate && video.AuthorID != viewerID {
			continue
		}
		result[id] = cloneVideo(video)
	}
	return result, nil
}

func (r *managementMemoryRepo) ListAssetReferences(_ context.Context, assetURL string) ([]domainvideo.AssetReference, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	references := make([]domainvideo.AssetReference, 0)
	for _, video := range r.videos {
		if video.MediaURL == assetURL || video.CoverURL == assetURL {
			references = append(references, domainvideo.AssetReference{
				AuthorID: video.AuthorID, Status: video.Status, Visibility: video.Visibility,
			})
		}
	}
	return references, nil
}

func (r *managementMemoryRepo) CreateLocalAsset(_ context.Context, asset *domainvideo.LocalAsset) error {
	return nil
}

func (r *managementMemoryRepo) FindLocalAsset(_ context.Context, assetURL string) (*domainvideo.LocalAsset, error) {
	return nil, domainvideo.ErrLocalAssetNotFound
}

func containsVideoStatus(statuses []int, target int) bool {
	for _, status := range statuses {
		if status == target {
			return true
		}
	}
	return false
}

func TestCreatorManagementAPIFlow(t *testing.T) {
	router := server.New()
	jwtManager, _ := infrajwt.NewManager("test-secret", "15m")
	auth := interfaceshttpmiddleware.NewJWTAuth(jwtManager)
	repo := newManagementMemoryRepo()
	baseService := applicationvideo.New(newMemoryVideoRepo())
	handler := interfaceshttpvideo.New(baseService, applicationvideo.NewManagement(repo, nil))
	users := router.Group("/api/users")
	users.POST("/me/video-queries", auth, handler.QueryMine)
	users.POST("/me/video-batch-actions", auth, handler.BatchAction)
	token := signTestToken(t, jwtManager, 42)

	query := performJSONRequest(router, http.MethodPost, "/api/users/me/video-queries", `{"visibility":"private","limit":20}`, token)
	requireStatus(t, query, http.StatusOK)
	var queryPayload struct {
		Items []videoAPIResponse `json:"items"`
	}
	decodeJSON(t, query, &queryPayload)
	if len(queryPayload.Items) != 1 || queryPayload.Items[0].ID != 2 {
		t.Fatalf("unexpected private query: %s", query.Body.String())
	}
	targetQuery := performJSONRequest(
		router, http.MethodPost, "/api/users/me/video-queries",
		`{"video_id":2,"visibility":"private","limit":1}`, token,
	)
	requireStatus(t, targetQuery, http.StatusOK)
	decodeJSON(t, targetQuery, &queryPayload)
	if len(queryPayload.Items) != 1 || queryPayload.Items[0].ID != 2 {
		t.Fatalf("unexpected target query: %s", targetQuery.Body.String())
	}
	pendingQuery := performJSONRequest(
		router,
		http.MethodPost,
		"/api/users/me/video-queries",
		`{"visibility":"public","statuses":[5,6],"limit":20}`,
		token,
	)
	requireStatus(t, pendingQuery, http.StatusOK)
	decodeJSON(t, pendingQuery, &queryPayload)
	if len(queryPayload.Items) != 2 ||
		queryPayload.Items[0].Status != domainvideo.StatusRejected ||
		queryPayload.Items[1].Status != domainvideo.StatusPendingReview {
		t.Fatalf("unexpected review lifecycle query: %s", pendingQuery.Body.String())
	}

	mixed := performJSONRequestWithHeaders(router, http.MethodPost, "/api/users/me/video-batch-actions", `{"video_ids":[1,3],"action":"make_private"}`,
		utHeader("Authorization", "Bearer "+token), utHeader("Idempotency-Key", "mixed"))
	requireStatus(t, mixed, http.StatusForbidden)
	if repo.videos[1].Visibility != domainvideo.VisibilityPublic {
		t.Fatal("mixed ownership batch partially applied")
	}

	success := performJSONRequestWithHeaders(router, http.MethodPost, "/api/users/me/video-batch-actions", `{"video_ids":[1],"action":"make_private"}`,
		utHeader("Authorization", "Bearer "+token), utHeader("Idempotency-Key", "privacy"))
	requireStatus(t, success, http.StatusOK)
	replay := performJSONRequestWithHeaders(router, http.MethodPost, "/api/users/me/video-batch-actions", `{"video_ids":[1],"action":"make_private"}`,
		utHeader("Authorization", "Bearer "+token), utHeader("Idempotency-Key", "privacy"))
	requireStatus(t, replay, http.StatusOK)
	var replayPayload map[string]json.RawMessage
	decodeJSON(t, replay, &replayPayload)
	var replayed bool
	_ = json.Unmarshal(replayPayload["replayed"], &replayed)
	if !replayed {
		t.Fatal("expected batch replay marker")
	}
	conflict := performJSONRequestWithHeaders(router, http.MethodPost, "/api/users/me/video-batch-actions", `{"video_ids":[2],"action":"delete"}`,
		utHeader("Authorization", "Bearer "+token), utHeader("Idempotency-Key", "privacy"))
	requireStatus(t, conflict, http.StatusConflict)

}

type librarySource struct {
	items         []domainlibrary.VideoCandidate
	cards         map[int64]*domainlibrary.VideoCard
	authors       map[int64]*domainlibrary.AuthorDisplay
	viewerActions map[int64]*domainlibrary.ViewerActionState
	public        bool
	watch         map[int64]*domainlibrary.WatchLater
}

func (s *librarySource) ListActionVideos(context.Context, int64, string, *domainlibrary.Cursor, int) ([]domainlibrary.VideoCandidate, error) {
	return append([]domainlibrary.VideoCandidate(nil), s.items...), nil
}
func (s *librarySource) ListHistoryVideos(context.Context, int64, *domainlibrary.Cursor, int) ([]domainlibrary.HistoryCandidate, error) {
	return nil, nil
}
func (s *librarySource) DeleteHistory(context.Context, int64, int64) error { return nil }
func (s *librarySource) ClearHistory(context.Context, int64) error         { return nil }
func (s *librarySource) SetWatchLater(_ context.Context, fact *domainlibrary.WatchLater) (*domainlibrary.WatchLater, error) {
	fact.UpdatedAt = time.Now().UTC()
	s.watch[fact.VideoID] = fact
	return fact, nil
}
func (s *librarySource) ListWatchLater(context.Context, int64, *domainlibrary.Cursor, int) ([]domainlibrary.VideoCandidate, error) {
	return nil, nil
}
func (s *librarySource) BatchGetReadable(_ context.Context, _ int64, ids []int64, publicOnly bool) (map[int64]*domainlibrary.VideoCard, error) {
	result := map[int64]*domainlibrary.VideoCard{}
	for _, id := range ids {
		card := s.cards[id]
		if card != nil && (!publicOnly || card.Visibility == domainvideo.VisibilityPublic) {
			result[id] = card
		}
	}
	return result, nil
}
func (s *librarySource) LikedVideosPublic(context.Context, int64) (bool, error) { return s.public, nil }
func (s *librarySource) BatchGetAuthorDisplays(_ context.Context, authorIDs []int64) (map[int64]*domainlibrary.AuthorDisplay, error) {
	result := map[int64]*domainlibrary.AuthorDisplay{}
	for _, authorID := range authorIDs {
		if display := s.authors[authorID]; display != nil {
			result[authorID] = display
		}
	}
	return result, nil
}
func (s *librarySource) BatchGetViewerActionStates(_ context.Context, _ int64, videoIDs []int64) (map[int64]*domainlibrary.ViewerActionState, error) {
	result := map[int64]*domainlibrary.ViewerActionState{}
	for _, videoID := range videoIDs {
		if state := s.viewerActions[videoID]; state != nil {
			result[videoID] = state
		}
	}
	return result, nil
}

func TestLibraryAPIAuthenticationAndPrivacy(t *testing.T) {
	router := server.New()
	jwtManager, _ := infrajwt.NewManager("test-secret", "15m")
	auth := interfaceshttpmiddleware.NewJWTAuth(jwtManager)
	source := &librarySource{
		items: []domainlibrary.VideoCandidate{{VideoID: 1, UpdatedAt: time.Now().UTC()}},
		cards: map[int64]*domainlibrary.VideoCard{
			1: {ID: 1, AuthorID: 42, Visibility: domainvideo.VisibilityPublic},
		},
		authors: map[int64]*domainlibrary.AuthorDisplay{
			42: {AuthorID: 42, Nickname: "library author", AvatarURL: "library avatar"},
		},
		viewerActions: map[int64]*domainlibrary.ViewerActionState{
			1: {VideoID: 1, Liked: true, Favorited: true},
		},
		watch: map[int64]*domainlibrary.WatchLater{},
	}
	handler := interfaceshttplibrary.New(applicationlibrary.New(source, source, source, source, source, source, source))
	users := router.Group("/api/users")
	users.GET("/me/liked-videos", auth, handler.ListLiked)
	users.GET("/:userId/liked-videos", handler.ListPublicLiked)
	videos := router.Group("/api/videos")
	videos.PUT("/:videoId/watch-later", auth, handler.AddWatchLater)

	unauthorized := performJSONRequest(router, http.MethodGet, "/api/users/me/liked-videos", "", "")
	assertAPIError(t, unauthorized, http.StatusUnauthorized, interfaceshttpapierror.CodeInvalidAccessToken, "invalid access token")
	private := performJSONRequest(router, http.MethodGet, "/api/users/42/liked-videos", "", "")
	assertAPIError(t, private, http.StatusForbidden, interfaceshttpapierror.CodeLibraryLikedVideosPrivate, "liked videos are private")
	source.public = true
	public := performJSONRequest(router, http.MethodGet, "/api/users/42/liked-videos", "", "")
	requireStatus(t, public, http.StatusOK)
	token := signTestToken(t, jwtManager, 42)
	personal := performJSONRequest(router, http.MethodGet, "/api/users/me/liked-videos", "", token)
	requireStatus(t, personal, http.StatusOK)
	var personalPage struct {
		Items []struct {
			Video struct {
				AuthorNickname  string `json:"author_nickname"`
				AuthorAvatarURL string `json:"author_avatar_url"`
				Liked           bool   `json:"liked"`
				Favorited       bool   `json:"favorited"`
			} `json:"video"`
		} `json:"items"`
	}
	decodeJSON(t, personal, &personalPage)
	if len(personalPage.Items) != 1 || personalPage.Items[0].Video.AuthorNickname != "library author" ||
		personalPage.Items[0].Video.AuthorAvatarURL != "library avatar" ||
		!personalPage.Items[0].Video.Liked || !personalPage.Items[0].Video.Favorited {
		t.Fatalf("unexpected queue-ready library response: %s", personal.Body.String())
	}
	watch := performJSONRequest(router, http.MethodPut, "/api/videos/1/watch-later", "", token)
	requireStatus(t, watch, http.StatusOK)
}

func utHeader(key, value string) ut.Header {
	return ut.Header{Key: key, Value: value}
}
