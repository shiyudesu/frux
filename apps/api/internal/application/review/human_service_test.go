package applicationreview

import (
	"bytes"
	"context"
	"errors"
	"sort"
	"sync"
	"testing"
	"time"

	domainadminaudit "github.com/shiyudesu/frux/internal/domain/adminaudit"
	domainreview "github.com/shiyudesu/frux/internal/domain/review"
)

type humanServiceRepo struct {
	mu       sync.Mutex
	cases    map[int64]*domainreview.ReviewCase
	receipts map[string]*domainreview.HumanDecisionResult
	hashes   map[string]string
}

func newHumanServiceRepo(now time.Time) *humanServiceRepo {
	return &humanServiceRepo{
		cases: map[int64]*domainreview.ReviewCase{
			1: domainreview.RestoreHumanCase(1, 101, 1, domainreview.CaseStatusPendingHuman, 1, 90, 1, 0, "", nil, now.Add(-time.Hour), now, nil),
			2: domainreview.RestoreHumanCase(2, 102, 1, domainreview.CaseStatusPendingHuman, 1, 90, 1, 0, "", nil, now.Add(-30*time.Minute), now, nil),
			3: domainreview.RestoreHumanCase(3, 103, 1, domainreview.CaseStatusPendingHuman, 1, 10, 1, 0, "", nil, now.Add(-2*time.Hour), now, nil),
		},
		receipts: map[string]*domainreview.HumanDecisionResult{},
		hashes:   map[string]string{},
	}
}

func (r *humanServiceRepo) ListHumanQueue(_ context.Context, filter domainreview.HumanQueueFilter) ([]*domainreview.HumanQueueItem, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]*domainreview.HumanQueueItem, 0, len(r.cases))
	for _, reviewCase := range r.cases {
		if reviewCase.Status != domainreview.CaseStatusPendingHuman || reviewCase.AssignedReviewerID != 0 ||
			reviewCase.Priority < filter.MinPriority || reviewCase.Priority > filter.MaxPriority {
			continue
		}
		if filter.Cursor != nil && !afterHumanCursor(reviewCase, filter.Cursor) {
			continue
		}
		copyCase := *reviewCase
		items = append(items, &domainreview.HumanQueueItem{Case: &copyCase, Title: "video"})
	}
	sort.Slice(items, func(i, j int) bool {
		left, right := items[i].Case, items[j].Case
		if left.Priority != right.Priority {
			return left.Priority > right.Priority
		}
		if !left.CreatedAt.Equal(right.CreatedAt) {
			return left.CreatedAt.Before(right.CreatedAt)
		}
		return left.ID < right.ID
	})
	if len(items) > filter.Limit {
		items = items[:filter.Limit]
	}
	return items, nil
}

func afterHumanCursor(reviewCase *domainreview.ReviewCase, cursor *domainreview.QueueCursor) bool {
	return reviewCase.Priority < cursor.Priority ||
		(reviewCase.Priority == cursor.Priority && reviewCase.CreatedAt.After(cursor.SortTime)) ||
		(reviewCase.Priority == cursor.Priority && reviewCase.CreatedAt.Equal(cursor.SortTime) && reviewCase.ID > cursor.CaseID)
}

