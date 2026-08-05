package interfaceshttpadmin

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	applicationadminaudit "github.com/shiyudesu/frux/internal/application/adminaudit"
	domainaccount "github.com/shiyudesu/frux/internal/domain/account"
	domainadminaudit "github.com/shiyudesu/frux/internal/domain/adminaudit"
	interfaceshttpapierror "github.com/shiyudesu/frux/internal/interfaces/http/apierror"
	interfaceshttpmiddleware "github.com/shiyudesu/frux/internal/interfaces/http/middleware"

	"github.com/cloudwego/hertz/pkg/app"
)

type AuditQueryService interface {
	Query(ctx context.Context, request applicationadminaudit.QueryRequest) (*applicationadminaudit.QueryPage, error)
}

type Option func(*Handler)

type Handler struct {
	auditQuery AuditQueryService
}

type principalResponse struct {
	UserID      int64    `json:"user_id"`
	Role        string   `json:"role"`
	Permissions []string `json:"permissions"`
}

type auditEventResponse struct {
	ID                 int64             `json:"id"`
	ActorID            int64             `json:"actor_id"`
	Permission         string            `json:"permission"`
	Action             string            `json:"action"`
	TargetType         string            `json:"target_type"`
	TargetID           string            `json:"target_id"`
	Outcome            string            `json:"outcome"`
	RequestID          string            `json:"request_id"`
	IdempotencyKeyHash string            `json:"idempotency_key_hash,omitempty"`
	Detail             map[string]string `json:"detail"`
	CreatedAt          time.Time         `json:"created_at"`
}

type auditEventListResponse struct {
	Items      []auditEventResponse `json:"items"`
	NextCursor string               `json:"next_cursor"`
	HasMore    bool                 `json:"has_more"`
}

func New(options ...Option) *Handler {
	handler := &Handler{}
	for _, option := range options {
		if option != nil {
			option(handler)
		}
	}
	return handler
}

func WithAuditQueryService(service AuditQueryService) Option {
	return func(handler *Handler) {
		handler.auditQuery = service
	}
}

func (h *Handler) Me(_ context.Context, c *app.RequestContext) {
	principal, ok := interfaceshttpmiddleware.AdminPrincipalFromContext(c)
	if !ok {
		interfaceshttpapierror.WriteServiceUnavailableCode(
			c,
			interfaceshttpapierror.CodeAdminAuthorizationUnavailable,
			"admin authorization unavailable",
			nil,
		)
		return
	}
	c.JSON(http.StatusOK, principalResponseFromDomain(principal))
}

func (h *Handler) ListAuditEvents(ctx context.Context, c *app.RequestContext) {
	if h.auditQuery == nil {
		interfaceshttpapierror.WriteServiceUnavailableCode(
			c,
			interfaceshttpapierror.CodeAdminAuditUnavailable,
			"admin audit unavailable",
			nil,
		)
		return
	}
	request, err := parseAuditQuery(c)
	if err != nil {
		writeAuditQueryError(c, err)
		return
	}
	page, err := h.auditQuery.Query(ctx, request)
	if err != nil {
		writeAuditQueryError(c, err)
		return
	}
	response := auditEventListResponse{
		Items:      make([]auditEventResponse, 0, len(page.Items)),
		NextCursor: page.NextCursor,
		HasMore:    page.HasMore,
	}
	for _, fact := range page.Items {
		response.Items = append(response.Items, auditEventResponseFromDomain(fact))
	}
	c.JSON(http.StatusOK, response)
}

func principalResponseFromDomain(principal *domainaccount.AdminPrincipal) principalResponse {
	permissions := principal.Permissions()
	response := principalResponse{
		UserID:      principal.UserID,
		Role:        principal.Role,
		Permissions: make([]string, 0, len(permissions)),
	}
	for _, permission := range permissions {
		response.Permissions = append(response.Permissions, string(permission))
	}
	return response
}

func auditEventResponseFromDomain(fact *domainadminaudit.Fact) auditEventResponse {
	return auditEventResponse{
		ID: fact.ID(), ActorID: fact.ActorID(), Permission: string(fact.Permission()),
		Action: string(fact.Action()), TargetType: string(fact.TargetType()), TargetID: fact.TargetID(),
		Outcome: string(fact.Outcome()), RequestID: fact.RequestID(),
		IdempotencyKeyHash: fact.IdempotencyKeyHash(), Detail: fact.Detail(), CreatedAt: fact.CreatedAt(),
	}
}

func parseAuditQuery(c *app.RequestContext) (applicationadminaudit.QueryRequest, error) {
	from, err := time.Parse(time.RFC3339, strings.TrimSpace(c.Query("from")))
	if err != nil {
		return applicationadminaudit.QueryRequest{}, domainadminaudit.ErrInvalidTimeRange
	}
	to, err := time.Parse(time.RFC3339, strings.TrimSpace(c.Query("to")))
	if err != nil {
		return applicationadminaudit.QueryRequest{}, domainadminaudit.ErrInvalidTimeRange
	}
	actorID, err := parseOptionalPositiveInt64(c.Query("actor_id"))
	if err != nil {
		return applicationadminaudit.QueryRequest{}, err
	}
	limit, err := parseOptionalPositiveInt(c.Query("limit"))
	if err != nil {
		return applicationadminaudit.QueryRequest{}, err
	}
	return applicationadminaudit.QueryRequest{
		ActorID: actorID, Action: c.Query("action"), TargetType: c.Query("target_type"),
		Outcome: c.Query("outcome"), From: from, To: to, Cursor: c.Query("cursor"), Limit: limit,
	}, nil
}

func parseOptionalPositiveInt64(raw string) (int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value <= 0 {
		return 0, domainadminaudit.ErrInvalidActorID
	}
	return value, nil
}

func parseOptionalPositiveInt(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return 0, domainadminaudit.ErrInvalidLimit
	}
	return value, nil
}

func writeAuditQueryError(c *app.RequestContext, err error) {
	switch {
	case errors.Is(err, domainadminaudit.ErrInvalidCursor):
		interfaceshttpapierror.Write(
			c,
			http.StatusBadRequest,
			interfaceshttpapierror.CodeAdminAuditCursorInvalid,
			"invalid admin audit cursor",
		)
	case errors.Is(err, domainadminaudit.ErrInvalidActorID),
		errors.Is(err, domainadminaudit.ErrInvalidAction),
		errors.Is(err, domainadminaudit.ErrInvalidTargetType),
		errors.Is(err, domainadminaudit.ErrInvalidOutcome),
		errors.Is(err, domainadminaudit.ErrInvalidTimeRange),
		errors.Is(err, domainadminaudit.ErrTimeRangeTooLarge),
		errors.Is(err, domainadminaudit.ErrInvalidLimit):
		interfaceshttpapierror.Write(
			c,
			http.StatusBadRequest,
			interfaceshttpapierror.CodeAdminAuditQueryInvalid,
			"invalid admin audit query",
		)
	default:
		interfaceshttpapierror.WriteServiceUnavailableCode(
			c,
			interfaceshttpapierror.CodeAdminAuditUnavailable,
			"admin audit unavailable",
			err,
		)
	}
}
