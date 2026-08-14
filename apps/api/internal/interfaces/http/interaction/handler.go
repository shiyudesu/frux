package interfaceshttpinteraction

import (
	"context"
	"errors"
	applicationinteraction "github.com/shiyudesu/frux/internal/application/interaction"
	domaininteraction "github.com/shiyudesu/frux/internal/domain/interaction"
	interfaceshttpapierror "github.com/shiyudesu/frux/internal/interfaces/http/apierror"
	interfaceshttpbinding "github.com/shiyudesu/frux/internal/interfaces/http/binding"
	interfaceshttpmiddleware "github.com/shiyudesu/frux/internal/interfaces/http/middleware"
	"net/http"
	"strconv"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
)

type Handler struct {
	service *applicationinteraction.Service
}

// Handler 层负责 HTTP 参数解析、鉴权上下文读取和响应转换。
func New(service *applicationinteraction.Service) *Handler {
	return &Handler{service: service}
}

// Like 处理点赞接口：把当前用户对指定视频的点赞状态设置为有效。
func (h *Handler) Like(ctx context.Context, c *app.RequestContext) {
	// PUT /videos/{videoId}/like 进入这里，active=true 表示设置点赞生效。
	h.setLike(ctx, c, true)
}

// Unlike 处理取消点赞接口：把当前用户对指定视频的点赞状态设置为取消。
func (h *Handler) Unlike(ctx context.Context, c *app.RequestContext) {
	// DELETE /videos/{videoId}/like 进入这里，active=false 表示取消点赞。
	h.setLike(ctx, c, false)
}

// Favorite 处理收藏接口：把当前用户对指定视频的收藏状态设置为有效。
func (h *Handler) Favorite(ctx context.Context, c *app.RequestContext) {
	// 收藏和点赞共享同一套状态模型，只是 action_type 不同。
	h.setFavorite(ctx, c, true)
}

// Unfavorite 处理取消收藏接口：把当前用户对指定视频的收藏状态设置为取消。
func (h *Handler) Unfavorite(ctx context.Context, c *app.RequestContext) {
	h.setFavorite(ctx, c, false)
}

// CreateComment 创建视频评论，videoId 来自路径，评论内容来自请求体。
func (h *Handler) CreateComment(ctx context.Context, c *app.RequestContext) {
	// JWT 中间件会把用户 ID 写入 RequestContext，业务 Handler 从上下文取登录用户。
	userID, ok := userIDFromContext(c)
	if !ok {
		interfaceshttpapierror.WriteInvalidAccessToken(c)
		return
	}

	// videoId 放在路径里，体现评论属于某个视频资源。
	videoID, err := parsePositiveInt64(c.Param("videoId"), domaininteraction.ErrInvalidVideoID)
	if err != nil {
		writeInteractionError(c, err)
		return
	}

	var req createCommentRequest
	if err := interfaceshttpbinding.BindJSON(c, &req); err != nil {
		interfaceshttpapierror.WriteInvalidRequest(c)
		return
	}

	result, err := h.service.CreateComment(ctx, userID, videoID, req.Content, string(c.GetHeader("Idempotency-Key")))
	if err != nil {
		writeInteractionError(c, err)
		return
	}
	c.JSON(http.StatusCreated, commentResponseFromResult(result))
}

// ListComments 查询指定视频的评论列表，分页参数来自 query。
func (h *Handler) ListComments(ctx context.Context, c *app.RequestContext) {
	// 评论列表是视频的子资源，查询条件只保留分页参数。
	videoID, err := parsePositiveInt64(c.Param("videoId"), domaininteraction.ErrInvalidVideoID)
	if err != nil {
		writeInteractionError(c, err)
		return
	}

	limit, err := parseLimit(c.Query("limit"))
	if err != nil {
		writeInteractionError(c, err)
		return
	}

	viewerID, _ := userIDFromContext(c)
	result, err := h.service.ListCommentRoots(ctx, videoID, viewerID, roleFromContext(c), c.Query("sort"), c.Query("cursor"), limit)
	if err != nil {
		writeInteractionError(c, err)
		return
	}
	c.JSON(http.StatusOK, commentListResponseFromResult(result))
}

func (h *Handler) CreateReply(ctx context.Context, c *app.RequestContext) {
	userID, ok := userIDFromContext(c)
	if !ok {
		interfaceshttpapierror.WriteInvalidAccessToken(c)
		return
	}
	videoID, err := parsePositiveInt64(c.Param("videoId"), domaininteraction.ErrInvalidVideoID)
	if err != nil {
		writeInteractionError(c, err)
		return
	}
	targetCommentID, err := parsePositiveInt64(c.Param("commentId"), domaininteraction.ErrInvalidReplyTargetID)
	if err != nil {
		writeInteractionError(c, err)
		return
	}
	var req createCommentRequest
	if err := interfaceshttpbinding.BindJSON(c, &req); err != nil {
		interfaceshttpapierror.WriteInvalidRequest(c)
		return
	}
	result, err := h.service.CreateReply(
		ctx, userID, videoID, targetCommentID, req.Content, string(c.GetHeader("Idempotency-Key")),
	)
	if err != nil {
		writeInteractionError(c, err)
		return
	}
	c.JSON(http.StatusCreated, commentResponseFromResult(result))
}

