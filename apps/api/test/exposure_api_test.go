package test

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	applicationexposure "github.com/shiyudesu/frux/internal/application/exposure"
	domainexposure "github.com/shiyudesu/frux/internal/domain/exposure"
	infrajwt "github.com/shiyudesu/frux/internal/infra/jwt"
	interfaceshttpapierror "github.com/shiyudesu/frux/internal/interfaces/http/apierror"
	interfaceshttpexposure "github.com/shiyudesu/frux/internal/interfaces/http/exposure"
	interfaceshttpmiddleware "github.com/shiyudesu/frux/internal/interfaces/http/middleware"

	"github.com/cloudwego/hertz/pkg/app/server"
)

type exposureAPIResponse struct {
	Event    exposureEventAPIResponse  `json:"event"`
	Exposure *exposureIndexAPIResponse `json:"exposure"`
	Replayed bool                      `json:"replayed"`
}

type exposureEventAPIResponse struct {
	ID                int64     `json:"id"`
	UserID            int64     `json:"user_id"`
	VideoID           int64     `json:"video_id"`
	Scene             string    `json:"scene"`
	RequestID         string    `json:"request_id"`
	EventType         string    `json:"event_type"`
	EventID           string    `json:"event_id"`
	PlaybackSessionID string    `json:"playback_session_id"`
	Sequence          int64     `json:"sequence"`
	OccurredAt        time.Time `json:"occurred_at"`
	PositionMs        int       `json:"position_ms"`
	WatchMs           int       `json:"watch_ms"`
	DurationMs        *int      `json:"duration_ms"`
	Completed         bool      `json:"completed"`
	CreatedAt         time.Time `json:"created_at"`
}

type exposureIndexAPIResponse struct {
	UserID         int64     `json:"user_id"`
	VideoID        int64     `json:"video_id"`
	FirstExposedAt time.Time `json:"first_exposed_at"`
	LastExposedAt  time.Time `json:"last_exposed_at"`
	ExposureCount  int       `json:"exposure_count"`
	LastScene      string    `json:"last_scene"`
}

// memoryExposureRepo 是曝光测试用内存仓储，模拟观看流水和曝光聚合索引。
type memoryExposureRepo struct {
	mu             sync.Mutex
	nextID         int64
	published      map[int64]bool
	events         []*domainexposure.ViewEvent
	eventIDs       map[string]*domainexposure.ViewEvent
	eventExposures map[string]*domainexposure.Exposure
	exposures      map[string]*domainexposure.Exposure
	histories      map[string]*domainexposure.ViewHistory
}

func newMemoryExposureRepo() *memoryExposureRepo {
	return &memoryExposureRepo{
		nextID:         1,
		published:      map[int64]bool{1001: true, 1002: true},
		events:         []*domainexposure.ViewEvent{},
		eventIDs:       map[string]*domainexposure.ViewEvent{},
		eventExposures: map[string]*domainexposure.Exposure{},
		exposures:      map[string]*domainexposure.Exposure{},
		histories:      map[string]*domainexposure.ViewHistory{},
	}
}

func (r *memoryExposureRepo) FindViewEventByIdentity(_ context.Context, userID int64, eventID string) (*domainexposure.SaveViewEventResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	existing := r.eventIDs[memoryExposureEventKey(userID, eventID)]
	if existing == nil {
		return nil, nil
	}
	return &domainexposure.SaveViewEventResult{
		Event: cloneExposureViewEvent(existing), Exposure: cloneExposure(r.eventExposures[memoryExposureEventKey(userID, eventID)]), Replayed: true,
	}, nil
}

