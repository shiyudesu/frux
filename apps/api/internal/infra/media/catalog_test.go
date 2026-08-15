package inframedia

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
)

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
	if !strings.HasPrefix(delivery.MediaURL, "https://cdn.example.test/media/v3/") ||
		!strings.HasSuffix(delivery.MediaURL, "/11/baseline.mp4") ||
		!strings.HasPrefix(delivery.CoverURL, "https://cdn.example.test/media/v3/") ||
		!strings.HasSuffix(delivery.CoverURL, "/21/cover.jpg") {
		t.Fatalf("unexpected promoted delivery: %+v", delivery)
	}
	if _, err := store.Head(context.Background(), videoKey); err != nil {
		t.Fatal("protected processed video should remain as the canonical copy")
	}
	publicObjects, err := store.List(context.Background(), "media")
	if err != nil {
		t.Fatalf("list public objects: %v", err)
	}
	if len(publicObjects) != 0 {
		t.Fatalf("promotion copied public objects: %+v", publicObjects)
	}
	if err := catalog.ProtectVideo(context.Background(), 9, 1, 2); err != nil {
		t.Fatalf("protect delivery: %v", err)
	}
	if _, err := store.Head(context.Background(), videoKey); err != nil {
		t.Fatalf("protected video missing after demotion: %v", err)
	}
	if repo.variants[1][0].Public ||
		repo.variants[1][0].ExposureGeneration != "" ||
		repo.variants[1][0].ObjectKey != videoKey {
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
	generation, _, _, ok := domainmedia.ParsePublicExposureKey(manifestKey)
	if !ok {
		t.Fatalf("invalid manifest exposure key %q", manifestKey)
	}
	for _, variant := range repo.variants[7] {
		if !variant.Public || variant.ExposureGeneration != generation {
			t.Fatalf("variant did not share exposure generation: %+v", variant)
		}
	}
	publicObjects, err := store.List(context.Background(), "media")
	if err != nil || len(publicObjects) != 0 {
		t.Fatalf("DASH promotion copied public objects: objects=%+v err=%v", publicObjects, err)
	}
}

func TestDeliveryCatalogMigratesLegacyPublicObjectWithoutImmediateDeletion(t *testing.T) {
	store := newCatalogTestStore(t)
	content, checksum := catalogTestContent()
	protectedKey := "processed/8/v1/baseline.mp4"
	legacyKey := "media/v2/legacy-generation/8/v1/baseline.mp4"
	putCatalogTestObjects(t, store, content, checksum, protectedKey, legacyKey)
	repo := &deliveryRepositoryStub{
		assets: map[int64]*domainmedia.MediaAsset{
			8: {ID: 8, State: domainmedia.AssetStateReady, StorageBackend: domainmedia.StorageBackendLocal},
		},
		variants: map[int64][]*domainmedia.MediaVariant{
			8: {{ID: 81, AssetID: 8, SourceType: domainmedia.SourceTypeMP4, ObjectKey: legacyKey, Role: domainmedia.VariantRoleBaseline, State: domainmedia.VariantStateReady, ChecksumSHA256: checksum, SizeBytes: int64(len(content)), Public: true}},
		},
	}
	catalog := newCatalogTestCatalog(t, repo, store)
	if _, err := catalog.ResolveVideo(context.Background(), 18, 8, 0); err != nil {
		t.Fatalf("migrate public object: %v", err)
	}
	variant := repo.variants[8][0]
	if variant.ObjectKey != protectedKey || !variant.Public || variant.ExposureGeneration == "" {
		t.Fatalf("legacy variant was not migrated to virtual exposure: %+v", variant)
	}
	if _, err := store.Head(context.Background(), legacyKey); err != nil {
		t.Fatalf("legacy object was deleted before cache window: %v", err)
	}
	if len(repo.cleanupTasks) != 1 ||
		repo.cleanupTasks[0].ObjectKey != legacyKey ||
		time.Until(repo.cleanupTasks[0].NotBefore) < 29*time.Minute {
		t.Fatalf("legacy cleanup was not delayed: %+v", repo.cleanupTasks)
	}
}

func TestDeliveryCatalogRepairsMissingProtectedLegacyCounterpart(t *testing.T) {
	store := newCatalogTestStore(t)
	content, checksum := catalogTestContent()
	protectedKey := "processed/9/v1/baseline.mp4"
	legacyKey := "media/v2/legacy-generation/9/v1/baseline.mp4"
	putCatalogTestObjects(t, store, content, checksum, legacyKey)
	repo := &deliveryRepositoryStub{
		assets: map[int64]*domainmedia.MediaAsset{
			9: {ID: 9, State: domainmedia.AssetStateReady, StorageBackend: domainmedia.StorageBackendLocal},
		},
		variants: map[int64][]*domainmedia.MediaVariant{
			9: {{ID: 91, AssetID: 9, SourceType: domainmedia.SourceTypeMP4, ObjectKey: legacyKey, Role: domainmedia.VariantRoleBaseline, State: domainmedia.VariantStateReady, ChecksumSHA256: checksum, SizeBytes: int64(len(content)), Public: true}},
		},
	}
	catalog := newCatalogTestCatalog(t, repo, store)
	if _, err := catalog.ResolveVideo(context.Background(), 19, 9, 0); err != nil {
		t.Fatalf("repair legacy object: %v", err)
	}
	if _, err := store.Head(context.Background(), protectedKey); err != nil {
		t.Fatalf("protected counterpart was not repaired: %v", err)
	}
}

