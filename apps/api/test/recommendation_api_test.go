package test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	applicationfeed "GCFeed/internal/application/feed"
	applicationrecommendation "GCFeed/internal/application/recommendation"
	domainfeed "GCFeed/internal/domain/feed"
	domainrecommendation "GCFeed/internal/domain/recommendation"
	infrajwt "GCFeed/internal/infra/jwt"
	interfaceshttpfeed "GCFeed/internal/interfaces/http/feed"
	interfaceshttpmiddleware "GCFeed/internal/interfaces/http/middleware"
	interfaceshttprecommendation "GCFeed/internal/interfaces/http/recommendation"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
)

const recommendationInternalToken = "recommendation-internal-token"

type recommendationAPIResponse struct {
	UserID     int64                             `json:"user_id"`
	Scene      string                            `json:"scene"`
	RequestID  string                            `json:"request_id"`
	Candidates []recommendationCandidateResponse `json:"candidates"`
	NextCursor string                            `json:"next_cursor"`
	HasMore    bool                              `json:"has_more"`
}

type recommendationCandidateResponse struct {
	VideoID        int64     `json:"video_id"`
	AuthorID       int64     `json:"author_id"`
	RankScore      float64   `json:"rank_score"`
	Similarity     float64   `json:"similarity"`
	HotScore       int       `json:"hot_score"`
	FreshnessScore float64   `json:"freshness_score"`
	Reason         string    `json:"reason"`
	PublishedAt    time.Time `json:"published_at"`
}

type recommendationExposureAPIResponse struct {
	Exposures []recommendationExposureResponse `json:"exposures"`
}

type recommendationExposureDecisionAPIResponse struct {
	UserID    int64                                    `json:"user_id"`
	Scene     string                                   `json:"scene"`
	RequestID string                                   `json:"request_id"`
	Decisions []recommendationExposureDecisionResponse `json:"decisions"`
}

type recommendationExposureDecisionResponse struct {
	VideoID       int64      `json:"video_id"`
	Allowed       bool       `json:"allowed"`
	Reason        string     `json:"reason"`
	LastExposedAt *time.Time `json:"last_exposed_at"`
}

type recommendationExposureResponse struct {
	UserID         int64     `json:"user_id"`
	VideoID        int64     `json:"video_id"`
	FirstExposedAt time.Time `json:"first_exposed_at"`
	LastExposedAt  time.Time `json:"last_exposed_at"`
	ExposureCount  int       `json:"exposure_count"`
	LastScene      string    `json:"last_scene"`
}

type recommendationFeedbackResponse struct {
	ID           int64     `json:"id"`
	VideoID      int64     `json:"video_id"`
	RequestID    string    `json:"request_id"`
	FeedbackType string    `json:"feedback_type"`
	CreatedAt    time.Time `json:"created_at"`
	Replayed     bool      `json:"replayed"`
}

type memoryRecommendationRepo struct {
	mu        sync.Mutex
	pool      []*domainrecommendation.Candidate
	vectors   map[int64][]float64
	interest  map[int64][]float64
	published map[int64]bool
	exposures map[string]*domainrecommendation.Exposure
	feedback  map[string]*domainrecommendation.Feedback
	evidence  map[string]bool
}

func newMemoryRecommendationRepo() *memoryRecommendationRepo {
	base := time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC)
	return &memoryRecommendationRepo{
		pool: []*domainrecommendation.Candidate{
			domainrecommendation.RestoreCandidate(1, 10, 0, 0, 8, 0, "", base.Add(-4*time.Hour)),
			domainrecommendation.RestoreCandidate(2, 20, 0, 0, 40, 0, "", base.Add(-3*time.Hour)),
			domainrecommendation.RestoreCandidate(3, 30, 0, 0, 8, 0, "", base.Add(-2*time.Hour)),
			domainrecommendation.RestoreCandidate(4, 20, 0, 0, 6, 0, "", base.Add(-1*time.Hour)),
		},
		vectors: map[int64][]float64{
			1: {1, 0},
			2: {0, 1},
			3: {1, 0},
			4: {0.7, 0.3},
		},
		interest: map[int64][]float64{
			42: {1, 0},
		},
		published: map[int64]bool{1: true, 2: true, 3: true, 4: true},
		exposures: map[string]*domainrecommendation.Exposure{},
		feedback:  map[string]*domainrecommendation.Feedback{},
		evidence: map[string]bool{
			"42:req-feedback:3":  true,
			"42:req-feedback:4":  true,
			"42:req-missing:404": true,
		},
	}
}

