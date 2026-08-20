package test

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	applicationchat "github.com/shiyudesu/frux/internal/application/chat"
	domainchat "github.com/shiyudesu/frux/internal/domain/chat"
	infrajwt "github.com/shiyudesu/frux/internal/infra/jwt"
	interfaceshttpchat "github.com/shiyudesu/frux/internal/interfaces/http/chat"
	interfaceshttpmiddleware "github.com/shiyudesu/frux/internal/interfaces/http/middleware"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
)

type apiChatRepo struct {
	mu           sync.Mutex
	conversation *domainchat.Conversation
	messages     []*domainchat.Message
	unread       map[int64]int
	nextID       int64
}

func (r *apiChatRepo) CreateOrGetConversation(_ context.Context, first, second int64) (*domainchat.Conversation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.conversation == nil {
		conversation, err := domainchat.NewConversation(first, second, time.Unix(1, 0))
		if err != nil {
			return nil, err
		}
		conversation.ID = 1
		r.conversation = conversation
		r.unread = map[int64]int{first: 0, second: 0}
	}
	return r.conversation, nil
}

func (r *apiChatRepo) FindConversationByPair(_ context.Context, lower, higher int64) (*domainchat.Conversation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.conversation == nil || r.conversation.LowerUserID != lower || r.conversation.HigherUserID != higher {
		return nil, domainchat.ErrConversationNotFound
	}
	return r.conversation, nil
}

func (r *apiChatRepo) FindConversationsByPairs(_ context.Context, userID int64, otherIDs []int64) (map[int64]*domainchat.Conversation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make(map[int64]*domainchat.Conversation)
	if r.conversation == nil {
		return result, nil
	}
	other := r.conversation.Counterpart(userID)
	for _, otherID := range otherIDs {
		if otherID == other {
			result[otherID] = r.conversation
		}
	}
	return result, nil
}

func (r *apiChatRepo) GetConversationForMember(_ context.Context, conversationID, userID int64) (*domainchat.Conversation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.conversation == nil || r.conversation.ID != conversationID || !r.conversation.Contains(userID) {
		return nil, domainchat.ErrConversationNotFound
	}
	return r.conversation, nil
}

func (r *apiChatRepo) GetConversationItemForMember(_ context.Context, conversationID, userID int64) (*domainchat.ConversationItem, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.conversation == nil || r.conversation.ID != conversationID || !r.conversation.Contains(userID) {
		return nil, domainchat.ErrConversationNotFound
	}
	return &domainchat.ConversationItem{
		Conversation: r.conversation,
		Member: domainchat.RestoreMember(
			conversationID, userID, 0, nil, r.unread[userID], nil, nil, time.Time{}, time.Time{},
		),
		LastMessage: r.messagesLast(),
	}, nil
}

func (r *apiChatRepo) ListConversations(_ context.Context, userID int64, _ *domainchat.ConversationCursor, _ int) ([]*domainchat.ConversationItem, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.conversation == nil || len(r.messages) == 0 {
		return nil, nil
	}
	last := r.messages[len(r.messages)-1]
	return []*domainchat.ConversationItem{{
		Conversation: r.conversation,
		Member: domainchat.RestoreMember(
			r.conversation.ID, userID, 0, nil, r.unread[userID], nil, nil, time.Time{}, time.Time{},
		),
		LastMessage: last,
	}}, nil
}

func (r *apiChatRepo) ListMessages(_ context.Context, conversationID, userID int64, cursor *domainchat.HistoryCursor, _ int) ([]*domainchat.Message, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.conversation == nil || r.conversation.ID != conversationID || !r.conversation.Contains(userID) {
		return nil, domainchat.ErrConversationNotFound
	}
	result := make([]*domainchat.Message, 0, len(r.messages))
	for index := len(r.messages) - 1; index >= 0; index-- {
		message := r.messages[index]
		if cursor == nil || message.ID < cursor.MessageID {
			result = append(result, message)
		}
	}
	return result, nil
}

func (r *apiChatRepo) ListMessagesAfter(_ context.Context, conversationID, userID, afterID int64, _ int) ([]*domainchat.Message, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.conversation == nil || r.conversation.ID != conversationID || !r.conversation.Contains(userID) {
		return nil, domainchat.ErrConversationNotFound
	}
	result := make([]*domainchat.Message, 0, len(r.messages))
	for _, message := range r.messages {
		if message.ID > afterID {
			result = append(result, message)
		}
	}
	return result, nil
}

