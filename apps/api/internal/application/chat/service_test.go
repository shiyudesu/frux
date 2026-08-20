package applicationchat

import (
	"context"
	"errors"
	"testing"
	"time"

	domainchat "github.com/shiyudesu/frux/internal/domain/chat"
)

type fakeChatRepo struct {
	conversation *domainchat.Conversation
	message      *domainchat.Message
	unread       int
	created      bool
}

func (r *fakeChatRepo) CreateOrGetConversation(_ context.Context, first, second int64) (*domainchat.Conversation, error) {
	if r.conversation == nil {
		conversation, err := domainchat.NewConversation(first, second, time.Unix(1, 0))
		if err != nil {
			return nil, err
		}
		conversation.ID = 7
		r.conversation = conversation
	}
	return r.conversation, nil
}

func (r *fakeChatRepo) FindConversationByPair(_ context.Context, lower, higher int64) (*domainchat.Conversation, error) {
	if r.conversation == nil || r.conversation.LowerUserID != lower || r.conversation.HigherUserID != higher {
		return nil, domainchat.ErrConversationNotFound
	}
	return r.conversation, nil
}

func (r *fakeChatRepo) FindConversationsByPairs(_ context.Context, _ int64, _ []int64) (map[int64]*domainchat.Conversation, error) {
	return map[int64]*domainchat.Conversation{}, nil
}

func (r *fakeChatRepo) GetConversationForMember(_ context.Context, conversationID, userID int64) (*domainchat.Conversation, error) {
	if r.conversation == nil || r.conversation.ID != conversationID || !r.conversation.Contains(userID) {
		return nil, domainchat.ErrConversationNotFound
	}
	return r.conversation, nil
}

func (r *fakeChatRepo) GetConversationItemForMember(_ context.Context, conversationID, userID int64) (*domainchat.ConversationItem, error) {
	if r.conversation == nil || r.conversation.ID != conversationID || !r.conversation.Contains(userID) {
		return nil, domainchat.ErrConversationNotFound
	}
	return &domainchat.ConversationItem{
		Conversation: r.conversation,
		Member: domainchat.RestoreMember(
			conversationID, userID, 0, nil, r.unread, nil, nil, time.Time{}, time.Time{},
		),
		LastMessage: r.message,
	}, nil
}

func (r *fakeChatRepo) ListConversations(_ context.Context, _ int64, _ *domainchat.ConversationCursor, _ int) ([]*domainchat.ConversationItem, error) {
	if r.conversation == nil || r.message == nil {
		return nil, nil
	}
	return []*domainchat.ConversationItem{{
		Conversation: r.conversation,
		Member:       domainchat.RestoreMember(r.conversation.ID, 1, 0, nil, r.unread, nil, nil, time.Time{}, time.Time{}),
		LastMessage:  r.message,
	}}, nil
}

func (r *fakeChatRepo) ListMessages(_ context.Context, conversationID, userID int64, cursor *domainchat.HistoryCursor, _ int) ([]*domainchat.Message, error) {
	if r.message == nil || r.conversation == nil || conversationID != r.message.ConversationID || !r.conversation.Contains(userID) {
		return nil, domainchat.ErrConversationNotFound
	}
	if cursor != nil && cursor.ConversationID != conversationID {
		return nil, domainchat.ErrInvalidCursor
	}
	return []*domainchat.Message{r.message}, nil
}

func (r *fakeChatRepo) FindMessageBySenderAndIdempotencyKey(_ context.Context, senderID int64, key string) (*domainchat.Message, error) {
	if r.message == nil || r.message.SenderID != senderID || r.message.IdempotencyKey != key {
		return nil, domainchat.ErrMessageNotFound
	}
	return r.message, nil
}

func (r *fakeChatRepo) Send(_ context.Context, input domainchat.SendInput) (*domainchat.Message, bool, error) {
	if r.message != nil {
		return r.message, false, nil
	}
	r.message = domainchat.RestoreMessage(11, input.ConversationID, input.SenderID, input.Kind, input.Text, input.VideoID, input.IdempotencyKey, nil, time.Unix(2, 0))
	r.created = true
	return r.message, true, nil
}

