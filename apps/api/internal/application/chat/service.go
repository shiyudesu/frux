package applicationchat

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"

	domainchat "github.com/shiyudesu/frux/internal/domain/chat"
)

var (
	ErrLoadChatFailed   = errors.New("failed to load chat")
	ErrSaveChatFailed   = errors.New("failed to save chat")
	ErrUpdateChatFailed = errors.New("failed to update chat")
)

type AccountReader interface {
	BatchGetParticipants(ctx context.Context, userIDs []int64) (map[int64]*domainchat.Participant, error)
}

type MutualFollowReader interface {
	AreMutuallyFollowing(ctx context.Context, firstUserID, secondUserID int64) (bool, error)
	ListMutualRecipients(ctx context.Context, userID int64, query string, cursor *domainchat.RecipientCursor, limit int) ([]*domainchat.Recipient, error)
}

type VideoReader interface {
	ValidatePublicVideo(ctx context.Context, videoID int64) (*domainchat.VideoCard, error)
	BatchHydratePublicVideos(ctx context.Context, videoIDs []int64) (map[int64]*domainchat.VideoCard, error)
}

type NotificationUnreadReader interface {
	CountUnread(ctx context.Context, userID int64) (int, error)
}

type Observer interface {
	Observe(operation, kind, outcome, errorClass string, latency time.Duration)
}

type Option func(*Service)

type Service struct {
	repo          domainchat.Repository
	accounts      AccountReader
	relations     MutualFollowReader
	videos        VideoReader
	notifications NotificationUnreadReader
	observer      Observer
	now           func() time.Time
}

type EligibilityResult struct {
	Eligible       bool
	Reason         domainchat.EligibilityReason
	ConversationID int64
}

type RecipientListResult struct {
	Items      []*domainchat.Recipient
	NextCursor string
	HasMore    bool
}

type ConversationView struct {
	ID                 int64
	Counterpart        *domainchat.Participant
	LastMessageID      int64
	LastMessageKind    domainchat.MessageKind
	LastMessagePreview string
	LastMessageAt      *time.Time
	UnreadCount        int
}

type ConversationListResult struct {
	Items      []*ConversationView
	NextCursor string
	HasMore    bool
}

type MessageView struct {
	ID             int64
	ConversationID int64
	Sender         *domainchat.Participant
	Kind           domainchat.MessageKind
	Text           string
	Video          *domainchat.VideoCard
	CreatedAt      time.Time
}

type HistoryResult struct {
	Items        []*MessageView
	NextCursor   string
	HasMore      bool
	Conversation *ConversationView
	Eligibility  *EligibilityResult
}

type SendResult struct {
	Message *MessageView
	Created bool
}

type ReadResult struct {
	LastReadMessageID int64
	UnreadCount       int
}

type InboxUnreadSummary struct {
	NotificationUnreadCount int
	ChatUnreadCount         int
	TotalUnreadCount        int
}

func New(repo domainchat.Repository, accounts AccountReader, relations MutualFollowReader, videos VideoReader, options ...Option) *Service {
	service := &Service{
		repo:      repo,
		accounts:  accounts,
		relations: relations,
		videos:    videos,
		now:       time.Now,
		observer:  noopObserver{},
	}
	for _, option := range options {
		if option != nil {
			option(service)
		}
	}
	return service
}

func WithNotificationUnreadReader(reader NotificationUnreadReader) Option {
	return func(service *Service) {
		service.notifications = reader
	}
}

func WithObserver(observer Observer) Option {
	return func(service *Service) {
		if observer != nil {
			service.observer = observer
		}
	}
}

func WithClock(now func() time.Time) Option {
	return func(service *Service) {
		if now != nil {
			service.now = now
		}
	}
}

