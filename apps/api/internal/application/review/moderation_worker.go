package applicationreview

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	domainreview "github.com/shiyudesu/frux/internal/domain/review"
)

type ModerationObserver interface {
	ObserveModeration(operation, result string)
}

type ModerationWorkerConfig struct {
	JobConfig       domainreview.ModerationJobConfig
	LeaseTTL        time.Duration
	PollInterval    time.Duration
	SampleURLTTL    time.Duration
	SampleRetention time.Duration
	Concurrency     int
}

type ModerationWorker struct {
	repository ModerationJobRepository
	preparer   ModerationInputPreparer
	provider   ModerationProvider
	review     *Service
	cleanup    ModerationSampleCleanup
	observer   ModerationObserver
	config     ModerationWorkerConfig
	now        func() time.Time
}

func NewModerationWorker(
	repository ModerationJobRepository,
	preparer ModerationInputPreparer,
	provider ModerationProvider,
	review *Service,
	cleanup ModerationSampleCleanup,
	observer ModerationObserver,
	config ModerationWorkerConfig,
) (*ModerationWorker, error) {
	if repository == nil || review == nil ||
		domainreview.ValidateModerationJobConfig(config.JobConfig) != nil ||
		config.LeaseTTL <= 0 || config.PollInterval <= 0 ||
		config.SampleURLTTL <= 0 || config.SampleRetention <= 0 ||
		config.Concurrency < 1 || config.Concurrency > 32 {
		return nil, domainreview.ErrInvalidModerationJob
	}
	if config.JobConfig.Mode != domainreview.ModerationModeDisabled &&
		(preparer == nil || provider == nil) {
		return nil, domainreview.ErrInvalidModerationInput
	}
	return &ModerationWorker{
		repository: repository, preparer: preparer, provider: provider,
		review: review, cleanup: cleanup, observer: observer, config: config,
		now: func() time.Time { return time.Now().UTC() },
	}, nil
}

func (w *ModerationWorker) Run(ctx context.Context, owner string) {
	if w == nil {
		return
	}
	var wait sync.WaitGroup
	for index := 0; index < w.config.Concurrency; index++ {
		wait.Add(1)
		go func(workerIndex int) {
			defer wait.Done()
			workerOwner := fmt.Sprintf("%s-%d", strings.TrimSpace(owner), workerIndex)
			ticker := time.NewTicker(w.config.PollInterval)
			defer ticker.Stop()
			for {
				if err := w.RunOnce(ctx, workerOwner); err != nil {
					w.observe("loop", "retry")
				}
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
				}
			}
		}(index)
	}
	wait.Wait()
}

func (w *ModerationWorker) RunOnce(ctx context.Context, owner string) error {
	jobs, err := w.repository.ClaimModerationJobs(ctx, owner, 1, w.config.LeaseTTL)
	if err != nil {
		w.observe("claim", "retry")
		return err
	}
	if len(jobs) == 0 {
		return nil
	}
	w.observe("claim", "success")
	return w.processWithLease(ctx, owner, jobs[0])
}

func (w *ModerationWorker) processWithLease(
	ctx context.Context,
	owner string,
	job *domainreview.ModerationJob,
) error {
	processCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := make(chan struct{})
	done := make(chan struct{})
	leaseErrors := make(chan error, 1)
	interval := w.config.LeaseTTL / 3
	if interval < time.Second {
		interval = time.Second
	}
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-processCtx.Done():
				return
			case <-ticker.C:
				if err := w.repository.RenewModerationJobLease(
					processCtx, job.ID, owner, w.config.LeaseTTL,
				); err != nil {
					select {
					case leaseErrors <- err:
					default:
					}
					cancel()
					return
				}
			}
		}
	}()
	processErr := w.process(processCtx, owner, job)
	close(stop)
	<-done
	select {
	case leaseErr := <-leaseErrors:
		if processErr != nil {
			return errors.Join(processErr, leaseErr)
		}
		return nil
	default:
		return processErr
	}
}

