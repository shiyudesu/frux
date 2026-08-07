package test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	applicationmedia "github.com/shiyudesu/frux/internal/application/media"
	applicationvideo "github.com/shiyudesu/frux/internal/application/video"
	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
	domainvideo "github.com/shiyudesu/frux/internal/domain/video"
	inframedia "github.com/shiyudesu/frux/internal/infra/media"
)

func TestProductionMediaDeliveryEndToEnd(t *testing.T) {
	ctx := context.Background()
	repo := newE2EMediaRepo()
	store := &e2eObjectStore{objects: map[string]domainmedia.ObjectMetadata{}}
	resolver, err := inframedia.NewURLResolver("https://cdn.example.test", store)
	if err != nil {
		t.Fatalf("create delivery resolver: %v", err)
	}

	catalog := inframedia.NewDeliveryCatalog(repo, resolver, store)
	ids := []string{"video-session", "cover-session"}
	idIndex := 0
	uploadService := applicationmedia.New(
		repo, store, domainmedia.StorageBackendS3, 15*time.Minute, "v1", 5,
		applicationmedia.WithIDGenerator(func() (string, error) {
			id := ids[idIndex]
			idIndex++
			return id, nil
		}),
	)
	checksum := strings.Repeat("a", 64)
	videoSession, err := uploadService.CreateUploadSession(ctx, applicationmedia.CreateUploadSessionInput{
		OwnerID: 42, Kind: domainmedia.AssetKindVideo, Filename: "clip.mp4",
		ContentType: "video/mp4", SizeBytes: 128, ChecksumSHA256: checksum, IdempotencyKey: "video-upload",
	})
	if err != nil {
		t.Fatalf("create video upload session: %v", err)
	}
	coverSession, err := uploadService.CreateUploadSession(ctx, applicationmedia.CreateUploadSessionInput{
		OwnerID: 42, Kind: domainmedia.AssetKindCover, Filename: "cover.jpg",
		ContentType: "image/jpeg", SizeBytes: 64, ChecksumSHA256: checksum, IdempotencyKey: "cover-upload",
	})
	if err != nil {
		t.Fatalf("create cover upload session: %v", err)
	}
	store.objects[videoSession.Session.ObjectKey] = domainmedia.ObjectMetadata{
		Key: videoSession.Session.ObjectKey, ContentType: "video/mp4", SizeBytes: 128, ChecksumSHA256: checksum,
	}
	store.objects[coverSession.Session.ObjectKey] = domainmedia.ObjectMetadata{
		Key: coverSession.Session.ObjectKey, ContentType: "image/jpeg", SizeBytes: 64, ChecksumSHA256: checksum,
	}
	videoAsset, err := uploadService.CompleteUploadSession(ctx, 42, videoSession.Session.ID)
	if err != nil {
		t.Fatalf("complete video upload: %v", err)
	}
	coverAsset, err := uploadService.CompleteUploadSession(ctx, 42, coverSession.Session.ID)
	if err != nil {
		t.Fatalf("complete cover upload: %v", err)
	}

	videoRepo := newMemoryVideoRepo()
	publisher := &memoryVideoPublisher{}
	cache := &e2eCacheInvalidator{}
	cleanup := applicationmedia.NewCleanupService(repo, store, domainmedia.StorageBackendS3, time.Millisecond, 3)
	videoService := applicationvideo.New(
		videoRepo,
		applicationvideo.WithMediaAssets(repo),
		applicationvideo.WithMediaDelivery(catalog),
		applicationvideo.WithPublishedEventPublisher(publisher),
		applicationvideo.WithVideoCacheInvalidator(cache),
		applicationvideo.WithMediaCleanup(cleanup),
	)
	created, err := videoService.CreateWithAssets(
		ctx, 42, "production media", "", videoAsset.Asset.ID, coverAsset.Asset.ID, "create-production-media",
	)
	if err != nil {
		t.Fatalf("create processing video: %v", err)
	}
	if created.Video.MediaStatus != domainmedia.MediaStatusProcessing || publisher.EventCount() != 0 {
		t.Fatalf("video became public before baseline: %+v events=%d", created.Video, publisher.EventCount())
	}

	processor := &e2eProcessor{store: store}
	publication := applicationvideo.NewMediaPublicationService(videoRepo, catalog, publisher, cache)
	worker := applicationmedia.NewMediaProcessingWorker(
		repo, processor, nil, time.Minute, 1,
		applicationmedia.WithMediaStateNotifier(publication),
	)
	if err := worker.HandleRequested(ctx, applicationmedia.NewProcessingRequestedEvent(videoAsset.Asset.ID, "v1", time.Now().UTC())); err != nil {
		t.Fatalf("process media: %v", err)
	}
	ready, err := videoRepo.FindByIDAnyStatus(ctx, created.Video.ID)
	if err != nil {
		t.Fatalf("load ready video: %v", err)
	}
	if ready.Status != domainvideo.StatusPendingReview ||
		ready.MediaStatus != domainmedia.MediaStatusReady ||
		ready.MediaURL != "" || len(ready.PlaybackSources) != 0 ||
		publisher.EventCount() != 0 {
		t.Fatalf("media readiness bypassed review: %+v events=%d", ready, publisher.EventCount())
	}
	if err := videoService.Approve(ctx, ready.ID, time.Now().UTC()); err != nil {
		t.Fatalf("approve ready video: %v", err)
	}
	ready, err = videoRepo.FindByID(ctx, created.Video.ID)
	if err != nil {
		t.Fatalf("load approved video: %v", err)
	}
	if !strings.HasPrefix(ready.MediaURL, "https://cdn.example.test/media/v2/") ||
		!strings.HasSuffix(ready.MediaURL, "/"+protectedMediaVariantSuffix(videoAsset.Asset.ID)) ||
		len(ready.PlaybackSources) != 1 || publisher.EventCount() != 1 {
		t.Fatalf("approval did not release ready media: %+v events=%d", ready, publisher.EventCount())
	}
	if err := videoService.SetOffline(ctx, ready.ID); err != nil {
		t.Fatalf("take production video offline: %v", err)
	}
	publicKey := strings.TrimPrefix(ready.MediaURL, "https://cdn.example.test/")
	if _, err := store.Head(ctx, publicKey); err == nil {
		t.Fatal("offline video retained public object")
	}
	protectedKey := "processed/" + protectedMediaVariantSuffix(videoAsset.Asset.ID)
	if _, err := store.Head(ctx, protectedKey); err != nil {
		t.Fatalf("offline video missing protected object: %v", err)
	}
	if _, err := videoRepo.FindByID(ctx, ready.ID); err != domainvideo.ErrVideoNotFound {
		t.Fatalf("offline video remained public: %v", err)
	}
	if err := videoService.RestorePublished(ctx, ready.ID); err != nil {
		t.Fatalf("restore production video: %v", err)
	}
	ready, err = videoRepo.FindByID(ctx, ready.ID)
	if err != nil || ready.MediaURL == "" {
		t.Fatalf("restored video did not republish media: video=%+v err=%v", ready, err)
	}

	if err := videoService.Delete(ctx, 42, ready.ID); err != nil {
		t.Fatalf("delete production video: %v", err)
	}
	time.Sleep(2 * time.Millisecond)
	if _, err := cleanup.RunCleanupOnce(ctx, "cleanup-worker", 20, time.Minute); err != nil {
		t.Fatalf("cleanup deleted media: %v", err)
	}
	if len(store.objects) != 0 {
		t.Fatalf("expected all media objects deleted, got %+v", store.objects)
	}
}

