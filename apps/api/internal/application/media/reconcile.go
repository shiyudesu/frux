package applicationmedia

import (
	"context"
	"errors"
	"time"

	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
	inframetrics "github.com/shiyudesu/frux/internal/infra/metrics"
)

type ReconciliationRepository interface {
	ReleaseExpiredProcessingLeases(ctx context.Context, now time.Time) (int64, error)
	ListAssetsForReconciliation(ctx context.Context, limit int) ([]*domainmedia.MediaAsset, error)
	FindProcessingJobByAsset(ctx context.Context, assetID int64) (*domainmedia.MediaProcessingJob, error)
	CreateOrGetProcessingJob(ctx context.Context, job *domainmedia.MediaProcessingJob) (*domainmedia.MediaProcessingJob, bool, error)
	ResetProcessingJob(ctx context.Context, assetID int64, profileVersion string, now time.Time) error
	ListReadyVariants(ctx context.Context, assetID int64) ([]*domainmedia.MediaVariant, error)
	UpdateAsset(ctx context.Context, asset *domainmedia.MediaAsset) error
	ListKnownObjectKeys(ctx context.Context, prefix string) (map[string]struct{}, error)
	CreateCleanupTasks(ctx context.Context, tasks []*domainmedia.CleanupTask) error
	MarkAssetReconciled(ctx context.Context, assetID int64, reconciledAt time.Time) error
	ExpireUploadSessions(ctx context.Context, now time.Time, limit int) ([]*domainmedia.UploadSession, error)
}

type Reconciler struct {
	repo           ReconciliationRepository
	store          domainmedia.MediaObjectStore
	notifier       MediaStateNotifier
	backend        string
	profileVersion string
	maxAttempts    int
	orphanDelay    time.Duration
	orphanCleanup  bool
	now            func() time.Time
}

type ReconcilerOption func(*Reconciler)

func WithoutOrphanObjectCleanup() ReconcilerOption {
	return func(reconciler *Reconciler) {
		reconciler.orphanCleanup = false
	}
}

func NewReconciler(repo ReconciliationRepository, store domainmedia.MediaObjectStore, notifier MediaStateNotifier, backend, profileVersion string, maxAttempts int, orphanDelay time.Duration, options ...ReconcilerOption) *Reconciler {
	reconciler := &Reconciler{
		repo: repo, store: store, notifier: notifier, backend: backend,
		profileVersion: profileVersion, maxAttempts: maxAttempts, orphanDelay: orphanDelay,
		orphanCleanup: true,
		now:           func() time.Time { return time.Now().UTC() },
	}
	for _, option := range options {
		if option != nil {
			option(reconciler)
		}
	}
	return reconciler
}

func (r *Reconciler) RunOnce(ctx context.Context, limit int) error {
	if r == nil || r.repo == nil || r.store == nil {
		return nil
	}
	now := r.now()
	expired, err := r.repo.ReleaseExpiredProcessingLeases(ctx, now)
	inframetrics.ObserveMediaReconciliation("expired_lease", int(expired), err)
	if err != nil {
		return err
	}
	expiredSessions, err := r.repo.ExpireUploadSessions(ctx, now, limit)
	if err != nil {
		return err
	}
	for _, session := range expiredSessions {
		if session != nil {
			if err := r.store.Delete(ctx, session.ObjectKey); err != nil {
				task, createErr := domainmedia.NewCleanupTask(0, session.StorageBackend, session.ObjectKey, now, r.maxAttempts)
				if createErr != nil {
					return createErr
				}
				if createErr := r.repo.CreateCleanupTasks(ctx, []*domainmedia.CleanupTask{task}); createErr != nil {
					return createErr
				}
			}
		}
	}
	assets, err := r.repo.ListAssetsForReconciliation(ctx, limit)
	if err != nil {
		return err
	}
	var reconcileErr error
	for _, asset := range assets {
		if err := r.reconcileAsset(ctx, asset); err != nil {
			reconcileErr = errors.Join(reconcileErr, err)
		}
		if err := r.repo.MarkAssetReconciled(ctx, asset.ID, now); err != nil {
			reconcileErr = errors.Join(reconcileErr, err)
		}
	}
	if r.orphanCleanup {
		if err := r.reconcileOrphanObjects(ctx, "media", now); err != nil {
			reconcileErr = errors.Join(reconcileErr, err)
		}
		if err := r.reconcileOrphanObjects(ctx, "processed", now); err != nil {
			reconcileErr = errors.Join(reconcileErr, err)
		}
		if err := r.reconcileOrphanObjects(ctx, "uploads", now); err != nil {
			reconcileErr = errors.Join(reconcileErr, err)
		}
	}
	return reconcileErr
}

