package interfaceshttpplayback

import (
	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
	"time"
)

type playbackConfigResponse struct {
	ID           int64     `json:"id"`
	Platform     string    `json:"platform"`
	NetworkType  string    `json:"network_type"`
	PreloadCount int       `json:"preload_count"`
	BufferMs     int       `json:"buffer_ms"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type preloadVideoResponse struct {
	VideoID         int64                        `json:"video_id"`
	MediaURL        string                       `json:"media_url"`
	CoverURL        string                       `json:"cover_url"`
	MediaStatus     string                       `json:"media_status"`
	PlaybackSources []domainmedia.PlaybackSource `json:"playback_sources,omitempty"`
}

type preloadVideosResponse struct {
	Items []preloadVideoResponse `json:"items"`
}

type createQoSReportRequest struct {
	UserID       int64 `json:"user_id,omitempty"`
	VideoID      int64 `json:"video_id"`
	FirstFrameMs *int  `json:"first_frame_ms,omitempty"`
	StutterCount int   `json:"stutter_count"`
	WatchMs      int   `json:"watch_ms"`
}

type qosReportResponse struct {
	ID           int64     `json:"id"`
	UserID       int64     `json:"user_id"`
	VideoID      int64     `json:"video_id"`
	FirstFrameMs *int      `json:"first_frame_ms,omitempty"`
	StutterCount int       `json:"stutter_count"`
	WatchMs      int       `json:"watch_ms"`
	CreatedAt    time.Time `json:"created_at"`
}

type createTelemetryBatchRequest struct {
	SchemaVersion     int                     `json:"schema_version"`
	BatchID           string                  `json:"batch_id"`
	PlaybackSessionID string                  `json:"playback_session_id"`
	ClientSentAt      time.Time               `json:"client_sent_at"`
	Context           telemetryContextRequest `json:"context"`
	Events            []telemetryEventRequest `json:"events"`
}

type telemetryContextRequest struct {
	VideoID        int64  `json:"video_id"`
	Scene          string `json:"scene"`
	RequestID      string `json:"request_id"`
	PlayerAdapter  string `json:"player_adapter"`
	SourceType     string `json:"source_type"`
	RenditionLabel string `json:"rendition_label"`
	CodecFamily    string `json:"codec_family"`
	NetworkClass   string `json:"network_class"`
	SaveData       bool   `json:"save_data"`
	BrowserFamily  string `json:"browser_family"`
	BrowserMajor   int    `json:"browser_major"`
	OSFamily       string `json:"os_family"`
	ViewportClass  string `json:"viewport_class"`
	CDNHost        string `json:"cdn_host"`
}

type telemetryEventRequest struct {
	EventID               string `json:"event_id"`
	EventType             string `json:"event_type"`
	OffsetMs              int64  `json:"offset_ms"`
	MediaPositionMs       int64  `json:"media_position_ms"`
	MediaDurationMs       *int64 `json:"media_duration_ms"`
	FirstFrameMs          *int64 `json:"first_frame_ms"`
	IntervalDurationMs    *int64 `json:"interval_duration_ms"`
	DroppedFrames         *int64 `json:"dropped_frames"`
	TotalFrames           *int64 `json:"total_frames"`
	RebufferCount         *int   `json:"rebuffer_count"`
	RebufferDurationMs    *int64 `json:"rebuffer_duration_ms"`
	MaxRebufferDurationMs *int64 `json:"max_rebuffer_duration_ms"`
	StartupRetryCount     *int   `json:"startup_retry_count"`
	MeasurementMethod     string `json:"measurement_method"`
	RecoveryOutcome       string `json:"recovery_outcome"`
	ErrorCategory         string `json:"error_category"`
	SourceType            string `json:"source_type"`
	RenditionLabel        string `json:"rendition_label"`
	CodecFamily           string `json:"codec_family"`
	CDNHost               string `json:"cdn_host"`
}

type telemetryBatchResponse struct {
	BatchID        string    `json:"batch_id"`
	EventCount     int       `json:"event_count"`
	AcceptedCount  int       `json:"accepted_count"`
	DuplicateCount int       `json:"duplicate_count"`
	CreatedAt      time.Time `json:"created_at"`
}
