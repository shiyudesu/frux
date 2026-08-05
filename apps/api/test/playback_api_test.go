package test

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	applicationplayback "github.com/shiyudesu/frux/internal/application/playback"
	domainplayback "github.com/shiyudesu/frux/internal/domain/playback"
	infrajwt "github.com/shiyudesu/frux/internal/infra/jwt"
	interfaceshttpmiddleware "github.com/shiyudesu/frux/internal/interfaces/http/middleware"
	interfaceshttpplayback "github.com/shiyudesu/frux/internal/interfaces/http/playback"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
)

type playbackConfigAPIResponse struct {
	ID           int64     `json:"id"`
	Platform     string    `json:"platform"`
	NetworkType  string    `json:"network_type"`
	PreloadCount int       `json:"preload_count"`
	BufferMs     int       `json:"buffer_ms"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type preloadVideoAPIResponse struct {
	VideoID  int64  `json:"video_id"`
	MediaURL string `json:"media_url"`
	CoverURL string `json:"cover_url"`
}

type preloadVideosAPIResponse struct {
	Items []preloadVideoAPIResponse `json:"items"`
}

type qosReportAPIResponse struct {
	ID           int64     `json:"id"`
	UserID       int64     `json:"user_id"`
	VideoID      int64     `json:"video_id"`
	FirstFrameMs *int      `json:"first_frame_ms"`
	StutterCount int       `json:"stutter_count"`
	WatchMs      int       `json:"watch_ms"`
	CreatedAt    time.Time `json:"created_at"`
}

type memoryPlaybackRepo struct {
	mu               sync.Mutex
	nextID           int64
	configs          map[memoryPlaybackConfigKey]*domainplayback.Config
	videos           []*memoryPlaybackVideo
	reports          map[int64]*domainplayback.QoSReport
	byRequest        map[memoryPlaybackReportKey]int64
	telemetryBatches map[memoryPlaybackReportKey]*domainplayback.TelemetryBatchWriteResult
	telemetryEvents  map[memoryPlaybackReportKey]struct{}
}

type memoryPlaybackConfigKey struct {
	Platform    string
	NetworkType string
}

type memoryPlaybackReportKey struct {
	UserID int64
	Key    string
}

type memoryPlaybackVideo struct {
	VideoID     int64
	MediaURL    string
	CoverURL    string
	PublishedAt time.Time
}

func newMemoryPlaybackRepo() *memoryPlaybackRepo {
	now := time.Now().UTC()
	return &memoryPlaybackRepo{
		nextID: 1,
		configs: map[memoryPlaybackConfigKey]*domainplayback.Config{
			{Platform: "Web", NetworkType: "WiFi"}:                            domainplayback.RestoreConfig(7, "Web", "WiFi", 4, 900, now),
			{Platform: "Android", NetworkType: domainplayback.NetworkDefault}: domainplayback.RestoreConfig(8, "Android", domainplayback.NetworkDefault, 2, 1500, now),
		},
		videos: []*memoryPlaybackVideo{
			{VideoID: 103, MediaURL: "/uploads/103.mp4", CoverURL: "/uploads/103.jpg", PublishedAt: now.Add(3 * time.Minute)},
			{VideoID: 102, MediaURL: "/uploads/102.mp4", CoverURL: "/uploads/102.jpg", PublishedAt: now.Add(2 * time.Minute)},
			{VideoID: 101, MediaURL: "/uploads/101.mp4", CoverURL: "/uploads/101.jpg", PublishedAt: now.Add(time.Minute)},
			{VideoID: 100, MediaURL: "/uploads/100.mp4", CoverURL: "/uploads/100.jpg", PublishedAt: now},
		},
		reports:          map[int64]*domainplayback.QoSReport{},
		byRequest:        map[memoryPlaybackReportKey]int64{},
		telemetryBatches: map[memoryPlaybackReportKey]*domainplayback.TelemetryBatchWriteResult{},
		telemetryEvents:  map[memoryPlaybackReportKey]struct{}{},
	}
}

func (r *memoryPlaybackRepo) FindConfig(ctx context.Context, platform string, networkType string) (*domainplayback.Config, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	config := r.configs[memoryPlaybackConfigKey{Platform: platform, NetworkType: networkType}]
	if config == nil {
		return nil, nil
	}
	cloned := *config
	return &cloned, nil
}

func (r *memoryPlaybackRepo) ListPreloadVideos(ctx context.Context, currentVideoID int64, limit int) ([]*domainplayback.PreloadVideo, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	videos := make([]*memoryPlaybackVideo, 0, len(r.videos))
	videos = append(videos, r.videos...)
	sort.Slice(videos, func(i, j int) bool {
		if videos[i].PublishedAt.Equal(videos[j].PublishedAt) {
			return videos[i].VideoID > videos[j].VideoID
		}
		return videos[i].PublishedAt.After(videos[j].PublishedAt)
	})

	var current *memoryPlaybackVideo
	for _, video := range videos {
		if video.VideoID == currentVideoID {
			current = video
			break
		}
	}

	items := make([]*domainplayback.PreloadVideo, 0, limit)
	for _, video := range videos {
		if current != nil && (video.PublishedAt.After(current.PublishedAt) || video.PublishedAt.Equal(current.PublishedAt) && video.VideoID >= current.VideoID) {
			continue
		}
		items = append(items, domainplayback.RestorePreloadVideo(video.VideoID, video.MediaURL, video.CoverURL))
		if len(items) >= limit {
			break
		}
	}
	return items, nil
}

func (r *memoryPlaybackRepo) CreateQoSReport(ctx context.Context, report *domainplayback.QoSReport) (*domainplayback.QoSReport, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if report.IdempotencyKey != "" {
		if id, exists := r.byRequest[memoryPlaybackReportKey{UserID: report.UserID, Key: report.IdempotencyKey}]; exists {
			return cloneQoSReport(r.reports[id]), false, nil
		}
	}

	created := cloneQoSReport(report)
	created.ID = r.nextID
	r.nextID++
	if created.CreatedAt.IsZero() {
		created.CreatedAt = time.Now().UTC().Add(time.Duration(created.ID) * time.Millisecond)
	}
	r.reports[created.ID] = created
	if created.IdempotencyKey != "" {
		r.byRequest[memoryPlaybackReportKey{UserID: created.UserID, Key: created.IdempotencyKey}] = created.ID
	}
	return cloneQoSReport(created), true, nil
}

func cloneQoSReport(report *domainplayback.QoSReport) *domainplayback.QoSReport {
	cloned := *report
	if report.FirstFrameMs != nil {
		firstFrameMs := *report.FirstFrameMs
		cloned.FirstFrameMs = &firstFrameMs
	}
	return &cloned
}

func (r *memoryPlaybackRepo) CreateTelemetryBatch(_ context.Context, batch *domainplayback.TelemetryBatch) (*domainplayback.TelemetryBatchWriteResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	batchKey := memoryPlaybackReportKey{UserID: batch.UserID, Key: batch.BatchID}
	if existing := r.telemetryBatches[batchKey]; existing != nil {
		cloned := *existing
		cloned.Created = false
		cloned.AcceptedEventIDs = nil
		cloned.DuplicateEventIDs = make([]string, 0, len(batch.Events))
		for _, event := range batch.Events {
			cloned.DuplicateEventIDs = append(cloned.DuplicateEventIDs, event.EventID)
		}
		return &cloned, nil
	}
	acceptedCount := 0
	duplicateCount := 0
	acceptedEventIDs := make([]string, 0, len(batch.Events))
	duplicateEventIDs := make([]string, 0, len(batch.Events))
	for _, event := range batch.Events {
		eventKey := memoryPlaybackReportKey{UserID: batch.UserID, Key: event.EventID}
		if _, exists := r.telemetryEvents[eventKey]; exists {
			duplicateCount++
			duplicateEventIDs = append(duplicateEventIDs, event.EventID)
			continue
		}
		r.telemetryEvents[eventKey] = struct{}{}
		acceptedCount++
		acceptedEventIDs = append(acceptedEventIDs, event.EventID)
	}
	result := &domainplayback.TelemetryBatchWriteResult{
		BatchID: batch.BatchID, EventCount: len(batch.Events),
		AcceptedCount: acceptedCount, DuplicateCount: duplicateCount,
		AcceptedEventIDs: acceptedEventIDs, DuplicateEventIDs: duplicateEventIDs,
		Created: true, CreatedAt: time.Now().UTC(),
	}
	r.telemetryBatches[batchKey] = result
	cloned := *result
	return &cloned, nil
}

func TestPlaybackAPIFlow(t *testing.T) {
	router, jwtManager := newPlaybackRouter(t)
	token := signTestToken(t, jwtManager, 42)

	configResponse := performJSONRequest(router, http.MethodGet, "/api/playback-config?platform=Web&network_type=WiFi", "", token)
	requireStatus(t, configResponse, http.StatusOK)

	var config playbackConfigAPIResponse
	decodeJSON(t, configResponse, &config)
	if config.ID != 7 || config.PreloadCount != 4 || config.BufferMs != 900 {
		t.Fatalf("unexpected playback config: %+v", config)
	}

	defaultConfigResponse := performJSONRequest(router, http.MethodGet, "/api/playback-config?platform=Web&network_type=4G", "", token)
	requireStatus(t, defaultConfigResponse, http.StatusOK)

	decodeJSON(t, defaultConfigResponse, &config)
	if config.ID != 0 || config.PreloadCount != domainplayback.DefaultPreloadCount || config.BufferMs != domainplayback.DefaultBufferMs {
		t.Fatalf("expected default playback config, got %+v", config)
	}

	fallbackConfigResponse := performJSONRequest(router, http.MethodGet, "/api/playback-config?platform=Android&network_type=5G", "", token)
	requireStatus(t, fallbackConfigResponse, http.StatusOK)

	decodeJSON(t, fallbackConfigResponse, &config)
	if config.ID != 8 || config.PreloadCount != 2 || config.BufferMs != 1500 {
		t.Fatalf("expected network fallback playback config, got %+v", config)
	}

	preloadResponse := performJSONRequest(router, http.MethodGet, "/api/preload-videos?current_video_id=103&limit=2", "", token)
	requireStatus(t, preloadResponse, http.StatusOK)

	var preload preloadVideosAPIResponse
	decodeJSON(t, preloadResponse, &preload)
	if len(preload.Items) != 2 || preload.Items[0].VideoID != 102 || preload.Items[1].VideoID != 101 {
		t.Fatalf("unexpected preload items: %+v", preload.Items)
	}

	refillResponse := performJSONRequest(router, http.MethodGet, "/api/preload-videos?limit=2", "", token)
	requireStatus(t, refillResponse, http.StatusOK)

	decodeJSON(t, refillResponse, &preload)
	if len(preload.Items) != 2 || preload.Items[0].VideoID != 103 || preload.Items[1].VideoID != 102 {
		t.Fatalf("unexpected compatibility refill items: %+v", preload.Items)
	}

	firstFrameMs := 186
	qosResponse := performPlaybackJSONRequest(
		router,
		http.MethodPost,
		"/api/playback-qos-reports",
		`{"video_id":103,"first_frame_ms":186,"stutter_count":1,"watch_ms":2400}`,
		token,
		"qos-1",
	)
	requireStatus(t, qosResponse, http.StatusCreated)

	var qos qosReportAPIResponse
	decodeJSON(t, qosResponse, &qos)
	if qos.UserID != 42 || qos.VideoID != 103 || qos.FirstFrameMs == nil || *qos.FirstFrameMs != firstFrameMs || qos.StutterCount != 1 {
		t.Fatalf("unexpected qos report: %+v", qos)
	}

	replayResponse := performPlaybackJSONRequest(
		router,
		http.MethodPost,
		"/api/playback-qos-reports",
		`{"video_id":103,"first_frame_ms":999,"stutter_count":9,"watch_ms":9999}`,
		token,
		"qos-1",
	)
	requireStatus(t, replayResponse, http.StatusOK)

	var replay qosReportAPIResponse
	decodeJSON(t, replayResponse, &replay)
	if replay.ID != qos.ID || replay.FirstFrameMs == nil || *replay.FirstFrameMs != firstFrameMs {
		t.Fatalf("expected idempotent qos replay, got %+v", replay)
	}

	internalResponse := performInternalPlaybackRequest(
		router,
		http.MethodPost,
		"/internal/playback-qos-reports",
		`{"user_id":77,"video_id":101,"stutter_count":0,"watch_ms":1200}`,
		testInternalToken,
		"qos-internal-1",
	)
	requireStatus(t, internalResponse, http.StatusCreated)

	telemetryBody := telemetryBatchBody(t, 1, "telemetry-batch-1", []string{"telemetry-event-1"})
	telemetryResponse := performPlaybackJSONRequest(
		router,
		http.MethodPost,
		"/api/playback-telemetry-batches",
		telemetryBody,
		token,
		"",
	)
	requireStatus(t, telemetryResponse, http.StatusCreated)

	var telemetry telemetryBatchResponse
	decodeJSON(t, telemetryResponse, &telemetry)
	if telemetry.AcceptedCount != 1 || telemetry.DuplicateCount != 0 || telemetry.EventCount != 1 {
		t.Fatalf("unexpected telemetry response: %+v", telemetry)
	}

	telemetryReplay := performPlaybackJSONRequest(
		router,
		http.MethodPost,
		"/api/playback-telemetry-batches",
		telemetryBody,
		token,
		"",
	)
	requireStatus(t, telemetryReplay, http.StatusOK)

	duplicateTelemetry := performPlaybackJSONRequest(
		router,
		http.MethodPost,
		"/api/playback-telemetry-batches",
		telemetryBatchBody(t, 1, "telemetry-batch-2", []string{"telemetry-event-1", "telemetry-event-2"}),
		token,
		"",
	)
	requireStatus(t, duplicateTelemetry, http.StatusCreated)
	decodeJSON(t, duplicateTelemetry, &telemetry)
	if telemetry.AcceptedCount != 1 || telemetry.DuplicateCount != 1 {
		t.Fatalf("unexpected telemetry duplicate accounting: %+v", telemetry)
	}
}

func TestPlaybackAPIValidation(t *testing.T) {
	router, jwtManager := newPlaybackRouter(t)
	token := signTestToken(t, jwtManager, 42)

	requireStatus(t, performJSONRequest(router, http.MethodGet, "/api/playback-config", "", ""), http.StatusUnauthorized)
	requireStatus(t, performJSONRequest(router, http.MethodGet, "/api/preload-videos", "", ""), http.StatusUnauthorized)
	requireStatus(t, performPlaybackJSONRequest(router, http.MethodPost, "/api/playback-qos-reports", `{"video_id":103}`, "", ""), http.StatusUnauthorized)
	requireStatus(t, performPlaybackJSONRequest(router, http.MethodPost, "/api/playback-telemetry-batches", telemetryBatchBody(t, 1, "batch", []string{"event"}), "", ""), http.StatusUnauthorized)

	requireStatus(t, performJSONRequest(router, http.MethodGet, "/api/playback-config?platform="+strings.Repeat("x", 17), "", token), http.StatusBadRequest)
	requireStatus(t, performJSONRequest(router, http.MethodGet, "/api/preload-videos?limit=0", "", token), http.StatusBadRequest)
	requireStatus(t, performJSONRequest(router, http.MethodGet, "/api/preload-videos?current_video_id=bad", "", token), http.StatusBadRequest)
	requireStatus(t, performPlaybackJSONRequest(router, http.MethodPost, "/api/playback-qos-reports", `{"video_id":0}`, token, ""), http.StatusBadRequest)
	requireStatus(t, performPlaybackJSONRequest(router, http.MethodPost, "/api/playback-qos-reports", `{"video_id":103,"first_frame_ms":-1}`, token, ""), http.StatusBadRequest)
	requireStatus(t, performPlaybackJSONRequest(router, http.MethodPost, "/api/playback-qos-reports", `{"video_id":103,"stutter_count":-1}`, token, ""), http.StatusBadRequest)
	requireStatus(t, performPlaybackJSONRequest(router, http.MethodPost, "/api/playback-qos-reports", `{"video_id":103,"watch_ms":-1}`, token, ""), http.StatusBadRequest)
	requireStatus(t, performInternalPlaybackRequest(router, http.MethodPost, "/internal/playback-qos-reports", `{"user_id":77,"video_id":101}`, "", ""), http.StatusUnauthorized)
	requireStatus(t, performInternalPlaybackRequest(router, http.MethodPost, "/internal/playback-qos-reports", `{"user_id":77,"video_id":101}`, "wrong-token", ""), http.StatusUnauthorized)
	requireStatus(t, performInternalPlaybackRequest(router, http.MethodPost, "/internal/playback-qos-reports", `{"user_id":-1,"video_id":101}`, testInternalToken, ""), http.StatusBadRequest)

	requireStatus(t, performPlaybackJSONRequest(
		router, http.MethodPost, "/api/playback-telemetry-batches",
		telemetryBatchBody(t, 2, "unsupported-version", []string{"event-version"}), token, "",
	), http.StatusBadRequest)
	requireStatus(t, performPlaybackJSONRequest(
		router, http.MethodPost, "/api/playback-telemetry-batches",
		strings.Replace(telemetryBatchBody(t, 1, "prohibited-field", []string{"event-prohibited"}), `"schema_version":1`, `"schema_version":1,"token":"secret"`, 1), token, "",
	), http.StatusBadRequest)
	requireStatus(t, performPlaybackJSONRequest(
		router, http.MethodPost, "/api/playback-telemetry-batches",
		strings.Replace(telemetryBatchBody(t, 1, "signed-url", []string{"event-signed-url"}), `"cdn_host":"cdn.example.com"`, `"cdn_host":"https://cdn.example.com/video.mp4?token=secret"`, 1), token, "",
	), http.StatusBadRequest)
	requireStatus(t, performPlaybackJSONRequest(
		router, http.MethodPost, "/api/playback-telemetry-batches",
		`{"schema_version":1,"padding":"`+strings.Repeat("x", domainplayback.MaxTelemetryPayloadBytes)+`"}`, token, "",
	), http.StatusBadRequest)
}

func newPlaybackRouter(t *testing.T) (*server.Hertz, *infrajwt.Manager) {
	t.Helper()

	repo := newMemoryPlaybackRepo()
	service := applicationplayback.New(repo, applicationplayback.WithTelemetryRepository(repo))
	handler := interfaceshttpplayback.New(service)
	jwtManager, err := infrajwt.NewManager("test-secret", "15m")
	if err != nil {
		t.Fatalf("new jwt manager: %v", err)
	}

	router := server.New()
	authMiddleware := interfaceshttpmiddleware.NewJWTAuth(jwtManager)
	api := router.Group("/api", authMiddleware)
	api.GET("/playback-config", handler.GetConfig)
	api.GET("/preload-videos", handler.ListPreloadVideos)
	api.POST("/playback-qos-reports", handler.CreateQoSReport)
	api.POST("/playback-telemetry-batches", handler.CreateTelemetryBatch)
	router.POST("/internal/playback-qos-reports", interfaceshttpmiddleware.NewInternalTokenAuth(testInternalToken), handler.CreateInternalQoSReport)

	return router, jwtManager
}

type telemetryBatchResponse struct {
	BatchID        string    `json:"batch_id"`
	EventCount     int       `json:"event_count"`
	AcceptedCount  int       `json:"accepted_count"`
	DuplicateCount int       `json:"duplicate_count"`
	CreatedAt      time.Time `json:"created_at"`
}

func telemetryBatchBody(t *testing.T, schemaVersion int, batchID string, eventIDs []string) string {
	t.Helper()
	events := make([]map[string]any, 0, len(eventIDs))
	for index, eventID := range eventIDs {
		events = append(events, map[string]any{
			"event_id": eventID, "event_type": "load_start", "offset_ms": index * 10,
			"media_position_ms": 0,
		})
	}
	payload := map[string]any{
		"schema_version":      schemaVersion,
		"batch_id":            batchID,
		"playback_session_id": "playback-telemetry-1",
		"client_sent_at":      time.Now().UTC(),
		"context": map[string]any{
			"video_id": 103, "scene": "recommend", "request_id": "req-telemetry",
			"player_adapter": "native_mp4", "source_type": "mp4", "rendition_label": "720p",
			"codec_family": "h264", "network_class": "wifi", "save_data": false,
			"browser_family": "chrome", "browser_major": 126, "os_family": "linux",
			"viewport_class": "large", "cdn_host": "cdn.example.com",
		},
		"events": events,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal telemetry batch: %v", err)
	}
	return string(encoded)
}

func performPlaybackJSONRequest(router *server.Hertz, method, path, body, accessToken, idempotencyKey string) *ut.ResponseRecorder {
	headers := make([]ut.Header, 0, 2)
	if accessToken != "" {
		headers = append(headers, ut.Header{Key: "Authorization", Value: "Bearer " + accessToken})
	}
	if idempotencyKey != "" {
		headers = append(headers, ut.Header{Key: "Idempotency-Key", Value: idempotencyKey})
	}
	return performJSONRequestWithHeaders(router, method, path, body, headers...)
}

func performInternalPlaybackRequest(router *server.Hertz, method, path, body, internalToken, idempotencyKey string) *ut.ResponseRecorder {
	headers := make([]ut.Header, 0, 2)
	if internalToken != "" {
		headers = append(headers, ut.Header{Key: "X-Internal-Token", Value: internalToken})
	}
	if idempotencyKey != "" {
		headers = append(headers, ut.Header{Key: "Idempotency-Key", Value: idempotencyKey})
	}
	return performJSONRequestWithHeaders(router, method, path, body, headers...)
}