func TestCreateWithAssetsRejectsAlreadyFailedVideoAsset(t *testing.T) {
	repo := newE2EMediaRepo()
	repo.assets[1] = &domainmedia.MediaAsset{
		ID: 1, OwnerID: 7, Kind: domainmedia.AssetKindVideo,
		State: domainmedia.AssetStateFailed,
	}
	repo.assets[2] = &domainmedia.MediaAsset{
		ID: 2, OwnerID: 7, Kind: domainmedia.AssetKindCover,
		State: domainmedia.AssetStateReady,
	}
	service := applicationvideo.New(
		&memoryVideoRepo{byID: map[int64]*domainvideo.Video{}},
		applicationvideo.WithMediaAssets(repo),
	)
	if _, err := service.CreateWithAssets(
		context.Background(), 7, "failed", "", 1, 2, "failed-asset",
	); !errors.Is(err, domainvideo.ErrVideoStateNotAllowed) {
		t.Fatalf("failed asset creation error = %v", err)
	}
}

func (r *memoryVideoRepo) ListByMediaAssetID(_ context.Context, mediaAssetID int64) ([]*domainvideo.Video, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := []*domainvideo.Video{}
	for _, video := range r.byID {
		if video.MediaAssetID == mediaAssetID && video.Status != domainvideo.StatusDeleted {
			result = append(result, cloneVideo(video))
		}
	}
	return result, nil
}

