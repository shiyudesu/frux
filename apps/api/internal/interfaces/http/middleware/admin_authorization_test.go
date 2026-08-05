package interfaceshttpmiddleware

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	domainaccount "github.com/shiyudesu/frux/internal/domain/account"
	infrajwt "github.com/shiyudesu/frux/internal/infra/jwt"
	interfaceshttpapierror "github.com/shiyudesu/frux/internal/interfaces/http/apierror"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
)

type fakeAdminPrincipalReader struct {
	principals map[int64]*domainaccount.AdminPrincipal
	errors     map[int64]error
}

func (r *fakeAdminPrincipalReader) FindAdminPrincipalByID(_ context.Context, userID int64) (*domainaccount.AdminPrincipal, error) {
	if err := r.errors[userID]; err != nil {
		return nil, err
	}
	principal, ok := r.principals[userID]
	if !ok {
		return nil, domainaccount.ErrUserNotFound
	}
	return principal, nil
}

func TestRequireAdminPermissionUsesCurrentAccountState(t *testing.T) {
	jwtManager, err := infrajwt.NewManager("test-secret", "15m")
	if err != nil {
		t.Fatalf("new jwt manager: %v", err)
	}
	reader := &fakeAdminPrincipalReader{
		principals: map[int64]*domainaccount.AdminPrincipal{
			1: domainaccount.RestoreAdminPrincipal(1, domainaccount.StatusNormal, domainaccount.RoleReviewer),
			2: domainaccount.RestoreAdminPrincipal(2, domainaccount.StatusNormal, domainaccount.RoleUser),
			3: domainaccount.RestoreAdminPrincipal(3, 2, domainaccount.RoleAdmin),
			4: domainaccount.RestoreAdminPrincipal(4, domainaccount.StatusNormal, "super-admin"),
			5: domainaccount.RestoreAdminPrincipal(5, domainaccount.StatusNormal, domainaccount.RoleAdmin),
		},
		errors: map[int64]error{},
	}

	h := server.New(server.WithDisablePrintRoute(true))
	handlerCalls := make(map[int64]int)
	h.GET(
		"/admin",
		NewJWTAuth(jwtManager),
		NewRequireAdminPermission(reader, domainaccount.PermissionReviewRead),
		func(_ context.Context, c *app.RequestContext) {
			principal, ok := AdminPrincipalFromContext(c)
			if !ok {
				c.Status(http.StatusInternalServerError)
				return
			}
			handlerCalls[principal.UserID]++
			c.Status(http.StatusNoContent)
		},
	)

	tests := []struct {
		name        string
		userID      int64
		claimedRole string
		status      int
		code        string
	}{
		{name: "current reviewer overrides stale user claim", userID: 1, claimedRole: domainaccount.RoleUser, status: http.StatusNoContent},
		{name: "demoted account rejects stale admin claim", userID: 2, claimedRole: domainaccount.RoleAdmin, status: http.StatusForbidden, code: interfaceshttpapierror.CodeAdminPermissionDenied},
		{name: "disabled account rejects stale admin claim", userID: 3, claimedRole: domainaccount.RoleAdmin, status: http.StatusForbidden, code: interfaceshttpapierror.CodeAdminPermissionDenied},
		{name: "unknown current role is denied", userID: 4, claimedRole: domainaccount.RoleAdmin, status: http.StatusForbidden, code: interfaceshttpapierror.CodeAdminPermissionDenied},
		{name: "compatible admin ignores stale user claim", userID: 5, claimedRole: domainaccount.RoleUser, status: http.StatusNoContent},
		{name: "missing account is denied", userID: 6, claimedRole: domainaccount.RoleAdmin, status: http.StatusForbidden, code: interfaceshttpapierror.CodeAdminPermissionDenied},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token, err := jwtManager.SignAccessToken(tt.userID, tt.claimedRole)
			if err != nil {
				t.Fatalf("sign access token: %v", err)
			}
			response := ut.PerformRequest(
				h.Engine,
				http.MethodGet,
				"/admin",
				nil,
				ut.Header{Key: "Authorization", Value: "Bearer " + token},
			)
			if response.Code != tt.status {
				t.Fatalf("status = %d, want %d body=%s", response.Code, tt.status, response.Body.String())
			}
			if tt.code != "" {
				var body interfaceshttpapierror.Envelope
				if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
					t.Fatalf("decode response: %v", err)
				}
				if body.Code != tt.code {
					t.Fatalf("code = %q, want %q", body.Code, tt.code)
				}
			}
		})
	}

	if handlerCalls[1] != 1 || handlerCalls[5] != 1 || len(handlerCalls) != 2 {
		t.Fatalf("unexpected handler calls: %#v", handlerCalls)
	}
}

func TestRequireAdminPermissionPreservesAuthenticationAndReaderErrors(t *testing.T) {
	jwtManager, err := infrajwt.NewManager("test-secret", "15m")
	if err != nil {
		t.Fatalf("new jwt manager: %v", err)
	}
	reader := &fakeAdminPrincipalReader{
		principals: map[int64]*domainaccount.AdminPrincipal{},
		errors:     map[int64]error{7: errors.New("database unavailable")},
	}
	h := server.New(server.WithDisablePrintRoute(true))
	h.GET(
		"/admin",
		NewJWTAuth(jwtManager),
		NewRequireAdminPermission(reader, domainaccount.PermissionReviewRead),
		func(_ context.Context, c *app.RequestContext) {
			c.Status(http.StatusNoContent)
		},
	)

	unauthenticated := ut.PerformRequest(h.Engine, http.MethodGet, "/admin", nil)
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want %d", unauthenticated.Code, http.StatusUnauthorized)
	}
	var unauthenticatedBody interfaceshttpapierror.Envelope
	if err := json.Unmarshal(unauthenticated.Body.Bytes(), &unauthenticatedBody); err != nil {
		t.Fatalf("decode unauthenticated response: %v", err)
	}
	if unauthenticatedBody.Code != interfaceshttpapierror.CodeInvalidAccessToken {
		t.Fatalf("unauthenticated code = %q", unauthenticatedBody.Code)
	}

	token, err := jwtManager.SignAccessToken(7, domainaccount.RoleAdmin)
	if err != nil {
		t.Fatalf("sign access token: %v", err)
	}
	unavailable := ut.PerformRequest(
		h.Engine,
		http.MethodGet,
		"/admin",
		nil,
		ut.Header{Key: "Authorization", Value: "Bearer " + token},
	)
	if unavailable.Code != http.StatusServiceUnavailable {
		t.Fatalf("unavailable status = %d, want %d", unavailable.Code, http.StatusServiceUnavailable)
	}
	var unavailableBody interfaceshttpapierror.Envelope
	if err := json.Unmarshal(unavailable.Body.Bytes(), &unavailableBody); err != nil {
		t.Fatalf("decode unavailable response: %v", err)
	}
	if unavailableBody.Code != interfaceshttpapierror.CodeAdminAuthorizationUnavailable {
		t.Fatalf("unavailable code = %q", unavailableBody.Code)
	}
}
