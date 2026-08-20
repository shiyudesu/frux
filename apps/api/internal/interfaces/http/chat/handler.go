package interfaceshttpchat

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	applicationchat "github.com/shiyudesu/frux/internal/application/chat"
	domainchat "github.com/shiyudesu/frux/internal/domain/chat"
	interfaceshttpapierror "github.com/shiyudesu/frux/internal/interfaces/http/apierror"
	interfaceshttpbinding "github.com/shiyudesu/frux/internal/interfaces/http/binding"
	interfaceshttpmiddleware "github.com/shiyudesu/frux/internal/interfaces/http/middleware"

	"github.com/cloudwego/hertz/pkg/app"
)

const maxChatJSONBodyBytes = 16 << 10

type Handler struct {
	service *applicationchat.Service
}

func New(service *applicationchat.Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) Eligibility(ctx context.Context, c *app.RequestContext) {
	userID, ok := userIDFromContext(c)
	if !ok {
		interfaceshttpapierror.WriteInvalidAccessToken(c)
		return
	}
	targetID, err := positivePathID(c.Param("targetUserId"), domainchat.ErrInvalidTargetUserID)
	if err != nil {
		writeChatError(c, err)
		return
	}
	result, err := h.service.Eligibility(ctx, userID, targetID)
	if err != nil {
		writeChatError(c, err)
		return
	}
	c.JSON(http.StatusOK, eligibilityResponse{
		Eligible: result.Eligible, Reason: string(result.Reason), ConversationID: result.ConversationID,
	})
}

func (h *Handler) ListRecipients(ctx context.Context, c *app.RequestContext) {
	userID, ok := userIDFromContext(c)
	if !ok {
		interfaceshttpapierror.WriteInvalidAccessToken(c)
		return
	}
	limit, err := parseLimit(c.Query("limit"))
	if err != nil {
		writeChatError(c, err)
		return
	}
	result, err := h.service.ListRecipients(ctx, userID, c.Query("q"), c.Query("cursor"), limit)
	if err != nil {
		writeChatError(c, err)
		return
	}
	items := make([]recipientResponse, 0, len(result.Items))
	for _, item := range result.Items {
		if item == nil {
			continue
		}
		items = append(items, recipientResponse{
			UserID: item.UserID, Nickname: item.Nickname, AvatarURL: item.AvatarURL,
			Bio: item.Bio, FollowedAt: item.FollowedAt, ConversationID: item.ConversationID,
		})
	}
	c.JSON(http.StatusOK, recipientListResponse{Items: items, NextCursor: result.NextCursor, HasMore: result.HasMore})
}

func (h *Handler) CreateConversation(ctx context.Context, c *app.RequestContext) {
	userID, ok := userIDFromContext(c)
	if !ok {
		interfaceshttpapierror.WriteInvalidAccessToken(c)
		return
	}
	var request createConversationRequest
	if err := interfaceshttpbinding.BindStrictJSON(c, &request, maxChatJSONBodyBytes); err != nil {
		interfaceshttpapierror.WriteInvalidRequest(c)
		return
	}
	conversation, err := h.service.CreateConversation(ctx, userID, request.TargetUserID)
	if err != nil {
		writeChatError(c, err)
		return
	}
	c.JSON(http.StatusOK, map[string]int64{"conversation_id": conversation.ID})
}

func (h *Handler) ListConversations(ctx context.Context, c *app.RequestContext) {
	userID, ok := userIDFromContext(c)
	if !ok {
		interfaceshttpapierror.WriteInvalidAccessToken(c)
		return
	}
	limit, err := parseLimit(c.Query("limit"))
	if err != nil {
		writeChatError(c, err)
		return
	}
	result, err := h.service.ListConversations(ctx, userID, c.Query("cursor"), limit)
	if err != nil {
		writeChatError(c, err)
		return
	}
	items := make([]conversationResponse, 0, len(result.Items))
	for _, item := range result.Items {
		if item == nil {
			continue
		}
		items = append(items, conversationFromView(item))
	}
	c.JSON(http.StatusOK, conversationListResponse{Items: items, NextCursor: result.NextCursor, HasMore: result.HasMore})
}

func (h *Handler) History(ctx context.Context, c *app.RequestContext) {
	userID, ok := userIDFromContext(c)
	if !ok {
		interfaceshttpapierror.WriteInvalidAccessToken(c)
		return
	}
	conversationID, err := positivePathID(c.Param("conversationId"), domainchat.ErrInvalidConversationID)
	if err != nil {
		writeChatError(c, err)
		return
	}
	limit, err := parseLimit(c.Query("limit"))
	if err != nil {
		writeChatError(c, err)
		return
	}
	afterMessageID, err := parseOptionalPositiveID(c.Query("after_message_id"))
	if err != nil {
		writeChatError(c, err)
		return
	}
	var result *applicationchat.HistoryResult
	if afterMessageID > 0 {
		result, err = h.service.ListHistory(ctx, userID, conversationID, c.Query("cursor"), limit, afterMessageID)
	} else {
		result, err = h.service.ListHistory(ctx, userID, conversationID, c.Query("cursor"), limit)
	}
	if err != nil {
		writeChatError(c, err)
		return
	}
	items := make([]messageResponse, 0, len(result.Items))
	for _, item := range result.Items {
		if item != nil {
			items = append(items, messageFromView(item))
		}
	}
	c.JSON(http.StatusOK, historyResponse{
		Items: items, NextCursor: result.NextCursor, HasMore: result.HasMore,
		Conversation: conversationFromView(result.Conversation),
		Eligibility:  eligibilityFromResult(result.Eligibility),
	})
}

