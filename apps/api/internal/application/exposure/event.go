package applicationexposure

import (
	domainexposure "github.com/shiyudesu/frux/internal/domain/exposure"
	"time"
)

// ViewEventRecordedEvent 是观看行为已落库事件，供用户画像和推荐画像 worker 消费。
type ViewEventRecordedEvent struct {
	EventID           string    `json:"event_id"`
	ViewEventID       int64     `json:"view_event_id"`
	UserID            int64     `json:"user_id"`
	VideoID           int64     `json:"video_id"`
	Scene             string    `json:"scene"`
	RequestID         string    `json:"request_id,omitempty"`
	EventType         string    `json:"event_type"`
	PlaybackSessionID string    `json:"playback_session_id,omitempty"`
	Sequence          int64     `json:"sequence,omitempty"`
	PositionMs        int       `json:"position_ms"`
	WatchMs           int       `json:"watch_ms"`
	DurationMs        *int      `json:"duration_ms,omitempty"`
	Completed         bool      `json:"completed"`
	RecordedAt        time.Time `json:"recorded_at"`
	OccurredAt        time.Time `json:"occurred_at"`
	ExposureCount     int       `json:"exposure_count,omitempty"`
}

func NewViewEventRecordedEvent(event *domainexposure.ViewEvent, exposure *domainexposure.Exposure) *ViewEventRecordedEvent {
	if event == nil {
		return nil
	}
	message := &ViewEventRecordedEvent{
		EventID:           event.EventID,
		ViewEventID:       event.ID,
		UserID:            event.UserID,
		VideoID:           event.VideoID,
		Scene:             event.Scene,
		RequestID:         event.RequestID,
		EventType:         event.EventType,
		PlaybackSessionID: event.PlaybackSessionID,
		Sequence:          event.Sequence,
		PositionMs:        event.PositionMs,
		WatchMs:           event.WatchMs,
		DurationMs:        cloneInt(event.DurationMs),
		Completed:         event.Completed,
		RecordedAt:        event.CreatedAt.UTC(),
		OccurredAt:        event.OccurredAt.UTC(),
	}
	if exposure != nil {
		message.ExposureCount = exposure.ExposureCount
	}
	return message
}

func cloneInt(value *int) *int {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
