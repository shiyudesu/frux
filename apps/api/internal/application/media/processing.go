package applicationmedia

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync/atomic"
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
	ExtendProcessingLease(ctx context.Context, jobID int64, leaseOwner string, leaseTTL time.Duration) error
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
	leaseTTL     time.Duration
	heartbeatTTL time.Duration
	pollInterval time.Duration
	concurrency  int
	now          func() time.Time
	notifier     MediaStateNotifier
	schedule     chan processingRequest
	slots        chan struct{}
	claimCounter atomic.Uint64
}

type processingRequest struct {
	event *ProcessingRequestedEvent
}

type ProcessingWorkerOption func(*MediaProcessingWorker)

func WithMediaStateNotifier(notifier MediaStateNotifier) ProcessingWorkerOption {
	return func(worker *MediaProcessingWorker) {
		worker.notifier = notifier
	}
}

func NewMediaProcessingWorker(repo ProcessingRepository, processor Processor, consumer ProcessingConsumer, leaseTTL time.Duration, concurrency int, options ...ProcessingWorkerOption) *MediaProcessingWorker {
	if leaseTTL <= 0 {
		leaseTTL = 10 * time.Minute
	}
	if concurrency <= 0 {
		concurrency = 1
	}
	heartbeatTTL := leaseTTL / 6
	if heartbeatTTL < 100*time.Millisecond {
		heartbeatTTL = 100 * time.Millisecond
	}
	if heartbeatTTL > 5*time.Second {
		heartbeatTTL = 5 * time.Second
	}
	worker := &MediaProcessingWorker{
		repo: repo, processor: processor, consumer: consumer,
		leaseTTL: leaseTTL, pollInterval: defaultProcessingPollInterval, concurrency: concurrency,
		heartbeatTTL: heartbeatTTL,
		now:          func() time.Time { return time.Now().UTC() },
		schedule:     make(chan processingRequest, concurrency),
		slots:        make(chan struct{}, concurrency),
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
		if err := w.consumer.ConsumeMediaProcessingRequested(ctx, w.SignalRequested); err != nil {
			return err
		}
	}
	for range w.concurrency {
		go w.runSchedulerWorker(ctx)
	}
	go func() {
		ticker := time.NewTicker(w.pollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				for range w.concurrency {
					select {
					case w.schedule <- processingRequest{}:
					default:
					}
				}
			}
		}
	}()
	return nil
}

func (w *MediaProcessingWorker) SignalRequested(
	ctx context.Context,
	event *ProcessingRequestedEvent,
) error {
	if event == nil || event.AssetID <= 0 || event.ProfileVersion == "" {
		return nil
	}
	if reader, ok := w.repo.(interface {
		FindProcessingJobByAsset(context.Context, int64) (*domainmedia.MediaProcessingJob, error)
	}); ok {
		job, err := reader.FindProcessingJobByAsset(ctx, event.AssetID)
		if errors.Is(err, domainmedia.ErrProcessingJobNotFound) {
			inframetrics.ObserveMediaWakeup("missing_job")
			return nil
		}
		if err != nil {
			inframetrics.ObserveMediaWakeup("validation_failed")
			return err
		}
		if job == nil || job.ProfileVersion != event.ProfileVersion {
			inframetrics.ObserveMediaWakeup("stale")
			return nil
		}
	}
	select {
	case w.schedule <- processingRequest{event: event}:
		inframetrics.ObserveMediaWakeup("signaled")
	default:
		inframetrics.ObserveMediaWakeup("capacity_full")
	}
	return nil
}

func (w *MediaProcessingWorker) runSchedulerWorker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case request := <-w.schedule:
			if request.event != nil {
				if err := w.HandleRequested(ctx, request.event); err != nil {
					inframetrics.ObserveWorkerJob("media_processing_wakeup", 0, err)
				}
				continue
			}
			if _, err := w.ProcessPending(ctx); err != nil {
				inframetrics.ObserveWorkerJob("media_processing_poll", 0, err)
			}
		}
	}
}

