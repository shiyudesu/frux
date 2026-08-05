package test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	domainaccount "github.com/shiyudesu/frux/internal/domain/account"
	infrajwt "github.com/shiyudesu/frux/internal/infra/jwt"
	interfaceshttpadmin "github.com/shiyudesu/frux/internal/interfaces/http/admin"
	interfaceshttpapierror "github.com/shiyudesu/frux/internal/interfaces/http/apierror"
	interfaceshttpmiddleware "github.com/shiyudesu/frux/internal/interfaces/http/middleware"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
)

type adminAuthorizationReader struct {
	principals map[int64]*domainaccount.AdminPrincipal
}

func (r *adminAuthorizationReader) FindAdminPrincipalByID(_ context.Context, userID int64) (*domainaccount.AdminPrincipal, error) {
	principal, ok := r.principals[userID]
	if !ok {
		return nil, domainaccount.ErrUserNotFound
	}
	return principal, nil
}

func TestAdminAuthorizationAPIFlow(t *testing.T) {
	jwtManager, err := infrajwt.NewManager("test-secret", "15m")
	if err != nil {
		t.Fatalf("new jwt manager: %v", err)
	}
	reader := &adminAuthorizationReader{principals: map[int64]*domainaccount.AdminPrincipal{
		1: domainaccount.RestoreAdminPrincipal(1, domainaccount.StatusNormal, domainaccount.RoleReviewer),
		2: domainaccount.RestoreAdminPrincipal(2, domainaccount.StatusNormal, domainaccount.RoleUser),
		3: domainaccount.RestoreAdminPrincipal(3, 2, domainaccount.RoleReviewer),
		4: domainaccount.RestoreAdminPrincipal(4, domainaccount.StatusNormal, domainaccount.RoleAdmin),
	}}
	handler := interfaceshttpadmin.New()
	router := server.New(server.WithDisablePrintRoute(true))
	api := router.Group("/api")
	admin := api.Group("/admin", interfaceshttpmiddleware.NewJWTAuth(jwtManager))
	admin.GET(
		"/me",
		interfaceshttpmiddleware.NewRequireAdminPermission(reader, domainaccount.PermissionReviewRead),
		handler.Me,
	)

	reviewerToken := signAdminAuthorizationToken(t, jwtManager, 1, domainaccount.RoleUser)
	reviewer := performAdminAuthorizationRequest(router, reviewerToken)
	if reviewer.Code != http.StatusOK {
		t.Fatalf("reviewer status = %d body=%s", reviewer.Code, reviewer.Body.String())
	}
	var reviewerBody struct {
		UserID      int64    `json:"user_id"`
		Role        string   `json:"role"`
		Permissions []string `json:"permissions"`
	}
	if err := json.Unmarshal(reviewer.Body.Bytes(), &reviewerBody); err != nil {
		t.Fatalf("decode reviewer response: %v", err)
	}
	if reviewerBody.UserID != 1 || reviewerBody.Role != domainaccount.RoleReviewer ||
		!containsString(reviewerBody.Permissions, string(domainaccount.PermissionReviewRead)) ||
		!containsString(reviewerBody.Permissions, string(domainaccount.PermissionReviewDecide)) {
		t.Fatalf("unexpected reviewer response: %+v", reviewerBody)
	}

	unauthenticated := performAdminAuthorizationRequest(router, "")
	requireAdminAuthorizationError(t, unauthenticated, http.StatusUnauthorized, interfaceshttpapierror.CodeInvalidAccessToken)

	userToken := signAdminAuthorizationToken(t, jwtManager, 2, domainaccount.RoleAdmin)
	forbidden := performAdminAuthorizationRequest(router, userToken)
	requireAdminAuthorizationError(t, forbidden, http.StatusForbidden, interfaceshttpapierror.CodeAdminPermissionDenied)

	disabledToken := signAdminAuthorizationToken(t, jwtManager, 3, domainaccount.RoleAdmin)
	disabled := performAdminAuthorizationRequest(router, disabledToken)
	requireAdminAuthorizationError(t, disabled, http.StatusForbidden, interfaceshttpapierror.CodeAdminPermissionDenied)

	adminToken := signAdminAuthorizationToken(t, jwtManager, 4, domainaccount.RoleUser)
	compatibleAdmin := performAdminAuthorizationRequest(router, adminToken)
	if compatibleAdmin.Code != http.StatusOK {
		t.Fatalf("compatible admin status = %d body=%s", compatibleAdmin.Code, compatibleAdmin.Body.String())
	}
	var adminBody struct {
		Permissions []string `json:"permissions"`
	}
	if err := json.Unmarshal(compatibleAdmin.Body.Bytes(), &adminBody); err != nil {
		t.Fatalf("decode compatible admin response: %v", err)
	}
	if len(adminBody.Permissions) != len(domainaccount.RegisteredAdminPermissions()) {
		t.Fatalf("compatible admin permissions = %#v", adminBody.Permissions)
	}
}

func signAdminAuthorizationToken(t *testing.T, manager *infrajwt.Manager, userID int64, role string) string {
	t.Helper()
	token, err := manager.SignAccessToken(userID, role)
	if err != nil {
		t.Fatalf("sign access token: %v", err)
	}
	return token
}

func performAdminAuthorizationRequest(router *server.Hertz, token string) *ut.ResponseRecorder {
	headers := make([]ut.Header, 0, 1)
	if token != "" {
		headers = append(headers, ut.Header{Key: "Authorization", Value: "Bearer " + token})
	}
	return ut.PerformRequest(router.Engine, http.MethodGet, "/api/admin/me", nil, headers...)
}

func requireAdminAuthorizationError(t *testing.T, response *ut.ResponseRecorder, status int, code string) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("status = %d, want %d body=%s", response.Code, status, response.Body.String())
	}
	var body interfaceshttpapierror.Envelope
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if body.Code != code {
		t.Fatalf("code = %q, want %q", body.Code, code)
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
