package interfaceshttpmessage

import (
	applicationmessage "github.com/shiyudesu/frux/internal/application/message"
	domainmessage "github.com/shiyudesu/frux/internal/domain/message"
	interfaceshttpbinding "github.com/shiyudesu/frux/internal/interfaces/http/binding"
	interfaceshttpmiddleware "github.com/shiyudesu/frux/internal/interfaces/http/middleware"
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
)

type Handler struct {
	service *applicationmessage.Service
}

// New 注入消息应用服务。
func New(service *applicationmessage.Service) *Handler {
	return &Handler{service: service}
}

// List 查询当前登录用户的消息列表。
func (h *Handler) List(ctx context.Context, c *app.RequestContext) {
	userID, ok := userIDFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, utils.H{"error": "invalid access token"})
		return
	}

	limit, err := parseLimit(c.Query("limit"))
	if err != nil {
		writeMessageError(c, err)
		return
	}

	result, err := h.service.List(ctx, userID, c.Query("cursor"), limit)
	if err != nil {
		writeMessageError(c, err)
		return
	}
	c.JSON(http.StatusOK, listResponseFromResult(result))
}

// CountUnread 查询当前登录用户未读消息数。
func (h *Handler) CountUnread(ctx context.Context, c *app.RequestContext) {
	userID, ok := userIDFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, utils.H{"error": "invalid access token"})
		return
	}

	stat, err := h.service.CountUnread(ctx, userID)
	if err != nil {
		writeMessageError(c, err)
		return
	}
	c.JSON(http.StatusOK, unreadStatResponse{UnreadCount: stat.UnreadCount})
}

// MarkRead 将当前登录用户的指定消息标记为已读。
func (h *Handler) MarkRead(ctx context.Context, c *app.RequestContext) {
	userID, ok := userIDFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, utils.H{"error": "invalid access token"})
		return
	}

	var req markReadRequest
	if err := interfaceshttpbinding.BindJSON(c, &req); err != nil {
		c.JSON(http.StatusBadRequest, utils.H{"error": "invalid request"})
		return
	}

	result, err := h.service.MarkRead(ctx, userID, req.MessageIDs)
	if err != nil {
		writeMessageError(c, err)
		return
	}
	c.JSON(http.StatusOK, markReadResponse{UpdatedCount: result.UpdatedCount})
}

// Create 供内部事件链路生成用户消息。
func (h *Handler) Create(ctx context.Context, c *app.RequestContext) {
	var req createMessageRequest
	if err := interfaceshttpbinding.BindJSON(c, &req); err != nil {
		c.JSON(http.StatusBadRequest, utils.H{"error": "invalid request"})
		return
	}

	result, err := h.service.CreateFromTargetedActorEvent(
		ctx,
		req.UserID,
		req.Type,
		req.Title,
		req.Content,
		req.EventID,
		string(c.GetHeader("Idempotency-Key")),
		req.ActorID,
		req.ActorNickname,
		req.ActorAvatarURL,
		req.VideoID,
		req.CommentID,
		req.RootCommentID,
	)
	if err != nil {
		writeMessageError(c, err)
		return
	}

	status := http.StatusCreated
	if !result.Created {
		status = http.StatusOK
	}
	c.JSON(status, responseFromDomain(result.Message))
}

func userIDFromContext(c *app.RequestContext) (int64, bool) {
	value, exists := c.Get(interfaceshttpmiddleware.ContextUserIDKey)
	if !exists {
		return 0, false
	}
	userID, ok := value.(int64)
	return userID, ok && userID > 0
}

func parseLimit(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 {
		return 0, domainmessage.ErrInvalidLimit
	}
	return limit, nil
}

func listResponseFromResult(result *applicationmessage.ListResult) messageListResponse {
	items := make([]messageResponse, 0, len(result.Items))
	for _, item := range result.Items {
		items = append(items, responseFromDomain(item))
	}
	return messageListResponse{
		Items:      items,
		NextCursor: result.NextCursor,
		HasMore:    result.HasMore,
	}
}

func responseFromDomain(message *domainmessage.Message) messageResponse {
	return messageResponse{
		ID:             message.ID,
		UserID:         message.UserID,
		Type:           message.Type,
		Title:          message.Title,
		Content:        message.Content,
		EventID:        message.EventID,
		ActorID:        message.ActorID,
		ActorNickname:  message.ActorNickname,
		ActorAvatarURL: message.ActorAvatarURL,
		VideoID:        message.VideoID,
		CommentID:      message.CommentID,
		RootCommentID:  message.RootCommentID,
		IsRead:         message.IsRead,
		CreatedAt:      message.CreatedAt,
		ReadAt:         message.ReadAt,
	}
}

func writeMessageError(c *app.RequestContext, err error) {
	if isBadRequestError(err) {
		c.JSON(http.StatusBadRequest, utils.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusInternalServerError, utils.H{"error": "internal server error"})
}

func isBadRequestError(err error) bool {
	return errors.Is(err, domainmessage.ErrInvalidUserID) ||
		errors.Is(err, domainmessage.ErrInvalidMessageID) ||
		errors.Is(err, domainmessage.ErrInvalidLimit) ||
		errors.Is(err, domainmessage.ErrInvalidCursor) ||
		errors.Is(err, domainmessage.ErrInvalidMessageType) ||
		errors.Is(err, domainmessage.ErrInvalidMessageTarget) ||
		errors.Is(err, domainmessage.ErrEmptyTitle) ||
		errors.Is(err, domainmessage.ErrTitleTooLong) ||
		errors.Is(err, domainmessage.ErrEmptyContent) ||
		errors.Is(err, domainmessage.ErrContentTooLong) ||
		errors.Is(err, domainmessage.ErrEventIDTooLong) ||
		errors.Is(err, domainmessage.ErrIdempotencyKeyTooLong)
}
