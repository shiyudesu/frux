package domainchat

import "context"

type SendInput struct {
	ConversationID int64
	SenderID       int64
	Kind           MessageKind
	Text           string
	VideoID        int64
	IdempotencyKey string
}

type Repository interface {
	CreateOrGetConversation(ctx context.Context, firstUserID, secondUserID int64) (*Conversation, error)
	FindConversationByPair(ctx context.Context, lowerUserID, higherUserID int64) (*Conversation, error)
	FindConversationsByPairs(ctx context.Context, userID int64, otherUserIDs []int64) (map[int64]*Conversation, error)
	GetConversationForMember(ctx context.Context, conversationID, userID int64) (*Conversation, error)
	// GetConversationItemForMember returns one authorized conversation/member snapshot, including its last message when present.
	GetConversationItemForMember(ctx context.Context, conversationID, userID int64) (*ConversationItem, error)
	ListConversations(ctx context.Context, userID int64, cursor *ConversationCursor, limit int) ([]*ConversationItem, error)
	ListMessages(ctx context.Context, conversationID, userID int64, cursor *HistoryCursor, limit int) ([]*Message, error)
	// FindMessageBySenderAndIdempotencyKey only returns committed messages and is safe to use before mutable send checks.
	FindMessageBySenderAndIdempotencyKey(ctx context.Context, senderID int64, idempotencyKey string) (*Message, error)
	Send(ctx context.Context, input SendInput) (*Message, bool, error)
	MarkRead(ctx context.Context, conversationID, userID, throughMessageID int64) (*Member, error)
	CountUnread(ctx context.Context, userID int64) (int, error)
	ReconcileUnread(ctx context.Context, userID int64) (int, error)
}

type IncrementalMessageRepository interface {
	ListMessagesAfter(ctx context.Context, conversationID, userID, afterMessageID int64, limit int) ([]*Message, error)
}
