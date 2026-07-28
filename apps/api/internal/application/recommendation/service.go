package applicationrecommendation

import (
	domainembedding "GCFeed/internal/domain/embedding"
	domainrecommendation "GCFeed/internal/domain/recommendation"
	inframetrics "GCFeed/internal/infra/metrics"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math"
	"sort"
	"strings"
	"time"
)

const defaultLimit = 10
const candidatePoolMultiplier = 8
const minCandidatePoolSize = 50
const maxCandidatePoolSize = 500
const defaultRecallProviderSlots = 16

var ErrLoadRecommendationFailed = errors.New("failed to load recommendations")
var ErrLoadExposureDecisionsFailed = errors.New("failed to load exposure decisions")
var ErrSaveRecommendationExposureFailed = errors.New("failed to save recommendation exposure")
var ErrSaveRecommendationFeedbackFailed = errors.New("failed to save recommendation feedback")
var ErrSaveRecommendationEvidenceFailed = errors.New("failed to save recommendation served-candidate evidence")

type Service struct {
	repo           domainrecommendation.Repository
	now            func() time.Time
	providers      []RecallProvider
	visibility     CandidateVisibilityFilter
	recallBudgets  map[string]int
	recallDeadline map[string]time.Duration
	exposureWindow time.Duration
	defaultPolicy  *domainrecommendation.Policy
	policySelector PolicySelector
	snapshots      SnapshotStore
	cursorSigner   SnapshotCursorSigner
	requestLogs    domainrecommendation.RequestLogRepository
	evidence       domainrecommendation.ServedCandidateEvidenceRepository
	recallSlots    chan struct{}
}

func applyFeedbackSuppression(candidates []*domainrecommendation.Candidate, features *domainrecommendation.RankingFeatures, config domainrecommendation.PolicyConfiguration) []*domainrecommendation.Candidate {
	if features == nil || (len(features.SuppressedVideos) == 0 && len(features.SuppressedAuthors) == 0) {
		return candidates
	}
	allowed := make([]*domainrecommendation.Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate != nil && !features.SuppressedVideos[candidate.VideoID] && !features.SuppressedAuthors[candidate.AuthorID] {
			allowed = append(allowed, candidate)
		}
	}
	minimum := config.MinimumFallbackPool
	if minimum > len(candidates) {
		minimum = len(candidates)
	}
	if len(allowed) < minimum {
		return candidates
	}
	return allowed
}

type Option func(*Service)

type CandidateRequest struct {
	UserID    int64
	Scene     string
	RequestID string
	Cursor    string
	Limit     int
	Context   *domainrecommendation.RecommendationContext
}

type CandidateResult struct {
	UserID            int64
	Scene             string
	RequestID         string
	Candidates        []*domainrecommendation.Candidate
	NextCursor        string
	HasMore           bool
	Degraded          bool
	DegradedProviders []ProviderDegradation
	PolicyVersion     int
	DeliveryExpiresAt time.Time
}

// DeliveredCandidatesInput contains only the final Feed item identities that
// were actually returned after card hydration and readability checks.
type DeliveredCandidatesInput struct {
	UserID        int64
	RequestID     string
	PolicyVersion int
	VideoIDs      []int64
	ExpiresAt     time.Time
}

// PolicySelector resolves the deterministic staged policy for one request.
type PolicySelector interface {
	Select(ctx context.Context, scene string, userID int64, requestID string) (*domainrecommendation.Policy, error)
}

// RankingFeatureSource performs all request-time feature lookups in bounded
// batch queries. Repositories that do not implement it retain the safe legacy
// feature subset during staged rollout.
type RankingFeatureSource interface {
	LoadRankingFeatures(ctx context.Context, userID int64, videoIDs []int64, since, now time.Time) (*domainrecommendation.RankingFeatures, error)
}

type ExposureInput struct {
	UserID    int64
	VideoID   int64
	Scene     string
	RequestID string
}

type ExposureDecisionInput struct {
	UserID    int64
	Scene     string
	RequestID string
	VideoIDs  []int64
}

type ExposureDecisionResult struct {
	UserID    int64
	Scene     string
	RequestID string
	Decisions []*domainrecommendation.ExposureDecision
}

type ExposureResult struct {
	Exposures []*domainrecommendation.Exposure
}

type FeedbackInput struct {
	UserID         int64
	VideoID        int64
	RequestID      string
	FeedbackType   string
	IdempotencyKey string
}

type FeedbackResult struct {
	Feedback *domainrecommendation.Feedback
	Replayed bool
}

type FeedbackVideo struct {
	VideoID  int64
	AuthorID int64
}

// FeedbackVideoCatalog verifies a feedback target and provides the durable
// author identity needed for author-scoped suppression.
type FeedbackVideoCatalog interface {
	GetFeedbackVideo(ctx context.Context, videoID int64) (*FeedbackVideo, error)
}

// FeedbackRequestEvidenceVerifier checks server-issued served-candidate
// evidence after a snapshot is unavailable or expired. It must bind user,
// request, and video; client-submitted view events are never evidence.
type FeedbackRequestEvidenceVerifier interface {
	HasServedCandidateEvidence(ctx context.Context, userID int64, requestID string, videoID int64, occurredAt time.Time) (bool, error)
}

type cursorPayload struct {
	RankScore   float64 `json:"rank_score"`
	PublishedAt string  `json:"published_at"`
	VideoID     int64   `json:"video_id"`
	RequestID   string  `json:"request_id,omitempty"`
}

func New(repo domainrecommendation.Repository, options ...Option) *Service {
	defaultPolicy := defaultRecallPolicy()
	service := &Service{
		repo:           repo,
		now:            func() time.Time { return time.Now().UTC() },
		recallBudgets:  defaultPolicy.RecallBudgets,
		recallDeadline: defaultPolicy.ProviderDeadlines,
		exposureWindow: defaultPolicy.ExposureWindow,
		recallSlots:    make(chan struct{}, defaultRecallProviderSlots),
	}
	if evidence, ok := repo.(domainrecommendation.ServedCandidateEvidenceRepository); ok {
		service.evidence = evidence
	}
	service.defaultPolicy = defaultRecommendationPolicy()
	for _, option := range options {
		option(service)
	}
	return service
}

func WithRecallProviders(providers ...RecallProvider) Option {
	return func(s *Service) {
		s.providers = append(s.providers, providers...)
	}
}

// WithRecallProviderSlots bounds provider calls that continue after their
// deadline because an implementation ignores cancellation.
func WithRecallProviderSlots(slots int) Option {
	return func(s *Service) {
		if slots > 0 {
			s.recallSlots = make(chan struct{}, slots)
		}
	}
}

func WithCandidateVisibilityFilter(filter CandidateVisibilityFilter) Option {
	return func(s *Service) {
		s.visibility = filter
	}
}

func WithPolicySelector(selector PolicySelector) Option {
	return func(s *Service) {
		s.policySelector = selector
	}
}

// WithSnapshotPagination enables the optional stable recommendation session
// path. Without both dependencies callers retain deterministic legacy cursors.
func WithSnapshotPagination(store SnapshotStore, signer SnapshotCursorSigner) Option {
	return func(s *Service) {
		s.snapshots = store
		s.cursorSigner = signer
	}
}