func (r *fakeChatRepo) MarkRead(_ context.Context, conversationID, userID, through int64) (*domainchat.Member, error) {
	if r.conversation == nil || r.conversation.ID != conversationID || userID <= 0 || through <= 0 {
		return nil, domainchat.ErrConversationNotFound
	}
	member := domainchat.RestoreMember(conversationID, userID, through, nil, 0, nil, nil, time.Time{}, time.Time{})
	return member, nil
}

func (r *fakeChatRepo) CountUnread(context.Context, int64) (int, error) {
	return r.unread, nil
}

func (r *fakeChatRepo) ReconcileUnread(context.Context, int64) (int, error) {
	return r.unread, nil
}

var _ domainchat.Repository = (*fakeChatRepo)(nil)

type fakeAccountReader struct {
	available map[int64]*domainchat.Participant
}

func (r fakeAccountReader) GetParticipant(_ context.Context, userID int64) (*domainchat.Participant, error) {
	return r.available[userID], nil
}

func (r fakeAccountReader) BatchGetParticipants(_ context.Context, userIDs []int64) (map[int64]*domainchat.Participant, error) {
	result := make(map[int64]*domainchat.Participant, len(userIDs))
	for _, userID := range userIDs {
		if participant := r.available[userID]; participant != nil {
			result[userID] = participant
		}
	}
	return result, nil
}

type fakeRelationReader struct {
	mutual bool
}

func (r fakeRelationReader) AreMutuallyFollowing(context.Context, int64, int64) (bool, error) {
	return r.mutual, nil
}

func (fakeRelationReader) ListMutualRecipients(context.Context, int64, string, *domainchat.RecipientCursor, int) ([]*domainchat.Recipient, error) {
	return []*domainchat.Recipient{{
		UserID: 2, Nickname: "two", FollowedAt: time.Unix(3, 0),
	}}, nil
}

type fakeVideoReader struct{}

func (fakeVideoReader) ValidatePublicVideo(context.Context, int64) (*domainchat.VideoCard, error) {
	return domainchat.RestoreVideoCard(88, 2, "video", "/cover", "/media", true), nil
}

type mutableChatRelationReader struct {
	mutual bool
}

func (r *mutableChatRelationReader) AreMutuallyFollowing(context.Context, int64, int64) (bool, error) {
	return r.mutual, nil
}

func (*mutableChatRelationReader) ListMutualRecipients(context.Context, int64, string, *domainchat.RecipientCursor, int) ([]*domainchat.Recipient, error) {
	return nil, nil
}

type mutableChatVideoReader struct {
	available bool
}

func (r *mutableChatVideoReader) ValidatePublicVideo(context.Context, int64) (*domainchat.VideoCard, error) {
	if !r.available {
		return nil, domainchat.ErrVideoUnavailable
	}
	return domainchat.RestoreVideoCard(88, 2, "video", "/cover", "/media", true), nil
}

func (r *mutableChatVideoReader) BatchHydratePublicVideos(_ context.Context, videoIDs []int64) (map[int64]*domainchat.VideoCard, error) {
	result := make(map[int64]*domainchat.VideoCard, len(videoIDs))
	if !r.available {
		return result, nil
	}
	for _, videoID := range videoIDs {
		result[videoID] = domainchat.RestoreVideoCard(videoID, 2, "video", "/cover", "/media", true)
	}
	return result, nil
}

func (fakeVideoReader) BatchHydratePublicVideos(_ context.Context, videoIDs []int64) (map[int64]*domainchat.VideoCard, error) {
	result := make(map[int64]*domainchat.VideoCard, len(videoIDs))
	for _, videoID := range videoIDs {
		result[videoID] = domainchat.RestoreVideoCard(videoID, 2, "video", "/cover", "/media", true)
	}
	return result, nil
}

