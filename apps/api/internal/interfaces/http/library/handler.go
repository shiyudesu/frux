package interfaceshttplibrary

import (
	applicationlibrary "GCFeed/internal/application/library"
	domainlibrary "GCFeed/internal/domain/library"
	interfaceshttpmiddleware "GCFeed/internal/interfaces/http/middleware"
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
)

type Handler struct {
	service *applicationlibrary.Service
}

func New(service *applicationlibrary.Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) ListLiked(ctx context.Context, c *app.RequestContext) {
	userID, ok := userIDFromContext(c)
	if !ok {
		unauthorized(c)
		return
	}
	page, err := h.service.ListLiked(ctx, userID, c.Query("cursor"), parseLimit(c))
	h.writePage(c, page, err)
}

func (h *Handler) ListFavorites(ctx context.Context, c *app.RequestContext) {
	userID, ok := userIDFromContext(c)
	if !ok {
		unauthorized(c)
		return
	}
	page, err := h.service.ListFavorites(ctx, userID, c.Query("cursor"), parseLimit(c))
	h.writePage(c, page, err)
}

func (h *Handler) ListPublicLiked(ctx context.Context, c *app.RequestContext) {
	userID, err := parseID(c.Param("userId"))
	if err != nil {
		writeError(c, err)
		return
	}
	page, err := h.service.ListPublicLiked(ctx, userID, c.Query("cursor"), parseLimit(c))
	h.writePage(c, page, err)
}

func (h *Handler) ListHistory(ctx context.Context, c *app.RequestContext) {
	userID, ok := userIDFromContext(c)
	if !ok {
		unauthorized(c)
		return
	}
	page, err := h.service.ListHistory(ctx, userID, c.Query("cursor"), parseLimit(c))
	h.writePage(c, page, err)
}

func (h *Handler) DeleteHistory(ctx context.Context, c *app.RequestContext) {
	userID, ok := userIDFromContext(c)
	if !ok {
		unauthorized(c)
		return
	}
	videoID, err := parseID(c.Param("videoId"))
	if err != nil {
		writeError(c, err)
		return
	}
	if err := h.service.DeleteHistory(ctx, userID, videoID); err != nil {
		writeError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) ClearHistory(ctx context.Context, c *app.RequestContext) {
	userID, ok := userIDFromContext(c)
	if !ok {
		unauthorized(c)
		return
	}
	if err := h.service.ClearHistory(ctx, userID); err != nil {
		writeError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) ListWatchLater(ctx context.Context, c *app.RequestContext) {
	userID, ok := userIDFromContext(c)
	if !ok {
		unauthorized(c)
		return
	}
	page, err := h.service.ListWatchLater(ctx, userID, c.Query("cursor"), parseLimit(c))
	h.writePage(c, page, err)
}

func (h *Handler) AddWatchLater(ctx context.Context, c *app.RequestContext) {
	h.setWatchLater(ctx, c, true)
}

func (h *Handler) RemoveWatchLater(ctx context.Context, c *app.RequestContext) {
	h.setWatchLater(ctx, c, false)
}

func (h *Handler) setWatchLater(ctx context.Context, c *app.RequestContext, active bool) {
	userID, ok := userIDFromContext(c)
	if !ok {
		unauthorized(c)
		return
	}
	videoID, err := parseID(c.Param("videoId"))
	if err != nil {
		writeError(c, err)
		return
	}
	fact, err := h.service.SetWatchLater(ctx, userID, videoID, active)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, watchLaterStateResponse{VideoID: fact.VideoID, Active: fact.Active(), UpdatedAt: fact.UpdatedAt})
}

func (h *Handler) writePage(c *app.RequestContext, page *applicationlibrary.Page, err error) {
	if err != nil {
		writeError(c, err)
		return
	}
	items := make([]videoItemResponse, 0, len(page.Items))
	for _, item := range page.Items {
		if item == nil || item.Video == nil {
			continue
		}
		response := videoItemResponse{Video: videoResponseFromDomain(item.Video), UpdatedAt: item.UpdatedAt}
		if item.History != nil {
			response.History = &historyMetadataResponse{
				LastScene: item.History.LastScene, LastEventType: item.History.LastEventType,
				LastPositionMs: item.History.LastPositionMs, LastWatchMs: item.History.LastWatchMs,
				EffectiveWatchMs: item.History.LastWatchMs, Completed: item.History.Completed,
				LastWatchedAt: item.History.UpdatedAt,
			}
		}
		items = append(items, response)
	}
	c.JSON(http.StatusOK, videoPageResponse{Items: items, NextCursor: page.NextCursor, HasMore: page.HasMore})
}

func videoResponseFromDomain(video *domainlibrary.VideoCard) videoResponse {
	return videoResponse{
		ID: video.ID, AuthorID: video.AuthorID, Title: video.Title, Description: video.Description,
		MediaURL: video.MediaURL, CoverURL: video.CoverURL, Status: video.Status, Visibility: video.Visibility,
		LikeCount: video.LikeCount, CommentCount: video.CommentCount, FavoriteCount: video.FavoriteCount,
		PublishedAt: video.PublishedAt, CreatedAt: video.CreatedAt, UpdatedAt: video.UpdatedAt,
	}
}

func parseLimit(c *app.RequestContext) int {
	raw := strings.TrimSpace(c.Query("limit"))
	if raw == "" {
		return 0
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return -1
	}
	return value
}

func parseID(raw string) (int64, error) {
	value, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || value <= 0 {
		return 0, domainlibrary.ErrInvalidVideoID
	}
	return value, nil
}

func userIDFromContext(c *app.RequestContext) (int64, bool) {
	value, exists := c.Get(interfaceshttpmiddleware.ContextUserIDKey)
	if !exists {
		return 0, false
	}
	userID, ok := value.(int64)
	return userID, ok && userID > 0
}

func unauthorized(c *app.RequestContext) {
	c.JSON(http.StatusUnauthorized, utils.H{"error": "invalid access token"})
}

func writeError(c *app.RequestContext, err error) {
	switch {
	case errors.Is(err, domainlibrary.ErrInvalidUserID),
		errors.Is(err, domainlibrary.ErrInvalidVideoID),
		errors.Is(err, domainlibrary.ErrInvalidCursor),
		errors.Is(err, domainlibrary.ErrInvalidLimit):
		c.JSON(http.StatusBadRequest, utils.H{"error": err.Error()})
	case errors.Is(err, domainlibrary.ErrVideoNotFound):
		c.JSON(http.StatusNotFound, utils.H{"error": "video not found"})
	case errors.Is(err, domainlibrary.ErrLikedVideosPrivate):
		c.JSON(http.StatusForbidden, utils.H{"error": "liked videos are private"})
	default:
		c.JSON(http.StatusInternalServerError, utils.H{"error": "internal server error"})
	}
}
