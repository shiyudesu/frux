package interfaceshttpreview

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	applicationreview "github.com/shiyudesu/frux/internal/application/review"
	domainreview "github.com/shiyudesu/frux/internal/domain/review"
	interfaceshttpapierror "github.com/shiyudesu/frux/internal/interfaces/http/apierror"
	interfaceshttpbinding "github.com/shiyudesu/frux/internal/interfaces/http/binding"
	interfaceshttpmiddleware "github.com/shiyudesu/frux/internal/interfaces/http/middleware"

	"github.com/cloudwego/hertz/pkg/app"
)

const maxMachineResultBodyBytes = 32 << 10
const maxHumanReviewBodyBytes = 8 << 10

type Handler struct {
	service  *applicationreview.Service
	observer applicationreview.Observer
}

func (h *Handler) ListHumanQueue(ctx context.Context, c *app.RequestContext) {
	principal, ok := interfaceshttpmiddleware.AdminPrincipalFromContext(c)
	if !ok {
		interfaceshttpapierror.Write(c, http.StatusForbidden, interfaceshttpapierror.CodeAdminPermissionDenied, "admin permission denied")
		return
	}
	scope, err := parseHumanQueueScope(c.Query("scope"))
	if err != nil {
		writeHumanReviewError(c, err)
		return
	}
	minPriority, err := parseOptionalPriority(c.Query("min_priority"), 0)
	if err != nil {
		writeHumanReviewError(c, err)
		return
	}
	maxPriority, err := parseOptionalPriority(c.Query("max_priority"), 100)
	if err != nil {
		writeHumanReviewError(c, err)
		return
	}
	limit, err := parseOptionalPositiveInt(c.Query("limit"), applicationreview.DefaultHumanQueueLimit)
	if err != nil {
		writeHumanReviewError(c, err)
		return
	}
	page, err := h.service.ListHumanQueue(ctx, applicationreview.HumanQueueRequest{
		MinPriority: minPriority, MaxPriority: maxPriority, Scope: scope,
		ReviewerID: principal.UserID,
		Cursor:     strings.TrimSpace(c.Query("cursor")), Limit: limit,
	})
	if err != nil {
		writeHumanReviewError(c, err)
		return
	}
	response := humanQueueResponse{
		Items:      make([]humanQueueItemResponse, 0, len(page.Items)),
		NextCursor: page.NextCursor, HasMore: page.HasMore, Scope: scope,
	}
	for _, item := range page.Items {
		response.Items = append(response.Items, humanQueueItemResponse{
			Case: humanCaseResponseFromDomain(item.Case), AuthorID: item.AuthorID,
			Title: item.Title, MediaURL: item.MediaURL, CoverURL: item.CoverURL,
		})
	}
	c.JSON(http.StatusOK, response)
}

func (h *Handler) GetHumanPreview(ctx context.Context, c *app.RequestContext) {
	caseID, ok := parseCaseID(c)
	if !ok {
		return
	}
	access, err := h.service.GetHumanPreview(ctx, caseID)
	if err != nil {
		writeHumanReviewError(c, err)
		return
	}
	c.JSON(http.StatusOK, humanPreviewResponse{
		MediaURL: access.MediaURL, CoverURL: access.CoverURL,
		ExpiresAt: access.ExpiresAt, ServerTime: access.ServerTime,
	})
}

func (h *Handler) GetHumanCase(ctx context.Context, c *app.RequestContext) {
	caseID, ok := parseCaseID(c)
	if !ok {
		return
	}
	detail, err := h.service.GetHumanCase(ctx, caseID)
	if err != nil {
		writeHumanReviewError(c, err)
		return
	}
	c.JSON(http.StatusOK, humanCaseDetailResponseFromDomain(detail))
}

func (h *Handler) ClaimHumanCase(ctx context.Context, c *app.RequestContext) {
	caseID, ok := parseCaseID(c)
	if !ok {
		return
	}
	principal, ok := interfaceshttpmiddleware.AdminPrincipalFromContext(c)
	if !ok {
		interfaceshttpapierror.Write(c, http.StatusForbidden, interfaceshttpapierror.CodeAdminPermissionDenied, "admin permission denied")
		return
	}
	var request humanClaimRequest
	if err := interfaceshttpbinding.BindStrictJSON(c, &request, maxHumanReviewBodyBytes); err != nil {
		interfaceshttpapierror.WriteInvalidRequest(c)
		return
	}
	result, err := h.service.ClaimHumanCase(ctx, applicationreview.ClaimRequest{
		CaseID: caseID, ReviewerID: principal.UserID, ExpectedCaseVersion: request.ExpectedCaseVersion,
	})
	if err != nil {
		writeHumanReviewError(c, err)
		return
	}
	c.JSON(http.StatusOK, humanLeaseResponse{
		Case: humanCaseResponseFromDomain(result.Case), LeaseToken: result.LeaseToken,
		ServerTime: result.Case.UpdatedAt,
	})
}