func (w *ModerationWorker) Reconcile(ctx context.Context, limit int) error {
	stats, err := w.repository.ReconcileModerationJobs(ctx, w.config.JobConfig, limit)
	if err != nil {
		w.observe("reconciliation", "retry")
		return err
	}
	if stats.Created > 0 {
		w.observe("reconciliation", "created")
	}
	if stats.Cancelled > 0 {
		w.observe("reconciliation", "cancelled")
	}
	if stats.RecoveredLeases > 0 {
		w.observe("reconciliation", "recovered")
	}
	if stats.Created == 0 && stats.Cancelled == 0 && stats.RecoveredLeases == 0 {
		w.observe("reconciliation", "noop")
	}
	return nil
}

func (w *ModerationWorker) process(
	ctx context.Context,
	owner string,
	job *domainreview.ModerationJob,
) error {
	current, err := w.repository.ModerationJobCurrent(ctx, job)
	if err != nil {
		return w.retry(ctx, owner, job, "subject_check", err)
	}
	if !current {
		accepted, acceptedErr := w.repository.ModerationResultAccepted(ctx, job.ResultID)
		if acceptedErr != nil {
			return w.retry(ctx, owner, job, "result_check", acceptedErr)
		}
		if accepted {
			processed, loadErr := w.repository.LoadModerationProcessingResult(
				ctx, job.ResultID,
			)
			if loadErr != nil {
				return w.retry(ctx, owner, job, "result_load", loadErr)
			}
			if applyErr := w.review.ApplyMachineResultSideEffects(
				ctx, processed,
			); applyErr != nil {
				return w.retry(ctx, owner, job, "outcome_side_effect", applyErr)
			}
			manifest, _ := existingModerationManifest(job)
			return w.complete(ctx, owner, job, manifest)
		}
		w.observe("cancellation", "stale")
		return w.repository.CancelModerationJob(ctx, job.ID, owner, "stale_subject")
	}
	subject, err := w.repository.LoadModerationSubject(ctx, job)
	if errors.Is(err, domainreview.ErrModerationJobStale) {
		return w.repository.CancelModerationJob(ctx, job.ID, owner, "stale_subject")
	}
	if err != nil {
		return w.retry(ctx, owner, job, "subject_load", err)
	}
	incompatibleConfig := job.ProviderConfigVersion !=
		w.config.JobConfig.ProviderConfigVersion ||
		job.RolloutMode != w.config.JobConfig.Mode ||
		job.InputProfileVersion != w.config.JobConfig.InputProfileVersion
	if incompatibleConfig ||
		job.RolloutMode == domainreview.ModerationModeDisabled ||
		job.Attempts > job.MaxAttempts ||
		strings.HasPrefix(job.LastErrorCode, "recovery_") {
		code := job.LastErrorCode
		if code == "" {
			if incompatibleConfig {
				code = "provider_config_superseded"
			} else {
				code = "moderation_disabled"
			}
		}
		return w.submitRecovery(ctx, owner, job, subject, code)
	}
	manifest, err := existingModerationManifest(job)
	if manifest != nil &&
		!manifest.PreparedAt.Add(w.config.SampleRetention).
			After(w.now().Add(w.config.SampleURLTTL)) {
		if cleanupErr := w.scheduleManifestCleanupAt(ctx, manifest, w.now()); cleanupErr != nil {
			return w.retry(ctx, owner, job, "cleanup_schedule", cleanupErr)
		}
		manifest = nil
	}
	if err != nil || manifest == nil {
		manifest, err = w.preparer.Prepare(ctx, subject, job)
		if err != nil {
			code, terminal, objectKeys := moderationInputFailure(err)
			if cleanupErr := w.scheduleKeysCleanup(ctx, objectKeys, w.now()); cleanupErr != nil {
				return w.retry(ctx, owner, job, "cleanup_schedule", cleanupErr)
			}
			w.observe("extraction", resultForFailure(terminal))
			if terminal || job.Attempts >= job.MaxAttempts {
				return w.submitRecovery(ctx, owner, job, subject, code)
			}
			return w.retry(ctx, owner, job, code, err)
		}
		encoded, encodeErr := json.Marshal(manifest)
		if encodeErr != nil {
			return w.submitRecovery(ctx, owner, job, subject, "manifest_encode")
		}
		if err := w.repository.SaveModerationInputManifest(
			ctx, job.ID, owner, string(encoded),
		); err != nil {
			return err
		}
		job.InputManifestJSON = string(encoded)
		w.observe("extraction", "success")
	}
	if err := w.scheduleManifestCleanup(ctx, manifest); err != nil {
		return w.retry(ctx, owner, job, "cleanup_schedule", err)
	}
	access, err := w.preparer.ResolveAccess(ctx, manifest, w.config.SampleURLTTL)
	if err != nil {
		code, terminal, objectKeys := moderationInputFailure(err)
		if cleanupErr := w.scheduleKeysCleanup(ctx, objectKeys, w.now()); cleanupErr != nil {
			return w.retry(ctx, owner, job, "cleanup_schedule", cleanupErr)
		}
		if terminal || job.Attempts >= job.MaxAttempts {
			return w.submitRecovery(ctx, owner, job, subject, code)
		}
		return w.retry(ctx, owner, job, code, err)
	}
	result, err := w.provider.Evaluate(ctx, ModerationProviderRequest{
		JobID: job.ID, CaseID: job.CaseID, VideoID: job.VideoID,
		ReviewVersion: job.ReviewVersion, RequestedPolicyVersion: subject.PolicyVersion,
		RequestID: job.RequestID, Title: subject.Title, Description: subject.Description,
		Frames: access,
	})
	if err != nil {
		code, retryable := moderationProviderFailure(err)
		w.observe("provider_call", resultForFailure(!retryable))
		if !retryable || job.Attempts >= job.MaxAttempts {
			return w.submitRecovery(ctx, owner, job, subject, code)
		}
		return w.retry(ctx, owner, job, code, err)
	}
	signals, err := providerSignals(result.Signals, manifest)
	if err != nil {
		return w.submitRecovery(ctx, owner, job, subject, "response_evidence")
	}
	_, err = w.review.SubmitMachineResult(ctx, domainreview.MachineResultInput{
		CaseID: job.CaseID, VideoID: job.VideoID, ReviewVersion: job.ReviewVersion,
		ResultID: job.ResultID, Provider: result.Provider, ModelVersion: result.ModelVersion,
		SourceKind:  domainreview.MachineSourceProductionProvider,
		GeneratedAt: result.GeneratedAt, RolloutMode: job.RolloutMode,
		ModerationJobID: job.ID, ModerationLeaseOwner: owner,
		PolicyVersion: subject.PolicyVersion, Signals: signals, ReceivedAt: w.now(),
	})
	if err != nil {
		if staleModerationResult(err) {
			return w.repository.CancelModerationJob(ctx, job.ID, owner, "stale_subject")
		}
		return w.retry(ctx, owner, job, "result_delivery", err)
	}
	w.observe("result_submission", "success")
	return w.complete(ctx, owner, job, manifest)
}

