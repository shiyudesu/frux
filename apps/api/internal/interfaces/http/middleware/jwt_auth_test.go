package interfaceshttpmiddleware

import (
	"context"
	"net/http"
	"testing"

	infrajwt "GCFeed/internal/infra/jwt"

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

	unauthorized := ut.PerformRequest(h.Engine, http.MethodGet, "/protected", nil)
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d body=%s", http.StatusUnauthorized, unauthorized.Code, unauthorized.Body.String())
	}
}
