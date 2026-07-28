package infrainteraction

import "time"

// ActionModel 映射 interaction_action 表，记录用户对视频的点赞/收藏状态。
type ActionModel struct {
	ID int64 `gorm:"column:id;primaryKey;autoIncrement"`
	// user_id + video_id + action_type 唯一，保证同一用户对同一视频只有一条同类行为记录。
	UserID     int64  `gorm:"column:user_id;not null;uniqueIndex:uk_interaction_action_user_video_type,priority:1;index:idx_interaction_action_user_type_status,priority:1;index:idx_interaction_action_user_type_status_updated,priority:1"`
	VideoID    int64  `gorm:"column:video_id;not null;uniqueIndex:uk_interaction_action_user_video_type,priority:2;index:idx_interaction_action_video_type_status,priority:1;index:idx_interaction_action_user_type_status_updated,priority:5"`
	ActionType string `gorm:"column:action_type;size:16;not null;uniqueIndex:uk_interaction_action_user_video_type,priority:3;index:idx_interaction_action_video_type_status,priority:2;index:idx_interaction_action_user_type_status,priority:2;index:idx_interaction_action_user_type_status_updated,priority:2"`
	Status     int    `gorm:"column:status;type:smallint;not null;default:1;index:idx_interaction_action_video_type_status,priority:3;index:idx_interaction_action_user_type_status,priority:3;index:idx_interaction_action_user_type_status_updated,priority:3"`
	// IdempotencyKey 保存最近一次请求键，用于重复请求返回稳定结果。
	IdempotencyKey          *string    `gorm:"column:idempotency_key;size:128"`
	RecommendationRequestID string     `gorm:"column:recommendation_request_id;size:64;not null;default:''"`
	LatestEventVersion      int64      `gorm:"column:latest_event_version;not null;default:0"`
	LatestEventOccurredAt   *time.Time `gorm:"column:latest_event_occurred_at"`
	LatestEventID           *string    `gorm:"column:latest_event_id;size:128"`
	CreatedAt               time.Time  `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt               time.Time  `gorm:"column:updated_at;autoUpdateTime;index:idx_interaction_action_user_type_status_updated,priority:4"`
}

// TableName 指定互动行为表名。
func (ActionModel) TableName() string {
	return "interaction_action"
}

// ActionEventModel records accepted asynchronous events for durable duplicate-delivery detection.
type ActionEventModel struct {
	EventID                           string     `gorm:"column:event_id;size:128;primaryKey"`
	UserID                            int64      `gorm:"column:user_id;not null"`
	VideoID                           int64      `gorm:"column:video_id;not null;index:idx_interaction_action_event_video"`
	ActionType                        string     `gorm:"column:action_type;size:16;not null"`
	Active                            bool       `gorm:"column:active;not null"`
	IdempotencyKey                    *string    `gorm:"column:idempotency_key;size:128"`
	RecommendationRequestID           string     `gorm:"column:recommendation_request_id;size:64;not null;default:''"`
	RecommendationOutcomeDispatchedAt *time.Time `gorm:"column:recommendation_outcome_dispatched_at;index:idx_interaction_action_event_outcome_pending,priority:1"`
	RecommendationOutcomeAvailableAt  time.Time  `gorm:"column:recommendation_outcome_available_at;not null;default:CURRENT_TIMESTAMP;index:idx_interaction_action_event_outcome_available"`
	RecommendationOutcomeLeasedUntil  *time.Time `gorm:"column:recommendation_outcome_leased_until;index:idx_interaction_action_event_outcome_pending,priority:3"`
	RecommendationOutcomeAttempts     int        `gorm:"column:recommendation_outcome_attempts;not null;default:0"`
	RecommendationOutcomeLastError    string     `gorm:"column:recommendation_outcome_last_error;size:1024;not null;default:''"`
	ProfileProjectionDispatchedAt     *time.Time `gorm:"column:profile_projection_dispatched_at;index:idx_interaction_action_event_profile_pending,priority:1"`
	ProfileProjectionLeasedUntil      *time.Time `gorm:"column:profile_projection_leased_until;index:idx_interaction_action_event_profile_pending,priority:3"`
	ProfileProjectionAttempts         int        `gorm:"column:profile_projection_attempts;not null;default:0"`
	ProfileProjectionAvailableAt      time.Time  `gorm:"column:profile_projection_available_at;not null;default:CURRENT_TIMESTAMP;index:idx_interaction_action_event_profile_pending,priority:2"`
	ProfileProjectionLastError        string     `gorm:"column:profile_projection_last_error;size:1024;not null;default:''"`
	Version                           int64      `gorm:"column:version;not null;default:0"`
	OccurredAt                        time.Time  `gorm:"column:occurred_at;not null"`
	ProcessedAt                       time.Time  `gorm:"column:processed_at;not null;autoCreateTime"`
}

func (ActionEventModel) TableName() string {
	return "interaction_action_event"
}

// ActionIdempotencyReceiptModel retains the exact synchronous API result for
// every keyed desired action, including no-op requests that must not enter
// profile or recommendation handoff queues.
type ActionIdempotencyReceiptModel struct {
	UserID         int64     `gorm:"column:user_id;primaryKey;autoIncrement:false"`
	VideoID        int64     `gorm:"column:video_id;primaryKey;autoIncrement:false"`
	ActionType     string    `gorm:"column:action_type;size:16;primaryKey"`
	IdempotencyKey string    `gorm:"column:idempotency_key;size:128;primaryKey"`
	Active         bool      `gorm:"column:active;not null"`
	ActionID       int64     `gorm:"column:action_id;not null;default:0"`
	ActionCount    int       `gorm:"column:action_count;not null;default:0"`
	CreatedAt      time.Time `gorm:"column:created_at;not null;autoCreateTime"`
}

func (ActionIdempotencyReceiptModel) TableName() string {
	return "interaction_action_idempotency_receipt"
}

// CommentModel 映射 interaction_comment 表，评论采用软删除状态。
type CommentModel struct {
	ID             int64     `gorm:"column:id;primaryKey;autoIncrement"`
	VideoID        int64     `gorm:"column:video_id;not null;index:idx_interaction_comment_video_status_created,priority:1"`
	UserID         int64     `gorm:"column:user_id;not null;index:idx_interaction_comment_user_created,priority:1;uniqueIndex:uk_interaction_comment_user_idempotency,priority:1"`
	Content        string    `gorm:"column:content;size:1000;not null"`
	Status         int       `gorm:"column:status;type:smallint;not null;default:1;index:idx_interaction_comment_video_status_created,priority:2"`
	IdempotencyKey *string   `gorm:"column:idempotency_key;size:128;uniqueIndex:uk_interaction_comment_user_idempotency,priority:2"`
	CreatedAt      time.Time `gorm:"column:created_at;autoCreateTime;index:idx_interaction_comment_video_status_created,priority:3;index:idx_interaction_comment_user_created,priority:2"`
	UpdatedAt      time.Time `gorm:"column:updated_at;autoUpdateTime;index:idx_interaction_comment_video_status_created,priority:4"`
}

// TableName 指定评论表名。
func (CommentModel) TableName() string {
	return "interaction_comment"
}