func (h *Handler) Send(ctx context.Context, c *app.RequestContext) {
	userID, ok := userIDFromContext(c)
	if !ok {
		interfaceshttpapierror.WriteInvalidAccessToken(c)
		return
	}
	conversationID, err := positivePathID(c.Param("conversationId"), domainchat.ErrInvalidConversationID)
	if err != nil {
		writeChatError(c, err)
		return
	}
	var request sendMessageRequest
	if err := interfaceshttpbinding.BindStrictJSON(c, &request, maxChatJSONBodyBytes); err != nil {
		interfaceshttpapierror.WriteInvalidRequest(c)
		return
	}
	var result *applicationchat.SendResult
	switch strings.ToUpper(strings.TrimSpace(request.Kind)) {
	case string(domainchat.MessageKindText):
		if request.Text == nil || request.VideoID != nil {
			writeChatError(c, domainchat.ErrInvalidMessageShape)
			return
		}
		result, err = h.service.SendText(ctx, userID, conversationID, *request.Text, string(c.GetHeader("Idempotency-Key")))
	case string(domainchat.MessageKindVideo):
		if request.Text != nil || request.VideoID == nil {
			writeChatError(c, domainchat.ErrInvalidMessageShape)
			return
		}
		result, err = h.service.SendVideo(ctx, userID, conversationID, *request.VideoID, string(c.GetHeader("Idempotency-Key")))
	default:
		err = domainchat.ErrInvalidMessageShape
	}
	if err != nil {
		writeChatError(c, err)
		return
	}
	status := http.StatusCreated
	if !result.Created {
		status = http.StatusOK
	}
	c.JSON(status, sendMessageResponse{Message: messageFromView(result.Message), Created: result.Created})
}

func (h *Handler) MarkRead(ctx context.Context, c *app.RequestContext) {
	userID, ok := userIDFromContext(c)
	if !ok {
		interfaceshttpapierror.WriteInvalidAccessToken(c)
		return
	}
	conversationID, err := positivePathID(c.Param("conversationId"), domainchat.ErrInvalidConversationID)
	if err != nil {
		writeChatError(c, err)
		return
	}
	var request markReadRequest
	if err := interfaceshttpbinding.BindStrictJSON(c, &request, maxChatJSONBodyBytes); err != nil {
		interfaceshttpapierror.WriteInvalidRequest(c)
		return
	}
	result, err := h.service.MarkRead(ctx, userID, conversationID, request.ThroughMessageID)
	if err != nil {
		writeChatError(c, err)
		return
	}
	c.JSON(http.StatusOK, markReadResponse{
		LastReadMessageID: result.LastReadMessageID, UnreadCount: result.UnreadCount,
	})
}

func (h *Handler) InboxUnread(ctx context.Context, c *app.RequestContext) {
	userID, ok := userIDFromContext(c)
	if !ok {
		interfaceshttpapierror.WriteInvalidAccessToken(c)
		return
	}
	result, err := h.service.InboxUnread(ctx, userID)
	if err != nil {
		writeChatError(c, err)
		return
	}
	c.JSON(http.StatusOK, inboxUnreadResponse{
		NotificationUnreadCount: result.NotificationUnreadCount,
		ChatUnreadCount:         result.ChatUnreadCount,
		TotalUnreadCount:        result.TotalUnreadCount,
	})
}

func userIDFromContext(c *app.RequestContext) (int64, bool) {
	value, exists := c.Get(interfaceshttpmiddleware.ContextUserIDKey)
	if !exists {
		return 0, false
	}
	userID, ok := value.(int64)
	return userID, ok && userID > 0
}

func positivePathID(raw string, invalid error) (int64, error) {
	id, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || id <= 0 {
		return 0, invalid
	}
	return id, nil
}

func parseLimit(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 {
		return 0, domainchat.ErrInvalidLimit
	}
	return limit, nil
}

func parseOptionalPositiveID(raw string) (int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return 0, domainchat.ErrInvalidMessageID
	}
	return id, nil
}

func participantFromDomain(participant *domainchat.Participant) participantResponse {
	if participant == nil {
		return participantResponse{Available: false}
	}
	return participantResponse{
		UserID: participant.UserID, Nickname: participant.Nickname,
		AvatarURL: participant.AvatarURL, Bio: participant.Bio, Available: participant.Available,
	}
}

