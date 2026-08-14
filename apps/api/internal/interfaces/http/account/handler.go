package interfaceshttpaccount

import (
	"context"
	"errors"
	applicationaccount "github.com/shiyudesu/frux/internal/application/account"
	domainaccount "github.com/shiyudesu/frux/internal/domain/account"
	interfaceshttpapierror "github.com/shiyudesu/frux/internal/interfaces/http/apierror"
	interfaceshttpbinding "github.com/shiyudesu/frux/internal/interfaces/http/binding"
	interfaceshttpmiddleware "github.com/shiyudesu/frux/internal/interfaces/http/middleware"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
)

type Handler struct {
	service *applicationaccount.Service
}

// New 注入账号应用服务，Handler 只处理 HTTP 输入输出。
func New(service *applicationaccount.Service) *Handler {
	return &Handler{
		service: service,
	}
}

// Register 处理用户注册请求，成功后返回新用户资料。
func (h *Handler) Register(ctx context.Context, c *app.RequestContext) {
	var req RegisterRequest
	if err := interfaceshttpbinding.BindJSON(c, &req); err != nil {
		interfaceshttpapierror.WriteInvalidRequest(c)
		return
	}

	// 具体注册规则在应用层和领域层执行，HTTP 层只传递请求字段。
	profile, err := h.service.Register(ctx, req.Account, req.Password, req.Nickname)
	if err != nil {
		if isPasswordPolicyError(err) {
			interfaceshttpapierror.Write(
				c, http.StatusBadRequest,
				interfaceshttpapierror.CodeAccountPasswordInvalid,
				err.Error(),
			)
			return
		}
		if isBadRequestError(err) {
			interfaceshttpapierror.Write(c, http.StatusBadRequest, interfaceshttpapierror.CodeAccountValidationFailed, err.Error())
			return
		}
		if errors.Is(err, domainaccount.ErrAccountAlreadyExists) {
			interfaceshttpapierror.Write(c, http.StatusConflict, interfaceshttpapierror.CodeAccountAlreadyExists, "account already exists")
			return
		}
		interfaceshttpapierror.WriteInternal(c, "internal server error", err)
		return
	}

	c.JSON(http.StatusCreated, profileResponse(profile))
}

// Login 处理账号密码登录，成功后返回 Bearer token。
func (h *Handler) Login(ctx context.Context, c *app.RequestContext) {
	if !validSessionRequestOrigin(c) || !validJSONRequest(c) {
		interfaceshttpapierror.WriteInvalidRequest(c)
		return
	}
	var req LoginByPasswordRequest
	if err := interfaceshttpbinding.BindJSON(c, &req); err != nil {
		interfaceshttpapierror.WriteInvalidRequest(c)
		return
	}

	// 登录失败统一映射为 401，避免暴露账号是否存在。
	token, err := h.service.Login(ctx, req.Account, req.Password)
	if err != nil {
		if isBadRequestError(err) {
			interfaceshttpapierror.Write(c, http.StatusBadRequest, interfaceshttpapierror.CodeAccountValidationFailed, err.Error())
			return
		}
		if errors.Is(err, domainaccount.ErrInvalidCredentials) {
			interfaceshttpapierror.Write(c, http.StatusUnauthorized, interfaceshttpapierror.CodeAuthInvalidCredentials, "invalid credentials")
			return
		}
		interfaceshttpapierror.WriteInternal(c, "internal server error", err)
		return
	}

	interfaceshttpmiddleware.SetAssetTokenCookie(c, token.AccessToken, time.Now().Add(time.Duration(token.ExpiresInSeconds)*time.Second))
	interfaceshttpmiddleware.SetRefreshTokenCookie(c, token.RefreshCredential, token.RefreshExpiresAt)
	c.JSON(http.StatusOK, tokenResponse{
		AccessToken:      token.AccessToken,
		TokenType:        token.TokenType,
		ExpiresInSeconds: token.ExpiresInSeconds,
	})
}

