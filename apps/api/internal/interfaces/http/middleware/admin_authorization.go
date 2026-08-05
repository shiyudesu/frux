package interfaceshttpmiddleware

import (
	"context"
	"errors"
	"net/http"

	domainaccount "github.com/shiyudesu/frux/internal/domain/account"
	interfaceshttpapierror "github.com/shiyudesu/frux/internal/interfaces/http/apierror"

	"github.com/cloudwego/hertz/pkg/app"
)

const ContextAdminPrincipalKey = "admin_principal"

func NewRequireAdminPermission(
	reader domainaccount.AdminPrincipalReader,
	required domainaccount.AdminPermission,
) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		userID, ok := authenticatedUserID(c)
		if !ok {
			interfaceshttpapierror.AbortInvalidAccessToken(c)
			return
		}
		if reader == nil {
			interfaceshttpapierror.Abort(
				c,
				http.StatusServiceUnavailable,
				interfaceshttpapierror.CodeAdminAuthorizationUnavailable,
				"admin authorization unavailable",
			)
			return
		}

		principal, err := reader.FindAdminPrincipalByID(ctx, userID)
		if err != nil {
			if errors.Is(err, domainaccount.ErrUserNotFound) {
				abortAdminPermissionDenied(c)
				return
			}
			interfaceshttpapierror.Abort(
				c,
				http.StatusServiceUnavailable,
				interfaceshttpapierror.CodeAdminAuthorizationUnavailable,
				"admin authorization unavailable",
			)
			return
		}
		if principal == nil {
			interfaceshttpapierror.Abort(
				c,
				http.StatusServiceUnavailable,
				interfaceshttpapierror.CodeAdminAuthorizationUnavailable,
				"admin authorization unavailable",
			)
			return
		}
		if !principal.HasPermission(required) {
			abortAdminPermissionDenied(c)
			return
		}

		c.Set(ContextAdminPrincipalKey, principal)
		c.Next(ctx)
	}
}

func AdminPrincipalFromContext(c *app.RequestContext) (*domainaccount.AdminPrincipal, bool) {
	value, exists := c.Get(ContextAdminPrincipalKey)
	if !exists {
		return nil, false
	}
	principal, ok := value.(*domainaccount.AdminPrincipal)
	return principal, ok && principal != nil
}

func authenticatedUserID(c *app.RequestContext) (int64, bool) {
	value, exists := c.Get(ContextUserIDKey)
	if !exists {
		return 0, false
	}
	userID, ok := value.(int64)
	return userID, ok && userID > 0
}

func abortAdminPermissionDenied(c *app.RequestContext) {
	interfaceshttpapierror.Abort(
		c,
		http.StatusForbidden,
		interfaceshttpapierror.CodeAdminPermissionDenied,
		"admin permission denied",
	)
}
