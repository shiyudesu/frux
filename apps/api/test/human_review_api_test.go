package test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	applicationreview "github.com/shiyudesu/frux/internal/application/review"
	domainaccount "github.com/shiyudesu/frux/internal/domain/account"
	domainadminaudit "github.com/shiyudesu/frux/internal/domain/adminaudit"
	domainreview "github.com/shiyudesu/frux/internal/domain/review"
	interfaceshttpmiddleware "github.com/shiyudesu/frux/internal/interfaces/http/middleware"
	interfaceshttpreview "github.com/shiyudesu/frux/internal/interfaces/http/review"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
)

type humanReviewAPIMemoryRepo struct {
	mu       sync.Mutex
	cases    map[int64]*domainreview.ReviewCase
	receipts map[string]*domainreview.HumanDecisionResult
	hashes   map[string]string
}

func newHumanReviewAPIMemoryRepo() *humanReviewAPIMemoryRepo {
	now := time.Now().UTC()
	return &humanReviewAPIMemoryRepo{
		cases: map[int64]*domainreview.ReviewCase{
			1: domainreview.RestoreHumanCase(1, 101, 1, domainreview.CaseStatusPendingHuman, 1, 80, 1, 0, "", nil, now.Add(-time.Hour), now, nil),
			2: domainreview.RestoreHumanCase(2, 102, 1, domainreview.CaseStatusPendingHuman, 1, 70, 1, 0, "", nil, now.Add(-30*time.Minute), now, nil),
			3: domainreview.RestoreHumanCase(3, 103, 1, domainreview.CaseStatusPendingHuman, 1, 60, 1, 0, "", nil, now.Add(-time.Minute), now, nil),
		},
		receipts: map[string]*domainreview.HumanDecisionResult{},
		hashes:   map[string]string{},
	}
}

func (r *humanReviewAPIMemoryRepo) CreateOrGetCase(context.Context, int64) (*domainreview.ReviewCase, bool, error) {
	return nil, false, errors.New("not used")
}
func (r *humanReviewAPIMemoryRepo) ProcessMachineResult(context.Context, *domainreview.MachineResult) (*domainreview.ProcessingResult, error) {
	return nil, errors.New("not used")
}
func (r *humanReviewAPIMemoryRepo) ListReviewableVideoIDsWithoutCase(context.Context, int) ([]int64, error) {
	return nil, nil
}

func (r *humanReviewAPIMemoryRepo) ListHumanQueue(_ context.Context, filter domainreview.HumanQueueFilter) ([]*domainreview.HumanQueueItem, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]*domainreview.HumanQueueItem, 0, len(r.cases))
	for _, reviewCase := range r.cases {
		if reviewCase.Status != domainreview.CaseStatusPendingHuman || reviewCase.AssignedReviewerID != 0 {
			continue
		}
		if filter.Cursor != nil && !(reviewCase.Priority < filter.Cursor.Priority ||
			(reviewCase.Priority == filter.Cursor.Priority && reviewCase.CreatedAt.After(filter.Cursor.SortTime)) ||
			(reviewCase.Priority == filter.Cursor.Priority && reviewCase.CreatedAt.Equal(filter.Cursor.SortTime) && reviewCase.ID > filter.Cursor.CaseID)) {
			continue
		}
		copyCase := *reviewCase
		items = append(items, &domainreview.HumanQueueItem{
			Case: &copyCase, AuthorID: 9, Title: fmt.Sprintf("video-%d", reviewCase.VideoID),
		})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Case.Priority != items[j].Case.Priority {
			return items[i].Case.Priority > items[j].Case.Priority
		}
		if !items[i].Case.CreatedAt.Equal(items[j].Case.CreatedAt) {
			return items[i].Case.CreatedAt.Before(items[j].Case.CreatedAt)
		}
		return items[i].Case.ID < items[j].Case.ID
	})
	if len(items) > filter.Limit {
		items = items[:filter.Limit]
	}
	return items, nil
}