func (r *apiChatRepo) FindMessageBySenderAndIdempotencyKey(_ context.Context, senderID int64, key string) (*domainchat.Message, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, message := range r.messages {
		if message.SenderID == senderID && message.IdempotencyKey == key {
			return message, nil
		}
	}
	return nil, domainchat.ErrMessageNotFound
}

func (r *apiChatRepo) Send(_ context.Context, input domainchat.SendInput) (*domainchat.Message, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, message := range r.messages {
		if message.SenderID == input.SenderID && message.IdempotencyKey == input.IdempotencyKey {
			if message.ConversationID != input.ConversationID || message.Kind != input.Kind ||
				message.Text != input.Text || message.VideoID != input.VideoID {
				return nil, false, domainchat.ErrIdempotencyConflict
			}
			return message, false, nil
		}
	}
	if !r.conversation.Contains(input.SenderID) {
		return nil, false, domainchat.ErrNotMember
	}
	r.nextID++
	message := domainchat.RestoreMessage(
		r.nextID, input.ConversationID, input.SenderID, input.Kind, input.Text,
		input.VideoID, input.IdempotencyKey, nil, time.Unix(r.nextID+1, 0),
	)
	r.messages = append(r.messages, message)
	r.conversation.LastMessageID = message.ID
	lastMessageAt := message.CreatedAt
	r.conversation.LastMessageAt = &lastMessageAt
	r.unread[r.conversation.Counterpart(input.SenderID)]++
	return message, true, nil
}

func (r *apiChatRepo) MarkRead(_ context.Context, conversationID, userID, throughID int64) (*domainchat.Member, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.conversation == nil || r.conversation.ID != conversationID || !r.conversation.Contains(userID) {
		return nil, domainchat.ErrConversationNotFound
	}
	for _, message := range r.messages {
		if message.ID == throughID {
			r.unread[userID] = 0
			return domainchat.RestoreMember(conversationID, userID, throughID, nil, 0, nil, nil, time.Time{}, time.Time{}), nil
		}
	}
	return nil, domainchat.ErrMessageNotFound
}

func (r *apiChatRepo) CountUnread(_ context.Context, userID int64) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.unread[userID], nil
}

func (r *apiChatRepo) ReconcileUnread(_ context.Context, userID int64) (int, error) {
	return r.CountUnread(context.Background(), userID)
}

var _ domainchat.Repository = (*apiChatRepo)(nil)
var _ domainchat.IncrementalMessageRepository = (*apiChatRepo)(nil)

func (r *apiChatRepo) messagesLast() *domainchat.Message {
	if len(r.messages) == 0 {
		return nil
	}
	return r.messages[len(r.messages)-1]
}

type apiChatAccounts struct{}

func (apiChatAccounts) BatchGetParticipants(_ context.Context, userIDs []int64) (map[int64]*domainchat.Participant, error) {
	result := make(map[int64]*domainchat.Participant, len(userIDs))
	for _, userID := range userIDs {
		result[userID] = domainchat.RestoreParticipant(userID, "user-"+int64String(userID), "", "", true)
	}
	return result, nil
}

type apiChatRelations struct {
	mutual bool
}

func (r apiChatRelations) AreMutuallyFollowing(context.Context, int64, int64) (bool, error) {
	return r.mutual, nil
}

func (apiChatRelations) ListMutualRecipients(context.Context, int64, string, *domainchat.RecipientCursor, int) ([]*domainchat.Recipient, error) {
	return []*domainchat.Recipient{{
		UserID: 2, Nickname: "user-2", FollowedAt: time.Unix(3, 0),
	}}, nil
}

type apiChatVideos struct{}

func (apiChatVideos) ValidatePublicVideo(context.Context, int64) (*domainchat.VideoCard, error) {
	return domainchat.RestoreVideoCard(88, 2, "shared", "/cover", "/media", true), nil
}