func (s *Service) Eligibility(ctx context.Context, userID, targetUserID int64) (*EligibilityResult, error) {
	start := s.now()
	if userID <= 0 {
		return nil, domainchat.ErrInvalidUserID
	}
	if targetUserID <= 0 {
		return nil, domainchat.ErrInvalidTargetUserID
	}
	if userID == targetUserID {
		return &EligibilityResult{Eligible: false, Reason: domainchat.EligibilityReasonSelf}, nil
	}

	available, availabilityErr := s.accountsAvailable(ctx, userID, targetUserID)
	if availabilityErr != nil {
		return nil, s.observeError("eligibility", "", ErrLoadChatFailed, start)
	}
	if !available {
		return &EligibilityResult{
			Eligible: false, Reason: domainchat.EligibilityReasonAccountUnavailable,
		}, nil
	}

	lower, higher, err := domainchat.CanonicalPair(userID, targetUserID)
	if err != nil {
		return nil, err
	}
	conversationID := int64(0)
	if conversation, findErr := s.repo.FindConversationByPair(ctx, lower, higher); findErr == nil && conversation != nil {
		conversationID = conversation.ID
	} else if findErr != nil && !errors.Is(findErr, domainchat.ErrConversationNotFound) {
		return nil, s.observeError("eligibility", "", ErrLoadChatFailed, start)
	}

	mutual, err := s.relations.AreMutuallyFollowing(ctx, userID, targetUserID)
	if err != nil {
		return nil, s.observeError("eligibility", "", ErrLoadChatFailed, start)
	}
	if !mutual {
		return &EligibilityResult{
			Eligible: false, Reason: domainchat.EligibilityReasonNotMutual,
			ConversationID: conversationID,
		}, nil
	}
	result := &EligibilityResult{
		Eligible: true, Reason: domainchat.EligibilityReasonEligible,
		ConversationID: conversationID,
	}
	s.observe("eligibility", "", "success", "", start)
	return result, nil
}

func (s *Service) CreateConversation(ctx context.Context, userID, targetUserID int64) (*domainchat.Conversation, error) {
	start := s.now()
	eligibility, err := s.Eligibility(ctx, userID, targetUserID)
	if err != nil {
		return nil, err
	}
	if !eligibility.Eligible {
		switch eligibility.Reason {
		case domainchat.EligibilityReasonSelf:
			return nil, s.observeError("create_conversation", "", domainchat.ErrSelfConversation, start)
		case domainchat.EligibilityReasonAccountUnavailable:
			return nil, s.observeError("create_conversation", "", domainchat.ErrAccountUnavailable, start)
		default:
			return nil, s.observeError("create_conversation", "", domainchat.ErrNotEligible, start)
		}
	}
	conversation, err := s.repo.CreateOrGetConversation(ctx, userID, targetUserID)
	if err != nil {
		return nil, s.observeError("create_conversation", "", ErrSaveChatFailed, start)
	}
	s.observe("create_conversation", "", "success", "", start)
	return conversation, nil
}