func (r *memoryRecommendationRepo) ListCandidatePool(ctx context.Context, userID int64, limit int) ([]*domainrecommendation.Candidate, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	items := make([]*domainrecommendation.Candidate, 0, len(r.pool))
	for _, item := range r.pool {
		if item == nil {
			continue
		}
		if r.exposures[recommendationExposureKey(userID, item.VideoID)] != nil {
			continue
		}
		value := *item
		items = append(items, &value)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].HotScore == items[j].HotScore {
			if items[i].PublishedAt.Equal(items[j].PublishedAt) {
				return items[i].VideoID > items[j].VideoID
			}
			return items[i].PublishedAt.After(items[j].PublishedAt)
		}
		return items[i].HotScore > items[j].HotScore
	})
	if limit > 0 && limit < len(items) {
		items = items[:limit]
	}
	return items, nil
}

func (r *memoryRecommendationRepo) LoadUserInterestVector(ctx context.Context, userID int64) ([]float64, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	vector := r.interest[userID]
	if len(vector) == 0 {
		return nil, false, nil
	}
	return append([]float64(nil), vector...), true, nil
}

func (r *memoryRecommendationRepo) LoadVideoVectors(ctx context.Context, videoIDs []int64) (map[int64][]float64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	vectors := map[int64][]float64{}
	for _, videoID := range videoIDs {
		if vector := r.vectors[videoID]; len(vector) > 0 {
			vectors[videoID] = append([]float64(nil), vector...)
		}
	}
	return vectors, nil
}

func (r *memoryRecommendationRepo) ListRecentExposures(ctx context.Context, userID int64, videoIDs []int64, since time.Time) ([]*domainrecommendation.Exposure, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	exposures := make([]*domainrecommendation.Exposure, 0, len(videoIDs))
	for _, videoID := range videoIDs {
		exposure := r.exposures[recommendationExposureKey(userID, videoID)]
		if exposure == nil || exposure.LastExposedAt.Before(since) {
			continue
		}
		exposures = append(exposures, cloneRecommendationExposure(exposure))
	}
	return exposures, nil
}

func (r *memoryRecommendationRepo) SaveExposures(ctx context.Context, writes []*domainrecommendation.ExposureWrite) ([]*domainrecommendation.Exposure, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Date(2026, 5, 3, 13, 0, 0, 0, time.UTC)
	exposures := make([]*domainrecommendation.Exposure, 0, len(writes))
	for _, write := range writes {
		if !r.published[write.VideoID] {
			return nil, domainrecommendation.ErrVideoNotFound
		}
		key := recommendationExposureKey(write.UserID, write.VideoID)
		exposure := r.exposures[key]
		if exposure == nil {
			exposure = domainrecommendation.RestoreExposure(int64(len(r.exposures)+1), write.UserID, write.VideoID, now, now, 1, write.Scene)
			r.exposures[key] = exposure
			exposures = append(exposures, cloneRecommendationExposure(exposure))
			continue
		}
		exposure.LastExposedAt = now.Add(time.Duration(exposure.ExposureCount) * time.Second)
		exposure.ExposureCount++
		exposure.LastScene = write.Scene
		exposures = append(exposures, cloneRecommendationExposure(exposure))
	}
	return exposures, nil
}

