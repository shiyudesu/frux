package test

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	applicationadminaudit "github.com/shiyudesu/frux/internal/application/adminaudit"
	domainaccount "github.com/shiyudesu/frux/internal/domain/account"
	domainadminaudit "github.com/shiyudesu/frux/internal/domain/adminaudit"
	infrajwt "github.com/shiyudesu/frux/internal/infra/jwt"
	interfaceshttpadmin "github.com/shiyudesu/frux/internal/interfaces/http/admin"
	interfaceshttpapierror "github.com/shiyudesu/frux/internal/interfaces/http/apierror"
	interfaceshttpmiddleware "github.com/shiyudesu/frux/internal/interfaces/http/middleware"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
)

type memoryAdminAuditRepository struct {
	mu       sync.Mutex
	nextID   int64
	facts    []*domainadminaudit.Fact
	appended chan struct{}
}

func (r *memoryAdminAuditRepository) Append(_ context.Context, fact *domainadminaudit.Fact) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	persisted := cloneAdminAuditFactWithID(fact, r.nextID)
	r.nextID++
	r.facts = append(r.facts, persisted)
	if r.appended != nil {
		select {
		case r.appended <- struct{}{}:
		default:
		}
	}
	return nil
}

func (r *memoryAdminAuditRepository) List(_ context.Context, query domainadminaudit.Query) ([]*domainadminaudit.Fact, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]*domainadminaudit.Fact, 0, len(r.facts))
	for _, fact := range r.facts {
		if fact.CreatedAt().Before(query.From) || fact.CreatedAt().After(query.To) {
			continue
		}
		if query.ActorID > 0 && fact.ActorID() != query.ActorID {
			continue
		}
		if query.Action != "" && fact.Action() != query.Action {
			continue
		}
		if query.TargetType != "" && fact.TargetType() != query.TargetType {
			continue
		}
		if query.Outcome != "" && fact.Outcome() != query.Outcome {
			continue
		}
		if query.Cursor != nil && !(fact.CreatedAt().Before(query.Cursor.CreatedAt) ||
			(fact.CreatedAt().Equal(query.Cursor.CreatedAt) && fact.ID() < query.Cursor.EventID)) {
			continue
		}
		items = append(items, fact)
	}
	sort.Slice(items, func(left, right int) bool {
		if items[left].CreatedAt().Equal(items[right].CreatedAt()) {
			return items[left].ID() > items[right].ID()
		}
		return items[left].CreatedAt().After(items[right].CreatedAt())
	})
	if len(items) > query.Limit {
		items = items[:query.Limit]
	}
	return append([]*domainadminaudit.Fact(nil), items...), nil
}

