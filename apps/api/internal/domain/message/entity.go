package domainmessage

import (
	"strings"
	"time"
	"unicode/utf8"
)

const (
	TypeLike         = "LIKE"
	TypeComment      = "COMMENT"
	TypeCommentReply = "COMMENT_REPLY"
	TypeCommentLike  = "COMMENT_LIKE"
	TypeFollow       = "FOLLOW"
	TypeSystem       = "SYSTEM"

	MaxTitleLength          = 128
	MaxContentLength        = 1024
	MaxEventIDLength        = 64
	MaxIdempotencyKeyLength = 128
	MaxLimit                = 100
)

// Message 表示一个用户收到的站内通知。
type Message struct {
	ID             int64
	UserID         int64
	Type           string
	Title          string
	Content        string
	EventID        string
	ActorID        int64
	ActorNickname  string
	ActorAvatarURL string
	VideoID        int64
	CommentID      int64
	RootCommentID  int64
	IsRead         bool
	CreatedAt      time.Time
	ReadAt         *time.Time
}

// Cursor 保存消息列表分页的排序字段。
type Cursor struct {
	CreatedAt time.Time
	MessageID int64
}

// New 创建消息领域对象，负责接收人、类型、标题、内容和事件 ID 校验。
func New(userID int64, messageType string, title string, content string, eventID string) (*Message, error) {
	if userID <= 0 {
		return nil, ErrInvalidUserID
	}

	messageType, err := NormalizeType(messageType)
	if err != nil {
		return nil, err
	}

	title = strings.TrimSpace(title)
	content = strings.TrimSpace(content)
	eventID = strings.TrimSpace(eventID)
	if title == "" {
		return nil, ErrEmptyTitle
	}
	if utf8.RuneCountInString(title) > MaxTitleLength {
		return nil, ErrTitleTooLong
	}
	if content == "" {
		return nil, ErrEmptyContent
	}
	if utf8.RuneCountInString(content) > MaxContentLength {
		return nil, ErrContentTooLong
	}
	if len(eventID) > MaxEventIDLength {
		return nil, ErrEventIDTooLong
	}

	return &Message{
		UserID:  userID,
		Type:    messageType,
		Title:   title,
		Content: content,
		EventID: eventID,
		IsRead:  false,
	}, nil
}

// WithActor 写入触发消息的用户展示信息。
func (m *Message) WithActor(actorID int64, nickname string, avatarURL string) {
	if m == nil {
		return
	}
	if actorID > 0 {
		m.ActorID = actorID
	}
	m.ActorNickname = strings.TrimSpace(nickname)
	m.ActorAvatarURL = strings.TrimSpace(avatarURL)
}

// WithTargets writes optional structured discussion targets.
func (m *Message) WithTargets(videoID int64, commentID int64, rootCommentID int64) {
	if m == nil {
		return
	}
	if videoID > 0 {
		m.VideoID = videoID
	}
	if commentID > 0 {
		m.CommentID = commentID
	}
	if rootCommentID > 0 {
		m.RootCommentID = rootCommentID
	}
}

func (m *Message) ValidateTargets() error {
	if m == nil {
		return ErrInvalidMessageTarget
	}
	if !IsCommentType(m.Type) {
		return nil
	}
	if m.VideoID <= 0 || m.CommentID <= 0 || m.RootCommentID <= 0 {
		return ErrInvalidMessageTarget
	}
	return nil
}

// Restore 从数据库记录恢复消息领域对象。
func Restore(id int64, userID int64, messageType string, title string, content string, eventID string, isRead bool, createdAt time.Time, readAt *time.Time) *Message {
	messageType, _ = NormalizeType(messageType)
	return &Message{
		ID:        id,
		UserID:    userID,
		Type:      messageType,
		Title:     strings.TrimSpace(title),
		Content:   strings.TrimSpace(content),
		EventID:   strings.TrimSpace(eventID),
		IsRead:    isRead,
		CreatedAt: createdAt,
		ReadAt:    readAt,
	}
}

// RestoreWithActor 从数据库记录恢复带触发用户信息的消息。
func RestoreWithActor(id int64, userID int64, messageType string, title string, content string, eventID string, actorID int64, actorNickname string, actorAvatarURL string, isRead bool, createdAt time.Time, readAt *time.Time) *Message {
	message := Restore(id, userID, messageType, title, content, eventID, isRead, createdAt, readAt)
	message.WithActor(actorID, actorNickname, actorAvatarURL)
	return message
}

// RestoreWithActorAndTargets restores actor display data and optional discussion targets.
func RestoreWithActorAndTargets(id int64, userID int64, messageType string, title string, content string, eventID string, actorID int64, actorNickname string, actorAvatarURL string, videoID int64, commentID int64, rootCommentID int64, isRead bool, createdAt time.Time, readAt *time.Time) *Message {
	message := RestoreWithActor(id, userID, messageType, title, content, eventID, actorID, actorNickname, actorAvatarURL, isRead, createdAt, readAt)
	message.WithTargets(videoID, commentID, rootCommentID)
	return message
}

// NormalizeType 统一消息类型大小写。
func NormalizeType(value string) (string, error) {
	value = strings.ToUpper(strings.TrimSpace(value))
	switch value {
	case TypeLike, TypeComment, TypeCommentReply, TypeCommentLike, TypeFollow, TypeSystem:
		return value, nil
	default:
		return "", ErrInvalidMessageType
	}
}

func IsCommentType(value string) bool {
	value = strings.ToUpper(strings.TrimSpace(value))
	return value == TypeComment || value == TypeCommentReply || value == TypeCommentLike
}
