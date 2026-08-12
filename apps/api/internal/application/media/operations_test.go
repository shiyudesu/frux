package applicationmedia

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
)

func TestCleanupServiceSchedulesAndDeletesMediaObjects(t *testing.T) {
	now := time.Date(2026, 7, 26, 8, 0, 0, 0, time.UTC)
	repo := &operationsRepositoryStub{
		assets: map[int64]*domainmedia.MediaAsset{
			1: {ID: 1, StorageBackend: domainmedia.StorageBackendS3, ObjectKey: "uploads/1/source.mp4", State: domainmedia.AssetStateReady},
		},
		variants: map[int64][]*domainmedia.MediaVariant{
			1: {{AssetID: 1, ObjectKey: "media/1/v1/base.mp4", State: domainmedia.VariantStateReady}},
		},
	}
	store := &operationsStore{objects: map[string]domainmedia.ObjectMetadata{
		"uploads/1/source.mp4": {Key: "uploads/1/source.mp4"},
		"media/1/v1/base.mp4":  {Key: "media/1/v1/base.mp4"},
	}}
	service := NewCleanupService(repo, store, domainmedia.StorageBackendS3, time.Minute, 3)
	service.now = func() time.Time { return now }
	if err := service.ScheduleMediaCleanup(context.Background(), 1, 0); err != nil {
		t.Fatalf("schedule cleanup: %v", err)
	}
	if repo.assets[1].State != domainmedia.AssetStateDeleted || len(repo.cleanupTasks) != 2 {
		t.Fatalf("unexpected scheduled cleanup: asset=%+v tasks=%+v", repo.assets[1], repo.cleanupTasks)
	}
	now = now.Add(2 * time.Minute)
	completed, err := service.RunCleanupOnce(context.Background(), "worker", 10, time.Minute)
	if err != nil {
		t.Fatalf("run cleanup: %v", err)
	}
	if completed != 2 || len(store.objects) != 0 {
		t.Fatalf("unexpected cleanup result: completed=%d objects=%+v", completed, store.objects)
	}
}

func TestCleanupServiceSchedulesModerationSamplesIdempotently(t *testing.T) {
	repo := &operationsRepositoryStub{}
	service := NewCleanupService(
		repo, nil, domainmedia.StorageBackendS3, time.Minute, 3,
	)
	notBefore := time.Now().UTC().Add(time.Hour)
	keys := []string{"moderation/1/a.jpg", "moderation/1/a.jpg", "moderation/1/b.jpg"}
	if err := service.ScheduleModerationSampleCleanup(
		context.Background(), keys, notBefore,
	); err != nil {
		t.Fatal(err)
	}
	if err := service.ScheduleModerationSampleCleanup(
		context.Background(), keys, notBefore,
	); err != nil {
		t.Fatal(err)
	}
	if len(repo.cleanupTasks) != 2 {
		t.Fatalf("cleanup tasks = %#v", repo.cleanupTasks)
	}
	for _, task := range repo.cleanupTasks {
		if task.AssetID != 0 || !task.NotBefore.Equal(notBefore) {
			t.Fatalf("moderation cleanup task = %#v", task)
		}
	}
}

func TestReconcilerResetsIncompleteVariantsAndQueuesOrphans(t *testing.T) {
	now := time.Date(2026, 7, 26, 8, 0, 0, 0, time.UTC)
	repo := &operationsRepositoryStub{
		assets: map[int64]*domainmedia.MediaAsset{
			2: {ID: 2, StorageBackend: domainmedia.StorageBackendS3, ObjectKey: "uploads/2/source.mp4", State: domainmedia.AssetStateReady},
		},
		variants: map[int64][]*domainmedia.MediaVariant{
			2: {{AssetID: 2, ObjectKey: "media/2/v1/missing.mp4", Role: domainmedia.VariantRoleBaseline, State: domainmedia.VariantStateReady}},
		},
		job: &domainmedia.MediaProcessingJob{ID: 3, AssetID: 2, ProfileVersion: "v1", State: domainmedia.JobStateCompleted, MaxAttempts: 5},
		knownKeys: map[string]struct{}{
			"uploads/2/source.mp4":   {},
			"media/2/v1/missing.mp4": {},
		},
	}
	store := &operationsStore{objects: map[string]domainmedia.ObjectMetadata{
		"uploads/2/source.mp4": {Key: "uploads/2/source.mp4", LastModified: now.Add(-time.Hour)},
		"media/orphan.mp4":     {Key: "media/orphan.mp4", LastModified: now.Add(-time.Hour)},
	}}
	notifier := &mediaStateNotifierStub{}
	reconciler := NewReconciler(repo, store, notifier, domainmedia.StorageBackendS3, "v1", 5, time.Minute)
	reconciler.now = func() time.Time { return now }
	if err := reconciler.RunOnce(context.Background(), 10); err != nil {
		t.Fatalf("run reconciliation: %v", err)
	}
	if repo.assets[2].State != domainmedia.AssetStateUploaded || repo.job.State != domainmedia.JobStateRetryable {
		t.Fatalf("expected incomplete asset retry: asset=%+v job=%+v", repo.assets[2], repo.job)
	}
	if notifier.failed != 0 || notifier.repairing != 1 {
		t.Fatalf(
			"expected retry projection notification, failed=%d repairing=%d",
			notifier.failed, notifier.repairing,
		)
	}
	foundOrphan := false
	for _, task := range repo.cleanupTasks {
		if task.ObjectKey == "media/orphan.mp4" && task.AssetID == 0 {
			foundOrphan = true
		}
	}
	if !foundOrphan {
		t.Fatalf("expected orphan cleanup task: %+v", repo.cleanupTasks)
	}
}