func TestDeliveryCatalogKeepsLegacyIdentityWhenCleanupSchedulingFails(t *testing.T) {
	store := newCatalogTestStore(t)
	content, checksum := catalogTestContent()
	protectedKey := "processed/10/v1/baseline.mp4"
	legacyKey := "media/v2/legacy-generation/10/v1/baseline.mp4"
	putCatalogTestObjects(t, store, content, checksum, legacyKey)
	repo := &deliveryRepositoryStub{
		assets: map[int64]*domainmedia.MediaAsset{
			10: {ID: 10, State: domainmedia.AssetStateReady, StorageBackend: domainmedia.StorageBackendLocal},
		},
		variants: map[int64][]*domainmedia.MediaVariant{
			10: {{ID: 101, AssetID: 10, SourceType: domainmedia.SourceTypeMP4, ObjectKey: legacyKey, Role: domainmedia.VariantRoleBaseline, State: domainmedia.VariantStateReady, ChecksumSHA256: checksum, SizeBytes: int64(len(content)), Public: true}},
		},
		createCleanupErr: errors.New("cleanup unavailable"),
	}
	catalog := newCatalogTestCatalog(t, repo, store)
	if _, err := catalog.ResolveVideo(context.Background(), 20, 10, 0); err == nil {
		t.Fatal("expected cleanup scheduling failure")
	}
	variant := repo.variants[10][0]
	if variant.ObjectKey != legacyKey || !variant.Public {
		t.Fatalf("legacy identity changed before cleanup scheduling: %+v", variant)
	}
	if _, err := store.Head(context.Background(), protectedKey); err != nil {
		t.Fatalf("protected repair was not retained for retry: %v", err)
	}
}

func TestDeliveryCatalogDeletesDueLegacyObject(t *testing.T) {
	store := newCatalogTestStore(t)
	content, checksum := catalogTestContent()
	protectedKey := "processed/11/v1/baseline.mp4"
	legacyKey := "media/v2/legacy-generation/11/v1/baseline.mp4"
	putCatalogTestObjects(t, store, content, checksum, protectedKey, legacyKey)
	repo := &deliveryRepositoryStub{
		assets: map[int64]*domainmedia.MediaAsset{
			11: {ID: 11, State: domainmedia.AssetStateReady, StorageBackend: domainmedia.StorageBackendLocal},
		},
		variants: map[int64][]*domainmedia.MediaVariant{
			11: {{ID: 111, AssetID: 11, SourceType: domainmedia.SourceTypeMP4, ObjectKey: legacyKey, Role: domainmedia.VariantRoleBaseline, State: domainmedia.VariantStateReady, ChecksumSHA256: checksum, SizeBytes: int64(len(content)), Public: true}},
		},
	}
	catalog := newCatalogTestCatalog(t, repo, store)
	if _, err := catalog.ResolveVideo(context.Background(), 21, 11, 0); err != nil {
		t.Fatal(err)
	}
	repo.cleanupTasks[0].NotBefore = time.Now().UTC().Add(-time.Minute)
	if err := catalog.ProtectVideo(context.Background(), 21, 11, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Head(context.Background(), legacyKey); !errors.Is(err, domainmedia.ErrObjectNotFound) {
		t.Fatalf("due legacy object still exists: %v", err)
	}
	if repo.cleanupTasks[0].State != domainmedia.CleanupStateCompleted {
		t.Fatalf("cleanup task = %+v", repo.cleanupTasks[0])
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
	assets           map[int64]*domainmedia.MediaAsset
	variants         map[int64][]*domainmedia.MediaVariant
	cleanupTasks     []*domainmedia.CleanupTask
	createCleanupErr error
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
	if r.createCleanupErr != nil {
		return r.createCleanupErr
	}
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
	now := time.Now().UTC()
	for _, task := range r.cleanupTasks {
		if _, ok := allowed[task.AssetID]; ok &&
			strings.HasPrefix(task.ObjectKey, "media/") &&
			!task.NotBefore.After(now) &&
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
				if !public {
					variant.ExposureGeneration = ""
				}
				return true, nil
			}
		}
	}
	return false, nil
}

func (r *deliveryRepositoryStub) UpdateVariantExposure(
	_ context.Context,
	variantID int64,
	expectedObjectKey string,
	expectedPublic bool,
	expectedGeneration string,
	public bool,
	generation string,
) (bool, error) {
	for _, variants := range r.variants {
		for _, variant := range variants {
			if variant.ID == variantID &&
				variant.ObjectKey == expectedObjectKey &&
				variant.Public == expectedPublic &&
				variant.ExposureGeneration == expectedGeneration {
				variant.Public = public
				variant.ExposureGeneration = generation
				return true, nil
			}
		}
	}
	return false, nil
}