type fakeUnreadReader struct{}

func (fakeUnreadReader) CountUnread(context.Context, int64) (int, error) {
	return 2, nil
}

type recordingObserver struct {
	operation string
	kind      string
	outcome   string
}

func (r *recordingObserver) Observe(operation, kind, outcome, _ string, _ time.Duration) {
	r.operation, r.kind, r.outcome = operation, kind, outcome
}

func testService(mutual bool, repo *fakeChatRepo) *Service {
	accounts := fakeAccountReader{available: map[int64]*domainchat.Participant{
		1: domainchat.RestoreParticipant(1, "one", "", "", true),
		2: domainchat.RestoreParticipant(2, "two", "", "", true),
	}}
	return New(repo, accounts, fakeRelationReader{mutual: mutual}, fakeVideoReader{},
		WithNotificationUnreadReader(fakeUnreadReader{}),
	)
}

func TestServiceRejectsNonMutualConversationAndSend(t *testing.T) {
	repo := &fakeChatRepo{}
	service := testService(false, repo)
	if _, err := service.CreateConversation(context.Background(), 1, 2); !errors.Is(err, domainchat.ErrNotEligible) {
		t.Fatalf("expected non-mutual create rejection, got %v", err)
	}
	conversation, err := repo.CreateOrGetConversation(context.Background(), 1, 2)
	if err != nil {
		t.Fatalf("failed to prepare conversation: %v", err)
	}
	if _, err := service.SendText(context.Background(), 1, conversation.ID, "hello", "key"); !errors.Is(err, domainchat.ErrNotEligible) {
		t.Fatalf("expected non-mutual send rejection, got %v", err)
	}
}

func TestServiceCommittedRetrySurvivesMutableStateChanges(t *testing.T) {
	repo := &fakeChatRepo{}
	relation := &mutableChatRelationReader{mutual: true}
	video := &mutableChatVideoReader{available: true}
	accounts := fakeAccountReader{available: map[int64]*domainchat.Participant{
		1: domainchat.RestoreParticipant(1, "one", "", "", true),
		2: domainchat.RestoreParticipant(2, "two", "", "", true),
	}}
	service := New(repo, accounts, relation, video)
	conversation, err := repo.CreateOrGetConversation(context.Background(), 1, 2)
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	first, err := service.SendVideo(context.Background(), 1, conversation.ID, 88, "video-key")
	if err != nil || first == nil || !first.Created {
		t.Fatalf("initial video send: result=%#v err=%v", first, err)
	}

	accounts.available[1] = domainchat.UnavailableParticipant(1)
	accounts.available[2] = domainchat.UnavailableParticipant(2)
	relation.mutual = false
	video.available = false
	retry, err := service.SendVideo(context.Background(), 1, conversation.ID, 88, "video-key")
	if err != nil || retry == nil || retry.Created || retry.Message.ID != first.Message.ID {
		t.Fatalf("committed retry was not replayed: result=%#v err=%v", retry, err)
	}
	if retry.Message.Video == nil || retry.Message.Video.Available {
		t.Fatalf("expected unavailable video tombstone on replay: %#v", retry.Message.Video)
	}

	if _, err := service.SendVideo(context.Background(), 1, conversation.ID, 99, "video-key"); !errors.Is(err, domainchat.ErrIdempotencyConflict) {
		t.Fatalf("expected same-key payload conflict before video validation, got %v", err)
	}
	if _, err := service.SendText(context.Background(), 1, conversation.ID, "new", "new-key"); !errors.Is(err, domainchat.ErrAccountUnavailable) {
		t.Fatalf("expected frozen account to deny a new send, got %v", err)
	}
	accounts.available[1] = domainchat.RestoreParticipant(1, "one", "", "", true)
	accounts.available[2] = domainchat.RestoreParticipant(2, "two", "", "", true)
	if _, err := service.SendText(context.Background(), 1, conversation.ID, "new", "new-key"); !errors.Is(err, domainchat.ErrNotEligible) {
		t.Fatalf("expected unfollowed users to deny a new send, got %v", err)
	}
}

