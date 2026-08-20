package infrachat

import "time"

type ConversationModel struct {
	ID            int64      `gorm:"column:id;primaryKey;autoIncrement;index:idx_chat_conversation_last_message,priority:2"`
	LowerUserID   int64      `gorm:"column:lower_user_id;not null;uniqueIndex:uk_chat_conversation_pair,priority:1"`
	HigherUserID  int64      `gorm:"column:higher_user_id;not null;uniqueIndex:uk_chat_conversation_pair,priority:2"`
	LastMessageID *int64     `gorm:"column:last_message_id;index:idx_chat_conversation_last_message,priority:1"`
	LastMessageAt *time.Time `gorm:"column:last_message_at"`
	CreatedAt     time.Time  `gorm:"column:created_at;not null;autoCreateTime"`
	UpdatedAt     time.Time  `gorm:"column:updated_at;not null;autoUpdateTime"`
}

func (ConversationModel) TableName() string {
	return "chat_conversation"
}

type ConversationMemberModel struct {
	ConversationID    int64      `gorm:"column:conversation_id;primaryKey"`
	UserID            int64      `gorm:"column:user_id;primaryKey;index:idx_chat_member_user_unread,priority:1"`
	LastReadMessageID int64      `gorm:"column:last_read_message_id;not null;default:0"`
	LastReadAt        *time.Time `gorm:"column:last_read_at"`
	UnreadCount       int        `gorm:"column:unread_count;not null;default:0;index:idx_chat_member_user_unread,priority:2"`
	MutedAt           *time.Time `gorm:"column:muted_at"`
	HiddenAt          *time.Time `gorm:"column:hidden_at"`
	CreatedAt         time.Time  `gorm:"column:created_at;not null;autoCreateTime"`
	UpdatedAt         time.Time  `gorm:"column:updated_at;not null;autoUpdateTime"`
}

func (ConversationMemberModel) TableName() string {
	return "chat_conversation_member"
}

type MessageModel struct {
	ID             int64      `gorm:"column:id;primaryKey;autoIncrement;index:idx_chat_message_conversation_id,priority:2"`
	ConversationID int64      `gorm:"column:conversation_id;not null;index:idx_chat_message_conversation_id,priority:1"`
	SenderID       int64      `gorm:"column:sender_id;not null;index:idx_chat_message_sender_key,priority:1"`
	Kind           string     `gorm:"column:kind;size:16;not null"`
	Text           string     `gorm:"column:text;type:text;not null;default:''"`
	VideoID        *int64     `gorm:"column:video_id;index:idx_chat_message_video"`
	IdempotencyKey string     `gorm:"column:idempotency_key;size:128;not null;uniqueIndex:uk_chat_message_sender_key,priority:1"`
	RevokedAt      *time.Time `gorm:"column:revoked_at"`
	CreatedAt      time.Time  `gorm:"column:created_at;not null;autoCreateTime"`
}

func (MessageModel) TableName() string {
	return "chat_message"
}
