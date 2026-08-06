package inframedia

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
)

type deleteFailingStore struct {
	domainmedia.MediaObjectStore
	failKey string
}

func (s *deleteFailingStore) Delete(ctx context.Context, key string) error {
	if key == s.failKey {
		return errors.New("forced delete failure")
	}
	return s.MediaObjectStore.Delete(ctx, key)
}

func TestDeliveryCatalogPromotesOnlyResolvedPublicAssets(t *testing.T) {
	store := newCatalogTestStore(t)
	content, checksum := catalogTestContent()
	videoKey := "processed/1/v1/baseline.mp4"
	coverKey := "processed/2/cover/cover.jpg"
	putCatalogTestObjects(t, store, content, checksum, videoKey, coverKey)
	repo := &deliveryRepositoryStub{
		assets: map[int64]*domainmedia.MediaAsset{
			1: {ID: 1, State: domainmedia.AssetStateReady},
			2: {ID: 2, State: domainmedia.AssetStateReady},
		},
		variants: map[int64][]*domainmedia.MediaVariant{
			1: {{ID: 11, AssetID: 1, SourceType: domainmedia.SourceTypeMP4, ObjectKey: videoKey, Role: domainmedia.VariantRoleBaseline, State: domainmedia.VariantStateReady, ChecksumSHA256: checksum, SizeBytes: int64(len(content))}},
			2: {{ID: 21, AssetID: 2, SourceType: domainmedia.SourceTypeImage, ObjectKey: coverKey, Role: domainmedia.VariantRoleCover, State: domainmedia.VariantStateReady, ChecksumSHA256: checksum, SizeBytes: int64(len(content)), Format: "jpg"}},
		},
	}
	catalog := newCatalogTestCatalog(t, repo, store)
	delivery, err := catalog.ResolveVideo(context.Background(), 9, 1, 2)
	if err != nil {
		t.Fatalf("resolve delivery: %v", err)
	}
	if !strings.HasPrefix(delivery.MediaURL, "https://cdn.example.test/media/v2/") ||
		!strings.HasSuffix(delivery.MediaURL, "/1/v1/baseline.mp4") ||
		!strings.HasPrefix(delivery.CoverURL, "https://cdn.example.test/media/v2/") ||
		!strings.HasSuffix(delivery.CoverURL, "/2/cover/cover.jpg") {
		t.Fatalf("unexpected promoted delivery: %+v", delivery)
	}
	if _, err := store.Head(context.Background(), videoKey); err != nil {
		t.Fatal("protected processed video should remain as the canonical copy")
	}
	publicVideoKey := strings.TrimPrefix(delivery.MediaURL, "https://cdn.example.test/")
	if _, err := store.Head(context.Background(), publicVideoKey); err != nil {
		t.Fatalf("public video missing after promotion: %v", err)
	}
	if err := catalog.ProtectVideo(context.Background(), 9, 1, 2); err != nil {
		t.Fatalf("protect delivery: %v", err)
	}
	if _, err := store.Head(context.Background(), publicVideoKey); err == nil {
		t.Fatal("public video remained after protection")
	}
	if _, err := store.Head(context.Background(), videoKey); err != nil {
		t.Fatalf("protected video missing after demotion: %v", err)
	}
	if repo.variants[1][0].Public || repo.variants[1][0].ObjectKey != videoKey {
		t.Fatalf("video variant remained public: %+v", repo.variants[1][0])
	}
}

func TestDeliveryCatalogUsesOneExposureGenerationForDashBundle(t *testing.T) {
	store := newCatalogTestStore(t)
	content, checksum := catalogTestContent()
	keys := []string{
		"processed/7/v1/baseline.mp4",
		"processed/7/v1/dash/manifest.mpd",
		"processed/7/v1/dash/init-stream0.m4s",
		"processed/7/v1/dash/chunk-stream0-00001.m4s",
	}
	putCatalogTestObjects(t, store, content, checksum, keys...)
	repo := &deliveryRepositoryStub{
		assets: map[int64]*domainmedia.MediaAsset{
			7: {ID: 7, State: domainmedia.AssetStateReady},
		},
		variants: map[int64][]*domainmedia.MediaVariant{
			7: {
				{ID: 70, AssetID: 7, SourceType: domainmedia.SourceTypeMP4, ObjectKey: keys[0], Role: domainmedia.VariantRoleBaseline, State: domainmedia.VariantStateReady, ChecksumSHA256: checksum, SizeBytes: int64(len(content))},
				{ID: 71, AssetID: 7, SourceType: domainmedia.SourceTypeDASH, ObjectKey: keys[1], Role: domainmedia.VariantRoleManifest, State: domainmedia.VariantStateReady, ChecksumSHA256: checksum, SizeBytes: int64(len(content))},
				{ID: 72, AssetID: 7, SourceType: domainmedia.SourceTypeDASH, ObjectKey: keys[2], Role: domainmedia.VariantRoleSegment, State: domainmedia.VariantStateReady, ChecksumSHA256: checksum, SizeBytes: int64(len(content))},
				{ID: 73, AssetID: 7, SourceType: domainmedia.SourceTypeDASH, ObjectKey: keys[3], Role: domainmedia.VariantRoleSegment, State: domainmedia.VariantStateReady, ChecksumSHA256: checksum, SizeBytes: int64(len(content))},
			},
		},
	}
	catalog := newCatalogTestCatalog(t, repo, store)
	delivery, err := catalog.ResolveVideo(context.Background(), 9, 7, 0)
	if err != nil {
		t.Fatalf("resolve DASH delivery: %v", err)
	}
	var manifestURL string
	for _, source := range delivery.PlaybackSources {
		if source.Role == domainmedia.VariantRoleManifest {
			manifestURL = source.URL
		}
	}
	if manifestURL == "" {
		t.Fatalf("unexpected DASH delivery: %+v", delivery)
	}
	manifestKey := strings.TrimPrefix(manifestURL, "https://cdn.example.test/")
	bundleDir := strings.TrimSuffix(manifestKey, "manifest.mpd")
	for _, name := range []string{"init-stream0.m4s", "chunk-stream0-00001.m4s"} {
		if _, err := store.Head(context.Background(), bundleDir+name); err != nil {
			t.Fatalf("DASH bundle member %s missing beside manifest: %v", name, err)
		}
	}
}

