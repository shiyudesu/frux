package applicationmedia

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
	inframetrics "github.com/shiyudesu/frux/internal/infra/metrics"
)

const defaultProcessingPollInterval = 5 * time.Second

type ProcessResult struct {
	Width      int
	Height     int
	DurationMS int64
	VideoCodec string
	AudioCodec string
	Variants   []*domainmedia.MediaVariant
}

type ProcessError struct {
	Code     string
	Terminal bool
	Err      error
}

func (e *ProcessError) Error() string {
	if e == nil || e.Err == nil {
		return "media processing failed"
	}
	return e.Err.Error()
}

func (e *ProcessError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

type Processor interface {
	Process(ctx context.Context, asset *domainmedia.MediaAsset, job *domainmedia.MediaProcessingJob) (*ProcessResult, error)
}

type ProcessingRepository interface {
	FindAssetByID(ctx context.Context, assetID int64) (*domainmedia.MediaAsset, error)
	UpdateAsset(ctx context.Context, asset *domainmedia.MediaAsset) error
	UpsertVariants(ctx context.Context, variants []*domainmedia.MediaVariant) error
	LeaseProcessingJob(ctx context.Context, assetID int64, profileVersion, owner string, now time.Time, leaseUntil time.Time) (*domainmedia.MediaProcessingJob, error)
	LeaseProcessingJobs(ctx context.Context, owner string, now time.Time, leaseUntil time.Time, limit int) ([]*domainmedia.MediaProcessingJob, error)
	UpdateProcessingJob(ctx context.Context, job *domainmedia.MediaProcessingJob) error
	UpdateProcessingJobOwned(ctx context.Context, job *domainmedia.MediaProcessingJob, leaseOwner string) error
	ExtendProcessingLease(ctx context.Context, jobID int64, leaseOwner string, leaseUntil time.Time) error
	CreateCleanupTasks(ctx context.Context, tasks []*domainmedia.CleanupTask) error
}

type ProcessingConsumer interface {
	ConsumeMediaProcessingRequested(ctx context.Context, handler func(context.Context, *ProcessingRequestedEvent) error) error
}

type MediaStateNotifier interface {
	MediaReady(ctx context.Context, assetID int64) error
	MediaRepairing(ctx context.Context, assetID int64, errorCode string) error
	MediaFailed(ctx context.Context, assetID int64, profileVersion, errorCode string) error
}

type MediaProcessingWorker struct {
	repo         ProcessingRepository
	processor    Processor
	consumer     ProcessingConsumer
	owner        string
	leaseTTL     time.Duration
	pollInterval time.Duration
	concurrency  int
	now          func() time.Time
	notifier     MediaStateNotifier
}

type ProcessingWorkerOption func(*MediaProcessingWorker)

func WithMediaStateNotifier(notifier MediaStateNotifier) ProcessingWorkerOption {
	return func(worker *MediaProcessingWorker) {
		worker.notifier = notifier
	}
}

func NewMediaProcessingWorker(repo ProcessingRepository, processor Processor, consumer ProcessingConsumer, leaseTTL time.Duration, concurrency int, options ...ProcessingWorkerOption) *MediaProcessingWorker {
	owner, _ := os.Hostname()
	if owner == "" {
		owner = "frux-worker"
	}
	if leaseTTL <= 0 {
		leaseTTL = 10 * time.Minute
	}
	if concurrency <= 0 {
		concurrency = 1
	}
	worker := &MediaProcessingWorker{
		repo: repo, processor: processor, consumer: consumer, owner: owner,
		leaseTTL: leaseTTL, pollInterval: defaultProcessingPollInterval, concurrency: concurrency,
		now: func() time.Time { return time.Now().UTC() },
	}
	for _, option := range options {
		option(worker)
	}
	return worker
}

func (w *MediaProcessingWorker) Start(ctx context.Context) error {
	if w == nil || w.repo == nil || w.processor == nil {
		return nil
	}
	if w.consumer != nil {
		if err := w.consumer.ConsumeMediaProcessingRequested(ctx, w.HandleRequested); err != nil {
			return err
		}
	}
	go func() {
		ticker := time.NewTicker(w.pollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if _, err := w.ProcessPending(ctx); err != nil {
					inframetrics.ObserveWorkerJob("media_processing_poll", 0, err)
				}
			}
		}
	}()
	return nil
}