func TestAdminAuditAPIFlow(t *testing.T) {
	base := time.Date(2026, 8, 5, 8, 0, 0, 0, time.UTC)
	repository := &memoryAdminAuditRepository{
		nextID:   4,
		appended: make(chan struct{}, 1),
		facts: []*domainadminaudit.Fact{
			mustTestAdminAuditFact(t, 1, 21, "video-1", base.Add(time.Minute)),
			mustTestAdminAuditFact(t, 2, 22, "video-2", base.Add(2*time.Minute)),
			mustTestAdminAuditFact(t, 3, 21, "video-3", base.Add(3*time.Minute)),
		},
	}
	auditService := applicationadminaudit.New(
		repository,
		applicationadminaudit.WithClock(func() time.Time { return base.Add(2 * time.Hour) }),
		applicationadminaudit.WithLogger(log.New(io.Discard, "", 0)),
	)
	jwtManager, err := infrajwt.NewManager("test-secret", "15m")
	if err != nil {
		t.Fatalf("new jwt manager: %v", err)
	}
	principalReader := &adminAuthorizationReader{principals: map[int64]*domainaccount.AdminPrincipal{
		10: domainaccount.RestoreAdminPrincipal(10, domainaccount.StatusNormal, domainaccount.RoleOperator),
		11: domainaccount.RestoreAdminPrincipal(11, domainaccount.StatusNormal, domainaccount.RoleReviewer),
	}}
	handler := interfaceshttpadmin.New(interfaceshttpadmin.WithAuditQueryService(auditService))
	router := server.New(server.WithDisablePrintRoute(true))
	api := router.Group("/api")
	admin := api.Group("/admin", interfaceshttpmiddleware.NewJWTAuth(jwtManager))
	admin.GET(
		"/audit-events",
		interfaceshttpmiddleware.NewRequireAdminPermission(
			principalReader,
			domainaccount.PermissionAuditRead,
			interfaceshttpmiddleware.WithDeniedAttemptAudit(
				auditService,
				domainadminaudit.ActionAuditQuery,
				domainadminaudit.TargetAuditTrail,
				"events",
			),
		),
		handler.ListAuditEvents,
	)

	operatorToken := signAdminAuthorizationToken(t, jwtManager, 10, domainaccount.RoleUser)
	queryPath := adminAuditQueryPath(base, base.Add(time.Hour), url.Values{"limit": {"2"}})
	first := performAdminAuditRequest(router, queryPath, operatorToken, "")
	if first.Code != http.StatusOK {
		t.Fatalf("first page status = %d body=%s", first.Code, first.Body.String())
	}
	var firstPage adminAuditPageResponse
	if err := json.Unmarshal(first.Body.Bytes(), &firstPage); err != nil {
		t.Fatalf("decode first page: %v", err)
	}
	if !firstPage.HasMore || firstPage.NextCursor == "" || len(firstPage.Items) != 2 ||
		firstPage.Items[0].ID != 3 || firstPage.Items[1].ID != 2 {
		t.Fatalf("unexpected first page: %+v", firstPage)
	}
	if strings.Contains(first.Body.String(), "access_token") || strings.Contains(first.Body.String(), "password") {
		t.Fatalf("audit response leaked forbidden detail: %s", first.Body.String())
	}

	secondPath := adminAuditQueryPath(base, base.Add(time.Hour), url.Values{
		"limit":  {"2"},
		"cursor": {firstPage.NextCursor},
	})
	second := performAdminAuditRequest(router, secondPath, operatorToken, "")
	if second.Code != http.StatusOK {
		t.Fatalf("second page status = %d body=%s", second.Code, second.Body.String())
	}
	var secondPage adminAuditPageResponse
	if err := json.Unmarshal(second.Body.Bytes(), &secondPage); err != nil {
		t.Fatalf("decode second page: %v", err)
	}
	if secondPage.HasMore || len(secondPage.Items) != 1 || secondPage.Items[0].ID != 1 {
		t.Fatalf("unexpected second page: %+v", secondPage)
	}

	filteredPath := adminAuditQueryPath(base, base.Add(time.Hour), url.Values{
		"actor_id": {"21"},
		"action":   {string(domainadminaudit.ActionContentEnforce)},
		"outcome":  {string(domainadminaudit.OutcomeSuccess)},
	})
	filtered := performAdminAuditRequest(router, filteredPath, operatorToken, "")
	if filtered.Code != http.StatusOK {
		t.Fatalf("filtered status = %d body=%s", filtered.Code, filtered.Body.String())
	}
	var filteredPage adminAuditPageResponse
	if err := json.Unmarshal(filtered.Body.Bytes(), &filteredPage); err != nil {
		t.Fatalf("decode filtered page: %v", err)
	}
	if len(filteredPage.Items) != 2 || filteredPage.Items[0].ActorID != 21 || filteredPage.Items[1].ActorID != 21 {
		t.Fatalf("unexpected filtered page: %+v", filteredPage)
	}

	changedFilterPath := adminAuditQueryPath(base, base.Add(time.Hour), url.Values{
		"limit":    {"2"},
		"cursor":   {firstPage.NextCursor},
		"actor_id": {"21"},
	})
	changedFilter := performAdminAuditRequest(router, changedFilterPath, operatorToken, "")
	requireAdminAuthorizationError(
		t,
		changedFilter,
		http.StatusBadRequest,
		interfaceshttpapierror.CodeAdminAuditCursorInvalid,
	)

	invalidRangePath := adminAuditQueryPath(base.Add(time.Hour), base, nil)
	invalidRange := performAdminAuditRequest(router, invalidRangePath, operatorToken, "")
	requireAdminAuthorizationError(
		t,
		invalidRange,
		http.StatusBadRequest,
		interfaceshttpapierror.CodeAdminAuditQueryInvalid,
	)

	reviewerToken := signAdminAuthorizationToken(t, jwtManager, 11, domainaccount.RoleAdmin)
	forbidden := performAdminAuditRequest(router, queryPath, reviewerToken, "audit-denied-request")
	requireAdminAuthorizationError(
		t,
		forbidden,
		http.StatusForbidden,
		interfaceshttpapierror.CodeAdminPermissionDenied,
	)
	select {
	case <-repository.appended:
	case <-time.After(time.Second):
		t.Fatal("denied audit attempt was not recorded")
	}
	repository.mu.Lock()
	denied := repository.facts[len(repository.facts)-1]
	repository.mu.Unlock()
	if denied.ActorID() != 11 || denied.Outcome() != domainadminaudit.OutcomeDenied ||
		!strings.HasPrefix(denied.RequestID(), "audit-") ||
		denied.RequestID() == "audit-denied-request" ||
		denied.Detail()["reason_code"] != "permission_denied" {
		t.Fatalf("unexpected denied audit fact: actor=%d outcome=%q request=%q detail=%#v",
			denied.ActorID(), denied.Outcome(), denied.RequestID(), denied.Detail())
	}
}

