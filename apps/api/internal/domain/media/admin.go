package domainmedia

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"time"
)

const (
	ProcessingStepWaiting      = "waiting"
	ProcessingStepDownloading  = "downloading"
	ProcessingStepInspecting   = "inspecting"
	ProcessingStepRemuxing     = "remuxing"
	ProcessingStepTranscoding  = "transcoding"
	ProcessingStepUploading    = "uploading"
	ProcessingStepFinalizing   = "finalizing"
	ProcessingStepCompleted    = "completed"
	ProcessingStepFailed       = "failed"
	MaxProcessingProgressBPS   = 10_000
	MaxAdminProcessingPageSize = 100
	MaxAdminRetryBatchSize     = 50
	MaxAdminRetryNoteLength    = 500

	ProcessingRetryReasonConfigurationChanged = "configuration_changed"
	ProcessingRetryReasonTemporaryFailure     = "temporary_failure"
	ProcessingRetryReasonOperatorRetry        = "operator_retry"

	RetryNotificationPending   = "pending"
	RetryNotificationDelivered = "delivered"
	RetryNotificationTerminal  = "terminal"
)

var processingSteps = map[string]struct{}{
	ProcessingStepWaiting: {}, ProcessingStepDownloading: {},
	ProcessingStepInspecting: {}, ProcessingStepRemuxing: {},
	ProcessingStepTranscoding: {}, ProcessingStepUploading: {},
	ProcessingStepFinalizing: {}, ProcessingStepCompleted: {},
	ProcessingStepFailed: {},
}

var processingRetryReasons = map[string]struct{}{
	ProcessingRetryReasonConfigurationChanged: {},
	ProcessingRetryReasonTemporaryFailure:     {},
	ProcessingRetryReasonOperatorRetry:        {},
}

type ProcessingProgress struct {
	Step        string
	ProgressBPS *int
	UpdatedAt   time.Time
}

type AdminProcessingSummary struct {
	Waiting         int64
	Processing      int64
	Failed          int64
	Completed       int64
	OldestWaitingAt *time.Time
}

type AdminProcessingCursor struct {
	CompletedAt time.Time
	JobID       int64
}

type AdminProcessingHistoryQuery struct {
	State         string
	Step          string
	ErrorCode     string
	AssetID       int64
	CompletedFrom *time.Time
	CompletedTo   *time.Time
	Cursor        *AdminProcessingCursor
	Limit         int
}

type AdminProcessingRetryCommand struct {
	ActorID        int64
	JobID          int64
	VideoID        int64
	ReasonCode     string
	Note           string
	Route          string
	IdempotencyKey string
	OccurredAt     time.Time
}

type AdminProcessingRetryResult struct {
	Job      *MediaProcessingJob
	Replayed bool
}

