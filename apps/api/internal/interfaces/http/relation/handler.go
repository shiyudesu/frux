package interfaceshttprelation

import (
	"context"
	"errors"
	applicationrelation "github.com/shiyudesu/frux/internal/application/relation"
	domainrelation "github.com/shiyudesu/frux/internal/domain/relation"
	interfaceshttpapierror "github.com/shiyudesu/frux/internal/interfaces/http/apierror"
	interfaceshttpmiddleware "github.com/shiyudesu/frux/internal/interfaces/http/middleware"
	"net/http"
	"strconv"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
)

type Handler struct {
	service *applicationrelation.Service
}

// New 创建关系 HTTP Handler。
func New(service *applicationrelation.Service) *Handler {
	return &Handler{service: service}
}

// Follow 处理关注用户接口。
func (h *Handler) Follow(ctx context.Context, c *app.RequestContext) {
	h.setFollow(ctx, c, true)
}

// Unfollow 处理取消关注用户接口。
func (h *Handler) Unfollow(ctx context.Context, c *app.RequestContext) {
	h.setFollow(ctx, c, false)
}

// GetFollowState reads the authenticated user's relationship to one target.
func (h *Handler) GetFollowState(ctx context.Context, c *app.RequestContext) {
	userID, ok := userIDFromContext(c)
	if !ok {
		interfaceshttpapierror.WriteInvalidAccessToken(c)
		return
	}
	targetUserID, err := parsePositiveInt64(c.Param("targetUserId"), domainrelation.ErrInvalidTargetUserID)
	if err != nil {
		writeRelationError(c, err)
		return
	}
	result, err := h.service.GetFollowState(ctx, userID, targetUserID)
	if err != nil {
		writeRelationError(c, err)
		return
	}
	c.JSON(http.StatusOK, followStateResponse{
		UserID:       result.UserID,
		TargetUserID: result.TargetUserID,
		Following:    result.Following,
	})
}

// ListFollowing 查询当前用户关注列表。
func (h *Handler) ListFollowing(ctx context.Context, c *app.RequestContext) {
	userID, ok := userIDFromContext(c)
	if !ok {
		interfaceshttpapierror.WriteInvalidAccessToken(c)
		return
	}

	limit, err := parseLimit(c.Query("limit"))
	if err != nil {
		writeRelationError(c, err)
		return
	}

	result, err := h.service.ListFollowing(ctx, userID, c.Query("cursor"), limit)
	if err != nil {
		writeRelationError(c, err)
		return
	}
	c.JSON(http.StatusOK, relationListResponseFromResult(result))
}

// ListFollowers 查询当前用户粉丝列表。
func (h *Handler) ListFollowers(ctx context.Context, c *app.RequestContext) {
	userID, ok := userIDFromContext(c)
	if !ok {
		interfaceshttpapierror.WriteInvalidAccessToken(c)
		return
	}

	limit, err := parseLimit(c.Query("limit"))
	if err != nil {
		writeRelationError(c, err)
		return
	}

	result, err := h.service.ListFollowers(ctx, userID, c.Query("cursor"), limit)
	if err != nil {
		writeRelationError(c, err)
		return
	}
	c.JSON(http.StatusOK, relationListResponseFromResult(result))
}

