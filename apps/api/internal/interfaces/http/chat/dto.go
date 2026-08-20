package interfaceshttpchat

import (
	"time"

	domainchat "github.com/shiyudesu/frux/internal/domain/chat"
)

type createConversationRequest struct {
	TargetUserID int64 `json:"target_user_id"`
}

type sendMessageRequest struct {
	Kind    string  `json:"kind"`
	Text    *string `json:"text"`
	VideoID *int64  `json:"video_id"`
}

type markReadRequest struct {
	ThroughMessageID int64 `json:"through_message_id"`
}

type participantResponse struct {
	UserID    int64  `json:"user_id"`
	Nickname  string `json:"nickname,omitempty"`
	AvatarURL string `json:"avatar_url,omitempty"`
	Bio       string `json:"bio,omitempty"`
	Available bool   `json:"available"`
}

type eligibilityResponse struct {
	Eligible       bool   `json:"eligible"`
	Reason         string `json:"reason"`
	ConversationID int64  `json:"conversation_id,omitempty"`
}

type recipientResponse struct {
	UserID         int64     `json:"user_id"`
	Nickname       string    `json:"nickname"`
	AvatarURL      string    `json:"avatar_url,omitempty"`
	Bio            string    `json:"bio,omitempty"`
	FollowedAt     time.Time `json:"followed_at"`
	ConversationID int64     `json:"conversation_id,omitempty"`
}

type recipientListResponse struct {
	Items      []recipientResponse `json:"items"`
	NextCursor string              `json:"next_cursor,omitempty"`
	HasMore    bool                `json:"has_more"`
}

type lastMessageResponse struct {
	ID        int64                  `json:"id"`
	Kind      domainchat.MessageKind `json:"kind"`
	Preview   string                 `json:"preview,omitempty"`
	CreatedAt time.Time              `json:"created_at"`
}

type conversationResponse struct {
	ID            int64                `json:"id"`
	Counterpart   participantResponse  `json:"counterpart"`
	LastMessageID int64                `json:"last_message_id"`
	LastMessage   *lastMessageResponse `json:"last_message,omitempty"`
	LastMessageAt *time.Time           `json:"last_message_at,omitempty"`
	UnreadCount   int                  `json:"unread_count"`
}

type conversationListResponse struct {
	Items      []conversationResponse `json:"items"`
	NextCursor string                 `json:"next_cursor,omitempty"`
	HasMore    bool                   `json:"has_more"`
}

type videoCardResponse struct {
	VideoID         int64  `json:"video_id"`
	Available       bool   `json:"available"`
	Title           string `json:"title,omitempty"`
	CoverURL        string `json:"cover_url,omitempty"`
	MediaURL        string `json:"media_url,omitempty"`
	AuthorID        int64  `json:"author_id,omitempty"`
	AuthorNickname  string `json:"author_nickname,omitempty"`
	AuthorAvatarURL string `json:"author_avatar_url,omitempty"`
}

type messageResponse struct {
	ID             int64                  `json:"id"`
	ConversationID int64                  `json:"conversation_id"`
	Sender         participantResponse    `json:"sender"`
	Kind           domainchat.MessageKind `json:"kind"`
	Text           string                 `json:"text,omitempty"`
	Video          *videoCardResponse     `json:"video,omitempty"`
	CreatedAt      time.Time              `json:"created_at"`
}

// historyResponse always includes the authorized conversation snapshot and
// current send eligibility, including for an empty conversation.
type historyResponse struct {
	Items        []messageResponse    `json:"items"`
	NextCursor   string               `json:"next_cursor,omitempty"`
	HasMore      bool                 `json:"has_more"`
	Conversation conversationResponse `json:"conversation"`
	Eligibility  eligibilityResponse  `json:"eligibility"`
}

type sendMessageResponse struct {
	Message messageResponse `json:"message"`
	Created bool            `json:"created"`
}

type markReadResponse struct {
	LastReadMessageID int64 `json:"last_read_message_id"`
	UnreadCount       int   `json:"unread_count"`
}

type inboxUnreadResponse struct {
	NotificationUnreadCount int `json:"notification_unread_count"`
	ChatUnreadCount         int `json:"chat_unread_count"`
	TotalUnreadCount        int `json:"total_unread_count"`
}
