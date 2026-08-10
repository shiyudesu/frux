package applicationmedia

import (
	"context"
	"errors"
	"strings"
	"time"

	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
	inframetrics "github.com/shiyudesu/frux/internal/infra/metrics"
)

const (
	defaultLifecycleBatchSize    = 1
	defaultLifecyclePollInterval = time.Second
	defaultLifecycleLeaseTTL     = 5 * time.Minute
	defaultLifecycleRunTimeout   = 2 * time.Minute
)

type VideoLifecycleRepository interface {
	ClaimVideoLifecycleTasks(
		ctx context.Context,
		owner string,
		now, leaseUntil time.Time,
		limit int,
	) ([]*domainmedia.VideoLifecycleTask, error)
	UpdateVideoLifecycleTaskOwned(
		ctx context.Context,
		task *domainmedia.VideoLifecycleTask,
		owner string,
	) error
	VideoLifecycleBacklog(ctx context.Context) (int64, *time.Time, error)
}

type VideoLifecycleState struct {
	Exists         bool
	Status         int
	Visibility     string
	PublicEligible bool
}

type VideoLifecycleStateReader interface {
	ReadVideoLifecycleState(ctx context.Context, videoID int64) (VideoLifecycleState, error)
}

type VideoLifecycleDelivery interface {
	ProtectVideo(ctx context.Context, videoID, mediaAssetID, coverAssetID int64) error
	RestoreVideo(ctx context.Context, videoID, mediaAssetID, coverAssetID int64) error
}

type VideoCleanupScheduler interface {
	ScheduleMediaCleanup(ctx context.Context, mediaAssetID, coverAssetID int64) error
}

type VideoLifecycleService struct {
	repo     VideoLifecycleRepository
	videos   VideoLifecycleStateReader
	delivery VideoLifecycleDelivery
	cleanup  VideoCleanupScheduler
	now      func() time.Time
}

func NewVideoLifecycleService(
	repo VideoLifecycleRepository,
	videos VideoLifecycleStateReader,
	delivery VideoLifecycleDelivery,
	cleanup VideoCleanupScheduler,
) *VideoLifecycleService {
	return &VideoLifecycleService{
		repo: repo, videos: videos, delivery: delivery, cleanup: cleanup,
		now: func() time.Time { return time.Now().UTC() },
	}
}

func (s *VideoLifecycleService) RunOnce(
	ctx context.Context,
	owner string,
	limit int,
	leaseTTL time.Duration,
) (int, error) {
	if s == nil || s.repo == nil || s.videos == nil || s.delivery == nil ||
		s.cleanup == nil || strings.TrimSpace(owner) == "" || limit <= 0 {
		return 0, nil
	}
	if leaseTTL <= 0 {
		leaseTTL = defaultLifecycleLeaseTTL
	}
	now := s.now()
	tasks, err := s.repo.ClaimVideoLifecycleTasks(
		ctx, owner, now, now.Add(leaseTTL), limit,
	)
	if err != nil {
		return 0, err
	}
	completed := 0
	var runErr error
	for _, task := range tasks {
		if task == nil {
			continue
		}
		runTimeout := defaultLifecycleRunTimeout
		if runTimeout >= leaseTTL {
			runTimeout = leaseTTL / 2
		}
		if runTimeout <= 0 {
			runTimeout = time.Second
		}
		runCtx, cancel := context.WithTimeout(ctx, runTimeout)
		result, terminal, taskErr := s.apply(runCtx, task)
		cancel()
		finishedAt := s.now()
		switch {
		case taskErr == nil:
			task.State = domainmedia.JobStateCompleted
			task.ErrorCode = result
			task.CompletedAt = &finishedAt
			completed++
		case terminal:
			task.State = domainmedia.JobStateFailed
			task.ErrorCode = lifecycleErrorCode(taskErr, terminal)
			task.CompletedAt = &finishedAt
			runErr = errors.Join(runErr, taskErr)
		default:
			task.State = domainmedia.JobStateRetryable
			if task.Attempts >= task.MaxAttempts {
				task.ErrorCode = "attempts_exhausted"
			} else {
				task.ErrorCode = "retryable"
			}
			task.NextAttemptAt = finishedAt.Add(processingRetryDelay(task.Attempts))
			task.CompletedAt = nil
			runErr = errors.Join(runErr, taskErr)
		}
		if updateErr := s.repo.UpdateVideoLifecycleTaskOwned(ctx, task, owner); updateErr != nil {
			if errors.Is(updateErr, domainmedia.ErrLifecycleTaskLeaseLost) {
				inframetrics.ObserveMediaLifecycleTask("lease_lost")
			}
			return completed, errors.Join(runErr, updateErr)
		}
		switch task.State {
		case domainmedia.JobStateCompleted:
			if task.ErrorCode == "superseded" {
				inframetrics.ObserveMediaLifecycleTask("superseded")
			} else {
				inframetrics.ObserveMediaLifecycleTask("completed")
			}
		case domainmedia.JobStateRetryable:
			inframetrics.ObserveMediaLifecycleTask("retryable")
		case domainmedia.JobStateFailed:
			inframetrics.ObserveMediaLifecycleTask("failed")
		}
	}
	return completed, runErr
}