func (h *Handler) ListReplies(ctx context.Context, c *app.RequestContext) {
	rootCommentID, err := parsePositiveInt64(c.Param("commentId"), domaininteraction.ErrInvalidRootCommentID)
	if err != nil {
		writeInteractionError(c, err)
		return
	}
	limit, err := parseLimit(c.Query("limit"))
	if err != nil {
		writeInteractionError(c, err)
		return
	}
	viewerID, _ := userIDFromContext(c)
	result, err := h.service.ListCommentReplies(
		ctx, rootCommentID, viewerID, roleFromContext(c), c.Query("cursor"), limit,
	)
	if err != nil {
		writeInteractionError(c, err)
		return
	}
	c.JSON(http.StatusOK, replyListResponseFromResult(result))
}

func (h *Handler) GetThreadContext(ctx context.Context, c *app.RequestContext) {
	targetCommentID, err := parsePositiveInt64(c.Param("commentId"), domaininteraction.ErrInvalidCommentID)
	if err != nil {
		writeInteractionError(c, err)
		return
	}
	limit, err := parseLimit(c.Query("limit"))
	if err != nil {
		writeInteractionError(c, err)
		return
	}
	viewerID, _ := userIDFromContext(c)
	result, err := h.service.GetCommentThreadContext(
		ctx, targetCommentID, viewerID, roleFromContext(c), limit,
	)
	if err != nil {
		writeInteractionError(c, err)
		return
	}
	c.JSON(http.StatusOK, threadContextResponseFromResult(result))
}

func (h *Handler) LikeComment(ctx context.Context, c *app.RequestContext) {
	h.setCommentLike(ctx, c, true)
}

func (h *Handler) UnlikeComment(ctx context.Context, c *app.RequestContext) {
	h.setCommentLike(ctx, c, false)
}

// DeleteComment 删除评论，权限判断交给应用层和仓储层完成。
func (h *Handler) DeleteComment(ctx context.Context, c *app.RequestContext) {
	userID, ok := userIDFromContext(c)
	if !ok {
		interfaceshttpapierror.WriteInvalidAccessToken(c)
		return
	}
	role := roleFromContext(c)

	// 删除权限在应用服务和仓储中判断，Handler 只负责传入操作者信息。
	commentID, err := parsePositiveInt64(c.Param("commentId"), domaininteraction.ErrInvalidCommentID)
	if err != nil {
		writeInteractionError(c, err)
		return
	}

	result, err := h.service.DeleteComment(ctx, commentID, userID, role)
	if err != nil {
		writeInteractionError(c, err)
		return
	}
	c.JSON(http.StatusOK, deleteCommentResponse{
		CommentID: result.CommentID, Status: result.Status, CommentCount: result.CommentCount,
		RootReplyCount: result.RootReplyCount, DeletedCount: result.DeletedCount,
		ThreadHidden: result.ThreadHidden, Tombstone: result.Tombstone,
	})
}

func (h *Handler) setCommentLike(ctx context.Context, c *app.RequestContext, active bool) {
	userID, ok := userIDFromContext(c)
	if !ok {
		interfaceshttpapierror.WriteInvalidAccessToken(c)
		return
	}
	commentID, err := parsePositiveInt64(c.Param("commentId"), domaininteraction.ErrInvalidCommentID)
	if err != nil {
		writeInteractionError(c, err)
		return
	}
	var result *applicationinteraction.CommentLikeResult
	if active {
		result, err = h.service.LikeComment(ctx, commentID, userID, string(c.GetHeader("Idempotency-Key")))
	} else {
		result, err = h.service.UnlikeComment(ctx, commentID, userID, string(c.GetHeader("Idempotency-Key")))
	}
	if err != nil {
		writeInteractionError(c, err)
		return
	}
	c.JSON(http.StatusOK, commentLikeResponse{
		CommentID: result.CommentID, RootCommentID: result.RootCommentID,
		Liked: result.Liked, LikeCount: result.LikeCount,
		LikedByVideoAuthor: result.LikedByVideoAuthor,
	})
}

