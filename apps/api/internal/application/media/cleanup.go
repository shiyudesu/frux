package applicationmedia

import (
	"context"
	"errors"
	"time"

	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
	inframetrics "github.com/shiyudesu/frux/internal/infra/metrics"
)

type CleanupRepository interface {
	FindAssetByID(ctx context.Context, assetID int64) (*domainmedia.MediaAsset, error)
	UpdateAsset(ctx context.Context, asset *domainmedia.MediaAsset) error
	ListReadyVariants(ctx context.Context, assetID int64) ([]*domainmedia.MediaVariant, error)
	ScheduleAssetCleanup(ctx context.Context, assetID int64, notBefore time.Time, maxAttempts int) error
	CreateCleanupTasks(ctx context.Context, tasks []*domainmedia.CleanupTask) error
	LeaseCleanupTasks(ctx context.Context, owner string, now time.Time, leaseUntil time.Time, limit int) ([]*domainmedia.CleanupTask, error)
	UpdateCleanupTask(ctx context.Context, task *domainmedia.CleanupTask) error
	UpdateCleanupTaskOwned(ctx context.Context, task *domainmedia.CleanupTask, leaseOwner string) error
	RenewCleanupTaskLease(ctx context.Context, taskID int64, leaseOwner string, leaseTTL time.Duration) error
	ReleaseExpiredCleanupLeases(ctx context.Context, now time.Time) (int64, error)
}

type CleanupService struct {
	repo        CleanupRepository
	store       domainmedia.MediaObjectStore
	backend     string
	delay       time.Duration
	maxAttempts int
	now         func() time.Time
}

func NewCleanupService(repo CleanupRepository, store domainmedia.MediaObjectStore, backend string, delay time.Duration, maxAttempts int) *CleanupService {
	if delay <= 0 {
		delay = 24 * time.Hour
	}
	if maxAttempts <= 0 {
		maxAttempts = 5
	}
	return &CleanupService{
		repo: repo, store: store, backend: backend, delay: delay, maxAttempts: maxAttempts,
		now: func() time.Time { return time.Now().UTC() },
	}
}

func (s *CleanupService) ScheduleMediaCleanup(ctx context.Context, mediaAssetID, coverAssetID int64) error {
	if s == nil || s.repo == nil {
		return nil
	}
	seen := map[int64]struct{}{}
	for _, assetID := range []int64{mediaAssetID, coverAssetID} {
		if assetID <= 0 {
			continue
		}
		if _, exists := seen[assetID]; exists {
			continue
		}
		seen[assetID] = struct{}{}
		notBefore := s.now().Add(s.delay)
		if err := s.repo.ScheduleAssetCleanup(
			ctx, assetID, notBefore, s.maxAttempts,
		); err != nil && !errors.Is(err, domainmedia.ErrMediaAssetNotFound) {
			return err
		}
	}
	return nil
}

func (s *CleanupService) ScheduleObjectCleanup(
	ctx context.Context,
	objectKeys []string,
	notBefore time.Time,
) error {
	if s == nil || s.repo == nil || len(objectKeys) == 0 {
		return nil
	}
	tasks := make([]*domainmedia.CleanupTask, 0, len(objectKeys))
	seen := make(map[string]struct{}, len(objectKeys))
	for _, objectKey := range objectKeys {
		if _, exists := seen[objectKey]; exists {
			continue
		}
		seen[objectKey] = struct{}{}
		task, err := domainmedia.NewCleanupTask(
			0, s.backend, objectKey, notBefore, s.maxAttempts,
		)
		if err != nil {
			return err
		}
		tasks = append(tasks, task)
	}
	return s.repo.CreateCleanupTasks(ctx, tasks)
}

func (s *CleanupService) ScheduleModerationSampleCleanup(
	ctx context.Context,
	objectKeys []string,
	notBefore time.Time,
) error {
	return s.ScheduleObjectCleanup(ctx, objectKeys, notBefore)
}