type RetryNotificationOutboxItem struct {
	EventID     string
	JobID       int64
	AssetID     int64
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

type ProcessingRetryAuditInput struct {
	AssetID          int64
	VideoID          int64
	PreviousState    string
	NewState         string
	PreviousAttempts int
}

func ValidProcessingStep(step string) bool {
	_, ok := processingSteps[strings.TrimSpace(step)]
	return ok
}

func ValidProcessingRetryReason(reason string) bool {
	_, ok := processingRetryReasons[strings.TrimSpace(reason)]
	return ok
}

func NormalizeProcessingProgress(
	step string,
	progressBPS *int,
	updatedAt time.Time,
) (ProcessingProgress, error) {
	step = strings.TrimSpace(step)
	if !ValidProcessingStep(step) || updatedAt.IsZero() {
		return ProcessingProgress{}, ErrInvalidProcessingProgress
	}
	if progressBPS != nil && (*progressBPS < 0 || *progressBPS > MaxProcessingProgressBPS) {
		return ProcessingProgress{}, ErrInvalidProcessingProgress
	}
	return ProcessingProgress{
		Step: step, ProgressBPS: cloneInt(progressBPS), UpdatedAt: updatedAt.UTC(),
	}, nil
}

func NormalizeAdminProcessingHistoryQuery(
	query AdminProcessingHistoryQuery,
) (AdminProcessingHistoryQuery, error) {
	query.State = strings.TrimSpace(query.State)
	query.Step = strings.TrimSpace(query.Step)
	query.ErrorCode = strings.TrimSpace(query.ErrorCode)
	if query.State != "" &&
		query.State != JobStateCompleted && query.State != JobStateFailed {
		return AdminProcessingHistoryQuery{}, ErrInvalidProcessingAdminQuery
	}
	if query.Step != "" && !ValidProcessingStep(query.Step) {
		return AdminProcessingHistoryQuery{}, ErrInvalidProcessingAdminQuery
	}
	if query.AssetID < 0 || len(query.ErrorCode) > 64 {
		return AdminProcessingHistoryQuery{}, ErrInvalidProcessingAdminQuery
	}
	if (query.CompletedFrom == nil) != (query.CompletedTo == nil) {
		return AdminProcessingHistoryQuery{}, ErrInvalidProcessingAdminQuery
	}
	if query.CompletedFrom != nil {
		from, to := query.CompletedFrom.UTC(), query.CompletedTo.UTC()
		if from.After(to) || to.Sub(from) > 31*24*time.Hour {
			return AdminProcessingHistoryQuery{}, ErrInvalidProcessingAdminQuery
		}
		query.CompletedFrom, query.CompletedTo = &from, &to
	}
	if query.Limit == 0 {
		query.Limit = 20
	}
	if query.Limit < 1 || query.Limit > MaxAdminProcessingPageSize+1 {
		return AdminProcessingHistoryQuery{}, ErrInvalidProcessingAdminQuery
	}
	if query.Cursor != nil &&
		(query.Cursor.JobID <= 0 || query.Cursor.CompletedAt.IsZero()) {
		return AdminProcessingHistoryQuery{}, ErrInvalidProcessingAdminCursor
	}
	return query, nil
}

func NormalizeAdminProcessingRetryCommand(
	command AdminProcessingRetryCommand,
) (AdminProcessingRetryCommand, error) {
	command.ReasonCode = strings.TrimSpace(command.ReasonCode)
	command.Note = strings.TrimSpace(command.Note)
	command.Route = strings.TrimSpace(command.Route)
	command.IdempotencyKey = strings.TrimSpace(command.IdempotencyKey)
	command.OccurredAt = command.OccurredAt.UTC()
	if command.ActorID <= 0 || command.JobID <= 0 || command.VideoID <= 0 {
		return AdminProcessingRetryCommand{}, ErrInvalidProcessingRetry
	}
	if !ValidProcessingRetryReason(command.ReasonCode) ||
		len([]rune(command.Note)) > MaxAdminRetryNoteLength ||
		command.IdempotencyKey == "" ||
		len(command.IdempotencyKey) > MaxIdempotencyKeyLength ||
		(command.Route != "/api/admin/media-processing/jobs/:jobId/retry" &&
			command.Route != "/api/admin/media-processing/jobs/bulk-retry") ||
		command.OccurredAt.IsZero() {
		return AdminProcessingRetryCommand{}, ErrInvalidProcessingRetry
	}
	return command, nil
}

func (command AdminProcessingRetryCommand) Fingerprint() string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		strconv.FormatInt(command.JobID, 10),
		strconv.FormatInt(command.VideoID, 10),
		command.ReasonCode,
		command.Note,
		command.Route,
	}, "\x00")))
	return hex.EncodeToString(sum[:])
}

func (command AdminProcessingRetryCommand) EventID() string {
	sum := sha256.Sum256([]byte(command.IdempotencyKey))
	return "media-retry:" + strconv.FormatInt(command.JobID, 10) + ":" +
		hex.EncodeToString(sum[:8])
}

func cloneInt(value *int) *int {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