func (h *Handler) setFollow(ctx context.Context, c *app.RequestContext, active bool) {
	userID, ok := userIDFromContext(c)
	if !ok {
		interfaceshttpapierror.WriteInvalidAccessToken(c)
		return
	}

	targetUserID, err := parsePositiveInt64(c.Param("targetUserId"), domainrelation.ErrInvalidTargetUserID)
	if err != nil {
		writeRelationError(c, err)
		return
	}

	var result *applicationrelation.FollowResult
	recommendationRequestID := string(c.GetHeader("X-Recommendation-Request-ID"))
	recommendationVideoID, err := parseOptionalRecommendationVideoID(string(c.GetHeader("X-Recommendation-Video-ID")))
	if err != nil {
		writeRelationError(c, err)
		return
	}
	if active {
		result, err = h.service.FollowWithRecommendation(ctx, userID, targetUserID, string(c.GetHeader("Idempotency-Key")), recommendationRequestID, recommendationVideoID)
	} else {
		result, err = h.service.UnfollowWithRecommendation(ctx, userID, targetUserID, string(c.GetHeader("Idempotency-Key")), recommendationRequestID, recommendationVideoID)
	}
	if err != nil {
		writeRelationError(c, err)
		return
	}
	c.JSON(http.StatusOK, followResponseFromResult(result))
}

func userIDFromContext(c *app.RequestContext) (int64, bool) {
	value, exists := c.Get(interfaceshttpmiddleware.ContextUserIDKey)
	if !exists {
		return 0, false
	}
	userID, ok := value.(int64)
	return userID, ok && userID > 0
}

func parsePositiveInt64(raw string, fallback error) (int64, error) {
	value, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || value <= 0 {
		return 0, fallback
	}
	return value, nil
}

func parseLimit(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 {
		return 0, domainrelation.ErrInvalidLimit
	}
	return limit, nil
}

func parseOptionalRecommendationVideoID(raw string) (int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	videoID, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || videoID <= 0 {
		return 0, domainrelation.ErrInvalidRecommendationVideoID
	}
	return videoID, nil
}

func followResponseFromResult(result *applicationrelation.FollowResult) followResponse {
	return followResponse{
		UserID:         result.UserID,
		TargetUserID:   result.TargetUserID,
		Status:         result.Status,
		Following:      result.Following,
		FollowingCount: result.FollowingCount,
		FollowerCount:  result.FollowerCount,
	}
}

func relationListResponseFromResult(result *applicationrelation.ListResult) relationListResponse {
	items := make([]relationUserResponse, 0, len(result.Items))
	for _, item := range result.Items {
		items = append(items, relationUserResponse{
			UserID:     item.UserID,
			Nickname:   item.Nickname,
			AvatarURL:  item.AvatarURL,
			Bio:        item.Bio,
			FollowedAt: item.FollowedAt,
		})
	}
	return relationListResponse{
		Items:      items,
		NextCursor: result.NextCursor,
		HasMore:    result.HasMore,
	}
}

func writeRelationError(c *app.RequestContext, err error) {
	if isBadRequestError(err) {
		interfaceshttpapierror.Write(c, http.StatusBadRequest, interfaceshttpapierror.CodeRelationValidationFailed, err.Error())
		return
	}
	if errors.Is(err, domainrelation.ErrTargetUserNotFound) {
		interfaceshttpapierror.Write(c, http.StatusNotFound, interfaceshttpapierror.CodeRelationTargetUserNotFound, "target user not found")
		return
	}
	if errors.Is(err, domainrelation.ErrFollowIdempotencyConflict) {
		interfaceshttpapierror.Write(c, http.StatusConflict, interfaceshttpapierror.CodeRelationIdempotencyConflict, "idempotency key conflict")
		return
	}
	interfaceshttpapierror.WriteInternal(c, "internal server error", err)
}

func isBadRequestError(err error) bool {
	return errors.Is(err, domainrelation.ErrInvalidUserID) ||
		errors.Is(err, domainrelation.ErrInvalidTargetUserID) ||
		errors.Is(err, domainrelation.ErrFollowSelfForbidden) ||
		errors.Is(err, domainrelation.ErrInvalidLimit) ||
		errors.Is(err, domainrelation.ErrInvalidCursor) ||
		errors.Is(err, domainrelation.ErrIdempotencyKeyTooLong) ||
		errors.Is(err, domainrelation.ErrRecommendationRequestIDTooLong) ||
		errors.Is(err, domainrelation.ErrInvalidRecommendationVideoID)
}