func (s *VideoLifecycleService) Backlog(
	ctx context.Context,
) (int64, *time.Time, error) {
	if s == nil || s.repo == nil {
		return 0, nil, nil
	}
	return s.repo.VideoLifecycleBacklog(ctx)
}

func (s *VideoLifecycleService) apply(
	ctx context.Context,
	task *domainmedia.VideoLifecycleTask,
) (string, bool, error) {
	state, err := s.videos.ReadVideoLifecycleState(ctx, task.VideoID)
	if err != nil {
		return "", false, err
	}
	if !state.Exists {
		return "", true, errors.New("video lifecycle target missing")
	}
	if !videoLifecycleTaskMatches(task, state) {
		if err := s.reconcileSuperseded(ctx, task, state, false); err != nil {
			return "", false, err
		}
		return "superseded", false, nil
	}
	if err := s.delivery.ProtectVideo(
		ctx, task.VideoID, task.MediaAssetID, task.CoverAssetID,
	); err != nil {
		return "", false, err
	}
	current, err := s.videos.ReadVideoLifecycleState(ctx, task.VideoID)
	if err != nil {
		return "", false, err
	}
	if !current.Exists {
		return "", true, errors.New("video lifecycle target missing")
	}
	if !videoLifecycleTaskMatches(task, current) {
		if err := s.reconcileSuperseded(ctx, task, current, true); err != nil {
			return "", false, err
		}
		return "superseded", false, nil
	}
	if task.Action == domainmedia.LifecycleActionDelete {
		if err := s.cleanup.ScheduleMediaCleanup(
			ctx, task.MediaAssetID, task.CoverAssetID,
		); err != nil {
			return "", false, err
		}
	}
	return "success", false, nil
}

func (s *VideoLifecycleService) reconcileSuperseded(
	ctx context.Context,
	task *domainmedia.VideoLifecycleTask,
	state VideoLifecycleState,
	alreadyProtected bool,
) error {
	if state.PublicEligible {
		return s.delivery.RestoreVideo(
			ctx, task.VideoID, task.MediaAssetID, task.CoverAssetID,
		)
	}
	if alreadyProtected {
		return nil
	}
	if err := s.delivery.ProtectVideo(
		ctx, task.VideoID, task.MediaAssetID, task.CoverAssetID,
	); err != nil {
		return err
	}
	current, err := s.videos.ReadVideoLifecycleState(ctx, task.VideoID)
	if err != nil {
		return err
	}
	if current.Exists && current.PublicEligible {
		return s.delivery.RestoreVideo(
			ctx, task.VideoID, task.MediaAssetID, task.CoverAssetID,
		)
	}
	return nil
}

func videoLifecycleTaskMatches(
	task *domainmedia.VideoLifecycleTask,
	state VideoLifecycleState,
) bool {
	if task.RequiredStatus > 0 && state.Status != task.RequiredStatus {
		return false
	}
	return task.RequiredVisibility == "" ||
		strings.ToLower(strings.TrimSpace(state.Visibility)) == task.RequiredVisibility
}

func lifecycleErrorCode(err error, terminal bool) string {
	if terminal {
		return "target_missing"
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return "timeout"
	}
	return "attempts_exhausted"
}

type VideoLifecycleWorker struct {
	service      *VideoLifecycleService
	owner        string
	pollInterval time.Duration
	leaseTTL     time.Duration
	batchSize    int
}

func NewVideoLifecycleWorker(
	service *VideoLifecycleService,
	owner string,
) *VideoLifecycleWorker {
	return &VideoLifecycleWorker{
		service: service, owner: owner,
		pollInterval: defaultLifecyclePollInterval,
		leaseTTL:     defaultLifecycleLeaseTTL,
		batchSize:    defaultLifecycleBatchSize,
	}
}

func (w *VideoLifecycleWorker) Start(ctx context.Context) {
	if w == nil || w.service == nil {
		return
	}
	run := func() {
		started := time.Now()
		_, err := w.service.RunOnce(
			ctx, w.owner, w.batchSize, w.leaseTTL,
		)
		inframetrics.ObserveWorkerJob(
			"media_video_lifecycle", time.Since(started), err,
		)
		count, oldest, statsErr := w.service.Backlog(ctx)
		inframetrics.ObserveMediaLifecycleBacklog(count, oldest, statsErr)
		if statsErr != nil {
			inframetrics.ObserveWorkerJob(
				"media_video_lifecycle_stats", 0, statsErr,
			)
		}
	}
	go func() {
		run()
		ticker := time.NewTicker(w.pollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				run()
			}
		}
	}()
}