func (apiChatVideos) BatchHydratePublicVideos(_ context.Context, videoIDs []int64) (map[int64]*domainchat.VideoCard, error) {
	result := make(map[int64]*domainchat.VideoCard, len(videoIDs))
	for _, videoID := range videoIDs {
		result[videoID] = domainchat.RestoreVideoCard(videoID, 2, "shared", "/cover", "/media", true)
	}
	return result, nil
}

type apiChatNotifications struct{}

func (apiChatNotifications) CountUnread(context.Context, int64) (int, error) {
	return 2, nil
}

func TestChatAPIFlow(t *testing.T) {
	repository := &apiChatRepo{}
	service := applicationchat.New(
		repository, apiChatAccounts{}, apiChatRelations{mutual: true}, apiChatVideos{},
		applicationchat.WithNotificationUnreadReader(apiChatNotifications{}),
	)
	handler := interfaceshttpchat.New(service)
	jwtManager, err := infrajwt.NewManager("chat-api-test-secret", "15m")
	if err != nil {
		t.Fatalf("new JWT manager: %v", err)
	}
	router := server.New()
	auth := interfaceshttpmiddleware.NewJWTAuth(jwtManager)
	api := router.Group("/api", auth)
	api.GET("/chat/users/:targetUserId/eligibility", handler.Eligibility)
	api.POST("/chat/conversations", handler.CreateConversation)
	api.GET("/chat/conversations", handler.ListConversations)
	api.GET("/chat/conversations/:conversationId/messages", handler.History)
	api.POST("/chat/conversations/:conversationId/messages", handler.Send)
	api.PATCH("/chat/conversations/:conversationId/read", handler.MarkRead)
	api.GET("/inbox-stats/unread", handler.InboxUnread)

	token := signTestToken(t, jwtManager, 1)
	eligibility := performJSONRequest(router, http.MethodGet, "/api/chat/users/2/eligibility", "", token)
	requireStatus(t, eligibility, http.StatusOK)
	var eligibilityBody struct {
		Eligible bool `json:"eligible"`
	}
	decodeJSON(t, eligibility, &eligibilityBody)
	if !eligibilityBody.Eligible {
		t.Fatal("expected mutually following users to be eligible")
	}

	created := performJSONRequestWithHeaders(
		router, http.MethodPost, "/api/chat/conversations", `{"target_user_id":2}`,
		ut.Header{Key: "Authorization", Value: "Bearer " + token},
	)
	requireStatus(t, created, http.StatusOK)
	var conversationBody struct {
		ConversationID int64 `json:"conversation_id"`
	}
	decodeJSON(t, created, &conversationBody)
	if conversationBody.ConversationID != 1 {
		t.Fatalf("unexpected conversation response: %+v", conversationBody)
	}

	emptyHistory := performJSONRequest(router, http.MethodGet, "/api/chat/conversations/1/messages", "", token)
	requireStatus(t, emptyHistory, http.StatusOK)
	var emptyHistoryBody struct {
		Items []struct {
			ID int64 `json:"id"`
		} `json:"items"`
		Conversation struct {
			ID          int64 `json:"id"`
			Counterpart struct {
				UserID    int64 `json:"user_id"`
				Available bool  `json:"available"`
			} `json:"counterpart"`
		} `json:"conversation"`
		Eligibility struct {
			Eligible       bool   `json:"eligible"`
			Reason         string `json:"reason"`
			ConversationID int64  `json:"conversation_id"`
		} `json:"eligibility"`
	}
	decodeJSON(t, emptyHistory, &emptyHistoryBody)
	if len(emptyHistoryBody.Items) != 0 ||
		emptyHistoryBody.Conversation.ID != 1 ||
		emptyHistoryBody.Conversation.Counterpart.UserID != 2 ||
		!emptyHistoryBody.Conversation.Counterpart.Available ||
		!emptyHistoryBody.Eligibility.Eligible ||
		emptyHistoryBody.Eligibility.ConversationID != 1 {
		t.Fatalf("unexpected empty history metadata: %+v", emptyHistoryBody)
	}

	sent := performJSONRequestWithHeaders(
		router, http.MethodPost, "/api/chat/conversations/1/messages", `{"kind":"TEXT","text":"hello"}`,
		ut.Header{Key: "Authorization", Value: "Bearer " + token},
		ut.Header{Key: "Idempotency-Key", Value: "chat-key"},
	)
	requireStatus(t, sent, http.StatusCreated)
	var sentBody struct {
		Message struct {
			ID int64 `json:"id"`
		} `json:"message"`
		Created bool `json:"created"`
	}
	decodeJSON(t, sent, &sentBody)
	if sentBody.Message.ID == 0 || !sentBody.Created {
		t.Fatalf("unexpected send response: %+v", sentBody)
	}

	replayed := performJSONRequestWithHeaders(
		router, http.MethodPost, "/api/chat/conversations/1/messages", `{"kind":"TEXT","text":"hello"}`,
		ut.Header{Key: "Authorization", Value: "Bearer " + token},
		ut.Header{Key: "Idempotency-Key", Value: "chat-key"},
	)
	requireStatus(t, replayed, http.StatusOK)
	conflict := performJSONRequestWithHeaders(
		router, http.MethodPost, "/api/chat/conversations/1/messages", `{"kind":"TEXT","text":"different"}`,
		ut.Header{Key: "Authorization", Value: "Bearer " + token},
		ut.Header{Key: "Idempotency-Key", Value: "chat-key"},
	)
	requireStatus(t, conflict, http.StatusConflict)
	second := performJSONRequestWithHeaders(
		router, http.MethodPost, "/api/chat/conversations/1/messages", `{"kind":"TEXT","text":"world"}`,
		ut.Header{Key: "Authorization", Value: "Bearer " + token},
		ut.Header{Key: "Idempotency-Key", Value: "chat-key-2"},
	)
	requireStatus(t, second, http.StatusCreated)

	history := performJSONRequest(router, http.MethodGet, "/api/chat/conversations/1/messages", "", token)
	requireStatus(t, history, http.StatusOK)
	var historyBody struct {
		Items []struct {
			ID int64 `json:"id"`
		} `json:"items"`
		Conversation struct {
			ID int64 `json:"id"`
		} `json:"conversation"`
		Eligibility struct {
			Eligible bool `json:"eligible"`
		} `json:"eligibility"`
	}
	decodeJSON(t, history, &historyBody)
	if len(historyBody.Items) != 2 || historyBody.Items[0].ID <= historyBody.Items[1].ID ||
		historyBody.Conversation.ID != 1 || !historyBody.Eligibility.Eligible {
		t.Fatalf("unexpected history: %+v", historyBody)
	}

	inbox := performJSONRequest(router, http.MethodGet, "/api/inbox-stats/unread", "", token)
	requireStatus(t, inbox, http.StatusOK)
	var inboxBody struct {
		Notification int `json:"notification_unread_count"`
		Chat         int `json:"chat_unread_count"`
		Total        int `json:"total_unread_count"`
	}
	decodeJSON(t, inbox, &inboxBody)
	if inboxBody.Notification != 2 || inboxBody.Chat != 0 || inboxBody.Total != 2 {
		t.Fatalf("unexpected inbox summary: %+v", inboxBody)
	}
}

