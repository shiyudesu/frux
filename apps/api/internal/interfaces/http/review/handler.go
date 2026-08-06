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

	"github.com/cloudwego/hertz/pkg/app"
)

const maxMachineResultBodyBytes = 32 << 10

type Handler struct {
	service  *applicationreview.Service
	observer applicationreview.Observer
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
		PolicyVersion: request.PolicyVersion, Signals: signals, ReceivedAt: time.Now().UTC(),
	})
	if err != nil {
		writeReviewError(c, err)
		return
	}
	c.JSON(http.StatusOK, machineResultResponse{
		CaseID: result.Case.ID, Status: result.Case.Status, Outcome: result.Decision.Outcome,
		PolicyVersion: result.Decision.PolicyVersion, Duplicate: result.Duplicate,
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
		errors.Is(err, domainreview.ErrReviewCaseNotOpen),
		errors.Is(err, domainreview.ErrReviewSubjectStale),
		errors.Is(err, domainreview.ErrReviewSubjectState),
		errors.Is(err, domainreview.ErrReviewSubjectNotReady):
		interfaceshttpapierror.Write(c, http.StatusConflict, interfaceshttpapierror.CodeReviewConflict, "review result conflict")
	default:
		interfaceshttpapierror.WriteServiceUnavailableCode(c, interfaceshttpapierror.CodeReviewUnavailable, "review unavailable", err)
	}
}
