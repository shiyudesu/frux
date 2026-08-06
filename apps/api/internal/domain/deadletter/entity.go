package domaindeadletter

import (
	"errors"
	"regexp"
	"strings"
	"time"
)

const (
	MaxPreviewLimit = 100
	MaxReasonLength = 64
)

var (
	ErrInvalidQueue      = errors.New("invalid dead-letter queue")
	ErrInvalidMessageID  = errors.New("invalid dead-letter message id")
	ErrInvalidReason     = errors.New("invalid dead-letter replay reason")
	ErrInvalidLimit      = errors.New("invalid dead-letter preview limit")
	ErrMessageNotFound   = errors.New("dead-letter message not found")
	ErrMessageNotAtHead  = errors.New("dead-letter message is not at queue head")
	ErrInspectionFailed  = errors.New("dead-letter inspection failed")
	ErrReplayFailed      = errors.New("dead-letter replay failed")
	ErrReplayUnconfirmed = errors.New("dead-letter replay publish unconfirmed")
	ErrReplayAuditFailed = errors.New("dead-letter replay audit failed")
	ErrReplayAckFailed   = errors.New("dead-letter replay acknowledgement failed")
)

var reasonPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

type QueueSummary struct {
	Consumer        string
	Queue           string
	Messages        int64
	MessagesReady   int64
	MessagesUnacked int64
	Consumers       int
	State           string
}

type MessagePreview struct {
	MessageID       string
	OriginalEventID string
	ReplayID        string
	ContentType     string
	Exchange        string
	RoutingKey      string
	PayloadBytes    int
	PayloadSHA256   string
	JSONValid       bool
	JSONFields      []string
	DeathCount      int64
	PublishedAt     time.Time
}

type ReplayMetadata struct {
	Queue           string
	MessageID       string
	OriginalEventID string
	Exchange        string
	RoutingKey      string
}

func NormalizeQueue(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 255 {
		return "", ErrInvalidQueue
	}
	return value, nil
}

func NormalizeMessageID(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 128 {
		return "", ErrInvalidMessageID
	}
	return value, nil
}

func NormalizeReason(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" || len(value) > MaxReasonLength || !reasonPattern.MatchString(value) {
		return "", ErrInvalidReason
	}
	return value, nil
}