// WithRecallPolicy applies validated policy bounds to recall. Invalid or
// incomplete configurations leave the conservative defaults in place.
func WithRecallPolicy(config domainrecommendation.PolicyConfiguration) Option {
	return func(s *Service) {
		policy, err := domainrecommendation.NewPolicy("feed", 1, true, config, time.Unix(1, 0).UTC())
		if err != nil {
			return
		}
		budgets := make(map[string]int, len(policy.Config.RecallBudgets))
		deadlines := make(map[string]time.Duration, len(policy.Config.ProviderDeadlinesMS))
		for provider, budget := range policy.Config.RecallBudgets {
			budgets[provider] = budget
		}
		for provider, milliseconds := range policy.Config.ProviderDeadlinesMS {
			deadlines[provider] = time.Duration(milliseconds) * time.Millisecond
		}
		s.recallBudgets = budgets
		s.recallDeadline = deadlines
		s.exposureWindow = time.Duration(policy.Config.ExposureWindowHours) * time.Hour
		s.defaultPolicy = policy
	}
}

func WithNow(now func() time.Time) Option {
	return func(s *Service) {
		if now != nil {
			s.now = now
		}
	}
}

func WithRequestLogRepository(repo domainrecommendation.RequestLogRepository) Option {
	return func(s *Service) { s.requestLogs = repo }
}

func (s *Service) Recommend(ctx context.Context, input CandidateRequest) (*CandidateResult, error) {
	limit := normalizeLimit(input.Limit)
	scene := strings.ToLower(strings.TrimSpace(input.Scene))
	if isSnapshotCursor(input.Cursor) {
		return s.recommendSnapshotPage(ctx, input, scene, limit)
	}
	payload, err := parseCursorPayload(input.Cursor)
	if err != nil {
		return nil, err
	}
	requestID := strings.TrimSpace(input.RequestID)
	if requestID == "" && input.Context != nil {
		requestID = strings.TrimSpace(input.Context.RequestID)
	}
	if payload != nil && payload.RequestID != "" {
		if requestID != "" && requestID != payload.RequestID {
			return nil, domainrecommendation.ErrInvalidCursor
		}
		requestID = payload.RequestID
	}
	if requestID == "" {
		requestID, err = generatedRequestID()
		if err != nil {
			return nil, ErrLoadRecommendationFailed
		}
	}
	recommendationContext := input.Context
	if input.Context != nil && input.Context.RequestID != requestID {
		recommendationContext = input.Context.Clone()
		recommendationContext.RequestID = requestID
	}

	var cursor *domainrecommendation.Cursor
	if payload != nil {
		cursor, err = legacyCursorFromPayload(payload)
		if err != nil {
			return nil, domainrecommendation.ErrInvalidCursor
		}
	}
	req, err := domainrecommendation.NewCandidateRequest(input.UserID, scene, requestID, cursor, limit, recommendationContext)
	if err != nil {
		return nil, err
	}
	return s.recommendFreshPage(ctx, req, false)
}

func (s *Service) recommendFreshPage(ctx context.Context, req *domainrecommendation.CandidateRequest, forceDegraded bool) (*CandidateResult, error) {
	if req.Scene == "recommend" && req.Cursor == nil && s.snapshots != nil && s.cursorSigner != nil {
		snapshot, found, err := s.snapshots.LoadSnapshotForRequest(ctx, req.UserID, req.Scene, req.RequestID)
		if err == nil && found && s.now().UTC().Before(snapshot.ExpiresAt.UTC()) {
			inframetrics.ObserveRecommendationSnapshot("hit")
			return s.assembleSnapshotPage(ctx, snapshot, 0, req.Limit, nil)
		}
		if err != nil {
			inframetrics.ObserveRecommendationSnapshot("read_failure")
			forceDegraded = true
		}
	}
	policy, err := s.selectPolicy(ctx, req)
	if err != nil {
		return nil, err
	}
	inframetrics.ObserveRecommendationPolicy(req.Scene, policy.Version)

	poolLimit := candidatePoolLimit(req.Limit)
	recall, err := s.recallCandidates(ctx, req, poolLimit, policy)
	if err != nil {
		return nil, err
	}
	var pool []*domainrecommendation.Candidate
	if recall != nil {
		for _, item := range recall.degraded {
			inframetrics.ObserveRecommendationDegraded(item.Provider, item.Reason)
		}
	}
	if recall == nil || recall.healthy == 0 {
		pool, err = s.repo.ListCandidatePool(ctx, req.UserID, poolLimit)
		if err != nil {
			return nil, ErrLoadRecommendationFailed
		}
	} else {
		pool = recall.candidates
		for _, candidate := range pool {
			for _, reason := range candidate.RecallReasons {
				inframetrics.ObserveRecommendationRecall(reason.Provider, 1)
			}
		}
	}

	ranked, err := s.rankCandidates(ctx, req.UserID, req.Context, pool, policy)
	if err != nil {
		return nil, ErrLoadRecommendationFailed
	}
	ranked = diversifyCandidates(ranked, policy.Config.Diversity)
	ranked = filterByCursor(ranked, req.Cursor)

	degraded := forceDegraded || (recall != nil && len(recall.degraded) > 0)
	if req.Scene == "recommend" && req.Cursor == nil && s.snapshots != nil && s.cursorSigner != nil {
		ttl := time.Duration(policy.Config.SnapshotTTLSeconds) * time.Second
		snapshot := &Snapshot{
			ID:                snapshotID(req.UserID, req.Scene, req.RequestID, policy.Version),
			UserID:            req.UserID,
			Scene:             req.Scene,
			RequestID:         req.RequestID,
			PolicyVersion:     policy.Version,
			ExpiresAt:         s.now().Add(ttl).UTC(),
			Candidates:        cloneCandidates(ranked),
			Degraded:          degraded,
			DegradedProviders: degradedProvidersFromRecall(recall),
		}
		stored, created, storeErr := s.snapshots.CreateSnapshot(ctx, snapshot, ttl)
		if stored != nil && stored.UserID == req.UserID && stored.Scene == req.Scene && stored.RequestID == req.RequestID &&
			s.now().UTC().Before(stored.ExpiresAt.UTC()) {
			if storeErr != nil {
				inframetrics.ObserveRecommendationSnapshot("maintenance_failure")
			}
			if created {
				inframetrics.ObserveRecommendationSnapshot("write_success")
				s.recordRequestLog(ctx, req, policy, stored.Candidates, stored.Degraded, recall, true)
			} else {
				inframetrics.ObserveRecommendationSnapshot("hit")
			}
			return s.assembleSnapshotPage(ctx, stored, 0, req.Limit, nil)
		}
		inframetrics.ObserveRecommendationSnapshot("write_failure")
		degraded = true
	} else if req.Scene == "recommend" && req.Cursor == nil {
		inframetrics.ObserveRecommendationSnapshot("degraded_fallback")
		degraded = true
	}
	// The legacy/degraded score-cursor path must preserve the whole bounded
	// ranked session in its sampled request log; slicing below only controls
	// this response page. Cursor pages intentionally do not write another log.
	if req.Scene == domainrecommendation.RecommendationRequestLogScene && req.Cursor == nil {
		s.recordRequestLog(ctx, req, policy, ranked, degraded, recall, false)
	}

	hasMore := len(ranked) > req.Limit
	if hasMore {
		ranked = ranked[:req.Limit]
	}

	nextCursor := ""
	if len(ranked) > 0 {
		nextCursor = encodeCursor(&domainrecommendation.Cursor{
			RankScore:   ranked[len(ranked)-1].RankScore,
			PublishedAt: ranked[len(ranked)-1].PublishedAt,
			VideoID:     ranked[len(ranked)-1].VideoID,
		}, req.RequestID)
	}
	result := &CandidateResult{
		UserID:        req.UserID,
		Scene:         req.Scene,
		RequestID:     req.RequestID,
		Candidates:    ranked,
		NextCursor:    nextCursor,
		HasMore:       hasMore,
		Degraded:      degraded,
		PolicyVersion: policy.Version,
		DegradedProviders: func() []ProviderDegradation {
			return degradedProvidersFromRecall(recall)
		}(),
		DeliveryExpiresAt: s.servedCandidateEvidenceExpiry(policy),
	}
	return result, nil
}

