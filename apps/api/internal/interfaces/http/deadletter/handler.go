package interfaceshttpdeadletter

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	applicationdeadletter "github.com/shiyudesu/frux/internal/application/deadletter"
	domaindeadletter "github.com/shiyudesu/frux/internal/domain/deadletter"
	interfaceshttpapierror "github.com/shiyudesu/frux/internal/interfaces/http/apierror"
	interfaceshttpbinding "github.com/shiyudesu/frux/internal/interfaces/http/binding"
	interfaceshttpmiddleware "github.com/shiyudesu/frux/internal/interfaces/http/middleware"

	"github.com/cloudwego/hertz/pkg/app"
)

const maxReplayBodyBytes = 2048

type Service interface {
	List(ctx context.Context) ([]domaindeadletter.QueueSummary, error)
	Preview(ctx context.Context, queue string, limit int) ([]domaindeadletter.MessagePreview, error)
	Replay(ctx context.Context, request applicationdeadletter.ReplayRequest) (*applicationdeadletter.ReplayResult, error)
}

type Handler struct {
	service Service
}

type replayRequest struct {
	Queue  string `json:"queue"`
	Reason string `json:"reason"`
}

type queueSummaryResponse struct {
	Consumer        string `json:"consumer"`
	Queue           string `json:"queue"`
	Messages        int64  `json:"messages"`
	MessagesReady   int64  `json:"messages_ready"`
	MessagesUnacked int64  `json:"messages_unacked"`
	Consumers       int    `json:"consumers"`
	State           string `json:"state"`
}

type messagePreviewResponse struct {
	MessageID       string    `json:"message_id"`
	OriginalEventID string    `json:"original_event_id"`
	ReplayID        string    `json:"replay_id,omitempty"`
	ContentType     string    `json:"content_type,omitempty"`
	Exchange        string    `json:"exchange"`
	RoutingKey      string    `json:"routing_key"`
	PayloadBytes    int       `json:"payload_bytes"`
	PayloadSHA256   string    `json:"payload_sha256"`
	JSONValid       bool      `json:"json_valid"`
	JSONFields      []string  `json:"json_fields"`
	DeathCount      int64     `json:"death_count"`
	PublishedAt     time.Time `json:"published_at,omitempty"`
}

type replayResponse struct {
	Queue           string `json:"queue"`
	MessageID       string `json:"message_id"`
	OriginalEventID string `json:"original_event_id"`
	ReplayID        string `json:"replay_id"`
}

func New(service Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) List(ctx context.Context, c *app.RequestContext) {
	if h == nil || h.service == nil {
		writeError(c, domaindeadletter.ErrInspectionFailed)
		return
	}
	items, err := h.service.List(ctx)
	if err != nil {
		writeError(c, err)
		return
	}
	response := make([]queueSummaryResponse, 0, len(items))
	for _, item := range items {
		response = append(response, queueSummaryResponse{
			Consumer: item.Consumer, Queue: item.Queue,
			Messages: item.Messages, MessagesReady: item.MessagesReady,
			MessagesUnacked: item.MessagesUnacked, Consumers: item.Consumers,
			State: item.State,
		})
	}
	c.JSON(http.StatusOK, map[string]any{"items": response})
}

func (h *Handler) Preview(ctx context.Context, c *app.RequestContext) {
	limit, err := parseLimit(c.Query("limit"))
	if err != nil {
		writeError(c, err)
		return
	}
	items, err := h.service.Preview(ctx, c.Param("queue"), limit)
	if err != nil {
		writeError(c, err)
		return
	}
	response := make([]messagePreviewResponse, 0, len(items))
	for _, item := range items {
		response = append(response, messagePreviewResponse{
			MessageID: item.MessageID, OriginalEventID: item.OriginalEventID,
			ReplayID: item.ReplayID, ContentType: item.ContentType,
			Exchange: item.Exchange, RoutingKey: item.RoutingKey,
			PayloadBytes: item.PayloadBytes, PayloadSHA256: item.PayloadSHA256,
			JSONValid: item.JSONValid, JSONFields: item.JSONFields,
			DeathCount: item.DeathCount, PublishedAt: item.PublishedAt,
		})
	}
	c.JSON(http.StatusOK, map[string]any{"items": response})
}

func (h *Handler) Replay(ctx context.Context, c *app.RequestContext) {
	principal, ok := interfaceshttpmiddleware.AdminPrincipalFromContext(c)
	if !ok {
		interfaceshttpapierror.WriteServiceUnavailableCode(
			c, interfaceshttpapierror.CodeAdminAuthorizationUnavailable,
			"admin authorization unavailable", nil,
		)
		return
	}
	var request replayRequest
	if err := interfaceshttpbinding.BindStrictJSON(c, &request, maxReplayBodyBytes); err != nil {
		interfaceshttpapierror.WriteInvalidRequest(c)
		return
	}
	result, err := h.service.Replay(ctx, applicationdeadletter.ReplayRequest{
		Queue: request.Queue, MessageID: c.Param("messageId"),
		ActorID: principal.UserID, Reason: request.Reason,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, replayResponse{
		Queue: result.Queue, MessageID: result.MessageID,
		OriginalEventID: result.OriginalEventID, ReplayID: result.ReplayID,
	})
}

func parseLimit(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > domaindeadletter.MaxPreviewLimit {
		return 0, domaindeadletter.ErrInvalidLimit
	}
	return limit, nil
}

func writeError(c *app.RequestContext, err error) {
	switch {
	case errors.Is(err, domaindeadletter.ErrInvalidQueue),
		errors.Is(err, domaindeadletter.ErrInvalidMessageID),
		errors.Is(err, domaindeadletter.ErrInvalidReason),
		errors.Is(err, domaindeadletter.ErrInvalidLimit):
		interfaceshttpapierror.Write(
			c, http.StatusBadRequest,
			interfaceshttpapierror.CodeDeadLetterValidationFailed,
			"invalid dead-letter request",
		)
	case errors.Is(err, domaindeadletter.ErrMessageNotFound):
		interfaceshttpapierror.Write(
			c, http.StatusNotFound,
			interfaceshttpapierror.CodeDeadLetterMessageNotFound,
			"dead-letter message not found",
		)
	case errors.Is(err, domaindeadletter.ErrMessageNotAtHead):
		interfaceshttpapierror.Write(
			c, http.StatusConflict,
			interfaceshttpapierror.CodeDeadLetterMessageConflict,
			"dead-letter message is not at queue head",
		)
	default:
		interfaceshttpapierror.WriteServiceUnavailableCode(
			c, interfaceshttpapierror.CodeDeadLetterUnavailable,
			"dead-letter recovery unavailable", err,
		)
	}
}