func (r *humanReviewAPIMemoryRepo) ListHumanAssigned(_ context.Context, filter domainreview.HumanQueueFilter) ([]*domainreview.HumanQueueItem, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now().UTC()
	items := make([]*domainreview.HumanQueueItem, 0, len(r.cases))
	for _, reviewCase := range r.cases {
		if reviewCase.Status != domainreview.CaseStatusPendingHuman ||
			reviewCase.AssignedReviewerID != filter.ReviewerID ||
			reviewCase.LeaseExpiresAt == nil || !reviewCase.LeaseExpiresAt.After(now) {
			continue
		}
		copyCase := *reviewCase
		items = append(items, &domainreview.HumanQueueItem{
			Case: &copyCase, AuthorID: 9, Title: fmt.Sprintf("video-%d", reviewCase.VideoID),
		})
	}
	sort.Slice(items, func(i, j int) bool {
		if !items[i].Case.LeaseExpiresAt.Equal(*items[j].Case.LeaseExpiresAt) {
			return items[i].Case.LeaseExpiresAt.Before(*items[j].Case.LeaseExpiresAt)
		}
		if items[i].Case.Priority != items[j].Case.Priority {
			return items[i].Case.Priority > items[j].Case.Priority
		}
		return items[i].Case.ID < items[j].Case.ID
	})
	if len(items) > filter.Limit {
		items = items[:filter.Limit]
	}
	return items, nil
}

func (r *humanReviewAPIMemoryRepo) ListHumanRecent(_ context.Context, filter domainreview.HumanQueueFilter) ([]*domainreview.HumanQueueItem, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := []*domainreview.HumanQueueItem{}
	seen := map[int64]struct{}{}
	for _, receipt := range r.receipts {
		if receipt == nil || receipt.Case == nil || receipt.Decision == nil ||
			receipt.Decision.ReviewerID != filter.ReviewerID {
			continue
		}
		if _, exists := seen[receipt.Case.ID]; exists {
			continue
		}
		seen[receipt.Case.ID] = struct{}{}
		copyCase := *receipt.Case
		items = append(items, &domainreview.HumanQueueItem{
			Case: &copyCase, AuthorID: 9, Title: fmt.Sprintf("video-%d", copyCase.VideoID),
		})
	}
	sort.Slice(items, func(i, j int) bool {
		left, right := items[i].Case, items[j].Case
		if left.ClosedAt != nil && right.ClosedAt != nil && !left.ClosedAt.Equal(*right.ClosedAt) {
			return left.ClosedAt.After(*right.ClosedAt)
		}
		return left.ID > right.ID
	})
	if len(items) > filter.Limit {
		items = items[:filter.Limit]
	}
	return items, nil
}

func (r *humanReviewAPIMemoryRepo) HumanQueueStats(context.Context, int, int) (int, time.Time, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	count := 0
	var oldest time.Time
	for _, reviewCase := range r.cases {
		if reviewCase.Status == domainreview.CaseStatusPendingHuman && reviewCase.AssignedReviewerID == 0 {
			count++
			if oldest.IsZero() || reviewCase.CreatedAt.Before(oldest) {
				oldest = reviewCase.CreatedAt
			}
		}
	}
	return count, oldest, nil
}

func (r *humanReviewAPIMemoryRepo) RecoverExpiredHumanLeases(context.Context, int) (int, error) {
	return 0, nil
}

func (r *humanReviewAPIMemoryRepo) GetHumanCaseDetail(_ context.Context, caseID int64) (*domainreview.HumanCaseDetail, error) {
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
			VideoID: reviewCase.VideoID, AuthorID: 9, Title: "video",
			ReviewVersion: reviewCase.ReviewVersion, PreviewAllowed: true,
			MediaURL: "https://media.example.test/video.mp4",
		},
	}, nil
}