func (s *Service) servedCandidateEvidenceExpiry(policy *domainrecommendation.Policy) time.Time {
	ttl := domainrecommendation.ServedCandidateEvidenceMinimumTTL
	if policy != nil {
		if configured := time.Duration(policy.Config.SnapshotTTLSeconds) * time.Second; configured > ttl {
			ttl = configured
		}
	}
	return s.now().UTC().Add(ttl)
}

// RecordDeliveredCandidates records only Feed items that completed final
// assembly. It is intentionally separate from ranking so candidates missing a
// card or current readability are never eligible for attribution.
func (s *Service) RecordDeliveredCandidates(ctx context.Context, input DeliveredCandidatesInput) error {
	if s.evidence == nil {
		return ErrSaveRecommendationEvidenceFailed
	}
	now := s.now().UTC()
	expiresAt := input.ExpiresAt.UTC()
	minimumExpiry := now.Add(domainrecommendation.ServedCandidateEvidenceMinimumTTL)
	if expiresAt.IsZero() || expiresAt.Before(minimumExpiry) {
		expiresAt = minimumExpiry
	}
	items := make([]domainrecommendation.ServedCandidateEvidenceItem, 0, len(input.VideoIDs))
	seen := make(map[int64]struct{}, len(input.VideoIDs))
	for _, videoID := range input.VideoIDs {
		if videoID <= 0 {
			continue
		}
		if _, exists := seen[videoID]; exists {
			continue
		}
		seen[videoID] = struct{}{}
		items = append(items, domainrecommendation.ServedCandidateEvidenceItem{
			VideoID: videoID, Position: len(items),
		})
	}
	if len(items) == 0 {
		return nil
	}
	evidence, err := domainrecommendation.NewServedCandidateEvidence(domainrecommendation.ServedCandidateEvidenceInput{
		UserID: input.UserID, RequestID: input.RequestID, Scene: domainrecommendation.RecommendationRequestLogScene, PolicyVersion: input.PolicyVersion,
		ServedAt: now, ExpiresAt: expiresAt, Candidates: items,
	})
	if err != nil {
		return ErrSaveRecommendationEvidenceFailed
	}
	if _, err := s.evidence.AppendServedCandidateEvidence(ctx, evidence); err != nil {
		return ErrSaveRecommendationEvidenceFailed
	}
	return nil
}

func (s *Service) recordRequestLog(ctx context.Context, req *domainrecommendation.CandidateRequest, policy *domainrecommendation.Policy, candidates []*domainrecommendation.Candidate, degraded bool, recall *recallExecution, snapshot bool) {
	if s.requestLogs == nil || req == nil || policy == nil || req.UserID <= 0 || !domainrecommendation.ShouldSampleRequestLog(
		domainrecommendation.RequestLogControl{SamplingRatePPM: policy.Config.SamplingRatePPM, RetentionDays: policy.Config.RetentionDays},
		req.UserID, req.Scene, req.RequestID,
	) {
		return
	}
	providers := make([]string, 0)
	if recall != nil {
		for _, item := range recall.degraded {
			providers = append(providers, item.Provider+":"+item.Reason)
		}
	}
	log, err := domainrecommendation.NewRecommendationRequestLog(domainrecommendation.RequestLogInput{
		RequestID: req.RequestID, UserID: req.UserID, Scene: req.Scene, PolicyVersion: policy.Version,
		Context: req.Context, Candidates: domainrecommendation.LoggedCandidatesFromRanked(candidates),
		Degraded: degraded, Snapshot: snapshot, DegradedProviders: providers, CreatedAt: s.now(),
	})
	if err != nil {
		inframetrics.ObserveRecommendationRequestLogFailure("validation")
		return
	}
	if _, _, err := s.requestLogs.SaveRequestLog(ctx, log); err != nil {
		inframetrics.ObserveRecommendationRequestLogFailure("storage")
	}
}

func degradedProvidersFromRecall(recall *recallExecution) []ProviderDegradation {
	if recall == nil {
		return nil
	}
	return append([]ProviderDegradation(nil), recall.degraded...)
}

func (s *Service) recommendSnapshotPage(ctx context.Context, input CandidateRequest, scene string, limit int) (*CandidateResult, error) {
	if s.cursorSigner == nil {
		return nil, domainrecommendation.ErrInvalidCursor
	}
	payload, err := s.cursorSigner.VerifySnapshotCursor(strings.TrimSpace(input.Cursor), input.UserID, scene, requestIDFromInput(input), s.now())
	if err != nil {
		return nil, err
	}
	if s.snapshots != nil {
		snapshot, found, loadErr := s.snapshots.LoadSnapshot(ctx, payload.SnapshotID)
		if loadErr == nil && found {
			inframetrics.ObserveRecommendationSnapshot("hit")
			if !snapshotMatchesCursor(snapshot, payload) {
				return nil, domainrecommendation.ErrInvalidCursor
			}
			return s.assembleSnapshotPage(ctx, snapshot, payload.Offset, limit, payload.Fallback)
		}
		if loadErr == nil && !found && !s.now().UTC().Before(time.Unix(0, payload.ExpiresAt).UTC()) {
			inframetrics.ObserveRecommendationSnapshot("miss")
			return nil, domainrecommendation.ErrInvalidCursor
		}
	}
	if payload.Fallback == nil {
		return nil, domainrecommendation.ErrInvalidCursor
	}
	inframetrics.ObserveRecommendationSnapshot("degraded_fallback")
	fallback, err := legacyCursorFromPayload(payload.Fallback)
	if err != nil {
		return nil, domainrecommendation.ErrInvalidCursor
	}
	req, err := domainrecommendation.NewCandidateRequest(input.UserID, scene, payload.RequestID, fallback, limit, input.Context)
	if err != nil {
		return nil, err
	}
	return s.recommendFreshPage(ctx, req, true)
}

