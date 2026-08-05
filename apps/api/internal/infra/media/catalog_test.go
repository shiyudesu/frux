package inframedia

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
)

func TestDeliveryCatalogPromotesOnlyResolvedPublicAssets(t *testing.T) {
	store, err := NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatalf("create local store: %v", err)
	}
	content := []byte("processed-output")
	sum := sha256.Sum256(content)
	checksum := hex.EncodeToString(sum[:])
	videoKey := "processed/1/v1/baseline.mp4"
	coverKey := "processed/2/cover/cover.jpg"
	for _, key := range []string{videoKey, coverKey} {
		if _, err := store.Put(context.Background(), key, bytes.NewReader(content), int64(len(content)), "application/octet-stream", checksum); err != nil {
			t.Fatalf("put %s: %v", key, err)
		}
	}
	repo := &deliveryRepositoryStub{
		assets: map[int64]*domainmedia.MediaAsset{
			1: {ID: 1, State: domainmedia.AssetStateReady},
			2: {ID: 2, State: domainmedia.AssetStateReady},
		},
		variants: map[int64][]*domainmedia.MediaVariant{
			1: {{AssetID: 1, SourceType: domainmedia.SourceTypeMP4, ObjectKey: videoKey, Role: domainmedia.VariantRoleBaseline, State: domainmedia.VariantStateReady, ChecksumSHA256: checksum, SizeBytes: int64(len(content))}},
			2: {{AssetID: 2, SourceType: domainmedia.SourceTypeImage, ObjectKey: coverKey, Role: domainmedia.VariantRoleCover, State: domainmedia.VariantStateReady, ChecksumSHA256: checksum, SizeBytes: int64(len(content)), Format: "jpg"}},
		},
	}
	resolver, err := NewURLResolver("https://cdn.example.test", store)
	if err != nil {
		t.Fatalf("create resolver: %v", err)
	}
	catalog := NewDeliveryCatalog(repo, resolver, store)
	delivery, err := catalog.ResolveVideo(context.Background(), 9, 1, 2)
	if err != nil {
		t.Fatalf("resolve delivery: %v", err)
	}
	if delivery.MediaURL != "https://cdn.example.test/media/1/v1/baseline.mp4" ||
		delivery.CoverURL != "https://cdn.example.test/media/2/cover/cover.jpg" {
		t.Fatalf("unexpected promoted delivery: %+v", delivery)
	}
	if _, err := store.Head(context.Background(), videoKey); err == nil {
		t.Fatal("protected processed video should be removed after promotion")
	}
	if _, err := store.Head(context.Background(), "media/1/v1/baseline.mp4"); err != nil {
		t.Fatalf("public video missing after promotion: %v", err)
	}
}

type deliveryRepositoryStub struct {
	assets   map[int64]*domainmedia.MediaAsset
	variants map[int64][]*domainmedia.MediaVariant
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

func (*deliveryRepositoryStub) UpdateVariantPromotion(context.Context, int64, string, bool) error {
	return nil
}
