package infraplayback

import (
	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
	"time"
)

// ConfigModel 映射 playback_config 表，保存端侧播放策略。
type ConfigModel struct {
	ID           int64     `gorm:"column:id;primaryKey;autoIncrement"`
	Platform     string    `gorm:"column:platform;size:16;not null;uniqueIndex:uk_playback_config_platform_network,priority:1"`
	NetworkType  string    `gorm:"column:network_type;size:16;not null;uniqueIndex:uk_playback_config_platform_network,priority:2"`
	PreloadCount int       `gorm:"column:preload_count;not null"`
	BufferMs     int       `gorm:"column:buffer_ms;not null"`
	UpdatedAt    time.Time `gorm:"column:updated_at;autoUpdateTime"`
}

// TableName 指定配置表名。
func (ConfigModel) TableName() string {
	return "playback_config"
}

// QoSLogModel 映射 playback_qos_log 表，记录播放质量流水。
type QoSLogModel struct {
	ID             int64     `gorm:"column:id;primaryKey;autoIncrement"`
	UserID         int64     `gorm:"column:user_id;not null;index:idx_playback_qos_log_user_video_time,priority:1;uniqueIndex:uk_playback_qos_log_user_idempotency,priority:1"`
	VideoID        int64     `gorm:"column:video_id;not null;index:idx_playback_qos_log_video_time,priority:1;index:idx_playback_qos_log_user_video_time,priority:2"`
	FirstFrameMs   *int      `gorm:"column:first_frame_ms"`
	StutterCount   int       `gorm:"column:stutter_count;not null;default:0"`
	WatchMs        int       `gorm:"column:watch_ms;not null;default:0"`
	IdempotencyKey *string   `gorm:"column:idempotency_key;size:128;uniqueIndex:uk_playback_qos_log_user_idempotency,priority:2"`
	CreatedAt      time.Time `gorm:"column:created_at;autoCreateTime;index:idx_playback_qos_log_video_time,priority:2;index:idx_playback_qos_log_user_video_time,priority:3"`
}

// TableName 指定 QoS 流水表名。
func (QoSLogModel) TableName() string {
	return "playback_qos_log"
}

// TelemetryBatchModel stores retry-stable batch identity and acceptance accounting.
type TelemetryBatchModel struct {
	ID                 int64     `gorm:"column:id;primaryKey;autoIncrement"`
	UserID             *int64    `gorm:"column:user_id;uniqueIndex:uk_playback_telemetry_batch_user_batch,priority:1;check:ck_playback_telemetry_batch_reporter,(user_id IS NOT NULL AND anonymous_session_id IS NULL) OR (user_id IS NULL AND anonymous_session_id IS NOT NULL)"`
	AnonymousSessionID *string   `gorm:"column:anonymous_session_id;size:128;uniqueIndex:uk_playback_telemetry_batch_anon_batch,priority:1"`
	SchemaVersion      int       `gorm:"column:schema_version;type:smallint;not null"`
	BatchID            string    `gorm:"column:batch_id;size:128;not null;uniqueIndex:uk_playback_telemetry_batch_user_batch,priority:2;uniqueIndex:uk_playback_telemetry_batch_anon_batch,priority:2"`
	PlaybackSessionID  string    `gorm:"column:playback_session_id;size:128;not null;index:idx_playback_telemetry_batch_session"`
	PayloadHash        string    `gorm:"column:payload_hash;size:64;not null"`
	EventCount         int       `gorm:"column:event_count;not null"`
	AcceptedCount      int       `gorm:"column:accepted_count;not null;default:0"`
	DuplicateCount     int       `gorm:"column:duplicate_count;not null;default:0"`
	ClientSentAt       time.Time `gorm:"column:client_sent_at;not null"`
	CreatedAt          time.Time `gorm:"column:created_at;autoCreateTime;index:idx_playback_telemetry_batch_created"`
}

func (TelemetryBatchModel) TableName() string {
	return "playback_telemetry_batch"
}