func TestDeliveryCatalogSchedulesCleanupWhenPublicDeleteFails(t *testing.T) {
	store := newCatalogTestStore(t)
	content, checksum := catalogTestContent()
	protectedKey := "processed/8/v1/baseline.mp4"
	putCatalogTestObjects(t, store, content, checksum, protectedKey)
	repo := &deliveryRepositoryStub{
		assets: map[int64]*domainmedia.MediaAsset{
			8: {ID: 8, State: domainmedia.AssetStateReady, StorageBackend: domainmedia.StorageBackendLocal},
		},
		variants: map[int64][]*domainmedia.MediaVariant{
			8: {{ID: 81, AssetID: 8, SourceType: domainmedia.SourceTypeMP4, ObjectKey: protectedKey, Role: domainmedia.VariantRoleBaseline, State: domainmedia.VariantStateReady, ChecksumSHA256: checksum, SizeBytes: int64(len(content))}},
		},
	}
	catalog := newCatalogTestCatalog(t, repo, store)
	if _, err := catalog.ResolveVideo(context.Background(), 18, 8, 0); err != nil {
		t.Fatalf("promote public object: %v", err)
	}
	publicKey := repo.variants[8][0].ObjectKey
	failing := &deleteFailingStore{MediaObjectStore: store, failKey: publicKey}
	catalog = NewDeliveryCatalog(repo, mustCatalogResolver(t, store), failing)
	if err := catalog.ProtectVideo(context.Background(), 18, 8, 0); err == nil {
		t.Fatal("expected public delete failure")
	}
	if len(repo.cleanupTasks) != 1 || repo.cleanupTasks[0].ObjectKey != publicKey {
		t.Fatalf("public delete failure was not persisted for retry: %+v", repo.cleanupTasks)
	}
	failing.failKey = ""
	if err := catalog.ProtectVideo(context.Background(), 18, 8, 0); err != nil {
		t.Fatalf("retry public cleanup: %v", err)
	}
	if repo.cleanupTasks[0].State != domainmedia.CleanupStateCompleted {
		t.Fatalf("retry did not complete public cleanup: %+v", repo.cleanupTasks[0])
	}
}

func newCatalogTestStore(t *testing.T) *LocalStore {
	t.Helper()
	store, err := NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatalf("create local store: %v", err)
	}
	return store
}

func newCatalogTestCatalog(t *testing.T, repo DeliveryRepository, store domainmedia.MediaObjectStore) *DeliveryCatalog {
	t.Helper()
	return NewDeliveryCatalog(repo, mustCatalogResolver(t, store), store)
}

func mustCatalogResolver(t *testing.T, store domainmedia.MediaObjectStore) domainmedia.MediaURLResolver {
	t.Helper()
	resolver, err := NewURLResolver("https://cdn.example.test", store)
	if err != nil {
		t.Fatalf("create resolver: %v", err)
	}
	return resolver
}

func catalogTestContent() ([]byte, string) {
	content := []byte("processed-output")
	sum := sha256.Sum256(content)
	return content, hex.EncodeToString(sum[:])
}

func putCatalogTestObjects(t *testing.T, store *LocalStore, content []byte, checksum string, keys ...string) {
	t.Helper()
	for _, key := range keys {
		if _, err := store.Put(context.Background(), key, bytes.NewReader(content), int64(len(content)), "application/octet-stream", checksum); err != nil {
			t.Fatalf("put %s: %v", key, err)
		}
	}
}

type deliveryRepositoryStub struct {
	assets       map[int64]*domainmedia.MediaAsset
	variants     map[int64][]*domainmedia.MediaVariant
	cleanupTasks []*domainmedia.CleanupTask
}

func (r *deliveryRepositoryStub) FindAssetsByIDs(_ context.Context, ids []int64) (map[int64]*domainmedia.MediaAsset, error) {
	result := map[int64]*domainmedia.MediaAsset{}
	for _, id := range ids {
		result[id] = r.assets[id]
	}
	return result, nil
}

func (r *deliveryRepositoryStub) ListReadyVariantsByAssetIDs(_ context.Context, ids []int64) (map[int64][]*domainmedia.MediaVariant, error) {
	result := map[int64][]*domainmedia.MediaVariant{}
	for _, id := range ids {
		result[id] = r.variants[id]
	}
	return result, nil
}

func (*deliveryRepositoryStub) UpsertVariants(context.Context, []*domainmedia.MediaVariant) error {
	return nil
}

func (r *deliveryRepositoryStub) CreateCleanupTasks(_ context.Context, tasks []*domainmedia.CleanupTask) error {
	for _, task := range tasks {
		cloned := *task
		cloned.ID = int64(len(r.cleanupTasks) + 1)
		r.cleanupTasks = append(r.cleanupTasks, &cloned)
	}
	return nil
}

func (r *deliveryRepositoryStub) ListIncompletePublicCleanupTasks(_ context.Context, assetIDs []int64) ([]*domainmedia.CleanupTask, error) {
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

func (*deliveryRepositoryStub) UpdateCleanupTask(context.Context, *domainmedia.CleanupTask) error {
	return nil
}

func (r *deliveryRepositoryStub) UpdateVariantPromotion(
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