func (r *memoryRecommendationRepo) SaveFeedback(ctx context.Context, feedback *domainrecommendation.Feedback) (*domainrecommendation.Feedback, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.published[feedback.VideoID] {
		return nil, false, domainrecommendation.ErrVideoNotFound
	}
	key := fmt.Sprintf("%d:%s", feedback.UserID, feedback.IdempotencyKey)
	if existing := r.feedback[key]; existing != nil {
		if !existing.SameNormalizedPayload(feedback) {
			return nil, false, domainrecommendation.ErrFeedbackIdempotencyConflict
		}
		cloned := *existing
		return &cloned, true, nil
	}
	cloned := *feedback
	cloned.ID = int64(len(r.feedback) + 1)
	r.feedback[key] = &cloned
	result := cloned
	return &result, false, nil
}

func (r *memoryRecommendationRepo) FindFeedbackByUserAndIdempotencyKey(_ context.Context, userID int64, idempotencyKey string) (*domainrecommendation.Feedback, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	feedback := r.feedback[fmt.Sprintf("%d:%s", userID, strings.TrimSpace(idempotencyKey))]
	if feedback == nil {
		return nil, domainrecommendation.ErrFeedbackNotFound
	}
	cloned := *feedback
	return &cloned, nil
}

func (r *memoryRecommendationRepo) GetFeedbackVideo(ctx context.Context, videoID int64) (*applicationrecommendation.FeedbackVideo, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.published[videoID] {
		return nil, domainrecommendation.ErrVideoNotFound
	}

	for _, candidate := range r.pool {
		if candidate != nil && candidate.VideoID == videoID && candidate.AuthorID > 0 {
			return &applicationrecommendation.FeedbackVideo{VideoID: videoID, AuthorID: candidate.AuthorID}, nil
		}
	}
	return nil, domainrecommendation.ErrVideoNotFound
}

func (r *memoryRecommendationRepo) SaveServedCandidateEvidence(_ context.Context, evidence *domainrecommendation.ServedCandidateEvidence) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, candidate := range evidence.Candidates {
		r.evidence[fmt.Sprintf("%d:%s:%d", evidence.UserID, evidence.RequestID, candidate.VideoID)] = true
	}
	return false, nil
}

func (r *memoryRecommendationRepo) AppendServedCandidateEvidence(ctx context.Context, evidence *domainrecommendation.ServedCandidateEvidence) (bool, error) {
	return r.SaveServedCandidateEvidence(ctx, evidence)
}

func (r *memoryRecommendationRepo) HasServedCandidateEvidence(_ context.Context, userID int64, requestID string, videoID int64, _ time.Time) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.evidence[fmt.Sprintf("%d:%s:%d", userID, requestID, videoID)], nil
}

func (*memoryRecommendationRepo) DeleteServedCandidateEvidenceBefore(context.Context, time.Time, int) (domainrecommendation.ServedCandidateEvidenceCleanupResult, error) {
	return domainrecommendation.ServedCandidateEvidenceCleanupResult{}, nil
}

func TestRecommendationExposureDecisionsAPI(t *testing.T) {
	router := newRecommendationRouter()

	for _, token := range []string{"", "wrong-internal-token"} {
		for _, path := range []string{
			"/internal/recommendation-candidates",
			"/internal/exposure-decisions",
			"/internal/exposures",
		} {
			response := performRecommendationInternalRequest(router, http.MethodPost, path, `{}`, token)
			requireStatus(t, response, http.StatusUnauthorized)
		}
	}

	saveResponse := performRecommendationInternalRequest(
		router,
		http.MethodPost,
		"/internal/exposures",
		`{"user_id":42,"scene":"recommend","request_id":"req-exp","video_ids":[1]}`,
		recommendationInternalToken,
	)
	requireStatus(t, saveResponse, http.StatusCreated)

	response := performRecommendationInternalRequest(
		router,
		http.MethodPost,
		"/internal/exposure-decisions",
		`{"user_id":42,"scene":"recommend","request_id":"req-decide","video_ids":[1,2,2,3]}`,
		recommendationInternalToken,
	)
	requireStatus(t, response, http.StatusOK)

	var result recommendationExposureDecisionAPIResponse
	decodeJSON(t, response, &result)
	if result.UserID != 42 || result.Scene != "recommend" || result.RequestID != "req-decide" {
		t.Fatalf("unexpected exposure decision metadata: %+v", result)
	}
	if len(result.Decisions) != 3 {
		t.Fatalf("unexpected exposure decision count: %+v", result)
	}
	if result.Decisions[0].VideoID != 1 ||
		result.Decisions[0].Allowed ||
		result.Decisions[0].Reason != domainrecommendation.ExposureDecisionReasonRecentlyExposed ||
		result.Decisions[0].LastExposedAt == nil {
		t.Fatalf("unexpected exposed decision: %+v", result.Decisions[0])
	}
	for _, decision := range result.Decisions[1:] {
		if !decision.Allowed || decision.Reason != domainrecommendation.ExposureDecisionReasonFresh || decision.LastExposedAt != nil {
			t.Fatalf("unexpected fresh decision: %+v", decision)
		}
	}

	badResponse := performRecommendationInternalRequest(
		router,
		http.MethodPost,
		"/internal/exposure-decisions",
		`{"user_id":42,"scene":"recommend","video_ids":[0]}`,
		recommendationInternalToken,
	)
	requireStatus(t, badResponse, http.StatusBadRequest)
}