func (h *Handler) setLike(ctx context.Context, c *app.RequestContext, active bool) {
	// 点赞和取消点赞共用参数解析逻辑，active 决定最终状态。
	userID, ok := userIDFromContext(c)
	if !ok {
		interfaceshttpapierror.WriteInvalidAccessToken(c)
		return
	}

	videoID, err := parsePositiveInt64(c.Param("videoId"), domaininteraction.ErrInvalidVideoID)
	if err != nil {
		writeInteractionError(c, err)
		return
	}

	var result *applicationinteraction.ActionResult
	recommendationRequestID := string(c.GetHeader("X-Recommendation-Request-ID"))
	if active {
		result, err = h.service.LikeWithRecommendation(ctx, userID, videoID, string(c.GetHeader("Idempotency-Key")), recommendationRequestID)
	} else {
		result, err = h.service.UnlikeWithRecommendation(ctx, userID, videoID, string(c.GetHeader("Idempotency-Key")), recommendationRequestID)
	}
	if err != nil {
		writeInteractionError(c, err)
		return
	}
	c.JSON(http.StatusOK, actionResponseFromResult(result))
}

func (h *Handler) setFavorite(ctx context.Context, c *app.RequestContext, active bool) {
	// 收藏和取消收藏共用参数解析逻辑，active 决定最终状态。
	userID, ok := userIDFromContext(c)
	if !ok {
		interfaceshttpapierror.WriteInvalidAccessToken(c)
		return
	}

	videoID, err := parsePositiveInt64(c.Param("videoId"), domaininteraction.ErrInvalidVideoID)
	if err != nil {
		writeInteractionError(c, err)
		return
	}

	var result *applicationinteraction.ActionResult
	recommendationRequestID := string(c.GetHeader("X-Recommendation-Request-ID"))
	if active {
		result, err = h.service.FavoriteWithRecommendation(ctx, userID, videoID, string(c.GetHeader("Idempotency-Key")), recommendationRequestID)
	} else {
		result, err = h.service.UnfavoriteWithRecommendation(ctx, userID, videoID, string(c.GetHeader("Idempotency-Key")), recommendationRequestID)
	}
	if err != nil {
		writeInteractionError(c, err)
		return
	}
	c.JSON(http.StatusOK, actionResponseFromResult(result))
}

// userIDFromContext 从 JWT 中间件写入的上下文中读取当前登录用户 ID。
func userIDFromContext(c *app.RequestContext) (int64, bool) {
	// ContextUserIDKey 由 JWT 中间件写入，缺失时按未登录处理。
	value, exists := c.Get(interfaceshttpmiddleware.ContextUserIDKey)
	if !exists {
		return 0, false
	}
	userID, ok := value.(int64)
	return userID, ok && userID > 0
}

func roleFromContext(c *app.RequestContext) string {
	value, exists := c.Get(interfaceshttpmiddleware.ContextRoleKey)
	if !exists {
		return ""
	}
	role, _ := value.(string)
	return role
}

// parsePositiveInt64 统一解析路径参数和查询参数中的正整数 ID。
func parsePositiveInt64(raw string, fallback error) (int64, error) {
	value, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || value <= 0 {
		return 0, fallback
	}
	return value, nil
}

// parseLimit 只处理用户显式传入的 limit，默认值由应用服务统一决定。
func parseLimit(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 {
		return 0, domaininteraction.ErrInvalidLimit
	}
	return limit, nil
}

// actionResponseFromResult 把应用层点赞/收藏结果转换为 HTTP 响应。
func actionResponseFromResult(result *applicationinteraction.ActionResult) actionResponse {
	return actionResponse{
		VideoID:       result.VideoID,
		ActionType:    result.ActionType,
		Active:        result.Active,
		LikeCount:     result.LikeCount,
		FavoriteCount: result.FavoriteCount,
	}
}

func commentResponseFromResult(result *applicationinteraction.CreateCommentResult) commentResponse {
	response := commentResponseFromDomain(result.Comment)
	response.CommentCount = result.CommentCount
	return response
}

// commentListResponseFromResult 把领域评论列表转换为前端需要的列表结构。
func commentListResponseFromResult(result *applicationinteraction.CommentListResult) commentListResponse {
	items := make([]commentResponse, 0, len(result.Items))
	for _, item := range result.Items {
		items = append(items, commentResponseFromDomain(item))
	}
	return commentListResponse{
		Items:        items,
		NextCursor:   result.NextCursor,
		HasMore:      result.HasMore,
		CommentCount: result.CommentCount,
		Sort:         result.Sort,
	}
}

func replyListResponseFromResult(result *applicationinteraction.ReplyListResult) replyListResponse {
	return replyListResponse{
		RootCommentID: result.RootCommentID,
		Items:         commentResponsesFromDomain(result.Items),
		NextCursor:    result.NextCursor,
		HasMore:       result.HasMore,
		CommentCount:  result.CommentCount,
	}
}