func TestServiceHistoryIncludesAuthorizedMetadataAndUnavailableParticipant(t *testing.T) {
	repo := &fakeChatRepo{}
	conversation, err := repo.CreateOrGetConversation(context.Background(), 1, 2)
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	repo.message = domainchat.RestoreMessage(
		11, conversation.ID, 3, domainchat.MessageKindText, "stale", 0, "stale-key", nil, time.Unix(2, 0),
	)
	accounts := fakeAccountReader{available: map[int64]*domainchat.Participant{
		1: domainchat.RestoreParticipant(1, "one", "", "", true),
		2: domainchat.RestoreParticipant(2, "two", "", "", true),
	}}
	service := New(repo, accounts, &mutableChatRelationReader{mutual: true}, fakeVideoReader{})
	history, err := service.ListHistory(context.Background(), 1, conversation.ID, "", 20)
	if err != nil {
		t.Fatalf("ListHistory returned error: %v", err)
	}
	if history.Conversation == nil || history.Conversation.ID != conversation.ID ||
		history.Conversation.Counterpart == nil || history.Conversation.Counterpart.UserID != 2 {
		t.Fatalf("missing conversation metadata: %#v", history.Conversation)
	}
	if history.Eligibility == nil || !history.Eligibility.Eligible ||
		history.Eligibility.ConversationID != conversation.ID {
		t.Fatalf("missing eligibility metadata: %#v", history.Eligibility)
	}
	if len(history.Items) != 1 || history.Items[0].Sender == nil || history.Items[0].Sender.Available {
		t.Fatalf("expected unavailable stale sender tombstone: %#v", history.Items)
	}
}

func TestServiceSeparatesNotificationAndChatUnreadCounts(t *testing.T) {
	repo := &fakeChatRepo{unread: 3}
	service := testService(true, repo)
	summary, err := service.InboxUnread(context.Background(), 1)
	if err != nil {
		t.Fatalf("InboxUnread returned error: %v", err)
	}
	if summary.NotificationUnreadCount != 2 || summary.ChatUnreadCount != 3 || summary.TotalUnreadCount != 5 {
		t.Fatalf("unexpected unread summary: %#v", summary)
	}
}

func TestServiceBindsHistoryAndRecipientCursors(t *testing.T) {
	repo := &fakeChatRepo{}
	conversation, err := repo.CreateOrGetConversation(context.Background(), 1, 2)
	if err != nil {
		t.Fatalf("failed to prepare conversation: %v", err)
	}
	repo.message = domainchat.RestoreMessage(11, conversation.ID, 1, domainchat.MessageKindText, "hello", 0, "key", nil, time.Unix(2, 0))
	service := testService(true, repo)
	history, err := service.ListHistory(context.Background(), 1, conversation.ID, "", 1)
	if err != nil {
		t.Fatalf("ListHistory returned error: %v", err)
	}
	if history.NextCursor == "" {
		t.Fatal("expected history cursor")
	}
	if _, err := service.ListHistory(context.Background(), 1, conversation.ID+1, history.NextCursor, 1); !errors.Is(err, domainchat.ErrInvalidCursor) {
		t.Fatalf("expected conversation-bound cursor rejection, got %v", err)
	}
	recipients, err := service.ListRecipients(context.Background(), 1, "Two", "", 1)
	if err != nil {
		t.Fatalf("ListRecipients returned error: %v", err)
	}
	if recipients.NextCursor == "" {
		t.Fatal("expected recipient cursor")
	}
	if _, err := service.ListRecipients(context.Background(), 1, "other", recipients.NextCursor, 1); !errors.Is(err, domainchat.ErrInvalidCursor) {
		t.Fatalf("expected query-bound cursor rejection, got %v", err)
	}
}
