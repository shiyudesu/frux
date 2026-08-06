package interfaceshttpadminauth

import (
	"context"
	"errors"
	"net/http"

	applicationadminauth "github.com/shiyudesu/frux/internal/application/adminauth"
	interfaceshttpapierror "github.com/shiyudesu/frux/internal/interfaces/http/apierror"
	interfaceshttpbinding "github.com/shiyudesu/frux/internal/interfaces/http/binding"

	"github.com/cloudwego/hertz/pkg/app"
)

const maxLoginBodyBytes = 4 << 10

type Handler struct {
	service *applicationadminauth.Service
}

func New(service *applicationadminauth.Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) Login(ctx context.Context, c *app.RequestContext) {
	var request loginRequest
	if err := interfaceshttpbinding.BindStrictJSON(c, &request, maxLoginBodyBytes); err != nil {
		interfaceshttpapierror.WriteInvalidRequest(c)
		return
	}
	result, err := h.service.Login(ctx, request.Account, request.Password)
	if err != nil {
		if errors.Is(err, applicationadminauth.ErrInvalidCredentials) {
			interfaceshttpapierror.Write(
				c, http.StatusUnauthorized,
				interfaceshttpapierror.CodeAdminAuthInvalidCredentials,
				"invalid admin credentials",
			)
			return
		}
		interfaceshttpapierror.WriteServiceUnavailableCode(
			c,
			interfaceshttpapierror.CodeAdminAuthenticationUnavailable,
			"admin authentication unavailable",
			err,
		)
		return
	}
	permissions := result.Principal.Permissions()
	permissionValues := make([]string, 0, len(permissions))
	for _, permission := range permissions {
		permissionValues = append(permissionValues, string(permission))
	}
	c.JSON(http.StatusOK, loginResponse{
		AccessToken: result.AccessToken, TokenType: result.TokenType,
		ExpiresInSeconds: result.ExpiresInSeconds,
		Principal: principalResponse{
			UserID: result.Principal.UserID, Role: result.Principal.Role,
			Permissions: permissionValues,
		},
	})
}