func (r *humanServiceRepo) ListHumanAssigned(_ context.Context, filter domainreview.HumanQueueFilter) ([]*domainreview.HumanQueueItem, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now().UTC()
	items := make([]*domainreview.HumanQueueItem, 0, len(r.cases))
	for _, reviewCase := range r.cases {
		if reviewCase.Status != domainreview.CaseStatusPendingHuman ||
			reviewCase.AssignedReviewerID != filter.ReviewerID ||
			reviewCase.LeaseExpiresAt == nil || !reviewCase.LeaseExpiresAt.After(now) ||
			reviewCase.Priority < filter.MinPriority || reviewCase.Priority > filter.MaxPriority {
			continue
		}
		if filter.Cursor != nil {
			leaseUntil := reviewCase.LeaseExpiresAt.UTC()
			cursorTime := filter.Cursor.SortTime.UTC()
			if leaseUntil.Before(cursorTime) ||
				(leaseUntil.Equal(cursorTime) && reviewCase.Priority > filter.Cursor.Priority) ||
				(leaseUntil.Equal(cursorTime) && reviewCase.Priority == filter.Cursor.Priority &&
					reviewCase.ID <= filter.Cursor.CaseID) {
				continue
			}
		}
		copyCase := *reviewCase
		items = append(items, &domainreview.HumanQueueItem{Case: &copyCase, Title: "video"})
	}
	sort.Slice(items, func(i, j int) bool {
		left, right := items[i].Case, items[j].Case
		if !left.LeaseExpiresAt.Equal(*right.LeaseExpiresAt) {
			return left.LeaseExpiresAt.Before(*right.LeaseExpiresAt)
		}
		if left.Priority != right.Priority {
			return left.Priority > right.Priority
		}
		return left.ID < right.ID
	})
	if len(items) > filter.Limit {
		items = items[:filter.Limit]
	}
	return items, nil
}

func (r *humanServiceRepo) ListHumanRecent(_ context.Context, filter domainreview.HumanQueueFilter) ([]*domainreview.HumanQueueItem, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]*domainreview.HumanQueueItem, 0, len(r.receipts))
	seen := map[int64]struct{}{}
	for _, receipt := range r.receipts {
		if receipt == nil || receipt.Decision == nil || receipt.Decision.ReviewerID != filter.ReviewerID ||
			receipt.Case == nil {
			continue
		}
		if _, exists := seen[receipt.Case.ID]; exists {
			continue
		}
		seen[receipt.Case.ID] = struct{}{}
		copyCase := *receipt.Case
		items = append(items, &domainreview.HumanQueueItem{Case: &copyCase, Title: "video"})
	}
	sort.Slice(items, func(i, j int) bool {
		left, right := items[i].Case, items[j].Case
		if left.ClosedAt != nil && right.ClosedAt != nil && !left.ClosedAt.Equal(*right.ClosedAt) {
			return left.ClosedAt.After(*right.ClosedAt)
		}
		return left.ID > right.ID
	})
	if filter.Cursor != nil {
		filtered := items[:0]
		for _, item := range items {
			if item.Case.ClosedAt != nil &&
				(item.Case.ClosedAt.Before(filter.Cursor.SortTime) ||
					(item.Case.ClosedAt.Equal(filter.Cursor.SortTime) && item.Case.ID < filter.Cursor.CaseID)) {
				filtered = append(filtered, item)
			}
		}
		items = filtered
	}
	if len(items) > filter.Limit {
		items = items[:filter.Limit]
	}
	return items, nil
}

func (r *humanServiceRepo) HumanQueueStats(_ context.Context, minPriority, maxPriority int) (int, time.Time, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	count := 0
	var oldest time.Time
	for _, reviewCase := range r.cases {
		if reviewCase.Status == domainreview.CaseStatusPendingHuman && reviewCase.AssignedReviewerID == 0 &&
			reviewCase.Priority >= minPriority && reviewCase.Priority <= maxPriority {
			count++
			if oldest.IsZero() || reviewCase.CreatedAt.Before(oldest) {
				oldest = reviewCase.CreatedAt
			}
		}
	}
	return count, oldest, nil
}

func (r *humanServiceRepo) RecoverExpiredHumanLeases(_ context.Context, _ int) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	recovered := 0
	for _, reviewCase := range r.cases {
		if reviewCase.Expire(time.Now().UTC()) {
			recovered++
		}
	}
	return recovered, nil
}