func (s *Service) assembleSnapshotPage(ctx context.Context, snapshot *Snapshot, offset int, limit int, fallback *cursorPayload) (*CandidateResult, error) {
	if snapshot == nil || offset < 0 || offset > len(snapshot.Candidates) || !s.now().UTC().Before(snapshot.ExpiresAt.UTC()) {
		return nil, domainrecommendation.ErrInvalidCursor
	}
	remaining := snapshot.Candidates[offset:]
	visible, err := s.visibleSnapshotCandidates(ctx, remaining)
	if err != nil {
		return nil, ErrLoadRecommendationFailed
	}
	visible, err = s.applySnapshotFeedbackSuppression(ctx, snapshot.UserID, visible)
	if err != nil {
		return nil, ErrLoadRecommendationFailed
	}
	items := make([]*domainrecommendation.Candidate, 0, limit)
	nextOffset := len(snapshot.Candidates)
	var last *domainrecommendation.Candidate
	for index, candidate := range remaining {
		current := visible[candidate.VideoID]
		if current == nil {
			continue
		}

		item := candidate.Clone()
		item.AuthorID = current.AuthorID
		item.PublishedAt = current.PublishedAt
		item.HotScore = current.HotScore
		items = append(items, item)
		last = item
		if len(items) == limit {
			nextOffset = offset + index + 1
			break
		}
	}
	hasMore := false
	if len(items) == limit {
		for _, candidate := range snapshot.Candidates[nextOffset:] {
			if visible[candidate.VideoID] != nil {
				hasMore = true
				break
			}
		}
	}
	nextCursor := ""
	if hasMore && last != nil {
		nextCursor, err = s.cursorSigner.SignSnapshotCursor(snapshotCursorPayload{
			Version: snapshotCursorVersion, SnapshotID: snapshot.ID, UserID: snapshot.UserID,
			Scene: snapshot.Scene, RequestID: snapshot.RequestID, PolicyVersion: snapshot.PolicyVersion,
			Offset: nextOffset, ExpiresAt: snapshot.ExpiresAt.UTC().UnixNano(),
			Fallback: &cursorPayload{RankScore: last.RankScore, PublishedAt: last.PublishedAt.UTC().Format(time.RFC3339Nano), VideoID: last.VideoID},
		})
		if err != nil {
			return nil, domainrecommendation.ErrInvalidCursor
		}
	}
	result := &CandidateResult{
		UserID: snapshot.UserID, Scene: snapshot.Scene, RequestID: snapshot.RequestID, Candidates: items,
		NextCursor: nextCursor, HasMore: hasMore, Degraded: snapshot.Degraded, PolicyVersion: snapshot.PolicyVersion,
		DeliveryExpiresAt: snapshot.ExpiresAt,
	}
	result.DegradedProviders = append([]ProviderDegradation(nil), snapshot.DegradedProviders...)
	return result, nil
}

// applySnapshotFeedbackSuppression deliberately does not use
// MinimumFallbackPool. A snapshot is a stable ordering optimization, not a
// license to return content the user actively suppressed after it was created.
func (s *Service) applySnapshotFeedbackSuppression(ctx context.Context, userID int64, visible map[int64]*domainrecommendation.Candidate) (map[int64]*domainrecommendation.Candidate, error) {
	if len(visible) == 0 {
		return visible, nil
	}
	videoIDs := make([]int64, 0, len(visible))
	for videoID := range visible {
		videoIDs = append(videoIDs, videoID)
	}
	features, err := s.loadRankingFeatures(ctx, userID, videoIDs, s.exposureWindow)
	if err != nil {
		return nil, err
	}
	for videoID, candidate := range visible {
		if candidate != nil && (features.SuppressedVideos[videoID] || features.SuppressedAuthors[candidate.AuthorID]) {
			delete(visible, videoID)
		}
	}
	return visible, nil
}

func (s *Service) visibleSnapshotCandidates(ctx context.Context, candidates []*domainrecommendation.Candidate) (map[int64]*domainrecommendation.Candidate, error) {
	visible := make(map[int64]*domainrecommendation.Candidate, len(candidates))
	if s.visibility == nil {
		for _, candidate := range candidates {
			if candidate != nil {
				visible[candidate.VideoID] = candidate
			}
		}
		return visible, nil
	}
	current, err := s.visibility.ListVisibleCandidates(ctx, candidateIDs(candidates))
	if err != nil {
		return nil, err
	}
	for _, candidate := range current {
		if candidate != nil && candidate.VideoID > 0 {
			visible[candidate.VideoID] = candidate
		}
	}
	return visible, nil
}

func isSnapshotCursor(raw string) bool {
	return strings.HasPrefix(strings.TrimSpace(raw), snapshotCursorVersion+".")
}

func requestIDFromInput(input CandidateRequest) string {
	requestID := strings.TrimSpace(input.RequestID)
	if requestID == "" && input.Context != nil {
		requestID = strings.TrimSpace(input.Context.RequestID)
	}
	return requestID
}

func snapshotMatchesCursor(snapshot *Snapshot, payload *snapshotCursorPayload) bool {
	return snapshot != nil && payload != nil && snapshot.ID == payload.SnapshotID &&
		snapshot.UserID == payload.UserID && snapshot.Scene == payload.Scene &&
		snapshot.RequestID == payload.RequestID && snapshot.PolicyVersion == payload.PolicyVersion &&
		snapshot.ExpiresAt.UTC().UnixNano() == payload.ExpiresAt && len(snapshot.Candidates) <= maxSnapshotCandidates
}

func legacyCursorFromPayload(payload *cursorPayload) (*domainrecommendation.Cursor, error) {
	if payload == nil {
		return nil, nil
	}
	publishedAt, err := time.Parse(time.RFC3339Nano, payload.PublishedAt)
	if err != nil {
		return nil, err
	}
	cursor := &domainrecommendation.Cursor{RankScore: payload.RankScore, PublishedAt: publishedAt, VideoID: payload.VideoID}
	if !cursor.Valid() {
		return nil, domainrecommendation.ErrInvalidCursor
	}
	return cursor, nil
}

func (s *Service) selectPolicy(ctx context.Context, req *domainrecommendation.CandidateRequest) (*domainrecommendation.Policy, error) {
	if s.policySelector == nil {
		return s.defaultPolicy.Clone(), nil
	}
	policy, err := s.policySelector.Select(ctx, req.Scene, req.UserID, req.RequestID)
	if errors.Is(err, domainrecommendation.ErrPolicyNotFound) {
		return s.defaultPolicy.Clone(), nil
	}
	if err != nil || policy == nil {
		return nil, ErrLoadRecommendationFailed
	}
	return policy, nil
}

func (s *Service) recallBudget(provider string, policies ...*domainrecommendation.Policy) (int, time.Duration, bool) {
	if len(policies) > 0 && policies[0] != nil {
		config := policies[0].Config
		budget, budgetOK := config.RecallBudgets[strings.TrimSpace(provider)]
		deadlineMS, deadlineOK := config.ProviderDeadlinesMS[strings.TrimSpace(provider)]
		return budget, time.Duration(deadlineMS) * time.Millisecond, budgetOK && deadlineOK && budget > 0 && deadlineMS > 0
	}
	budget, budgetOK := s.recallBudgets[strings.TrimSpace(provider)]
	deadline, deadlineOK := s.recallDeadline[strings.TrimSpace(provider)]
	return budget, deadline, budgetOK && deadlineOK && budget > 0 && deadline > 0
}

