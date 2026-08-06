package interfaceshttpmiddleware

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	infrajwt "github.com/shiyudesu/frux/internal/infra/jwt"
	interfaceshttpapierror "github.com/shiyudesu/frux/internal/interfaces/http/apierror"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
)

func TestJWTAuthPropagatesIdentityAndAbortsUnauthorized(t *testing.T) {
	jwtManager, err := infrajwt.NewManager("test-secret", "15m")
	if err != nil {
		t.Fatalf("new jwt manager: %v", err)
	}

	token, err := jwtManager.SignAccessToken(42, "admin")
	if err != nil {
		t.Fatalf("sign access token: %v", err)
	}

	h := server.New(server.WithDisablePrintRoute(true))
	h.GET("/protected", NewJWTAuth(jwtManager), func(_ context.Context, c *app.RequestContext) {
		userID, userExists := c.Get(ContextUserIDKey)
		role, roleExists := c.Get(ContextRoleKey)
		if !userExists || !roleExists || userID != int64(42) || role != "admin" {
			c.Status(http.StatusInternalServerError)
			return
		}
		c.Status(http.StatusNoContent)
	})

	authorized := ut.PerformRequest(
		h.Engine,
		http.MethodGet,
		"/protected",
		nil,
		ut.Header{Key: "Authorization", Value: "Bearer " + token},
	)
	if authorized.Code != http.StatusNoContent {
		t.Fatalf("expected status %d, got %d body=%s", http.StatusNoContent, authorized.Code, authorized.Body.String())
	}

	tests := []struct {
		name    string
		headers []ut.Header
		message string
	}{
		{name: "missing header", message: "authorization header is required"},
		{
			name:    "malformed header",
			headers: []ut.Header{{Key: "Authorization", Value: "bad-format"}},
			message: "authorization format must be ******",
		},
		{
			name:    "wrong scheme",
			headers: []ut.Header{{Key: "Authorization", Value: "Basic credentials"}},
			message: "authorization scheme must be Bearer",
		},
		{
			name:    "invalid token",
			headers: []ut.Header{{Key: "Authorization", Value: "Bearer invalid"}},
			message: "invalid access token",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			unauthorized := ut.PerformRequest(
				h.Engine,
				http.MethodGet,
				"/protected",
				nil,
				tt.headers...,
			)
			if unauthorized.Code != http.StatusUnauthorized {
				t.Fatalf("expected status %d, got %d body=%s", http.StatusUnauthorized, unauthorized.Code, unauthorized.Body.String())
			}
			var body struct {
				Code    string `json:"code"`
				Error   string `json:"error"`
				Message string `json:"message"`
			}
			if err := json.Unmarshal(unauthorized.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode unauthorized body: %v", err)
			}
			if body.Code != interfaceshttpapierror.CodeInvalidAccessToken ||
				body.Error != "invalid access token" ||
				body.Message != tt.message {
				t.Fatalf("unexpected unauthorized body: %+v raw=%s", body, unauthorized.Body.String())
			}
		})
	}
}

func TestAdminJWTAuthRejectsConsumerPurpose(t *testing.T) {
	jwtManager, err := infrajwt.NewManager("test-secret", "15m", "30m")
	if err != nil {
		t.Fatal(err)
	}
	consumer, _ := jwtManager.SignAccessToken(42, "admin")
	admin, _ := jwtManager.SignAdminAccessToken(42, "admin")
	h := server.New(server.WithDisablePrintRoute(true))
	h.GET("/admin", NewAdminJWTAuth(jwtManager), func(_ context.Context, c *app.RequestContext) {
		c.Status(http.StatusNoContent)
	})
	authorized := ut.PerformRequest(
		h.Engine, http.MethodGet, "/admin", nil,
		ut.Header{Key: "Authorization", Value: "Bearer " + admin},
	)
	if authorized.Code != http.StatusNoContent {
		t.Fatalf("admin status=%d body=%s", authorized.Code, authorized.Body.String())
	}
	for _, token := range []string{"", consumer, "invalid"} {
		headers := []ut.Header{}
		if token != "" {
			headers = append(headers, ut.Header{Key: "Authorization", Value: "Bearer " + token})
		}
		response := ut.PerformRequest(h.Engine, http.MethodGet, "/admin", nil, headers...)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("token %q status=%d body=%s", token, response.Code, response.Body.String())
		}
		var body interfaceshttpapierror.Envelope
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if body.Code != interfaceshttpapierror.CodeAdminAuthInvalidAccessToken {
			t.Fatalf("code=%q", body.Code)
		}
	}
}