func (r *humanServiceRepo) GetHumanCaseDetail(_ context.Context, caseID int64) (*domainreview.HumanCaseDetail, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	reviewCase := r.cases[caseID]
	if reviewCase == nil {
		return nil, domainreview.ErrReviewCaseNotFound
	}
	copyCase := *reviewCase
	return &domainreview.HumanCaseDetail{
		Case: &copyCase,
		Subject: domainreview.ReviewSubject{
			VideoID: reviewCase.VideoID, ReviewVersion: reviewCase.ReviewVersion,
			PreviewAllowed: true, MediaURL: "https://media.example.test/video.mp4",
		},
	}, nil
}

func (r *humanServiceRepo) ClaimHumanCase(_ context.Context, caseID, reviewerID int64, tokenHash string, expectedVersion int, duration time.Duration) (*domainreview.ReviewCase, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	reviewCase := r.cases[caseID]
	if reviewCase == nil {
		return nil, domainreview.ErrReviewCaseNotFound
	}
	if err := reviewCase.Claim(reviewerID, tokenHash, expectedVersion, time.Now().UTC(), duration); err != nil {
		return nil, err
	}
	copyCase := *reviewCase
	return &copyCase, nil
}

func (r *humanServiceRepo) RenewHumanLease(_ context.Context, caseID, reviewerID int64, tokenHash string, expectedVersion int, duration time.Duration) (*domainreview.ReviewCase, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	reviewCase := r.cases[caseID]
	if err := reviewCase.Renew(reviewerID, tokenHash, expectedVersion, time.Now().UTC(), duration); err != nil {
		return nil, err
	}
	copyCase := *reviewCase
	return &copyCase, nil
}

func (r *humanServiceRepo) ResumeHumanLease(_ context.Context, caseID, reviewerID int64, tokenHash string, expectedVersion int, duration time.Duration) (*domainreview.ReviewCase, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	reviewCase := r.cases[caseID]
	if reviewCase == nil {
		return nil, domainreview.ErrReviewCaseNotFound
	}
	if err := reviewCase.Resume(reviewerID, tokenHash, expectedVersion, time.Now().UTC(), duration); err != nil {
		return nil, err
	}
	copyCase := *reviewCase
	return &copyCase, nil
}

func (r *humanServiceRepo) ReleaseHumanLease(_ context.Context, caseID, reviewerID int64, tokenHash string, expectedVersion int) (*domainreview.ReviewCase, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	reviewCase := r.cases[caseID]
	if err := reviewCase.Release(reviewerID, tokenHash, expectedVersion, time.Now().UTC()); err != nil {
		return nil, err
	}
	copyCase := *reviewCase
	return &copyCase, nil
}

func (r *humanServiceRepo) CommitHumanDecision(_ context.Context, decision *domainreview.HumanDecision, tokenHash string, auditFact *domainadminaudit.Fact) (*domainreview.HumanDecisionResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := decision.IdempotencyKeyHash
	if existing := r.receipts[key]; existing != nil {
		if r.hashes[key] != decision.PayloadHash {
			return nil, domainreview.ErrDecisionIdentityConflict
		}
		replayed := *existing
		replayed.Duplicate = true
		return &replayed, nil
	}
	if auditFact == nil {
		return nil, errors.New("missing audit fact")
	}
	reviewCase := r.cases[decision.CaseID]
	if err := reviewCase.ValidateDecision(
		decision.ReviewerID, tokenHash, decision.CaseVersion, decision.ReviewVersion, time.Now().UTC(),
	); err != nil {
		return nil, err
	}
	if decision.Outcome == domainreview.OutcomeApprove {
		reviewCase.Status = domainreview.CaseStatusApproved
	} else {
		reviewCase.Status = domainreview.CaseStatusRejected
	}
	reviewCase.Version++
	closedAt := time.Now().UTC()
	reviewCase.ClosedAt = &closedAt
	copyCase := *reviewCase
	decision.ID = int64(len(r.receipts) + 1)
	result := &domainreview.HumanDecisionResult{
		Case: &copyCase, Decision: decision, ApplySideEffects: true,
	}
	r.receipts[key] = result
	r.hashes[key] = decision.PayloadHash
	return result, nil
}

