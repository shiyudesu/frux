package test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	applicationgovernance "github.com/shiyudesu/frux/internal/application/governance"
	domainaccount "github.com/shiyudesu/frux/internal/domain/account"
	domainadminaudit "github.com/shiyudesu/frux/internal/domain/adminaudit"
	domaingovernance "github.com/shiyudesu/frux/internal/domain/governance"
	infrajwt "github.com/shiyudesu/frux/internal/infra/jwt"
	interfaceshttpapierror "github.com/shiyudesu/frux/internal/interfaces/http/apierror"
	interfaceshttpgovernance "github.com/shiyudesu/frux/internal/interfaces/http/governance"
	interfaceshttpmiddleware "github.com/shiyudesu/frux/internal/interfaces/http/middleware"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
)

type apiGovernanceRepository struct {
	mu        sync.Mutex
	active    *domaingovernance.Revision
	revisions []*domaingovernance.Revision
	audits    []*domainadminaudit.Fact
	failAudit bool
}

func (r *apiGovernanceRepository) ListActive(context.Context) ([]*domaingovernance.Revision, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.active == nil {
		return nil, nil
	}
	return []*domaingovernance.Revision{r.active}, nil
}

func (r *apiGovernanceRepository) ListRevisions(
	_ context.Context,
	_ domaingovernance.Key,
	limit int,
) ([]*domaingovernance.Revision, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]*domaingovernance.Revision, 0, min(limit, len(r.revisions)))
	for index := len(r.revisions) - 1; index >= 0 && len(result) < limit; index-- {
		result = append(result, r.revisions[index])
	}
	return result, nil
}

func (r *apiGovernanceRepository) FindRevision(
	_ context.Context,
	_ domaingovernance.Key,
	revision int64,
) (*domaingovernance.Revision, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, candidate := range r.revisions {
		if candidate.Number() == revision {
			return candidate, nil
		}
	}
	return nil, domaingovernance.ErrRevisionNotFound
}

func (r *apiGovernanceRepository) CommitRevision(
	_ context.Context,
	expectedRevision int64,
	revision *domaingovernance.Revision,
	fact *domainadminaudit.Fact,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	current := int64(0)
	if r.active != nil {
		current = r.active.Number()
	}
	if current != expectedRevision {
		return domaingovernance.ErrRevisionConflict
	}
	if r.failAudit {
		return errors.New("forced audit failure")
	}
	r.revisions = append(r.revisions, revision)
	r.active = revision
	r.audits = append(r.audits, fact)
	return nil
}

func TestGovernanceAdminAPIFlow(t *testing.T) {
	now := time.Date(2026, 8, 6, 11, 0, 0, 0, time.UTC)
	repository := &apiGovernanceRepository{}
	service := applicationgovernance.New(
		domaingovernance.DefaultRegistry(),
		repository,
		applicationgovernance.WithClock(func() time.Time { return now }),
	)
	handler := interfaceshttpgovernance.New(service)
	jwtManager, err := infrajwt.NewManager("governance-test-secret", "15m")
	if err != nil {
		t.Fatalf("new jwt manager: %v", err)
	}
	principals := &adminAuthorizationReader{principals: map[int64]*domainaccount.AdminPrincipal{
		10: domainaccount.RestoreAdminPrincipal(10, domainaccount.StatusNormal, domainaccount.RoleOperator),
		11: domainaccount.RestoreAdminPrincipal(11, domainaccount.StatusNormal, domainaccount.RoleReviewer),
	}}
	router := server.New(server.WithDisablePrintRoute(true))
	admin := router.Group(
		"/api/admin",
		interfaceshttpmiddleware.NewJWTAuth(jwtManager),
	)
	requireGovernance := interfaceshttpmiddleware.NewRequireAdminPermission(
		principals, domainaccount.PermissionGovernanceExecute,
	)
	admin.GET("/governance/controls", requireGovernance, handler.List)
	admin.GET("/governance/controls/:key/revisions", requireGovernance, handler.ListRevisions)
	admin.PATCH("/governance/controls/:key", requireGovernance, handler.Update)
	admin.POST("/governance/controls/:key/rollback", requireGovernance, handler.Rollback)

	operatorToken := signAdminAuthorizationToken(t, jwtManager, 10, domainaccount.RoleUser)
	reviewerToken := signAdminAuthorizationToken(t, jwtManager, 11, domainaccount.RoleAdmin)
	keyPath := "/api/admin/governance/controls/" + string(domaingovernance.FeedPreloadEnabled)

	forbidden := performGovernanceRequest(router, http.MethodGet, "/api/admin/governance/controls", reviewerToken, nil)
	requireAdminAuthorizationError(
		t, forbidden, http.StatusForbidden, interfaceshttpapierror.CodeAdminPermissionDenied,
	)

	unknown := performGovernanceRequest(
		router, http.MethodPatch, "/api/admin/governance/controls/unknown",
		operatorToken, map[string]any{
			"expected_revision": 0, "value": false, "reason": "unknown key",
		},
	)
	requireAdminAuthorizationError(
		t, unknown, http.StatusBadRequest, interfaceshttpapierror.CodeGovernanceControlUnknown,
	)

	first := performGovernanceRequest(router, http.MethodPatch, keyPath, operatorToken, map[string]any{
		"expected_revision": 0, "value": false, "reason": "disable optional preloading",
	})
	requireGovernanceRevision(t, first, http.StatusOK, 1, false, 0)

	conflict := performGovernanceRequest(router, http.MethodPatch, keyPath, operatorToken, map[string]any{
		"expected_revision": 0, "value": true, "reason": "stale operator",
	})
	requireAdminAuthorizationError(
		t, conflict, http.StatusConflict, interfaceshttpapierror.CodeGovernanceRevisionConflict,
	)

	second := performGovernanceRequest(router, http.MethodPatch, keyPath, operatorToken, map[string]any{
		"expected_revision": 1, "value": true, "reason": "restore optional preloading",
	})
	requireGovernanceRevision(t, second, http.StatusOK, 2, true, 0)

	rollback := performGovernanceRequest(
		router, http.MethodPost, keyPath+"/rollback", operatorToken,
		map[string]any{
			"expected_revision": 2, "target_revision": 1,
			"reason": "rollback regression",
		},
	)
	requireGovernanceRevision(t, rollback, http.StatusOK, 3, false, 1)

	expired := performGovernanceRequest(router, http.MethodPatch, keyPath, operatorToken, map[string]any{
		"expected_revision": 3, "value": true, "reason": "expired switch",
		"expires_at": now.Add(-time.Second).Format(time.RFC3339),
	})
	requireAdminAuthorizationError(
		t, expired, http.StatusBadRequest, interfaceshttpapierror.CodeGovernanceValidationFailed,
	)

	list := performGovernanceRequest(
		router, http.MethodGet, "/api/admin/governance/controls", operatorToken, nil,
	)
	if list.Code != http.StatusOK || !bytes.Contains(list.Body.Bytes(), []byte(`"revision":3`)) ||
		!bytes.Contains(list.Body.Bytes(), []byte(`"max_staleness_seconds":120`)) {
		t.Fatalf("unexpected control list status=%d body=%s", list.Code, list.Body.String())
	}
	history := performGovernanceRequest(
		router, http.MethodGet, keyPath+"/revisions?limit=2", operatorToken, nil,
	)
	if history.Code != http.StatusOK || !bytes.Contains(history.Body.Bytes(), []byte(`"revision":3`)) ||
		!bytes.Contains(history.Body.Bytes(), []byte(`"revision":2`)) {
		t.Fatalf("unexpected revision history status=%d body=%s", history.Code, history.Body.String())
	}
	if len(repository.audits) != 3 ||
		repository.audits[2].Detail()["operation"] != "rollback" {
		t.Fatalf("unexpected governance audit facts: %#v", repository.audits)
	}
}