func (r *memoryVideoRepo) UpdateMediaProjection(_ context.Context, video *domainvideo.Video) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	stored := r.byID[video.ID]
	if stored == nil {
		return false, domainvideo.ErrVideoNotFound
	}

	eligible := stored.Status == domainvideo.StatusPublished &&
		stored.Visibility == domainvideo.VisibilityPublic &&
		domainmedia.IsPublicReadyStatus(video.MediaStatus)
	if !eligible {
		video.MediaURL = ""
		video.CoverURL = ""
		video.PlaybackSources = nil
	}
	stored.MediaURL = video.MediaURL
	stored.CoverURL = video.CoverURL
	stored.MediaStatus = video.MediaStatus
	stored.MediaErrorCode = video.MediaErrorCode
	stored.PlaybackSources = append([]domainmedia.PlaybackSource(nil), video.PlaybackSources...)
	return eligible, nil
}

func (*memoryVideoRepo) MarkLifecyclePublicationReady(
	context.Context, string, time.Time,
) error {
	return nil
}

func (*memoryVideoRepo) LifecyclePublicationTracked(context.Context, string) (bool, error) {
	return true, nil
}

func (*memoryVideoRepo) LifecyclePublicationReady(context.Context, string) (bool, error) {
	return false, nil
}

type e2eMediaRepo struct {
	nextID       int64
	sessions     map[string]*domainmedia.UploadSession
	assets       map[int64]*domainmedia.MediaAsset
	variants     map[int64][]*domainmedia.MediaVariant
	job          *domainmedia.MediaProcessingJob
	cleanupTasks []*domainmedia.CleanupTask
}

func newE2EMediaRepo() *e2eMediaRepo {
	return &e2eMediaRepo{
		nextID: 100, sessions: map[string]*domainmedia.UploadSession{},
		assets: map[int64]*domainmedia.MediaAsset{}, variants: map[int64][]*domainmedia.MediaVariant{},
	}
}

func (r *e2eMediaRepo) CreateUploadSession(_ context.Context, session *domainmedia.UploadSession) (*domainmedia.UploadSession, bool, error) {
	copy := *session
	r.sessions[copy.ID] = &copy
	return &copy, true, nil
}

func (r *e2eMediaRepo) FindUploadSession(_ context.Context, sessionID string) (*domainmedia.UploadSession, error) {
	session := r.sessions[sessionID]
	if session == nil {
		return nil, domainmedia.ErrUploadSessionNotFound
	}
	copy := *session
	return &copy, nil
}

func (r *e2eMediaRepo) CompleteUploadSession(_ context.Context, sessionID string, asset *domainmedia.MediaAsset, completedAt time.Time) (*domainmedia.UploadSession, *domainmedia.MediaAsset, bool, error) {
	session := r.sessions[sessionID]
	if session.CompletedAssetID > 0 {
		return session, r.assets[session.CompletedAssetID], true, nil
	}
	r.nextID++
	assetCopy := *asset
	assetCopy.ID = r.nextID
	r.assets[assetCopy.ID] = &assetCopy
	session.State = domainmedia.UploadSessionStateCompleted
	session.CompletedAssetID = assetCopy.ID
	session.CompletedAt = &completedAt
	return session, &assetCopy, false, nil
}

func (r *e2eMediaRepo) CreateOrGetProcessingJob(_ context.Context, job *domainmedia.MediaProcessingJob) (*domainmedia.MediaProcessingJob, bool, error) {
	if r.job != nil {
		return r.job, false, nil
	}
	copy := *job
	copy.ID = 1
	r.job = &copy
	return r.job, true, nil
}

func (r *e2eMediaRepo) FindAssetByID(_ context.Context, assetID int64) (*domainmedia.MediaAsset, error) {
	asset := r.assets[assetID]
	if asset == nil {
		return nil, domainmedia.ErrMediaAssetNotFound
	}
	return asset, nil
}

