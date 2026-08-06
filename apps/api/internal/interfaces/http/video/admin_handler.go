package interfaceshttpvideo

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	applicationvideo "github.com/shiyudesu/frux/internal/application/video"
	domainvideo "github.com/shiyudesu/frux/internal/domain/video"
	interfaceshttpapierror "github.com/shiyudesu/frux/internal/interfaces/http/apierror"
	interfaceshttpbinding "github.com/shiyudesu/frux/internal/interfaces/http/binding"
	interfaceshttpmiddleware "github.com/shiyudesu/frux/internal/interfaces/http/middleware"

	"github.com/cloudwego/hertz/pkg/app"
)

const maxAdminVideoBodyBytes = 8 << 10

type AdminHandler struct {
	service *applicationvideo.AdminService
}

type adminVideoResponse struct {
	ID            int64      `json:"id"`
	AuthorID      int64      `json:"author_id"`
	Title         string     `json:"title"`
	Description   string     `json:"description"`
	MediaURL      string     `json:"media_url"`
	CoverURL      string     `json:"cover_url"`
	Status        int        `json:"status"`
	StatusName    string     `json:"status_name"`
	Visibility    string     `json:"visibility"`
	MediaStatus   string     `json:"media_status"`
	ReviewVersion int        `json:"review_version"`
	Version       int        `json:"version"`
	PublishedAt   *time.Time `json:"published_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

type adminVideoListResponse struct {
	Items      []adminVideoResponse `json:"items"`
	NextCursor string               `json:"next_cursor"`
	HasMore    bool                 `json:"has_more"`
}

type adminEnforcementRequest struct {
	ReasonCode      string `json:"reason_code"`
	Note            string `json:"note"`
	ExpectedVersion int    `json:"expected_version"`
}

type adminTransitionResponse struct {
	Video          adminVideoResponse `json:"video"`
	PreviousStatus string             `json:"previous_status"`
	AuditCommitted bool               `json:"audit_committed"`
}

func NewAdmin(service *applicationvideo.AdminService) *AdminHandler {
	return &AdminHandler{service: service}
}

func (h *AdminHandler) Search(ctx context.Context, c *app.RequestContext) {
	request, err := parseAdminVideoSearch(c)
	if err != nil {
		writeAdminVideoError(c, err)
		return
	}
	page, err := h.service.Search(ctx, request)
	if err != nil {
		writeAdminVideoError(c, err)
		return
	}
	response := adminVideoListResponse{
		Items:      make([]adminVideoResponse, 0, len(page.Items)),
		NextCursor: page.NextCursor, HasMore: page.HasMore,
	}
	for _, video := range page.Items {
		response.Items = append(response.Items, adminVideoResponseFromDomain(video))
	}
	c.JSON(http.StatusOK, response)
}

func (h *AdminHandler) TakeDown(ctx context.Context, c *app.RequestContext) {
	h.transition(ctx, c, false)
}

func (h *AdminHandler) Restore(ctx context.Context, c *app.RequestContext) {
	h.transition(ctx, c, true)
}

func (h *AdminHandler) transition(ctx context.Context, c *app.RequestContext, restore bool) {
	videoID, err := parsePositiveInt64(c.Param("videoId"))
	if err != nil {
		writeAdminVideoError(c, err)
		return
	}
	principal, ok := interfaceshttpmiddleware.AdminPrincipalFromContext(c)
	if !ok {
		interfaceshttpapierror.Write(
			c, http.StatusForbidden, interfaceshttpapierror.CodeAdminPermissionDenied,
			"admin permission denied",
		)
		return
	}
	var request adminEnforcementRequest
	if err := interfaceshttpbinding.BindStrictJSON(c, &request, maxAdminVideoBodyBytes); err != nil {
		interfaceshttpapierror.WriteInvalidRequest(c)
		return
	}
	input := applicationvideo.AdminEnforcementRequest{
		VideoID: videoID, ActorID: principal.UserID, ExpectedVersion: request.ExpectedVersion,
		ReasonCode: request.ReasonCode, Note: request.Note,
	}
	var result *domainvideo.AdminTransitionResult
	if restore {
		result, err = h.service.Restore(ctx, input)
	} else {
		result, err = h.service.TakeDown(ctx, input)
	}
	if err != nil {
		writeAdminVideoError(c, err)
		return
	}
	c.JSON(http.StatusOK, adminTransitionResponse{
		Video:          adminVideoResponseFromDomain(result.Video),
		PreviousStatus: videoStatusName(result.PreviousStatus),
		AuditCommitted: true,
	})
}

func parseAdminVideoSearch(c *app.RequestContext) (applicationvideo.AdminVideoSearchRequest, error) {
	status, err := parseAdminVideoStatus(c.Query("status"))
	if err != nil {
		return applicationvideo.AdminVideoSearchRequest{}, err
	}
	authorID, err := parseOptionalAdminID(c.Query("author_id"))
	if err != nil {
		return applicationvideo.AdminVideoSearchRequest{}, err
	}
	videoID, err := parseOptionalAdminID(c.Query("video_id"))
	if err != nil {
		return applicationvideo.AdminVideoSearchRequest{}, err
	}
	limit := 0
	if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
		limit, err = strconv.Atoi(raw)
		if err != nil {
			return applicationvideo.AdminVideoSearchRequest{}, domainvideo.ErrInvalidLimit
		}
	}
	from, to, err := parseAdminDateRange(c.Query("created_from"), c.Query("created_to"))
	if err != nil {
		return applicationvideo.AdminVideoSearchRequest{}, err
	}
	return applicationvideo.AdminVideoSearchRequest{
		Status: status, AuthorID: authorID, VideoID: videoID,
		Keyword: c.Query("keyword"), CreatedFrom: from, CreatedTo: to,
		Cursor: c.Query("cursor"), Limit: limit,
	}, nil
}

func parseAdminVideoStatus(raw string) (int, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "":
		return 0, nil
	case "draft":
		return domainvideo.StatusDraft, nil
	case "published":
		return domainvideo.StatusPublished, nil
	case "offline":
		return domainvideo.StatusOffline, nil
	case "pending_review":
		return domainvideo.StatusPendingReview, nil
	case "rejected":
		return domainvideo.StatusRejected, nil
	default:
		return 0, domainvideo.ErrInvalidStatus
	}
}

func parseOptionalAdminID(raw string) (int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value <= 0 {
		return 0, domainvideo.ErrInvalidVideoID
	}
	return value, nil
}

func parseAdminDateRange(fromRaw, toRaw string) (*time.Time, *time.Time, error) {
	fromRaw, toRaw = strings.TrimSpace(fromRaw), strings.TrimSpace(toRaw)
	if fromRaw == "" && toRaw == "" {
		return nil, nil, nil
	}
	if fromRaw == "" || toRaw == "" {
		return nil, nil, domainvideo.ErrInvalidDateRange
	}
	from, err := time.Parse(time.RFC3339, fromRaw)
	if err != nil {
		return nil, nil, domainvideo.ErrInvalidDateRange
	}
	to, err := time.Parse(time.RFC3339, toRaw)
	if err != nil {
		return nil, nil, domainvideo.ErrInvalidDateRange
	}
	return &from, &to, nil
}

func adminVideoResponseFromDomain(video *domainvideo.Video) adminVideoResponse {
	return adminVideoResponse{
		ID: video.ID, AuthorID: video.AuthorID, Title: video.Title, Description: video.Description,
		MediaURL: video.MediaURL, CoverURL: video.CoverURL, Status: video.Status,
		StatusName: videoStatusName(video.Status), Visibility: video.Visibility,
		MediaStatus: video.MediaStatus, ReviewVersion: video.ReviewVersion, Version: video.Version,
		PublishedAt: video.PublishedAt, CreatedAt: video.CreatedAt, UpdatedAt: video.UpdatedAt,
	}
}

func videoStatusName(status int) string {
	switch status {
	case domainvideo.StatusDraft:
		return "draft"
	case domainvideo.StatusPublished:
		return "published"
	case domainvideo.StatusOffline:
		return "offline"
	case domainvideo.StatusDeleted:
		return "deleted"
	case domainvideo.StatusPendingReview:
		return "pending_review"
	case domainvideo.StatusRejected:
		return "rejected"
	default:
		return "unknown"
	}
}

func writeAdminVideoError(c *app.RequestContext, err error) {
	switch {
	case errors.Is(err, domainvideo.ErrInvalidCursor):
		interfaceshttpapierror.Write(
			c, http.StatusBadRequest, interfaceshttpapierror.CodeAdminVideoCursorInvalid,
			"invalid admin video cursor",
		)
	case errors.Is(err, domainvideo.ErrInvalidVideoID),
		errors.Is(err, domainvideo.ErrInvalidAuthorID),
		errors.Is(err, domainvideo.ErrInvalidStatus),
		errors.Is(err, domainvideo.ErrInvalidDateRange),
		errors.Is(err, domainvideo.ErrInvalidLimit),
		errors.Is(err, domainvideo.ErrAdminQueryInvalid),
		errors.Is(err, domainvideo.ErrInvalidExpectedVersion),
		errors.Is(err, domainvideo.ErrInvalidEnforcementReason),
		errors.Is(err, domainvideo.ErrEnforcementNoteTooLong):
		interfaceshttpapierror.Write(
			c, http.StatusBadRequest, interfaceshttpapierror.CodeAdminVideoValidationFailed,
			"invalid admin video request",
		)
	case errors.Is(err, domainvideo.ErrVideoNotFound):
		interfaceshttpapierror.Write(
			c, http.StatusNotFound, interfaceshttpapierror.CodeVideoNotFound, "video not found",
		)
	case errors.Is(err, domainvideo.ErrVideoVersionConflict):
		interfaceshttpapierror.Write(
			c, http.StatusConflict, interfaceshttpapierror.CodeAdminVideoVersionConflict,
			"video version conflict",
		)
	case errors.Is(err, domainvideo.ErrVideoStateNotAllowed):
		interfaceshttpapierror.Write(
			c, http.StatusConflict, interfaceshttpapierror.CodeAdminVideoStateConflict,
			"video state conflict",
		)
	default:
		interfaceshttpapierror.WriteServiceUnavailableCode(
			c, interfaceshttpapierror.CodeAdminVideoUnavailable, "admin video unavailable", err,
		)
	}
}
