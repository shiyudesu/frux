package domaininteraction

import (
	"strings"
	"time"
)

const (
	ActionTypeLike     = "LIKE"
	ActionTypeFavorite = "FAVORITE"

	ActionStatusActive   = 1
	ActionStatusCanceled = 2

	CommentStatusNormal  = 1
	CommentStatusDeleted = 2

	MaxCommentContentLength = 1000
	MaxIdempotencyKeyLength = 128
	MaxActionEventIDLength  = 128
	MaxLimit                = 100
)

// Action 表示用户对视频的一类互动状态，例如点赞或收藏。
type Action struct {
	ID             int64
	UserID         int64
	VideoID        int64
	ActionType     string
	Status         int
	IdempotencyKey string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type ActionCursor struct {
	UpdatedAt time.Time
	VideoID   int64
}

type ActionVideo struct {
	VideoID   int64
	UpdatedAt time.Time
}

// ActionStateSnapshot is the durable baseline used when Redis has no action state.
type ActionStateSnapshot struct {
	Exists         bool
	Active         bool
	IdempotencyKey string
	Version        int64
	EventID        string
	OccurredAt     time.Time
	UpdatedAt      time.Time
}

// AcceptedActionEvent represents an interaction that passed public request validation before enqueueing.
type AcceptedActionEvent struct {
	EventID        string
	UserID         int64
	VideoID        int64
	ActionType     string
	Active         bool
	IdempotencyKey string
	Version        int64
	OccurredAt     time.Time
}

// ActionEventComesAfter compares the durable order tuple used for one action fact.
func ActionEventComesAfter(version int64, occurredAt time.Time, eventID string, latestVersion int64, latestOccurredAt time.Time, latestEventID string) bool {
	if version != latestVersion {
		return version > latestVersion
	}
	occurredAt = occurredAt.UTC().Truncate(time.Microsecond)
	latestOccurredAt = latestOccurredAt.UTC().Truncate(time.Microsecond)
	if latestOccurredAt.IsZero() || occurredAt.After(latestOccurredAt) {
		return true
	}
	if occurredAt.Before(latestOccurredAt) {
		return false
	}
	return eventID > latestEventID
}

// Comment 表示视频评论，包含评论者展示信息和软删除状态。
type Comment struct {
	ID             int64
	VideoID        int64
	UserID         int64
	UserNickname   string
	UserAvatarURL  string
	Content        string
	Status         int
	IdempotencyKey string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// CommentCursor 保存评论列表分页的排序字段。
type CommentCursor struct {
	CreatedAt time.Time
	CommentID int64
}

// VideoStat 保存互动模块需要的视频统计快照。
type VideoStat struct {
	VideoID       int64
	LikeCount     int
	CommentCount  int
	FavoriteCount int
}

// UserProfile 保存互动消息需要展示的用户资料。
type UserProfile struct {
	ID        int64
	Nickname  string
	AvatarURL string
}

// NormalizeActionType 统一行为类型大小写，避免外层传入 like、LIKE 等不同写法。
func NormalizeActionType(value string) (string, error) {
	value = strings.ToUpper(strings.TrimSpace(value))
	if value != ActionTypeLike && value != ActionTypeFavorite {
		return "", ErrInvalidActionType
	}
	return value, nil
}

func NewAcceptedActionEvent(eventID string, userID int64, videoID int64, actionType string, active bool, idempotencyKey string, version int64, occurredAt time.Time) (*AcceptedActionEvent, error) {
	eventID = strings.TrimSpace(eventID)
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if eventID == "" || len(eventID) > MaxActionEventIDLength {
		return nil, ErrInvalidActionEventID
	}
	if userID <= 0 {
		return nil, ErrInvalidUserID
	}
	if videoID <= 0 {
		return nil, ErrInvalidVideoID
	}
	normalizedActionType, err := NormalizeActionType(actionType)
	if err != nil {
		return nil, err
	}
	if len(idempotencyKey) > MaxIdempotencyKeyLength {
		return nil, ErrIdempotencyKeyTooLong
	}
	if version < 0 {
		return nil, ErrInvalidActionEvent
	}
	if occurredAt.IsZero() {
		return nil, ErrInvalidActionEventTime
	}
	return &AcceptedActionEvent{
		EventID:        eventID,
		UserID:         userID,
		VideoID:        videoID,
		ActionType:     normalizedActionType,
		Active:         active,
		IdempotencyKey: idempotencyKey,
		Version:        version,
		OccurredAt:     occurredAt.UTC().Truncate(time.Microsecond),
	}, nil
}

// NewComment 创建评论领域对象，负责校验视频、用户、内容和幂等键。
func NewComment(videoID int64, userID int64, content string, idempotencyKey string) (*Comment, error) {
	if videoID <= 0 {
		return nil, ErrInvalidVideoID
	}
	if userID <= 0 {
		return nil, ErrInvalidUserID
	}

	content = strings.TrimSpace(content)
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if content == "" {
		return nil, ErrEmptyCommentContent
	}
	if len(content) > MaxCommentContentLength {
		return nil, ErrCommentContentTooLong
	}
	if len(idempotencyKey) > MaxIdempotencyKeyLength {
		return nil, ErrIdempotencyKeyTooLong
	}

	// 新评论默认处于正常状态，删除时再切换为 CommentStatusDeleted。
	return &Comment{
		VideoID:        videoID,
		UserID:         userID,
		Content:        content,
		Status:         CommentStatusNormal,
		IdempotencyKey: idempotencyKey,
	}, nil
}

// RestoreAction 从数据库记录恢复互动行为，供仓储层返回领域对象。
func RestoreAction(id int64, userID int64, videoID int64, actionType string, status int, idempotencyKey string, createdAt time.Time, updatedAt time.Time) *Action {
	actionType, _ = NormalizeActionType(actionType)
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if status == 0 {
		status = ActionStatusActive
	}

	return &Action{
		ID:             id,
		UserID:         userID,
		VideoID:        videoID,
		ActionType:     actionType,
		Status:         status,
		IdempotencyKey: idempotencyKey,
		CreatedAt:      createdAt,
		UpdatedAt:      updatedAt,
	}
}

// RestoreComment 从数据库查询结果恢复评论对象，并清洗展示字段。
func RestoreComment(id int64, videoID int64, userID int64, userNickname string, userAvatarURL string, content string, status int, idempotencyKey string, createdAt time.Time, updatedAt time.Time) *Comment {
	content = strings.TrimSpace(content)
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if status == 0 {
		status = CommentStatusNormal
	}

	return &Comment{
		ID:             id,
		VideoID:        videoID,
		UserID:         userID,
		UserNickname:   strings.TrimSpace(userNickname),
		UserAvatarURL:  strings.TrimSpace(userAvatarURL),
		Content:        content,
		Status:         status,
		IdempotencyKey: idempotencyKey,
		CreatedAt:      createdAt,
		UpdatedAt:      updatedAt,
	}
}

// Active 判断点赞或收藏当前是否处于有效状态。
func (a *Action) Active() bool {
	return a.Status == ActionStatusActive
}

// Deleted 判断评论是否已经被软删除。
func (c *Comment) Deleted() bool {
	return c.Status == CommentStatusDeleted
}
