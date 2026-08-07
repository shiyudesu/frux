package domainmessage

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

const (
	LifecycleStageSubmitted       = "submitted"
	LifecycleStageReview          = "review"
	LifecycleStageMediaProcessing = "media_processing"
	LifecycleStagePublished       = "published"
	LifecycleStageEnforcement     = "enforcement"
	LifecycleStageRestoration     = "restoration"

	LifecycleResultPending   = "pending"
	LifecycleResultApproved  = "approved"
	LifecycleResultRejected  = "rejected"
	LifecycleResultFailed    = "failed"
	LifecycleResultPublic    = "public"
	LifecycleResultTakenDown = "taken_down"
	LifecycleResultRestored  = "restored"

	LifecycleReasonMediaProcessingFailed = "media_processing_failed"

	LifecycleOutboxPending   = "pending"
	LifecycleOutboxDelivered = "delivered"
	LifecycleOutboxTerminal  = "terminal"
)

type LifecycleNotification struct {
	EventID       string
	RecipientID   int64
	VideoID       int64
	ReviewVersion int
	Stage         string
	Result        string
	ReasonCode    string
	OccurredAt    time.Time
}

type LifecycleOutboxItem struct {
	LifecycleNotification
	State       string
	Attempts    int
	AvailableAt time.Time
	LeaseOwner  string
	LeaseUntil  *time.Time
	LastError   string
	DeliveredAt *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func ValidateLifecycle(stage, result, reasonCode string, videoID int64) error {
	stage = normalizeLifecycleToken(stage)
	result = normalizeLifecycleToken(result)
	reasonCode = normalizeLifecycleToken(reasonCode)
	if videoID <= 0 {
		return ErrInvalidMessageTarget
	}
	valid := false
	switch stage {
	case LifecycleStageSubmitted:
		valid = result == LifecycleResultPending && reasonCode == ""
	case LifecycleStageReview:
		valid = result == LifecycleResultApproved && reasonCode == "" ||
			result == LifecycleResultRejected && ValidReviewReasonCode(reasonCode)
	case LifecycleStageMediaProcessing:
		valid = result == LifecycleResultFailed &&
			reasonCode == LifecycleReasonMediaProcessingFailed
	case LifecycleStagePublished:
		valid = result == LifecycleResultPublic && reasonCode == ""
	case LifecycleStageEnforcement:
		valid = result == LifecycleResultTakenDown &&
			(reasonCode == "manual_enforcement" || reasonCode == "policy_violation")
	case LifecycleStageRestoration:
		valid = result == LifecycleResultRestored && reasonCode == "compliance_restored"
	}
	if !valid {
		return ErrInvalidLifecycle
	}
	return nil
}

func SubmissionEventID(videoID int64, reviewVersion int) string {
	return fmt.Sprintf("video-submitted:%d:%d", videoID, reviewVersion)
}

func ReviewEventID(videoID int64, reviewVersion int, result string) string {
	return fmt.Sprintf("video-review-%s:%d:%d", normalizeLifecycleToken(result), videoID, reviewVersion)
}

func PublicationEventID(videoID int64, reviewVersion int) string {
	return fmt.Sprintf("video-published:%d:%d", videoID, reviewVersion)
}

func MediaFailureEventID(videoID int64, assetID int64, profileVersion string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(profileVersion)))
	return fmt.Sprintf(
		"video-media-failed:%d:%d:%s",
		videoID, assetID, hex.EncodeToString(sum[:6]),
	)
}

func EnforcementEventID(videoID int64, actionID int64) string {
	return fmt.Sprintf("video-taken-down:%d:%d", videoID, actionID)
}

func RestorationEventID(videoID int64, actionID int64) string {
	return fmt.Sprintf("video-restored:%d:%d", videoID, actionID)
}

func ValidReviewReasonCode(reason string) bool {
	switch reason {
	case "sexual_content", "graphic_violence", "hate", "harassment",
		"self_harm", "illegal_activity", "spam", "other_policy_violation":
		return true
	default:
		return false
	}
}

func normalizeLifecycleToken(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}