func (s *CleanupService) RunCleanupOnce(ctx context.Context, owner string, limit int, leaseTTL time.Duration) (int, error) {
	if s == nil || s.repo == nil || s.store == nil {
		return 0, nil
	}
	if limit <= 0 {
		return 0, nil
	}
	if _, err := s.repo.ReleaseExpiredCleanupLeases(ctx, s.now()); err != nil {
		return 0, err
	}
	completed := 0
	var cleanupErr error
	for processed := 0; processed < limit; processed++ {
		claimTime := s.now()
		tasks, err := s.repo.LeaseCleanupTasks(
			ctx, owner, claimTime, claimTime.Add(leaseTTL), 1,
		)
		if err != nil {
			return completed, errors.Join(cleanupErr, err)
		}
		if len(tasks) == 0 {
			break
		}
		task := tasks[0]
		if task.AssetID > 0 {
			asset, assetErr := s.repo.FindAssetByID(ctx, task.AssetID)
			if assetErr != nil && !errors.Is(assetErr, domainmedia.ErrMediaAssetNotFound) {
				return completed, assetErr
			}
			if asset != nil && task.ObjectKey == asset.ObjectKey {
				variants, variantsErr := s.repo.ListReadyVariants(ctx, task.AssetID)
				if variantsErr != nil {
					return completed, variantsErr
				}
				additional := make([]*domainmedia.CleanupTask, 0, len(variants))
				for _, variant := range variants {
					if variant == nil || variant.ObjectKey == task.ObjectKey {
						continue
					}
					candidate, createErr := domainmedia.NewCleanupTask(
						task.AssetID, task.StorageBackend, variant.ObjectKey,
						claimTime, task.MaxAttempts,
					)
					if createErr != nil {
						return completed, createErr
					}
					additional = append(additional, candidate)
				}
				if err := s.repo.CreateCleanupTasks(ctx, additional); err != nil {
					return completed, err
				}
			}
		}
		start := time.Now()
		if err := s.repo.RenewCleanupTaskLease(ctx, task.ID, owner, leaseTTL); err != nil {
			return completed, err
		}
		deleteTimeout := leaseTTL / 2
		if deleteTimeout <= 0 || deleteTimeout > 30*time.Second {
			deleteTimeout = 30 * time.Second
		}
		deleteCtx, cancel := context.WithTimeout(ctx, deleteTimeout)
		err = s.store.Delete(deleteCtx, task.ObjectKey)
		cancel()
		inframetrics.ObserveMediaObject("delete", task.StorageBackend, time.Since(start), err)
		if err == nil {
			finishedAt := s.now()
			task.State = domainmedia.CleanupStateCompleted
			task.CompletedAt = &finishedAt
			completed++
		} else if task.Attempts >= task.MaxAttempts {
			task.State = domainmedia.CleanupStateFailed
			task.ErrorMessage = truncateProcessingError(err)
		} else {
			task.State = domainmedia.CleanupStatePending
			task.NotBefore = s.now().Add(processingRetryDelay(task.Attempts))
			task.ErrorMessage = truncateProcessingError(err)
			cleanupErr = errors.Join(cleanupErr, err)
		}
		if updateErr := s.repo.UpdateCleanupTaskOwned(ctx, task, owner); updateErr != nil {
			return completed, updateErr
		}
	}
	return completed, cleanupErr
}

type CleanupWorker struct {
	service      *CleanupService
	owner        string
	pollInterval time.Duration
	leaseTTL     time.Duration
	batchSize    int
}

func NewCleanupWorker(service *CleanupService, owner string) *CleanupWorker {
	if owner == "" {
		owner = "frux-cleanup"
	}
	return &CleanupWorker{
		service: service, owner: owner, pollInterval: 30 * time.Second,
		leaseTTL: 5 * time.Minute, batchSize: 50,
	}
}

func (w *CleanupWorker) Start(ctx context.Context) {
	if w == nil || w.service == nil {
		return
	}
	go func() {
		ticker := time.NewTicker(w.pollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				start := time.Now()
				_, err := w.service.RunCleanupOnce(ctx, w.owner, w.batchSize, w.leaseTTL)
				inframetrics.ObserveWorkerJob("media_cleanup", time.Since(start), err)
			}
		}
	}()
}