func TestHumanServiceStableCursorClaimsAndDecisionIdempotency(t *testing.T) {
	now := time.Now().UTC()
	repo := newHumanServiceRepo(now)
	applier := &countingOutcomeApplier{}
	tokenBytes := append(bytes.Repeat([]byte{7}, 32), bytes.Repeat([]byte{8}, 32)...)
	tokenBytes = append(tokenBytes, bytes.Repeat([]byte{9}, 64)...)
	service := New(
		newReviewServiceRepo(t, domainreview.OutcomeHuman),
		WithHumanRepository(repo),
		WithHumanCursorSecret("cursor-secret"),
		WithHumanTokenReader(bytes.NewReader(tokenBytes)),
		WithOutcomeApplier(applier),
	)
	first, err := service.ListHumanQueue(context.Background(), HumanQueueRequest{Limit: 2})
	if err != nil || len(first.Items) != 2 || first.Items[0].Case.ID != 1 || first.NextCursor == "" {
		t.Fatalf("first page = %#v err=%v", first, err)
	}
	second, err := service.ListHumanQueue(context.Background(), HumanQueueRequest{Limit: 2, Cursor: first.NextCursor})
	if err != nil || len(second.Items) != 1 || second.Items[0].Case.ID != 3 {
		t.Fatalf("second page = %#v err=%v", second, err)
	}
	if _, err := service.ListHumanQueue(context.Background(), HumanQueueRequest{
		Limit: 2, Cursor: first.NextCursor, MinPriority: 20, MaxPriority: 100,
	}); !errors.Is(err, domainreview.ErrInvalidQueueCursor) {
		t.Fatalf("filter-bound cursor error = %v", err)
	}
	if _, err := service.ListHumanQueue(context.Background(), HumanQueueRequest{
		Scope: domainreview.HumanQueueScopeMine, ReviewerID: 7,
		Limit: 2, Cursor: first.NextCursor,
	}); !errors.Is(err, domainreview.ErrInvalidQueueCursor) {
		t.Fatalf("scope-bound cursor error = %v", err)
	}
	claim, err := service.ClaimHumanCase(context.Background(), ClaimRequest{
		CaseID: 1, ReviewerID: 7, ExpectedCaseVersion: 1,
	})
	if err != nil || claim.LeaseToken == "" || claim.Case.Version != 2 {
		t.Fatalf("claim = %#v err=%v", claim, err)
	}
	if _, err := service.ClaimHumanCase(context.Background(), ClaimRequest{
		CaseID: 1, ReviewerID: 8, ExpectedCaseVersion: 2,
	}); !errors.Is(err, domainreview.ErrReviewCaseClaimed) {
		t.Fatalf("concurrent claim error = %v", err)
	}
	mine, err := service.ListHumanQueue(context.Background(), HumanQueueRequest{
		Scope: domainreview.HumanQueueScopeMine, ReviewerID: 7, Limit: 2,
	})
	if err != nil || len(mine.Items) != 1 || mine.Items[0].Case.ID != 1 {
		t.Fatalf("mine queue = %#v err=%v", mine, err)
	}
	resumed, err := service.ResumeHumanLease(context.Background(), ResumeLeaseRequest{
		CaseID: 1, ReviewerID: 7, ExpectedCaseVersion: 2,
	})
	if err != nil || resumed.Case.Version != 3 || resumed.LeaseToken == claim.LeaseToken {
		t.Fatalf("resume = %#v err=%v", resumed, err)
	}
	if _, err := service.RenewHumanLease(context.Background(), RenewLeaseRequest{
		CaseID: 1, ReviewerID: 7, LeaseToken: claim.LeaseToken, ExpectedCaseVersion: 3,
	}); !errors.Is(err, domainreview.ErrReviewLeaseNotOwned) {
		t.Fatalf("old token renewal error = %v", err)
	}
	renewed, err := service.RenewHumanLease(context.Background(), RenewLeaseRequest{
		CaseID: 1, ReviewerID: 7, LeaseToken: resumed.LeaseToken, ExpectedCaseVersion: 3,
	})
	if err != nil || renewed.Case.Version != 4 {
		t.Fatalf("renew = %#v err=%v", renewed, err)
	}
	request := DecisionRequest{
		CaseID: 1, ReviewerID: 7, LeaseToken: resumed.LeaseToken, ExpectedCaseVersion: 4,
		ReviewVersion: 1, Outcome: domainreview.OutcomeApprove,
		ReasonCode: domainreview.ReasonApproveCompliant, IdempotencyKey: "decision-1",
	}
	decided, err := service.DecideHumanCase(context.Background(), request)
	if err != nil || decided.Duplicate || decided.Case.Status != domainreview.CaseStatusApproved {
		t.Fatalf("decision = %#v err=%v", decided, err)
	}
	if applier.calls != 1 {
		t.Fatalf("new decision side effects = %d", applier.calls)
	}
	recent, err := service.ListHumanQueue(context.Background(), HumanQueueRequest{
		Scope: domainreview.HumanQueueScopeRecent, ReviewerID: 7, Limit: 2,
	})
	if err != nil || len(recent.Items) != 1 || recent.Items[0].Case.ID != 1 {
		t.Fatalf("recent queue = %#v err=%v", recent, err)
	}
	for _, receipt := range repo.receipts {
		receipt.ApplySideEffects = false
	}

	replayed, err := service.DecideHumanCase(context.Background(), request)
	if err != nil || !replayed.Duplicate || replayed.Decision.ID != decided.Decision.ID {
		t.Fatalf("replay = %#v err=%v", replayed, err)
	}
	if applier.calls != 1 {
		t.Fatalf("stale replay side effects = %d", applier.calls)
	}
	request.ReasonCode = domainreview.ReasonApproveFalsePositive
	if _, err := service.DecideHumanCase(context.Background(), request); !errors.Is(err, domainreview.ErrDecisionIdentityConflict) {
		t.Fatalf("payload conflict error = %v", err)
	}
}

