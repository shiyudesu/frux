package interfaceshttprouter

import (
	"context"
	"errors"
	"testing"
	"time"

	applicationembedding "github.com/shiyudesu/frux/internal/application/embedding"
	applicationsearch "github.com/shiyudesu/frux/internal/application/search"
	domainembedding "github.com/shiyudesu/frux/internal/domain/embedding"
	domainsearch "github.com/shiyudesu/frux/internal/domain/search"
	domainvideo "github.com/shiyudesu/frux/internal/domain/video"
	infraconfig "github.com/shiyudesu/frux/internal/infra/config"
)

type routerMultimodalProviderStub struct {
	contract domainembedding.MultimodalContractIdentity
	err      error
}

func (s *routerMultimodalProviderStub) Contract() domainembedding.MultimodalContractIdentity {
	return s.contract
}

func (s *routerMultimodalProviderStub) EmbedVideoContent(
	context.Context,
	applicationembedding.MultimodalVideoEmbeddingRequest,
) (*applicationembedding.MultimodalEmbeddingResult, error) {
	return nil, errors.New("unused")
}

func (s *routerMultimodalProviderStub) EmbedQueryText(
	_ context.Context,
	request applicationembedding.MultimodalQueryEmbeddingRequest,
) (*applicationembedding.MultimodalEmbeddingResult, error) {
	if s.err != nil {
		return nil, s.err
	}
	vector := make([]float64, s.contract.Dimension)
	vector[0] = 1
	return &applicationembedding.MultimodalEmbeddingResult{
		Identity: domainembedding.MultimodalVectorIdentity{
			Contract: s.contract, SourceHash: request.SourceHash,
			VectorDigest: domainembedding.MultimodalVectorDigest(vector),
		},
		Vector: vector,
	}, nil
}

type routerMultimodalVideoRepositoryStub struct {
	lexical  []*domainsearch.VideoIndexItem
	readable map[int64]*domainvideo.Video
}

func (s *routerMultimodalVideoRepositoryStub) SearchVideos(
	context.Context,
	string,
	*domainsearch.VideoCursor,
	int,
) ([]*domainsearch.VideoIndexItem, error) {
	return append([]*domainsearch.VideoIndexItem(nil), s.lexical...), nil
}

func (s *routerMultimodalVideoRepositoryStub) BatchGetReadable(
	_ context.Context,
	_ int64,
	ids []int64,
	_ bool,
) (map[int64]*domainvideo.Video, error) {
	result := make(map[int64]*domainvideo.Video, len(ids))
	for _, id := range ids {
		if video := s.readable[id]; video != nil {
			cloned := *video
			result[id] = &cloned
		}
	}
	return result, nil
}

type routerMultimodalUserRepositoryStub struct{}

func (*routerMultimodalUserRepositoryStub) SearchUsers(
	context.Context,
	string,
	*domainsearch.UserCursor,
	int,
) ([]*domainsearch.UserIndexItem, error) {
	return nil, nil
}

type routerMultimodalSemanticIndexStub struct {
	candidates []domainembedding.MultimodalExactCandidate
}

func (s *routerMultimodalSemanticIndexStub) ExactMultimodalSearch(
	context.Context,
	domainembedding.MultimodalContractIdentity,
	[]float64,
	[]int64,
	int,
) ([]domainembedding.MultimodalExactCandidate, error) {
	return append([]domainembedding.MultimodalExactCandidate(nil), s.candidates...), nil
}

func TestNewMultimodalSearchServiceScopesProviderByFeature(t *testing.T) {
	contract := routerMultimodalContract(t)
	videoRepository := &routerMultimodalVideoRepositoryStub{}
	userRepository := &routerMultimodalUserRepositoryStub{}
	semanticIndex := &routerMultimodalSemanticIndexStub{}
	for _, test := range []struct {
		name string
		cfg  infraconfig.MultimodalConfig
	}{
		{name: "disabled"},
		{name: "similar only", cfg: infraconfig.MultimodalConfig{
			Enabled: true, SimilarVideosEnabled: true,
			Contract: routerMultimodalContractConfig(contract),
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			service, err := newMultimodalSearchService(
				context.Background(), test.cfg, videoRepository, userRepository, semanticIndex,
				func(context.Context, infraconfig.MultimodalConfig, string) (readyMultimodalProvider, error) {
					calls++
					return nil, errors.New("provider must not be constructed")
				},
			)
			if err != nil || service == nil || calls != 0 {
				t.Fatalf("service=%#v calls=%d err=%v", service, calls, err)
			}
		})
	}
}