func (r *e2eMediaRepo) RenewExpiredUploadSession(_ context.Context, expiredSessionID string, replacement *domainmedia.UploadSession) (*domainmedia.UploadSession, error) {
	delete(r.sessions, expiredSessionID)
	copy := *replacement
	r.sessions[copy.ID] = &copy
	return &copy, nil
}

func (r *e2eMediaRepo) UpdateAsset(_ context.Context, asset *domainmedia.MediaAsset) error {
	r.assets[asset.ID] = asset
	return nil
}

func (r *e2eMediaRepo) UpsertVariants(_ context.Context, variants []*domainmedia.MediaVariant) error {
	for _, variant := range variants {
		r.variants[variant.AssetID] = append(r.variants[variant.AssetID], variant)
	}
	return nil
}

func (r *e2eMediaRepo) LeaseProcessingJob(_ context.Context, assetID int64, profileVersion, owner string, now, leaseUntil time.Time) (*domainmedia.MediaProcessingJob, error) {
	if r.job == nil || r.job.AssetID != assetID || r.job.ProfileVersion != profileVersion ||
		(r.job.State != domainmedia.JobStatePending && r.job.State != domainmedia.JobStateRetryable) {
		return nil, domainmedia.ErrProcessingJobNotFound
	}
	r.job.State = domainmedia.JobStateProcessing
	r.job.Attempts++
	r.job.LeaseOwner = owner
	r.job.LeaseUntil = &leaseUntil
	return r.job, nil
}

func (*e2eMediaRepo) LeaseProcessingJobs(context.Context, string, time.Time, time.Time, int) ([]*domainmedia.MediaProcessingJob, error) {
	return nil, nil
}

func (r *e2eMediaRepo) UpdateProcessingJob(_ context.Context, job *domainmedia.MediaProcessingJob) error {
	r.job = job
	return nil
}

func (r *e2eMediaRepo) UpdateProcessingJobOwned(_ context.Context, job *domainmedia.MediaProcessingJob, _ string) error {
	r.job = job
	return nil
}

func (*e2eMediaRepo) ExtendProcessingLease(context.Context, int64, string, time.Time) error {
	return nil
}

func (r *e2eMediaRepo) FindAssetsByIDs(_ context.Context, ids []int64) (map[int64]*domainmedia.MediaAsset, error) {
	result := map[int64]*domainmedia.MediaAsset{}
	for _, id := range ids {
		if asset := r.assets[id]; asset != nil {
			result[id] = asset
		}
	}
	return result, nil
}

func (r *e2eMediaRepo) ListReadyVariantsByAssetIDs(_ context.Context, ids []int64) (map[int64][]*domainmedia.MediaVariant, error) {
	result := map[int64][]*domainmedia.MediaVariant{}
	for _, id := range ids {
		result[id] = r.variants[id]
	}
	return result, nil
}

func (r *e2eMediaRepo) UpdateVariantPromotion(
	_ context.Context,
	variantID int64,
	expectedObjectKey string,
	expectedPublic bool,
	objectKey string,
	public bool,
) (bool, error) {
	for _, variants := range r.variants {
		for _, variant := range variants {
			if variant.ID == variantID && variant.ObjectKey == expectedObjectKey && variant.Public == expectedPublic {
				variant.ObjectKey = objectKey
				variant.Public = public
				return true, nil
			}
		}
	}
	return false, nil
}

func (r *e2eMediaRepo) ListReadyVariants(_ context.Context, assetID int64) ([]*domainmedia.MediaVariant, error) {
	return r.variants[assetID], nil
}

func (r *e2eMediaRepo) CreateCleanupTasks(_ context.Context, tasks []*domainmedia.CleanupTask) error {
	for _, task := range tasks {
		copy := *task
		copy.ID = int64(len(r.cleanupTasks) + 1)
		r.cleanupTasks = append(r.cleanupTasks, &copy)
	}
	return nil
}

