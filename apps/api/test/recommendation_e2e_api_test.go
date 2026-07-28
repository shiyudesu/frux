package test

import (
	applicationexposure "GCFeed/internal/application/exposure"
	applicationfeed "GCFeed/internal/application/feed"
	applicationrecommendation "GCFeed/internal/application/recommendation"
	domainfeed "GCFeed/internal/domain/feed"
	domainrecommendation "GCFeed/internal/domain/recommendation"
	infrajwt "GCFeed/internal/infra/jwt"
	interfaceshttpfeed "GCFeed/internal/interfaces/http/feed"
	interfaceshttpmiddleware "GCFeed/internal/interfaces/http/middleware"
	interfaceshttprecommendation "GCFeed/internal/interfaces/http/recommendation"
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"testing"
	"time"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
)

type recommendationE2ERepo struct {
	*memoryRecommendationRepo
	logs              []*domainrecommendation.RecommendationRequestLog
	outcomes          map[string]*domainrecommendation.Outcome
	suppressedVideos  map[int64]bool
	suppressedAuthors map[int64]bool
}

func newRecommendationE2ERepo() *recommendationE2ERepo {
	return &recommendationE2ERepo{
		memoryRecommendationRepo: newMemoryRecommendationRepo(),
		outcomes:                 map[string]*domainrecommendation.Outcome{},
		suppressedVideos:         map[int64]bool{},
		suppressedAuthors:        map[int64]bool{},
	}
}

func (r *recommendationE2ERepo) ListFreshCandidates(_ context.Context, limit int) ([]*domainrecommendation.Candidate, error) {
	return r.candidates(limit, func(left, right *domainrecommendation.Candidate) bool {
		if !left.PublishedAt.Equal(right.PublishedAt) {
			return left.PublishedAt.After(right.PublishedAt)
		}
		return left.VideoID > right.VideoID
	}), nil
}

func (r *recommendationE2ERepo) ListHotCandidates(_ context.Context, limit int) ([]*domainrecommendation.Candidate, error) {
	return r.candidates(limit, func(left, right *domainrecommendation.Candidate) bool {
		if left.HotScore != right.HotScore {
			return left.HotScore > right.HotScore
		}
		return left.VideoID > right.VideoID
	}), nil
}

func (r *recommendationE2ERepo) ListEmbeddingCandidates(_ context.Context, _ string, limit int) ([]*domainrecommendation.Candidate, error) {
	return r.candidates(limit, func(left, right *domainrecommendation.Candidate) bool {
		return left.VideoID > right.VideoID
	}), nil
}

func (r *recommendationE2ERepo) ListPublicCandidatesByAuthors(_ context.Context, _ []int64, limit int) ([]*domainrecommendation.Candidate, error) {
	return r.candidates(limit, func(left, right *domainrecommendation.Candidate) bool {
		return left.VideoID > right.VideoID
	}), nil
}

func (r *recommendationE2ERepo) candidates(limit int, less func(*domainrecommendation.Candidate, *domainrecommendation.Candidate) bool) []*domainrecommendation.Candidate {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]*domainrecommendation.Candidate, 0, len(r.pool))
	for _, candidate := range r.pool {
		if candidate != nil && r.published[candidate.VideoID] {
			items = append(items, candidate.Clone())
		}
	}
	sort.Slice(items, func(i, j int) bool { return less(items[i], items[j]) })
	if limit < len(items) {
		items = items[:limit]
	}
	return items
}

func (r *recommendationE2ERepo) ListVisibleCandidates(_ context.Context, videoIDs []int64) ([]*domainrecommendation.Candidate, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	byID := make(map[int64]*domainrecommendation.Candidate, len(r.pool))
	for _, candidate := range r.pool {
		if candidate != nil && r.published[candidate.VideoID] {
			byID[candidate.VideoID] = candidate
		}
	}
	items := make([]*domainrecommendation.Candidate, 0, len(videoIDs))
	for _, videoID := range videoIDs {
		if candidate := byID[videoID]; candidate != nil {
			items = append(items, candidate.Clone())
		}
	}
	return items, nil
}

