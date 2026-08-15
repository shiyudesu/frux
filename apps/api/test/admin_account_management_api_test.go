package test

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	applicationaccount "github.com/shiyudesu/frux/internal/application/account"
	applicationmessage "github.com/shiyudesu/frux/internal/application/message"
	domainaccount "github.com/shiyudesu/frux/internal/domain/account"
	domainadminaudit "github.com/shiyudesu/frux/internal/domain/adminaudit"
	infrajwt "github.com/shiyudesu/frux/internal/infra/jwt"
	interfaceshttpaccount "github.com/shiyudesu/frux/internal/interfaces/http/account"
	interfaceshttpapierror "github.com/shiyudesu/frux/internal/interfaces/http/apierror"
	interfaceshttpmessage "github.com/shiyudesu/frux/internal/interfaces/http/message"
	interfaceshttpmiddleware "github.com/shiyudesu/frux/internal/interfaces/http/middleware"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
)

type managedAccountAPIEntry struct {
	account *domainaccount.ManagedAccount
	role    string
}

type managedAccountAPIOperation struct {
	fingerprint string
	result      *domainaccount.AccountManagementResult
}

type managedAccountAPIMemoryRepo struct {
	mu         sync.Mutex
	entries    map[int64]*managedAccountAPIEntry
	operations map[string]managedAccountAPIOperation
	audits     []*domainadminaudit.Fact
}

func (r *managedAccountAPIMemoryRepo) ListManagedAccounts(
	_ context.Context,
	query domainaccount.ManagedAccountQuery,
) ([]*domainaccount.ManagedAccount, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]*domainaccount.ManagedAccount, 0, len(r.entries))
	for _, entry := range r.entries {
		if entry.role != domainaccount.RoleUser {
			continue
		}
		account := entry.account
		if query.UserID > 0 && account.ID != query.UserID {
			continue
		}
		if query.Status != 0 && account.Status != query.Status {
			continue
		}
		if query.Search != "" &&
			!strings.Contains(strings.ToLower(account.Account), strings.ToLower(query.Search)) &&
			!strings.Contains(strings.ToLower(account.Nickname), strings.ToLower(query.Search)) {
			continue
		}
		if query.Cursor != nil &&
			(account.CreatedAt.After(query.Cursor.CreatedAt) ||
				(account.CreatedAt.Equal(query.Cursor.CreatedAt) && account.ID >= query.Cursor.UserID)) {
			continue
		}
		copyAccount := *account
		items = append(items, &copyAccount)
	}
	sort.Slice(items, func(left, right int) bool {
		if items[left].CreatedAt.Equal(items[right].CreatedAt) {
			return items[left].ID > items[right].ID
		}
		return items[left].CreatedAt.After(items[right].CreatedAt)
	})
	if len(items) > query.Limit {
		items = items[:query.Limit]
	}
	return items, nil
}

func (r *managedAccountAPIMemoryRepo) GetManagedAccount(
	_ context.Context,
	userID int64,
) (*domainaccount.ManagedAccount, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry := r.entries[userID]
	if entry == nil || entry.role != domainaccount.RoleUser {
		return nil, domainaccount.ErrManagedAccountNotFound
	}
	copyAccount := *entry.account
	return &copyAccount, nil
}

