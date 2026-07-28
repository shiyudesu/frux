package infrarelation

import "time"

// FollowModel 映射 user_follow 表，记录用户之间的关注状态。
type FollowModel struct {
	ID             int64     `gorm:"column:id;primaryKey;autoIncrement"`
	UserID         int64     `gorm:"column:user_id;not null;uniqueIndex:uk_user_follow_user_target,priority:1;index:idx_user_follow_user_status_updated,priority:1"`
	TargetUserID   int64     `gorm:"column:target_user_id;not null;uniqueIndex:uk_user_follow_user_target,priority:2;index:idx_user_follow_target_status_updated,priority:1;index:idx_user_follow_user_status_updated,priority:4"`
	Status         int       `gorm:"column:status;type:smallint;not null;default:1;index:idx_user_follow_user_status_updated,priority:2;index:idx_user_follow_target_status_updated,priority:2"`
	IdempotencyKey *string   `gorm:"column:idempotency_key;size:128"`
	CreatedAt      time.Time `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt      time.Time `gorm:"column:updated_at;autoUpdateTime;index:idx_user_follow_user_status_updated,priority:3;index:idx_user_follow_target_status_updated,priority:3"`
}

// TableName 指定关注关系表名。
func (FollowModel) TableName() string {
	return "user_follow"
}

// RelationStatModel 映射 user_relation_stat 表，保存关注数和粉丝数。
type RelationStatModel struct {
	UserID         int64     `gorm:"column:user_id;primaryKey"`
	FollowingCount int       `gorm:"column:following_count;not null;default:0"`
	FollowerCount  int       `gorm:"column:follower_count;not null;default:0"`
	CreatedAt      time.Time `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt      time.Time `gorm:"column:updated_at;autoUpdateTime"`
}

// TableName 指定关系统计表名。
func (RelationStatModel) TableName() string {
	return "user_relation_stat"
}

// FollowProfileOutboxModel is written with user_follow so recommendation
// projection is recoverable when the worker is unavailable.
type FollowProfileOutboxModel struct {
	ID                      int64      `gorm:"column:id;primaryKey;autoIncrement"`
	EventID                 string     `gorm:"column:event_id;size:128;not null;uniqueIndex:uk_relation_profile_outbox_event"`
	FollowID                int64      `gorm:"column:follow_id;not null;index:idx_relation_profile_outbox_follow"`
	UserID                  int64      `gorm:"column:user_id;not null"`
	TargetUserID            int64      `gorm:"column:target_user_id;not null"`
	Active                  bool       `gorm:"column:active;not null"`
	OccurredAt              time.Time  `gorm:"column:occurred_at;not null;index:idx_relation_profile_outbox_pending,priority:4"`
	RecommendationRequestID string     `gorm:"column:recommendation_request_id;size:64;not null;default:''"`
	RecommendationVideoID   int64      `gorm:"column:recommendation_video_id;not null;default:0"`
	AvailableAt             time.Time  `gorm:"column:available_at;not null;index:idx_relation_profile_outbox_pending,priority:2"`
	LeasedUntil             *time.Time `gorm:"column:leased_until;index:idx_relation_profile_outbox_pending,priority:3"`
	DispatchedAt            *time.Time `gorm:"column:dispatched_at;index:idx_relation_profile_outbox_pending,priority:1"`
	Attempts                int        `gorm:"column:attempts;not null;default:0"`
	LastError               string     `gorm:"column:last_error;size:1024;not null;default:''"`
	CreatedAt               time.Time  `gorm:"column:created_at;not null;autoCreateTime"`
	UpdatedAt               time.Time  `gorm:"column:updated_at;not null;autoUpdateTime"`
}

func (FollowProfileOutboxModel) TableName() string {
	return "relation_profile_projection_outbox"
}