func (h *Handler) RenewHumanLease(ctx context.Context, c *app.RequestContext) {
	caseID, ok := parseCaseID(c)
	if !ok {
		return
	}
	principal, ok := interfaceshttpmiddleware.AdminPrincipalFromContext(c)
	if !ok {
		interfaceshttpapierror.Write(c, http.StatusForbidden, interfaceshttpapierror.CodeAdminPermissionDenied, "admin permission denied")
		return
	}
	var request humanLeaseRequest
	if err := interfaceshttpbinding.BindStrictJSON(c, &request, maxHumanReviewBodyBytes); err != nil {
		interfaceshttpapierror.WriteInvalidRequest(c)
		return
	}
	result, err := h.service.RenewHumanLease(ctx, applicationreview.RenewLeaseRequest{
		CaseID: caseID, ReviewerID: principal.UserID, LeaseToken: request.LeaseToken,
		ExpectedCaseVersion: request.ExpectedCaseVersion,
	})
	if err != nil {
		writeHumanReviewError(c, err)
		return
	}
	c.JSON(http.StatusOK, humanLeaseResponse{
		Case: humanCaseResponseFromDomain(result.Case), LeaseToken: result.LeaseToken,
		ServerTime: result.Case.UpdatedAt,
	})
}

func (h *Handler) ResumeHumanLease(ctx context.Context, c *app.RequestContext) {
	caseID, ok := parseCaseID(c)
	if !ok {
		return
	}
	principal, ok := interfaceshttpmiddleware.AdminPrincipalFromContext(c)
	if !ok {
		interfaceshttpapierror.Write(c, http.StatusForbidden, interfaceshttpapierror.CodeAdminPermissionDenied, "admin permission denied")
		return
	}
	var request humanClaimRequest
	if err := interfaceshttpbinding.BindStrictJSON(c, &request, maxHumanReviewBodyBytes); err != nil {
		interfaceshttpapierror.WriteInvalidRequest(c)
		return
	}
	result, err := h.service.ResumeHumanLease(ctx, applicationreview.ResumeLeaseRequest{
		CaseID: caseID, ReviewerID: principal.UserID, ExpectedCaseVersion: request.ExpectedCaseVersion,
	})
	if err != nil {
		writeHumanReviewError(c, err)
		return
	}
	c.JSON(http.StatusOK, humanLeaseResponse{
		Case: humanCaseResponseFromDomain(result.Case), LeaseToken: result.LeaseToken,
		ServerTime: result.Case.UpdatedAt,
	})
}

func (h *Handler) ReleaseHumanLease(ctx context.Context, c *app.RequestContext) {
	caseID, ok := parseCaseID(c)
	if !ok {
		return
	}
	principal, ok := interfaceshttpmiddleware.AdminPrincipalFromContext(c)
	if !ok {
		interfaceshttpapierror.Write(c, http.StatusForbidden, interfaceshttpapierror.CodeAdminPermissionDenied, "admin permission denied")
		return
	}
	var request humanLeaseRequest
	if err := interfaceshttpbinding.BindStrictJSON(c, &request, maxHumanReviewBodyBytes); err != nil {
		interfaceshttpapierror.WriteInvalidRequest(c)
		return
	}
	reviewCase, err := h.service.ReleaseHumanLease(ctx, applicationreview.ReleaseLeaseRequest{
		CaseID: caseID, ReviewerID: principal.UserID, LeaseToken: request.LeaseToken,
		ExpectedCaseVersion: request.ExpectedCaseVersion,
	})
	if err != nil {
		writeHumanReviewError(c, err)
		return
	}
	c.JSON(http.StatusOK, humanCaseResponseFromDomain(reviewCase))
}

func (h *Handler) DecideHumanCase(ctx context.Context, c *app.RequestContext) {
	caseID, ok := parseCaseID(c)
	if !ok {
		return
	}
	principal, ok := interfaceshttpmiddleware.AdminPrincipalFromContext(c)
	if !ok {
		interfaceshttpapierror.Write(c, http.StatusForbidden, interfaceshttpapierror.CodeAdminPermissionDenied, "admin permission denied")
		return
	}
	var request humanDecisionRequest
	if err := interfaceshttpbinding.BindStrictJSON(c, &request, maxHumanReviewBodyBytes); err != nil {
		interfaceshttpapierror.WriteInvalidRequest(c)
		return
	}
	result, err := h.service.DecideHumanCase(ctx, applicationreview.DecisionRequest{
		CaseID: caseID, ReviewerID: principal.UserID, LeaseToken: request.LeaseToken,
		ExpectedCaseVersion: request.ExpectedCaseVersion, ReviewVersion: request.ReviewVersion,
		Outcome: request.Outcome, ReasonCode: request.ReasonCode, Note: request.Note,
		IdempotencyKey: strings.TrimSpace(string(c.GetHeader("Idempotency-Key"))),
	})
	if err != nil {
		writeHumanReviewError(c, err)
		return
	}
	c.JSON(http.StatusOK, humanDecisionResultResponse{
		Case:     humanCaseResponseFromDomain(result.Case),
		Decision: humanDecisionResponseFromDomain(result.Decision), Duplicate: result.Duplicate,
	})
}

