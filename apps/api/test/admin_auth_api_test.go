package test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	applicationadminauth "github.com/shiyudesu/frux/internal/application/adminauth"
	domainaccount "github.com/shiyudesu/frux/internal/domain/account"
	infrajwt "github.com/shiyudesu/frux/internal/infra/jwt"
	interfaceshttpadmin "github.com/shiyudesu/frux/internal/interfaces/http/admin"
	interfaceshttpadminauth "github.com/shiyudesu/frux/internal/interfaces/http/adminauth"
	interfaceshttpapierror "github.com/shiyudesu/frux/internal/interfaces/http/apierror"
	interfaceshttpmiddleware "github.com/shiyudesu/frux/internal/interfaces/http/middleware"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
)

type adminAuthAPIRepository struct {
	users map[string]*domainaccount.User
}

func (r adminAuthAPIRepository) FindByAccount(_ context.Context, account string) (*domainaccount.User, error) {
	user := r.users[domainaccount.NormalizeAccount(account)]
	if user == nil {
		return nil, domainaccount.ErrUserNotFound
	}
	return user, nil
}

func (r adminAuthAPIRepository) FindAdminPrincipalByID(_ context.Context, userID int64) (*domainaccount.AdminPrincipal, error) {
	for _, user := range r.users {
		if user.ID == userID {
			return domainaccount.RestoreAdminPrincipal(user.ID, user.Status, user.Role), nil
		}
	}
	return nil, domainaccount.ErrUserNotFound
}

func TestDedicatedAdminAuthenticationAPIFlow(t *testing.T) {
	reviewer := newAdminAuthAPIUser(t, 1, "reviewer-login", domainaccount.StatusNormal, domainaccount.RoleReviewer)
	user := newAdminAuthAPIUser(t, 2, "user-login", domainaccount.StatusNormal, domainaccount.RoleUser)
	repository := adminAuthAPIRepository{users: map[string]*domainaccount.User{
		reviewer.Account: reviewer,
		user.Account:     user,
	}}
	jwtManager, err := infrajwt.NewManager("admin-auth-test-secret", "15m", "30m")
	if err != nil {
		t.Fatal(err)
	}
	authHandler := interfaceshttpadminauth.New(applicationadminauth.New(repository, jwtManager))
	adminHandler := interfaceshttpadmin.New()
	router := server.New(server.WithDisablePrintRoute(true))
	router.POST("/api/admin/auth/login", authHandler.Login)
	adminCalls := 0
	router.GET(
		"/api/admin/me",
		interfaceshttpmiddleware.NewAdminJWTAuth(jwtManager),
		interfaceshttpmiddleware.NewRequireAdminPermission(repository, domainaccount.PermissionReviewRead),
		func(ctx context.Context, c *app.RequestContext) {
			adminCalls++
			adminHandler.Me(ctx, c)
		},
	)
	router.GET(
		"/api/users/me",
		interfaceshttpmiddleware.NewJWTAuth(jwtManager),
		func(_ context.Context, c *app.RequestContext) { c.Status(http.StatusNoContent) },
	)

	login := performAdminAuthJSON(
		router, `{"account":"reviewer-login","password":"Password123!"}`,
	)
	if login.Code != http.StatusOK {
		t.Fatalf("login status=%d body=%s", login.Code, login.Body.String())
	}
	var result struct {
		AccessToken string `json:"access_token"`
		Principal   struct {
			UserID int64 `json:"user_id"`
		} `json:"principal"`
	}
	if err := json.Unmarshal(login.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.AccessToken == "" || result.Principal.UserID != reviewer.ID {
		t.Fatalf("login response = %#v", result)
	}
	adminMe := performAdminAuthRequest(router, "/api/admin/me", result.AccessToken)
	if adminMe.Code != http.StatusOK || adminCalls != 1 {
		t.Fatalf("admin me status=%d calls=%d body=%s", adminMe.Code, adminCalls, adminMe.Body.String())
	}

	consumerToken, err := jwtManager.SignAccessToken(reviewer.ID, domainaccount.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	consumerOnAdmin := performAdminAuthRequest(router, "/api/admin/me", consumerToken)
	requireAdminAuthError(
		t, consumerOnAdmin, http.StatusUnauthorized,
		interfaceshttpapierror.CodeAdminAuthInvalidAccessToken,
	)
	if adminCalls != 1 {
		t.Fatalf("consumer token executed handler: %d", adminCalls)
	}
	adminOnConsumer := performAdminAuthRequest(router, "/api/users/me", result.AccessToken)
	if adminOnConsumer.Code != http.StatusUnauthorized {
		t.Fatalf("admin token consumer status=%d", adminOnConsumer.Code)
	}

	for _, body := range []string{
		`{"account":"reviewer-login","password":"wrong"}`,
		`{"account":"user-login","password":"Password123!"}`,
		`{"account":"missing","password":"Password123!"}`,
	} {
		response := performAdminAuthJSON(router, body)
		requireAdminAuthError(
			t, response, http.StatusUnauthorized,
			interfaceshttpapierror.CodeAdminAuthInvalidCredentials,
		)
	}
	strict := performAdminAuthJSON(
		router,
		`{"account":"reviewer-login","password":"Password123!","register":true}`,
	)
	if strict.Code != http.StatusBadRequest {
		t.Fatalf("strict status=%d body=%s", strict.Code, strict.Body.String())
	}
}

func newAdminAuthAPIUser(
	t *testing.T,
	id int64,
	account string,
	status int,
	role string,
) *domainaccount.User {
	t.Helper()
	user, err := domainaccount.New(account, "Password123!", account)
	if err != nil {
		t.Fatal(err)
	}
	user.ID = id
	user.Status = status
	user.Role = role
	return user
}

func performAdminAuthJSON(router *server.Hertz, body string) *ut.ResponseRecorder {
	return ut.PerformRequest(
		router.Engine, http.MethodPost, "/api/admin/auth/login",
		&ut.Body{Body: strings.NewReader(body), Len: len(body)},
		ut.Header{Key: "Content-Type", Value: "application/json"},
	)
}

func performAdminAuthRequest(router *server.Hertz, path, token string) *ut.ResponseRecorder {
	headers := []ut.Header{}
	if token != "" {
		headers = append(headers, ut.Header{Key: "Authorization", Value: "Bearer " + token})
	}
	return ut.PerformRequest(router.Engine, http.MethodGet, path, nil, headers...)
}

func requireAdminAuthError(
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
		t.Fatalf("code=%q want=%q", envelope.Code, code)
	}
}
