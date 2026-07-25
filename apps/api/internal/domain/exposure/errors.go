package domainexposure

import "errors"

// 曝光领域错误描述观看上报中的参数和资源状态问题。
var ErrInvalidUserID = errors.New("user id must be positive")
var ErrInvalidVideoID = errors.New("video id must be positive")
var ErrEmptyScene = errors.New("scene is required")
var ErrSceneTooLong = errors.New("scene is too long")
var ErrInvalidEventType = errors.New("event type is invalid")
var ErrRequestIDTooLong = errors.New("request id is too long")
var ErrEmptyEventID = errors.New("event id is required")
var ErrEventIDTooLong = errors.New("event id is too long")
var ErrEmptyPlaybackSessionID = errors.New("playback session id is required")
var ErrPlaybackSessionIDTooLong = errors.New("playback session id is too long")
var ErrInvalidSequence = errors.New("sequence is invalid")
var ErrEmptyOccurredAt = errors.New("occurred at is required")
var ErrOccurredAtOutOfRange = errors.New("occurred at is outside the accepted window")
var ErrPositionMsNegative = errors.New("position ms must be non-negative")
var ErrWatchMsNegative = errors.New("watch ms must be non-negative")
var ErrInvalidDurationMs = errors.New("duration ms is invalid")
var ErrEventIDConflict = errors.New("event id is already used by another payload")
var ErrVideoNotFound = errors.New("video not found")