func TestGovernanceAPIAuditFailureDoesNotActivateRevision(t *testing.T) {
	repository := &apiGovernanceRepository{failAudit: true}
	service := applicationgovernance.New(
		domaingovernance.DefaultRegistry(), repository,
		applicationgovernance.WithClock(func() time.Time {
			return time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
		}),
	)
	handler := interfaceshttpgovernance.New(service)
	jwtManager, err := infrajwt.NewManager("governance-audit-test-secret", "15m")
	if err != nil {
		t.Fatalf("new jwt manager: %v", err)
	}
	principals := &adminAuthorizationReader{principals: map[int64]*domainaccount.AdminPrincipal{
		10: domainaccount.RestoreAdminPrincipal(10, domainaccount.StatusNormal, domainaccount.RoleOperator),
	}}
	router := server.New(server.WithDisablePrintRoute(true))
	admin := router.Group("/api/admin", interfaceshttpmiddleware.NewJWTAuth(jwtManager))
	admin.PATCH(
		"/governance/controls/:key",
		interfaceshttpmiddleware.NewRequireAdminPermission(
			principals, domainaccount.PermissionGovernanceExecute,
		),
		handler.Update,
	)
	token := signAdminAuthorizationToken(t, jwtManager, 10, domainaccount.RoleUser)
	response := performGovernanceRequest(
		router, http.MethodPatch,
		"/api/admin/governance/controls/"+string(domaingovernance.FeedPreloadEnabled),
		token, map[string]any{
			"expected_revision": 0, "value": false, "reason": "must be audited",
		},
	)
	requireAdminAuthorizationError(
		t, response, http.StatusServiceUnavailable, interfaceshttpapierror.CodeGovernanceUnavailable,
	)
	if repository.active != nil || len(repository.revisions) != 0 {
		t.Fatal("audit failure activated a governance revision")
	}
}

func performGovernanceRequest(
	router *server.Hertz,
	method, path, token string,
	body map[string]any,
) *ut.ResponseRecorder {
	var payload []byte
	if body != nil {
		payload, _ = json.Marshal(body)
	}
	headers := []ut.Header{{Key: "Authorization", Value: "Bearer " + token}}
	if body != nil {
		headers = append(headers, ut.Header{Key: "Content-Type", Value: "application/json"})
	}
	var requestBody *ut.Body
	if body != nil {
		requestBody = &ut.Body{Body: bytes.NewReader(payload), Len: len(payload)}
	}
	return ut.PerformRequest(router.Engine, method, path, requestBody, headers...)
}

func requireGovernanceRevision(
	t *testing.T,
	response *ut.ResponseRecorder,
	status int,
	revision int64,
	value bool,
	rollbackFrom int64,
) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("status=%d want=%d body=%s", response.Code, status, response.Body.String())
	}
	var body struct {
		Revision             int64 `json:"revision"`
		Value                bool  `json:"value"`
		RollbackFromRevision int64 `json:"rollback_from_revision"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode revision response: %v", err)
	}
	if body.Revision != revision || body.Value != value ||
		body.RollbackFromRevision != rollbackFrom {
		t.Fatalf("unexpected revision response: %+v", body)
	}
}