// SaveViewEvent 模拟写入观看流水，并在 exposed 事件时维护聚合索引。
func (r *memoryExposureRepo) SaveViewEvent(ctx context.Context, event *domainexposure.ViewEvent) (*domainexposure.SaveViewEventResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.published[event.VideoID] {
		return nil, domainexposure.ErrVideoNotFound
	}
	if event.EventID != "" {
		if existing := r.eventIDs[memoryExposureEventKey(event.UserID, event.EventID)]; existing != nil {
			if !existing.SameNormalizedPayload(event) {
				return nil, domainexposure.ErrEventIDConflict
			}
			return &domainexposure.SaveViewEventResult{
				Event: cloneExposureViewEvent(existing), Exposure: cloneExposure(r.eventExposures[memoryExposureEventKey(event.UserID, event.EventID)]), Replayed: true,
			}, nil
		}
	}

	now := event.OccurredAt.Add(time.Millisecond)
	saved := cloneExposureViewEvent(event)
	saved.ID = r.nextID
	r.nextID++
	saved.CreatedAt = now
	if saved.EventID == "" {
		saved.EventID = fmt.Sprintf("legacy-%d", saved.ID)
	}
	r.events = append(r.events, cloneExposureViewEvent(saved))
	r.eventIDs[memoryExposureEventKey(saved.UserID, saved.EventID)] = cloneExposureViewEvent(saved)

	if saved.CountsAsHistory() {
		key := memoryExposureKey(saved.UserID, saved.VideoID)
		history := r.histories[key]
		if history == nil {
			history = &domainexposure.ViewHistory{
				UserID: saved.UserID, VideoID: saved.VideoID, FirstWatchedAt: saved.OccurredAt,
			}
			r.histories[key] = history
		}
		sameSession := history.LastSessionID != "" && saved.PlaybackSessionID != "" && history.LastSessionID == saved.PlaybackSessionID
		newer := false
		if sameSession {
			newer = !saved.OccurredAt.Before(history.LastOccurredAt) && saved.Sequence > history.LastSequence
		} else {
			newer = history.LastOccurredAt.IsZero() || history.LastOccurredAt.Before(saved.OccurredAt) ||
				(history.LastOccurredAt.Equal(saved.OccurredAt) && history.LastEventID < saved.EventID)
		}
		if newer {
			history.LastScene = saved.Scene
			history.LastEventType = saved.EventType
			if saved.OccurredAt.After(history.LastWatchedAt) {
				history.LastWatchedAt = saved.OccurredAt
			}
			history.LastOccurredAt = saved.OccurredAt
			history.LastEventID = saved.EventID
			history.LastSessionID = saved.PlaybackSessionID
			history.LastSequence = saved.Sequence
		}
		history.LastPositionMs = maxExposureInt(history.LastPositionMs, saved.PositionMs)
		history.LastWatchMs = maxExposureInt(history.LastWatchMs, saved.WatchMs)
		history.Completed = history.Completed || saved.Completed
		if saved.OccurredAt.Before(history.FirstWatchedAt) {
			history.FirstWatchedAt = saved.OccurredAt
		}
	}

	if !saved.CountsAsExposure() {
		r.eventExposures[memoryExposureEventKey(saved.UserID, saved.EventID)] = nil
		return &domainexposure.SaveViewEventResult{Event: cloneExposureViewEvent(saved)}, nil
	}

	key := memoryExposureKey(saved.UserID, saved.VideoID)
	exposure, exists := r.exposures[key]
	if !exists {
		exposure = &domainexposure.Exposure{
			ID:             int64(len(r.exposures) + 1),
			UserID:         saved.UserID,
			VideoID:        saved.VideoID,
			FirstExposedAt: saved.CreatedAt,
			LastExposedAt:  saved.CreatedAt,
			ExposureCount:  1,
			LastScene:      saved.Scene,
			CreatedAt:      saved.CreatedAt,
			UpdatedAt:      saved.CreatedAt,
		}
		r.exposures[key] = exposure
		r.eventExposures[memoryExposureEventKey(saved.UserID, saved.EventID)] = cloneExposure(exposure)
		return &domainexposure.SaveViewEventResult{Event: cloneExposureViewEvent(saved), Exposure: cloneExposure(exposure)}, nil
	}

	exposure.LastExposedAt = saved.CreatedAt
	exposure.ExposureCount++
	exposure.LastScene = saved.Scene
	exposure.UpdatedAt = saved.CreatedAt
	r.eventExposures[memoryExposureEventKey(saved.UserID, saved.EventID)] = cloneExposure(exposure)
	return &domainexposure.SaveViewEventResult{Event: cloneExposureViewEvent(saved), Exposure: cloneExposure(exposure)}, nil
}

func (r *memoryExposureRepo) EventCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.events)
}

func (r *memoryExposureRepo) History(userID, videoID int64) *domainexposure.ViewHistory {
	r.mu.Lock()
	defer r.mu.Unlock()
	history := r.histories[memoryExposureKey(userID, videoID)]
	if history == nil {
		return nil
	}
	cloned := *history
	return &cloned
}

