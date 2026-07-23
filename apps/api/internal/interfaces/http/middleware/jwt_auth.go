package interfaceshttpmiddleware

import (
	infrajwt "GCFeed/internal/infra/jwt"
	"context"
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
)

const ContextUserIDKey = "auth_user_id"
const ContextRoleKey = "auth_role"

// NewJWTAuth validates access tokens and stores identity in the request context.
func NewJWTAuth(jwtManager *infrajwt.Manager) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		header := strings.TrimSpace(string(c.GetHeader("Authorization")))
		if header == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, utils.H{
				"message": "authorization header is required",
			})
			return
		}

		parts := strings.SplitN(header, " ", 2)
		if len(parts) != 2 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, utils.H{
				"message": "authorization format must be Bearer token",
			})
			return
		}
		if !strings.EqualFold(strings.TrimSpace(parts[0]), "Bearer") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, utils.H{
				"message": "authorization scheme must be Bearer",
			})
			return
		}

		token := strings.TrimSpace(parts[1])
		claims, err := jwtManager.ParseAndValidateToken(token, infrajwt.TokenTypeAccess)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, utils.H{
				"message": "invalid access token",
			})
			return
		}

		c.Set(ContextUserIDKey, claims.UserID)
		c.Set(ContextRoleKey, claims.Role)
		c.Next(ctx)
	}
}

// NewInternalTokenAuth validates the token used by internal service calls.
func NewInternalTokenAuth(token string) app.HandlerFunc {
	token = strings.TrimSpace(token)
	return func(ctx context.Context, c *app.RequestContext) {
		if token == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, utils.H{
				"message": "internal token is required",
			})
			return
		}
		provided := strings.TrimSpace(string(c.GetHeader("X-Internal-Token")))
		if subtle.ConstantTimeCompare([]byte(provided), []byte(token)) != 1 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, utils.H{
				"message": "invalid internal token",
			})
			return
		}
		c.Next(ctx)
	}
}

// NewOptionalJWTAuth adds viewer identity to public requests when a token is valid.
func NewOptionalJWTAuth(jwtManager *infrajwt.Manager) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		header := strings.TrimSpace(string(c.GetHeader("Authorization")))
		if header == "" {
			c.Next(ctx)
			return
		}

		parts := strings.SplitN(header, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(strings.TrimSpace(parts[0]), "Bearer") {
			c.Next(ctx)
			return
		}

		token := strings.TrimSpace(parts[1])
		claims, err := jwtManager.ParseAndValidateToken(token, infrajwt.TokenTypeAccess)
		if err == nil {
			c.Set(ContextUserIDKey, claims.UserID)
			c.Set(ContextRoleKey, claims.Role)
		}
		c.Next(ctx)
	}
}