func TestRecommendationCandidatesAPI(t *testing.T) {
	router := newRecommendationRouter()

	response := performRecommendationInternalRequest(
		router,
		http.MethodPost,
		"/internal/recommendation-candidates",
		`{"user_id":42,"scene":"recommend","request_id":"req-1","limit":2}`,
		recommendationInternalToken,
	)
	requireStatus(t, response, http.StatusOK)

	var page recommendationAPIResponse
	decodeJSON(t, response, &page)
	if page.UserID != 42 || page.Scene != "recommend" || page.RequestID != "req-1" {
		t.Fatalf("unexpected recommendation metadata: %+v", page)
	}
	if len(page.Candidates) != 2 || page.Candidates[0].VideoID != 3 || page.Candidates[1].VideoID != 1 {
		t.Fatalf("unexpected recommendation page: %+v", page)
	}
	if page.Candidates[0].Reason != "interest_match" || page.Candidates[0].Similarity <= 0 {
		t.Fatalf("unexpected recommendation reason: %+v", page.Candidates[0])
	}
	if page.NextCursor == "" || !page.HasMore {
		t.Fatalf("unexpected recommendation cursor: %+v", page)
	}

	nextResponse := performRecommendationInternalRequest(
		router,
		http.MethodPost,
		"/internal/recommendation-candidates",
		fmt.Sprintf(`{"user_id":42,"scene":"recommend","cursor":%q,"limit":2}`, page.NextCursor),
		recommendationInternalToken,
	)
	requireStatus(t, nextResponse, http.StatusOK)

	var nextPage recommendationAPIResponse
	decodeJSON(t, nextResponse, &nextPage)
	if len(nextPage.Candidates) != 2 || nextPage.Candidates[0].VideoID != 4 || nextPage.Candidates[1].VideoID != 2 || nextPage.HasMore {
		t.Fatalf("unexpected recommendation next page: %+v", nextPage)
	}
}

func TestRecommendationExposuresAPI(t *testing.T) {
	router := newRecommendationRouter()

	firstResponse := performRecommendationInternalRequest(
		router,
		http.MethodPost,
		"/internal/exposures",
		`{"user_id":42,"scene":"recommend","request_id":"req-exp","video_ids":[1,1,3]}`,
		recommendationInternalToken,
	)
	requireStatus(t, firstResponse, http.StatusCreated)

	var first recommendationExposureAPIResponse
	decodeJSON(t, firstResponse, &first)
	if len(first.Exposures) != 2 || first.Exposures[0].ExposureCount != 1 || first.Exposures[1].VideoID != 3 {
		t.Fatalf("unexpected exposure response: %+v", first)
	}

	secondResponse := performRecommendationInternalRequest(
		router,
		http.MethodPost,
		"/internal/exposures",
		`{"user_id":42,"scene":"recommend","request_id":"req-exp-2","video_ids":[1]}`,
		recommendationInternalToken,
	)
	requireStatus(t, secondResponse, http.StatusCreated)

	var second recommendationExposureAPIResponse
	decodeJSON(t, secondResponse, &second)
	if len(second.Exposures) != 1 || second.Exposures[0].ExposureCount != 2 {
		t.Fatalf("unexpected repeated exposure response: %+v", second)
	}

	candidateResponse := performRecommendationInternalRequest(
		router,
		http.MethodPost,
		"/internal/recommendation-candidates",
		`{"user_id":42,"scene":"recommend","limit":10}`,
		recommendationInternalToken,
	)
	requireStatus(t, candidateResponse, http.StatusOK)
	var page recommendationAPIResponse
	decodeJSON(t, candidateResponse, &page)
	for _, candidate := range page.Candidates {
		if candidate.VideoID == 1 || candidate.VideoID == 3 {
			t.Fatalf("exposed video returned: %+v", page)
		}
	}

	missingResponse := performRecommendationInternalRequest(
		router,
		http.MethodPost,
		"/internal/exposures",
		`{"user_id":42,"scene":"recommend","video_ids":[404]}`,
		recommendationInternalToken,
	)
	requireStatus(t, missingResponse, http.StatusNotFound)
}