func defaultRecallBudgets() map[string]int {
	return map[string]int{
		domainrecommendation.RecallProviderFresh:               100,
		domainrecommendation.RecallProviderHot:                 100,
		domainrecommendation.RecallProviderContentSimilarity:   100,
		domainrecommendation.RecallProviderFollowedAuthor:      100,
		domainrecommendation.RecallProviderSessionContinuation: 100,
	}
}

func defaultRecallDeadlines() map[string]time.Duration {
	return map[string]time.Duration{
		domainrecommendation.RecallProviderFresh:               150 * time.Millisecond,
		domainrecommendation.RecallProviderHot:                 150 * time.Millisecond,
		domainrecommendation.RecallProviderContentSimilarity:   250 * time.Millisecond,
		domainrecommendation.RecallProviderFollowedAuthor:      200 * time.Millisecond,
		domainrecommendation.RecallProviderSessionContinuation: 250 * time.Millisecond,
	}
}

type recallPolicyDefaults struct {
	RecallBudgets     map[string]int
	ProviderDeadlines map[string]time.Duration
	ExposureWindow    time.Duration
}

func defaultRecallPolicy() recallPolicyDefaults {
	config := defaultRecommendationPolicyConfiguration()
	policy, err := domainrecommendation.NewPolicy("feed", 1, true, config, time.Unix(1, 0).UTC())
	if err != nil {
		return recallPolicyDefaults{
			RecallBudgets: defaultRecallBudgets(), ProviderDeadlines: defaultRecallDeadlines(),
			ExposureWindow: domainrecommendation.RecentExposureWindow,
		}
	}
	deadlines := make(map[string]time.Duration, len(policy.Config.ProviderDeadlinesMS))
	for provider, milliseconds := range policy.Config.ProviderDeadlinesMS {
		deadlines[provider] = time.Duration(milliseconds) * time.Millisecond
	}
	return recallPolicyDefaults{
		RecallBudgets: policy.Config.RecallBudgets, ProviderDeadlines: deadlines,
		ExposureWindow: time.Duration(policy.Config.ExposureWindowHours) * time.Hour,
	}
}

func defaultRecommendationPolicy() *domainrecommendation.Policy {
	policy, err := domainrecommendation.NewPolicy("feed", 1, true, defaultRecommendationPolicyConfiguration(), time.Unix(1, 0).UTC())
	if err != nil {
		panic("invalid built-in recommendation policy")
	}
	return policy
}

func defaultRecommendationPolicyConfiguration() domainrecommendation.PolicyConfiguration {
	return domainrecommendation.InitialRecommendationPolicyConfiguration()
}

func (s *Service) SubmitFeedback(ctx context.Context, input FeedbackInput) (*FeedbackResult, error) {
	feedback, err := domainrecommendation.NewFeedback(
		input.UserID,
		input.VideoID,
		input.RequestID,
		input.FeedbackType,
		input.IdempotencyKey,
		s.now(),
	)
	if err != nil {
		return nil, err
	}
	existing, err := s.repo.FindFeedbackByUserAndIdempotencyKey(ctx, feedback.UserID, feedback.IdempotencyKey)
	if err == nil {
		if !existing.SameNormalizedPayload(feedback) {
			return nil, domainrecommendation.ErrFeedbackIdempotencyConflict
		}
		return &FeedbackResult{Feedback: existing, Replayed: true}, nil
	}
	if !errors.Is(err, domainrecommendation.ErrFeedbackNotFound) {
		return nil, ErrSaveRecommendationFeedbackFailed
	}
	if err := s.verifyFeedbackRequestAssociation(ctx, feedback); err != nil {
		return nil, err
	}
	catalog, ok := s.repo.(FeedbackVideoCatalog)
	if !ok {
		return nil, ErrSaveRecommendationFeedbackFailed
	}
	video, err := catalog.GetFeedbackVideo(ctx, feedback.VideoID)
	if err != nil {
		if errors.Is(err, domainrecommendation.ErrVideoNotFound) {
			return nil, domainrecommendation.ErrVideoNotFound
		}
		return nil, ErrSaveRecommendationFeedbackFailed
	}
	if video == nil || video.VideoID != feedback.VideoID || video.AuthorID <= 0 {
		return nil, domainrecommendation.ErrVideoNotFound
	}
	policy := s.defaultPolicy
	if selected, selectErr := s.selectPolicy(ctx, &domainrecommendation.CandidateRequest{
		UserID: input.UserID, Scene: "recommend", RequestID: input.RequestID,
	}); selectErr == nil && selected != nil {
		policy = selected
	}
	hours := policy.Config.SuppressionHours[feedback.FeedbackType]
	if hours <= 0 {
		hours = 24
	}
	scope := domainrecommendation.SuppressionScopeVideo
	scopeID := feedback.VideoID
	if feedback.FeedbackType == domainrecommendation.FeedbackTypeReduceAuthor {
		scope = domainrecommendation.SuppressionScopeAuthor
		scopeID = video.AuthorID
	}
	if err := feedback.SetSuppression(scope, scopeID, feedback.CreatedAt.Add(time.Duration(hours)*time.Hour)); err != nil {
		return nil, err
	}
	saved, replayed, err := s.repo.SaveFeedback(ctx, feedback)
	if err != nil {
		if errors.Is(err, domainrecommendation.ErrFeedbackIdempotencyConflict) ||
			errors.Is(err, domainrecommendation.ErrVideoNotFound) {
			return nil, err
		}
		return nil, ErrSaveRecommendationFeedbackFailed
	}
	if !replayed {
		inframetrics.ObserveRecommendationOutcome(saved.FeedbackType)
	}
	return &FeedbackResult{Feedback: saved, Replayed: replayed}, nil
}

func (s *Service) verifyFeedbackRequestAssociation(ctx context.Context, feedback *domainrecommendation.Feedback) error {
	if feedback == nil {
		return domainrecommendation.ErrFeedbackRequestMismatch
	}
	verifier, ok := s.repo.(FeedbackRequestEvidenceVerifier)
	if !ok {
		return domainrecommendation.ErrFeedbackRequestMismatch
	}
	valid, err := verifier.HasServedCandidateEvidence(ctx, feedback.UserID, feedback.RequestID, feedback.VideoID, feedback.CreatedAt)
	if err != nil {
		return ErrLoadRecommendationFailed
	}
	if !valid {
		return domainrecommendation.ErrFeedbackRequestMismatch
	}
	return nil
}