func (w *MediaProcessingWorker) HandleRequested(ctx context.Context, event *ProcessingRequestedEvent) error {
	if event == nil || event.AssetID <= 0 || event.ProfileVersion == "" {
		return nil
	}
	now := w.now()
	job, err := w.repo.LeaseProcessingJob(ctx, event.AssetID, event.ProfileVersion, w.owner, now, now.Add(w.leaseTTL))
	if errors.Is(err, domainmedia.ErrProcessingJobNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	return w.processLeased(ctx, job)
}

func (w *MediaProcessingWorker) ProcessPending(ctx context.Context) (int, error) {
	now := w.now()
	jobs, err := w.repo.LeaseProcessingJobs(ctx, w.owner, now, now.Add(w.leaseTTL), w.concurrency)
	if err != nil {
		return 0, err
	}
	processed := 0
	var processErr error
	for _, job := range jobs {
		if err := w.processLeased(ctx, job); err != nil {
			processErr = errors.Join(processErr, err)
			continue
		}
		processed++
	}
	return processed, processErr
}

func (w *MediaProcessingWorker) processLeased(ctx context.Context, job *domainmedia.MediaProcessingJob) (err error) {
	start := time.Now()
	defer func() {
		inframetrics.ObserveWorkerJob("media_processing", time.Since(start), err)
	}()
	asset, err := w.repo.FindAssetByID(ctx, job.AssetID)
	if err != nil {
		return w.failJob(ctx, nil, job, &ProcessError{Code: "asset_not_found", Terminal: true, Err: err})
	}
	leaseOwner := job.LeaseOwner
	if asset.State == domainmedia.AssetStateDeleted {
		return w.failJobOwned(ctx, asset, job, leaseOwner, &ProcessError{Code: "asset_deleted", Terminal: true, Err: errors.New("media asset is deleted")})
	}
	if asset.State == domainmedia.AssetStateReady {
		if w.notifier != nil {
			if err := w.notifier.MediaReady(ctx, asset.ID); err != nil {
				return err
			}
		}
		return w.completeJob(ctx, job, leaseOwner)
	}
	if asset.State == domainmedia.AssetStateFailed {
		if w.notifier != nil {
			if err := w.notifier.MediaFailed(ctx, asset.ID, job.ProfileVersion, asset.ErrorCode); err != nil {
				return err
			}
		}
		now := w.now()
		job.State = domainmedia.JobStateFailed
		job.ErrorCode = asset.ErrorCode
		job.LeaseOwner = ""
		job.LeaseUntil = nil
		job.CompletedAt = &now
		return w.repo.UpdateProcessingJobOwned(ctx, job, leaseOwner)
	}
	asset.State = domainmedia.AssetStateProcessing
	asset.ErrorCode = ""
	if err := w.repo.UpdateAsset(ctx, asset); err != nil {
		return err
	}
	processCtx, cancel := context.WithCancel(ctx)
	heartbeatDone := w.startLeaseHeartbeat(processCtx, cancel, job.ID, leaseOwner)
	defer func() {
		cancel()
		if heartbeatErr := <-heartbeatDone; err == nil && heartbeatErr != nil {
			err = heartbeatErr
		}
	}()
	result, err := w.processor.Process(processCtx, asset, job)
	if err != nil {
		return w.failJobOwned(ctx, asset, job, leaseOwner, err)
	}
	currentAsset, err := w.repo.FindAssetByID(ctx, asset.ID)
	if err != nil {
		return err
	}
	if currentAsset.State == domainmedia.AssetStateDeleted {
		return w.abortDeletedAsset(ctx, currentAsset, job, leaseOwner, result)
	}
	if result == nil || len(result.Variants) == 0 {
		return w.failJobOwned(ctx, asset, job, leaseOwner, &ProcessError{Code: "empty_outputs", Terminal: true, Err: errors.New("media processor produced no outputs")})
	}

	if err := w.repo.UpsertVariants(ctx, result.Variants); err != nil {
		inframetrics.ObserveMediaRenditions(len(result.Variants), err)
		return w.failJobOwned(ctx, asset, job, leaseOwner, &ProcessError{Code: "variant_persistence", Err: err})
	}
	inframetrics.ObserveMediaRenditions(len(result.Variants), nil)
	asset.Width = result.Width
	asset.Height = result.Height
	asset.DurationMS = result.DurationMS
	asset.VideoCodec = result.VideoCodec
	asset.AudioCodec = result.AudioCodec
	asset.State = domainmedia.AssetStateReady
	asset.ErrorCode = ""
	if err := w.repo.UpdateAsset(ctx, asset); err != nil {
		return err
	}
	if w.notifier != nil {
		if err := w.notifier.MediaReady(ctx, asset.ID); err != nil {
			return err
		}
	}
	return w.completeJob(ctx, job, leaseOwner)
}

func (w *MediaProcessingWorker) abortDeletedAsset(ctx context.Context, asset *domainmedia.MediaAsset, job *domainmedia.MediaProcessingJob, leaseOwner string, result *ProcessResult) error {
	now := w.now()
	tasks := make([]*domainmedia.CleanupTask, 0, len(result.Variants))
	for _, variant := range result.Variants {
		if variant == nil {
			continue
		}
		task, err := domainmedia.NewCleanupTask(asset.ID, asset.StorageBackend, variant.ObjectKey, now, job.MaxAttempts)
		if err != nil {
			return err
		}
		tasks = append(tasks, task)
	}
	if err := w.repo.CreateCleanupTasks(ctx, tasks); err != nil {
		return err
	}
	job.State = domainmedia.JobStateFailed
	job.ErrorCode = "asset_deleted"
	job.ErrorMessage = "media asset was deleted during processing"
	job.LeaseOwner = ""
	job.LeaseUntil = nil
	job.CompletedAt = &now
	return w.repo.UpdateProcessingJobOwned(ctx, job, leaseOwner)
}

func (w *MediaProcessingWorker) completeJob(ctx context.Context, job *domainmedia.MediaProcessingJob, leaseOwner string) error {
	now := w.now()
	job.State = domainmedia.JobStateCompleted
	job.ErrorCode = ""
	job.ErrorMessage = ""
	job.LeaseOwner = ""
	job.LeaseUntil = nil
	job.CompletedAt = &now
	err := w.repo.UpdateProcessingJobOwned(ctx, job, leaseOwner)
	if err == nil {
		inframetrics.ObserveMediaProcessing(domainmedia.JobStateCompleted, "")
	}
	return err
}

func (w *MediaProcessingWorker) failJob(ctx context.Context, asset *domainmedia.MediaAsset, job *domainmedia.MediaProcessingJob, processingErr error) error {
	return w.failJobOwned(ctx, asset, job, job.LeaseOwner, processingErr)
}

func (w *MediaProcessingWorker) failJobOwned(ctx context.Context, asset *domainmedia.MediaAsset, job *domainmedia.MediaProcessingJob, leaseOwner string, processingErr error) error {
	now := w.now()
	code := "processing_failed"
	terminal := job.Attempts >= job.MaxAttempts
	var typed *ProcessError
	if errors.As(processingErr, &typed) {
		if typed.Code != "" {
			code = typed.Code
		}
		terminal = terminal || typed.Terminal
	}
	job.ErrorCode = code
	job.ErrorMessage = truncateProcessingError(processingErr)
	job.LeaseOwner = ""
	job.LeaseUntil = nil
	if terminal {
		job.State = domainmedia.JobStateFailed
		job.CompletedAt = &now
	} else {
		job.State = domainmedia.JobStateRetryable
		job.NextAttemptAt = now.Add(processingRetryDelay(job.Attempts))
	}
	if asset != nil {
		asset.ErrorCode = code
		if terminal {
			asset.State = domainmedia.AssetStateFailed
		} else {
			asset.State = domainmedia.AssetStateUploaded
		}
		if err := w.repo.UpdateAsset(ctx, asset); err != nil {
			return err
		}
	}
	if terminal && asset != nil && w.notifier != nil {
		if err := w.notifier.MediaFailed(ctx, asset.ID, job.ProfileVersion, code); err != nil {
			return err
		}
	}
	if err := w.repo.UpdateProcessingJobOwned(ctx, job, leaseOwner); err != nil {
		return err
	}
	inframetrics.ObserveMediaProcessing(job.State, code)
	if terminal {
		return nil
	}
	return nil
}

func (w *MediaProcessingWorker) startLeaseHeartbeat(ctx context.Context, cancel context.CancelFunc, jobID int64, leaseOwner string) <-chan error {
	done := make(chan error, 1)
	interval := w.leaseTTL / 3
	if interval < time.Second {
		interval = time.Second
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				done <- nil
				return
			case <-ticker.C:
				if err := w.repo.ExtendProcessingLease(context.Background(), jobID, leaseOwner, w.now().Add(w.leaseTTL)); err != nil {
					cancel()
					done <- err
					return
				}
			}
		}
	}()
	return done
}

func processingRetryDelay(attempts int) time.Duration {
	if attempts < 1 {
		attempts = 1
	}
	delay := time.Second << min(attempts-1, 8)
	if delay > 5*time.Minute {
		return 5 * time.Minute
	}
	return delay
}

func truncateProcessingError(err error) string {
	value := fmt.Sprint(err)
	if len(value) > 512 {
		return value[:512]
	}
	return value
}