func TestRecommendationFeedbackAPIIdempotency(t *testing.T) {
	router, jwtManager, repo := newRecommendationFeedbackRouterWithRepository(t)
	token := signTestToken(t, jwtManager, 42)

	first := performJSONRequestWithHeaders(
		router,
		http.MethodPost,
		"/api/recommendation-feedback",
		`{"video_id":3,"request_id":"req-feedback","feedback_type":"not_interested"}`,
		ut.Header{Key: "Authorization", Value: "Bearer " + token},
		ut.Header{Key: "Idempotency-Key", Value: "feedback-key"},
	)
	requireStatus(t, first, http.StatusCreated)
	var created recommendationFeedbackResponse
	decodeJSON(t, first, &created)
	if created.ID <= 0 || created.VideoID != 3 || created.RequestID != "req-feedback" || created.FeedbackType != domainrecommendation.FeedbackTypeNotInterested || created.Replayed {
		t.Fatalf("unexpected feedback response: %+v", created)
	}
	repo.mu.Lock()
	delete(repo.evidence, "42:req-feedback:3")
	delete(repo.evidence, "42:req-feedback:4")
	repo.mu.Unlock()

	replay := performJSONRequestWithHeaders(
		router,
		http.MethodPost,
		"/api/recommendation-feedback",
		`{"video_id":3,"request_id":"req-feedback","feedback_type":"not_interested"}`,
		ut.Header{Key: "Authorization", Value: "Bearer " + token},
		ut.Header{Key: "Idempotency-Key", Value: "feedback-key"},
	)
	requireStatus(t, replay, http.StatusOK)
	var replayed recommendationFeedbackResponse
	decodeJSON(t, replay, &replayed)
	if !replayed.Replayed || replayed.ID != created.ID || !replayed.CreatedAt.Equal(created.CreatedAt) {
		t.Fatalf("feedback replay after evidence expiry = created=%+v replayed=%+v", created, replayed)
	}

	conflict := performJSONRequestWithHeaders(
		router,
		http.MethodPost,
		"/api/recommendation-feedback",
		`{"video_id":4,"request_id":"req-feedback","feedback_type":"already_seen"}`,
		ut.Header{Key: "Authorization", Value: "Bearer " + token},
		ut.Header{Key: "Idempotency-Key", Value: "feedback-key"},
	)
	requireStatus(t, conflict, http.StatusConflict)
}

func TestRecommendationFeedbackRequiresAuthentication(t *testing.T) {
	router, _ := newRecommendationFeedbackRouter(t)
	response := performJSONRequestWithHeaders(
		router,
		http.MethodPost,
		"/api/recommendation-feedback",
		`{"video_id":3,"request_id":"req-feedback","feedback_type":"not_interested"}`,
		ut.Header{Key: "Idempotency-Key", Value: "feedback-key"},
	)
	requireStatus(t, response, http.StatusUnauthorized)
}