type humanPreviewProviderStub struct {
	access *domainreview.HumanPreviewAccess
	err    error
}

func (p humanPreviewProviderStub) ResolveHumanPreview(
	context.Context,
	domainreview.ReviewSubject,
	time.Duration,
) (*domainreview.HumanPreviewAccess, error) {
	return p.access, p.err
}

func TestHumanServiceProtectedPreview(t *testing.T) {
	now := time.Now().UTC()
	repo := newHumanServiceRepo(now)
	expected := &domainreview.HumanPreviewAccess{
		MediaURL:  "https://signed.example.test/video.mp4",
		CoverURL:  "https://signed.example.test/cover.jpg",
		ExpiresAt: now.Add(DefaultHumanPreviewTTL),
	}
	service := New(
		newReviewServiceRepo(t, domainreview.OutcomeHuman),
		WithHumanRepository(repo),
		WithHumanCursorSecret("cursor-secret"),
		WithHumanPreviewProvider(humanPreviewProviderStub{access: expected}),
	)
	access, err := service.GetHumanPreview(context.Background(), 1)
	if err != nil || access != expected {
		t.Fatalf("preview = %#v err=%v", access, err)
	}
	repo.cases[1].Status = domainreview.CaseStatusSuperseded
	if _, err := service.GetHumanPreview(context.Background(), 1); !errors.Is(err, domainreview.ErrReviewPreviewUnavailable) {
		t.Fatalf("stale preview error = %v", err)
	}
}

func TestHumanSubjectStateErrorsAreConflicts(t *testing.T) {
	for _, err := range []error{
		domainreview.ErrReviewSubjectState,
		domainreview.ErrReviewSubjectStale,
		domainreview.ErrReviewCaseNotHuman,
	} {
		if result := humanErrorResult(err); result != "conflict" {
			t.Fatalf("humanErrorResult(%v) = %q", err, result)
		}
	}
}