func (r *Reconciler) reconcileAsset(ctx context.Context, asset *domainmedia.MediaAsset) error {
	if asset == nil {
		return nil
	}
	if asset.State == domainmedia.AssetStateFailed {
		if r.notifier != nil {
			return r.notifier.MediaFailed(
				ctx, asset.ID, "reconcile", asset.ErrorCode,
			)
		}
		return nil
	}
	if _, err := r.store.Head(ctx, asset.ObjectKey); err != nil {
		if errors.Is(err, domainmedia.ErrObjectNotFound) {
			asset.State = domainmedia.AssetStateFailed
			asset.ErrorCode = "source_missing"
			if updateErr := r.repo.UpdateAsset(ctx, asset); updateErr != nil {
				return updateErr
			}
			if r.notifier != nil {
				return r.notifier.MediaFailed(ctx, asset.ID, "reconcile", asset.ErrorCode)
			}
			return nil
		}
		return err
	}
	if asset.Kind == domainmedia.AssetKindCover {
		variants, err := r.repo.ListReadyVariants(ctx, asset.ID)
		if err != nil {
			return err
		}
		for _, variant := range variants {
			if variant == nil || variant.Role != domainmedia.VariantRoleCover {
				continue
			}
			if _, err := r.store.Head(ctx, variant.ObjectKey); err == nil {
				return nil
			}
		}
		asset.State = domainmedia.AssetStateFailed
		asset.ErrorCode = "cover_output_missing"
		return r.repo.UpdateAsset(ctx, asset)
	}
	if asset.State != domainmedia.AssetStateReady {
		if _, err := r.repo.FindProcessingJobByAsset(ctx, asset.ID); errors.Is(err, domainmedia.ErrProcessingJobNotFound) {
			job, createErr := domainmedia.NewProcessingJob(asset.ID, r.profileVersion, r.maxAttempts, r.now())
			if createErr != nil {
				return createErr
			}
			_, _, createErr = r.repo.CreateOrGetProcessingJob(ctx, job)
			return createErr
		} else {
			return err
		}
	}
	variants, err := r.repo.ListReadyVariants(ctx, asset.ID)
	if err != nil {
		return err
	}
	baselineReady := false
	for _, variant := range variants {
		if variant == nil {
			continue
		}
		if variant.Role == domainmedia.VariantRoleBaseline {
			baselineReady = true
		}
		if _, err := r.store.Head(ctx, variant.ObjectKey); err != nil {
			baselineReady = false
			break
		}
	}
	if baselineReady {
		if r.notifier != nil {
			return r.notifier.MediaReady(ctx, asset.ID)
		}
		return nil
	}
	asset.State = domainmedia.AssetStateUploaded
	asset.ErrorCode = "variant_missing"
	if err := r.repo.UpdateAsset(ctx, asset); err != nil {
		return err
	}
	job, err := r.repo.FindProcessingJobByAsset(ctx, asset.ID)
	if errors.Is(err, domainmedia.ErrProcessingJobNotFound) {
		newJob, createErr := domainmedia.NewProcessingJob(asset.ID, r.profileVersion, r.maxAttempts, r.now())
		if createErr != nil {
			return createErr
		}
		_, _, err = r.repo.CreateOrGetProcessingJob(ctx, newJob)
	} else if err == nil {
		err = r.repo.ResetProcessingJob(ctx, asset.ID, job.ProfileVersion, r.now())
	}
	if err != nil {
		return err
	}
	if r.notifier != nil {
		return r.notifier.MediaRepairing(ctx, asset.ID, asset.ErrorCode)
	}
	return nil
}

func (r *Reconciler) reconcileOrphanObjects(ctx context.Context, prefix string, now time.Time) error {
	objects, err := r.store.List(ctx, prefix)
	if err != nil {
		return err
	}
	known, err := r.repo.ListKnownObjectKeys(ctx, prefix)
	if err != nil {
		return err
	}
	tasks := []*domainmedia.CleanupTask{}
	for _, object := range objects {
		if _, exists := known[object.Key]; exists || object.LastModified.Add(r.orphanDelay).After(now) {
			continue
		}
		task, err := domainmedia.NewCleanupTask(0, r.backend, object.Key, now, r.maxAttempts)
		if err != nil {
			return err
		}
		tasks = append(tasks, task)
	}
	inframetrics.ObserveMediaReconciliation("orphan_object", len(tasks), nil)
	return r.repo.CreateCleanupTasks(ctx, tasks)
}

type ReconciliationWorker struct {
	reconciler   *Reconciler
	pollInterval time.Duration
	batchSize    int
}

func NewReconciliationWorker(reconciler *Reconciler) *ReconciliationWorker {
	return &ReconciliationWorker{reconciler: reconciler, pollInterval: time.Minute, batchSize: 100}
}

func (w *ReconciliationWorker) Start(ctx context.Context) {
	if w == nil || w.reconciler == nil {
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
				err := w.reconciler.RunOnce(ctx, w.batchSize)
				inframetrics.ObserveWorkerJob("media_reconciliation", time.Since(start), err)
			}
		}
	}()
}