// TestExposureAPIFlow 覆盖首次曝光、重复曝光聚合和普通观看事件。
func TestExposureAPIFlow(t *testing.T) {
	router, jwtManager, repo := newExposureRouter(t)
	token := signTestToken(t, jwtManager, 42)

	firstResponse := performJSONRequest(
		router,
		http.MethodPost,
		"/api/video-view-events",
		`{"video_id":1001,"scene":"timeline","request_id":"req-1","event_type":"exposed","watch_ms":0,"completed":false}`,
		token,
	)
	requireStatus(t, firstResponse, http.StatusCreated)

	var first exposureAPIResponse
	decodeJSON(t, firstResponse, &first)
	if first.Event.ID == 0 || first.Event.UserID != 42 || first.Event.Scene != "timeline" || first.Event.EventType != domainexposure.EventTypeExposed {
		t.Fatalf("unexpected first exposure response: %+v", first)
	}
	if first.Exposure == nil || first.Exposure.ExposureCount != 1 || first.Exposure.LastScene != "timeline" {
		t.Fatalf("unexpected first exposure index: %+v", first.Exposure)
	}

	secondResponse := performJSONRequest(
		router,
		http.MethodPost,
		"/api/video-view-events",
		`{"video_id":1001,"scene":"hot","request_id":"req-2","event_type":"exposed","watch_ms":800,"completed":false}`,
		token,
	)
	requireStatus(t, secondResponse, http.StatusCreated)

	var second exposureAPIResponse
	decodeJSON(t, secondResponse, &second)
	if second.Exposure == nil || second.Exposure.ExposureCount != 2 || second.Exposure.LastScene != "hot" {
		t.Fatalf("unexpected repeated exposure index: %+v", second.Exposure)
	}
	if !second.Exposure.FirstExposedAt.Equal(first.Exposure.FirstExposedAt) {
		t.Fatalf("first exposed time changed: first=%s second=%s", first.Exposure.FirstExposedAt, second.Exposure.FirstExposedAt)
	}

	completeResponse := performJSONRequest(
		router,
		http.MethodPost,
		"/api/video-view-events",
		`{"video_id":1001,"scene":"recommend","request_id":"req-3","event_type":"complete","watch_ms":12000,"completed":false}`,
		token,
	)
	requireStatus(t, completeResponse, http.StatusCreated)

	var completed exposureAPIResponse
	decodeJSON(t, completeResponse, &completed)
	if completed.Exposure != nil || !completed.Event.Completed || completed.Event.EventType != domainexposure.EventTypeComplete {
		t.Fatalf("unexpected complete response: %+v", completed)
	}
	if repo.EventCount() != 3 {
		t.Fatalf("unexpected event count: %d", repo.EventCount())
	}
}