func (r *managedAccountAPIMemoryRepo) CommitManagedAccountOperation(
	_ context.Context,
	command domainaccount.AccountManagementCommand,
	buildAudit func(domainaccount.AccountManagementAuditInput) (*domainadminaudit.Fact, error),
) (*domainaccount.AccountManagementResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := strings.Join([]string{
		strconv.FormatInt(command.ActorID, 10), command.IdempotencyKey,
	}, ":")
	if existing, ok := r.operations[key]; ok {
		if existing.fingerprint != command.Fingerprint() {
			return nil, domainaccount.ErrAccountManagementIdempotencyConflict
		}
		copyResult := *existing.result
		copyResult.Replayed = true
		return &copyResult, nil
	}
	entry := r.entries[command.UserID]
	if entry == nil || entry.role != domainaccount.RoleUser {
		return nil, domainaccount.ErrManagedAccountNotFound
	}
	account := entry.account
	if account.Version != command.ExpectedVersion {
		return nil, domainaccount.ErrAccountManagementVersionConflict
	}
	nextStatus, revoke, err := command.Transition(account.Status)
	if err != nil {
		return nil, err
	}
	previousStatus, previousVersion := account.Status, account.Version
	revoked := int64(0)
	if revoke {
		revoked = int64(account.ActiveSessionCount)
		account.ActiveSessionCount = 0
	}
	account.Status = nextStatus
	account.Version++
	account.UpdatedAt = command.OccurredAt
	fact, err := buildAudit(domainaccount.AccountManagementAuditInput{
		PreviousStatus: previousStatus, NewStatus: nextStatus,
		PreviousVersion: previousVersion, NewVersion: account.Version,
		RevokedSessionCount: revoked,
	})
	if err != nil {
		return nil, err
	}
	result, err := domainaccount.RestoreAccountManagementResult(
		account.ID, command.Operation, account.Status, account.Version, revoked, command.OccurredAt,
	)
	if err != nil {
		return nil, err
	}
	r.audits = append(r.audits, fact)
	r.operations[key] = managedAccountAPIOperation{
		fingerprint: command.Fingerprint(), result: result,
	}
	copyResult := *result
	return &copyResult, nil
}

type managedAccountPrincipalReader struct {
	principals map[int64]*domainaccount.AdminPrincipal
}

func (r managedAccountPrincipalReader) FindAdminPrincipalByID(
	_ context.Context,
	userID int64,
) (*domainaccount.AdminPrincipal, error) {
	principal := r.principals[userID]
	if principal == nil {
		return nil, domainaccount.ErrUserNotFound
	}
	return principal, nil
}