func (s *Service) DecideExposures(ctx context.Context, input ExposureDecisionInput) (*ExposureDecisionResult, error) {
	req, err := domainrecommendation.NewExposureDecisionRequest(input.UserID, input.Scene, input.RequestID, input.VideoIDs)
	if err != nil {
		return nil, err
	}
	if len(req.VideoIDs) == 0 {
		return &ExposureDecisionResult{
			UserID:    req.UserID,
			Scene:     req.Scene,
			RequestID: req.RequestID,
			Decisions: []*domainrecommendation.ExposureDecision{},
		}, nil
	}

	policy, err := s.selectPolicy(ctx, &domainrecommendation.CandidateRequest{
		UserID: req.UserID, Scene: req.Scene, RequestID: req.RequestID,
	})
	if err != nil {
		return nil, ErrLoadExposureDecisionsFailed
	}
	exposureWindow := time.Duration(policy.Config.ExposureWindowHours) * time.Hour
	exposures, err := s.repo.ListRecentExposures(ctx, req.UserID, req.VideoIDs, s.now().Add(-exposureWindow))
	if err != nil {
		return nil, ErrLoadExposureDecisionsFailed
	}
	exposureByVideoID := make(map[int64]*domainrecommendation.Exposure, len(exposures))
	for _, exposure := range exposures {
		if exposure != nil {
			exposureByVideoID[exposure.VideoID] = exposure
		}
	}

	decisions := make([]*domainrecommendation.ExposureDecision, 0, len(req.VideoIDs))
	for _, videoID := range req.VideoIDs {
		if exposure := exposureByVideoID[videoID]; exposure != nil {
			decisions = append(decisions, domainrecommendation.RestoreExposureDecision(
				videoID,
				false,
				domainrecommendation.ExposureDecisionReasonRecentlyExposed,
				&exposure.LastExposedAt,
			))
			continue
		}
		decisions = append(decisions, domainrecommendation.RestoreExposureDecision(
			videoID,
			true,
			domainrecommendation.ExposureDecisionReasonFresh,
			nil,
		))
	}
	return &ExposureDecisionResult{
		UserID:    req.UserID,
		Scene:     req.Scene,
		RequestID: req.RequestID,
		Decisions: decisions,
	}, nil
}

func (s *Service) SaveExposures(ctx context.Context, inputs []ExposureInput) (*ExposureResult, error) {
	writes := make([]*domainrecommendation.ExposureWrite, 0, len(inputs))
	seen := map[int64]struct{}{}
	for _, input := range inputs {
		write, err := domainrecommendation.NewExposureWrite(input.UserID, input.VideoID, input.Scene, input.RequestID)
		if err != nil {
			return nil, err
		}
		if _, exists := seen[write.VideoID]; exists {
			continue
		}
		seen[write.VideoID] = struct{}{}
		writes = append(writes, write)
	}
	if len(writes) == 0 {
		return &ExposureResult{Exposures: []*domainrecommendation.Exposure{}}, nil
	}

	exposures, err := s.repo.SaveExposures(ctx, writes)
	if err != nil {
		if errors.Is(err, domainrecommendation.ErrVideoNotFound) {
			return nil, err
		}
		return nil, ErrSaveRecommendationExposureFailed
	}
	return &ExposureResult{Exposures: exposures}, nil
}

func (s *Service) rankCandidates(ctx context.Context, userID int64, recommendationContext *domainrecommendation.RecommendationContext, pool []*domainrecommendation.Candidate, policies ...*domainrecommendation.Policy) ([]*domainrecommendation.Candidate, error) {
	if len(pool) == 0 {
		return []*domainrecommendation.Candidate{}, nil
	}
	policy := s.defaultPolicy
	if len(policies) > 0 && policies[0] != nil {
		policy = policies[0]
	}

	videoIDs := make([]int64, 0, len(pool))
	for _, candidate := range pool {
		if candidate != nil && candidate.VideoID > 0 {
			videoIDs = append(videoIDs, candidate.VideoID)
		}
	}
	vectorIDs := append([]int64(nil), videoIDs...)
	vectorIDs = append(vectorIDs, sessionSeedIDs(recommendationContext)...)
	vectors, err := s.repo.LoadVideoVectors(ctx, uniqueVideoIDs(vectorIDs))
	if err != nil {
		return nil, err
	}
	features, err := s.loadRankingFeatures(ctx, userID, videoIDs, time.Duration(policy.Config.ExposureWindowHours)*time.Hour)
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	profileConfidence := 1.0
	negativeVectorConfidence := 1.0
	if profile := features.Profile; profile != nil {
		factors := profile.DecayFactors(now, profileDecayForPolicy(policy))
		if len(profile.RecentVector) > 0 {
			profileConfidence = factors.Recent
		} else if len(profile.LongTermVector) > 0 {
			profileConfidence = factors.LongTerm
		}
		negativeVectorConfidence = factors.Recent
		features.Profile = profile.DecayTo(now, profileDecayForPolicy(policy))
	}
	userVector, hasUserVector, err := s.loadRankingInterestVector(ctx, userID, features.Profile)
	if err != nil {
		return nil, err
	}
	sessionVector := averageVector(vectors, sessionSeedIDs(recommendationContext))

	ranked := make([]*domainrecommendation.Candidate, 0, len(pool))
	for _, candidate := range pool {
		if candidate == nil {
			continue
		}
		value := *candidate
		vector := vectors[value.VideoID]
		value.FreshnessScore = freshnessScoreWithHalfLife(now, value.PublishedAt, policy.Config.FreshnessHalfLifeHours)
		value.Similarity = boundedFloat(normalizedCosine(userVector, vector, hasUserVector) * profileConfidence)
		components := map[string]float64{
			domainrecommendation.FeatureContentSimilarity: value.Similarity,
			domainrecommendation.FeatureSessionSimilarity: normalizedCosine(sessionVector, vector, len(sessionVector) > 0),
			domainrecommendation.FeatureHotness:           normalizedHotness(value.HotScore, pool),
			domainrecommendation.FeatureFreshness:         value.FreshnessScore,
			domainrecommendation.FeatureAuthorAffinity:    normalizedAffinity(features.Profile, value.AuthorID, false),
			domainrecommendation.FeatureFollowRelation:    boolFeature(features.FollowedAuthors[value.AuthorID]),
			domainrecommendation.FeatureNegativePenalty:   negativePenalty(features, vector, value, negativeVectorConfidence),
			domainrecommendation.FeatureExposurePenalty:   exposurePenalty(features.RecentExposures[value.VideoID]),
		}
		value.ScoreComponents = sanitizeComponents(components)
		value.RankScore = policyScore(value.ScoreComponents, policy.Config.FeatureWeights)
		value.PolicyVersion = policy.Version
		value.Reason = recommendationReason(hasUserVector, value.Similarity, value.HotScore)
		ranked = append(ranked, &value)
	}

	sortCandidates(ranked)
	ranked = applyFeedbackSuppression(ranked, features, policy.Config)
	return applyExposureSuppression(ranked, features.RecentExposures, policy.Config), nil
}