func TestNewMultimodalSearchServiceWiresHybridAndDegrades(t *testing.T) {
	contract := routerMultimodalContract(t)
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	videos := &routerMultimodalVideoRepositoryStub{
		lexical: []*domainsearch.VideoIndexItem{{
			ID: 1, Title: "lexical", Relevance: domainsearch.VideoRelevanceExactTitle, PublishedAt: now,
		}},
		readable: map[int64]*domainvideo.Video{
			1: routerReadableVideo(1, "lexical", now),
			2: routerReadableVideo(2, "semantic", now.Add(-time.Minute)),
		},
	}
	semantic := &routerMultimodalSemanticIndexStub{candidates: []domainembedding.MultimodalExactCandidate{{
		VideoID: 2, Similarity: 0.9, PublishedAt: now.Add(-time.Minute),
	}}}
	cfg := routerMultimodalHybridConfig(contract)
	provider := &routerMultimodalProviderStub{contract: contract}
	calls := 0
	service, err := newMultimodalSearchService(
		context.Background(), cfg, videos, &routerMultimodalUserRepositoryStub{}, semantic,
		func(_ context.Context, _ infraconfig.MultimodalConfig, capability string) (readyMultimodalProvider, error) {
			calls++
			if capability != "query" {
				t.Fatalf("capability=%q", capability)
			}
			return provider, nil
		},
	)
	if err != nil || calls != 1 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
	page, err := service.SearchVideos(context.Background(), applicationsearch.Request{Query: "semantic", Limit: 10})
	if err != nil || len(page.Items) != 2 || page.Items[1].ID != 2 {
		t.Fatalf("hybrid page=%#v err=%v", page, err)
	}

	provider.err = errors.New("provider unavailable")
	degradedService, err := newMultimodalSearchService(
		context.Background(), cfg, videos, &routerMultimodalUserRepositoryStub{}, semantic,
		func(context.Context, infraconfig.MultimodalConfig, string) (readyMultimodalProvider, error) {
			return provider, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	degraded, err := degradedService.SearchVideos(context.Background(), applicationsearch.Request{Query: "semantic", Limit: 10})
	if err != nil || len(degraded.Items) != 1 || degraded.Items[0].ID != 1 {
		t.Fatalf("degraded page=%#v err=%v", degraded, err)
	}
}

func TestNewMultimodalSearchServiceFailsClosedOnProviderStartup(t *testing.T) {
	contract := routerMultimodalContract(t)
	want := errors.New("provider unavailable")
	service, err := newMultimodalSearchService(
		context.Background(), routerMultimodalHybridConfig(contract),
		&routerMultimodalVideoRepositoryStub{}, &routerMultimodalUserRepositoryStub{},
		&routerMultimodalSemanticIndexStub{},
		func(context.Context, infraconfig.MultimodalConfig, string) (readyMultimodalProvider, error) {
			return nil, want
		},
	)
	if service != nil || !errors.Is(err, want) {
		t.Fatalf("service=%#v err=%v", service, err)
	}
}

func routerMultimodalHybridConfig(contract domainembedding.MultimodalContractIdentity) infraconfig.MultimodalConfig {
	return infraconfig.MultimodalConfig{
		Enabled: true, QueryEmbeddingEnabled: true, HybridSearchEnabled: true,
		Contract: routerMultimodalContractConfig(contract),
		Provider: infraconfig.MultimodalProviderConfig{Deadline: "1s", AdmissionLimit: 1},
		Query:    infraconfig.MultimodalQueryConfig{MaxRunes: 64, CacheTTL: "1m", CacheEntries: 10},
		Exact:    infraconfig.MultimodalExactConfig{MaxLimit: 100},
		Hybrid: infraconfig.MultimodalHybridConfig{
			Version:   domainembedding.MultimodalHybridMergeVersionV1,
			PoolLimit: 100, LexicalReservation: 1, SemanticReservation: 1, CursorTTL: "15m",
		},
	}
}

func routerMultimodalContractConfig(contract domainembedding.MultimodalContractIdentity) infraconfig.MultimodalContractConfig {
	return infraconfig.MultimodalContractConfig{
		ProviderAlias: contract.ProviderAlias, ModelAlias: contract.ModelAlias,
		RevisionAlias: contract.RevisionAlias, Dimension: contract.Dimension,
		TextCanonicalizer:        contract.TextCanonicalizer,
		FrameSamplingPolicy:      contract.FrameSamplingPolicy,
		ImagePreprocessingPolicy: contract.ImagePreprocessingPolicy,
		FusionPolicy:             contract.FusionPolicy,
	}
}

func routerMultimodalContract(t testing.TB) domainembedding.MultimodalContractIdentity {
	t.Helper()
	contract, err := domainembedding.NewMultimodalContractIdentity(
		"provider", "model", "revision", domainembedding.MinMultimodalDimension,
		domainembedding.MultimodalTextCanonicalizerV1,
		domainembedding.MultimodalFrameSamplingPolicyV1,
		domainembedding.MultimodalImagePreprocessingV1,
		domainembedding.MultimodalFusionPolicyV1,
	)
	if err != nil {
		t.Fatal(err)
	}
	return contract
}

func routerReadableVideo(id int64, title string, publishedAt time.Time) *domainvideo.Video {
	return &domainvideo.Video{
		ID: id, AuthorID: 1, Title: title, Status: domainvideo.StatusPublished,
		Visibility: domainvideo.VisibilityPublic, MediaStatus: "ready",
		MediaURL: "https://example.com/video.mp4", PublishedAt: &publishedAt,
		CreatedAt: publishedAt, UpdatedAt: publishedAt,
	}
}
