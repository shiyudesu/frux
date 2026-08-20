package interfaceshttpvideo

import (
	"context"
	"errors"
	applicationvideo "github.com/shiyudesu/frux/internal/application/video"
	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
	domainvideo "github.com/shiyudesu/frux/internal/domain/video"
	interfaceshttpapierror "github.com/shiyudesu/frux/internal/interfaces/http/apierror"
	interfaceshttpbinding "github.com/shiyudesu/frux/internal/interfaces/http/binding"
	interfaceshttpmiddleware "github.com/shiyudesu/frux/internal/interfaces/http/middleware"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
)

const defaultListLimit = 20

type Handler struct {
	service    *applicationvideo.Service
	management *applicationvideo.ManagementService
}

// New 注入视频应用服务。
func New(service *applicationvideo.Service, management ...*applicationvideo.ManagementService) *Handler {
	handler := &Handler{service: service}
	if len(management) > 0 {
		handler.management = management[0]
	}
	return handler
}

func (h *Handler) QueryMine(ctx context.Context, c *app.RequestContext) {
	userID, ok := userIDFromContext(c)
	if !ok {
		interfaceshttpapierror.WriteInvalidAccessToken(c)
		return
	}
	var req CreatorVideoQueryRequest
	if err := interfaceshttpbinding.BindJSON(c, &req); err != nil {
		interfaceshttpapierror.WriteInvalidRequest(c)
		return
	}
	createdFrom, err := parseOptionalDateTime(req.CreatedFrom, false)
	if err != nil {
		writeVideoError(c, domainvideo.ErrInvalidDateRange)
		return
	}
	createdTo, err := parseOptionalDateTime(req.CreatedTo, true)
	if err != nil {
		writeVideoError(c, domainvideo.ErrInvalidDateRange)
		return
	}
	result, err := h.management.QueryCreatorVideos(ctx, userID, applicationvideo.CreatorQueryRequest{
		VideoID: req.VideoID, Visibility: req.Visibility,
		Statuses: req.Statuses, Query: req.Query, CreatedFrom: createdFrom,
		CreatedTo: createdTo, Cursor: req.Cursor, Limit: req.Limit,
	})
	if err != nil {
		writeVideoError(c, err)
		return
	}
	items := make([]videoResponse, 0, len(result.Items))
	for _, video := range result.Items {
		items = append(items, videoResponseFromDomain(video))
	}
	c.JSON(http.StatusOK, cursorVideoListResponse{Items: items, NextCursor: result.NextCursor, HasMore: result.HasMore})
}

func (h *Handler) ListArchiveMonths(ctx context.Context, c *app.RequestContext) {
	userID, ok := userIDFromContext(c)
	if !ok {
		interfaceshttpapierror.WriteInvalidAccessToken(c)
		return
	}
	result, err := h.management.ListCreatorArchiveMonths(ctx, userID, c.Query("visibility"))
	if err != nil {
		writeVideoError(c, err)
		return
	}
	c.JSON(http.StatusOK, creatorArchiveMonthResponse{Months: result.Months})
}

func (h *Handler) BatchAction(ctx context.Context, c *app.RequestContext) {
	userID, ok := userIDFromContext(c)
	if !ok {
		interfaceshttpapierror.WriteInvalidAccessToken(c)
		return
	}
	var req BatchVideoActionRequest
	if err := interfaceshttpbinding.BindJSON(c, &req); err != nil {
		interfaceshttpapierror.WriteInvalidRequest(c)
		return
	}
	result, err := h.management.ApplyBatch(ctx, userID, req.Action, req.VideoIDs, string(c.GetHeader("Idempotency-Key")))
	if err != nil {
		writeVideoError(c, err)
		return
	}
	c.JSON(http.StatusOK, batchVideoActionResponse{Action: result.Action, VideoIDs: result.VideoIDs, Replayed: result.Replayed})
}

// Create 处理发布视频请求，用户身份来自 JWT，上行数据来自 JSON 请求体。
func (h *Handler) Create(ctx context.Context, c *app.RequestContext) {
	userID, ok := userIDFromContext(c)
	if !ok {
		interfaceshttpapierror.WriteInvalidAccessToken(c)
		return
	}

	var req CreateVideoRequest
	if err := interfaceshttpbinding.BindJSON(c, &req); err != nil {
		interfaceshttpapierror.WriteInvalidRequest(c)
		return
	}

	// Idempotency-Key 来自请求头，用于客户端重试时获得同一个视频结果。
	var result *applicationvideo.CreateResult
	var err error
	if req.MediaAssetID > 0 || req.CoverAssetID > 0 {
		result, err = h.service.CreateWithAssets(
			ctx, userID, req.Title, req.Description, req.MediaAssetID, req.CoverAssetID,
			string(c.GetHeader("Idempotency-Key")),
		)
	} else {
		result, err = h.service.CreatePublished(
			ctx, userID, req.Title, req.Description, req.MediaURL, req.CoverURL,
			string(c.GetHeader("Idempotency-Key")),
		)
	}
	if err != nil {
		writeVideoError(c, err)
		return
	}

	status := http.StatusCreated
	if !result.Created {
		// 幂等重放返回已有资源，使用 200 表示本次没有新建记录。
		status = http.StatusOK
	}
	c.JSON(status, videoResponseFromDomain(result.Video))
}