func (s *Service) loadRankingFeatures(ctx context.Context, userID int64, videoIDs []int64, exposureWindow time.Duration) (*domainrecommendation.RankingFeatures, error) {
	empty := &domainrecommendation.RankingFeatures{
		FollowedAuthors: map[int64]bool{}, RecentExposures: map[int64]*domainrecommendation.Exposure{},
		NegativeVideos: map[int64]bool{}, NegativeAuthors: map[int64]bool{},
		SuppressedVideos: map[int64]bool{}, SuppressedAuthors: map[int64]bool{},
	}
	source, ok := s.repo.(RankingFeatureSource)
	if !ok {
		exposures, err := s.repo.ListRecentExposures(ctx, userID, videoIDs, s.now().Add(-exposureWindow))
		if err != nil {
			return nil, err
		}
		for _, exposure := range exposures {
			if exposure != nil {
				empty.RecentExposures[exposure.VideoID] = exposure
			}
		}
		return empty, nil
	}
	features, err := source.LoadRankingFeatures(ctx, userID, videoIDs, s.now().Add(-exposureWindow), s.now())
	if err != nil {
		return nil, err
	}
	if features == nil {
		return empty, nil
	}
	if features.FollowedAuthors == nil {
		features.FollowedAuthors = empty.FollowedAuthors
	}
	if features.RecentExposures == nil {
		features.RecentExposures = empty.RecentExposures
	}
	if features.NegativeVideos == nil {
		features.NegativeVideos = empty.NegativeVideos
	}
	if features.NegativeAuthors == nil {
		features.NegativeAuthors = empty.NegativeAuthors
	}
	if features.SuppressedVideos == nil {
		features.SuppressedVideos = map[int64]bool{}
	}
	if features.SuppressedAuthors == nil {
		features.SuppressedAuthors = map[int64]bool{}
	}
	return features, nil
}

func (s *Service) loadRankingInterestVector(ctx context.Context, userID int64, profile *domainrecommendation.UserInterestProfile) ([]float64, bool, error) {
	if profile != nil {
		if len(profile.RecentVector) > 0 {
			return append([]float64(nil), profile.RecentVector...), true, nil
		}
		if len(profile.LongTermVector) > 0 {
			return append([]float64(nil), profile.LongTermVector...), true, nil
		}
	}
	return loadProfileInterestVector(ctx, s.repo, userID)
}

// ProfileLoader is optional during the staged rollout. A materialized profile
// wins; durable-fact reconstruction runs only when it is absent.
type ProfileLoader interface {
	LoadUserInterestProfile(ctx context.Context, userID int64) (*domainrecommendation.UserInterestProfile, bool, error)
	RebuildUserInterestVector(ctx context.Context, userID int64) ([]float64, bool, error)
}

func loadProfileInterestVector(ctx context.Context, repo domainrecommendation.Repository, userID int64) ([]float64, bool, error) {
	loader, ok := repo.(ProfileLoader)
	if !ok {
		return repo.LoadUserInterestVector(ctx, userID)
	}
	profile, found, err := loader.LoadUserInterestProfile(ctx, userID)
	if err != nil || !found || profile == nil {
		if err != nil {
			return nil, false, err
		}
		return loader.RebuildUserInterestVector(ctx, userID)
	}
	if len(profile.RecentVector) > 0 {
		return append([]float64(nil), profile.RecentVector...), true, nil
	}
	if len(profile.LongTermVector) > 0 {
		return append([]float64(nil), profile.LongTermVector...), true, nil
	}
	return []float64{}, false, nil
}

func normalizeLimit(limit int) int {
	if limit <= 0 {
		return defaultLimit
	}
	if limit > domainrecommendation.MaxLimit {
		return domainrecommendation.MaxLimit
	}
	return limit
}

func candidatePoolLimit(limit int) int {
	poolLimit := limit * candidatePoolMultiplier
	if poolLimit < minCandidatePoolSize {
		poolLimit = minCandidatePoolSize
	}
	if poolLimit > maxCandidatePoolSize {
		poolLimit = maxCandidatePoolSize
	}
	return poolLimit
}

func freshnessScore(now time.Time, publishedAt time.Time) float64 {
	return freshnessScoreWithHalfLife(now, publishedAt, 72)
}

func freshnessScoreWithHalfLife(now time.Time, publishedAt time.Time, halfLifeHours int) float64 {
	if publishedAt.IsZero() {
		return 0
	}
	hours := now.Sub(publishedAt).Hours()
	if hours < 0 {
		hours = 0
	}
	if halfLifeHours <= 0 {
		return 0
	}
	return boundedFloat(math.Exp(-math.Ln2 * hours / float64(halfLifeHours)))
}

func recommendationReason(hasUserVector bool, similarity float64, hotScore int) string {
	if hasUserVector && similarity > 0.05 {
		return "interest_match"
	}
	if hotScore > 0 {
		return "hot"
	}
	return "fresh"
}

func sortCandidates(candidates []*domainrecommendation.Candidate) {
	sort.SliceStable(candidates, func(i, j int) bool {
		left := candidates[i]
		right := candidates[j]
		if left.RankScore != right.RankScore {
			return left.RankScore > right.RankScore
		}
		if !left.PublishedAt.Equal(right.PublishedAt) {
			return left.PublishedAt.After(right.PublishedAt)
		}
		return left.VideoID > right.VideoID
	})
}

func normalizedCosine(left []float64, right []float64, enabled bool) float64 {
	if !enabled || len(left) == 0 || len(right) == 0 {
		return 0
	}
	value, err := domainembedding.CosineSimilarity(left, right)
	if err != nil {
		return 0
	}
	return boundedFloat(maxFloat(0, value))
}

func normalizedHotness(hotScore int, candidates []*domainrecommendation.Candidate) float64 {
	maximum := 0
	for _, candidate := range candidates {
		if candidate != nil && candidate.HotScore > maximum {
			maximum = candidate.HotScore
		}
	}
	if maximum <= 0 || hotScore <= 0 {
		return 0
	}
	return boundedFloat(math.Log1p(float64(hotScore)) / math.Log1p(float64(maximum)))
}

func normalizedAffinity(profile *domainrecommendation.UserInterestProfile, authorID int64, negative bool) float64 {
	if profile == nil || authorID <= 0 {
		return 0
	}
	affinities := profile.AuthorAffinities
	if negative {
		affinities = profile.NegativeAuthorAffinities
	}
	return boundedFloat(affinities[authorID] / domainrecommendation.MaxProfileComponentWeight)
}

func negativePenalty(features *domainrecommendation.RankingFeatures, vector []float64, candidate domainrecommendation.Candidate, vectorConfidence float64) float64 {
	if features == nil {
		return 0
	}
	penalty := normalizedAffinity(features.Profile, candidate.AuthorID, true)
	if features.Profile != nil {
		penalty = maxFloat(penalty, normalizedCosine(features.Profile.NegativeTopicVector, vector, true)*boundedFloat(vectorConfidence))
	}
	if features.NegativeVideos[candidate.VideoID] || features.NegativeAuthors[candidate.AuthorID] {
		penalty = 1
	}
	return boundedFloat(penalty)
}

func profileDecayForPolicy(policy *domainrecommendation.Policy) domainrecommendation.ProfileDecay {
	if policy == nil {
		return domainrecommendation.DefaultProfileDecay()
	}
	return domainrecommendation.ProfileDecay{
		LongTermHalfLife: time.Duration(policy.Config.ProfileLongTermHalfLifeHours) * time.Hour,
		RecentHalfLife:   time.Duration(policy.Config.ProfileRecentHalfLifeHours) * time.Hour,
	}.Normalized()
}