func threadContextResponseFromResult(result *applicationinteraction.ThreadContextResult) threadContextResponse {
	return threadContextResponse{
		Root:         commentResponseFromDomain(result.Root),
		Replies:      commentResponsesFromDomain(result.Replies),
		Target:       commentResponseFromDomain(result.Target),
		NextCursor:   result.NextCursor,
		HasMore:      result.HasMore,
		CommentCount: result.CommentCount,
	}
}

func commentResponsesFromDomain(comments []*domaininteraction.Comment) []commentResponse {
	items := make([]commentResponse, 0, len(comments))
	for _, comment := range comments {
		items = append(items, commentResponseFromDomain(comment))
	}
	return items
}

func commentResponseFromDomain(comment *domaininteraction.Comment) commentResponse {
	if comment == nil {
		return commentResponse{}
	}
	return commentResponse{
		ID: comment.ID, VideoID: comment.VideoID, UserID: comment.UserID,
		UserNickname: comment.UserNickname, UserAvatarURL: comment.UserAvatarURL,
		RootCommentID: comment.RootCommentID, ReplyToCommentID: comment.ReplyToCommentID,
		ReplyToUserID:        comment.ReplyToUserID,
		ReplyToUserNickname:  comment.ReplyToUserNickname,
		ReplyToUserAvatarURL: comment.ReplyToUserAvatarURL,
		Content:              comment.Content, Status: comment.Status, Deleted: comment.Deleted(),
		ReplyCount: comment.ReplyCount, ReplyPreviews: commentResponsesFromDomain(comment.ReplyPreviews),
		LikeCount: comment.LikeCount, Liked: comment.Liked, CanDelete: comment.CanDelete,
		IsVideoAuthor: comment.IsVideoAuthor, LikedByVideoAuthor: comment.LikedByVideoAuthor,
		HotScore: comment.HotScore, CreatedAt: comment.CreatedAt,
	}
}

func writeInteractionError(c *app.RequestContext, err error) {
	// 统一错误映射让所有互动接口返回一致的 HTTP 状态码和 JSON 格式。
	if isBadRequestError(err) {
		interfaceshttpapierror.Write(c, http.StatusBadRequest, interfaceshttpapierror.CodeInteractionValidationFailed, err.Error())
		return
	}
	if errors.Is(err, domaininteraction.ErrVideoNotFound) ||
		errors.Is(err, domaininteraction.ErrCommentNotFound) ||
		errors.Is(err, domaininteraction.ErrCommentUnavailable) ||
		errors.Is(err, domaininteraction.ErrCommentModerated) ||
		errors.Is(err, domaininteraction.ErrReplyTargetUnavailable) {
		interfaceshttpapierror.Write(c, http.StatusNotFound, interfaceshttpapierror.CodeInteractionResourceNotFound, "resource not found")
		return
	}
	if errors.Is(err, domaininteraction.ErrCommentPermissionDenied) {
		interfaceshttpapierror.Write(c, http.StatusForbidden, interfaceshttpapierror.CodeInteractionCommentPermissionDenied, "comment permission denied")
		return
	}
	if errors.Is(err, domaininteraction.ErrActionIdempotencyConflict) ||
		errors.Is(err, domaininteraction.ErrCommentIdempotencyConflict) ||
		errors.Is(err, domaininteraction.ErrCommentLikeIdempotencyConflict) {
		interfaceshttpapierror.Write(c, http.StatusConflict, interfaceshttpapierror.CodeInteractionIdempotencyConflict, "idempotency key conflicts with another payload")
		return
	}
	interfaceshttpapierror.WriteInternal(c, "internal server error", err)
}

func isBadRequestError(err error) bool {
	return errors.Is(err, domaininteraction.ErrInvalidUserID) ||
		errors.Is(err, domaininteraction.ErrInvalidVideoID) ||
		errors.Is(err, domaininteraction.ErrInvalidCommentID) ||
		errors.Is(err, domaininteraction.ErrInvalidRootCommentID) ||
		errors.Is(err, domaininteraction.ErrInvalidReplyTargetID) ||
		errors.Is(err, domaininteraction.ErrInvalidActionType) ||
		errors.Is(err, domaininteraction.ErrInvalidLimit) ||
		errors.Is(err, domaininteraction.ErrInvalidCursor) ||
		errors.Is(err, domaininteraction.ErrInvalidCommentSort) ||
		errors.Is(err, domaininteraction.ErrEmptyCommentContent) ||
		errors.Is(err, domaininteraction.ErrCommentContentTooLong) ||
		errors.Is(err, domaininteraction.ErrIdempotencyKeyTooLong) ||
		errors.Is(err, domaininteraction.ErrRecommendationRequestIDTooLong)
}