type adminAuditPageResponse struct {
	Items []struct {
		ID      int64             `json:"id"`
		ActorID int64             `json:"actor_id"`
		Detail  map[string]string `json:"detail"`
	} `json:"items"`
	NextCursor string `json:"next_cursor"`
	HasMore    bool   `json:"has_more"`
}

func mustTestAdminAuditFact(t *testing.T, id, actorID int64, targetID string, createdAt time.Time) *domainadminaudit.Fact {
	t.Helper()
	fact, err := domainadminaudit.RestoreFact(id, domainadminaudit.FactInput{
		ActorID: actorID, Permission: domainaccount.PermissionContentEnforce,
		Action: domainadminaudit.ActionContentEnforce, TargetType: domainadminaudit.TargetVideo,
		TargetID: targetID, Outcome: domainadminaudit.OutcomeSuccess,
		RequestID: domainadminaudit.NewRequestID(),
		Detail: map[string]string{
			"http_method": "POST", "route": "/api/admin/videos/:videoId/enforcement",
			"reason_code": "policy_violation", "previous_status": "published", "new_status": "offline",
		},
		CreatedAt: createdAt,
	})
	if err != nil {
		t.Fatalf("restore audit fact: %v", err)
	}
	return fact
}

func cloneAdminAuditFactWithID(fact *domainadminaudit.Fact, id int64) *domainadminaudit.Fact {
	cloned, err := domainadminaudit.RestoreFact(id, domainadminaudit.FactInput{
		ActorID: fact.ActorID(), Permission: fact.Permission(), Action: fact.Action(),
		TargetType: fact.TargetType(), TargetID: fact.TargetID(), Outcome: fact.Outcome(),
		RequestID: fact.RequestID(), IdempotencyKeyHash: fact.IdempotencyKeyHash(),
		Detail: fact.Detail(), CreatedAt: fact.CreatedAt(),
	})
	if err != nil {
		panic(err)
	}
	return cloned
}

func adminAuditQueryPath(from, to time.Time, extra url.Values) string {
	values := url.Values{
		"from": {from.Format(time.RFC3339)},
		"to":   {to.Format(time.RFC3339)},
	}
	for key, entries := range extra {
		for _, entry := range entries {
			values.Add(key, entry)
		}
	}
	return "/api/admin/audit-events?" + values.Encode()
}

func performAdminAuditRequest(router *server.Hertz, path, token, requestID string) *ut.ResponseRecorder {
	headers := []ut.Header{{Key: "Authorization", Value: "Bearer " + token}}
	if requestID != "" {
		headers = append(headers, ut.Header{Key: "X-Request-ID", Value: requestID})
	}
	return ut.PerformRequest(router.Engine, http.MethodGet, path, nil, headers...)
}
