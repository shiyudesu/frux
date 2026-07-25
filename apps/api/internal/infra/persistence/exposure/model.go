package infraexposure

import "time"

// ViewEventModel 映射 video_view_events 表，保存观看行为流水。
type ViewEventModel struct {
	ID                int64      `gorm:"column:id;primaryKey;autoIncrement"`
	UserID            int64      `gorm:"column:user_id;not null;index:idx_video_view_events_user_created,priority:1;index:idx_video_view_events_user_occurred,priority:1;uniqueIndex:uk_video_view_events_user_event,priority:1"`
	VideoID           int64      `gorm:"column:video_id;not null;index:idx_video_view_events_video_created,priority:1"`
	Scene             string     `gorm:"column:scene;size:32;not null;index:idx_video_view_events_user_scene_created,priority:2"`
	RequestID         *string    `gorm:"column:request_id;size:64;index:idx_video_view_events_request_event,priority:1"`
	EventType         string     `gorm:"column:event_type;size:32;not null;index:idx_video_view_events_request_event,priority:2"`
	EventID           *string    `gorm:"column:event_id;size:128;uniqueIndex:uk_video_view_events_user_event,priority:2"`
	PlaybackSessionID *string    `gorm:"column:playback_session_id;size:128;index:idx_video_view_events_session_sequence,priority:1"`
	Sequence          *int64     `gorm:"column:sequence;index:idx_video_view_events_session_sequence,priority:2"`
	OccurredAt        time.Time  `gorm:"column:occurred_at;not null;default:CURRENT_TIMESTAMP;index:idx_video_view_events_user_occurred,priority:2"`
	PositionMs        int        `gorm:"column:position_ms;not null;default:0"`
	WatchMs           int        `gorm:"column:watch_ms;not null;default:0"`
	DurationMs        *int       `gorm:"column:duration_ms"`
	Completed         bool       `gorm:"column:completed;not null;default:false"`
	ExposureFirstAt   *time.Time `gorm:"column:exposure_first_at"`
	ExposureCount     int        `gorm:"column:exposure_count_snapshot;not null;default:0"`
	CreatedAt         time.Time  `gorm:"column:created_at;autoCreateTime;index:idx_video_view_events_user_created,priority:2;index:idx_video_view_events_video_created,priority:2;index:idx_video_view_events_user_scene_created,priority:3"`
}

// TableName 指定观看行为表名。
func (ViewEventModel) TableName() string {
	return "video_view_events"
}

// ExposureModel 映射 exposures 表，保存用户看过视频的聚合索引。
type ExposureModel struct {
	ID             int64     `gorm:"column:id;primaryKey;autoIncrement"`
	UserID         int64     `gorm:"column:user_id;not null;uniqueIndex:uk_exposures_user_video,priority:1;index:idx_exposures_user_last_exposed,priority:1"`
	VideoID        int64     `gorm:"column:video_id;not null;uniqueIndex:uk_exposures_user_video,priority:2;index:idx_exposures_video_last_exposed,priority:1"`
	FirstExposedAt time.Time `gorm:"column:first_exposed_at;not null"`
	LastExposedAt  time.Time `gorm:"column:last_exposed_at;not null;index:idx_exposures_user_last_exposed,priority:2;index:idx_exposures_video_last_exposed,priority:2"`
	ExposureCount  int       `gorm:"column:exposure_count;not null;default:1"`
	LastScene      string    `gorm:"column:last_scene;size:32;not null"`
	CreatedAt      time.Time `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt      time.Time `gorm:"column:updated_at;autoUpdateTime"`
}

// TableName 指定曝光聚合表名。
func (ExposureModel) TableName() string {
	return "exposures"
}

type ViewHistoryModel struct {
	UserID         int64     `gorm:"column:user_id;primaryKey;index:idx_video_view_history_user_last,priority:1"`
	VideoID        int64     `gorm:"column:video_id;primaryKey;index:idx_video_view_history_user_last,priority:3"`
	LastScene      string    `gorm:"column:last_scene;size:32;not null"`
	LastEventType  string    `gorm:"column:last_event_type;size:32;not null"`
	LastPositionMs int       `gorm:"column:last_position_ms;not null;default:0"`
	LastWatchMs    int       `gorm:"column:last_watch_ms;not null;default:0"`
	Completed      bool      `gorm:"column:completed;not null;default:false"`
	FirstWatchedAt time.Time `gorm:"column:first_watched_at;not null"`
	LastWatchedAt  time.Time `gorm:"column:last_watched_at;not null;index:idx_video_view_history_user_last,priority:2"`
	LastOccurredAt time.Time `gorm:"column:last_occurred_at;not null;default:CURRENT_TIMESTAMP"`
	LastEventID    string    `gorm:"column:last_event_id;size:128;not null;default:''"`
	LastSessionID  *string   `gorm:"column:last_playback_session_id;size:128"`
	LastSequence   *int64    `gorm:"column:last_sequence"`
	CreatedAt      time.Time `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt      time.Time `gorm:"column:updated_at;autoUpdateTime"`
}

func (ViewHistoryModel) TableName() string {
	return "video_view_history"
}

type ViewHistoryDeletionModel struct {
	UserID    int64     `gorm:"column:user_id;primaryKey"`
	VideoID   int64     `gorm:"column:video_id;primaryKey"`
	DeletedAt time.Time `gorm:"column:deleted_at;not null"`
}

func (ViewHistoryDeletionModel) TableName() string {
	return "video_view_history_deletion"
}

type ViewEventOutboxModel struct {
	ID            int64      `gorm:"column:id;primaryKey;autoIncrement"`
	EventID       string     `gorm:"column:event_id;size:128;not null;index:idx_view_event_outbox_event"`
	ViewEventID   int64      `gorm:"column:view_event_id;not null;uniqueIndex:uk_view_event_outbox_view_event"`
	ExposureCount int        `gorm:"column:exposure_count;not null;default:0"`
	Attempts      int        `gorm:"column:attempts;not null;default:0"`
	AvailableAt   time.Time  `gorm:"column:available_at;not null;index:idx_view_event_outbox_pending,priority:2"`
	LeasedUntil   *time.Time `gorm:"column:leased_until;index:idx_view_event_outbox_pending,priority:3"`
	LastError     string     `gorm:"column:last_error;size:512"`
	DispatchedAt  *time.Time `gorm:"column:dispatched_at;index:idx_view_event_outbox_pending,priority:1"`
	CreatedAt     time.Time  `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt     time.Time  `gorm:"column:updated_at;autoUpdateTime"`
}

func (ViewEventOutboxModel) TableName() string {
	return "view_event_outbox"
}