func (s *Service) ListRecipients(ctx context.Context, userID int64, query, rawCursor string, limit int) (*RecipientListResult, error) {
	start := s.now()
	if userID <= 0 {
		return nil, domainchat.ErrInvalidUserID
	}
	normalizedQuery, err := domainchat.NormalizeQuery(query)
	if err != nil {
		return nil, err
	}
	cursor, err := decodeRecipientCursor(rawCursor, normalizedQuery)
	if err != nil {
		return nil, err
	}
	limit = domainchat.NormalizeLimit(limit)
	items, err := s.relations.ListMutualRecipients(ctx, userID, normalizedQuery, cursor, limit+1)
	if err != nil {
		return nil, s.observeError("list_recipients", "", ErrLoadChatFailed, start)
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	otherIDs := make([]int64, 0, len(items))
	for _, item := range items {
		if item != nil {
			otherIDs = append(otherIDs, item.UserID)
		}
	}
	conversations, err := s.repo.FindConversationsByPairs(ctx, userID, otherIDs)
	if err != nil {
		return nil, s.observeError("list_recipients", "", ErrLoadChatFailed, start)
	}
	for _, item := range items {
		if item == nil {
			continue
		}
		if conversation := conversations[item.UserID]; conversation != nil {
			item.ConversationID = conversation.ID
		}
	}
	nextCursor := ""
	if len(items) > 0 {
		last := items[len(items)-1]
		nextCursor = encodeRecipientCursor(&domainchat.RecipientCursor{
			Version: domainchat.CursorVersion, Query: normalizedQuery,
			FollowedAt: last.FollowedAt, UserID: last.UserID,
		})
	}
	s.observe("list_recipients", "", "success", "", start)
	return &RecipientListResult{Items: items, NextCursor: nextCursor, HasMore: hasMore}, nil
}

func (s *Service) ListConversations(ctx context.Context, userID int64, rawCursor string, limit int) (*ConversationListResult, error) {
	start := s.now()
	if userID <= 0 {
		return nil, domainchat.ErrInvalidUserID
	}
	cursor, err := decodeConversationCursor(rawCursor)
	if err != nil {
		return nil, err
	}
	limit = domainchat.NormalizeLimit(limit)
	items, err := s.repo.ListConversations(ctx, userID, cursor, limit+1)
	if err != nil {
		return nil, s.observeError("list_conversations", "", ErrLoadChatFailed, start)
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	counterpartIDs := make([]int64, 0, len(items))
	for _, item := range items {
		if item != nil && item.Conversation != nil {
			counterpartIDs = append(counterpartIDs, item.Conversation.Counterpart(userID))
		}
	}
	participants, err := s.accounts.BatchGetParticipants(ctx, counterpartIDs)
	if err != nil {
		return nil, s.observeError("list_conversations", "", ErrLoadChatFailed, start)
	}
	views := make([]*ConversationView, 0, len(items))
	for _, item := range items {
		if item == nil || item.Conversation == nil || item.Member == nil {
			continue
		}
		counterpartID := item.Conversation.Counterpart(userID)
		counterpart := participants[counterpartID]
		if counterpart == nil {
			counterpart = domainchat.UnavailableParticipant(counterpartID)
		}
		view := &ConversationView{
			ID: item.Conversation.ID, Counterpart: counterpart,
			LastMessageID: item.Conversation.LastMessageID,
			LastMessageAt: cloneTime(item.Conversation.LastMessageAt),
			UnreadCount:   item.Member.UnreadCount,
		}
		if item.LastMessage != nil && item.LastMessage.ConversationID == item.Conversation.ID {
			view.LastMessageKind = item.LastMessage.Kind
			if item.LastMessage.Kind == domainchat.MessageKindText {
				view.LastMessagePreview = item.LastMessage.Text
			} else {
				view.LastMessagePreview = "视频"
			}
		}
		views = append(views, view)
	}
	nextCursor := ""
	if len(views) > 0 {
		last := views[len(views)-1]
		nextCursor = encodeConversationCursor(&domainchat.ConversationCursor{
			Version: domainchat.CursorVersion, LastMessageID: last.LastMessageID, ConversationID: last.ID,
		})
	}
	s.observe("list_conversations", "", "success", "", start)
	return &ConversationListResult{Items: views, NextCursor: nextCursor, HasMore: hasMore}, nil
}

func (s *Service) ListHistory(ctx context.Context, userID, conversationID int64, rawCursor string, limit int, afterMessageID ...int64) (*HistoryResult, error) {
	start := s.now()
	if userID <= 0 {
		return nil, domainchat.ErrInvalidUserID
	}
	if conversationID <= 0 {
		return nil, domainchat.ErrInvalidConversationID
	}
	if len(afterMessageID) > 0 && afterMessageID[0] > 0 && strings.TrimSpace(rawCursor) != "" {
		return nil, domainchat.ErrInvalidCursor
	}
	cursor, err := decodeHistoryCursor(rawCursor, conversationID)
	if err != nil {
		return nil, err
	}
	if s.accounts == nil {
		return nil, s.observeError("history", "", ErrLoadChatFailed, start)
	}
	limit = domainchat.NormalizeLimit(limit)
	conversationView, eligibility, err := s.historyMetadata(ctx, userID, conversationID)
	if err != nil {
		return nil, mapServiceReadError(err, start, s)
	}
	var messages []*domainchat.Message
	if len(afterMessageID) > 0 && afterMessageID[0] > 0 {
		incrementalRepo, ok := s.repo.(domainchat.IncrementalMessageRepository)
		if !ok {
			return nil, s.observeError("history", "", ErrLoadChatFailed, start)
		}
		messages, err = incrementalRepo.ListMessagesAfter(ctx, conversationID, userID, afterMessageID[0], limit)
	} else {
		messages, err = s.repo.ListMessages(ctx, conversationID, userID, cursor, limit+1)
	}
	if err != nil {
		return nil, mapServiceReadError(err, start, s)
	}
	hasMore := len(messages) > limit
	if len(afterMessageID) == 0 && hasMore {
		messages = messages[:limit]
	}
	if len(afterMessageID) > 0 && afterMessageID[0] > 0 {
		hasMore = false
	}
	senderIDs := make([]int64, 0, len(messages))
	videoIDs := make([]int64, 0, len(messages))
	for _, message := range messages {
		if message == nil {
			continue
		}
		senderIDs = append(senderIDs, message.SenderID)
		if message.Kind == domainchat.MessageKindVideo && message.VideoID > 0 {
			videoIDs = append(videoIDs, message.VideoID)
		}
	}
	participants, err := s.accounts.BatchGetParticipants(ctx, senderIDs)
	if err != nil {
		return nil, s.observeError("history", "", ErrLoadChatFailed, start)
	}
	if participants == nil {
		participants = make(map[int64]*domainchat.Participant)
	}
	videos := make(map[int64]*domainchat.VideoCard)
	if len(videoIDs) > 0 && s.videos != nil {
		videos, err = s.videos.BatchHydratePublicVideos(ctx, uniqueIDs(videoIDs))
		if err != nil {
			return nil, s.observeError("history", "VIDEO", ErrLoadChatFailed, start)
		}
	}
	displayIDs := append([]int64{}, senderIDs...)
	for _, video := range videos {
		if video != nil && video.Available && video.AuthorID > 0 {
			displayIDs = append(displayIDs, video.AuthorID)
		}
	}
	if len(displayIDs) > len(senderIDs) {
		authorDisplays, displayErr := s.accounts.BatchGetParticipants(ctx, uniqueIDs(displayIDs))
		if displayErr != nil {
			return nil, s.observeError("history", "", ErrLoadChatFailed, start)
		}
		for userID, participant := range authorDisplays {
			if _, exists := participants[userID]; !exists {
				participants[userID] = participant
			}
		}
	}
	views := make([]*MessageView, 0, len(messages))
	for _, message := range messages {
		if message == nil {
			continue
		}
		sender := participants[message.SenderID]
		if sender == nil {
			sender = domainchat.UnavailableParticipant(message.SenderID)
		}
		view := &MessageView{
			ID: message.ID, ConversationID: message.ConversationID, Sender: sender,
			Kind: message.Kind, Text: message.Text, CreatedAt: message.CreatedAt,
		}
		if message.Kind == domainchat.MessageKindVideo {
			video := videos[message.VideoID]
			if video == nil || !video.Available {
				video = domainchat.UnavailableVideoCard(message.VideoID)
			} else {
				if author := participants[video.AuthorID]; author != nil && author.Available {
					video.AuthorNickname = author.Nickname
					video.AuthorAvatarURL = author.AvatarURL
				}
			}
			view.Video = video
		}
		views = append(views, view)
	}
	nextCursor := ""
	if len(messages) > 0 {
		nextCursor = encodeHistoryCursor(&domainchat.HistoryCursor{
			Version: domainchat.CursorVersion, ConversationID: conversationID, MessageID: messages[len(messages)-1].ID,
		})
	}
	s.observe("history", "", "success", "", start)
	return &HistoryResult{
		Items: views, NextCursor: nextCursor, HasMore: hasMore,
		Conversation: conversationView, Eligibility: eligibility,
	}, nil
}

func (s *Service) SendText(ctx context.Context, userID, conversationID int64, text, idempotencyKey string) (*SendResult, error) {
	message, err := domainchat.NewTextMessage(conversationID, userID, text, idempotencyKey, s.now())
	if err != nil {
		return nil, err
	}
	return s.send(ctx, message)
}

func (s *Service) SendVideo(ctx context.Context, userID, conversationID, videoID int64, idempotencyKey string) (*SendResult, error) {
	message, err := domainchat.NewVideoMessage(conversationID, userID, videoID, idempotencyKey, s.now())
	if err != nil {
		return nil, err
	}
	return s.send(ctx, message)
}

func (s *Service) send(ctx context.Context, message *domainchat.Message) (*SendResult, error) {
	start := s.now()
	if message == nil {
		return nil, domainchat.ErrInvalidMessageShape
	}
	if committed, found, err := s.resolveCommittedRetry(ctx, message, start); err != nil {
		return nil, err
	} else if found {
		s.observe("send", string(message.Kind), "success", "", start)
		return committed, nil
	}
	if message.Kind == domainchat.MessageKindVideo {
		if err := s.validateNewVideo(ctx, message.VideoID, start); err != nil {
			return nil, err
		}
	}
	conversation, err := s.repo.GetConversationForMember(ctx, message.ConversationID, message.SenderID)
	if err != nil {
		return nil, mapServiceReadError(err, start, s)
	}
	counterpartID := conversation.Counterpart(message.SenderID)
	available, availabilityErr := s.accountsAvailable(ctx, message.SenderID, counterpartID)
	if availabilityErr != nil {
		return nil, s.observeError("send", string(message.Kind), ErrLoadChatFailed, start)
	}
	if !available {
		return nil, s.observeError("send", string(message.Kind), domainchat.ErrAccountUnavailable, start)
	}
	if s.relations == nil {
		return nil, s.observeError("send", string(message.Kind), ErrLoadChatFailed, start)
	}
	mutual, err := s.relations.AreMutuallyFollowing(ctx, message.SenderID, counterpartID)
	if err != nil {
		return nil, s.observeError("send", string(message.Kind), ErrLoadChatFailed, start)
	}
	if !mutual {
		return nil, s.observeError("send", string(message.Kind), domainchat.ErrNotEligible, start)
	}
	persisted, created, err := s.repo.Send(ctx, domainchat.SendInput{
		ConversationID: message.ConversationID, SenderID: message.SenderID,
		Kind: message.Kind, Text: message.Text, VideoID: message.VideoID,
		IdempotencyKey: message.IdempotencyKey,
	})
	if err != nil {
		if errors.Is(err, domainchat.ErrIdempotencyConflict) {
			return nil, s.observeError("send", string(message.Kind), err, start)
		}
		return nil, s.observeError("send", string(message.Kind), ErrSaveChatFailed, start)
	}
	view, err := s.messageView(ctx, persisted)
	if err != nil {
		return nil, err
	}
	s.observe("send", string(message.Kind), "success", "", start)
	return &SendResult{Message: view, Created: created}, nil
}

func (s *Service) resolveCommittedRetry(ctx context.Context, message *domainchat.Message, start time.Time) (*SendResult, bool, error) {
	existing, err := s.repo.FindMessageBySenderAndIdempotencyKey(ctx, message.SenderID, message.IdempotencyKey)
	if errors.Is(err, domainchat.ErrMessageNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, s.observeError("send", string(message.Kind), ErrLoadChatFailed, start)
	}
	if existing == nil {
		return nil, false, nil
	}
	if !existing.SameSendPayload(message) {
		return nil, false, s.observeError("send", string(message.Kind), domainchat.ErrIdempotencyConflict, start)
	}
	view, err := s.messageView(ctx, existing)
	if err != nil {
		return nil, false, s.observeError("send", string(message.Kind), ErrLoadChatFailed, start)
	}
	return &SendResult{Message: view, Created: false}, true, nil
}

func (s *Service) validateNewVideo(ctx context.Context, videoID int64, start time.Time) error {
	if s.videos == nil {
		return s.observeError("send", string(domainchat.MessageKindVideo), domainchat.ErrVideoUnavailable, start)
	}
	video, err := s.videos.ValidatePublicVideo(ctx, videoID)
	if err != nil {
		if errors.Is(err, domainchat.ErrVideoUnavailable) {
			return s.observeError("send", string(domainchat.MessageKindVideo), err, start)
		}
		return s.observeError("send", string(domainchat.MessageKindVideo), ErrLoadChatFailed, start)
	}
	if video == nil || !video.Available {
		return s.observeError("send", string(domainchat.MessageKindVideo), domainchat.ErrVideoUnavailable, start)
	}
	return nil
}

func (s *Service) historyMetadata(ctx context.Context, userID, conversationID int64) (*ConversationView, *EligibilityResult, error) {
	item, err := s.repo.GetConversationItemForMember(ctx, conversationID, userID)
	if err != nil {
		return nil, nil, err
	}
	if item == nil || item.Conversation == nil {
		return nil, nil, domainchat.ErrConversationNotFound
	}
	counterpartID := item.Conversation.Counterpart(userID)
	if counterpartID <= 0 {
		return nil, nil, domainchat.ErrNotMember
	}
	participants := make(map[int64]*domainchat.Participant)
	if s.accounts != nil {
		var err error
		participants, err = s.accounts.BatchGetParticipants(ctx, []int64{userID, counterpartID})
		if err != nil {
			return nil, nil, err
		}
	}
	counterpart := participants[counterpartID]
	if counterpart == nil {
		counterpart = domainchat.UnavailableParticipant(counterpartID)
	}
	view := &ConversationView{
		ID: item.Conversation.ID, Counterpart: counterpart,
		LastMessageID: item.Conversation.LastMessageID,
		LastMessageAt: cloneTime(item.Conversation.LastMessageAt),
	}
	if item.Member != nil {
		view.UnreadCount = item.Member.UnreadCount
	}
	if item.LastMessage != nil {
		view.LastMessageKind = item.LastMessage.Kind
		if item.LastMessage.Kind == domainchat.MessageKindText {
			view.LastMessagePreview = item.LastMessage.Text
		} else {
			view.LastMessagePreview = "视频"
		}
	}
	eligibility := &EligibilityResult{
		Eligible: false, Reason: domainchat.EligibilityReasonAccountUnavailable,
		ConversationID: item.Conversation.ID,
	}
	current := participants[userID]
	if current != nil && current.Available && counterpart.Available {
		if s.relations == nil {
			return view, eligibility, nil
		}
		mutual, err := s.relations.AreMutuallyFollowing(ctx, userID, counterpartID)
		if err != nil {
			return nil, nil, err
		}
		if mutual {
			eligibility.Eligible = true
			eligibility.Reason = domainchat.EligibilityReasonEligible
		} else {
			eligibility.Reason = domainchat.EligibilityReasonNotMutual
		}
	}
	return view, eligibility, nil
}

func (s *Service) MarkRead(ctx context.Context, userID, conversationID, throughMessageID int64) (*ReadResult, error) {
	start := s.now()
	if userID <= 0 {
		return nil, domainchat.ErrInvalidUserID
	}
	if conversationID <= 0 {
		return nil, domainchat.ErrInvalidConversationID
	}
	if throughMessageID <= 0 {
		return nil, domainchat.ErrInvalidMessageID
	}
	member, err := s.repo.MarkRead(ctx, conversationID, userID, throughMessageID)
	if err != nil {
		if errors.Is(err, domainchat.ErrMessageNotFound) ||
			errors.Is(err, domainchat.ErrConversationNotFound) {
			return nil, err
		}
		return nil, s.observeError("mark_read", "", ErrUpdateChatFailed, start)
	}
	s.observe("mark_read", "", "success", "", start)
	return &ReadResult{LastReadMessageID: member.LastReadMessageID, UnreadCount: member.UnreadCount}, nil
}

func (s *Service) InboxUnread(ctx context.Context, userID int64) (*InboxUnreadSummary, error) {
	start := s.now()
	if userID <= 0 {
		return nil, domainchat.ErrInvalidUserID
	}
	chatCount, err := s.repo.CountUnread(ctx, userID)
	if err != nil {
		return nil, s.observeError("inbox_unread", "", ErrLoadChatFailed, start)
	}
	notificationCount := 0
	if s.notifications != nil {
		notificationCount, err = s.notifications.CountUnread(ctx, userID)
		if err != nil {
			return nil, s.observeError("inbox_unread", "", ErrLoadChatFailed, start)
		}
	}
	s.observe("inbox_unread", "", "success", "", start)
	return &InboxUnreadSummary{
		NotificationUnreadCount: notificationCount,
		ChatUnreadCount:         chatCount,
		TotalUnreadCount:        notificationCount + chatCount,
	}, nil
}

func (s *Service) messageView(ctx context.Context, message *domainchat.Message) (*MessageView, error) {
	if message == nil {
		return nil, ErrLoadChatFailed
	}
	if s.accounts == nil {
		return nil, ErrLoadChatFailed
	}
	participants, err := s.accounts.BatchGetParticipants(ctx, []int64{message.SenderID})
	if err != nil {
		return nil, ErrLoadChatFailed
	}
	sender := participants[message.SenderID]
	if sender == nil {
		sender = domainchat.UnavailableParticipant(message.SenderID)
	}
	view := &MessageView{
		ID: message.ID, ConversationID: message.ConversationID, Sender: sender,
		Kind: message.Kind, Text: message.Text, CreatedAt: message.CreatedAt,
	}
	if message.Kind == domainchat.MessageKindVideo {
		if s.videos == nil {
			view.Video = domainchat.UnavailableVideoCard(message.VideoID)
		} else {
			video, videoErr := s.videos.BatchHydratePublicVideos(ctx, []int64{message.VideoID})
			if videoErr != nil || video[message.VideoID] == nil || !video[message.VideoID].Available {
				view.Video = domainchat.UnavailableVideoCard(message.VideoID)
			} else {
				view.Video = video[message.VideoID]
				s.hydrateVideoAuthor(ctx, view.Video)
			}
		}
	}
	return view, nil
}

func (s *Service) hydrateVideoAuthor(ctx context.Context, video *domainchat.VideoCard) {
	if video == nil || !video.Available || video.AuthorID <= 0 {
		return
	}
	participants, err := s.accounts.BatchGetParticipants(ctx, []int64{video.AuthorID})
	if err != nil {
		return
	}
	author := participants[video.AuthorID]
	if author == nil || !author.Available {
		return
	}
	video.AuthorNickname = author.Nickname
	video.AuthorAvatarURL = author.AvatarURL
}

func (s *Service) accountsAvailable(ctx context.Context, userIDs ...int64) (bool, error) {
	if s.accounts == nil {
		return false, domainchat.ErrAccountUnavailable
	}
	participants, err := s.accounts.BatchGetParticipants(ctx, uniqueIDs(userIDs))
	if err != nil {
		return false, err
	}
	for _, userID := range userIDs {
		participant := participants[userID]
		if participant == nil || !participant.Available {
			return false, nil
		}
	}
	return true, nil
}

func (s *Service) observe(operation, kind, outcome, errorClass string, start time.Time) {
	if s.observer == nil {
		return
	}
	s.observer.Observe(operation, kind, outcome, errorClass, time.Since(start))
}

func (s *Service) observeError(operation, kind string, err error, start time.Time) error {
	errorClass := "internal"
	switch {
	case errors.Is(err, domainchat.ErrNotEligible):
		errorClass = "not_eligible"
	case errors.Is(err, domainchat.ErrAccountUnavailable):
		errorClass = "account_unavailable"
	case errors.Is(err, domainchat.ErrIdempotencyConflict):
		errorClass = "idempotency_conflict"
	case errors.Is(err, domainchat.ErrVideoUnavailable):
		errorClass = "video_unavailable"
	case errors.Is(err, domainchat.ErrConversationNotFound):
		errorClass = "conversation_not_found"
	}
	s.observe(operation, kind, "error", errorClass, start)
	return err
}

func mapServiceReadError(err error, start time.Time, service *Service) error {
	switch {
	case errors.Is(err, domainchat.ErrConversationNotFound),
		errors.Is(err, domainchat.ErrNotMember),
		errors.Is(err, domainchat.ErrInvalidCursor),
		errors.Is(err, domainchat.ErrMessageNotFound):
		return service.observeError("read", "", err, start)
	default:
		return service.observeError("read", "", ErrLoadChatFailed, start)
	}
}

func decodeConversationCursor(raw string) (*domainchat.ConversationCursor, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var payload conversationCursorPayload
	content, err := decodeCursor(raw)
	if err != nil || json.Unmarshal(content, &payload) != nil ||
		payload.Version != domainchat.CursorVersion ||
		payload.LastMessageID <= 0 || payload.ConversationID <= 0 {
		return nil, domainchat.ErrInvalidCursor
	}
	return &domainchat.ConversationCursor{
		Version: payload.Version, LastMessageID: payload.LastMessageID, ConversationID: payload.ConversationID,
	}, nil
}

func decodeHistoryCursor(raw string, conversationID int64) (*domainchat.HistoryCursor, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var payload historyCursorPayload
	content, err := decodeCursor(raw)
	if err != nil || json.Unmarshal(content, &payload) != nil ||
		payload.Version != domainchat.CursorVersion ||
		payload.ConversationID != conversationID || payload.MessageID <= 0 {
		return nil, domainchat.ErrInvalidCursor
	}
	return &domainchat.HistoryCursor{
		Version: payload.Version, ConversationID: payload.ConversationID, MessageID: payload.MessageID,
	}, nil
}

func decodeRecipientCursor(raw, query string) (*domainchat.RecipientCursor, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var payload recipientCursorPayload
	content, err := decodeCursor(raw)
	if err != nil || json.Unmarshal(content, &payload) != nil ||
		payload.Version != domainchat.CursorVersion || payload.Query != query ||
		payload.UserID <= 0 {
		return nil, domainchat.ErrInvalidCursor
	}
	followedAt, err := time.Parse(time.RFC3339Nano, payload.FollowedAt)
	if err != nil || followedAt.IsZero() {
		return nil, domainchat.ErrInvalidCursor
	}
	return &domainchat.RecipientCursor{
		Version: payload.Version, Query: payload.Query, FollowedAt: followedAt, UserID: payload.UserID,
	}, nil
}

type conversationCursorPayload struct {
	Version        int   `json:"v"`
	LastMessageID  int64 `json:"last_message_id"`
	ConversationID int64 `json:"conversation_id"`
}

type historyCursorPayload struct {
	Version        int   `json:"v"`
	ConversationID int64 `json:"conversation_id"`
	MessageID      int64 `json:"message_id"`
}

type recipientCursorPayload struct {
	Version    int    `json:"v"`
	Query      string `json:"query"`
	FollowedAt string `json:"followed_at"`
	UserID     int64  `json:"user_id"`
}

func encodeConversationCursor(cursor *domainchat.ConversationCursor) string {
	if cursor == nil || cursor.LastMessageID <= 0 || cursor.ConversationID <= 0 {
		return ""
	}
	content, err := json.Marshal(conversationCursorPayload{
		Version: domainchat.CursorVersion, LastMessageID: cursor.LastMessageID, ConversationID: cursor.ConversationID,
	})
	if err != nil {
		return ""
	}
	return encodeCursor(content)
}

func encodeHistoryCursor(cursor *domainchat.HistoryCursor) string {
	if cursor == nil || cursor.ConversationID <= 0 || cursor.MessageID <= 0 {
		return ""
	}
	content, err := json.Marshal(historyCursorPayload{
		Version: domainchat.CursorVersion, ConversationID: cursor.ConversationID, MessageID: cursor.MessageID,
	})
	if err != nil {
		return ""
	}
	return encodeCursor(content)
}

func encodeRecipientCursor(cursor *domainchat.RecipientCursor) string {
	if cursor == nil || cursor.UserID <= 0 || cursor.FollowedAt.IsZero() {
		return ""
	}
	content, err := json.Marshal(recipientCursorPayload{
		Version: domainchat.CursorVersion, Query: cursor.Query,
		FollowedAt: cursor.FollowedAt.UTC().Format(time.RFC3339Nano), UserID: cursor.UserID,
	})
	if err != nil {
		return ""
	}
	return encodeCursor(content)
}

func encodeCursor(content []byte) string {
	return base64.RawURLEncoding.EncodeToString(content)
}

func decodeCursor(raw string) ([]byte, error) {
	content, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(raw))
	if err != nil {
		content, err = base64.StdEncoding.DecodeString(strings.TrimSpace(raw))
		if err != nil {
			return nil, err
		}
	}
	return content, nil
}

func uniqueIDs(values []int64) []int64 {
	result := make([]int64, 0, len(values))
	seen := make(map[int64]struct{}, len(values))
	for _, value := range values {
		if value <= 0 {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copy := value.UTC()
	return &copy
}

type noopObserver struct{}

func (noopObserver) Observe(string, string, string, string, time.Duration) {}

var _ Observer = noopObserver{}