func TestExposureLifecycleReplayConflictAndOrdering(t *testing.T) {
	current := time.Now().UTC().Truncate(time.Millisecond)
	router, jwtManager, repo := newExposureRouterWithNow(t, func() time.Time { return current })
	token := signTestToken(t, jwtManager, 42)
	base := current

	progressBody := fmt.Sprintf(
		`{"video_id":1001,"scene":"recommend","request_id":"req-profile","event_type":"progress","event_id":"event-progress-2","playback_session_id":"session-1","sequence":2,"occurred_at":%q,"position_ms":20000,"watch_ms":18000,"duration_ms":60000}`,
		base.Add(2*time.Second).Format(time.RFC3339Nano),
	)
	response := performJSONRequest(
		router,
		http.MethodPost,
		"/api/video-view-events",
		progressBody,
		token,
	)
	requireStatus(t, response, http.StatusCreated)

	var progress exposureAPIResponse
	decodeJSON(t, response, &progress)
	if progress.Event.EventID != "event-progress-2" || progress.Event.PositionMs != 20000 || progress.Event.WatchMs != 18000 {
		t.Fatalf("unexpected progress response: %+v", progress)
	}

	current = current.Add(48 * time.Hour)
	replay := performJSONRequest(router, http.MethodPost, "/api/video-view-events", progressBody, token)
	requireStatus(t, replay, http.StatusOK)
	var replayed exposureAPIResponse
	decodeJSON(t, replay, &replayed)
	if !replayed.Replayed || replayed.Event.ID != progress.Event.ID || repo.EventCount() != 1 {
		t.Fatalf("retry was not replayed: %+v count=%d", replayed, repo.EventCount())
	}

	conflictBody := fmt.Sprintf(
		`{"video_id":1001,"scene":"recommend","event_type":"progress","event_id":"event-progress-2","playback_session_id":"session-1","sequence":2,"occurred_at":%q,"position_ms":21000,"watch_ms":18000,"duration_ms":60000}`,
		base.Add(2*time.Second).Format(time.RFC3339Nano),
	)
	conflict := performJSONRequest(router, http.MethodPost, "/api/video-view-events", conflictBody, token)
	assertAPIError(t, conflict, http.StatusConflict, interfaceshttpapierror.CodeExposureEventConflict, domainexposure.ErrEventIDConflict.Error())
	current = base

	completeBody := fmt.Sprintf(
		`{"video_id":1001,"scene":"recommend","event_type":"complete","event_id":"event-complete-3","playback_session_id":"session-1","sequence":3,"occurred_at":%q,"position_ms":59000,"watch_ms":55000,"duration_ms":60000}`,
		base.Add(3*time.Second).Format(time.RFC3339Nano),
	)
	requireStatus(t, performJSONRequest(router, http.MethodPost, "/api/video-view-events", completeBody, token), http.StatusCreated)

	delayedBody := fmt.Sprintf(
		`{"video_id":1001,"scene":"recommend","event_type":"progress","event_id":"event-progress-1","playback_session_id":"session-1","sequence":1,"occurred_at":%q,"position_ms":10000,"watch_ms":9000,"duration_ms":60000}`,
		base.Add(time.Second).Format(time.RFC3339Nano),
	)
	requireStatus(t, performJSONRequest(router, http.MethodPost, "/api/video-view-events", delayedBody, token), http.StatusCreated)
	history := repo.History(42, 1001)
	if history == nil || history.LastEventID != "event-complete-3" || history.LastPositionMs != 59000 || !history.Completed {
		t.Fatalf("delayed event regressed history: %+v", history)
	}

	missingVideoResponse := performJSONRequest(
		router,
		http.MethodPost,
		"/api/video-view-events",
		fmt.Sprintf(`{"video_id":404,"scene":"recommend","event_type":"play","event_id":"missing-video","playback_session_id":"session-2","sequence":1,"occurred_at":%q}`, base.Format(time.RFC3339Nano)),
		token,
	)
	assertAPIError(t, missingVideoResponse, http.StatusNotFound, interfaceshttpapierror.CodeExposureVideoNotFound, "video not found")
}

func TestExposureReplayReturnsOriginalSnapshot(t *testing.T) {
	current := time.Now().UTC().Truncate(time.Millisecond)
	router, jwtManager, _ := newExposureRouterWithNow(t, func() time.Time { return current })
	token := signTestToken(t, jwtManager, 42)

	firstBody := fmt.Sprintf(
		`{"video_id":1001,"scene":"timeline","event_type":"exposed","event_id":"exposure-first","playback_session_id":"exposure-session-1","sequence":1,"occurred_at":%q}`,
		current.Format(time.RFC3339Nano),
	)
	firstResponse := performJSONRequest(router, http.MethodPost, "/api/video-view-events", firstBody, token)
	requireStatus(t, firstResponse, http.StatusCreated)
	var first exposureAPIResponse
	decodeJSON(t, firstResponse, &first)

	current = current.Add(time.Second)
	secondBody := fmt.Sprintf(
		`{"video_id":1001,"scene":"hot","event_type":"exposed","event_id":"exposure-second","playback_session_id":"exposure-session-2","sequence":1,"occurred_at":%q}`,
		current.Format(time.RFC3339Nano),
	)
	requireStatus(t, performJSONRequest(router, http.MethodPost, "/api/video-view-events", secondBody, token), http.StatusCreated)

	replay := performJSONRequest(router, http.MethodPost, "/api/video-view-events", firstBody, token)
	requireStatus(t, replay, http.StatusOK)
	var replayed exposureAPIResponse
	decodeJSON(t, replay, &replayed)
	if !replayed.Replayed || replayed.Exposure == nil || first.Exposure == nil ||
		replayed.Exposure.ExposureCount != first.Exposure.ExposureCount ||
		replayed.Exposure.LastScene != first.Exposure.LastScene ||
		!replayed.Exposure.LastExposedAt.Equal(first.Exposure.LastExposedAt) {
		t.Fatalf("replay did not preserve original exposure snapshot: first=%+v replay=%+v", first, replayed)
	}
}