func (h *Handler) Refresh(ctx context.Context, c *app.RequestContext) {
	if !validSessionRequestOrigin(c) {
		interfaceshttpapierror.WriteInvalidRequest(c)
		return
	}
	token, err := h.service.Refresh(ctx, refreshCredentialFromCookie(c))
	if err != nil {
		writeSessionError(c, err)
		return
	}
	interfaceshttpmiddleware.SetAssetTokenCookie(
		c, token.AccessToken,
		time.Now().Add(time.Duration(token.ExpiresInSeconds)*time.Second),
	)
	interfaceshttpmiddleware.SetRefreshTokenCookie(
		c, token.RefreshCredential, token.RefreshExpiresAt,
	)
	c.JSON(http.StatusOK, tokenResponse{
		AccessToken:      token.AccessToken,
		TokenType:        token.TokenType,
		ExpiresInSeconds: token.ExpiresInSeconds,
	})
}

func (h *Handler) Logout(ctx context.Context, c *app.RequestContext) {
	if !validSessionRequestOrigin(c) {
		interfaceshttpapierror.WriteInvalidRequest(c)
		return
	}
	if err := h.service.Logout(ctx, refreshCredentialFromCookie(c)); err != nil {
		interfaceshttpapierror.Write(
			c, http.StatusServiceUnavailable,
			interfaceshttpapierror.CodeAuthenticationUnavailable,
			"authentication unavailable",
		)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) ChangePassword(ctx context.Context, c *app.RequestContext) {
	userID, ok := userIDFromContext(c)
	if !ok {
		interfaceshttpapierror.WriteInvalidAccessToken(c)
		return
	}
	var req ChangePasswordRequest
	if err := interfaceshttpbinding.BindJSON(c, &req); err != nil {
		interfaceshttpapierror.WriteInvalidRequest(c)
		return
	}
	token, err := h.service.ChangePassword(
		ctx, userID, req.CurrentPassword, req.NewPassword,
	)
	if err != nil {
		writePasswordChangeError(c, err)
		return
	}
	interfaceshttpmiddleware.SetAssetTokenCookie(
		c, token.AccessToken,
		time.Now().Add(time.Duration(token.ExpiresInSeconds)*time.Second),
	)
	interfaceshttpmiddleware.SetRefreshTokenCookie(
		c, token.RefreshCredential, token.RefreshExpiresAt,
	)
	c.JSON(http.StatusOK, tokenResponse{
		AccessToken:      token.AccessToken,
		TokenType:        token.TokenType,
		ExpiresInSeconds: token.ExpiresInSeconds,
	})
}

// Me 读取当前登录用户资料，用户 ID 来自 JWT 中间件写入的上下文。
func (h *Handler) Me(ctx context.Context, c *app.RequestContext) {
	userID, ok := userIDFromContext(c)
	if !ok {
		interfaceshttpapierror.WriteInvalidAccessToken(c)
		return
	}

	profile, err := h.service.GetProfile(ctx, userID)
	if err != nil {
		writeProfileError(c, err)
		return
	}

	c.JSON(http.StatusOK, profileResponse(profile))
}

// Get 读取公开用户资料，用于访问他人主页。
func (h *Handler) Get(ctx context.Context, c *app.RequestContext) {
	userID, err := parsePositiveUserID(c.Param("userId"))
	if err != nil {
		writeProfileError(c, err)
		return
	}

	profile, err := h.service.GetPublicProfile(ctx, userID)
	if err != nil {
		writeProfileError(c, err)
		return
	}

	c.JSON(http.StatusOK, publicProfileResponse(profile))
}

// UpdateMe 更新当前登录用户资料，请求体支持部分字段更新。
func (h *Handler) UpdateMe(ctx context.Context, c *app.RequestContext) {
	userID, ok := userIDFromContext(c)
	if !ok {
		interfaceshttpapierror.WriteInvalidAccessToken(c)
		return
	}

	var req UpdateProfileRequest
	if err := interfaceshttpbinding.BindJSON(c, &req); err != nil {
		interfaceshttpapierror.WriteInvalidRequest(c)
		return
	}

	var likedVisibility, favoriteVisibility *string
	if req.ProfileSetting != nil {
		likedVisibility = req.ProfileSetting.LikedVisibility
		favoriteVisibility = req.ProfileSetting.FavoriteVisibility
	}
	profile, err := h.service.UpdateProfileAndSetting(
		ctx,
		userID,
		req.Nickname,
		req.AvatarURL,
		req.Bio,
		req.Gender,
		likedVisibility,
		favoriteVisibility,
	)
	if err != nil {
		writeProfileError(c, err)
		return
	}
	c.JSON(http.StatusOK, profileResponse(profile))
}

func (h *Handler) GetProfileSettings(ctx context.Context, c *app.RequestContext) {
	userID, ok := userIDFromContext(c)
	if !ok {
		interfaceshttpapierror.WriteInvalidAccessToken(c)
		return
	}
	setting, err := h.service.GetProfileSetting(ctx, userID)
	if err != nil {
		writeProfileError(c, err)
		return
	}
	c.JSON(http.StatusOK, profileSettingResponseFromApplication(setting))
}

func (h *Handler) UpdateProfileSettings(ctx context.Context, c *app.RequestContext) {
	userID, ok := userIDFromContext(c)
	if !ok {
		interfaceshttpapierror.WriteInvalidAccessToken(c)
		return
	}
	var req UpdateProfileSettingRequest
	if err := interfaceshttpbinding.BindJSON(c, &req); err != nil {
		interfaceshttpapierror.WriteInvalidRequest(c)
		return
	}
	setting, err := h.service.UpdateProfileSetting(ctx, userID, req.LikedVisibility, req.FavoriteVisibility)
	if err != nil {
		writeProfileError(c, err)
		return
	}
	c.JSON(http.StatusOK, profileSettingResponseFromApplication(setting))
}

// publicProfileResponse 将应用层 Profile 转成公开 JSON 结构。
func publicProfileResponse(profile *applicationaccount.Profile) publicUserProfileResponse {
	return publicUserProfileResponse{
		ID:                profile.ID,
		Nickname:          profile.Nickname,
		AvatarURL:         profile.AvatarURL,
		Bio:               profile.Bio,
		FollowingCount:    profile.FollowingCount,
		FollowerCount:     profile.FollowerCount,
		WorkCount:         profile.WorkCount,
		Account:           profile.Account,
		Gender:            profile.Gender,
		PublicWorkCount:   profile.PublicWorkCount,
		ReceivedLikeCount: profile.ReceivedLikeCount,
		LikedVideosPublic: profile.ProfileSettings != nil && profile.ProfileSettings.LikedVisibility == domainaccount.ProfileVisibilityPublic,
	}
}

// profileResponse 将应用层 Profile 转成对外 JSON 结构。
func profileResponse(profile *applicationaccount.Profile) userProfileResponse {
	return userProfileResponse{
		ID:                profile.ID,
		Account:           profile.Account,
		Nickname:          profile.Nickname,
		AvatarURL:         profile.AvatarURL,
		Bio:               profile.Bio,
		Status:            profile.Status,
		Role:              profile.Role,
		FollowingCount:    profile.FollowingCount,
		FollowerCount:     profile.FollowerCount,
		WorkCount:         profile.WorkCount,
		Gender:            profile.Gender,
		PublicWorkCount:   profile.PublicWorkCount,
		PrivateWorkCount:  profile.PrivateWorkCount,
		ReceivedLikeCount: profile.ReceivedLikeCount,
		ProfileSettings:   profileSettingResponseFromApplication(profile.ProfileSettings),
	}
}

func profileSettingResponseFromApplication(setting *applicationaccount.ProfileSetting) *profileSettingResponse {
	if setting == nil {
		return nil
	}
	return &profileSettingResponse{LikedVisibility: setting.LikedVisibility, FavoriteVisibility: setting.FavoriteVisibility}
}

// userIDFromContext 从 JWT 中间件写入的上下文中读取登录用户 ID。
func userIDFromContext(c *app.RequestContext) (int64, bool) {
	value, exists := c.Get(interfaceshttpmiddleware.ContextUserIDKey)
	if !exists {
		return 0, false
	}
	userID, ok := value.(int64)
	return userID, ok && userID > 0
}

func parsePositiveUserID(raw string) (int64, error) {
	value, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || value <= 0 {
		return 0, domainaccount.ErrInvalidUserID
	}
	return value, nil
}

// writeProfileError 统一账号资料相关接口的错误响应。
func writeProfileError(c *app.RequestContext, err error) {
	if isBadRequestError(err) {
		interfaceshttpapierror.Write(c, http.StatusBadRequest, interfaceshttpapierror.CodeAccountValidationFailed, err.Error())
		return
	}
	if errors.Is(err, domainaccount.ErrUserNotFound) {
		interfaceshttpapierror.Write(c, http.StatusNotFound, interfaceshttpapierror.CodeAccountNotFound, "user not found")
		return
	}
	interfaceshttpapierror.WriteInternal(c, "internal server error", err)
}

// isBadRequestError 判断哪些领域错误属于客户端请求参数问题。
func isBadRequestError(err error) bool {
	return errors.Is(err, domainaccount.ErrEmptyAccount) ||
		errors.Is(err, domainaccount.ErrEmptyPassword) ||
		errors.Is(err, domainaccount.ErrPasswordTooShort) ||
		errors.Is(err, domainaccount.ErrPasswordTooLong) ||
		errors.Is(err, domainaccount.ErrPasswordInvalidEncoding) ||
		errors.Is(err, domainaccount.ErrEmptyNickname) ||
		errors.Is(err, domainaccount.ErrInvalidUserID) ||
		errors.Is(err, domainaccount.ErrEmptyProfileUpdate) ||
		errors.Is(err, domainaccount.ErrInvalidGender) ||
		errors.Is(err, domainaccount.ErrEmptyProfileSettingUpdate) ||
		errors.Is(err, domainaccount.ErrInvalidProfileVisibility)
}

func isPasswordPolicyError(err error) bool {
	return errors.Is(err, domainaccount.ErrEmptyPassword) ||
		errors.Is(err, domainaccount.ErrPasswordTooShort) ||
		errors.Is(err, domainaccount.ErrPasswordTooLong) ||
		errors.Is(err, domainaccount.ErrPasswordInvalidEncoding)
}

func writePasswordChangeError(c *app.RequestContext, err error) {
	switch {
	case errors.Is(err, domainaccount.ErrInvalidCredentials):
		interfaceshttpapierror.Write(
			c, http.StatusBadRequest,
			interfaceshttpapierror.CodeAccountCurrentPasswordIncorrect,
			"current password is incorrect",
		)
	case errors.Is(err, domainaccount.ErrPasswordUnchanged):
		interfaceshttpapierror.Write(
			c, http.StatusBadRequest,
			interfaceshttpapierror.CodeAccountPasswordUnchanged,
			err.Error(),
		)
	case errors.Is(err, domainaccount.ErrEmptyPassword),
		errors.Is(err, domainaccount.ErrPasswordTooShort),
		errors.Is(err, domainaccount.ErrPasswordTooLong),
		errors.Is(err, domainaccount.ErrPasswordInvalidEncoding):
		interfaceshttpapierror.Write(
			c, http.StatusBadRequest,
			interfaceshttpapierror.CodeAccountPasswordInvalid,
			err.Error(),
		)
	case errors.Is(err, domainaccount.ErrCredentialChanged):
		interfaceshttpapierror.Write(
			c, http.StatusConflict,
			interfaceshttpapierror.CodeAccountCredentialChanged,
			err.Error(),
		)
	case errors.Is(err, domainaccount.ErrUserNotFound):
		interfaceshttpapierror.Write(
			c, http.StatusNotFound,
			interfaceshttpapierror.CodeAccountNotFound,
			"user not found",
		)
	default:
		interfaceshttpapierror.Write(
			c, http.StatusServiceUnavailable,
			interfaceshttpapierror.CodeAuthenticationUnavailable,
			"authentication unavailable",
		)
	}
}

func writeSessionError(c *app.RequestContext, err error) {
	switch {
	case errors.Is(err, domainaccount.ErrRefreshSessionSuperseded):
		interfaceshttpapierror.Write(
			c, http.StatusConflict,
			interfaceshttpapierror.CodeAuthRefreshSuperseded,
			"refresh session superseded",
		)
	case errors.Is(err, domainaccount.ErrRefreshSessionReplayed):
		interfaceshttpmiddleware.ClearRefreshTokenCookie(c)
		interfaceshttpapierror.Write(
			c, http.StatusUnauthorized,
			interfaceshttpapierror.CodeAuthRefreshReplayed,
			"refresh session replayed",
		)
	case errors.Is(err, domainaccount.ErrInvalidRefreshSession),
		errors.Is(err, domainaccount.ErrRefreshSessionExpired),
		errors.Is(err, domainaccount.ErrRefreshSessionRevoked):
		interfaceshttpmiddleware.ClearRefreshTokenCookie(c)
		interfaceshttpapierror.Write(
			c, http.StatusUnauthorized,
			interfaceshttpapierror.CodeAuthRefreshInvalid,
			"refresh session invalid",
		)
	default:
		interfaceshttpapierror.Write(
			c, http.StatusServiceUnavailable,
			interfaceshttpapierror.CodeAuthenticationUnavailable,
			"authentication unavailable",
		)
	}
}
