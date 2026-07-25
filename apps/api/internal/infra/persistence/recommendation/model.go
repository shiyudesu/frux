package infrarecommendation

import "time"

type BehaviorEventModel struct {
	EventID           string    `gorm:"column:event_id;size:128;primaryKey;priority:2"`
	ViewEventID       int64     `gorm:"column:view_event_id;not null;uniqueIndex:uk_recommendation_behavior_view_event"`
	UserID            int64     `gorm:"column:user_id;not null;primaryKey;priority:1;index:idx_recommendation_behavior_user_occurred,priority:1"`
	VideoID           int64     `gorm:"column:video_id;not null"`
	EventType         string    `gorm:"column:event_type;size:32;not null"`
	PlaybackSessionID *string   `gorm:"column:playback_session_id;size:128"`
	Sequence          *int64    `gorm:"column:sequence"`
	PositionMs        int       `gorm:"column:position_ms;not null;default:0"`
	WatchMs           int       `gorm:"column:watch_ms;not null;default:0"`
	DurationMs        *int      `gorm:"column:duration_ms"`
	Completed         bool      `gorm:"column:completed;not null;default:false"`
	OccurredAt        time.Time `gorm:"column:occurred_at;not null;index:idx_recommendation_behavior_user_occurred,priority:2"`
	CreatedAt         time.Time `gorm:"column:created_at;autoCreateTime"`
}

func (BehaviorEventModel) TableName() string {
	return "recommendation_behavior_event"
}
