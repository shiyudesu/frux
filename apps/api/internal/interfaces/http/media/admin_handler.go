package interfaceshttpmedia

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	applicationmedia "github.com/shiyudesu/frux/internal/application/media"
	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
	interfaceshttpapierror "github.com/shiyudesu/frux/internal/interfaces/http/apierror"
	interfaceshttpbinding "github.com/shiyudesu/frux/internal/interfaces/http/binding"
	interfaceshttpmiddleware "github.com/shiyudesu/frux/internal/interfaces/http/middleware"

	"github.com/cloudwego/hertz/pkg/app"
)

const maxAdminMediaRetryBody = 16 << 10

type AdminHandler struct {
	service *applicationmedia.AdminProcessingService
}

type adminProcessingSummaryResponse struct {
	Waiting         int64      `json:"waiting"`
	Processing      int64      `json:"processing"`
	Failed          int64      `json:"failed"`
	Completed       int64      `json:"completed"`
	OldestWaitingAt *time.Time `json:"oldest_waiting_at,omitempty"`
}

type adminProcessingItemResponse struct {
	JobID             int64      `json:"job_id"`
	VideoID           int64      `json:"video_id,omitempty"`
	AuthorID          int64      `json:"author_id,omitempty"`
	Title             string     `json:"title"`
	ProfileVersion    string     `json:"profile_version"`
	State             string     `json:"state"`
	Stage             string     `json:"stage"`
	StageProgressBPS  *int       `json:"stage_progress_bps"`
	Attempts          int        `json:"attempts"`
	MaxAttempts       int        `json:"max_attempts"`
	ErrorCode         string     `json:"error_code"`
	ErrorMessage      string     `json:"error_message"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
	ProgressUpdatedAt *time.Time `json:"progress_updated_at,omitempty"`
	NextAttemptAt     time.Time  `json:"next_attempt_at"`
	CompletedAt       *time.Time `json:"completed_at,omitempty"`
}

type adminProcessingOverviewResponse struct {
	Summary     adminProcessingSummaryResponse `json:"summary"`
	ActiveItems []adminProcessingItemResponse  `json:"active_items"`
	RefreshedAt time.Time                      `json:"refreshed_at"`
}

type adminProcessingHistoryResponse struct {
	Items      []adminProcessingItemResponse `json:"items"`
	NextCursor string                        `json:"next_cursor"`
	HasMore    bool                          `json:"has_more"`
}

type adminProcessingRetryRequest struct {
	ReasonCode string `json:"reason_code"`
	Note       string `json:"note"`
}

type adminProcessingBulkRetryRequest struct {
	JobIDs     []int64 `json:"job_ids"`
	ReasonCode string  `json:"reason_code"`
	Note       string  `json:"note"`
}

type adminProcessingRetryResponse struct {
	Item           adminProcessingItemResponse `json:"item"`
	AuditCommitted bool                        `json:"audit_committed"`
	Replayed       bool                        `json:"replayed"`
}

type adminProcessingBulkItemResponse struct {
	JobID     int64                        `json:"job_id"`
	Status    string                       `json:"status"`
	Item      *adminProcessingItemResponse `json:"item,omitempty"`
	ErrorCode string                       `json:"error_code,omitempty"`
}

type adminProcessingBulkResponse struct {
	Items []adminProcessingBulkItemResponse `json:"items"`
}

func NewAdmin(service *applicationmedia.AdminProcessingService) *AdminHandler {
	return &AdminHandler{service: service}
}

func (h *AdminHandler) Overview(ctx context.Context, c *app.RequestContext) {
	result, err := h.service.Overview(ctx)
	if err != nil {
		writeAdminProcessingError(c, err)
		return
	}
	response := adminProcessingOverviewResponse{
		Summary: adminProcessingSummaryResponse{
			Waiting: result.Summary.Waiting, Processing: result.Summary.Processing,
			Failed: result.Summary.Failed, Completed: result.Summary.Completed,
			OldestWaitingAt: result.Summary.OldestWaitingAt,
		},
		ActiveItems: make([]adminProcessingItemResponse, 0, len(result.ActiveItems)),
		RefreshedAt: result.RefreshedAt,
	}
	for _, item := range result.ActiveItems {
		response.ActiveItems = append(response.ActiveItems, processingItemResponse(item))
	}
	c.JSON(http.StatusOK, response)
}

func (h *AdminHandler) History(ctx context.Context, c *app.RequestContext) {
	request, err := parseHistoryRequest(c)
	if err != nil {
		writeAdminProcessingError(c, err)
		return
	}
	result, err := h.service.History(ctx, request)
	if err != nil {
		writeAdminProcessingError(c, err)
		return
	}
	response := adminProcessingHistoryResponse{
		Items:      make([]adminProcessingItemResponse, 0, len(result.Items)),
		NextCursor: result.NextCursor, HasMore: result.HasMore,
	}
	for _, item := range result.Items {
		response.Items = append(response.Items, processingItemResponse(item))
	}
	c.JSON(http.StatusOK, response)
}

func (h *AdminHandler) Retry(ctx context.Context, c *app.RequestContext) {
	principal, ok := interfaceshttpmiddleware.AdminPrincipalFromContext(c)
	if !ok {
		interfaceshttpapierror.Write(
			c, http.StatusForbidden, interfaceshttpapierror.CodeAdminPermissionDenied,
			"admin permission denied",
		)
		return
	}
	jobID, err := strconv.ParseInt(strings.TrimSpace(c.Param("jobId")), 10, 64)
	if err != nil || jobID <= 0 {
		writeAdminProcessingError(c, domainmedia.ErrInvalidProcessingRetry)
		return
	}
	var body adminProcessingRetryRequest
	if err := interfaceshttpbinding.BindStrictJSON(c, &body, maxAdminMediaRetryBody); err != nil {
		interfaceshttpapierror.WriteInvalidRequest(c)
		return
	}
	result, err := h.service.Retry(ctx, applicationmedia.AdminProcessingRetryRequest{
		ActorID: principal.UserID, JobID: jobID,
		ReasonCode: body.ReasonCode, Note: body.Note,
		IdempotencyKey: strings.TrimSpace(string(c.GetHeader("Idempotency-Key"))),
	})
	if err != nil {
		writeAdminProcessingError(c, err)
		return
	}
	c.JSON(http.StatusOK, adminProcessingRetryResponse{
		Item:           processingItemResponse(result.Item),
		AuditCommitted: true, Replayed: result.Replayed,
	})
}

func (h *AdminHandler) BulkRetry(ctx context.Context, c *app.RequestContext) {
	principal, ok := interfaceshttpmiddleware.AdminPrincipalFromContext(c)
	if !ok {
		interfaceshttpapierror.Write(
			c, http.StatusForbidden, interfaceshttpapierror.CodeAdminPermissionDenied,
			"admin permission denied",
		)
		return
	}
	var body adminProcessingBulkRetryRequest
	if err := interfaceshttpbinding.BindStrictJSON(c, &body, maxAdminMediaRetryBody); err != nil {
		interfaceshttpapierror.WriteInvalidRequest(c)
		return
	}
	results, err := h.service.BulkRetry(ctx, applicationmedia.AdminProcessingBulkRetryRequest{
		ActorID: principal.UserID, JobIDs: body.JobIDs,
		ReasonCode: body.ReasonCode, Note: body.Note,
		IdempotencyKey: strings.TrimSpace(string(c.GetHeader("Idempotency-Key"))),
	})
	if err != nil {
		writeAdminProcessingError(c, err)
		return
	}
	response := adminProcessingBulkResponse{
		Items: make([]adminProcessingBulkItemResponse, 0, len(results)),
	}
	for _, result := range results {
		item := adminProcessingBulkItemResponse{
			JobID: result.JobID, Status: result.Status, ErrorCode: result.ErrorCode,
		}
		if result.Item != nil {
			value := processingItemResponse(result.Item)
			item.Item = &value
		}
		response.Items = append(response.Items, item)
	}
	c.JSON(http.StatusOK, response)
}

func parseHistoryRequest(
	c *app.RequestContext,
) (applicationmedia.AdminProcessingHistoryRequest, error) {
	videoID, err := parseOptionalID(c.Query("video_id"))
	if err != nil {
		return applicationmedia.AdminProcessingHistoryRequest{}, err
	}
	limit := 0
	if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
		limit, err = strconv.Atoi(raw)
		if err != nil {
			return applicationmedia.AdminProcessingHistoryRequest{}, domainmedia.ErrInvalidProcessingAdminQuery
		}
	}
	from, to, err := parseOptionalRange(
		c.Query("completed_from"), c.Query("completed_to"),
	)
	if err != nil {
		return applicationmedia.AdminProcessingHistoryRequest{}, err
	}
	return applicationmedia.AdminProcessingHistoryRequest{
		State: c.Query("state"), Step: c.Query("stage"),
		ErrorCode: c.Query("error_code"), VideoID: videoID,
		CompletedFrom: from, CompletedTo: to,
		Cursor: c.Query("cursor"), Limit: limit,
	}, nil
}

func parseOptionalID(raw string) (int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value <= 0 {
		return 0, domainmedia.ErrInvalidProcessingAdminQuery
	}
	return value, nil
}

func parseOptionalRange(fromRaw, toRaw string) (*time.Time, *time.Time, error) {
	fromRaw, toRaw = strings.TrimSpace(fromRaw), strings.TrimSpace(toRaw)
	if fromRaw == "" && toRaw == "" {
		return nil, nil, nil
	}
	if fromRaw == "" || toRaw == "" {
		return nil, nil, domainmedia.ErrInvalidProcessingAdminQuery
	}
	from, err := time.Parse(time.RFC3339, fromRaw)
	if err != nil {
		return nil, nil, domainmedia.ErrInvalidProcessingAdminQuery
	}
	to, err := time.Parse(time.RFC3339, toRaw)
	if err != nil {
		return nil, nil, domainmedia.ErrInvalidProcessingAdminQuery
	}
	return &from, &to, nil
}

func processingItemResponse(
	item *applicationmedia.AdminProcessingItem,
) adminProcessingItemResponse {
	job := item.Job
	return adminProcessingItemResponse{
		JobID: job.ID, VideoID: item.VideoID, AuthorID: item.AuthorID, Title: item.Title,
		ProfileVersion: job.ProfileVersion, State: job.State, Stage: job.ProcessingStep,
		StageProgressBPS: job.ProgressBPS, Attempts: job.Attempts,
		MaxAttempts: job.MaxAttempts, ErrorCode: job.ErrorCode,
		ErrorMessage: safeDiagnostic(job.ErrorMessage),
		CreatedAt:    job.CreatedAt, UpdatedAt: job.UpdatedAt,
		ProgressUpdatedAt: job.ProgressUpdatedAt, NextAttemptAt: job.NextAttemptAt,
		CompletedAt: job.CompletedAt,
	}
}

func safeDiagnostic(value string) string {
	fields := strings.Fields(strings.TrimSpace(value))
	for index, field := range fields {
		if strings.Contains(field, "://") ||
			strings.Contains(field, "/") ||
			strings.Contains(field, "\\") {
			fields[index] = "[path]"
		}
	}
	result := strings.Join(fields, " ")
	if len(result) > 512 {
		return result[len(result)-512:]
	}
	return result
}

func writeAdminProcessingError(c *app.RequestContext, err error) {
	switch {
	case errors.Is(err, domainmedia.ErrInvalidProcessingAdminCursor):
		interfaceshttpapierror.Write(
			c, http.StatusBadRequest,
			interfaceshttpapierror.CodeAdminMediaProcessingCursorInvalid,
			"invalid media processing cursor",
		)
	case errors.Is(err, domainmedia.ErrInvalidProcessingAdminQuery),
		errors.Is(err, domainmedia.ErrInvalidProcessingRetry):
		interfaceshttpapierror.Write(
			c, http.StatusBadRequest,
			interfaceshttpapierror.CodeAdminMediaProcessingValidationFailed,
			"invalid media processing request",
		)
	case errors.Is(err, domainmedia.ErrProcessingJobNotFound):
		interfaceshttpapierror.Write(
			c, http.StatusNotFound,
			interfaceshttpapierror.CodeAdminMediaProcessingJobNotFound,
			"media processing job not found",
		)
	case errors.Is(err, domainmedia.ErrProcessingRetryIdempotencyConflict):
		interfaceshttpapierror.Write(
			c, http.StatusConflict,
			interfaceshttpapierror.CodeAdminMediaProcessingIdempotencyConflict,
			"media processing idempotency conflict",
		)
	case errors.Is(err, domainmedia.ErrProcessingRetryConflict):
		interfaceshttpapierror.Write(
			c, http.StatusConflict,
			interfaceshttpapierror.CodeAdminMediaProcessingRetryConflict,
			"media processing retry conflict",
		)
	default:
		interfaceshttpapierror.WriteServiceUnavailableCode(
			c, interfaceshttpapierror.CodeAdminMediaProcessingUnavailable,
			"media processing operations unavailable", err,
		)
	}
}
