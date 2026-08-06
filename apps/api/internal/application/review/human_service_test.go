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
		(reviewCase.Priority == cursor.Priority && reviewCase.CreatedAt.After(cursor.CreatedAt)) ||
		(reviewCase.Priority == cursor.Priority && reviewCase.CreatedAt.Equal(cursor.CreatedAt) && reviewCase.ID > cursor.CaseID)
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
	return &domainreview.HumanCaseDetail{Case: &copyCase, Subject: domainreview.ReviewSubject{VideoID: reviewCase.VideoID}}, nil
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
	service := New(
		newReviewServiceRepo(t, domainreview.OutcomeHuman),
		WithHumanRepository(repo),
		WithHumanCursorSecret("cursor-secret"),
		WithHumanTokenReader(bytes.NewReader(bytes.Repeat([]byte{7}, 96))),
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
	renewed, err := service.RenewHumanLease(context.Background(), RenewLeaseRequest{
		CaseID: 1, ReviewerID: 7, LeaseToken: claim.LeaseToken, ExpectedCaseVersion: 2,
	})
	if err != nil || renewed.Case.Version != 3 {
		t.Fatalf("renew = %#v err=%v", renewed, err)
	}
	request := DecisionRequest{
		CaseID: 1, ReviewerID: 7, LeaseToken: claim.LeaseToken, ExpectedCaseVersion: 3,
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