func (r *e2eMediaRepo) LeaseCleanupTasks(_ context.Context, owner string, now, leaseUntil time.Time, limit int) ([]*domainmedia.CleanupTask, error) {
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

func (*e2eMediaRepo) UpdateCleanupTask(context.Context, *domainmedia.CleanupTask) error {
	return nil
}

func (r *e2eMediaRepo) ListIncompletePublicCleanupTasks(_ context.Context, assetIDs []int64) ([]*domainmedia.CleanupTask, error) {
	allowed := map[int64]struct{}{}
	for _, assetID := range assetIDs {
		allowed[assetID] = struct{}{}
	}
	var tasks []*domainmedia.CleanupTask
	for _, task := range r.cleanupTasks {
		if _, ok := allowed[task.AssetID]; ok &&
			strings.HasPrefix(task.ObjectKey, "media/") &&
			task.State != domainmedia.CleanupStateCompleted {
			tasks = append(tasks, task)
		}
	}
	return tasks, nil
}

func (*e2eMediaRepo) ReleaseExpiredCleanupLeases(context.Context, time.Time) (int64, error) {
	return 0, nil
}

type e2eObjectStore struct {
	objects map[string]domainmedia.ObjectMetadata
}

func (s *e2eObjectStore) Put(_ context.Context, key string, _ io.Reader, size int64, contentType, checksum string) (*domainmedia.ObjectMetadata, error) {
	metadata := domainmedia.ObjectMetadata{Key: key, SizeBytes: size, ContentType: contentType, ChecksumSHA256: checksum, LastModified: time.Now().UTC()}
	s.objects[key] = metadata
	return &metadata, nil
}

func (s *e2eObjectStore) Open(_ context.Context, key string) (io.ReadCloser, *domainmedia.ObjectMetadata, error) {
	metadata, ok := s.objects[key]
	if !ok {
		return nil, nil, domainmedia.ErrObjectNotFound
	}
	return io.NopCloser(bytes.NewReader(make([]byte, metadata.SizeBytes))), &metadata, nil
}

func (s *e2eObjectStore) Head(_ context.Context, key string) (*domainmedia.ObjectMetadata, error) {
	metadata, ok := s.objects[key]
	if !ok {
		return nil, domainmedia.ErrObjectNotFound
	}
	return &metadata, nil
}

func (s *e2eObjectStore) Delete(_ context.Context, key string) error {
	delete(s.objects, key)
	return nil
}

func (s *e2eObjectStore) List(context.Context, string) ([]domainmedia.ObjectMetadata, error) {
	result := []domainmedia.ObjectMetadata{}
	for _, metadata := range s.objects {
		result = append(result, metadata)
	}
	return result, nil
}

func (*e2eObjectStore) PresignPut(_ context.Context, key, _ string, _ string, _ int64, expiry time.Duration) (*domainmedia.PresignedRequest, error) {
	return &domainmedia.PresignedRequest{URL: "https://upload.example.test/" + key, Method: http.MethodPut, ExpiresAt: time.Now().Add(expiry)}, nil
}

func (*e2eObjectStore) PresignGet(_ context.Context, key string, expiry time.Duration) (*domainmedia.PresignedRequest, error) {
	return &domainmedia.PresignedRequest{URL: "https://signed.example.test/" + key, Method: http.MethodGet, ExpiresAt: time.Now().Add(expiry)}, nil
}

type e2eProcessor struct {
	store *e2eObjectStore
}

type e2eCacheInvalidator struct {
	count int
}

func (c *e2eCacheInvalidator) InvalidateVideo(context.Context, int64) error {
	c.count++
	return nil
}

func (p *e2eProcessor) Process(_ context.Context, asset *domainmedia.MediaAsset, job *domainmedia.MediaProcessingJob) (*applicationmedia.ProcessResult, error) {
	key := "processed/" + protectedMediaVariantSuffix(asset.ID)
	p.store.objects[key] = domainmedia.ObjectMetadata{Key: key, SizeBytes: 96, ChecksumSHA256: strings.Repeat("b", 64)}
	return &applicationmedia.ProcessResult{
		Width: 1280, Height: 720, DurationMS: 5000, VideoCodec: "h264", AudioCodec: "aac",
		Variants: []*domainmedia.MediaVariant{{
			AssetID: asset.ID, ProfileVersion: job.ProfileVersion, SourceType: domainmedia.SourceTypeMP4,
			Format: "mp4", Codec: "h264", AudioCodec: "aac", Width: 1280, Height: 720,
			Bitrate: 2_500_000, Quality: "720p", ObjectKey: key, Role: domainmedia.VariantRoleBaseline,
			SortOrder: 10, State: domainmedia.VariantStateReady, ChecksumSHA256: strings.Repeat("b", 64),
			SizeBytes: 96, Public: false,
		}},
	}, nil
}

func protectedMediaVariantSuffix(assetID int64) string {
	return strconv.FormatInt(assetID, 10) + "/v1/baseline.mp4"
}