// Get 查询公开视频详情，videoId 来自 RESTful 路径参数。
func (h *Handler) Get(ctx context.Context, c *app.RequestContext) {
	videoID, err := parsePositiveInt64(c.Param("videoId"))
	if err != nil {
		interfaceshttpapierror.Write(c, http.StatusBadRequest, interfaceshttpapierror.CodeVideoValidationFailed, "invalid video id")
		return
	}

	video, err := h.service.Get(ctx, videoID)
	if err != nil {
		writeVideoError(c, err)
		return
	}

	c.JSON(http.StatusOK, videoResponseFromDomain(video))
}

// Delete 删除当前用户自己的视频，删除操作在领域层做作者权限校验。
func (h *Handler) Delete(ctx context.Context, c *app.RequestContext) {
	userID, ok := userIDFromContext(c)
	if !ok {
		interfaceshttpapierror.WriteInvalidAccessToken(c)
		return
	}

	videoID, err := parsePositiveInt64(c.Param("videoId"))
	if err != nil {
		interfaceshttpapierror.Write(c, http.StatusBadRequest, interfaceshttpapierror.CodeVideoValidationFailed, "invalid video id")
		return
	}

	if err := h.service.Delete(ctx, userID, videoID); err != nil {
		writeVideoError(c, err)
		return
	}

	c.Status(http.StatusNoContent)
}

// ListByAuthor 查询指定用户的公开作品列表。
func (h *Handler) ListByAuthor(ctx context.Context, c *app.RequestContext) {
	authorID, err := parsePositiveInt64(c.Param("userId"))
	if err != nil {
		interfaceshttpapierror.Write(c, http.StatusBadRequest, interfaceshttpapierror.CodeVideoValidationFailed, "invalid user id")
		return
	}

	limit, offset, err := parsePagination(c)
	if err != nil {
		writeVideoError(c, err)
		return
	}

	videos, err := h.service.ListByAuthor(ctx, authorID, limit, offset)
	if err != nil {
		writeVideoError(c, err)
		return
	}

	c.JSON(http.StatusOK, videoListResponseFromDomain(videos, limit, offset))
}

// ListMine 查询当前登录用户自己的作品列表。
func (h *Handler) ListMine(ctx context.Context, c *app.RequestContext) {
	userID, ok := userIDFromContext(c)
	if !ok {
		interfaceshttpapierror.WriteInvalidAccessToken(c)
		return
	}

	limit, offset, err := parsePagination(c)
	if err != nil {
		writeVideoError(c, err)
		return
	}

	videos, err := h.service.ListMine(ctx, userID, limit, offset)
	if err != nil {
		writeVideoError(c, err)
		return
	}

	c.JSON(http.StatusOK, videoListResponseFromDomain(videos, limit, offset))
}

// userIDFromContext 从 JWT 中间件写入的上下文读取登录用户 ID。
func userIDFromContext(c *app.RequestContext) (int64, bool) {
	value, exists := c.Get(interfaceshttpmiddleware.ContextUserIDKey)
	if !exists {
		return 0, false
	}
	userID, ok := value.(int64)
	return userID, ok && userID > 0
}

// parsePositiveInt64 解析 RESTful 路径中的正整数 ID。
func parsePositiveInt64(raw string) (int64, error) {
	value, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || value <= 0 {
		return 0, domainvideo.ErrInvalidVideoID
	}
	return value, nil
}

// parsePagination 解析 offset 分页参数，默认 limit 在 Handler 层给出。
func parsePagination(c *app.RequestContext) (int, int, error) {
	limit := defaultListLimit
	offset := 0

	rawLimit := strings.TrimSpace(c.Query("limit"))
	if rawLimit != "" {
		// limit 必须为正数，应用层会进一步限制最大值。
		value, err := strconv.Atoi(rawLimit)
		if err != nil || value <= 0 {
			return 0, 0, domainvideo.ErrInvalidLimit
		}
		limit = value
	}

	rawOffset := strings.TrimSpace(c.Query("offset"))
	if rawOffset != "" {
		// offset 允许为 0，表示从第一条开始。
		value, err := strconv.Atoi(rawOffset)
		if err != nil || value < 0 {
			return 0, 0, domainvideo.ErrInvalidOffset
		}
		offset = value
	}

	return limit, offset, nil
}