func conversationFromView(view *applicationchat.ConversationView) conversationResponse {
	if view == nil {
		return conversationResponse{}
	}
	response := conversationResponse{
		ID: view.ID, Counterpart: participantFromDomain(view.Counterpart),
		LastMessageID: view.LastMessageID, LastMessageAt: view.LastMessageAt,
		UnreadCount: view.UnreadCount,
	}
	if view.LastMessageID > 0 && view.LastMessageAt != nil && view.LastMessageKind != "" {
		response.LastMessage = &lastMessageResponse{
			ID: view.LastMessageID, Kind: view.LastMessageKind,
			Preview: view.LastMessagePreview, CreatedAt: *view.LastMessageAt,
		}
	}
	return response
}

func eligibilityFromResult(result *applicationchat.EligibilityResult) eligibilityResponse {
	if result == nil {
		return eligibilityResponse{}
	}
	return eligibilityResponse{
		Eligible: result.Eligible, Reason: string(result.Reason),
		ConversationID: result.ConversationID,
	}
}

func videoFromDomain(video *domainchat.VideoCard) *videoCardResponse {
	if video == nil {
		return nil
	}
	return &videoCardResponse{
		VideoID: video.VideoID, Available: video.Available, Title: video.Title,
		CoverURL: video.CoverURL, MediaURL: video.MediaURL, AuthorID: video.AuthorID,
		AuthorNickname: video.AuthorNickname, AuthorAvatarURL: video.AuthorAvatarURL,
	}
}

func messageFromView(message *applicationchat.MessageView) messageResponse {
	return messageResponse{
		ID: message.ID, ConversationID: message.ConversationID,
		Sender: participantFromDomain(message.Sender), Kind: message.Kind,
		Text: message.Text, Video: videoFromDomain(message.Video), CreatedAt: message.CreatedAt,
	}
}

func writeChatError(c *app.RequestContext, err error) {
	switch {
	case errors.Is(err, domainchat.ErrInvalidUserID),
		errors.Is(err, domainchat.ErrInvalidTargetUserID),
		errors.Is(err, domainchat.ErrInvalidConversationID),
		errors.Is(err, domainchat.ErrInvalidQuery),
		errors.Is(err, domainchat.ErrInvalidMessageID),
		errors.Is(err, domainchat.ErrInvalidLimit),
		errors.Is(err, domainchat.ErrInvalidMessageShape),
		errors.Is(err, domainchat.ErrEmptyText),
		errors.Is(err, domainchat.ErrTextTooLong),
		errors.Is(err, domainchat.ErrInvalidVideoID),
		errors.Is(err, domainchat.ErrIdempotencyKeyRequired),
		errors.Is(err, domainchat.ErrIdempotencyKeyTooLong):
		interfaceshttpapierror.Write(c, http.StatusBadRequest, interfaceshttpapierror.CodeChatValidationFailed, "invalid chat request")
	case errors.Is(err, domainchat.ErrInvalidCursor):
		interfaceshttpapierror.Write(c, http.StatusBadRequest, interfaceshttpapierror.CodeChatCursorInvalid, "chat cursor is invalid")
	case errors.Is(err, domainchat.ErrSelfConversation):
		interfaceshttpapierror.Write(c, http.StatusBadRequest, interfaceshttpapierror.CodeChatSelfConversation, "self conversation is not allowed")
	case errors.Is(err, domainchat.ErrNotEligible):
		interfaceshttpapierror.Write(c, http.StatusConflict, interfaceshttpapierror.CodeChatNotEligible, "mutual follow is required")
	case errors.Is(err, domainchat.ErrAccountUnavailable):
		interfaceshttpapierror.Write(c, http.StatusNotFound, interfaceshttpapierror.CodeChatAccountUnavailable, "chat participant unavailable")
	case errors.Is(err, domainchat.ErrConversationNotFound), errors.Is(err, domainchat.ErrNotMember):
		interfaceshttpapierror.Write(c, http.StatusNotFound, interfaceshttpapierror.CodeChatConversationNotFound, "conversation not found")
	case errors.Is(err, domainchat.ErrMessageNotFound):
		interfaceshttpapierror.Write(c, http.StatusBadRequest, interfaceshttpapierror.CodeChatMessageNotFound, "message not found")
	case errors.Is(err, domainchat.ErrVideoUnavailable):
		interfaceshttpapierror.Write(c, http.StatusConflict, interfaceshttpapierror.CodeChatVideoUnavailable, "video unavailable")
	case errors.Is(err, domainchat.ErrIdempotencyConflict):
		interfaceshttpapierror.Write(c, http.StatusConflict, interfaceshttpapierror.CodeChatIdempotencyConflict, "idempotency key conflict")
	case errors.Is(err, applicationchat.ErrLoadChatFailed), errors.Is(err, applicationchat.ErrSaveChatFailed), errors.Is(err, applicationchat.ErrUpdateChatFailed):
		interfaceshttpapierror.WriteServiceUnavailableCode(c, interfaceshttpapierror.CodeChatUnavailable, "chat service unavailable", err)
	default:
		interfaceshttpapierror.WriteInternal(c, "internal server error", err)
	}
}