func (w *ModerationWorker) submitRecovery(
	ctx context.Context,
	owner string,
	job *domainreview.ModerationJob,
	subject *domainreview.ModerationSubject,
	errorCode string,
) error {
	_ = errorCode
	_, err := w.review.SubmitMachineResult(ctx, domainreview.MachineResultInput{
		CaseID: job.CaseID, VideoID: job.VideoID, ReviewVersion: job.ReviewVersion,
		ResultID: job.ResultID, Provider: "frux-moderation-recovery",
		ModelVersion: "recovery-v1", SourceKind: domainreview.MachineSourceRecovery,
		GeneratedAt: job.CreatedAt, RolloutMode: job.RolloutMode,
		ModerationJobID: job.ID, ModerationLeaseOwner: owner,
		PolicyVersion: subject.PolicyVersion,
		Signals: []domainreview.MachineSignal{{
			Label: "moderation_unavailable", Confidence: 1,
			EvidenceRefs: []string{"error:moderation_unavailable"},
		}},
		ReceivedAt: w.now(),
	})
	if err != nil {
		if staleModerationResult(err) {
			return w.repository.CancelModerationJob(ctx, job.ID, owner, "stale_subject")
		}
		job.LastErrorCode = "recovery_delivery"
		return w.retry(ctx, owner, job, "recovery_delivery", err)
	}
	w.observe("fallback", "human")
	manifest, _ := existingModerationManifest(job)
	return w.complete(ctx, owner, job, manifest)
}

func (w *ModerationWorker) complete(
	ctx context.Context,
	owner string,
	job *domainreview.ModerationJob,
	manifest *domainreview.ModerationInputManifest,
) error {
	if err := w.scheduleManifestCleanupAt(ctx, manifest, w.now()); err != nil {
		return w.retry(ctx, owner, job, "cleanup_schedule", err)
	}
	return w.repository.MarkModerationJobSubmitted(ctx, job.ID, owner, w.now())
}