// TelemetryEventModel stores normalized, bounded playback diagnostics.
type TelemetryEventModel struct {
	ID                    int64     `gorm:"column:id;primaryKey;autoIncrement"`
	BatchRecordID         int64     `gorm:"column:batch_record_id;not null;index:idx_playback_telemetry_event_batch"`
	UserID                *int64    `gorm:"column:user_id;uniqueIndex:uk_playback_telemetry_event_user_event,priority:1;index:idx_playback_telemetry_event_user_created,priority:1;check:ck_playback_telemetry_event_reporter,(user_id IS NOT NULL AND anonymous_session_id IS NULL) OR (user_id IS NULL AND anonymous_session_id IS NOT NULL)"`
	AnonymousSessionID    *string   `gorm:"column:anonymous_session_id;size:128;uniqueIndex:uk_playback_telemetry_event_anon_event,priority:1"`
	SchemaVersion         int       `gorm:"column:schema_version;type:smallint;not null"`
	BatchID               string    `gorm:"column:batch_id;size:128;not null"`
	PlaybackSessionID     string    `gorm:"column:playback_session_id;size:128;not null;index:idx_playback_telemetry_event_session_offset,priority:1"`
	EventID               string    `gorm:"column:event_id;size:128;not null;uniqueIndex:uk_playback_telemetry_event_user_event,priority:2;uniqueIndex:uk_playback_telemetry_event_anon_event,priority:2"`
	PayloadHash           string    `gorm:"column:payload_hash;size:64;not null"`
	EventType             string    `gorm:"column:event_type;size:32;not null"`
	VideoID               int64     `gorm:"column:video_id;not null;index:idx_playback_telemetry_event_video_created,priority:1"`
	Scene                 string    `gorm:"column:scene;size:32;not null"`
	RequestID             *string   `gorm:"column:request_id;size:64"`
	OffsetMs              int64     `gorm:"column:offset_ms;not null;index:idx_playback_telemetry_event_session_offset,priority:2"`
	MediaPositionMs       int64     `gorm:"column:media_position_ms;not null;default:0"`
	MediaDurationMs       *int64    `gorm:"column:media_duration_ms"`
	FirstFrameMs          *int64    `gorm:"column:first_frame_ms"`
	IntervalDurationMs    *int64    `gorm:"column:interval_duration_ms"`
	DroppedFrames         *int64    `gorm:"column:dropped_frames"`
	TotalFrames           *int64    `gorm:"column:total_frames"`
	RebufferCount         *int      `gorm:"column:rebuffer_count"`
	RebufferDurationMs    *int64    `gorm:"column:rebuffer_duration_ms"`
	MaxRebufferDurationMs *int64    `gorm:"column:max_rebuffer_duration_ms"`
	StartupRetryCount     *int      `gorm:"column:startup_retry_count"`
	MeasurementMethod     *string   `gorm:"column:measurement_method;size:32"`
	RecoveryOutcome       *string   `gorm:"column:recovery_outcome;size:32"`
	ErrorCategory         *string   `gorm:"column:error_category;size:32"`
	PlayerAdapter         string    `gorm:"column:player_adapter;size:16;not null"`
	SourceType            string    `gorm:"column:source_type;size:16;not null"`
	RenditionLabel        string    `gorm:"column:rendition_label;size:32;not null"`
	CodecFamily           string    `gorm:"column:codec_family;size:16;not null"`
	NetworkClass          string    `gorm:"column:network_class;size:16;not null"`
	SaveData              bool      `gorm:"column:save_data;not null;default:false"`
	BrowserFamily         string    `gorm:"column:browser_family;size:16;not null"`
	BrowserMajor          int       `gorm:"column:browser_major;not null;default:0"`
	OSFamily              string    `gorm:"column:os_family;size:16;not null"`
	ViewportClass         string    `gorm:"column:viewport_class;size:16;not null"`
	CDNHost               *string   `gorm:"column:cdn_host;size:253"`
	ClientSentAt          time.Time `gorm:"column:client_sent_at;not null"`
	CreatedAt             time.Time `gorm:"column:created_at;autoCreateTime;index:idx_playback_telemetry_event_created;index:idx_playback_telemetry_event_video_created,priority:2;index:idx_playback_telemetry_event_user_created,priority:2"`
}

func (TelemetryEventModel) TableName() string {
	return "playback_telemetry_event"
}

type PreloadVideoModel struct {
	VideoID         int64                        `gorm:"column:video_id"`
	MediaURL        string                       `gorm:"column:media_url"`
	CoverURL        string                       `gorm:"column:cover_url"`
	MediaAssetID    int64                        `gorm:"column:media_asset_id"`
	CoverAssetID    int64                        `gorm:"column:cover_asset_id"`
	MediaStatus     string                       `gorm:"column:media_status"`
	PlaybackSources []domainmedia.PlaybackSource `gorm:"-"`
}