func (r *humanReviewAPIMemoryRepo) ClaimHumanCase(_ context.Context, caseID, reviewerID int64, tokenHash string, expectedVersion int, duration time.Duration) (*domainreview.ReviewCase, error) {
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

func (r *humanReviewAPIMemoryRepo) RenewHumanLease(_ context.Context, caseID, reviewerID int64, tokenHash string, expectedVersion int, duration time.Duration) (*domainreview.ReviewCase, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	reviewCase := r.cases[caseID]
	if err := reviewCase.Renew(reviewerID, tokenHash, expectedVersion, time.Now().UTC(), duration); err != nil {
		return nil, err
	}
	copyCase := *reviewCase
	return &copyCase, nil
}
func (r *humanReviewAPIMemoryRepo) ResumeHumanLease(_ context.Context, caseID, reviewerID int64, tokenHash string, expectedVersion int, duration time.Duration) (*domainreview.ReviewCase, error) {
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
func (r *humanReviewAPIMemoryRepo) ReleaseHumanLease(_ context.Context, caseID, reviewerID int64, tokenHash string, expectedVersion int) (*domainreview.ReviewCase, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	reviewCase := r.cases[caseID]
	if err := reviewCase.Release(reviewerID, tokenHash, expectedVersion, time.Now().UTC()); err != nil {
		return nil, err
	}
	copyCase := *reviewCase
	return &copyCase, nil
}

func (r *humanReviewAPIMemoryRepo) CommitHumanDecision(_ context.Context, decision *domainreview.HumanDecision, tokenHash string, _ *domainadminaudit.Fact) (*domainreview.HumanDecisionResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := fmt.Sprintf("%d|%d|%s", decision.CaseID, decision.ReviewerID, decision.IdempotencyKeyHash)
	if existing := r.receipts[key]; existing != nil {
		if r.hashes[key] != decision.PayloadHash {
			return nil, domainreview.ErrDecisionIdentityConflict
		}
		replay := *existing
		replay.Duplicate = true
		return &replay, nil
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
	decision.ID = int64(len(r.receipts) + 1)
	copyCase := *reviewCase
	result := &domainreview.HumanDecisionResult{
		Case: &copyCase, Decision: decision, ApplySideEffects: true,
	}
	r.receipts[key] = result
	r.hashes[key] = decision.PayloadHash
	return result, nil
}

type humanReviewPrincipalReader struct {
	principal *domainaccount.AdminPrincipal
}

type humanReviewPreviewProvider struct{}

func (humanReviewPreviewProvider) ResolveHumanPreview(
	context.Context,
	domainreview.ReviewSubject,
	time.Duration,
) (*domainreview.HumanPreviewAccess, error) {
	return &domainreview.HumanPreviewAccess{
		MediaURL:  "https://signed.example.test/video.mp4",
		CoverURL:  "https://signed.example.test/cover.jpg",
		ExpiresAt: time.Now().UTC().Add(5 * time.Minute),
	}, nil
}

func (r humanReviewPrincipalReader) FindAdminPrincipalByID(context.Context, int64) (*domainaccount.AdminPrincipal, error) {
	return r.principal, nil
}

func newHumanReviewAPIRouter(repo *humanReviewAPIMemoryRepo, principal *domainaccount.AdminPrincipal) *server.Hertz {
	tokenBytes := append(bytes.Repeat([]byte{5}, 32), bytes.Repeat([]byte{6}, 32)...)
	tokenBytes = append(tokenBytes, bytes.Repeat([]byte{7}, 128)...)
	service := applicationreview.New(
		repo,
		applicationreview.WithHumanRepository(repo),
		applicationreview.WithHumanCursorSecret("api-cursor-secret"),
		applicationreview.WithHumanTokenReader(bytes.NewReader(tokenBytes)),
		applicationreview.WithHumanPreviewProvider(humanReviewPreviewProvider{}),
	)
	handler := interfaceshttpreview.New(service, nil)
	router := server.Default()
	auth := func(ctx context.Context, c *app.RequestContext) {
		c.Set(interfaceshttpmiddleware.ContextUserIDKey, principal.UserID)
		c.Set(interfaceshttpmiddleware.ContextAuthVersionKey, principal.AuthVersion)
		c.Next(ctx)
	}
	reader := humanReviewPrincipalReader{principal: principal}
	router.GET("/api/admin/review/cases", auth,
		interfaceshttpmiddleware.NewRequireAdminPermission(reader, domainaccount.PermissionReviewRead),
		handler.ListHumanQueue)
	router.GET("/api/admin/review/cases/:caseId", auth,
		interfaceshttpmiddleware.NewRequireAdminPermission(reader, domainaccount.PermissionReviewRead),
		handler.GetHumanCase)
	router.GET("/api/admin/review/cases/:caseId/preview-access", auth,
		interfaceshttpmiddleware.NewRequireAdminPermission(reader, domainaccount.PermissionReviewRead),
		handler.GetHumanPreview)
	router.POST("/api/admin/review/cases/:caseId/claim", auth,
		interfaceshttpmiddleware.NewRequireAdminPermission(reader, domainaccount.PermissionReviewDecide),
		handler.ClaimHumanCase)
	router.POST("/api/admin/review/cases/:caseId/lease/resume", auth,
		interfaceshttpmiddleware.NewRequireAdminPermission(reader, domainaccount.PermissionReviewDecide),
		handler.ResumeHumanLease)
	router.POST("/api/admin/review/cases/:caseId/lease/renew", auth,
		interfaceshttpmiddleware.NewRequireAdminPermission(reader, domainaccount.PermissionReviewDecide),
		handler.RenewHumanLease)
	router.DELETE("/api/admin/review/cases/:caseId/lease", auth,
		interfaceshttpmiddleware.NewRequireAdminPermission(reader, domainaccount.PermissionReviewDecide),
		handler.ReleaseHumanLease)
	router.POST("/api/admin/review/cases/:caseId/decision", auth,
		interfaceshttpmiddleware.NewRequireAdminPermission(reader, domainaccount.PermissionReviewDecide),
		handler.DecideHumanCase)
	return router
}

func TestHumanReviewAdminAPIFlow(t *testing.T) {
	repo := newHumanReviewAPIMemoryRepo()
	reviewer := domainaccount.RestoreAdminPrincipal(7, domainaccount.StatusNormal, domainaccount.RoleReviewer)
	router := newHumanReviewAPIRouter(repo, reviewer)

	first := ut.PerformRequest(router.Engine, http.MethodGet, "/api/admin/review/cases?limit=2", nil)
	if first.Code != http.StatusOK {
		t.Fatalf("queue status = %d body=%s", first.Code, first.Body.String())
	}
	var page struct {
		Items []struct {
			Case struct {
				ID int64 `json:"id"`
			} `json:"case"`
		} `json:"items"`
		NextCursor string `json:"next_cursor"`
		Scope      string `json:"scope"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 || page.Items[0].Case.ID != 1 ||
		page.NextCursor == "" || page.Scope != domainreview.HumanQueueScopeAvailable {
		t.Fatalf("queue page = %#v", page)
	}
	second := ut.PerformRequest(
		router.Engine, http.MethodGet,
		"/api/admin/review/cases?limit=2&cursor="+page.NextCursor, nil,
	)
	if second.Code != http.StatusOK || strings.Contains(second.Body.String(), `"id":1`) {
		t.Fatalf("second queue status=%d body=%s", second.Code, second.Body.String())
	}

	claim := performHumanReviewJSON(t, router, http.MethodPost, "/api/admin/review/cases/1/claim",
		`{"expected_case_version":1}`, "")
	if claim.Code != http.StatusOK {
		t.Fatalf("claim status=%d body=%s", claim.Code, claim.Body.String())
	}
	var lease struct {
		LeaseToken string `json:"lease_token"`
		ServerTime string `json:"server_time"`
		Case       struct {
			Version int `json:"version"`
		} `json:"case"`
	}
	if err := json.Unmarshal(claim.Body.Bytes(), &lease); err != nil {
		t.Fatal(err)
	}
	if lease.LeaseToken == "" || lease.ServerTime == "" || lease.Case.Version != 2 {
		t.Fatalf("lease = %#v", lease)
	}
	oldLeaseToken := lease.LeaseToken
	mine := ut.PerformRequest(
		router.Engine, http.MethodGet, "/api/admin/review/cases?scope=mine", nil,
	)
	if mine.Code != http.StatusOK || !strings.Contains(mine.Body.String(), `"id":1`) {
		t.Fatalf("mine status=%d body=%s", mine.Code, mine.Body.String())
	}
	detail := ut.PerformRequest(router.Engine, http.MethodGet, "/api/admin/review/cases/1", nil)
	if detail.Code != http.StatusOK || !strings.Contains(detail.Body.String(), `"video_id":101`) {
		t.Fatalf("detail status=%d body=%s", detail.Code, detail.Body.String())
	}
	preview := ut.PerformRequest(
		router.Engine, http.MethodGet, "/api/admin/review/cases/1/preview-access", nil,
	)
	if preview.Code != http.StatusOK ||
		!strings.Contains(preview.Body.String(), "https://signed.example.test/video.mp4") ||
		!strings.Contains(preview.Body.String(), `"server_time"`) {
		t.Fatalf("preview status=%d body=%s", preview.Code, preview.Body.String())
	}
	if preview.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("review preview response was cacheable: %q", preview.Header().Get("Cache-Control"))
	}
	resumed := performHumanReviewJSON(
		t, router, http.MethodPost, "/api/admin/review/cases/1/lease/resume",
		`{"expected_case_version":2}`, "",
	)
	if resumed.Code != http.StatusOK {
		t.Fatalf("resume status=%d body=%s", resumed.Code, resumed.Body.String())
	}
	if err := json.Unmarshal(resumed.Body.Bytes(), &lease); err != nil {
		t.Fatal(err)
	}
	if lease.Case.Version != 3 || lease.LeaseToken == oldLeaseToken {
		t.Fatalf("resumed lease = %#v", lease)
	}
	oldRenewBody := fmt.Sprintf(`{"lease_token":%q,"expected_case_version":3}`, oldLeaseToken)
	oldRenewed := performHumanReviewJSON(
		t, router, http.MethodPost, "/api/admin/review/cases/1/lease/renew", oldRenewBody, "",
	)
	if oldRenewed.Code != http.StatusConflict ||
		!strings.Contains(oldRenewed.Body.String(), "REVIEW_LEASE_NOT_OWNED") {
		t.Fatalf("old renew status=%d body=%s", oldRenewed.Code, oldRenewed.Body.String())
	}
	renewBody := fmt.Sprintf(`{"lease_token":%q,"expected_case_version":3}`, lease.LeaseToken)
	renewed := performHumanReviewJSON(
		t, router, http.MethodPost, "/api/admin/review/cases/1/lease/renew", renewBody, "",
	)
	if renewed.Code != http.StatusOK || !strings.Contains(renewed.Body.String(), `"version":4`) {
		t.Fatalf("renew status=%d body=%s", renewed.Code, renewed.Body.String())
	}
	releaseBody := fmt.Sprintf(`{"lease_token":%q,"expected_case_version":4}`, lease.LeaseToken)
	released := performHumanReviewJSON(
		t, router, http.MethodDelete, "/api/admin/review/cases/1/lease", releaseBody, "",
	)
	if released.Code != http.StatusOK || !strings.Contains(released.Body.String(), `"version":5`) {
		t.Fatalf("release status=%d body=%s", released.Code, released.Body.String())
	}
	claim = performHumanReviewJSON(t, router, http.MethodPost, "/api/admin/review/cases/1/claim",
		`{"expected_case_version":5}`, "")
	if claim.Code != http.StatusOK {
		t.Fatalf("reclaim status=%d body=%s", claim.Code, claim.Body.String())
	}
	if err := json.Unmarshal(claim.Body.Bytes(), &lease); err != nil {
		t.Fatal(err)
	}
	strict := performHumanReviewJSON(
		t, router, http.MethodPost, "/api/admin/review/cases/2/claim",
		`{"expected_case_version":1,"unexpected":true}`, "",
	)
	if strict.Code != http.StatusBadRequest {
		t.Fatalf("strict claim status=%d body=%s", strict.Code, strict.Body.String())
	}
	decisionBody := fmt.Sprintf(
		`{"lease_token":%q,"expected_case_version":6,"review_version":1,"outcome":"approve","reason_code":"content_compliant","note":""}`,
		lease.LeaseToken,
	)
	otherReviewerRouter := newHumanReviewAPIRouter(
		repo, domainaccount.RestoreAdminPrincipal(9, domainaccount.StatusNormal, domainaccount.RoleReviewer),
	)
	foreign := performHumanReviewJSON(
		t, otherReviewerRouter, http.MethodPost, "/api/admin/review/cases/1/decision",
		decisionBody, "foreign-decision",
	)
	if foreign.Code != http.StatusConflict || !strings.Contains(foreign.Body.String(), "REVIEW_LEASE_NOT_OWNED") {
		t.Fatalf("foreign decision status=%d body=%s", foreign.Code, foreign.Body.String())
	}
	decided := performHumanReviewJSON(
		t, router, http.MethodPost, "/api/admin/review/cases/1/decision", decisionBody, "decision-1",
	)
	if decided.Code != http.StatusOK || !strings.Contains(decided.Body.String(), `"duplicate":false`) {
		t.Fatalf("decision status=%d body=%s", decided.Code, decided.Body.String())
	}
	replayed := performHumanReviewJSON(
		t, router, http.MethodPost, "/api/admin/review/cases/1/decision", decisionBody, "decision-1",
	)
	if replayed.Code != http.StatusOK || !strings.Contains(replayed.Body.String(), `"duplicate":true`) {
		t.Fatalf("replay status=%d body=%s", replayed.Code, replayed.Body.String())
	}
	recent := ut.PerformRequest(
		router.Engine, http.MethodGet, "/api/admin/review/cases?scope=recent", nil,
	)
	if recent.Code != http.StatusOK || !strings.Contains(recent.Body.String(), `"id":1`) {
		t.Fatalf("recent status=%d body=%s", recent.Code, recent.Body.String())
	}
	changedBody := strings.Replace(decisionBody, "content_compliant", "false_positive", 1)
	conflict := performHumanReviewJSON(
		t, router, http.MethodPost, "/api/admin/review/cases/1/decision", changedBody, "decision-1",
	)
	if conflict.Code != http.StatusConflict ||
		!strings.Contains(conflict.Body.String(), "REVIEW_DECISION_IDEMPOTENCY_CONFLICT") {
		t.Fatalf("conflict status=%d body=%s", conflict.Code, conflict.Body.String())
	}

	userRouter := newHumanReviewAPIRouter(
		repo, domainaccount.RestoreAdminPrincipal(8, domainaccount.StatusNormal, domainaccount.RoleUser),
	)
	forbidden := ut.PerformRequest(userRouter.Engine, http.MethodGet, "/api/admin/review/cases", nil)
	if forbidden.Code != http.StatusForbidden {
		t.Fatalf("permission status=%d body=%s", forbidden.Code, forbidden.Body.String())
	}
	forbiddenPreview := ut.PerformRequest(
		userRouter.Engine, http.MethodGet, "/api/admin/review/cases/1/preview-access", nil,
	)
	if forbiddenPreview.Code != http.StatusForbidden {
		t.Fatalf("preview permission status=%d body=%s", forbiddenPreview.Code, forbiddenPreview.Body.String())
	}
}

func performHumanReviewJSON(
	t *testing.T,
	router *server.Hertz,
	method, path, body, idempotencyKey string,
) *ut.ResponseRecorder {
	t.Helper()
	headers := []ut.Header{{Key: "Content-Type", Value: "application/json"}}
	if idempotencyKey != "" {
		headers = append(headers, ut.Header{Key: "Idempotency-Key", Value: idempotencyKey})
	}
	return ut.PerformRequest(
		router.Engine, method, path,
		&ut.Body{Body: strings.NewReader(body), Len: len(body)}, headers...,
	)
}