func (r *recommendationE2ERepo) LoadRankingFeatures(_ context.Context, _ int64, _ []int64, _ time.Time, _ time.Time) (*domainrecommendation.RankingFeatures, error) {
	return &domainrecommendation.RankingFeatures{
		FollowedAuthors:   map[int64]bool{},
		RecentExposures:   map[int64]*domainrecommendation.Exposure{},
		NegativeVideos:    map[int64]bool{},
		NegativeAuthors:   map[int64]bool{},
		SuppressedVideos:  cloneBoolMap(r.suppressedVideos),
		SuppressedAuthors: cloneBoolMap(r.suppressedAuthors),
	}, nil
}

func (r *recommendationE2ERepo) SaveFeedback(ctx context.Context, feedback *domainrecommendation.Feedback) (*domainrecommendation.Feedback, bool, error) {
	saved, replayed, err := r.memoryRecommendationRepo.SaveFeedback(ctx, feedback)
	if err != nil || replayed {
		return saved, replayed, err
	}
	if saved.FeedbackType == domainrecommendation.FeedbackTypeReduceAuthor {
		for _, candidate := range r.pool {
			if candidate != nil && candidate.VideoID == saved.VideoID {
				r.suppressedAuthors[candidate.AuthorID] = true
				break
			}
		}
	} else {
		r.suppressedVideos[saved.VideoID] = true
	}
	return saved, false, nil
}

func (r *recommendationE2ERepo) SaveRequestLog(_ context.Context, log *domainrecommendation.RecommendationRequestLog) (*domainrecommendation.RecommendationRequestLog, bool, error) {
	r.logs = append(r.logs, log)
	return log, false, nil
}

func (*recommendationE2ERepo) DeleteRequestLogsBefore(context.Context, time.Time, int) (int64, error) {
	return 0, nil
}

func (r *recommendationE2ERepo) ApplyBehaviorEvent(context.Context, *applicationexposure.ViewEventRecordedEvent) (bool, error) {
	return true, nil
}

func (r *recommendationE2ERepo) SaveOutcome(_ context.Context, outcome *domainrecommendation.Outcome) (bool, error) {
	if _, exists := r.outcomes[outcome.ID]; exists {
		return false, nil
	}
	r.outcomes[outcome.ID] = outcome
	return true, nil
}

func (r *recommendationE2ERepo) VerifyAndSaveOutcome(ctx context.Context, outcome *domainrecommendation.Outcome, followedTargetUserID int64) (bool, bool, error) {
	if outcome == nil {
		return false, false, domainrecommendation.ErrInvalidRequestLog
	}
	if followedTargetUserID != 0 {
		video, err := r.GetFeedbackVideo(ctx, outcome.VideoID)
		if err != nil || video == nil || video.AuthorID != followedTargetUserID {
			return false, false, err
		}
	}
	valid, err := r.HasServedCandidateEvidence(ctx, outcome.UserID, outcome.RequestID, outcome.VideoID, outcome.RecordedAt)
	if err != nil || !valid {
		return false, false, err
	}
	recorded, err := r.SaveOutcome(ctx, outcome)
	return recorded, err == nil, err
}

type recommendationE2ESnapshotStore struct {
	snapshots map[string]*applicationrecommendation.Snapshot
}

func (s *recommendationE2ESnapshotStore) CreateSnapshot(_ context.Context, snapshot *applicationrecommendation.Snapshot, _ time.Duration) (*applicationrecommendation.Snapshot, bool, error) {
	if s.snapshots == nil {
		s.snapshots = map[string]*applicationrecommendation.Snapshot{}
	}
	for _, existing := range s.snapshots {
		if existing.UserID == snapshot.UserID && existing.Scene == snapshot.Scene && existing.RequestID == snapshot.RequestID {
			return existing.Clone(), false, nil
		}
	}
	s.snapshots[snapshot.ID] = snapshot.Clone()
	return snapshot.Clone(), true, nil
}

func (s *recommendationE2ESnapshotStore) LoadSnapshot(_ context.Context, id string) (*applicationrecommendation.Snapshot, bool, error) {
	snapshot := s.snapshots[id]
	return snapshot.Clone(), snapshot != nil, nil
}

