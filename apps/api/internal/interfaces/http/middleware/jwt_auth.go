package interfaceshttpmiddleware

import (
	"context"
	"crypto/subtle"
	infrajwt "github.com/shiyudesu/frux/internal/infra/jwt"
	interfaceshttpapierror "github.com/shiyudesu/frux/internal/interfaces/http/apierror"
	"net/http"
	"strings"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol"
)

const ContextUserIDKey = "auth_user_id"
const ContextRoleKey = "auth_role"
const ContextSessionIDKey = "auth_session_id"
const ContextAuthVersionKey = "auth_version"
const ContextAccessExpiresAtKey = "auth_access_expires_at"
const AssetTokenCookieName = "frux_asset_token"
const AssetActiveCookieName = "frux_asset_active"
const RefreshTokenCookieName = "frux_refresh_token"

// NewJWTAuth validates access tokens and stores identity in the request context.
func NewJWTAuth(jwtManager *infrajwt.Manager) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		header := strings.TrimSpace(string(c.GetHeader("Authorization")))
		if header == "" {
			interfaceshttpapierror.AbortInvalidAccessTokenWithMessage(c, "authorization header is required")
			return
		}

		parts := strings.SplitN(header, " ", 2)
		if len(parts) != 2 {
			interfaceshttpapierror.AbortInvalidAccessTokenWithMessage(c, "authorization format must be ******")
			return
		}
		if !strings.EqualFold(strings.TrimSpace(parts[0]), "Bearer") {
			interfaceshttpapierror.AbortInvalidAccessTokenWithMessage(c, "authorization scheme must be Bearer")
			return
		}

		token := strings.TrimSpace(parts[1])
		claims, err := jwtManager.ParseAndValidateConsumerToken(token)
		if err != nil {
			interfaceshttpapierror.AbortInvalidAccessToken(c)
			return
		}

		c.Set(ContextUserIDKey, claims.UserID)
		c.Set(ContextRoleKey, claims.Role)
		c.Set(ContextSessionIDKey, claims.SessionID)
		c.Set(ContextAuthVersionKey, claims.AuthVersion)
		c.Set(ContextAccessExpiresAtKey, claims.ExpiresAt)
		c.Next(ctx)
	}
}

func NewAdminJWTAuth(jwtManager *infrajwt.Manager) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		header := strings.TrimSpace(string(c.GetHeader("Authorization")))
		parts := strings.SplitN(header, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(strings.TrimSpace(parts[0]), "Bearer") {
			interfaceshttpapierror.AbortInvalidAdminAccessToken(c)
			return
		}
		token := strings.TrimSpace(parts[1])
		claims, err := jwtManager.ParseAndValidateAdminToken(token)
		if err != nil {
			interfaceshttpapierror.AbortInvalidAdminAccessToken(c)
			return
		}
		c.Set(ContextUserIDKey, claims.UserID)
		c.Set(ContextRoleKey, claims.Role)
		c.Set(ContextAuthVersionKey, claims.AuthVersion)
		c.Set(ContextAccessExpiresAtKey, claims.ExpiresAt)
		c.Next(ctx)
	}
}

// NewInternalTokenAuth validates the token used by internal service calls.
func NewInternalTokenAuth(token string) app.HandlerFunc {
	token = strings.TrimSpace(token)
	return func(ctx context.Context, c *app.RequestContext) {
		if token == "" {
			interfaceshttpapierror.Abort(c, http.StatusUnauthorized, interfaceshttpapierror.CodeInternalTokenRequired, "internal token is required")
			return
		}
		provided := strings.TrimSpace(string(c.GetHeader("X-Internal-Token")))
		if subtle.ConstantTimeCompare([]byte(provided), []byte(token)) != 1 {
			interfaceshttpapierror.Abort(c, http.StatusUnauthorized, interfaceshttpapierror.CodeInvalidInternalToken, "invalid internal token")
			return
		}
		c.Next(ctx)
	}
}

// NewOptionalJWTAuth adds viewer identity to public requests when a token is valid.
func NewOptionalJWTAuth(jwtManager *infrajwt.Manager) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		header := strings.TrimSpace(string(c.GetHeader("Authorization")))
		token := ""
		if header != "" {
			parts := strings.SplitN(header, " ", 2)
			if len(parts) == 2 && strings.EqualFold(strings.TrimSpace(parts[0]), "Bearer") {
				token = strings.TrimSpace(parts[1])
			}
		} else if assetCookieActive(c) {
			token = strings.TrimSpace(string(c.Cookie(AssetTokenCookieName)))
		}
		if token == "" {
			c.Next(ctx)
			return
		}

		claims, err := jwtManager.ParseAndValidateConsumerToken(token)
		if err == nil {
			c.Set(ContextUserIDKey, claims.UserID)
			c.Set(ContextRoleKey, claims.Role)
			c.Set(ContextSessionIDKey, claims.SessionID)
			c.Set(ContextAuthVersionKey, claims.AuthVersion)
			c.Set(ContextAccessExpiresAtKey, claims.ExpiresAt)
		}
		c.Next(ctx)
	}
}

func SetAssetTokenCookie(c *app.RequestContext, token string, expiresAt time.Time) {
	maxAge := int(time.Until(expiresAt).Seconds())
	if maxAge < 1 {
		maxAge = 1
	}
	c.SetCookie(
		AssetTokenCookieName,
		token,
		maxAge,
		"/uploads",
		"",
		protocol.CookieSameSiteStrictMode,
		requestIsHTTPS(c),
		true,
	)
}

func SetRefreshTokenCookie(c *app.RequestContext, credential string, expiresAt time.Time) {
	maxAge := int(time.Until(expiresAt).Seconds())
	if maxAge < 1 {
		maxAge = 1
	}
	c.SetCookie(
		RefreshTokenCookieName,
		credential,
		maxAge,
		"/api/sessions",
		"",
		protocol.CookieSameSiteStrictMode,
		requestIsHTTPS(c),
		true,
	)
}

func ClearRefreshTokenCookie(c *app.RequestContext) {
	c.SetCookie(
		RefreshTokenCookieName,
		"",
		-1,
		"/api/sessions",
		"",
		protocol.CookieSameSiteStrictMode,
		requestIsHTTPS(c),
		true,
	)
}

func requestIsHTTPS(c *app.RequestContext) bool {
	return strings.EqualFold(strings.TrimSpace(string(c.GetHeader("X-Forwarded-Proto"))), "https")
}

func assetCookieActive(c *app.RequestContext) bool {
	return subtle.ConstantTimeCompare(
		[]byte(strings.TrimSpace(string(c.Cookie(AssetActiveCookieName)))),
		[]byte("1"),
	) == 1
}
