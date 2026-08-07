package domainreview

import (
	"fmt"
	"strings"
	"time"
)

const (
	ModerationJobPending   = "pending"
	ModerationJobLeased    = "leased"
	ModerationJobRetryWait = "retry_wait"
	ModerationJobSubmitted = "submitted"
	ModerationJobTerminal  = "terminal"
	ModerationJobCancelled = "cancelled"

	MaxModerationProfileVersionLength       = 64
	MaxModerationLeaseOwnerLength           = 128
	MaxModerationErrorCodeLength            = 64
	MaxModerationFrames                     = 12
	MaxModerationFrameEdge                  = 512
	MaxModerationInputBytes           int64 = 8 << 20
	MaxModerationManifestBytes              = 16 << 10
)

type ModerationJobConfig struct {
	Mode                  string
	ProviderConfigVersion int
	InputProfileVersion   string
	MaxAttempts           int
}

type ModerationJob struct {
	ID                    int64
	CaseID                int64
	VideoID               int64
	ReviewVersion         int
	ProviderConfigVersion int
	InputProfileVersion   string
	RolloutMode           string
	Status                string
	ResultID              string
	RequestID             string
	Attempts              int
	MaxAttempts           int
	AvailableAt           time.Time
	LeaseOwner            string
	LeaseUntil            *time.Time
	InputManifestJSON     string
	LastErrorCode         string
	SubmittedAt           *time.Time
	CancelledAt           *time.Time
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

type ModerationReconciliationStats struct {
	Created         int64
	Cancelled       int64
	RecoveredLeases int64
}

type ModerationSubject struct {
	CaseID          int64
	VideoID         int64
	ReviewVersion   int
	Title           string
	Description     string
	PolicyVersion   int
	SourceObjectKey string
}

type ModerationFrameSample struct {
	TimestampMS int64  `json:"timestamp_ms"`
	SHA256      string `json:"sha256"`
	ObjectKey   string `json:"object_key"`
	SizeBytes   int64  `json:"size_bytes"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
}

type ModerationInputManifest struct {
	ProfileVersion string                  `json:"profile_version"`
	DurationMS     int64                   `json:"duration_ms"`
	Frames         []ModerationFrameSample `json:"frames"`
	PreparedAt     time.Time               `json:"prepared_at"`
}

type ModerationFrameAccess struct {
	TimestampMS int64
	SHA256      string
	URL         string
	ExpiresAt   time.Time
}

func ValidateModerationInputManifest(manifest *ModerationInputManifest) error {
	if manifest == nil || strings.TrimSpace(manifest.ProfileVersion) == "" ||
		len(manifest.ProfileVersion) > MaxModerationProfileVersionLength ||
		manifest.DurationMS <= 0 || manifest.PreparedAt.IsZero() ||
		len(manifest.Frames) == 0 || len(manifest.Frames) > MaxModerationFrames {
		return ErrInvalidModerationInput
	}
	var total int64
	lastTimestamp := int64(-1)
	for _, frame := range manifest.Frames {
		if frame.TimestampMS < 0 || frame.TimestampMS >= manifest.DurationMS ||
			frame.TimestampMS <= lastTimestamp ||
			len(frame.SHA256) != 64 || !validHex(frame.SHA256) ||
			frame.SizeBytes <= 0 || frame.Width <= 0 || frame.Height <= 0 ||
			frame.Width > MaxModerationFrameEdge || frame.Height > MaxModerationFrameEdge {
			return ErrInvalidModerationInput
		}
		if frame.ObjectKey == "" || len(frame.ObjectKey) > 1024 {
			return ErrInvalidModerationInput
		}
		total += frame.SizeBytes
		if total > MaxModerationInputBytes {
			return ErrInvalidModerationInput
		}
		lastTimestamp = frame.TimestampMS
	}
	return nil
}

func validHex(value string) bool {
	for _, char := range value {
		if !((char >= '0' && char <= '9') || (char >= 'a' && char <= 'f')) {
			return false
		}
	}
	return true
}

func ValidateModerationJobConfig(config ModerationJobConfig) error {
	if !ValidModerationMode(config.Mode) ||
		config.ProviderConfigVersion <= 0 ||
		config.MaxAttempts < 1 || config.MaxAttempts > 10 {
		return ErrInvalidModerationJob
	}
	profile := strings.TrimSpace(config.InputProfileVersion)
	if !ValidModerationProfileVersion(profile) {
		return ErrInvalidModerationJob
	}
	return nil
}

func NewModerationJob(
	caseID int64,
	videoID int64,
	reviewVersion int,
	config ModerationJobConfig,
	now time.Time,
) (*ModerationJob, error) {
	if caseID <= 0 || videoID <= 0 || reviewVersion <= 0 ||
		now.IsZero() || ValidateModerationJobConfig(config) != nil {
		return nil, ErrInvalidModerationJob
	}
	now = now.UTC().Truncate(time.Microsecond)
	return &ModerationJob{
		CaseID: caseID, VideoID: videoID, ReviewVersion: reviewVersion,
		ProviderConfigVersion: config.ProviderConfigVersion,
		InputProfileVersion:   strings.TrimSpace(config.InputProfileVersion),
		RolloutMode:           normalizeToken(config.Mode),
		Status:                ModerationJobPending,
		ResultID:              ModerationResultID(caseID, reviewVersion, config.ProviderConfigVersion),
		RequestID:             ModerationRequestID(caseID, reviewVersion, config.ProviderConfigVersion),
		MaxAttempts:           config.MaxAttempts, AvailableAt: now,
		InputManifestJSON: "{}", CreatedAt: now, UpdatedAt: now,
	}, nil
}

func ModerationResultID(caseID int64, reviewVersion, providerConfigVersion int) string {
	return fmt.Sprintf("moderation-result:%d:%d:%d", caseID, reviewVersion, providerConfigVersion)
}

func ModerationRequestID(caseID int64, reviewVersion, providerConfigVersion int) string {
	return fmt.Sprintf("moderation-request:%d:%d:%d", caseID, reviewVersion, providerConfigVersion)
}

func ValidModerationJobStatus(status string) bool {
	switch normalizeToken(status) {
	case ModerationJobPending, ModerationJobLeased, ModerationJobRetryWait,
		ModerationJobSubmitted, ModerationJobTerminal, ModerationJobCancelled:
		return true
	default:
		return false
	}
}
