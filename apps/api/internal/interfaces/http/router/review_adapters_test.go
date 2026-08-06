package interfaceshttprouter

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
	domainreview "github.com/shiyudesu/frux/internal/domain/review"
)

type reviewPreviewRepositoryStub struct {
	assets   map[int64]*domainmedia.MediaAsset
	variants map[int64][]*domainmedia.MediaVariant
}

func (r reviewPreviewRepositoryStub) FindAssetByID(_ context.Context, assetID int64) (*domainmedia.MediaAsset, error) {
	asset := r.assets[assetID]
	if asset == nil {
		return nil, domainmedia.ErrMediaAssetNotFound
	}
	return asset, nil
}

func (r reviewPreviewRepositoryStub) ListReadyVariants(_ context.Context, assetID int64) ([]*domainmedia.MediaVariant, error) {
	return r.variants[assetID], nil
}

type reviewPreviewResolverStub struct {
	keys []string
}

func (*reviewPreviewResolverStub) PublicURL(string) (string, error) {
	return "", nil
}

func (r *reviewPreviewResolverStub) ProtectedURL(
	_ context.Context,
	objectKey string,
	expiry time.Duration,
) (string, time.Time, error) {
	r.keys = append(r.keys, objectKey)
	return "https://signed.example.test/" + objectKey, time.Now().UTC().Add(expiry), nil
}

type reviewPreviewLocalSignerStub struct {
	keys []string
}

func (s *reviewPreviewLocalSignerStub) Sign(
	objectKey string,
	expiry time.Duration,
) (string, time.Time, error) {
	s.keys = append(s.keys, objectKey)
	return "/review-media/" + objectKey, time.Now().UTC().Add(expiry), nil
}

func TestReviewPreviewProviderUsesProtectedReadyVariants(t *testing.T) {
	resolver := &reviewPreviewResolverStub{}
	provider := reviewPreviewProvider{
		repository: reviewPreviewRepositoryStub{
			assets: map[int64]*domainmedia.MediaAsset{
				1: {ID: 1, StorageBackend: domainmedia.StorageBackendS3, ObjectKey: "uploads/1/source.mp4", State: domainmedia.AssetStateReady},
				2: {ID: 2, StorageBackend: domainmedia.StorageBackendS3, ObjectKey: "uploads/2/source.jpg", State: domainmedia.AssetStateReady},
			},
			variants: map[int64][]*domainmedia.MediaVariant{
				1: {{AssetID: 1, Role: domainmedia.VariantRoleBaseline, ObjectKey: "processed/1/baseline.mp4"}},
				2: {{AssetID: 2, Role: domainmedia.VariantRoleCover, ObjectKey: "processed/2/cover.jpg"}},
			},
		},
		resolver: resolver,
	}
	access, err := provider.ResolveHumanPreview(context.Background(), domainreview.ReviewSubject{
		MediaAssetID: 1, CoverAssetID: 2,
	}, 5*time.Minute)
	if err != nil {
		t.Fatalf("resolve preview: %v", err)
	}
	if access.MediaURL != "https://signed.example.test/processed/1/baseline.mp4" ||
		access.CoverURL != "https://signed.example.test/processed/2/cover.jpg" ||
		fmt.Sprint(resolver.keys) != "[processed/1/baseline.mp4 processed/2/cover.jpg]" {
		t.Fatalf("preview access = %#v keys=%v", access, resolver.keys)
	}
}

func TestReviewPreviewProviderSignsLegacyLocalAssets(t *testing.T) {
	signer := &reviewPreviewLocalSignerStub{}
	provider := reviewPreviewProvider{localSigner: signer}
	access, err := provider.ResolveHumanPreview(context.Background(), domainreview.ReviewSubject{
		MediaURL: "/uploads/video/source.mp4",
		CoverURL: "/uploads/cover/source.jpg",
	}, 5*time.Minute)
	if err != nil {
		t.Fatalf("resolve local preview: %v", err)
	}

	if access.MediaURL != "/review-media/video/source.mp4" ||
		access.CoverURL != "/review-media/cover/source.jpg" ||
		fmt.Sprint(signer.keys) != "[video/source.mp4 cover/source.jpg]" {
		t.Fatalf("local preview = %#v keys=%v", access, signer.keys)
	}
}

func TestReviewPreviewProviderRejectsUnprotectedExternalURL(t *testing.T) {
	provider := reviewPreviewProvider{}
	if _, err := provider.ResolveHumanPreview(context.Background(), domainreview.ReviewSubject{
		MediaURL: "https://public.example.test/source.mp4",
	}, 5*time.Minute); !errors.Is(err, domainreview.ErrReviewPreviewUnavailable) {
		t.Fatalf("external preview error = %v", err)
	}
}
