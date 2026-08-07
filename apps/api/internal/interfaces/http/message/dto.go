package interfaceshttpmessage

import "time"

type createMessageRequest struct {
	UserID          int64     `json:"user_id"`
	Type            string    `json:"type"`
	Title           string    `json:"title"`
	Content         string    `json:"content"`
	EventID         string    `json:"event_id"`
	ActorID         int64     `json:"actor_id"`
	ActorNickname   string    `json:"actor_nickname"`
	ActorAvatarURL  string    `json:"actor_avatar_url"`
	VideoID         int64     `json:"video_id"`
	CommentID       int64     `json:"comment_id"`
	RootCommentID   int64     `json:"root_comment_id"`
	LifecycleStage  string    `json:"lifecycle_stage"`
	LifecycleResult string    `json:"lifecycle_result"`
	ReasonCode      string    `json:"reason_code"`
	ReviewVersion   int       `json:"review_version"`
	OccurredAt      time.Time `json:"occurred_at"`
}

type markReadRequest struct {
	MessageIDs []int64 `json:"message_ids"`
}

type messageResponse struct {
	ID                  int64      `json:"id"`
	UserID              int64      `json:"user_id"`
	Type                string     `json:"type"`
	Title               string     `json:"title"`
	Content             string     `json:"content"`
	EventID             string     `json:"event_id,omitempty"`
	ActorID             int64      `json:"actor_id,omitempty"`
	ActorNickname       string     `json:"actor_nickname,omitempty"`
	ActorAvatarURL      string     `json:"actor_avatar_url,omitempty"`
	VideoID             int64      `json:"video_id,omitempty"`
	CommentID           int64      `json:"comment_id,omitempty"`
	RootCommentID       int64      `json:"root_comment_id,omitempty"`
	LifecycleStage      string     `json:"lifecycle_stage,omitempty"`
	LifecycleResult     string     `json:"lifecycle_result,omitempty"`
	ReasonCode          string     `json:"reason_code,omitempty"`
	ReviewVersion       int        `json:"review_version,omitempty"`
	LifecycleOccurredAt *time.Time `json:"lifecycle_occurred_at,omitempty"`
	IsRead              bool       `json:"is_read"`
	CreatedAt           time.Time  `json:"created_at"`
	ReadAt              *time.Time `json:"read_at,omitempty"`
}

type messageListResponse struct {
	Items      []messageResponse `json:"items"`
	NextCursor string            `json:"next_cursor"`
	HasMore    bool              `json:"has_more"`
}

type unreadStatResponse struct {
	UnreadCount int `json:"unread_count"`
}

type markReadResponse struct {
	UpdatedCount int `json:"updated_count"`
}