// videoResponseFromDomain 把领域视频转换成 HTTP JSON 响应。
func videoResponseFromDomain(video *domainvideo.Video) videoResponse {
	return videoResponse{
		ID:              video.ID,
		AuthorID:        video.AuthorID,
		Title:           video.Title,
		Description:     video.Description,
		MediaURL:        video.MediaURL,
		CoverURL:        video.CoverURL,
		Status:          video.Status,
		Visibility:      video.Visibility,
		LikeCount:       video.LikeCount,
		CommentCount:    video.CommentCount,
		FavoriteCount:   video.FavoriteCount,
		PublishedAt:     video.PublishedAt,
		CreatedAt:       video.CreatedAt,
		UpdatedAt:       video.UpdatedAt,
		MediaAssetID:    video.MediaAssetID,
		CoverAssetID:    video.CoverAssetID,
		MediaStatus:     video.MediaStatus,
		ReviewVersion:   video.ReviewVersion,
		MediaErrorCode:  video.MediaErrorCode,
		PlaybackSources: video.PlaybackSources,
	}
}

func parseOptionalDateTime(raw string, endOfDay bool) (*time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if value, err := time.Parse(time.RFC3339, raw); err == nil {
		return &value, nil
	}
	value, err := time.Parse("2006-01-02", raw)
	if err != nil {
		return nil, err
	}
	if endOfDay {
		value = value.Add(24*time.Hour - time.Nanosecond)
	}
	return &value, nil
}

// videoListResponseFromDomain 组装列表响应，并回显本次分页参数。
func videoListResponseFromDomain(videos []*domainvideo.Video, limit, offset int) videoListResponse {
	items := make([]videoResponse, 0, len(videos))
	for _, video := range videos {
		items = append(items, videoResponseFromDomain(video))
	}
	return videoListResponse{
		Items:  items,
		Limit:  limit,
		Offset: offset,
	}
}

// writeVideoError 统一视频接口错误到 HTTP 状态码的映射。
func writeVideoError(c *app.RequestContext, err error) {
	if isBadRequestError(err) {
		interfaceshttpapierror.Write(c, http.StatusBadRequest, interfaceshttpapierror.CodeVideoValidationFailed, err.Error())
		return
	}
	if errors.Is(err, domainvideo.ErrVideoNotFound) {
		interfaceshttpapierror.Write(c, http.StatusNotFound, interfaceshttpapierror.CodeVideoNotFound, "video not found")
		return
	}
	if errors.Is(err, domainmedia.ErrMediaAssetNotFound) {
		interfaceshttpapierror.Write(c, http.StatusNotFound, interfaceshttpapierror.CodeMediaAssetNotFound, "media asset not found")
		return
	}
	if errors.Is(err, domainvideo.ErrVideoPermissionDenied) {
		interfaceshttpapierror.Write(c, http.StatusForbidden, interfaceshttpapierror.CodeVideoPermissionDenied, "video permission denied")
		return
	}
	if errors.Is(err, domainvideo.ErrLocalAssetPermissionDenied) {
		interfaceshttpapierror.Write(c, http.StatusForbidden, interfaceshttpapierror.CodeLocalAssetPermissionDenied, "local asset permission denied")
		return
	}
	if errors.Is(err, domainvideo.ErrBatchIdempotencyConflict) {
		interfaceshttpapierror.Write(c, http.StatusConflict, interfaceshttpapierror.CodeVideoIdempotencyConflict, "idempotency key conflict")
		return
	}
	interfaceshttpapierror.WriteInternal(c, "internal server error", err)
}

// isBadRequestError 判断哪些视频领域错误属于客户端请求问题。
func isBadRequestError(err error) bool {
	return errors.Is(err, domainvideo.ErrInvalidVideoID) ||
		errors.Is(err, domainvideo.ErrInvalidAuthorID) ||
		errors.Is(err, domainvideo.ErrEmptyTitle) ||
		errors.Is(err, domainvideo.ErrTitleTooLong) ||
		errors.Is(err, domainvideo.ErrDescriptionTooLong) ||
		errors.Is(err, domainvideo.ErrEmptyMediaURL) ||
		errors.Is(err, domainvideo.ErrEmptyCoverURL) ||
		errors.Is(err, domainvideo.ErrIdempotencyKeyTooLong) ||
		errors.Is(err, domainvideo.ErrInvalidLimit) ||
		errors.Is(err, domainvideo.ErrInvalidOffset) ||
		errors.Is(err, domainvideo.ErrInvalidVisibility) ||
		errors.Is(err, domainvideo.ErrInvalidStatus) ||
		errors.Is(err, domainvideo.ErrVideoStateNotAllowed) ||
		errors.Is(err, domainvideo.ErrInvalidCursor) ||
		errors.Is(err, domainvideo.ErrInvalidDateRange) ||
		errors.Is(err, domainvideo.ErrInvalidBatchAction) ||
		errors.Is(err, domainvideo.ErrTooManyVideoIDs) ||
		errors.Is(err, domainvideo.ErrEmptyVideoIDs) ||
		errors.Is(err, domainvideo.ErrIdempotencyKeyRequired) ||
		errors.Is(err, domainvideo.ErrInvalidLocalAsset) ||
		errors.Is(err, domainmedia.ErrInvalidAssetID)
}
