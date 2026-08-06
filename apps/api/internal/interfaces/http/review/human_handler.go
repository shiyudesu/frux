package interfaceshttpreview

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	domainreview "github.com/shiyudesu/frux/internal/domain/review"
	interfaceshttpapierror "github.com/shiyudesu/frux/internal/interfaces/http/apierror"

	"github.com/cloudwego/hertz/pkg/app"
)

func parseCaseID(c *app.RequestContext) (int64, bool) {
	caseID, err := strconv.ParseInt(strings.TrimSpace(c.Param("caseId")), 10, 64)
	if err != nil || caseID <= 0 {
		interfaceshttpapierror.WriteInvalidRequest(c)
		return 0, false
	}
	return caseID, true
}

func parseOptionalPriority(raw string, fallback int) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || !domainreview.ValidPriority(value) {
		return 0, domainreview.ErrInvalidQueueFilter
	}
	return value, nil
}

func parseOptionalPositiveInt(raw string, fallback int) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return 0, domainreview.ErrInvalidQueueFilter
	}
	return value, nil
}

func writeHumanReviewError(c *app.RequestContext, err error) {
	switch {
	case errors.Is(err, domainreview.ErrInvalidQueueCursor):
		interfaceshttpapierror.Write(c, http.StatusBadRequest, interfaceshttpapierror.CodeReviewCursorInvalid, "invalid review cursor")
	case errors.Is(err, domainreview.ErrInvalidCaseID),
		errors.Is(err, domainreview.ErrInvalidReviewerID),
		errors.Is(err, domainreview.ErrInvalidCaseVersion),
		errors.Is(err, domainreview.ErrInvalidLeaseToken),
		errors.Is(err, domainreview.ErrInvalidLeaseDuration),
		errors.Is(err, domainreview.ErrInvalidReasonCode),
		errors.Is(err, domainreview.ErrInvalidDecisionOutcome),
		errors.Is(err, domainreview.ErrReviewNoteTooLong),
		errors.Is(err, domainreview.ErrReviewNoteRequired),
		errors.Is(err, domainreview.ErrInvalidIdempotencyKey),
		errors.Is(err, domainreview.ErrInvalidQueueFilter):
		interfaceshttpapierror.Write(c, http.StatusBadRequest, interfaceshttpapierror.CodeReviewValidationFailed, "invalid human review request")
	case errors.Is(err, domainreview.ErrReviewCaseNotFound):
		interfaceshttpapierror.Write(c, http.StatusNotFound, interfaceshttpapierror.CodeReviewCaseNotFound, "review case not found")
	case errors.Is(err, domainreview.ErrReviewCaseClaimed):
		interfaceshttpapierror.Write(c, http.StatusConflict, interfaceshttpapierror.CodeReviewCaseClaimed, "review case already claimed")
	case errors.Is(err, domainreview.ErrReviewLeaseExpired):
		interfaceshttpapierror.Write(c, http.StatusConflict, interfaceshttpapierror.CodeReviewLeaseExpired, "review lease expired")
	case errors.Is(err, domainreview.ErrReviewLeaseNotOwned):
		interfaceshttpapierror.Write(c, http.StatusConflict, interfaceshttpapierror.CodeReviewLeaseNotOwned, "review lease not owned")
	case errors.Is(err, domainreview.ErrReviewCaseVersion):
		interfaceshttpapierror.Write(c, http.StatusConflict, interfaceshttpapierror.CodeReviewCaseVersionConflict, "review case version conflict")
	case errors.Is(err, domainreview.ErrReviewSubjectStale):
		interfaceshttpapierror.Write(c, http.StatusConflict, interfaceshttpapierror.CodeReviewSubjectVersionConflict, "review subject version conflict")
	case errors.Is(err, domainreview.ErrDecisionIdentityConflict):
		interfaceshttpapierror.Write(c, http.StatusConflict, interfaceshttpapierror.CodeReviewDecisionIdempotencyConflict, "review decision idempotency conflict")
	case errors.Is(err, domainreview.ErrReviewCaseNotHuman),
		errors.Is(err, domainreview.ErrReviewCaseNotOpen),
		errors.Is(err, domainreview.ErrReviewSubjectState):
		interfaceshttpapierror.Write(c, http.StatusConflict, interfaceshttpapierror.CodeReviewConflict, "review conflict")
	default:
		interfaceshttpapierror.WriteServiceUnavailableCode(c, interfaceshttpapierror.CodeReviewUnavailable, "review unavailable", err)
	}
}
