package interfaceshttpfeed

import (
	applicationfeed "GCFeed/internal/application/feed"
	domainfeed "GCFeed/internal/domain/feed"
	domainrecommendation "GCFeed/internal/domain/recommendation"
	interfaceshttpbinding "GCFeed/internal/interfaces/http/binding"
	interfaceshttpmiddleware "GCFeed/internal/interfaces/http/middleware"
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
)

const maxFeedQueryBodyBytes = 16 * 1024

type Handler struct {
	service *applicationfeed.Service
}

// New 注入 Feed 应用服务。
func New(service *applicationfeed.Service) *Handler {
	return &Handler{service: service}
}

// ListFeedItems 读取指定 scene 的 Feed，cursor 和 limit 来自 query 参数。
func (h *Handler) ListFeedItems(ctx context.Context, c *app.RequestContext) {
	limit, err := parseLimit(c.Query("limit"))
	if err != nil {
		writeFeedError(c, err)
		return
	}

	viewerID, _ := viewerIDFromContext(c)
	result, err := h.service.GetFeed(ctx, applicationfeed.FeedRequest{
		Scene:    domainfeed.Scene(c.Query("scene")),
		Cursor:   c.Query("cursor"),
		Limit:    limit,
		ViewerID: viewerID,
	})
	if err != nil {
		writeFeedError(c, err)
		return
	}

	c.JSON(http.StatusOK, feedItemsResponseFromResult(result))
}

// Query 通过请求体接收复杂 Feed 查询参数，适合推荐上下文逐步扩展。
func (h *Handler) Query(ctx context.Context, c *app.RequestContext) {
	var req feedQueryRequest
	if err := interfaceshttpbinding.BindStrictJSON(c, &req, maxFeedQueryBodyBytes); err != nil {
		c.JSON(http.StatusBadRequest, utils.H{"error": "invalid request"})
		return
	}

	limit, err := parseBodyLimit(req.Limit)
	if err != nil {
		writeFeedError(c, err)
		return
	}

	recommendationContext, err := recommendationContextFromRequest(req.Context)
	if err != nil {
		c.JSON(http.StatusBadRequest, utils.H{"error": err.Error()})
		return
	}

	viewerID, _ := viewerIDFromContext(c)
	result, err := h.service.GetFeed(ctx, applicationfeed.FeedRequest{
		Scene:                 domainfeed.Scene(req.Scene),
		Cursor:                req.Cursor,
		Limit:                 limit,
		ViewerID:              viewerID,
		RecommendationContext: recommendationContext,
	})
	if err != nil {
		writeFeedError(c, err)
		return
	}

	c.JSON(http.StatusOK, feedItemsResponseFromResult(result))
}

func recommendationContextFromRequest(req *recommendationContextRequest) (*domainrecommendation.RecommendationContext, error) {
	if req == nil {
		return nil, nil
	}
	return domainrecommendation.NewRecommendationContext(domainrecommendation.RecommendationContextInput{
		RequestID:            req.RequestID,
		SessionID:            req.SessionID,
		RefreshIndex:         req.RefreshIndex,
		RecentVideoIDs:       req.RecentVideoIDs,
		CurrentVideoID:       req.CurrentVideoID,
		NetworkClass:         req.NetworkClass,
		SaveData:             req.SaveData,
		ViewportClass:        req.ViewportClass,
		PlaybackCapabilities: req.PlaybackCapabilities,
	})
}

// Refresh 从第一页重新读取 Feed，适合下拉刷新语义。
func (h *Handler) Refresh(ctx context.Context, c *app.RequestContext) {
	limit, err := parseLimit(c.Query("limit"))
	if err != nil {
		writeFeedError(c, err)
		return
	}

	result, err := h.service.RefreshFeed(ctx, limit)
	if err != nil {
		writeFeedError(c, err)
		return
	}

	c.JSON(http.StatusOK, feedItemsResponseFromResult(result))
}

// parseLimit 只校验用户传入的 limit，默认值交给应用服务处理。
func parseLimit(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}

	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 {
		return 0, domainfeed.ErrInvalidLimit
	}
	return limit, nil
}

// parseBodyLimit 校验 JSON 请求体中的 limit，空值交给应用服务使用默认页大小。
func parseBodyLimit(value *int) (int, error) {
	if value == nil {
		return 0, nil
	}
	if *value <= 0 {
		return 0, domainfeed.ErrInvalidLimit
	}
	return *value, nil
}

// feedItemsResponseFromResult 把应用层 Feed 结果转换为 HTTP 响应结构。
func feedItemsResponseFromResult(result *applicationfeed.FeedResult) feedItemsResponse {
	items := make([]feedItemResponse, 0, len(result.Items))
	for _, item := range result.Items {
		items = append(items, feedItemResponse{
			VideoID:         item.VideoID,
			AuthorID:        item.AuthorID,
			AuthorNickname:  item.AuthorNickname,
			AuthorAvatarURL: item.AuthorAvatarURL,
			Title:           item.Title,
			Description:     item.Description,
			MediaURL:        item.MediaURL,
			CoverURL:        item.CoverURL,
			LikeCount:       item.LikeCount,
			CommentCount:    item.CommentCount,
			FavoriteCount:   item.FavoriteCount,
			Liked:           item.Liked,
			Favorited:       item.Favorited,
			PublishedAt:     item.PublishedAt,
			MediaStatus:     item.MediaStatus,
			PlaybackSources: item.PlaybackSources,
		})
	}
	return feedItemsResponse{
		Scene:      string(result.Scene),
		RequestID:  result.RequestID,
		Items:      items,
		NextCursor: result.NextCursor,
		HasMore:    result.HasMore,
	}
}

// writeFeedError 统一 Feed 接口错误响应。
func writeFeedError(c *app.RequestContext, err error) {
	if errors.Is(err, domainfeed.ErrViewerRequired) {
		c.JSON(http.StatusUnauthorized, utils.H{"error": err.Error()})
		return
	}
	if errors.Is(err, domainrecommendation.ErrInvalidCursor) {
		c.JSON(http.StatusBadRequest, utils.H{"error": "invalid request"})
		return
	}
	if isBadRequestError(err) {
		c.JSON(http.StatusBadRequest, utils.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusInternalServerError, utils.H{"error": "internal server error"})
}

// isBadRequestError 判断 Feed 参数错误。
func isBadRequestError(err error) bool {
	return errors.Is(err, domainfeed.ErrInvalidLimit) ||
		errors.Is(err, domainfeed.ErrInvalidCursor) ||
		errors.Is(err, domainfeed.ErrUnsupportedScene)
}

// viewerIDFromContext 读取可选登录用户 ID，个性化 Feed 策略可以使用。
func viewerIDFromContext(c *app.RequestContext) (int64, bool) {
	value, exists := c.Get(interfaceshttpmiddleware.ContextUserIDKey)
	if !exists {
		return 0, false
	}
	userID, ok := value.(int64)
	return userID, ok && userID > 0
}