func (w *MediaProcessingWorker) HandleRequested(ctx context.Context, event *ProcessingRequestedEvent) error {
	if event == nil || event.AssetID <= 0 || event.ProfileVersion == "" {
		return nil
	}
	if err := w.acquireSlot(ctx); err != nil {
		return err
	}
	defer w.releaseSlot()
	now := w.now()
	claimToken, err := w.claimToken()
	if err != nil {
		return err
	}
	job, err := w.repo.LeaseProcessingJob(
		ctx, event.AssetID, event.ProfileVersion, claimToken, now, now.Add(w.leaseTTL),
	)
	if errors.Is(err, domainmedia.ErrProcessingJobNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	return w.processLeased(ctx, job)
}

func (w *MediaProcessingWorker) ProcessPending(ctx context.Context) (int, error) {
	if err := w.acquireSlot(ctx); err != nil {
		return 0, err
	}
	defer w.releaseSlot()
	now := w.now()
	claimToken, err := w.claimToken()
	if err != nil {
		return 0, err
	}
	jobs, err := w.repo.LeaseProcessingJobs(
		ctx, claimToken, now, now.Add(w.leaseTTL), 1,
	)
	if err != nil {
		return 0, err
	}
	if len(jobs) > 0 {
		inframetrics.ObserveMediaWakeup("polling_recovery")
	}
	for _, job := range jobs {
		if err := w.processLeased(ctx, job); err != nil {
			return 0, err
		}
		return 1, nil
	}
	return 0, nil
}

func (w *MediaProcessingWorker) processLeased(ctx context.Context, job *domainmedia.MediaProcessingJob) (err error) {
	start := time.Now()
	defer func() {
		inframetrics.ObserveWorkerJob("media_processing", time.Since(start), err)
	}()
	if job == nil {
		return nil
	}
	leaseOwner := job.LeaseOwner
	if err := w.renewLease(ctx, job.ID, leaseOwner); err != nil {
		return err
	}
	asset, err := w.repo.FindAssetByID(ctx, job.AssetID)
	if err != nil {
		return w.failJob(ctx, nil, job, &ProcessError{Code: "asset_not_found", Terminal: true, Err: err})
	}
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
		err = errors.Join(err, <-heartbeatDone)
	}()
	result, processErr := w.processor.Process(processCtx, asset, job)
	select {
	case heartbeatErr := <-heartbeatDone:
		heartbeatDone = closedHeartbeatResult()
		if heartbeatErr != nil {
			return heartbeatErr
		}
	default:
	}
	if processErr != nil {
		return w.failJobOwned(processCtx, asset, job, leaseOwner, processErr)
	}
	if err := w.renewLease(processCtx, job.ID, leaseOwner); err != nil {
		return err
	}
	currentAsset, err := w.repo.FindAssetByID(processCtx, asset.ID)
	if err != nil {
		return err
	}
	if currentAsset.State == domainmedia.AssetStateDeleted {
		return w.abortDeletedAsset(processCtx, currentAsset, job, leaseOwner, result)
	}
	if result == nil || len(result.Variants) == 0 {
		return w.failJobOwned(processCtx, asset, job, leaseOwner, &ProcessError{Code: "empty_outputs", Terminal: true, Err: errors.New("media processor produced no outputs")})
	}

	if err := w.repo.UpsertVariants(processCtx, result.Variants); err != nil {
		inframetrics.ObserveMediaRenditions(len(result.Variants), err)
		return w.failJobOwned(processCtx, asset, job, leaseOwner, &ProcessError{Code: "variant_persistence", Err: err})
	}
	inframetrics.ObserveMediaRenditions(len(result.Variants), nil)
	asset.Width = result.Width
	asset.Height = result.Height
	asset.DurationMS = result.DurationMS
	asset.VideoCodec = result.VideoCodec
	asset.AudioCodec = result.AudioCodec
	asset.State = domainmedia.AssetStateReady
	asset.ErrorCode = ""
	if err := w.repo.UpdateAsset(processCtx, asset); err != nil {
		return err
	}
	if w.notifier != nil {
		if err := w.notifier.MediaReady(processCtx, asset.ID); err != nil {
			return err
		}
	}
	return w.completeJob(processCtx, job, leaseOwner)
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
	if interval < 100*time.Millisecond {
		interval = 100 * time.Millisecond
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
				if err := w.renewLease(ctx, jobID, leaseOwner); err != nil {
					cancel()
					done <- err
					return
				}
			}
		}
	}()
	return done
}

func (w *MediaProcessingWorker) renewLease(
	ctx context.Context,
	jobID int64,
	claimToken string,
) error {
	heartbeatCtx, cancel := context.WithTimeout(ctx, w.heartbeatTTL)
	defer cancel()
	return w.repo.ExtendProcessingLease(
		heartbeatCtx, jobID, claimToken, w.leaseTTL,
	)
}

func (w *MediaProcessingWorker) acquireSlot(ctx context.Context) error {
	select {
	case w.slots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (w *MediaProcessingWorker) releaseSlot() {
	<-w.slots
}

func (w *MediaProcessingWorker) claimToken() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err == nil {
		return "media:" + hex.EncodeToString(value[:]), nil
	}

	sequence := w.claimCounter.Add(1)
	if sequence == 0 {
		return "", errors.New("media claim token unavailable")
	}
	return fmt.Sprintf("media:fallback:%d:%d", time.Now().UnixNano(), sequence), nil
}

func closedHeartbeatResult() <-chan error {
	done := make(chan error, 1)
	done <- nil
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