func TestRecommendationFeedbackRejectsMissingVideoBeforePersistence(t *testing.T) {
	router, jwtManager, repo := newRecommendationFeedbackRouterWithRepository(t)
	token := signTestToken(t, jwtManager, 42)

	response := performJSONRequestWithHeaders(
		router,
		http.MethodPost,
		"/api/recommendation-feedback",
		`{"video_id":404,"request_id":"req-missing","feedback_type":"reduce_author"}`,
		ut.Header{Key: "Authorization", Value: "Bearer " + token},
		ut.Header{Key: "Idempotency-Key", Value: "missing-feedback-key"},
	)
	requireStatus(t, response, http.StatusNotFound)
	if len(repo.feedback) != 0 {
		t.Fatalf("missing feedback persisted a row: %#v", repo.feedback)
	}
}

func TestRecommendationFeedbackRejectsFabricatedRequestVideoBeforePersistence(t *testing.T) {
	router, jwtManager, repo := newRecommendationFeedbackRouterWithRepository(t)
	token := signTestToken(t, jwtManager, 42)

	response := performJSONRequestWithHeaders(
		router,
		http.MethodPost,
		"/api/recommendation-feedback",
		`{"video_id":2,"request_id":"req-feedback","feedback_type":"not_interested"}`,
		ut.Header{Key: "Authorization", Value: "Bearer " + token},
		ut.Header{Key: "Idempotency-Key", Value: "fabricated-feedback-key"},
	)
	requireStatus(t, response, http.StatusBadRequest)
	if len(repo.feedback) != 0 {
		t.Fatalf("fabricated feedback persisted a row: %#v", repo.feedback)
	}
}