func TestAdminAccountManagementAPIFlow(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	repository := &managedAccountAPIMemoryRepo{
		entries: map[int64]*managedAccountAPIEntry{
			1: {account: &domainaccount.ManagedAccount{
				ID: 1, Account: "alice-login", Nickname: "Alice",
				Status: domainaccount.StatusNormal, Version: 1,
				ActiveSessionCount: 2, PublicWorkCount: 3,
				CreatedAt: now, UpdatedAt: now,
			}, role: domainaccount.RoleUser},
			2: {account: &domainaccount.ManagedAccount{
				ID: 2, Account: "bob-login", Nickname: "Bob",
				Status: domainaccount.StatusFrozen, Version: 4,
				CreatedAt: now.Add(-time.Minute), UpdatedAt: now,
			}, role: domainaccount.RoleUser},
			3: {account: &domainaccount.ManagedAccount{
				ID: 3, Account: "privileged-login", Nickname: "Privileged",
				Status: domainaccount.StatusNormal, Version: 1,
				CreatedAt: now.Add(-2 * time.Minute), UpdatedAt: now,
			}, role: domainaccount.RoleAdmin},
		},
		operations: make(map[string]managedAccountAPIOperation),
	}
	jwtManager, err := infrajwt.NewManager("test-secret", "15m")
	if err != nil {
		t.Fatal(err)
	}
	principals := managedAccountPrincipalReader{principals: map[int64]*domainaccount.AdminPrincipal{
		100: domainaccount.RestoreAdminPrincipal(100, domainaccount.StatusNormal, domainaccount.RoleAdmin),
		101: domainaccount.RestoreAdminPrincipal(101, domainaccount.StatusNormal, domainaccount.RoleOperator),
	}}
	service := applicationaccount.NewManagement(repository, "cursor-secret")
	handler := interfaceshttpaccount.NewManagementHandler(service)
	router := server.New(server.WithDisablePrintRoute(true))
	admin := router.Group("/api/admin", interfaceshttpmiddleware.NewAdminJWTAuth(jwtManager))
	admin.GET("/accounts", interfaceshttpmiddleware.NewRequireAdminPermission(
		principals, domainaccount.PermissionAccountManage,
	), handler.List)
	admin.GET("/accounts/:userId", interfaceshttpmiddleware.NewRequireAdminPermission(
		principals, domainaccount.PermissionAccountManage,
	), handler.Get)
	admin.POST("/accounts/:userId/freeze", interfaceshttpmiddleware.NewRequireAdminPermission(
		principals, domainaccount.PermissionAccountManage,
	), handler.Freeze)
	admin.POST("/accounts/:userId/unfreeze", interfaceshttpmiddleware.NewRequireAdminPermission(
		principals, domainaccount.PermissionAccountManage,
	), handler.Unfreeze)
	admin.POST("/accounts/:userId/sessions/revoke", interfaceshttpmiddleware.NewRequireAdminPermission(
		principals, domainaccount.PermissionAccountManage,
	), handler.RevokeSessions)

	adminToken := signAdminAuthorizationToken(t, jwtManager, 100, domainaccount.RoleUser)
	operatorToken := signAdminAuthorizationToken(t, jwtManager, 101, domainaccount.RoleAdmin)
	consumerToken, err := jwtManager.SignAccessToken(100, domainaccount.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	residualUserToken, err := jwtManager.SignAccessToken(1, domainaccount.RoleUser)
	if err != nil {
		t.Fatal(err)
	}
	messageRepository := newMemoryMessageRepo()
	messageService := applicationmessage.New(messageRepository)
	messageHandler := interfaceshttpmessage.New(messageService)
	messageRouter := server.New(server.WithDisablePrintRoute(true))
	messageRouter.GET(
		"/api/messages",
		interfaceshttpmiddleware.NewJWTAuth(jwtManager),
		messageHandler.List,
	)

	list := performManagedAccountRequest(
		router, http.MethodGet, "/api/admin/accounts?query=login&limit=1", "", adminToken, "",
	)
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), `"account":"alice-login"`) ||
		strings.Contains(list.Body.String(), "privileged-login") ||
		strings.Contains(list.Body.String(), "password") ||
		!strings.Contains(list.Body.String(), `"has_more":true`) {
		t.Fatalf("list status=%d body=%s", list.Code, list.Body.String())
	}
	privileged := performManagedAccountRequest(
		router, http.MethodGet, "/api/admin/accounts/3", "", adminToken, "",
	)
	requireManagedAccountAPIError(
		t, privileged, http.StatusNotFound, interfaceshttpapierror.CodeAdminUserAccountNotFound,
	)
	forbidden := performManagedAccountRequest(
		router, http.MethodGet, "/api/admin/accounts", "", operatorToken, "",
	)
	requireManagedAccountAPIError(
		t, forbidden, http.StatusForbidden, interfaceshttpapierror.CodeAdminPermissionDenied,
	)
	wrongPurpose := performManagedAccountRequest(
		router, http.MethodGet, "/api/admin/accounts", "", consumerToken, "",
	)
	requireManagedAccountAPIError(
		t, wrongPurpose, http.StatusUnauthorized, interfaceshttpapierror.CodeAdminAuthInvalidAccessToken,
	)

	freezeBody := `{"expected_version":1,"reason_code":"abuse"}`
	frozen := performManagedAccountRequest(
		router, http.MethodPost, "/api/admin/accounts/1/freeze",
		freezeBody, adminToken, "freeze-key",
	)
	if frozen.Code != http.StatusOK ||
		!strings.Contains(frozen.Body.String(), `"status_name":"frozen"`) ||
		!strings.Contains(frozen.Body.String(), `"revoked_session_count":2`) ||
		!strings.Contains(frozen.Body.String(), `"audit_committed":true`) {
		t.Fatalf("freeze status=%d body=%s", frozen.Code, frozen.Body.String())
	}
	freezeNotification, err := domainaccount.NewAccountLifecycleNotification(
		1, domainaccount.AccountOperationFreeze,
		domainaccount.AccountReasonAbuse, 2, now,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := messageService.CreateAccountLifecycle(
		context.Background(), *freezeNotification,
	); err != nil {
		t.Fatal(err)
	}
	if replay, err := messageService.CreateAccountLifecycle(
		context.Background(), *freezeNotification,
	); err != nil || replay.Created {
		t.Fatalf("freeze message replay=%+v err=%v", replay, err)
	}
	residualMessages := performJSONRequest(
		messageRouter, http.MethodGet, "/api/messages", "", residualUserToken,
	)
	requireStatus(t, residualMessages, http.StatusOK)
	if !strings.Contains(residualMessages.Body.String(), `"title":"账号已被冻结"`) ||
		!strings.Contains(residualMessages.Body.String(), "存在滥用行为") ||
		strings.Count(residualMessages.Body.String(), `"title":"账号已被冻结"`) != 1 {
		t.Fatalf("residual messages body=%s", residualMessages.Body.String())
	}
	replayed := performManagedAccountRequest(
		router, http.MethodPost, "/api/admin/accounts/1/freeze",
		freezeBody, adminToken, "freeze-key",
	)
	if replayed.Code != http.StatusOK || !strings.Contains(replayed.Body.String(), `"replayed":true`) ||
		len(repository.audits) != 1 {
		t.Fatalf("replay status=%d body=%s audits=%d", replayed.Code, replayed.Body.String(), len(repository.audits))
	}
	idempotencyConflict := performManagedAccountRequest(
		router, http.MethodPost, "/api/admin/accounts/1/freeze",
		`{"expected_version":1,"reason_code":"security_risk"}`, adminToken, "freeze-key",
	)
	requireManagedAccountAPIError(
		t, idempotencyConflict, http.StatusConflict,
		interfaceshttpapierror.CodeAdminUserAccountIdempotencyConflict,
	)
	stale := performManagedAccountRequest(
		router, http.MethodPost, "/api/admin/accounts/1/unfreeze",
		`{"expected_version":1,"reason_code":"appeal_approved"}`, adminToken, "stale-key",
	)
	requireManagedAccountAPIError(
		t, stale, http.StatusConflict, interfaceshttpapierror.CodeAdminUserAccountVersionConflict,
	)
	unfrozen := performManagedAccountRequest(
		router, http.MethodPost, "/api/admin/accounts/1/unfreeze",
		`{"expected_version":2,"reason_code":"appeal_approved"}`, adminToken, "unfreeze-key",
	)
	if unfrozen.Code != http.StatusOK || !strings.Contains(unfrozen.Body.String(), `"status_name":"normal"`) {
		t.Fatalf("unfreeze status=%d body=%s", unfrozen.Code, unfrozen.Body.String())
	}
	unfreezeNotification, err := domainaccount.NewAccountLifecycleNotification(
		1, domainaccount.AccountOperationUnfreeze,
		domainaccount.AccountReasonAppealApproved, 3, now.Add(time.Second),
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := messageService.CreateAccountLifecycle(
		context.Background(), *unfreezeNotification,
	); err != nil {
		t.Fatal(err)
	}
	newUserToken, err := jwtManager.SignAccessToken(1, domainaccount.RoleUser)
	if err != nil {
		t.Fatal(err)
	}
	retainedMessages := performJSONRequest(
		messageRouter, http.MethodGet, "/api/messages", "", newUserToken,
	)
	requireStatus(t, retainedMessages, http.StatusOK)
	if !strings.Contains(retainedMessages.Body.String(), `"title":"账号已解冻"`) ||
		!strings.Contains(retainedMessages.Body.String(), `"title":"账号已被冻结"`) {
		t.Fatalf("retained messages body=%s", retainedMessages.Body.String())
	}
	missingKey := performManagedAccountRequest(
		router, http.MethodPost, "/api/admin/accounts/1/sessions/revoke",
		`{"expected_version":3,"reason_code":"security_response"}`, adminToken, "",
	)
	requireManagedAccountAPIError(
		t, missingKey, http.StatusBadRequest,
		interfaceshttpapierror.CodeAdminUserAccountValidationFailed,
	)
}

func performManagedAccountRequest(
	router *server.Hertz,
	method, path, body, token, idempotencyKey string,
) *ut.ResponseRecorder {
	headers := []ut.Header{{Key: "Authorization", Value: "Bearer " + token}}
	var requestBody *ut.Body
	if body != "" {
		requestBody = &ut.Body{Body: strings.NewReader(body), Len: len(body)}
		headers = append(headers, ut.Header{Key: "Content-Type", Value: "application/json"})
	}
	if idempotencyKey != "" {
		headers = append(headers, ut.Header{Key: "Idempotency-Key", Value: idempotencyKey})
	}
	return ut.PerformRequest(router.Engine, method, path, requestBody, headers...)
}

func requireManagedAccountAPIError(
	t *testing.T,
	response *ut.ResponseRecorder,
	status int,
	code string,
) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("status=%d want=%d body=%s", response.Code, status, response.Body.String())
	}
	var envelope interfaceshttpapierror.Envelope
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Code != code {
		t.Fatalf("code=%q want=%q body=%s", envelope.Code, code, response.Body.String())
	}
}
