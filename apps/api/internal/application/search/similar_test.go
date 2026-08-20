package applicationsearch

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	domainembedding "github.com/shiyudesu/frux/internal/domain/embedding"
	domainsearch "github.com/shiyudesu/frux/internal/domain/search"
	domainvideo "github.com/shiyudesu/frux/internal/domain/video"
)

type similarVideoRepositoryStub struct {
	fact       *domainembedding.MultimodalVectorFact
	factErr    error
	candidates []domainembedding.MultimodalExactCandidate
	exactErr   error
	exclusions []int64
}

func (r *similarVideoRepositoryStub) FindMultimodalVectorFact(context.Context, int64, domainembedding.MultimodalContractIdentity) (*domainembedding.MultimodalVectorFact, error) {
	return r.fact.Clone(), r.factErr
}

func (r *similarVideoRepositoryStub) ExactMultimodalSearch(_ context.Context, _ domainembedding.MultimodalContractIdentity, _ []float64, exclusions []int64, _ int) ([]domainembedding.MultimodalExactCandidate, error) {
	r.exclusions = append([]int64(nil), exclusions...)
	return append([]domainembedding.MultimodalExactCandidate(nil), r.candidates...), r.exactErr
}

func TestSimilarVideoServiceReturnsUnavailableWithoutSourceVector(t *testing.T) {
	now := time.Date(2026, 8, 20, 14, 0, 0, 0, time.UTC)
	contract := searchHybridContract(t)
	repository := &similarVideoRepositoryStub{factErr: domainembedding.ErrMultimodalVectorFactNotFound}
	loader := &publicVideoLoaderStub{videos: map[int64]*domainvideo.Video{1: searchHybridVideo(1, now)}}
	service := similarVideoService(t, repository, loader, contract, now)
	page, err := service.Search(context.Background(), SimilarVideoRequest{SourceVideoID: 1, Limit: 10})
	if err != nil || page.SemanticAvailable || len(page.Items) != 0 || page.HasMore {
		t.Fatalf("unavailable page=%#v err=%v", page, err)
	}
}

func TestSimilarVideoServiceFiltersStaleCandidatesAndPaginates(t *testing.T) {
	now := time.Date(2026, 8, 20, 14, 0, 0, 0, time.UTC)
	contract := searchHybridContract(t)
	repository := &similarVideoRepositoryStub{
		fact: similarSourceFact(t, 1, contract, now),
		candidates: []domainembedding.MultimodalExactCandidate{
			{VideoID: 2, Similarity: 0.9, PublishedAt: now},
			{VideoID: 3, Similarity: 0.8, PublishedAt: now.Add(-time.Minute)},
			{VideoID: 4, Similarity: 0.7, PublishedAt: now.Add(-2 * time.Minute)},
			{VideoID: 5, Similarity: 0.6, PublishedAt: now.Add(-3 * time.Minute)},
		},
	}
	loader := &publicVideoLoaderStub{videos: map[int64]*domainvideo.Video{
		1: searchHybridVideo(1, now),
		2: searchHybridVideo(2, now),
		3: searchHybridVideo(3, now.Add(-time.Minute)),
		4: searchHybridVideo(4, now.Add(-time.Hour)),
		5: searchHybridVideo(5, now.Add(-3*time.Minute)),
	}}
	service := similarVideoService(t, repository, loader, contract, now)
	first, err := service.Search(context.Background(), SimilarVideoRequest{SourceVideoID: 1, Limit: 2})
	if err != nil || !first.SemanticAvailable || !first.HasMore || first.NextCursor == "" ||
		!reflect.DeepEqual(videoResultIDs(first.Items), []int64{2, 3}) ||
		!reflect.DeepEqual(repository.exclusions, []int64{1}) {
		t.Fatalf("first similar page=%#v exclusions=%v err=%v", first, repository.exclusions, err)
	}
	second, err := service.Search(context.Background(), SimilarVideoRequest{
		SourceVideoID: 1, Cursor: first.NextCursor, Limit: 2,
	})
	if err != nil || second.HasMore || !reflect.DeepEqual(videoResultIDs(second.Items), []int64{5}) {
		t.Fatalf("second similar page=%#v err=%v", second, err)
	}
	if _, err := service.Search(context.Background(), SimilarVideoRequest{SourceVideoID: 9, Cursor: first.NextCursor, Limit: 2}); !errors.Is(err, domainsearch.ErrInvalidCursor) {
		t.Fatalf("rebound cursor error=%v", err)
	}
	otherContract := contract
	otherContract.RevisionAlias = "revision-2"
	if _, err := DecodeSimilarVideoCursor(first.NextCursor, 1, otherContract.Key(), now); !errors.Is(err, domainsearch.ErrInvalidCursor) {
		t.Fatalf("contract-changed cursor error=%v", err)
	}
}

func TestSimilarVideoServiceRejectsUnreadableSourceAndInfrastructureFailure(t *testing.T) {
	now := time.Date(2026, 8, 20, 14, 0, 0, 0, time.UTC)
	contract := searchHybridContract(t)
	repository := &similarVideoRepositoryStub{fact: similarSourceFact(t, 1, contract, now)}
	service := similarVideoService(t, repository, &publicVideoLoaderStub{videos: map[int64]*domainvideo.Video{}}, contract, now)
	if _, err := service.Search(context.Background(), SimilarVideoRequest{SourceVideoID: 1}); !errors.Is(err, domainvideo.ErrVideoNotFound) {
		t.Fatalf("unreadable source error=%v", err)
	}
	service = similarVideoService(t, repository, &publicVideoLoaderStub{err: errors.New("database unavailable")}, contract, now)
	if _, err := service.Search(context.Background(), SimilarVideoRequest{SourceVideoID: 1}); !errors.Is(err, ErrSearchFailed) {
		t.Fatalf("load failure error=%v", err)
	}
}

func similarVideoService(
	t testing.TB,
	repository SimilarVideoRepository,
	loader PublicVideoLoader,
	contract domainembedding.MultimodalContractIdentity,
	now time.Time,
) *SimilarVideoService {
	t.Helper()
	service, err := NewSimilarVideoService(repository, loader, contract, 100, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	service.now = func() time.Time { return now }
	return service
}

func similarSourceFact(t testing.TB, videoID int64, contract domainembedding.MultimodalContractIdentity, now time.Time) *domainembedding.MultimodalVectorFact {
	t.Helper()
	values := make([]float64, contract.Dimension)
	values[0] = 1
	sourceHash := domainembedding.MultimodalSourceHash([]byte("source"))
	fact, err := domainembedding.NewMultimodalVectorFact(videoID, &domainembedding.MultimodalVector{
		Identity: domainembedding.MultimodalVectorIdentity{
			Contract: contract, SourceHash: sourceHash,
			VectorDigest: domainembedding.MultimodalVectorDigest(values),
		}, Values: values,
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	return fact
}

func videoResultIDs(items []VideoResult) []int64 {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}
