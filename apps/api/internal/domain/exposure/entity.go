package domainexposure

import (
	"strings"
	"time"
)

const (
	EventTypeExposed  = "exposed"
	EventTypePlay     = "play"
	EventTypeProgress = "progress"
	EventTypeComplete = "complete"
	EventTypeSkip     = "skip"

	MaxSceneLength             = 32
	MaxRequestIDLength         = 64
	MaxEventIDLength           = 128
	MaxPlaybackSessionIDLength = 128
	MaxSequence                = int64(1<<31 - 1)
	MaxMediaDurationMs         = 24 * 60 * 60 * 1000
	MaxPastOccurrenceSkew      = 24 * time.Hour
	MaxFutureOccurrenceSkew    = 5 * time.Minute
)

type NewViewEventInput struct {
	UserID            int64
	VideoID           int64
	Scene             string
	RequestID         string
	EventType         string
	EventID           string
	PlaybackSessionID string
	Sequence          int64
	OccurredAt        time.Time
	PositionMs        int
	WatchMs           int
	DurationMs        *int
	Completed         bool
}

// ViewEvent 保存一次客户端观看行为，适合做行为流水和后续推荐特征。
type ViewEvent struct {
	ID                int64
	UserID            int64
	VideoID           int64
	Scene             string
	RequestID         string
	EventType         string
	EventID           string
	PlaybackSessionID string
	Sequence          int64
	OccurredAt        time.Time
	PositionMs        int
	WatchMs           int
	DurationMs        *int
	Completed         bool
	CreatedAt         time.Time
	ClientEnvelope    bool
}