func (s *recommendationE2ESnapshotStore) LoadSnapshotForRequest(_ context.Context, userID int64, scene string, requestID string) (*applicationrecommendation.Snapshot, bool, error) {
	for _, snapshot := range s.snapshots {
		if snapshot.UserID == userID && snapshot.Scene == scene && snapshot.RequestID == requestID {
			return snapshot.Clone(), true, nil
		}
	}
	return nil, false, nil
}

type recommendationE2EPolicySelector struct {
	policy *domainrecommendation.Policy
}

func (s *recommendationE2EPolicySelector) Select(context.Context, string, int64, string) (*domainrecommendation.Policy, error) {
	return s.policy.Clone(), nil
}

type recommendationE2EFailingProvider struct{}

func (recommendationE2EFailingProvider) Name() string {
	return domainrecommendation.RecallProviderContentSimilarity
}

func (recommendationE2EFailingProvider) Recall(context.Context, applicationrecommendation.RecallRequest) ([]*domainrecommendation.Candidate, error) {
	return nil, errors.New("embedding backend unavailable")
}

type recommendationE2EFeedRepo struct {
	*memoryFeedRepo
	unreadable map[int64]bool
}

func (r *recommendationE2EFeedRepo) BatchPublicVideoIDs(_ context.Context, videoIDs []int64) (map[int64]struct{}, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	readable := make(map[int64]struct{}, len(videoIDs))
	for _, videoID := range videoIDs {
		if r.unreadable[videoID] {
			continue
		}
		for _, item := range r.items {
			if item != nil && item.VideoID == videoID {
				readable[videoID] = struct{}{}
				break
			}
		}
	}
	return readable, nil
}

func newLegacyRecommendationE2EService(t *testing.T, repo *recommendationE2ERepo, now time.Time) *applicationrecommendation.Service {
	t.Helper()
	policies, err := domainrecommendation.InitialRecommendationPolicies(now)
	if err != nil {
		t.Fatal(err)
	}
	policy := policies[0]
	policy.Config.FeatureWeights = map[string]float64{domainrecommendation.FeatureHotness: 1}
	return applicationrecommendation.New(
		repo,
		applicationrecommendation.WithNow(func() time.Time { return now }),
		applicationrecommendation.WithPolicySelector(&recommendationE2EPolicySelector{policy: policy}),
		applicationrecommendation.WithCandidateVisibilityFilter(repo),
		applicationrecommendation.WithRecallProviders(
			applicationrecommendation.NewFreshContentProvider(repo),
			applicationrecommendation.NewHotContentProvider(repo),
		),
	)
}

