package interfaceshttpgovernance

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	applicationgovernance "github.com/shiyudesu/frux/internal/application/governance"
	domaingovernance "github.com/shiyudesu/frux/internal/domain/governance"
	interfaceshttpapierror "github.com/shiyudesu/frux/internal/interfaces/http/apierror"
	interfaceshttpbinding "github.com/shiyudesu/frux/internal/interfaces/http/binding"
	interfaceshttpmiddleware "github.com/shiyudesu/frux/internal/interfaces/http/middleware"

	"github.com/cloudwego/hertz/pkg/app"
)

const maxMutationBodyBytes = 4096

type Service interface {
	ListControls(ctx context.Context) ([]applicationgovernance.Control, error)
	ListRevisions(ctx context.Context, key domaingovernance.Key, limit int) ([]*domaingovernance.Revision, error)
	Update(ctx context.Context, request applicationgovernance.UpdateRequest) (*domaingovernance.Revision, error)
	Rollback(ctx context.Context, request applicationgovernance.RollbackRequest) (*domaingovernance.Revision, error)
}

type Handler struct {
	service Service
}

type updateRequest struct {
	ExpectedRevision *int64     `json:"expected_revision"`
	Value            *bool      `json:"value"`
	Reason           string     `json:"reason"`
	ExpiresAt        *time.Time `json:"expires_at"`
}

type rollbackRequest struct {
	ExpectedRevision *int64 `json:"expected_revision"`
	TargetRevision   *int64 `json:"target_revision"`
	Reason           string `json:"reason"`
}

type definitionResponse struct {
	Key                 string            `json:"key"`
	Owner               string            `json:"owner"`
	Description         string            `json:"description"`
	ValueType           string            `json:"value_type"`
	DefaultValue        bool              `json:"default_value"`
	FailureDefaultValue bool              `json:"failure_default_value"`
	Processes           []string          `json:"processes"`
	MaxStalenessSeconds int64             `json:"max_staleness_seconds"`
	ActiveRevision      *revisionResponse `json:"active_revision"`
}

type revisionResponse struct {
	Key                  string     `json:"key"`
	Revision             int64      `json:"revision"`
	ValueType            string     `json:"value_type"`
	Value                bool       `json:"value"`
	Reason               string     `json:"reason"`
	ExpiresAt            *time.Time `json:"expires_at"`
	ActorID              int64      `json:"actor_id"`
	CreatedAt            time.Time  `json:"created_at"`
	RollbackFromRevision int64      `json:"rollback_from_revision,omitempty"`
}

type controlsResponse struct {
	Items []definitionResponse `json:"items"`
}

type revisionsResponse struct {
	Items []revisionResponse `json:"items"`
}

func New(service Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) List(ctx context.Context, c *app.RequestContext) {
	if h == nil || h.service == nil {
		writeError(c, applicationgovernance.ErrLoadControlsFailed)
		return
	}
	controls, err := h.service.ListControls(ctx)
	if err != nil {
		writeError(c, err)
		return
	}
	response := controlsResponse{Items: make([]definitionResponse, 0, len(controls))}
	for _, control := range controls {
		response.Items = append(response.Items, definitionResponseFromControl(control))
	}
	c.JSON(http.StatusOK, response)
}

func (h *Handler) ListRevisions(ctx context.Context, c *app.RequestContext) {
	limit, err := parseLimit(c.Query("limit"))
	if err != nil {
		writeError(c, err)
		return
	}
	revisions, err := h.service.ListRevisions(
		ctx, applicationgovernance.NormalizeKey(c.Param("key")), limit,
	)
	if err != nil {
		writeError(c, err)
		return
	}
	response := revisionsResponse{Items: make([]revisionResponse, 0, len(revisions))}
	for _, revision := range revisions {
		response.Items = append(response.Items, revisionResponseFromDomain(revision))
	}
	c.JSON(http.StatusOK, response)
}