func TestMemoryRecommendationFeedbackRepositoryRejectsMissingVideo(t *testing.T) {
	repo := newMemoryRecommendationRepo()
	feedback, err := domainrecommendation.NewFeedback(
		42, 404, "req-missing", domainrecommendation.FeedbackTypeAlreadySeen, "missing-repository-key", time.Now().UTC(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := repo.SaveFeedback(context.Background(), feedback); !errors.Is(err, domainrecommendation.ErrVideoNotFound) {
		t.Fatalf("missing feedback target error = %v", err)
	}
	if len(repo.feedback) != 0 {
		t.Fatalf("missing feedback persisted a row: %#v", repo.feedback)
	}
}

func TestRecommendFeedScene(t *testing.T) {
	recommendationRepo := newMemoryRecommendationRepo()
	recommendationService := applicationrecommendation.New(
		recommendationRepo,
		applicationrecommendation.WithNow(func() time.Time {
			return time.Date(2026, 5, 3, 13, 0, 0, 0, time.UTC)
		}),
	)
	feedRepo := newMemoryFeedRepo(seedRecommendFeedItems())
	router, jwtManager := newRecommendFeedRouterWithJWT(t, applicationfeed.New(feedRepo, applicationfeed.WithRecommender(recommendationService)))
	token := signTestToken(t, jwtManager, 42)

	response := performJSONRequest(router, http.MethodGet, "/api/feed-items?scene=recommend&limit=2", "", token)
	requireStatus(t, response, http.StatusOK)

	var page feedAPIResponse
	decodeJSON(t, response, &page)
	if page.Scene != string(domainfeed.SceneRecommend) {
		t.Fatalf("unexpected recommend feed scene: %+v", page)
	}
	if len(page.Items) != 2 || page.Items[0].VideoID != 3 || page.Items[1].VideoID != 1 || !page.HasMore {
		t.Fatalf("unexpected recommend feed response: %+v", page)
	}

	unauthorizedResponse := performJSONRequest(router, http.MethodGet, "/api/feed-items?scene=recommend&limit=2", "", "")
	requireStatus(t, unauthorizedResponse, http.StatusUnauthorized)
}

func newRecommendationRouter() *server.Hertz {
	router := server.New()
	service := applicationrecommendation.New(
		newMemoryRecommendationRepo(),
		applicationrecommendation.WithNow(func() time.Time {
			return time.Date(2026, 5, 3, 13, 0, 0, 0, time.UTC)
		}),
	)
	handler := interfaceshttprecommendation.New(service)

	internal := router.Group("/internal")
	internal.POST("/recommendation-candidates", interfaceshttpmiddleware.NewInternalTokenAuth(recommendationInternalToken), handler.ListCandidates)
	internal.POST("/exposure-decisions", interfaceshttpmiddleware.NewInternalTokenAuth(recommendationInternalToken), handler.DecideExposures)
	internal.POST("/exposures", interfaceshttpmiddleware.NewInternalTokenAuth(recommendationInternalToken), handler.SaveExposures)

	return router
}

func performRecommendationInternalRequest(router *server.Hertz, method string, path string, body string, token string) *ut.ResponseRecorder {
	headers := []ut.Header{}
	if token != "" {
		headers = append(headers, ut.Header{Key: "X-Internal-Token", Value: token})
	}
	return performJSONRequestWithHeaders(router, method, path, body, headers...)
}

func newRecommendationFeedbackRouter(t *testing.T) (*server.Hertz, *infrajwt.Manager) {
	router, jwtManager, _ := newRecommendationFeedbackRouterWithRepository(t)
	return router, jwtManager
}

func newRecommendationFeedbackRouterWithRepository(t *testing.T) (*server.Hertz, *infrajwt.Manager, *memoryRecommendationRepo) {
	t.Helper()
	router := server.New()
	jwtManager, err := infrajwt.NewManager("test-secret", "15m")
	if err != nil {
		t.Fatalf("new jwt manager: %v", err)
	}
	repo := newMemoryRecommendationRepo()
	service := applicationrecommendation.New(
		repo,
		applicationrecommendation.WithNow(func() time.Time {
			return time.Date(2026, 5, 3, 13, 0, 0, 0, time.UTC)
		}),
	)
	handler := interfaceshttprecommendation.New(service)
	api := router.Group("/api")
	api.POST("/recommendation-feedback", interfaceshttpmiddleware.NewJWTAuth(jwtManager), handler.CreateFeedback)
	return router, jwtManager, repo
}

func newRecommendFeedRouterWithJWT(t *testing.T, service *applicationfeed.Service) (*server.Hertz, *infrajwt.Manager) {
	t.Helper()

	router := server.New()
	jwtManager, err := infrajwt.NewManager("test-secret", "15m")
	if err != nil {
		t.Fatalf("new jwt manager: %v", err)
	}
	handler := interfaceshttpfeed.New(service)
	optionalAuthMiddleware := interfaceshttpmiddleware.NewOptionalJWTAuth(jwtManager)

	api := router.Group("/api")
	api.GET("/feed-items", optionalAuthMiddleware, handler.ListFeedItems)

	return router, jwtManager
}

func seedRecommendFeedItems() []*domainfeed.FeedItem {
	base := time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC)
	return []*domainfeed.FeedItem{
		domainfeed.RestoreFeedItem(1, 10, "author 1", "https://example.com/a1.jpg", "match old", "match old description", "https://example.com/1.mp4", "https://example.com/1.jpg", 1, 1, 1, base.Add(-4*time.Hour)),
		domainfeed.RestoreFeedItem(2, 20, "author 2", "https://example.com/a2.jpg", "hot miss", "hot miss description", "https://example.com/2.mp4", "https://example.com/2.jpg", 4, 4, 4, base.Add(-3*time.Hour)),
		domainfeed.RestoreFeedItem(3, 30, "author 3", "https://example.com/a3.jpg", "match new", "match new description", "https://example.com/3.mp4", "https://example.com/3.jpg", 1, 1, 1, base.Add(-2*time.Hour)),
		domainfeed.RestoreFeedItem(4, 20, "author 2", "https://example.com/a2.jpg", "mixed", "mixed description", "https://example.com/4.mp4", "https://example.com/4.jpg", 1, 1, 0, base.Add(-1*time.Hour)),
	}
}

func cloneRecommendationExposure(exposure *domainrecommendation.Exposure) *domainrecommendation.Exposure {
	cloned := *exposure
	return &cloned
}

func recommendationExposureKey(userID int64, videoID int64) string {
	return fmt.Sprintf("%d:%d", userID, videoID)
}
