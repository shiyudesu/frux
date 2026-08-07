package applicationreview

import (
	"context"
	"errors"
	"time"

	domainreview "github.com/shiyudesu/frux/internal/domain/review"
)

type ModerationJobRepository interface {
	ClaimModerationJobs(
		ctx context.Context,
		leaseOwner string,
		limit int,
		leaseTTL time.Duration,
	) ([]*domainreview.ModerationJob, error)
	LoadModerationSubject(
		ctx context.Context,
		job *domainreview.ModerationJob,
	) (*domainreview.ModerationSubject, error)
	ModerationJobCurrent(ctx context.Context, job *domainreview.ModerationJob) (bool, error)
	ModerationResultAccepted(ctx context.Context, resultID string) (bool, error)
	LoadModerationProcessingResult(
		ctx context.Context,
		resultID string,
	) (*domainreview.ProcessingResult, error)
	RenewModerationJobLease(
		ctx context.Context,
		jobID int64,
		leaseOwner string,
		leaseTTL time.Duration,
	) error
	SaveModerationInputManifest(ctx context.Context, jobID int64, leaseOwner, manifestJSON string) error
	MarkModerationJobRetry(
		ctx context.Context,
		jobID int64,
		leaseOwner string,
		availableAt time.Time,
		errorCode string,
	) error
	MarkModerationJobSubmitted(
		ctx context.Context,
		jobID int64,
		leaseOwner string,
		submittedAt time.Time,
	) error
	MarkModerationJobTerminal(ctx context.Context, jobID int64, leaseOwner, errorCode string) error
	CancelModerationJob(ctx context.Context, jobID int64, leaseOwner, reason string) error
	ReconcileModerationJobs(
		ctx context.Context,
		config domainreview.ModerationJobConfig,
		limit int,
	) (domainreview.ModerationReconciliationStats, error)
}

type ModerationInputPreparer interface {
	Prepare(
		ctx context.Context,
		subject *domainreview.ModerationSubject,
		job *domainreview.ModerationJob,
	) (*domainreview.ModerationInputManifest, error)
	ResolveAccess(
		ctx context.Context,
		manifest *domainreview.ModerationInputManifest,
		expiry time.Duration,
	) ([]domainreview.ModerationFrameAccess, error)
}

type ModerationSampleCleanup interface {
	ScheduleModerationSampleCleanup(
		ctx context.Context,
		objectKeys []string,
		notBefore time.Time,
	) error
}

type ModerationProviderRequest struct {
	JobID                  int64
	CaseID                 int64
	VideoID                int64
	ReviewVersion          int
	RequestedPolicyVersion int
	RequestID              string
	Title                  string
	Description            string
	Frames                 []domainreview.ModerationFrameAccess
}

type ModerationProviderResult struct {
	Provider     string
	ModelVersion string
	GeneratedAt  time.Time
	Signals      []ModerationProviderSignal
}

type ModerationProviderSignal struct {
	Label             string
	Confidence        float64
	FrameTimestampsMS []int64
}

type ModerationProvider interface {
	Evaluate(
		ctx context.Context,
		request ModerationProviderRequest,
	) (*ModerationProviderResult, error)
}

type ModerationProviderError struct {
	Code      string
	Retryable bool
	Err       error
}

func (e *ModerationProviderError) Error() string {
	if e == nil {
		return "moderation provider failed"
	}
	if e.Err != nil {
		return e.Code + ": " + e.Err.Error()
	}
	return e.Code
}

func (e *ModerationProviderError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

type ModerationInputError struct {
	Code       string
	Terminal   bool
	ObjectKeys []string
	Err        error
}

func (e *ModerationInputError) Error() string {
	if e == nil {
		return "moderation input failed"
	}
	if e.Err != nil {
		return e.Code + ": " + e.Err.Error()
	}
	return e.Code
}

func (e *ModerationInputError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func moderationInputFailure(err error) (string, bool, []string) {
	var inputErr *ModerationInputError
	if errors.As(err, &inputErr) {
		return inputErr.Code, inputErr.Terminal, append([]string(nil), inputErr.ObjectKeys...)
	}
	return "input_unavailable", false, nil
}

func moderationProviderFailure(err error) (string, bool) {
	var providerErr *ModerationProviderError
	if errors.As(err, &providerErr) {
		return providerErr.Code, providerErr.Retryable
	}
	return "provider_unavailable", true
}
