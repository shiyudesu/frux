package interfaceshttpembedding

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	applicationembedding "github.com/shiyudesu/frux/internal/application/embedding"
	domainembedding "github.com/shiyudesu/frux/internal/domain/embedding"
	interfaceshttpapierror "github.com/shiyudesu/frux/internal/interfaces/http/apierror"
	interfaceshttpbinding "github.com/shiyudesu/frux/internal/interfaces/http/binding"
	interfaceshttpmiddleware "github.com/shiyudesu/frux/internal/interfaces/http/middleware"

	"github.com/cloudwego/hertz/pkg/app"
)

const maxMultimodalRequeueBody = 4 << 10

type AdminHandler struct {
	service *applicationembedding.AdminMultimodalService
}

type adminMultimodalItemResponse struct {
	JobID                    int64      `json:"job_id"`
	State                    string     `json:"state"`
	Attempts                 int        `json:"attempts"`
	MaxAttempts              int        `json:"max_attempts"`
	FailureCode              string     `json:"failure_code,omitempty"`
	ProviderAlias            string     `json:"provider_alias"`
	ModelAlias               string     `json:"model_alias"`
	RevisionAlias            string     `json:"revision_alias"`
	Dimension                int        `json:"dimension"`
	TextCanonicalizer        string     `json:"text_canonicalizer"`
	FrameSamplingPolicy      string     `json:"frame_sampling_policy"`
	ImagePreprocessingPolicy string     `json:"image_preprocessing_policy"`
	FusionPolicy             string     `json:"fusion_policy"`
	NextAttemptAt            time.Time  `json:"next_attempt_at"`
	CreatedAt                time.Time  `json:"created_at"`
	UpdatedAt                time.Time  `json:"updated_at"`
	CompletedAt              *time.Time `json:"completed_at,omitempty"`
}

type adminMultimodalPageResponse struct {
	Items      []adminMultimodalItemResponse `json:"items"`
	NextCursor string                        `json:"next_cursor"`
	HasMore    bool                          `json:"has_more"`
}

type adminMultimodalRequeueRequest struct {
	ReasonCode string `json:"reason_code"`
}

func NewAdmin(service *applicationembedding.AdminMultimodalService) *AdminHandler {
	return &AdminHandler{service: service}
}

func (h *AdminHandler) List(ctx context.Context, c *app.RequestContext) {
	limit := 0
	if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			writeAdminMultimodalError(c, domainembedding.ErrInvalidMultimodalJob)
			return
		}
		limit = parsed
	}
	page, err := h.service.List(ctx, c.Query("state"), c.Query("cursor"), limit)
	if err != nil {
		writeAdminMultimodalError(c, err)
		return
	}
	response := adminMultimodalPageResponse{
		Items:      make([]adminMultimodalItemResponse, 0, len(page.Items)),
		NextCursor: page.NextCursor, HasMore: page.HasMore,
	}
	for _, item := range page.Items {
		response.Items = append(response.Items, adminMultimodalResponse(item))
	}
	c.JSON(http.StatusOK, response)
}

func (h *AdminHandler) Requeue(ctx context.Context, c *app.RequestContext) {
	principal, ok := interfaceshttpmiddleware.AdminPrincipalFromContext(c)
	if !ok {
		interfaceshttpapierror.Write(c, http.StatusForbidden, interfaceshttpapierror.CodeAdminPermissionDenied, "admin permission denied")
		return
	}
	jobID, err := strconv.ParseInt(strings.TrimSpace(c.Param("jobId")), 10, 64)
	if err != nil || jobID <= 0 {
		writeAdminMultimodalError(c, domainembedding.ErrInvalidMultimodalJob)
		return
	}
	var body adminMultimodalRequeueRequest
	if err := interfaceshttpbinding.BindStrictJSON(c, &body, maxMultimodalRequeueBody); err != nil {
		interfaceshttpapierror.WriteInvalidRequest(c)
		return
	}
	item, replayed, err := h.service.Requeue(ctx, applicationembedding.AdminMultimodalRequeueRequest{
		ActorID: principal.UserID, JobID: jobID, ReasonCode: body.ReasonCode,
		IdempotencyKey: strings.TrimSpace(string(c.GetHeader("Idempotency-Key"))),
	})
	if err != nil {
		writeAdminMultimodalError(c, err)
		return
	}
	c.JSON(http.StatusOK, map[string]any{
		"item": adminMultimodalResponse(item), "replayed": replayed, "audit_committed": true,
	})
}

func adminMultimodalResponse(item applicationembedding.AdminMultimodalJobItem) adminMultimodalItemResponse {
	return adminMultimodalItemResponse{
		JobID: item.JobID, State: item.State, Attempts: item.Attempts, MaxAttempts: item.MaxAttempts,
		FailureCode: item.FailureCode, ProviderAlias: item.ProviderAlias,
		ModelAlias: item.ModelAlias, RevisionAlias: item.RevisionAlias, Dimension: item.Dimension,
		TextCanonicalizer: item.TextCanonicalizer, FrameSamplingPolicy: item.FrameSamplingPolicy,
		ImagePreprocessingPolicy: item.ImagePreprocessingPolicy, FusionPolicy: item.FusionPolicy,
		NextAttemptAt: item.NextAttemptAt, CreatedAt: item.CreatedAt,
		UpdatedAt: item.UpdatedAt, CompletedAt: item.CompletedAt,
	}
}

func writeAdminMultimodalError(c *app.RequestContext, err error) {
	switch {
	case errors.Is(err, domainembedding.ErrInvalidMultimodalJob):
		interfaceshttpapierror.Write(c, http.StatusBadRequest, interfaceshttpapierror.CodeInvalidRequest, "invalid request")
	case errors.Is(err, domainembedding.ErrMultimodalJobNotFound):
		interfaceshttpapierror.Write(c, http.StatusNotFound, interfaceshttpapierror.CodeNotFound, "multimodal job not found")
	case errors.Is(err, domainembedding.ErrMultimodalOperationConflict):
		interfaceshttpapierror.Write(c, http.StatusConflict, interfaceshttpapierror.CodeConflict, "multimodal job cannot be requeued")
	default:
		interfaceshttpapierror.Write(c, http.StatusInternalServerError, interfaceshttpapierror.CodeInternal, "multimodal operation failed")
	}
}
