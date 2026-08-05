package interfaceshttpadmin

import (
	"context"
	"net/http"

	domainaccount "github.com/shiyudesu/frux/internal/domain/account"
	interfaceshttpapierror "github.com/shiyudesu/frux/internal/interfaces/http/apierror"
	interfaceshttpmiddleware "github.com/shiyudesu/frux/internal/interfaces/http/middleware"

	"github.com/cloudwego/hertz/pkg/app"
)

type Handler struct{}

type principalResponse struct {
	UserID      int64    `json:"user_id"`
	Role        string   `json:"role"`
	Permissions []string `json:"permissions"`
}

func New() *Handler {
	return &Handler{}
}

func (h *Handler) Me(_ context.Context, c *app.RequestContext) {
	principal, ok := interfaceshttpmiddleware.AdminPrincipalFromContext(c)
	if !ok {
		interfaceshttpapierror.WriteServiceUnavailableCode(
			c,
			interfaceshttpapierror.CodeAdminAuthorizationUnavailable,
			"admin authorization unavailable",
			nil,
		)
		return
	}
	c.JSON(http.StatusOK, principalResponseFromDomain(principal))
}

func principalResponseFromDomain(principal *domainaccount.AdminPrincipal) principalResponse {
	permissions := principal.Permissions()
	response := principalResponse{
		UserID:      principal.UserID,
		Role:        principal.Role,
		Permissions: make([]string, 0, len(permissions)),
	}
	for _, permission := range permissions {
		response.Permissions = append(response.Permissions, string(permission))
	}
	return response
}
