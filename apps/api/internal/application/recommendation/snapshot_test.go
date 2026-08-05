package applicationrecommendation

import (
	applicationexposure "github.com/shiyudesu/frux/internal/application/exposure"
	domainrecommendation "github.com/shiyudesu/frux/internal/domain/recommendation"
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type memorySnapshotStore struct {
	snapshots       map[string]*Snapshot
	err             error
	committedErr    error
	missRequestLoad bool
}

func (s *memorySnapshotStore) CreateSnapshot(_ context.Context, snapshot *Snapshot, _ time.Duration) (*Snapshot, bool, error) {
	if s.err != nil {
		return nil, false, s.err
	}
	if s.snapshots == nil {
		s.snapshots = map[string]*Snapshot{}
	}
	for _, existing := range s.snapshots {
		if existing.UserID == snapshot.UserID && existing.Scene == snapshot.Scene && existing.RequestID == snapshot.RequestID {
			return existing.Clone(), false, s.committedErr
		}
	}
	s.snapshots[snapshot.ID] = snapshot.Clone()
	return snapshot.Clone(), true, s.committedErr
}

func (s *memorySnapshotStore) LoadSnapshot(_ context.Context, id string) (*Snapshot, bool, error) {
	if s.err != nil {
		return nil, false, s.err
	}
	snapshot := s.snapshots[id]
	return snapshot.Clone(), snapshot != nil, nil
}

func (s *memorySnapshotStore) LoadSnapshotForRequest(_ context.Context, userID int64, scene string, requestID string) (*Snapshot, bool, error) {
	if s.err != nil {
		return nil, false, s.err
	}
	if s.missRequestLoad {
		return nil, false, nil
	}
	for _, snapshot := range s.snapshots {
		if snapshot.UserID == userID && snapshot.Scene == scene && snapshot.RequestID == requestID {
			return snapshot.Clone(), true, nil
		}
	}
	return nil, false, nil
}

type mutablePolicySelector struct{ policy *domainrecommendation.Policy }

func (s *mutablePolicySelector) Select(context.Context, string, int64, string) (*domainrecommendation.Policy, error) {
	return s.policy.Clone(), nil
}

func snapshotService(t *testing.T, now *time.Time, store SnapshotStore, visible *visibilityCatalog, selector PolicySelector) (*Service, *rankerTestRepo) {
	t.Helper()
	repo := &rankerTestRepo{
		vectors:         map[int64][]float64{1: {1}, 2: {1}, 3: {1}, 4: {1}},
		features:        emptyRankingFeatures(),
		captureEvidence: true,
		pool: []*domainrecommendation.Candidate{
			rankerCandidate(1, 1, 1, now.Add(-4*time.Minute), domainrecommendation.RecallProviderHot),
			rankerCandidate(2, 2, 2, now.Add(-3*time.Minute), domainrecommendation.RecallProviderHot),
			rankerCandidate(3, 3, 3, now.Add(-2*time.Minute), domainrecommendation.RecallProviderHot),
			rankerCandidate(4, 4, 4, now.Add(-time.Minute), domainrecommendation.RecallProviderHot),
		},
	}
	signer, err := NewHMACSnapshotCursorSigner("snapshot-test-secret")
	if err != nil {
		t.Fatal(err)
	}
	return New(repo,
		WithNow(func() time.Time { return now.UTC() }),
		WithPolicySelector(selector),
		WithCandidateVisibilityFilter(*visible),
		WithSnapshotPagination(store, signer),
	), repo
}

func snapshotPolicy(t *testing.T, version int) *domainrecommendation.Policy {
	t.Helper()
	return rankerPolicy(t, version, map[string]float64{domainrecommendation.FeatureHotness: 1})
}

func TestGeneratedRequestIDIsBoundedAndSessionUnique(t *testing.T) {
	firstBytes := bytes.Repeat([]byte{1}, 24)
	secondBytes := bytes.Repeat([]byte{2}, 24)
	first, err := generatedRequestIDFromReader(bytes.NewReader(firstBytes))
	if err != nil {
		t.Fatal(err)
	}
	second, err := generatedRequestIDFromReader(bytes.NewReader(secondBytes))
	if err != nil {
		t.Fatal(err)
	}
	if first == second || !strings.HasPrefix(first, "srv-") || len(first) > domainrecommendation.MaxRequestIDLength {
		t.Fatalf("generated request IDs must be unique and bounded: %q %q", first, second)
	}
}

func TestSnapshotRestoresOriginalDegradationOnRetriesAndLaterPages(t *testing.T) {
	now := time.Date(2026, 7, 27, 1, 30, 0, 0, time.UTC)
	candidates := []*domainrecommendation.Candidate{
		rankerCandidate(2, 2, 2, now, domainrecommendation.RecallProviderHot),
		rankerCandidate(1, 1, 1, now.Add(-time.Minute), domainrecommendation.RecallProviderFresh),
	}
	snapshot := &Snapshot{
		ID: "degraded-snapshot", UserID: 7, Scene: "recommend", RequestID: "retry", PolicyVersion: 1,
		ExpiresAt: now.Add(5 * time.Minute), Candidates: candidates, Degraded: true,
		DegradedProviders: []ProviderDegradation{{Provider: domainrecommendation.RecallProviderContentSimilarity, Reason: "timeout"}},
	}
	store := &memorySnapshotStore{snapshots: map[string]*Snapshot{snapshot.ID: snapshot}}
	visible := &visibilityCatalog{visible: map[int64]*domainrecommendation.Candidate{
		1: candidates[1], 2: candidates[0],
	}}
	service, _ := snapshotService(t, &now, store, visible, &mutablePolicySelector{policy: snapshotPolicy(t, 1)})

	first, err := service.Recommend(context.Background(), CandidateRequest{
		UserID: 7, Scene: "recommend", RequestID: "retry", Limit: 1,
	})
	if err != nil || !first.Degraded || len(first.DegradedProviders) != 1 ||
		first.DegradedProviders[0].Provider != domainrecommendation.RecallProviderContentSimilarity ||
		first.DegradedProviders[0].Reason != "timeout" {
		t.Fatalf("first snapshot retry lost original degradation: %#v, %v", first, err)
	}

	next, err := service.Recommend(context.Background(), CandidateRequest{
		UserID: 7, Scene: "recommend", RequestID: "retry", Cursor: first.NextCursor, Limit: 1,
	})
	if err != nil || !next.Degraded || len(next.DegradedProviders) != 1 ||
		next.DegradedProviders[0] != first.DegradedProviders[0] {
		t.Fatalf("snapshot page lost original degradation: %#v, %v", next, err)
	}
}

func TestLegacyCursorRetainsGeneratedRequestID(t *testing.T) {
	now := time.Date(2026, 7, 27, 1, 0, 0, 0, time.UTC)
	repo := &rankerTestRepo{
		vectors:  map[int64][]float64{1: {1}, 2: {1}, 3: {1}},
		features: emptyRankingFeatures(),
		pool: []*domainrecommendation.Candidate{
			rankerCandidate(3, 3, 3, now, domainrecommendation.RecallProviderHot),
			rankerCandidate(2, 2, 2, now.Add(-time.Minute), domainrecommendation.RecallProviderHot),
			rankerCandidate(1, 1, 1, now.Add(-2*time.Minute), domainrecommendation.RecallProviderHot),
		},
	}
	service := New(repo, WithNow(func() time.Time { return now }))
	first, err := service.Recommend(context.Background(), CandidateRequest{UserID: 7, Scene: "recommend", Limit: 1})
	if err != nil || first.RequestID == "" || first.NextCursor == "" {
		t.Fatalf("first generated session = %#v, %v", first, err)
	}
	next, err := service.Recommend(context.Background(), CandidateRequest{
		UserID: 7, Scene: "recommend", Cursor: first.NextCursor, Limit: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if next.RequestID != first.RequestID {
		t.Fatalf("legacy cursor lost generated request ID: got %q want %q", next.RequestID, first.RequestID)
	}
	if _, err := service.Recommend(context.Background(), CandidateRequest{
		UserID: 7, Scene: "recommend", RequestID: "different-session", Cursor: first.NextCursor, Limit: 1,
	}); !errors.Is(err, domainrecommendation.ErrInvalidCursor) {
		t.Fatalf("legacy cursor accepted a mismatched request ID: %v", err)
	}
}

func TestFirstPageRequestLogsKeepFullRankedPoolForLegacyAndSnapshotSessions(t *testing.T) {
	now := time.Date(2026, 7, 27, 1, 0, 0, 0, time.UTC)
	newRepo := func() *rankerTestRepo {
		return &rankerTestRepo{
			vectors:  map[int64][]float64{1: {1}, 2: {1}, 3: {1}, 4: {1}},
			features: emptyRankingFeatures(),
			pool: []*domainrecommendation.Candidate{
				rankerCandidate(4, 4, 4, now, domainrecommendation.RecallProviderHot),
				rankerCandidate(3, 3, 3, now.Add(-time.Minute), domainrecommendation.RecallProviderFresh),
				rankerCandidate(2, 2, 2, now.Add(-2*time.Minute), domainrecommendation.RecallProviderHot),
				rankerCandidate(1, 1, 1, now.Add(-3*time.Minute), domainrecommendation.RecallProviderFresh),
			},
		}
	}
	policy := snapshotPolicy(t, 1)
	policy.Config.SamplingRatePPM = domainrecommendation.MaxSamplingRatePPM

	t.Run("legacy degraded cursor", func(t *testing.T) {
		repo := newRepo()
		logs := &memoryRequestLogRepository{}
		service := New(repo,
			WithNow(func() time.Time { return now }),
			WithPolicySelector(&mutablePolicySelector{policy: policy}),
			WithRequestLogRepository(logs),
		)
		first, err := service.Recommend(context.Background(), CandidateRequest{
			UserID: 7, Scene: "recommend", RequestID: "legacy-full-pool", Limit: 2,
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(first.Candidates) != 2 || len(logs.logs) != 1 || len(logs.logs[0].Candidates) != 4 {
			t.Fatalf("legacy log should contain full pool before page slicing: page=%#v logs=%#v", first, logs.logs)
		}
		for _, candidate := range logs.logs[0].Candidates {
			if len(candidate.Reasons) == 0 || len(candidate.ScoreComponents) == 0 {
				t.Fatalf("legacy request log omitted rank explanation: %#v", candidate)
			}
		}
		if _, err := service.Recommend(context.Background(), CandidateRequest{
			UserID: 7, Scene: "recommend", RequestID: "legacy-full-pool", Cursor: first.NextCursor, Limit: 2,
		}); err != nil {
			t.Fatal(err)
		}
		if len(logs.logs) != 1 {
			t.Fatalf("cursor page duplicated request log: %#v", logs.logs)
		}
	})

	t.Run("snapshot session", func(t *testing.T) {
		repo := newRepo()
		logs := &memoryRequestLogRepository{}
		signer, err := NewHMACSnapshotCursorSigner("full-pool-log-secret")
		if err != nil {
			t.Fatal(err)
		}
		service := New(repo,
			WithNow(func() time.Time { return now }),
			WithPolicySelector(&mutablePolicySelector{policy: policy}),
			WithSnapshotPagination(&memorySnapshotStore{}, signer),
			WithRequestLogRepository(logs),
		)
		if _, err := service.Recommend(context.Background(), CandidateRequest{
			UserID: 7, Scene: "recommend", RequestID: "snapshot-full-pool", Limit: 2,
		}); err != nil {
			t.Fatal(err)
		}
		if len(logs.logs) != 1 || !logs.logs[0].Snapshot || len(logs.logs[0].Candidates) != 4 {
			t.Fatalf("snapshot log should retain full candidate pool: %#v", logs.logs)
		}
	})
}

func TestRecommendationSnapshotRetainsOrderAcrossPolicyChangesAndSkipsVisibilityGaps(t *testing.T) {
	now := time.Date(2026, 7, 27, 1, 0, 0, 0, time.UTC)
	visible := &visibilityCatalog{visible: map[int64]*domainrecommendation.Candidate{}}
	for id := int64(1); id <= 4; id++ {
		visible.visible[id] = rankerCandidate(id, id, int(id), now, domainrecommendation.RecallProviderHot)
	}

	selector := &mutablePolicySelector{policy: snapshotPolicy(t, 1)}
	store := &memorySnapshotStore{}
	service, repo := snapshotService(t, &now, store, visible, selector)

	first, err := service.Recommend(context.Background(), CandidateRequest{UserID: 7, Scene: "recommend", RequestID: "stable", Limit: 2})
	if err != nil || first.NextCursor == "" || first.Degraded {
		t.Fatalf("snapshot first page failed: %#v, %v", first, err)
	}
	recordDeliveredCandidates(t, service, first)
	if len(repo.servedEvidence) != 1 {
		t.Fatalf("first snapshot page did not durably save served candidates: %#v", repo.servedEvidence)
	}
	evidence := repo.servedEvidence[0]
	if evidence.UserID != 7 || evidence.RequestID != "stable" || evidence.PolicyVersion != 1 ||
		!evidence.ExpiresAt.After(now.Add(domainrecommendation.ServedCandidateEvidenceMinimumTTL-time.Nanosecond)) ||
		!equalServedCandidateIDs(evidence.Candidates, []int64{4, 3}) {
		t.Fatalf("unexpected snapshot served-candidate evidence: %#v", evidence)
	}
	if got := candidateIDs(first.Candidates); len(got) != 2 || got[0] != 4 || got[1] != 3 {
		t.Fatalf("unexpected first snapshot page: %v", got)
	}

	selector.policy = snapshotPolicy(t, 2)
	repo.pool[0].HotScore = 1000
	delete(visible.visible, 2)
	second, err := service.Recommend(context.Background(), CandidateRequest{UserID: 7, Scene: "recommend", RequestID: "stable", Cursor: first.NextCursor, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if got := candidateIDs(second.Candidates); len(got) != 1 || got[0] != 1 || second.HasMore {
		t.Fatalf("snapshot did not retain order or skip visibility gap: %v, hasMore=%v", got, second.HasMore)
	}
	if second.PolicyVersion != 1 {
		t.Fatalf("snapshot must retain initial policy version, got %d", second.PolicyVersion)
	}
	recordDeliveredCandidates(t, service, second)
	if len(repo.servedEvidence) != 2 || !equalServedCandidateIDs(repo.servedEvidence[1].Candidates, []int64{1}) {
		t.Fatalf("later snapshot page did not append only delivered evidence: %#v", repo.servedEvidence)
	}
}

func TestSnapshotEvidenceAuthorizesOnlyDeliveredPages(t *testing.T) {
	now := time.Date(2026, 7, 27, 1, 0, 0, 0, time.UTC)
	visible := &visibilityCatalog{visible: map[int64]*domainrecommendation.Candidate{}}
	for id := int64(1); id <= 4; id++ {
		visible.visible[id] = rankerCandidate(id, id, int(id), now, domainrecommendation.RecallProviderHot)
	}
	service, repo := snapshotService(
		t,
		&now,
		&memorySnapshotStore{},
		visible,
		&mutablePolicySelector{policy: snapshotPolicy(t, 1)},
	)

	first, err := service.Recommend(context.Background(), CandidateRequest{
		UserID: 7, Scene: "recommend", RequestID: "delivered-pages", Limit: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	recordDeliveredCandidates(t, service, first)
	if err := func() error {
		_, err := service.SubmitFeedback(context.Background(), FeedbackInput{
			UserID: 7, VideoID: 2, RequestID: "delivered-pages",
			FeedbackType: domainrecommendation.FeedbackTypeNotInterested, IdempotencyKey: "future-page",
		})
		return err
	}(); !errors.Is(err, domainrecommendation.ErrFeedbackRequestMismatch) {
		t.Fatalf("unseen future-page candidate was authorized: %v", err)
	}
	worker := NewBehaviorEventWorker(repo, nil)
	if err := worker.Handle(context.Background(), &applicationexposure.ViewEventRecordedEvent{
		EventID: "unseen-page-outcome", UserID: 7, VideoID: 2, Scene: "recommend", RequestID: "delivered-pages",
		EventType: "complete", Completed: true, OccurredAt: now, RecordedAt: now,
	}); err != nil {
		t.Fatalf("unseen outcome should remain an accepted behavior fact: %v", err)
	}
	if len(repo.outcomes) != 0 {
		t.Fatalf("unseen future-page outcome was attributed: %#v", repo.outcomes)
	}

	second, err := service.Recommend(context.Background(), CandidateRequest{
		UserID: 7, Scene: "recommend", RequestID: "delivered-pages", Cursor: first.NextCursor, Limit: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !equalCandidateIDs(candidateIDs(second.Candidates), []int64{2, 1}) {
		t.Fatalf("unexpected second page: %#v", candidateIDs(second.Candidates))
	}
	recordDeliveredCandidates(t, service, second)
	if _, err := service.SubmitFeedback(context.Background(), FeedbackInput{
		UserID: 7, VideoID: 2, RequestID: "delivered-pages",
		FeedbackType: domainrecommendation.FeedbackTypeNotInterested, IdempotencyKey: "delivered-page",
	}); err != nil {
		t.Fatalf("delivered future-page candidate was rejected: %v", err)
	}
	if err := worker.Handle(context.Background(), &applicationexposure.ViewEventRecordedEvent{
		EventID: "delivered-page-outcome", UserID: 7, VideoID: 2, Scene: "recommend", RequestID: "delivered-pages",
		EventType: "complete", Completed: true, OccurredAt: now, RecordedAt: now,
	}); err != nil {
		t.Fatalf("delivered future-page outcome was rejected: %v", err)
	}
	if repo.outcomes[domainrecommendation.ViewOutcomeID(7, "delivered-page-outcome")] == nil {
		t.Fatalf("delivered future-page outcome was not attributed: %#v", repo.outcomes)
	}
}

func TestRecommendationSnapshotFirstPageRetryUsesExistingRequestSnapshot(t *testing.T) {
	now := time.Date(2026, 7, 27, 1, 0, 0, 0, time.UTC)
	visible := &visibilityCatalog{visible: map[int64]*domainrecommendation.Candidate{}}
	for id := int64(1); id <= 4; id++ {
		visible.visible[id] = rankerCandidate(id, id, int(id), now, domainrecommendation.RecallProviderHot)
	}
	selector := &mutablePolicySelector{policy: snapshotPolicy(t, 1)}
	store := &memorySnapshotStore{}
	service, repo := snapshotService(t, &now, store, visible, selector)

	first, err := service.Recommend(context.Background(), CandidateRequest{UserID: 7, Scene: "recommend", RequestID: "first-page-retry", Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	firstIDs := candidateIDs(first.Candidates)
	selector.policy = snapshotPolicy(t, 2)
	repo.pool[0].HotScore = 10_000
	store.missRequestLoad = true // Simulate expiration between lookup and atomic create.
	retry, err := service.Recommend(context.Background(), CandidateRequest{UserID: 7, Scene: "recommend", RequestID: "first-page-retry", Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if got := candidateIDs(retry.Candidates); !equalCandidateIDs(got, firstIDs) || retry.PolicyVersion != first.PolicyVersion {
		t.Fatalf("first-page retry diverged: first=%v retry=%v policies=%d/%d", firstIDs, got, first.PolicyVersion, retry.PolicyVersion)
	}
	if len(store.snapshots) != 1 {
		t.Fatalf("first-page retry replaced the snapshot: %d snapshots", len(store.snapshots))
	}
}

func TestCommittedSnapshotMaintenanceFailureUsesAuthoritativeStoredRanking(t *testing.T) {
	now := time.Date(2026, 7, 27, 1, 0, 0, 0, time.UTC)
	visible := &visibilityCatalog{visible: map[int64]*domainrecommendation.Candidate{}}
	for id := int64(1); id <= 4; id++ {
		visible.visible[id] = rankerCandidate(id, id, int(id), now, domainrecommendation.RecallProviderHot)
	}
	authoritative := &Snapshot{
		ID: "authoritative", UserID: 7, Scene: "recommend", RequestID: "maintenance-failure", PolicyVersion: 1,
		ExpiresAt: now.Add(5 * time.Minute),
		Candidates: []*domainrecommendation.Candidate{
			visible.visible[1], visible.visible[2],
		},
	}
	store := &memorySnapshotStore{
		snapshots:       map[string]*Snapshot{authoritative.ID: authoritative},
		missRequestLoad: true,
		committedErr:    errors.New("snapshot user-index maintenance failed"),
	}
	service, _ := snapshotService(t, &now, store, visible, &mutablePolicySelector{policy: snapshotPolicy(t, 1)})

	result, err := service.Recommend(context.Background(), CandidateRequest{
		UserID: 7, Scene: "recommend", RequestID: "maintenance-failure", Limit: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Degraded || !strings.HasPrefix(result.NextCursor, snapshotCursorVersion+".") ||
		!equalCandidateIDs(candidateIDs(result.Candidates), []int64{1}) {
		t.Fatalf("committed snapshot fell back to divergent degraded ranking: %#v", result)
	}
}

func equalCandidateIDs(left, right []int64) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func TestSnapshotCursorRejectsTamperingBindingsExpiryAndOffsets(t *testing.T) {
	now := time.Date(2026, 7, 27, 1, 0, 0, 0, time.UTC)
	signer, err := NewHMACSnapshotCursorSigner("snapshot-test-secret")
	if err != nil {
		t.Fatal(err)
	}

	payload := snapshotCursorPayload{
		Version: snapshotCursorVersion, SnapshotID: "snapshot", UserID: 7, Scene: "recommend",
		RequestID: "request", PolicyVersion: 1, Offset: 2, ExpiresAt: now.Add(time.Minute).UnixNano(),
	}
	cursor, err := signer.SignSnapshotCursor(payload)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := signer.VerifySnapshotCursor(cursor, 8, "recommend", "request", now); !errors.Is(err, domainrecommendation.ErrInvalidCursor) {
		t.Fatalf("wrong user accepted: %v", err)
	}
	if _, err := signer.VerifySnapshotCursor(cursor, 7, "recommend", "other", now); !errors.Is(err, domainrecommendation.ErrInvalidCursor) {
		t.Fatalf("wrong request accepted: %v", err)
	}
	if _, err := signer.VerifySnapshotCursor(cursor+"x", 7, "recommend", "request", now); !errors.Is(err, domainrecommendation.ErrInvalidCursor) {
		t.Fatalf("tampered signature accepted: %v", err)
	}
	if _, err := signer.VerifySnapshotCursor(cursor, 7, "recommend", "request", now.Add(2*time.Minute)); !errors.Is(err, domainrecommendation.ErrInvalidCursor) {
		t.Fatalf("expired cursor accepted: %v", err)
	}
	payload.Offset = maxSnapshotCandidates + 1
	if _, err := signer.SignSnapshotCursor(payload); !errors.Is(err, domainrecommendation.ErrInvalidCursor) {
		t.Fatalf("overflow offset signed: %v", err)
	}
	payload.Offset = -1
	if _, err := signer.SignSnapshotCursor(payload); !errors.Is(err, domainrecommendation.ErrInvalidCursor) {
		t.Fatalf("negative offset signed: %v", err)
	}
	if _, err := signer.VerifySnapshotCursor(strings.Repeat("x", maxSnapshotCursorLength+1), 7, "recommend", "request", now); !errors.Is(err, domainrecommendation.ErrInvalidCursor) {
		t.Fatalf("oversized cursor accepted: %v", err)
	}
}

func TestSnapshotCursorPreservesSubsecondExpiryBoundary(t *testing.T) {
	now := time.Date(2026, 7, 27, 1, 0, 0, 123_456_789, time.UTC)
	signer, err := NewHMACSnapshotCursorSigner("snapshot-test-secret")
	if err != nil {
		t.Fatal(err)
	}
	payload := snapshotCursorPayload{
		Version: snapshotCursorVersion, SnapshotID: "snapshot", UserID: 7, Scene: "recommend",
		RequestID: "request", PolicyVersion: 1, ExpiresAt: now.Add(500 * time.Millisecond).UnixNano(),
	}
	cursor, err := signer.SignSnapshotCursor(payload)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := signer.VerifySnapshotCursor(cursor, 7, "recommend", "request", now.Add(499*time.Millisecond)); err != nil {
		t.Fatalf("cursor expired before exact UTC deadline: %v", err)
	}
	if _, err := signer.VerifySnapshotCursor(cursor, 7, "recommend", "request", now.Add(500*time.Millisecond)); !errors.Is(err, domainrecommendation.ErrInvalidCursor) {
		t.Fatalf("cursor remained valid at exact expiry: %v", err)
	}
}

func TestSnapshotPagesReapplyActiveFeedbackWithoutFallback(t *testing.T) {
	now := time.Date(2026, 7, 27, 1, 0, 0, 0, time.UTC)
	visible := &visibilityCatalog{visible: map[int64]*domainrecommendation.Candidate{}}
	for id := int64(1); id <= 4; id++ {
		visible.visible[id] = rankerCandidate(id, id, int(id), now, domainrecommendation.RecallProviderHot)
	}
	store := &memorySnapshotStore{}
	service, repo := snapshotService(t, &now, store, visible, &mutablePolicySelector{policy: snapshotPolicy(t, 1)})

	first, err := service.Recommend(context.Background(), CandidateRequest{UserID: 7, Scene: "recommend", RequestID: "feedback-gap", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	repo.features.SuppressedVideos = map[int64]bool{3: true}
	repo.features.SuppressedAuthors = map[int64]bool{}
	next, err := service.Recommend(context.Background(), CandidateRequest{
		UserID: 7, Scene: "recommend", RequestID: "feedback-gap", Cursor: first.NextCursor, Limit: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if ids := candidateIDs(next.Candidates); len(ids) != 1 || ids[0] != 2 {
		t.Fatalf("active feedback was not applied to later snapshot page: %v", ids)
	}
}

func TestRecommendationSnapshotExpiryAndRedisFailureUseExpectedPaths(t *testing.T) {
	now := time.Date(2026, 7, 27, 1, 0, 0, 0, time.UTC)
	visible := &visibilityCatalog{visible: map[int64]*domainrecommendation.Candidate{}}
	for id := int64(1); id <= 4; id++ {
		visible.visible[id] = rankerCandidate(id, id, int(id), now, domainrecommendation.RecallProviderHot)
	}
	store := &memorySnapshotStore{}
	service, _ := snapshotService(t, &now, store, visible, &mutablePolicySelector{policy: snapshotPolicy(t, 1)})
	first, err := service.Recommend(context.Background(), CandidateRequest{UserID: 7, Scene: "recommend", RequestID: "expiry", Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(6 * time.Minute)
	if _, err := service.Recommend(context.Background(), CandidateRequest{UserID: 7, Scene: "recommend", RequestID: "expiry", Cursor: first.NextCursor, Limit: 2}); !errors.Is(err, domainrecommendation.ErrInvalidCursor) {
		t.Fatalf("expired snapshot did not fail cursor validation: %v", err)
	}

	now = time.Date(2026, 7, 27, 1, 0, 0, 0, time.UTC)
	store = &memorySnapshotStore{}
	service, repo := snapshotService(t, &now, store, visible, &mutablePolicySelector{policy: snapshotPolicy(t, 1)})
	first, err = service.Recommend(context.Background(), CandidateRequest{UserID: 7, Scene: "recommend", RequestID: "degraded", Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	recordDeliveredCandidates(t, service, first)
	repo.vectors[5] = []float64{1}
	repo.pool = append(repo.pool, rankerCandidate(5, 5, 3, now.Add(-10*time.Minute), domainrecommendation.RecallProviderHot))
	visible.visible[5] = rankerCandidate(5, 5, 3, now.Add(-10*time.Minute), domainrecommendation.RecallProviderHot)
	store.err = errors.New("redis unavailable")
	second, err := service.Recommend(context.Background(), CandidateRequest{UserID: 7, Scene: "recommend", RequestID: "degraded", Cursor: first.NextCursor, Limit: 2})
	if err != nil || !second.Degraded || strings.HasPrefix(second.NextCursor, snapshotCursorVersion+".") {
		t.Fatalf("snapshot read failure did not use degraded legacy path: %#v, %v", second, err)
	}
	if ids := candidateIDs(second.Candidates); len(ids) != 2 || ids[1] != 5 {
		t.Fatalf("degraded cursor did not serve the newly recalled candidate: %v", ids)
	}
	recordDeliveredCandidates(t, service, second)
	if len(repo.servedEvidence) != 2 || !equalServedCandidateIDs(repo.servedEvidence[1].Candidates, []int64{3, 5}) {
		t.Fatalf("degraded cursor did not append durable membership before return: %#v", repo.servedEvidence)
	}
	if _, err := service.SubmitFeedback(context.Background(), FeedbackInput{
		UserID: 7, VideoID: 5, RequestID: "degraded", FeedbackType: domainrecommendation.FeedbackTypeNotInterested, IdempotencyKey: "later-page-feedback",
	}); err != nil {
		t.Fatalf("feedback for an appended degraded-page candidate was rejected: %v", err)
	}
	if err := NewBehaviorEventWorker(repo, nil).Handle(context.Background(), &applicationexposure.ViewEventRecordedEvent{
		EventID: "later-page-complete", UserID: 7, VideoID: 5, Scene: "recommend", RequestID: "degraded",
		EventType: "complete", Completed: true, OccurredAt: now, RecordedAt: now,
	}); err != nil {
		t.Fatalf("outcome for an appended degraded-page candidate was rejected: %v", err)
	}
	if repo.outcomes[domainrecommendation.ViewOutcomeID(7, "later-page-complete")] == nil {
		t.Fatalf("outcome was not attributed to the appended degraded-page evidence: %#v", repo.outcomes)
	}

	store = &memorySnapshotStore{err: errors.New("redis unavailable")}
	service, repo = snapshotService(t, &now, store, visible, &mutablePolicySelector{policy: snapshotPolicy(t, 1)})
	first, err = service.Recommend(context.Background(), CandidateRequest{UserID: 7, Scene: "recommend", RequestID: "write-failure", Limit: 2})
	if err != nil || !first.Degraded || strings.HasPrefix(first.NextCursor, snapshotCursorVersion+".") {
		t.Fatalf("snapshot write failure did not use degraded legacy path: %#v, %v", first, err)
	}
	recordDeliveredCandidates(t, service, first)
	if len(repo.servedEvidence) != 1 || !equalServedCandidateIDs(repo.servedEvidence[0].Candidates, []int64{4, 3}) {
		t.Fatalf("degraded first page did not durably save only returned candidates: %#v", repo.servedEvidence)
	}
}

func recordDeliveredCandidates(t testing.TB, service *Service, result *CandidateResult) {
	t.Helper()
	videoIDs := make([]int64, 0, len(result.Candidates))
	for _, candidate := range result.Candidates {
		if candidate != nil {
			videoIDs = append(videoIDs, candidate.VideoID)
		}
	}
	if err := service.RecordDeliveredCandidates(context.Background(), DeliveredCandidatesInput{
		UserID: result.UserID, RequestID: result.RequestID, PolicyVersion: result.PolicyVersion,
		VideoIDs: videoIDs, ExpiresAt: result.DeliveryExpiresAt,
	}); err != nil {
		t.Fatalf("record delivered candidates: %v", err)
	}
}

func equalServedCandidateIDs(items []domainrecommendation.ServedCandidateEvidenceItem, want []int64) bool {
	if len(items) != len(want) {
		return false
	}
	for index, item := range items {
		if item.VideoID != want[index] || item.Position != index {
			return false
		}
	}
	return true
}
