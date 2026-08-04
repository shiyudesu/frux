package interfaceshttpsearch

import (
	applicationsearch "GCFeed/internal/application/search"
	domainsearch "GCFeed/internal/domain/search"
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
)

type Handler struct {
	service *applicationsearch.Service
}

func New(service *applicationsearch.Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) Videos(ctx context.Context, c *app.RequestContext) {
	page, err := h.service.SearchVideos(ctx, applicationsearch.Request{
		Query: c.Query("q"), Cursor: c.Query("cursor"), Limit: parseLimit(c),
	})
	if err != nil {
		writeSearchError(c, err)
		return
	}
	items := make([]videoResultResponse, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, videoResultResponse{
			ID: item.ID, AuthorID: item.AuthorID, Title: item.Title, Description: item.Description,
			MediaURL: item.MediaURL, CoverURL: item.CoverURL, Status: item.Status,
			Visibility: item.Visibility, LikeCount: item.LikeCount, CommentCount: item.CommentCount,
			FavoriteCount: item.FavoriteCount, PublishedAt: item.PublishedAt,
			CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt, MediaStatus: item.MediaStatus,
			PlaybackSources: item.PlaybackSources,
		})
	}
	c.JSON(http.StatusOK, videoPageResponse{
		Items: items, NextCursor: page.NextCursor, HasMore: page.HasMore,
	})
}

func (h *Handler) Users(ctx context.Context, c *app.RequestContext) {
	page, err := h.service.SearchUsers(ctx, applicationsearch.Request{
		Query: c.Query("q"), Cursor: c.Query("cursor"), Limit: parseLimit(c),
	})
	if err != nil {
		writeSearchError(c, err)
		return
	}
	items := make([]userResultResponse, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, userResultResponse{
			ID: item.ID, Account: item.Account, Nickname: item.Nickname,
			AvatarURL: item.AvatarURL, Bio: item.Bio,
		})
	}
	c.JSON(http.StatusOK, userPageResponse{
		Items: items, NextCursor: page.NextCursor, HasMore: page.HasMore,
	})
}

func parseLimit(c *app.RequestContext) int {
	value := strings.TrimSpace(c.Query("limit"))
	if value == "" {
		return 0
	}
	limit, err := strconv.Atoi(value)
	if err != nil || limit <= 0 {
		return -1
	}
	return limit
}

func writeSearchError(c *app.RequestContext, err error) {
	switch {
	case errors.Is(err, domainsearch.ErrEmptyQuery),
		errors.Is(err, domainsearch.ErrInvalidQuery),
		errors.Is(err, domainsearch.ErrQueryTooLong),
		errors.Is(err, domainsearch.ErrInvalidLimit),
		errors.Is(err, domainsearch.ErrInvalidCursor):
		c.JSON(http.StatusBadRequest, utils.H{"error": err.Error()})
	default:
		c.JSON(http.StatusInternalServerError, utils.H{"error": "internal server error"})
	}
}