func (h *Handler) Update(ctx context.Context, c *app.RequestContext) {
	principal, ok := interfaceshttpmiddleware.AdminPrincipalFromContext(c)
	if !ok {
		interfaceshttpapierror.WriteServiceUnavailableCode(
			c, interfaceshttpapierror.CodeAdminAuthorizationUnavailable,
			"admin authorization unavailable", nil,
		)
		return
	}
	var request updateRequest
	if err := interfaceshttpbinding.BindStrictJSON(c, &request, maxMutationBodyBytes); err != nil {
		interfaceshttpapierror.WriteInvalidRequest(c)
		return
	}
	if request.ExpectedRevision == nil || request.Value == nil {
		interfaceshttpapierror.WriteInvalidRequest(c)
		return
	}
	revision, err := h.service.Update(ctx, applicationgovernance.UpdateRequest{
		Key:     applicationgovernance.NormalizeKey(c.Param("key")),
		ActorID: principal.UserID, ExpectedRevision: *request.ExpectedRevision,
		Value:  domaingovernance.BooleanValue(*request.Value),
		Reason: request.Reason, ExpiresAt: request.ExpiresAt,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, revisionResponseFromDomain(revision))
}

func (h *Handler) Rollback(ctx context.Context, c *app.RequestContext) {
	principal, ok := interfaceshttpmiddleware.AdminPrincipalFromContext(c)
	if !ok {
		interfaceshttpapierror.WriteServiceUnavailableCode(
			c, interfaceshttpapierror.CodeAdminAuthorizationUnavailable,
			"admin authorization unavailable", nil,
		)
		return
	}
	var request rollbackRequest
	if err := interfaceshttpbinding.BindStrictJSON(c, &request, maxMutationBodyBytes); err != nil {
		interfaceshttpapierror.WriteInvalidRequest(c)
		return
	}
	if request.ExpectedRevision == nil || request.TargetRevision == nil {
		interfaceshttpapierror.WriteInvalidRequest(c)
		return
	}
	revision, err := h.service.Rollback(ctx, applicationgovernance.RollbackRequest{
		Key:     applicationgovernance.NormalizeKey(c.Param("key")),
		ActorID: principal.UserID, ExpectedRevision: *request.ExpectedRevision,
		TargetRevision: *request.TargetRevision, Reason: request.Reason,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, revisionResponseFromDomain(revision))
}

func definitionResponseFromControl(control applicationgovernance.Control) definitionResponse {
	defaultValue, _ := control.Definition.Default.Boolean()
	failureDefault, _ := control.Definition.FailureDefault.Boolean()
	processes := make([]string, 0, len(control.Definition.Processes))
	for _, process := range control.Definition.Processes {
		processes = append(processes, string(process))
	}
	response := definitionResponse{
		Key: string(control.Definition.Key), Owner: control.Definition.Owner,
		Description:  control.Definition.Description,
		ValueType:    string(control.Definition.ValueType),
		DefaultValue: defaultValue, FailureDefaultValue: failureDefault,
		Processes:           processes,
		MaxStalenessSeconds: int64(control.Definition.MaxStaleness / time.Second),
	}
	if control.ActiveRevision != nil {
		active := revisionResponseFromDomain(control.ActiveRevision)
		response.ActiveRevision = &active
	}
	return response
}

func revisionResponseFromDomain(revision *domaingovernance.Revision) revisionResponse {
	value, _ := revision.Value().Boolean()
	return revisionResponse{
		Key: string(revision.Key()), Revision: revision.Number(),
		ValueType: string(revision.Value().Type()), Value: value,
		Reason: revision.Reason(), ExpiresAt: revision.ExpiresAt(),
		ActorID: revision.ActorID(), CreatedAt: revision.CreatedAt(),
		RollbackFromRevision: revision.RollbackFromRevision(),
	}
}

func parseLimit(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > domaingovernance.MaxListLimit {
		return 0, domaingovernance.ErrInvalidLimit
	}
	return limit, nil
}

func writeError(c *app.RequestContext, err error) {
	switch {
	case errors.Is(err, domaingovernance.ErrUnknownControl):
		interfaceshttpapierror.Write(
			c, http.StatusBadRequest,
			interfaceshttpapierror.CodeGovernanceControlUnknown,
			"unknown governance control",
		)
	case errors.Is(err, domaingovernance.ErrRevisionConflict):
		interfaceshttpapierror.Write(
			c, http.StatusConflict,
			interfaceshttpapierror.CodeGovernanceRevisionConflict,
			"governance revision conflict",
		)
	case errors.Is(err, domaingovernance.ErrRevisionNotFound):
		interfaceshttpapierror.Write(
			c, http.StatusNotFound,
			interfaceshttpapierror.CodeGovernanceRevisionNotFound,
			"governance revision not found",
		)
	case errors.Is(err, domaingovernance.ErrInvalidControlValue),
		errors.Is(err, domaingovernance.ErrInvalidRevision),
		errors.Is(err, domaingovernance.ErrInvalidActorID),
		errors.Is(err, domaingovernance.ErrInvalidReason),
		errors.Is(err, domaingovernance.ErrReasonTooLong),
		errors.Is(err, domaingovernance.ErrInvalidExpiry),
		errors.Is(err, domaingovernance.ErrInvalidLimit):
		interfaceshttpapierror.Write(
			c, http.StatusBadRequest,
			interfaceshttpapierror.CodeGovernanceValidationFailed,
			"invalid governance control request",
		)
	default:
		interfaceshttpapierror.WriteServiceUnavailableCode(
			c, interfaceshttpapierror.CodeGovernanceUnavailable,
			"governance controls unavailable", err,
		)
	}
}