// Exposure 保存用户看过某个视频的聚合事实，供推荐系统在线去重查询。
type Exposure struct {
	ID             int64
	UserID         int64
	VideoID        int64
	FirstExposedAt time.Time
	LastExposedAt  time.Time
	ExposureCount  int
	LastScene      string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type ViewHistory struct {
	UserID         int64
	VideoID        int64
	LastScene      string
	LastEventType  string
	LastPositionMs int
	LastWatchMs    int
	Completed      bool
	FirstWatchedAt time.Time
	LastWatchedAt  time.Time
	LastOccurredAt time.Time
	LastEventID    string
	LastSessionID  string
	LastSequence   int64
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type HistoryCursor struct {
	LastWatchedAt time.Time
	VideoID       int64
}

// NewViewEvent 创建观看事件并完成基础参数清洗。
func NewViewEvent(input NewViewEventInput) (*ViewEvent, error) {
	if input.UserID <= 0 {
		return nil, ErrInvalidUserID
	}
	if input.VideoID <= 0 {
		return nil, ErrInvalidVideoID
	}

	scene := strings.TrimSpace(strings.ToLower(input.Scene))
	requestID := strings.TrimSpace(input.RequestID)
	eventType := strings.TrimSpace(strings.ToLower(input.EventType))
	eventID := strings.TrimSpace(input.EventID)
	playbackSessionID := strings.TrimSpace(input.PlaybackSessionID)
	if scene == "" {
		return nil, ErrEmptyScene
	}
	if len(scene) > MaxSceneLength {
		return nil, ErrSceneTooLong
	}
	if len(requestID) > MaxRequestIDLength {
		return nil, ErrRequestIDTooLong
	}
	if len(eventID) > MaxEventIDLength {
		return nil, ErrEventIDTooLong
	}
	if len(playbackSessionID) > MaxPlaybackSessionIDLength {
		return nil, ErrPlaybackSessionIDTooLong
	}
	if input.PositionMs < 0 {
		return nil, ErrPositionMsNegative
	}
	if input.PositionMs > MaxMediaDurationMs {
		return nil, ErrInvalidDurationMs
	}
	if input.WatchMs < 0 {
		return nil, ErrWatchMsNegative
	}
	if input.WatchMs > MaxMediaDurationMs {
		return nil, ErrInvalidDurationMs
	}
	if input.DurationMs != nil && (*input.DurationMs <= 0 || *input.DurationMs > MaxMediaDurationMs) {
		return nil, ErrInvalidDurationMs
	}
	if input.DurationMs != nil && input.PositionMs > *input.DurationMs {
		return nil, ErrInvalidDurationMs
	}
	if !isSupportedEventType(eventType) {
		return nil, ErrInvalidEventType
	}

	clientEnvelope := eventID != "" || playbackSessionID != "" || input.Sequence != 0 || !input.OccurredAt.IsZero() || input.PositionMs != 0 || input.DurationMs != nil
	if eventType == EventTypeProgress && !clientEnvelope {
		return nil, ErrEmptyEventID
	}
	if clientEnvelope {
		if eventID == "" {
			return nil, ErrEmptyEventID
		}
		if playbackSessionID == "" {
			return nil, ErrEmptyPlaybackSessionID
		}
		if input.Sequence <= 0 || input.Sequence > MaxSequence {
			return nil, ErrInvalidSequence
		}
		if input.OccurredAt.IsZero() {
			return nil, ErrEmptyOccurredAt
		}
	}

	var durationMs *int
	if input.DurationMs != nil {
		value := *input.DurationMs
		durationMs = &value
	}
	return &ViewEvent{
		UserID:            input.UserID,
		VideoID:           input.VideoID,
		Scene:             scene,
		RequestID:         requestID,
		EventType:         eventType,
		EventID:           eventID,
		PlaybackSessionID: playbackSessionID,
		Sequence:          input.Sequence,
		OccurredAt:        input.OccurredAt.UTC().Truncate(time.Microsecond),
		PositionMs:        input.PositionMs,
		WatchMs:           input.WatchMs,
		DurationMs:        durationMs,
		Completed:         input.Completed || eventType == EventTypeComplete,
		ClientEnvelope:    clientEnvelope,
	}, nil
}

// RestoreViewEvent 从数据库恢复观看事件。
func RestoreViewEvent(id int64, userID int64, videoID int64, scene string, requestID string, eventType string, eventID string, playbackSessionID string, sequence int64, occurredAt time.Time, positionMs int, watchMs int, durationMs *int, completed bool, createdAt time.Time) *ViewEvent {
	var duration *int
	if durationMs != nil {
		value := *durationMs
		duration = &value
	}
	return &ViewEvent{
		ID: id, UserID: userID, VideoID: videoID, Scene: strings.TrimSpace(scene),
		RequestID: strings.TrimSpace(requestID), EventType: strings.TrimSpace(eventType),
		EventID: strings.TrimSpace(eventID), PlaybackSessionID: strings.TrimSpace(playbackSessionID),
		Sequence: sequence, OccurredAt: occurredAt, PositionMs: positionMs, WatchMs: watchMs,
		DurationMs: duration, Completed: completed, CreatedAt: createdAt,
		ClientEnvelope: playbackSessionID != "" && sequence > 0,
	}
}

// RestoreExposure 从数据库恢复曝光聚合事实。
func RestoreExposure(id int64, userID int64, videoID int64, firstExposedAt time.Time, lastExposedAt time.Time, exposureCount int, lastScene string, createdAt time.Time, updatedAt time.Time) *Exposure {
	return &Exposure{
		ID:             id,
		UserID:         userID,
		VideoID:        videoID,
		FirstExposedAt: firstExposedAt,
		LastExposedAt:  lastExposedAt,
		ExposureCount:  exposureCount,
		LastScene:      strings.TrimSpace(lastScene),
		CreatedAt:      createdAt,
		UpdatedAt:      updatedAt,
	}
}

// CountsAsExposure 判断当前事件是否写入曝光聚合索引。
func (e *ViewEvent) CountsAsExposure() bool {
	return e != nil && e.EventType == EventTypeExposed
}

func (e *ViewEvent) CountsAsHistory() bool {
	if e == nil {
		return false
	}
	switch e.EventType {
	case EventTypePlay, EventTypeProgress, EventTypeComplete, EventTypeSkip:
		return true
	default:
		return false
	}
}

func RestoreViewHistory(userID, videoID int64, lastScene, lastEventType string, lastPositionMs, lastWatchMs int, completed bool, firstWatchedAt, lastWatchedAt, lastOccurredAt time.Time, lastEventID, lastSessionID string, lastSequence int64, createdAt, updatedAt time.Time) *ViewHistory {
	return &ViewHistory{
		UserID: userID, VideoID: videoID, LastScene: strings.TrimSpace(lastScene),
		LastEventType: strings.TrimSpace(lastEventType), LastPositionMs: lastPositionMs, LastWatchMs: lastWatchMs,
		Completed: completed, FirstWatchedAt: firstWatchedAt, LastWatchedAt: lastWatchedAt,
		LastOccurredAt: lastOccurredAt, LastEventID: strings.TrimSpace(lastEventID),
		LastSessionID: strings.TrimSpace(lastSessionID), LastSequence: lastSequence,
		CreatedAt: createdAt, UpdatedAt: updatedAt,
	}
}

func isSupportedEventType(eventType string) bool {
	switch eventType {
	case EventTypeExposed, EventTypePlay, EventTypeProgress, EventTypeComplete, EventTypeSkip:
		return true
	default:
		return false
	}
}

func (e *ViewEvent) SameNormalizedPayload(other *ViewEvent) bool {
	if e == nil || other == nil {
		return false
	}
	return e.UserID == other.UserID &&
		e.VideoID == other.VideoID &&
		e.Scene == other.Scene &&
		e.RequestID == other.RequestID &&
		e.EventType == other.EventType &&
		e.EventID == other.EventID &&
		e.PlaybackSessionID == other.PlaybackSessionID &&
		e.Sequence == other.Sequence &&
		e.OccurredAt.Equal(other.OccurredAt) &&
		e.PositionMs == other.PositionMs &&
		e.WatchMs == other.WatchMs &&
		sameOptionalInt(e.DurationMs, other.DurationMs) &&
		e.Completed == other.Completed
}

func sameOptionalInt(left, right *int) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}