func TestChatAPIStrictValidationAndEligibility(t *testing.T) {
	repository := &apiChatRepo{}
	service := applicationchat.New(repository, apiChatAccounts{}, apiChatRelations{mutual: false}, apiChatVideos{})
	handler := interfaceshttpchat.New(service)
	jwtManager, err := infrajwt.NewManager("chat-api-validation-secret", "15m")
	if err != nil {
		t.Fatalf("new JWT manager: %v", err)
	}
	router := server.New()
	auth := interfaceshttpmiddleware.NewJWTAuth(jwtManager)
	api := router.Group("/api", auth)
	api.POST("/chat/conversations", handler.CreateConversation)
	api.POST("/chat/conversations/:conversationId/messages", handler.Send)

	token := signTestToken(t, jwtManager, 1)
	notEligible := performJSONRequest(router, http.MethodPost, "/api/chat/conversations", `{"target_user_id":2}`, token)
	requireStatus(t, notEligible, http.StatusConflict)

	strict := performJSONRequestWithHeaders(
		router, http.MethodPost, "/api/chat/conversations/1/messages", `{"kind":"TEXT","text":"hello","extra":true}`,
		ut.Header{Key: "Authorization", Value: "Bearer " + token},
		ut.Header{Key: "Idempotency-Key", Value: "strict-key"},
	)
	requireStatus(t, strict, http.StatusBadRequest)
}