func New(service *applicationreview.Service, observer applicationreview.Observer) *Handler {
	return &Handler{service: service, observer: observer}
}

func (h *Handler) PutMachineResult(ctx context.Context, c *app.RequestContext) {
	caseID, err := strconv.ParseInt(strings.TrimSpace(c.Param("caseId")), 10, 64)
	if err != nil || caseID <= 0 {
		h.invalid(c)
		return
	}
	resultID := strings.TrimSpace(c.Param("resultId"))
	if resultID == "" || len(resultID) > domainreview.MaxResultIdentityLength {
		h.invalid(c)
		return
	}
	var request machineResultRequest
	if err := interfaceshttpbinding.BindStrictJSON(c, &request, maxMachineResultBodyBytes); err != nil {
		h.invalid(c)
		return
	}
	signals := make([]domainreview.MachineSignal, 0, len(request.Signals))
	for _, signal := range request.Signals {
		signals = append(signals, domainreview.MachineSignal{
			Label: signal.Label, Confidence: signal.Confidence, EvidenceRefs: signal.EvidenceRefs,
		})
	}
	result, err := h.service.SubmitMachineResult(ctx, domainreview.MachineResultInput{
		CaseID: caseID, VideoID: request.VideoID, ReviewVersion: request.ReviewVersion,
		ResultID: resultID, Provider: request.Provider, ModelVersion: request.ModelVersion,
		SourceKind: request.SourceKind, GeneratedAt: request.GeneratedAt,
		RolloutMode: request.RolloutMode, PolicyVersion: request.PolicyVersion,
		Signals: signals, ReceivedAt: time.Now().UTC(),
	})
	if err != nil {
		writeReviewError(c, err)
		return
	}
	c.JSON(http.StatusOK, machineResultResponse{
		CaseID: result.Case.ID, Status: result.Case.Status, Outcome: result.Decision.Outcome,
		PolicyVersion: result.Decision.PolicyVersion, RolloutMode: result.Decision.RolloutMode,
		Duplicate: result.Duplicate,
	})
}

func (h *Handler) invalid(c *app.RequestContext) {
	if h.observer != nil {
		h.observer.Observe("provider_result", "invalid")
	}
	interfaceshttpapierror.Write(c, http.StatusBadRequest, interfaceshttpapierror.CodeReviewValidationFailed, "invalid review result")
}

func writeReviewError(c *app.RequestContext, err error) {
	switch {
	case errors.Is(err, domainreview.ErrInvalidCaseID),
		errors.Is(err, domainreview.ErrInvalidVideoID),
		errors.Is(err, domainreview.ErrInvalidReviewVersion),
		errors.Is(err, domainreview.ErrInvalidPolicyVersion),
		errors.Is(err, domainreview.ErrInvalidResultIdentity),
		errors.Is(err, domainreview.ErrInvalidProvider),
		errors.Is(err, domainreview.ErrInvalidModelVersion),
		errors.Is(err, domainreview.ErrInvalidMachineSource),
		errors.Is(err, domainreview.ErrInvalidGeneratedAt),
		errors.Is(err, domainreview.ErrInvalidModerationMode),
		errors.Is(err, domainreview.ErrInvalidSignal),
		errors.Is(err, domainreview.ErrInvalidConfidence),
		errors.Is(err, domainreview.ErrTooManySignals),
		errors.Is(err, domainreview.ErrTooManyEvidenceRefs),
		errors.Is(err, domainreview.ErrEvidenceRefTooLong),
		errors.Is(err, domainreview.ErrEvidenceTooLarge):
		interfaceshttpapierror.Write(c, http.StatusBadRequest, interfaceshttpapierror.CodeReviewValidationFailed, "invalid review result")
	case errors.Is(err, domainreview.ErrReviewCaseNotFound):
		interfaceshttpapierror.Write(c, http.StatusNotFound, interfaceshttpapierror.CodeReviewCaseNotFound, "review case not found")
	case errors.Is(err, domainreview.ErrResultIdentityConflict),
		errors.Is(err, domainreview.ErrModerationJobNotOwned),
		errors.Is(err, domainreview.ErrReviewCaseNotOpen),
		errors.Is(err, domainreview.ErrReviewSubjectStale),
		errors.Is(err, domainreview.ErrReviewSubjectState),
		errors.Is(err, domainreview.ErrReviewSubjectNotReady):
		interfaceshttpapierror.Write(c, http.StatusConflict, interfaceshttpapierror.CodeReviewConflict, "review result conflict")
	default:
		interfaceshttpapierror.WriteServiceUnavailableCode(c, interfaceshttpapierror.CodeReviewUnavailable, "review unavailable", err)
	}
}