// TestExposureAPIValidation 覆盖鉴权、参数错误和视频可见性校验。
func TestExposureAPIValidation(t *testing.T) {
	router, jwtManager, _ := newExposureRouter(t)
	token := signTestToken(t, jwtManager, 42)

	unauthorizedResponse := performJSONRequest(
		router,
		http.MethodPost,
		"/api/video-view-events",
		`{"video_id":1001,"scene":"timeline","event_type":"exposed"}`,
		"",
	)
	assertAPIError(t, unauthorizedResponse, http.StatusUnauthorized, interfaceshttpapierror.CodeInvalidAccessToken, "invalid access token")

	emptySceneResponse := performJSONRequest(
		router,
		http.MethodPost,
		"/api/video-view-events",
		`{"video_id":1001,"scene":" ","event_type":"exposed"}`,
		token,
	)
	assertAPIError(t, emptySceneResponse, http.StatusBadRequest, interfaceshttpapierror.CodeExposureValidationFailed, domainexposure.ErrEmptyScene.Error())

	badEventResponse := performJSONRequest(
		router,
		http.MethodPost,
		"/api/video-view-events",
		`{"video_id":1001,"scene":"timeline","event_type":"progress"}`,
		token,
	)
	requireStatus(t, badEventResponse, http.StatusBadRequest)

	negativeWatchResponse := performJSONRequest(
		router,
		http.MethodPost,
		"/api/video-view-events",
		`{"video_id":1001,"scene":"timeline","event_type":"play","watch_ms":-1}`,
		token,
	)
	requireStatus(t, negativeWatchResponse, http.StatusBadRequest)

	outOfRangeOccurrence := performJSONRequest(
		router,
		http.MethodPost,
		"/api/video-view-events",
		fmt.Sprintf(
			`{"video_id":1001,"scene":"timeline","event_type":"play","event_id":"old-event","playback_session_id":"session-old","sequence":1,"occurred_at":%q}`,
			time.Now().UTC().Add(-25*time.Hour).Format(time.RFC3339Nano),
		),
		token,
	)
	requireStatus(t, outOfRangeOccurrence, http.StatusBadRequest)

	invalidDuration := performJSONRequest(
		router,
		http.MethodPost,
		"/api/video-view-events",
		fmt.Sprintf(
			`{"video_id":1001,"scene":"timeline","event_type":"progress","event_id":"bad-duration","playback_session_id":"session-duration","sequence":2,"occurred_at":%q,"position_ms":2000,"watch_ms":1000,"duration_ms":1000}`,
			time.Now().UTC().Format(time.RFC3339Nano),
		),
		token,
	)
	requireStatus(t, invalidDuration, http.StatusBadRequest)

	missingVideoResponse := performJSONRequest(
		router,
		http.MethodPost,
		"/api/video-view-events",
		`{"video_id":404,"scene":"timeline","event_type":"exposed"}`,
		token,
	)
	assertAPIError(t, missingVideoResponse, http.StatusNotFound, interfaceshttpapierror.CodeExposureVideoNotFound, "video not found")
}

func newExposureRouter(t *testing.T) (*server.Hertz, *infrajwt.Manager, *memoryExposureRepo) {
	return newExposureRouterWithNow(t, nil)
}

func newExposureRouterWithNow(t *testing.T, now func() time.Time) (*server.Hertz, *infrajwt.Manager, *memoryExposureRepo) {
	t.Helper()

	router := server.New()

	jwtManager, err := infrajwt.NewManager("test-secret", "15m")
	if err != nil {
		t.Fatalf("new jwt manager: %v", err)
	}

	repo := newMemoryExposureRepo()
	options := []applicationexposure.Option{}
	if now != nil {
		options = append(options, applicationexposure.WithNow(now))
	}
	service := applicationexposure.New(repo, options...)
	handler := interfaceshttpexposure.New(service)
	authMiddleware := interfaceshttpmiddleware.NewJWTAuth(jwtManager)

	api := router.Group("/api")
	api.POST("/video-view-events", authMiddleware, handler.CreateViewEvent)

	return router, jwtManager, repo
}

func cloneExposureViewEvent(event *domainexposure.ViewEvent) *domainexposure.ViewEvent {
	cloned := *event
	return &cloned
}

func cloneExposure(exposure *domainexposure.Exposure) *domainexposure.Exposure {
	if exposure == nil {
		return nil
	}
	cloned := *exposure
	return &cloned
}

func memoryExposureKey(userID int64, videoID int64) string {
	return fmt.Sprintf("%d:%d", userID, videoID)
}

func memoryExposureEventKey(userID int64, eventID string) string {
	return fmt.Sprintf("%d:%s", userID, eventID)
}

func maxExposureInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}
