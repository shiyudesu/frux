package interfaceshttpsearch

import (
	"context"
	"errors"
	applicationsearch "github.com/shiyudesu/frux/internal/application/search"
	domainsearch "github.com/shiyudesu/frux/internal/domain/search"
	domainvideo "github.com/shiyudesu/frux/internal/domain/video"
	interfaceshttpapierror "github.com/shiyudesu/frux/internal/interfaces/http/apierror"
	"net/http"
	"strconv"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
)

type Handler struct {
	service       *applicationsearch.Service
	similarVideos *applicationsearch.SimilarVideoService
}

type Option func(*Handler)

func WithSimilarVideoService(service *applicationsearch.SimilarVideoService) Option {
	return func(handler *Handler) { handler.similarVideos = service }
}

func New(service *applicationsearch.Service, options ...Option) *Handler {
	handler := &Handler{service: service}
	for _, option := range options {
		if option != nil {
			option(handler)
		}
	}
	return handler
}

func (h *Handler) SimilarVideos(ctx context.Context, c *app.RequestContext) {
	videoID, err := strconv.ParseInt(strings.TrimSpace(c.Param("videoId")), 10, 64)
	if err != nil || videoID <= 0 {
		interfaceshttpapierror.Write(c, http.StatusBadRequest, interfaceshttpapierror.CodeSearchParametersInvalid, "搜索参数已失效，请重新搜索")
		return
	}
	if h == nil || h.similarVideos == nil {
		c.JSON(http.StatusOK, similarVideoPageResponse{Items: []videoResultResponse{}, SemanticAvailable: false})
		return
	}
	page, err := h.similarVideos.Search(ctx, applicationsearch.SimilarVideoRequest{
		SourceVideoID: videoID, Cursor: c.Query("cursor"), Limit: parseLimit(c),
	})
	if err != nil {
		writeSimilarVideoError(c, err)
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
	c.JSON(http.StatusOK, similarVideoPageResponse{
		Items: items, NextCursor: page.NextCursor,
		HasMore: page.HasMore, SemanticAvailable: page.SemanticAvailable,
	})
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
			ID: item.ID, Nickname: item.Nickname,
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
	case errors.Is(err, domainsearch.ErrEmptyQuery):
		interfaceshttpapierror.Write(c, http.StatusBadRequest, interfaceshttpapierror.CodeSearchQueryRequired, "请输入搜索关键词")
	case errors.Is(err, domainsearch.ErrInvalidQuery):
		interfaceshttpapierror.Write(c, http.StatusBadRequest, interfaceshttpapierror.CodeSearchQueryInvalid, "搜索关键词格式无效")
	case errors.Is(err, domainsearch.ErrQueryTooLong):
		interfaceshttpapierror.Write(c, http.StatusBadRequest, interfaceshttpapierror.CodeSearchQueryTooLong, "搜索关键词不能超过 64 个字符")
	case errors.Is(err, domainsearch.ErrInvalidLimit),
		errors.Is(err, domainsearch.ErrInvalidCursor):
		interfaceshttpapierror.Write(c, http.StatusBadRequest, interfaceshttpapierror.CodeSearchParametersInvalid, "搜索参数已失效，请重新搜索")
	case errors.Is(err, applicationsearch.ErrSemanticContinuationUnavailable):
		interfaceshttpapierror.Write(c, http.StatusServiceUnavailable, interfaceshttpapierror.CodeSearchServiceUnavailable, "语义搜索暂时不可用，请稍后重试")
	default:
		interfaceshttpapierror.Write(c, http.StatusInternalServerError, interfaceshttpapierror.CodeSearchServiceUnavailable, "搜索服务暂时不可用，请稍后重试")
	}
}

func writeSimilarVideoError(c *app.RequestContext, err error) {
	switch {
	case errors.Is(err, domainvideo.ErrVideoNotFound):
		interfaceshttpapierror.Write(c, http.StatusNotFound, interfaceshttpapierror.CodeVideoNotFound, "video not found")
	case errors.Is(err, domainsearch.ErrInvalidLimit), errors.Is(err, domainsearch.ErrInvalidCursor),
		errors.Is(err, applicationsearch.ErrInvalidHybridSearchConfig):
		interfaceshttpapierror.Write(c, http.StatusBadRequest, interfaceshttpapierror.CodeSearchParametersInvalid, "搜索参数已失效，请重新搜索")
	case errors.Is(err, applicationsearch.ErrSemanticContinuationUnavailable),
		errors.Is(err, applicationsearch.ErrSemanticVideoUnavailable):
		interfaceshttpapierror.Write(c, http.StatusServiceUnavailable, interfaceshttpapierror.CodeSearchServiceUnavailable, "语义相似视频暂时不可用，请稍后重试")
	default:
		interfaceshttpapierror.Write(c, http.StatusInternalServerError, interfaceshttpapierror.CodeSearchServiceUnavailable, "搜索服务暂时不可用，请稍后重试")
	}
}