func exposurePenalty(exposure *domainrecommendation.Exposure) float64 {
	if exposure == nil || exposure.ExposureCount <= 0 {
		return 0
	}
	return boundedFloat(1 - math.Exp(-float64(exposure.ExposureCount)))
}

func sanitizeComponents(components map[string]float64) map[string]float64 {
	sanitized := make(map[string]float64, len(components))
	for name, value := range components {
		sanitized[name] = boundedFloat(value)
	}
	return sanitized
}

func policyScore(components map[string]float64, weights map[string]float64) float64 {
	var score float64
	for feature, weight := range weights {
		component := boundedFloat(components[feature])
		if math.IsNaN(weight) || math.IsInf(weight, 0) {
			continue
		}
		score += component * weight
	}
	if math.IsNaN(score) || math.IsInf(score, 0) {
		return 0
	}
	return score
}

func applyExposureSuppression(candidates []*domainrecommendation.Candidate, exposures map[int64]*domainrecommendation.Exposure, config domainrecommendation.PolicyConfiguration) []*domainrecommendation.Candidate {
	if !config.HardSuppressExposures || len(exposures) == 0 {
		return candidates
	}
	allowed := make([]*domainrecommendation.Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate != nil && exposures[candidate.VideoID] == nil {
			allowed = append(allowed, candidate)
		}
	}
	minimum := config.MinimumFallbackPool
	if minimum > len(candidates) {
		minimum = len(candidates)
	}
	if len(allowed) < minimum {
		return candidates
	}
	return allowed
}

func diversifyCandidates(candidates []*domainrecommendation.Candidate, rules domainrecommendation.DiversityRules) []*domainrecommendation.Candidate {
	if len(candidates) < 2 {
		return candidates
	}
	remaining := append([]*domainrecommendation.Candidate(nil), candidates...)
	output := make([]*domainrecommendation.Candidate, 0, len(candidates))
	authorCount := map[int64]int{}
	for len(remaining) > 0 {
		index := diversityCandidateIndex(remaining, output, authorCount, rules, true)
		if index < 0 {
			index = diversityCandidateIndex(remaining, output, authorCount, rules, false)
		}
		if index < 0 {
			// The pool cannot satisfy caps (for example, one-author cold start).
			// Preserve the established score/time/ID order as bounded fallback.
			output = append(output, remaining...)
			break
		}
		candidate := remaining[index]
		output = append(output, candidate)
		authorCount[candidate.AuthorID]++
		remaining = append(remaining[:index], remaining[index+1:]...)
	}
	return output
}

func diversityCandidateIndex(remaining []*domainrecommendation.Candidate, output []*domainrecommendation.Candidate, authorCount map[int64]int, rules domainrecommendation.DiversityRules, enforceGaps bool) int {
	for index, candidate := range remaining {
		if candidate == nil || authorCount[candidate.AuthorID] >= rules.MaxPerAuthor {
			continue
		}
		if enforceGaps && (recentAuthor(output, candidate.AuthorID, rules.MinAuthorGap) || recentContentBucket(output, contentBucket(candidate), rules.MinContentGap)) {
			continue
		}
		return index
	}
	return -1
}

func recentAuthor(candidates []*domainrecommendation.Candidate, authorID int64, gap int) bool {
	for index := len(candidates) - 1; index >= 0 && len(candidates)-index <= gap; index-- {
		if candidates[index].AuthorID == authorID {
			return true
		}
	}
	return false
}

func recentContentBucket(candidates []*domainrecommendation.Candidate, bucket string, gap int) bool {
	if gap <= 0 || bucket == "" {
		return false
	}
	for index := len(candidates) - 1; index >= 0 && len(candidates)-index <= gap; index-- {
		if contentBucket(candidates[index]) == bucket {
			return true
		}
	}
	return false
}

func contentBucket(candidate *domainrecommendation.Candidate) string {
	if candidate == nil || len(candidate.RecallReasons) == 0 {
		return ""
	}
	// Recall provider names form a bounded, explainable content bucket without
	// exposing vectors or adding another unbounded feature-store lookup.
	bucket := candidate.RecallReasons[0].Provider
	for _, reason := range candidate.RecallReasons[1:] {
		if reason.Provider < bucket {
			bucket = reason.Provider
		}
	}
	return bucket
}

func uniqueVideoIDs(ids []int64) []int64 {
	unique := make([]int64, 0, len(ids))
	seen := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		if id > 0 {
			if _, exists := seen[id]; !exists {
				seen[id] = struct{}{}
				unique = append(unique, id)
			}
		}
	}
	return unique
}

func boundedFloat(value float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) || value <= 0 {
		return 0
	}
	if value >= 1 {
		return 1
	}
	return value
}

func boolFeature(value bool) float64 {
	if value {
		return 1
	}
	return 0
}

func maxFloat(left, right float64) float64 {
	if left > right {
		return left
	}
	return right
}

func filterByCursor(candidates []*domainrecommendation.Candidate, cursor *domainrecommendation.Cursor) []*domainrecommendation.Candidate {
	if cursor == nil {
		return candidates
	}
	filtered := make([]*domainrecommendation.Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate == nil {
			continue
		}
		if candidate.RankScore < cursor.RankScore ||
			(sameScore(candidate.RankScore, cursor.RankScore) && candidate.PublishedAt.Before(cursor.PublishedAt)) ||
			(sameScore(candidate.RankScore, cursor.RankScore) && candidate.PublishedAt.Equal(cursor.PublishedAt) && candidate.VideoID < cursor.VideoID) {
			filtered = append(filtered, candidate)
		}
	}
	return filtered
}

func sameScore(left float64, right float64) bool {
	return math.Abs(left-right) < 0.000000001
}

func parseCursorPayload(raw string) (*cursorPayload, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}

	content, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		content, err = base64.StdEncoding.DecodeString(raw)
		if err != nil {
			return nil, domainrecommendation.ErrInvalidCursor
		}
	}
	var payload cursorPayload
	if err := json.Unmarshal(content, &payload); err != nil {
		return nil, domainrecommendation.ErrInvalidCursor
	}
	if _, err := legacyCursorFromPayload(&payload); err != nil {
		return nil, domainrecommendation.ErrInvalidCursor
	}
	if len(strings.TrimSpace(payload.RequestID)) > domainrecommendation.MaxRequestIDLength {
		return nil, domainrecommendation.ErrInvalidCursor
	}
	return &payload, nil
}

func parseCursor(raw string) (*domainrecommendation.Cursor, error) {
	payload, err := parseCursorPayload(raw)
	if err != nil || payload == nil {
		return nil, err
	}
	return legacyCursorFromPayload(payload)
}

func encodeCursor(cursor *domainrecommendation.Cursor, requestID string) string {
	if cursor == nil || !cursor.Valid() {
		return ""
	}
	content, err := json.Marshal(cursorPayload{
		RankScore:   cursor.RankScore,
		PublishedAt: cursor.PublishedAt.UTC().Format(time.RFC3339Nano),
		VideoID:     cursor.VideoID,
		RequestID:   strings.TrimSpace(requestID),
	})
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(content)
}

func maxInt(left int, right int) int {
	if left > right {
		return left
	}
	return right
}
