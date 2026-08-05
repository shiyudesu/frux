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
	CreateCleanupTasks(ctx context.Context, tasks []*domainmedia.CleanupTask) error
	LeaseCleanupTasks(ctx context.Context, owner string, now time.Time, leaseUntil time.Time, limit int) ([]*domainmedia.CleanupTask, error)
	UpdateCleanupTask(ctx context.Context, task *domainmedia.CleanupTask) error
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
		asset, err := s.repo.FindAssetByID(ctx, assetID)
		if errors.Is(err, domainmedia.ErrMediaAssetNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		variants, err := s.repo.ListReadyVariants(ctx, assetID)
		if err != nil {
			return err
		}
		notBefore := s.now().Add(s.delay)
		tasks := make([]*domainmedia.CleanupTask, 0, len(variants)+1)
		original, err := domainmedia.NewCleanupTask(asset.ID, asset.StorageBackend, asset.ObjectKey, notBefore, s.maxAttempts)
		if err != nil {
			return err
		}
		tasks = append(tasks, original)
		for _, variant := range variants {
			if variant == nil {
				continue
			}
			task, err := domainmedia.NewCleanupTask(asset.ID, asset.StorageBackend, variant.ObjectKey, notBefore, s.maxAttempts)
			if err != nil {
				return err
			}
			tasks = append(tasks, task)
		}
		if err := s.repo.CreateCleanupTasks(ctx, tasks); err != nil {
			return err
		}
		asset.State = domainmedia.AssetStateDeleted
		if err := s.repo.UpdateAsset(ctx, asset); err != nil {
			return err
		}
	}
	return nil
}

func (s *CleanupService) RunCleanupOnce(ctx context.Context, owner string, limit int, leaseTTL time.Duration) (int, error) {
	if s == nil || s.repo == nil || s.store == nil {
		return 0, nil
	}
	now := s.now()
	if _, err := s.repo.ReleaseExpiredCleanupLeases(ctx, now); err != nil {
		return 0, err
	}
	tasks, err := s.repo.LeaseCleanupTasks(ctx, owner, now, now.Add(leaseTTL), limit)
	if err != nil {
		return 0, err
	}
	completed := 0
	var cleanupErr error
	for _, task := range tasks {
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
					candidate, createErr := domainmedia.NewCleanupTask(task.AssetID, task.StorageBackend, variant.ObjectKey, now, task.MaxAttempts)
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
		err := s.store.Delete(ctx, task.ObjectKey)
		inframetrics.ObserveMediaObject("delete", task.StorageBackend, time.Since(start), err)
		task.LeaseOwner = ""
		task.LeaseUntil = nil
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
		if updateErr := s.repo.UpdateCleanupTask(ctx, task); updateErr != nil {
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
