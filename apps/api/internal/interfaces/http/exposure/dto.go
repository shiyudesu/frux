package interfaceshttpexposure

import "time"

// createViewEventRequest 是观看行为上报请求体。
type createViewEventRequest struct {
	VideoID           int64      `json:"video_id"`
	Scene             string     `json:"scene"`
	RequestID         string     `json:"request_id"`
	EventType         string     `json:"event_type"`
	EventID           string     `json:"event_id"`
	PlaybackSessionID string     `json:"playback_session_id"`
	Sequence          int64      `json:"sequence"`
	OccurredAt        *time.Time `json:"occurred_at"`
	PositionMs        int        `json:"position_ms"`
	WatchMs           int        `json:"watch_ms"`
	DurationMs        *int       `json:"duration_ms"`
	Completed         bool       `json:"completed"`
}

// viewEventResponse 是观看行为流水响应。
type viewEventResponse struct {
	ID                int64     `json:"id"`
	UserID            int64     `json:"user_id"`
	VideoID           int64     `json:"video_id"`
	Scene             string    `json:"scene"`
	RequestID         string    `json:"request_id,omitempty"`
	EventType         string    `json:"event_type"`
	EventID           string    `json:"event_id"`
	PlaybackSessionID string    `json:"playback_session_id,omitempty"`
	Sequence          int64     `json:"sequence,omitempty"`
	OccurredAt        time.Time `json:"occurred_at"`
	PositionMs        int       `json:"position_ms"`
	WatchMs           int       `json:"watch_ms"`
	DurationMs        *int      `json:"duration_ms,omitempty"`
	Completed         bool      `json:"completed"`
	CreatedAt         time.Time `json:"created_at"`
}

// exposureResponse 是曝光聚合响应，exposed 事件会返回该对象。
type exposureResponse struct {
	UserID         int64     `json:"user_id"`
	VideoID        int64     `json:"video_id"`
	FirstExposedAt time.Time `json:"first_exposed_at"`
	LastExposedAt  time.Time `json:"last_exposed_at"`
	ExposureCount  int       `json:"exposure_count"`
	LastScene      string    `json:"last_scene"`
}

// createViewEventResponse 是上报后的完整响应。
type createViewEventResponse struct {
	Event    viewEventResponse `json:"event"`
	Exposure *exposureResponse `json:"exposure,omitempty"`
	Replayed bool              `json:"replayed,omitempty"`
}
