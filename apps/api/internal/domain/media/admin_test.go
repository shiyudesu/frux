package domainmedia

import (
	"errors"
	"testing"
	"time"
)

func TestNormalizeProcessingProgress(t *testing.T) {
	now := time.Now().UTC()
	progress := 5000
	got, err := NormalizeProcessingProgress(
		ProcessingStepTranscoding, &progress, now,
	)
	if err != nil || got.Step != ProcessingStepTranscoding ||
		got.ProgressBPS == nil || *got.ProgressBPS != 5000 {
		t.Fatalf("progress=%+v err=%v", got, err)
	}
	invalid := 10_001
	if _, err := NormalizeProcessingProgress(
		ProcessingStepTranscoding, &invalid, now,
	); !errors.Is(err, ErrInvalidProcessingProgress) {
		t.Fatalf("invalid progress error=%v", err)
	}
	if _, err := NormalizeProcessingProgress(
		"unknown", nil, now,
	); !errors.Is(err, ErrInvalidProcessingProgress) {
		t.Fatalf("unknown step error=%v", err)
	}
}

func TestNormalizeAdminProcessingRetryCommand(t *testing.T) {
	now := time.Now().UTC()
	command, err := NormalizeAdminProcessingRetryCommand(
		AdminProcessingRetryCommand{
			ActorID: 1, JobID: 2, VideoID: 3,
			ReasonCode:     ProcessingRetryReasonTemporaryFailure,
			IdempotencyKey: "retry-key",
			Route:          "/api/admin/media-processing/jobs/:jobId/retry",
			OccurredAt:     now,
		},
	)
	if err != nil || command.Fingerprint() == "" || command.EventID() == "" {
		t.Fatalf("command=%+v err=%v", command, err)
	}
	command.ReasonCode = "unknown"
	if _, err := NormalizeAdminProcessingRetryCommand(command); !errors.Is(
		err, ErrInvalidProcessingRetry,
	) {
		t.Fatalf("invalid reason error=%v", err)
	}
}

func TestNormalizeAdminProcessingHistoryQuery(t *testing.T) {
	now := time.Now().UTC()
	from, to := now.Add(-time.Hour), now
	query, err := NormalizeAdminProcessingHistoryQuery(
		AdminProcessingHistoryQuery{
			State: JobStateFailed, Step: ProcessingStepFailed,
			CompletedFrom: &from, CompletedTo: &to,
		},
	)
	if err != nil || query.Limit != 20 {
		t.Fatalf("query=%+v err=%v", query, err)
	}
	query.State = JobStateProcessing
	if _, err := NormalizeAdminProcessingHistoryQuery(query); !errors.Is(
		err, ErrInvalidProcessingAdminQuery,
	) {
		t.Fatalf("invalid state error=%v", err)
	}
}