func TestRecommendationFeedEndToEndContextPolicySnapshotFeedbackAndOutcomes(t *testing.T) {
	now := time.Date(2026, 7, 27, 2, 0, 0, 0, time.UTC)
	repo := newRecommendationE2ERepo()
	policies, err := domainrecommendation.InitialRecommendationPolicies(now)
	if err != nil {
		t.Fatal(err)
	}
	v1, v2 := policies[0], policies[1]
	v2.Config.RolloutPercentage = 100
	v2.Config.SamplingRatePPM = domainrecommendation.MaxSamplingRatePPM
	v2.Config.FeatureWeights = map[string]float64{domainrecommendation.FeatureHotness: 1}
	selector := &recommendationE2EPolicySelector{policy: v2}
	signer, err := applicationrecommendation.NewHMACSnapshotCursorSigner("recommendation-e2e-test-secret")
	if err != nil {
		t.Fatal(err)
	}
	recommendationService := applicationrecommendation.New(
		repo,
		applicationrecommendation.WithNow(func() time.Time { return now }),
		applicationrecommendation.WithPolicySelector(selector),
		applicationrecommendation.WithRequestLogRepository(repo),
		applicationrecommendation.WithCandidateVisibilityFilter(repo),
		applicationrecommendation.WithSnapshotPagination(&recommendationE2ESnapshotStore{}, signer),
		applicationrecommendation.WithRecallProviders(
			applicationrecommendation.NewFreshContentProvider(repo),
			applicationrecommendation.NewHotContentProvider(repo),
			recommendationE2EFailingProvider{},
		),
	)
	feedRepo := newMemoryFeedRepo(seedRecommendFeedItems())
	router, jwtManager := newRecommendationE2ERouter(t, applicationfeed.New(feedRepo, applicationfeed.WithRecommender(recommendationService)), recommendationService)
	token := signTestToken(t, jwtManager, 42)

	unauthenticated := performJSONRequest(router, http.MethodPost, "/api/feed-queries", `{"scene":"recommend","limit":1}`, "")
	requireStatus(t, unauthenticated, http.StatusUnauthorized)

	first := performJSONRequest(router, http.MethodPost, "/api/feed-queries", `{
		"scene":"recommend","limit":1,
		"context":{"request_id":" request-e2e ","session_id":" session-e2e ","refresh_index":2,
		"recent_video_ids":[3,1,3],"current_video_id":3,"network_class":"4G",
		"save_data":true,"viewport_class":"LARGE","playback_capabilities":["MP4","media-source","mp4"]}}`, token)
	requireStatus(t, first, http.StatusOK)
	var firstPage feedAPIResponse
	decodeJSON(t, first, &firstPage)
	if len(firstPage.Items) != 1 || firstPage.Items[0].VideoID != 2 || firstPage.NextCursor == "" {
		t.Fatalf("policy weight did not select hot candidate: %+v", firstPage)
	}
	if len(repo.logs) != 1 {
		t.Fatalf("expected one sampled request log, got %d", len(repo.logs))
	}
	log := repo.logs[0]
	if log.RequestID != "request-e2e" || log.PolicyVersion != 2 || !log.Snapshot || !log.Degraded ||
		log.Context == nil || log.Context.SessionID != "session-e2e" || log.Context.NetworkClass != domainrecommendation.NetworkClass4G ||
		len(log.Context.RecentVideoIDs) != 2 || len(log.Context.PlaybackCapabilities) != 2 {
		t.Fatalf("request context/policy/log normalization failed: %#v", log)
	}
	if !loggedCandidateHasReasons(log.Candidates, 2, domainrecommendation.RecallProviderFresh, domainrecommendation.RecallProviderHot) {
		t.Fatalf("multi-source recall merge was not logged: %#v", log.Candidates)
	}

	selector.policy = v1
	repo.interest[42] = []float64{0, 1}
	repo.published[3] = false
	next := performJSONRequest(router, http.MethodPost, "/api/feed-queries", fmt.Sprintf(
		`{"scene":"recommend","cursor":%q,"limit":2,"context":{"request_id":"request-e2e"}}`, firstPage.NextCursor), token)
	requireStatus(t, next, http.StatusOK)
	var nextPage feedAPIResponse
	decodeJSON(t, next, &nextPage)
	for _, item := range nextPage.Items {
		if item.VideoID == 3 {
			t.Fatalf("snapshot page exposed an item that lost visibility: %+v", nextPage)
		}
	}
	if len(repo.logs) != 1 {
		t.Fatalf("later snapshot pages must not create another request log")
	}

	feedbackBody := `{"video_id":1,"request_id":"request-e2e","feedback_type":"not_interested"}`
	created := performJSONRequestWithHeaders(router, http.MethodPost, "/api/recommendation-feedback", feedbackBody,
		ut.Header{Key: "Authorization", Value: "Bearer " + token}, ut.Header{Key: "Idempotency-Key", Value: "e2e-feedback"})
	requireStatus(t, created, http.StatusCreated)
	replayed := performJSONRequestWithHeaders(router, http.MethodPost, "/api/recommendation-feedback", feedbackBody,
		ut.Header{Key: "Authorization", Value: "Bearer " + token}, ut.Header{Key: "Idempotency-Key", Value: "e2e-feedback"})
	requireStatus(t, replayed, http.StatusOK)
	conflict := performJSONRequestWithHeaders(router, http.MethodPost, "/api/recommendation-feedback",
		`{"video_id":4,"request_id":"request-e2e","feedback_type":"already_seen"}`,
		ut.Header{Key: "Authorization", Value: "Bearer " + token}, ut.Header{Key: "Idempotency-Key", Value: "e2e-feedback"})
	requireStatus(t, conflict, http.StatusConflict)

	afterFeedback := performJSONRequest(router, http.MethodPost, "/api/feed-queries",
		`{"scene":"recommend","limit":4,"context":{"request_id":"after-feedback"}}`, token)
	requireStatus(t, afterFeedback, http.StatusOK)
	var afterFeedbackPage feedAPIResponse
	decodeJSON(t, afterFeedback, &afterFeedbackPage)
	for _, item := range afterFeedbackPage.Items {
		if item.VideoID == 1 {
			t.Fatalf("feedback suppression did not remove video: %+v", afterFeedbackPage)
		}
	}

	worker := applicationrecommendation.NewBehaviorEventWorker(repo, nil)
	if err := worker.Handle(context.Background(), &applicationexposure.ViewEventRecordedEvent{
		EventID: "view-e2e-complete", UserID: 42, VideoID: 2, Scene: "recommend", RequestID: "request-e2e",
		EventType: "complete", OccurredAt: now, RecordedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	outcome := repo.outcomes[domainrecommendation.ViewOutcomeID(42, "view-e2e-complete")]
	if outcome == nil || outcome.RequestID != "request-e2e" || outcome.OutcomeType != "complete" {
		t.Fatalf("outcome was not linked to the sampled request: %#v", outcome)
	}
}

func TestRecommendationLegacyCursorDeliversLaterPageEvidence(t *testing.T) {
	now := time.Date(2026, 7, 27, 2, 0, 0, 0, time.UTC)
	repo := newRecommendationE2ERepo()
	recommendationService := newLegacyRecommendationE2EService(t, repo, now)
	feedRepo := &recommendationE2EFeedRepo{memoryFeedRepo: newMemoryFeedRepo(seedRecommendFeedItems())}
	router, jwtManager := newRecommendationE2ERouter(t, applicationfeed.New(feedRepo, applicationfeed.WithRecommender(recommendationService)), recommendationService)
	token := signTestToken(t, jwtManager, 42)

	first := performJSONRequest(router, http.MethodPost, "/api/feed-queries",
		`{"scene":"recommend","limit":1,"context":{"request_id":"legacy-delivery"}}`, token)
	requireStatus(t, first, http.StatusOK)
	var firstPage feedAPIResponse
	decodeJSON(t, first, &firstPage)
	if firstPage.NextCursor == "" || len(firstPage.Items) != 1 {
		t.Fatalf("legacy first page = %#v", firstPage)
	}

	second := performJSONRequest(router, http.MethodPost, "/api/feed-queries",
		fmt.Sprintf(`{"scene":"recommend","cursor":%q,"limit":1}`, firstPage.NextCursor), token)
	requireStatus(t, second, http.StatusOK)
	var secondPage feedAPIResponse
	decodeJSON(t, second, &secondPage)
	if len(secondPage.Items) != 1 {
		t.Fatalf("legacy second page = %#v", secondPage)
	}
	videoID := secondPage.Items[0].VideoID

	feedback := performJSONRequestWithHeaders(router, http.MethodPost, "/api/recommendation-feedback",
		fmt.Sprintf(`{"video_id":%d,"request_id":"legacy-delivery","feedback_type":"already_seen"}`, videoID),
		ut.Header{Key: "Authorization", Value: "Bearer " + token}, ut.Header{Key: "Idempotency-Key", Value: "legacy-later-page"},
	)
	requireStatus(t, feedback, http.StatusCreated)

	if err := applicationrecommendation.NewBehaviorEventWorker(repo, nil).Handle(context.Background(), &applicationexposure.ViewEventRecordedEvent{
		EventID: "legacy-later-page-view", UserID: 42, VideoID: videoID, Scene: "recommend", RequestID: "legacy-delivery",
		EventType: "complete", Completed: true, OccurredAt: now,
	}); err != nil {
		t.Fatalf("later legacy-page outcome = %v", err)
	}
	if repo.outcomes[domainrecommendation.ViewOutcomeID(42, "legacy-later-page-view")] == nil {
		t.Fatalf("later legacy-page outcome was not attributed: %#v", repo.outcomes)
	}
}

func TestRecommendationFeedRejectsEvidenceForUnreturnedCandidates(t *testing.T) {
	now := time.Date(2026, 7, 27, 2, 0, 0, 0, time.UTC)
	for _, testCase := range []struct {
		name       string
		feedItems  []*domainfeed.FeedItem
		unreadable bool
	}{
		{name: "missing-card", feedItems: []*domainfeed.FeedItem{}},
		{name: "unreadable-card", feedItems: seedRecommendFeedItems(), unreadable: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			repo := newRecommendationE2ERepo()
			recommendationService := newLegacyRecommendationE2EService(t, repo, now)
			feedRepo := &recommendationE2EFeedRepo{
				memoryFeedRepo: newMemoryFeedRepo(testCase.feedItems),
				unreadable:     map[int64]bool{2: testCase.unreadable},
			}
			router, jwtManager := newRecommendationE2ERouter(t, applicationfeed.New(feedRepo, applicationfeed.WithRecommender(recommendationService)), recommendationService)
			token := signTestToken(t, jwtManager, 42)
			requestID := "unreturned-" + testCase.name

			response := performJSONRequest(router, http.MethodPost, "/api/feed-queries",
				fmt.Sprintf(`{"scene":"recommend","limit":1,"context":{"request_id":%q}}`, requestID), token)
			requireStatus(t, response, http.StatusOK)
			var page feedAPIResponse
			decodeJSON(t, response, &page)
			if len(page.Items) != 0 {
				t.Fatalf("unreturned candidate reached Feed response: %#v", page)
			}

			feedback := performJSONRequestWithHeaders(router, http.MethodPost, "/api/recommendation-feedback",
				fmt.Sprintf(`{"video_id":2,"request_id":%q,"feedback_type":"not_interested"}`, requestID),
				ut.Header{Key: "Authorization", Value: "Bearer " + token},
				ut.Header{Key: "Idempotency-Key", Value: "unreturned-feedback-" + testCase.name},
			)
			requireStatus(t, feedback, http.StatusBadRequest)
			if err := applicationrecommendation.NewBehaviorEventWorker(repo, nil).Handle(context.Background(), &applicationexposure.ViewEventRecordedEvent{
				EventID: "unreturned-view-" + testCase.name, UserID: 42, VideoID: 2, Scene: "recommend", RequestID: requestID,
				EventType: "complete", Completed: true, OccurredAt: now,
			}); err != nil {
				t.Fatalf("unreturned view must remain an accepted fact: %v", err)
			}
			if len(repo.outcomes) != 0 {
				t.Fatalf("unreturned candidate was attributed: %#v", repo.outcomes)
			}
		})
	}
}

func newRecommendationE2ERouter(t *testing.T, feedService *applicationfeed.Service, recommendationService *applicationrecommendation.Service) (*server.Hertz, *infrajwt.Manager) {
	t.Helper()
	router := server.New()
	jwtManager, err := infrajwt.NewManager("test-secret", "15m")
	if err != nil {
		t.Fatal(err)
	}
	feedHandler := interfaceshttpfeed.New(feedService)
	recommendationHandler := interfaceshttprecommendation.New(recommendationService)
	api := router.Group("/api")
	api.POST("/feed-queries", interfaceshttpmiddleware.NewOptionalJWTAuth(jwtManager), feedHandler.Query)
	api.POST("/recommendation-feedback", interfaceshttpmiddleware.NewJWTAuth(jwtManager), recommendationHandler.CreateFeedback)
	return router, jwtManager
}

func cloneBoolMap(values map[int64]bool) map[int64]bool {
	cloned := make(map[int64]bool, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func loggedCandidateHasReasons(candidates []domainrecommendation.LoggedCandidate, videoID int64, expected ...string) bool {
	for _, candidate := range candidates {
		if candidate.VideoID != videoID {
			continue
		}
		reasons := map[string]bool{}
		for _, reason := range candidate.Reasons {
			reasons[reason] = true
		}
		for _, reason := range expected {
			if !reasons[reason] {
				return false
			}
		}
		return true
	}
	return false
}