func TestReconcilerCanDisableOrphanObjectCleanup(t *testing.T) {
	now := time.Date(2026, 7, 26, 8, 0, 0, 0, time.UTC)
	repo := &operationsRepositoryStub{}
	store := &operationsStore{objects: map[string]domainmedia.ObjectMetadata{
		"media/orphan.mp4": {
			Key: "media/orphan.mp4", LastModified: now.Add(-time.Hour),
		},
	}}
	reconciler := NewReconciler(
		repo, store, nil, domainmedia.StorageBackendS3, "v1", 5, time.Minute,
		WithoutOrphanObjectCleanup(),
	)
	reconciler.now = func() time.Time { return now }
	if err := reconciler.RunOnce(context.Background(), 10); err != nil {
		t.Fatal(err)
	}
	if len(repo.cleanupTasks) != 0 {
		t.Fatalf("unexpected orphan cleanup tasks: %+v", repo.cleanupTasks)
	}
}

type operationsRepositoryStub struct {
	assets       map[int64]*domainmedia.MediaAsset
	variants     map[int64][]*domainmedia.MediaVariant
	job          *domainmedia.MediaProcessingJob
	cleanupTasks []*domainmedia.CleanupTask
	knownKeys    map[string]struct{}
}

func (r *operationsRepositoryStub) FindAssetByID(_ context.Context, assetID int64) (*domainmedia.MediaAsset, error) {
	asset := r.assets[assetID]
	if asset == nil {
		return nil, domainmedia.ErrMediaAssetNotFound
	}
	return asset, nil
}

func (r *operationsRepositoryStub) UpdateAsset(_ context.Context, asset *domainmedia.MediaAsset) error {
	r.assets[asset.ID] = asset
	return nil
}

func (r *operationsRepositoryStub) ListReadyVariants(_ context.Context, assetID int64) ([]*domainmedia.MediaVariant, error) {
	return r.variants[assetID], nil
}

func (r *operationsRepositoryStub) CreateCleanupTasks(_ context.Context, tasks []*domainmedia.CleanupTask) error {
	for _, task := range tasks {
		exists := false
		for _, current := range r.cleanupTasks {
			if current.ObjectKey == task.ObjectKey {
				exists = true
			}
		}
		if !exists {
			copy := *task
			copy.ID = int64(len(r.cleanupTasks) + 1)
			r.cleanupTasks = append(r.cleanupTasks, &copy)
		}
	}
	return nil
}

func (r *operationsRepositoryStub) ScheduleAssetCleanup(
	ctx context.Context,
	assetID int64,
	notBefore time.Time,
	maxAttempts int,
) error {
	asset, err := r.FindAssetByID(ctx, assetID)
	if err != nil {
		return err
	}
	tasks := make([]*domainmedia.CleanupTask, 0, len(r.variants[assetID])+1)
	original, err := domainmedia.NewCleanupTask(
		assetID, asset.StorageBackend, asset.ObjectKey, notBefore, maxAttempts,
	)
	if err != nil {
		return err
	}
	tasks = append(tasks, original)
	for _, variant := range r.variants[assetID] {
		task, err := domainmedia.NewCleanupTask(
			assetID, asset.StorageBackend, variant.ObjectKey, notBefore, maxAttempts,
		)
		if err != nil {
			return err
		}
		tasks = append(tasks, task)
	}
	if err := r.CreateCleanupTasks(ctx, tasks); err != nil {
		return err
	}
	asset.State = domainmedia.AssetStateDeleted
	return r.UpdateAsset(ctx, asset)
}