func (w *ModerationWorker) scheduleManifestCleanup(
	ctx context.Context,
	manifest *domainreview.ModerationInputManifest,
) error {
	if manifest == nil || w.cleanup == nil {
		return nil
	}
	keys := make([]string, 0, len(manifest.Frames))
	for _, frame := range manifest.Frames {
		keys = append(keys, frame.ObjectKey)
	}
	return w.cleanup.ScheduleModerationSampleCleanup(
		ctx, keys, manifest.PreparedAt.Add(w.config.SampleRetention),
	)
}

func (w *ModerationWorker) scheduleManifestCleanupAt(
	ctx context.Context,
	manifest *domainreview.ModerationInputManifest,
	notBefore time.Time,
) error {
	if manifest == nil {
		return nil
	}
	keys := make([]string, 0, len(manifest.Frames))
	for _, frame := range manifest.Frames {
		keys = append(keys, frame.ObjectKey)
	}
	return w.scheduleKeysCleanup(ctx, keys, notBefore)
}

func (w *ModerationWorker) scheduleKeysCleanup(
	ctx context.Context,
	keys []string,
	notBefore time.Time,
) error {
	if len(keys) == 0 || w.cleanup == nil {
		return nil
	}
	return w.cleanup.ScheduleModerationSampleCleanup(ctx, keys, notBefore)
}

func (w *ModerationWorker) retry(
	ctx context.Context,
	owner string,
	job *domainreview.ModerationJob,
	code string,
	cause error,
) error {
	if markErr := w.repository.MarkModerationJobRetry(
		ctx, job.ID, owner, w.now().Add(moderationBackoff(job.Attempts)), code,
	); markErr != nil {
		return errors.Join(cause, markErr)
	}
	w.observe("retry", "scheduled")
	return cause
}

func existingModerationManifest(
	job *domainreview.ModerationJob,
) (*domainreview.ModerationInputManifest, error) {
	value := strings.TrimSpace(job.InputManifestJSON)
	if value == "" || value == "{}" {
		return nil, nil
	}
	var manifest domainreview.ModerationInputManifest
	if err := json.Unmarshal([]byte(value), &manifest); err != nil {
		return nil, err
	}
	if manifest.ProfileVersion != job.InputProfileVersion {
		return nil, domainreview.ErrInvalidModerationInput
	}
	if err := domainreview.ValidateModerationInputManifest(&manifest); err != nil {
		return nil, err
	}
	return &manifest, nil
}

func providerSignals(
	signals []ModerationProviderSignal,
	manifest *domainreview.ModerationInputManifest,
) ([]domainreview.MachineSignal, error) {
	hashes := make(map[int64]string, len(manifest.Frames))
	for _, frame := range manifest.Frames {
		hashes[frame.TimestampMS] = frame.SHA256
	}
	result := make([]domainreview.MachineSignal, 0, len(signals))
	for _, signal := range signals {
		refs := make([]string, 0, len(signal.FrameTimestampsMS))
		for _, timestamp := range signal.FrameTimestampsMS {
			hash, exists := hashes[timestamp]
			if !exists {
				return nil, domainreview.ErrInvalidModerationInput
			}
			refs = append(refs, fmt.Sprintf("frame_ms:%d;sha256:%s", timestamp, hash))
		}
		result = append(result, domainreview.MachineSignal{
			Label: signal.Label, Confidence: signal.Confidence, EvidenceRefs: refs,
		})
	}
	return result, nil
}

func moderationBackoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > 7 {
		attempt = 7
	}
	delay := time.Second << (attempt - 1)
	if delay > time.Minute {
		return time.Minute
	}
	return delay
}

func staleModerationResult(err error) bool {
	return errors.Is(err, domainreview.ErrReviewCaseNotFound) ||
		errors.Is(err, domainreview.ErrReviewCaseNotOpen) ||
		errors.Is(err, domainreview.ErrReviewSubjectStale) ||
		errors.Is(err, domainreview.ErrReviewSubjectState)
}

func resultForFailure(terminal bool) string {
	if terminal {
		return "terminal"
	}
	return "retry"
}

func (w *ModerationWorker) observe(operation, result string) {
	if w != nil && w.observer != nil {
		w.observer.ObserveModeration(operation, result)
	}
}