func (r *operationsRepositoryStub) LeaseCleanupTasks(_ context.Context, owner string, now time.Time, leaseUntil time.Time, limit int) ([]*domainmedia.CleanupTask, error) {
	result := []*domainmedia.CleanupTask{}
	for _, task := range r.cleanupTasks {
		if len(result) >= limit || task.State != domainmedia.CleanupStatePending || task.NotBefore.After(now) {
			continue
		}
		task.State = domainmedia.CleanupStateProcessing
		task.Attempts++
		task.LeaseOwner = owner
		task.LeaseUntil = &leaseUntil
		result = append(result, task)
	}
	return result, nil
}

func (*operationsRepositoryStub) UpdateCleanupTask(context.Context, *domainmedia.CleanupTask) error {
	return nil
}

func (*operationsRepositoryStub) UpdateCleanupTaskOwned(
	context.Context, *domainmedia.CleanupTask, string,
) error {
	return nil
}

func (*operationsRepositoryStub) RenewCleanupTaskLease(
	context.Context, int64, string, time.Duration,
) error {
	return nil
}

func (*operationsRepositoryStub) ReleaseExpiredCleanupLeases(context.Context, time.Time) (int64, error) {
	return 0, nil
}

func (*operationsRepositoryStub) ReleaseExpiredProcessingLeases(context.Context, time.Time) (int64, error) {
	return 0, nil
}

func (r *operationsRepositoryStub) ListAssetsForReconciliation(context.Context, int) ([]*domainmedia.MediaAsset, error) {
	result := make([]*domainmedia.MediaAsset, 0, len(r.assets))
	for _, asset := range r.assets {
		result = append(result, asset)
	}
	return result, nil
}

func (r *operationsRepositoryStub) FindProcessingJobByAsset(context.Context, int64) (*domainmedia.MediaProcessingJob, error) {
	if r.job == nil {
		return nil, domainmedia.ErrProcessingJobNotFound
	}
	return r.job, nil
}

func (r *operationsRepositoryStub) CreateOrGetProcessingJob(_ context.Context, job *domainmedia.MediaProcessingJob) (*domainmedia.MediaProcessingJob, bool, error) {
	if r.job != nil {
		return r.job, false, nil
	}
	r.job = job
	return job, true, nil
}

func (r *operationsRepositoryStub) ResetProcessingJob(_ context.Context, _ int64, _ string, now time.Time) error {
	if r.job == nil {
		return domainmedia.ErrProcessingJobNotFound
	}
	r.job.State = domainmedia.JobStateRetryable
	r.job.NextAttemptAt = now
	r.job.CompletedAt = nil
	return nil
}

func (r *operationsRepositoryStub) ListKnownObjectKeys(context.Context, string) (map[string]struct{}, error) {
	return r.knownKeys, nil
}

func (*operationsRepositoryStub) MarkAssetReconciled(context.Context, int64, time.Time) error {
	return nil
}

func (*operationsRepositoryStub) ExpireUploadSessions(context.Context, time.Time, int) ([]*domainmedia.UploadSession, error) {
	return nil, nil
}

type operationsStore struct {
	objects map[string]domainmedia.ObjectMetadata
}

func (*operationsStore) Put(context.Context, string, io.Reader, int64, string, string) (*domainmedia.ObjectMetadata, error) {
	return nil, errors.New("not implemented")
}

func (*operationsStore) Open(context.Context, string) (io.ReadCloser, *domainmedia.ObjectMetadata, error) {
	return nil, nil, errors.New("not implemented")
}

func (s *operationsStore) Head(_ context.Context, key string) (*domainmedia.ObjectMetadata, error) {
	value, ok := s.objects[key]
	if !ok {
		return nil, domainmedia.ErrObjectNotFound
	}
	copy := value
	return &copy, nil
}

func (s *operationsStore) Delete(_ context.Context, key string) error {
	delete(s.objects, key)
	return nil
}

func (s *operationsStore) List(_ context.Context, prefix string) ([]domainmedia.ObjectMetadata, error) {
	result := []domainmedia.ObjectMetadata{}
	for key, metadata := range s.objects {
		if len(key) >= len(prefix) && key[:len(prefix)] == prefix {
			result = append(result, metadata)
		}
	}
	return result, nil
}

func (*operationsStore) PresignPut(context.Context, string, string, string, int64, time.Duration) (*domainmedia.PresignedRequest, error) {
	return nil, errors.New("not implemented")
}

func (*operationsStore) PresignGet(context.Context, string, time.Duration) (*domainmedia.PresignedRequest, error) {
	return nil, errors.New("not implemented")
}

type mediaStateNotifierStub struct {
	failed    int
	repairing int
}

func (*mediaStateNotifierStub) MediaReady(context.Context, int64) error {
	return nil
}

func (s *mediaStateNotifierStub) MediaRepairing(context.Context, int64, string) error {
	s.repairing++
	return nil
}

func (s *mediaStateNotifierStub) MediaFailed(context.Context, int64, string, string) error {
	s.failed++
	return nil
}
