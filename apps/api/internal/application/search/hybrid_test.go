package applicationsearch

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	domainembedding "github.com/shiyudesu/frux/internal/domain/embedding"
	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
	domainsearch "github.com/shiyudesu/frux/internal/domain/search"
	domainvideo "github.com/shiyudesu/frux/internal/domain/video"
)

type semanticQueryEmbedderStub struct {
	vector *domainembedding.MultimodalQueryVector
	err    error
	calls  int
}

func (s *semanticQueryEmbedderStub) EmbedPublicQuery(context.Context, string) (*domainembedding.MultimodalQueryVector, error) {
	s.calls++
	return s.vector.Clone(), s.err
}

type semanticVideoIndexStub struct {
	items []domainembedding.MultimodalExactCandidate
	err   error
	calls int
}

func (s *semanticVideoIndexStub) ExactMultimodalSearch(context.Context, domainembedding.MultimodalContractIdentity, []float64, []int64, int) ([]domainembedding.MultimodalExactCandidate, error) {
	s.calls++
	return append([]domainembedding.MultimodalExactCandidate(nil), s.items...), s.err
}

type publicVideoLoaderStub struct {
	videos map[int64]*domainvideo.Video
	err    error
	calls  int
}

func (s *publicVideoLoaderStub) BatchGetReadable(_ context.Context, _ int64, ids []int64, _ bool) (map[int64]*domainvideo.Video, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	result := make(map[int64]*domainvideo.Video, len(ids))
	for _, id := range ids {
		if video := s.videos[id]; video != nil {
			cloned := *video
			result[id] = &cloned
		}
	}
	return result, nil
}

func TestHybridVideoCursorBindsModeVersionContractAndExpiry(t *testing.T) {
	now := time.Date(2026, 8, 20, 13, 0, 0, 0, time.UTC)
	cursor := EncodeHybridVideoCursor("cat", &HybridVideoCursor{
		Mode: VideoRetrievalModeHybrid, RankingVersion: domainembedding.MultimodalHybridMergeVersionV1,
		ContractKey: "contract", HybridScore: 1.25, PublishedAt: now, VideoID: 9,
		ExpiresAt: now.Add(time.Minute),
	})
	decoded, err := DecodeHybridVideoCursor(cursor, "cat", domainembedding.MultimodalHybridMergeVersionV1, "contract", now)
	if err != nil || decoded.Mode != VideoRetrievalModeHybrid || decoded.HybridScore != 1.25 || decoded.VideoID != 9 {
		t.Fatalf("decoded cursor=%#v err=%v", decoded, err)
	}
	for _, test := range []struct {
		query, version, contract string
		now                      time.Time
	}{
		{query: "dog", version: domainembedding.MultimodalHybridMergeVersionV1, contract: "contract", now: now},
		{query: "cat", version: "v2", contract: "contract", now: now},
		{query: "cat", version: domainembedding.MultimodalHybridMergeVersionV1, contract: "other", now: now},
		{query: "cat", version: domainembedding.MultimodalHybridMergeVersionV1, contract: "contract", now: now.Add(2 * time.Minute)},
	} {
		if _, err := DecodeHybridVideoCursor(cursor, test.query, test.version, test.contract, test.now); !errors.Is(err, domainsearch.ErrInvalidCursor) {
			t.Fatalf("rebound cursor error=%v for %#v", err, test)
		}
	}
	legacy := EncodeVideoCursor("cat", &domainsearch.VideoCursor{
		Relevance: domainsearch.VideoRelevanceExactTitle, PublishedAt: now, VideoID: 9,
	})
	if _, err := DecodeHybridVideoCursor(legacy, "cat", domainembedding.MultimodalHybridMergeVersionV1, "contract", now); !errors.Is(err, domainsearch.ErrInvalidCursor) {
		t.Fatalf("legacy cursor error=%v", err)
	}
}

func TestMixHybridVideoCandidatesIsDeterministicAndRetainsOverlap(t *testing.T) {
	now := time.Date(2026, 8, 20, 13, 0, 0, 0, time.UTC)
	config := HybridVideoSearchConfig{PoolLimit: 4, LexicalReservation: 1, SemanticReservation: 1}
	lexical := []*domainsearch.VideoIndexItem{
		{ID: 1, Relevance: domainsearch.VideoRelevanceExactTitle, PublishedAt: now},
		{ID: 2, Relevance: domainsearch.VideoRelevanceTitleContains, PublishedAt: now.Add(-time.Minute)},
		{ID: 4, Relevance: domainsearch.VideoRelevanceDescriptionOnly, PublishedAt: now.Add(-2 * time.Minute)},
	}
	semantic := []domainembedding.MultimodalExactCandidate{
		{VideoID: 2, Similarity: 1, PublishedAt: now},
		{VideoID: 3, Similarity: 0.9, PublishedAt: now},
	}
	videos := map[int64]*domainvideo.Video{
		2: searchHybridVideo(2, now.Add(-time.Minute)),
		3: searchHybridVideo(3, now.Add(-2*time.Minute)),
	}
	first := mixHybridVideoCandidates(lexical, semantic, videos, config)
	second := mixHybridVideoCandidates(lexical, semantic, videos, config)
	if ids := hybridCandidateIDs(first); !reflect.DeepEqual(ids, []int64{1, 2, 4, 3}) ||
		!reflect.DeepEqual(ids, hybridCandidateIDs(second)) {
		t.Fatalf("hybrid order=%v second=%v", ids, hybridCandidateIDs(second))
	}
	if !reflect.DeepEqual(first[1].reasons, []string{HybridReasonLexical, HybridReasonSemantic}) {
		t.Fatalf("overlap reasons=%v", first[1].reasons)
	}
}

func TestHybridVideoSearchFallsBackLexicallyAndPreservesMode(t *testing.T) {
	now := time.Date(2026, 8, 20, 13, 0, 0, 0, time.UTC)
	contract := searchHybridContract(t)
	videoIndex := &videoIndexStub{items: []*domainsearch.VideoIndexItem{
		{ID: 3, Relevance: domainsearch.VideoRelevanceExactTitle, PublishedAt: now},
		{ID: 2, Relevance: domainsearch.VideoRelevanceTitlePrefix, PublishedAt: now},
		{ID: 1, Relevance: domainsearch.VideoRelevanceTitleContains, PublishedAt: now},
	}}
	embedder := &semanticQueryEmbedderStub{err: errors.New("provider unavailable")}
	service := hybridSearchService(t, videoIndex, embedder, &semanticVideoIndexStub{}, &publicVideoLoaderStub{}, contract, now)
	page, err := service.SearchVideos(context.Background(), Request{Query: "cat", Limit: 2})
	if err != nil || len(page.Items) != 2 || !page.HasMore || page.NextCursor == "" {
		t.Fatalf("fallback page=%#v err=%v", page, err)
	}
	cursor, err := DecodeHybridVideoCursor(page.NextCursor, "cat", domainembedding.MultimodalHybridMergeVersionV1, contract.Key(), now)
	if err != nil || cursor.Mode != VideoRetrievalModeLexical || cursor.ContractKey != "" {
		t.Fatalf("fallback cursor=%#v err=%v", cursor, err)
	}
	videoIndex.items = []*domainsearch.VideoIndexItem{{ID: 1, Relevance: domainsearch.VideoRelevanceTitleContains, PublishedAt: now}}
	second, err := service.SearchVideos(context.Background(), Request{Query: "cat", Cursor: page.NextCursor, Limit: 2})
	if err != nil || len(second.Items) != 1 || embedder.calls != 1 || videoIndex.lastCursor.VideoID != 2 {
		t.Fatalf("fallback continuation=%#v embed_calls=%d lexical_cursor=%#v err=%v", second, embedder.calls, videoIndex.lastCursor, err)
	}
}

func TestHybridVideoSearchMergesSemanticOnlyAndPaginates(t *testing.T) {
	now := time.Date(2026, 8, 20, 13, 0, 0, 0, time.UTC)
	contract := searchHybridContract(t)
	vector := make([]float64, contract.Dimension)
	vector[0] = 1
	embedder := &semanticQueryEmbedderStub{vector: &domainembedding.MultimodalQueryVector{Contract: contract, Values: vector}}
	videoIndex := &videoIndexStub{items: []*domainsearch.VideoIndexItem{
		{ID: 1, Relevance: domainsearch.VideoRelevanceExactTitle, PublishedAt: now},
		{ID: 2, Relevance: domainsearch.VideoRelevanceTitleContains, PublishedAt: now.Add(-time.Minute)},
		{ID: 4, Relevance: domainsearch.VideoRelevanceDescriptionOnly, PublishedAt: now.Add(-90 * time.Second)},
	}}
	exact := &semanticVideoIndexStub{items: []domainembedding.MultimodalExactCandidate{
		{VideoID: 2, Similarity: 1, PublishedAt: now.Add(-time.Minute)},
		{VideoID: 3, Similarity: 0.9, PublishedAt: now.Add(-2 * time.Minute)},
	}}
	loader := &publicVideoLoaderStub{videos: map[int64]*domainvideo.Video{
		1: searchHybridVideo(1, now), 2: searchHybridVideo(2, now.Add(-time.Minute)),
		3: searchHybridVideo(3, now.Add(-2*time.Minute)),
	}}
	service := hybridSearchService(t, videoIndex, embedder, exact, loader, contract, now)
	page, err := service.SearchVideos(context.Background(), Request{Query: "cat", Limit: 2})
	if err != nil || len(page.Items) != 2 || !page.HasMore || page.NextCursor == "" ||
		page.Items[0].ID != 1 || page.Items[1].ID != 2 ||
		!reflect.DeepEqual(page.Items[1].RetrievalReasons, []string{HybridReasonLexical, HybridReasonSemantic}) {
		t.Fatalf("hybrid first page=%#v err=%v", page, err)
	}
	second, err := service.SearchVideos(context.Background(), Request{Query: "cat", Cursor: page.NextCursor, Limit: 2})
	if err != nil || len(second.Items) != 1 || second.Items[0].ID != 3 || second.HasMore {
		t.Fatalf("hybrid second page=%#v err=%v", second, err)
	}
	embedder.err = errors.New("provider unavailable")
	if _, err := service.SearchVideos(context.Background(), Request{Query: "cat", Cursor: page.NextCursor, Limit: 2}); !errors.Is(err, ErrSemanticContinuationUnavailable) {
		t.Fatalf("hybrid continuation error=%v", err)
	}
}

func TestHybridVideoSearchLeavesUserSearchUnchanged(t *testing.T) {
	contract := searchHybridContract(t)
	userIndex := &userIndexStub{items: []*domainsearch.UserIndexItem{{
		ID: 7, Nickname: "cat", UpdatedAt: time.Now(), Relevance: domainsearch.UserRelevanceExactNickname,
	}}}
	config, err := NewHybridVideoSearchConfig(contract, domainembedding.MultimodalHybridMergeVersionV1, domainsearch.MaxLimit+1, 1, 1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	service := New(&videoIndexStub{}, userIndex, WithHybridVideoSearch(
		&semanticQueryEmbedderStub{}, &semanticVideoIndexStub{}, &publicVideoLoaderStub{}, config,
	))
	page, err := service.SearchUsers(context.Background(), Request{Query: "cat", Limit: 1})
	if err != nil || len(page.Items) != 1 || userIndex.lastQuery != "cat" {
		t.Fatalf("user search changed: page=%#v err=%v", page, err)
	}
}

func hybridSearchService(
	t testing.TB,
	videos domainsearch.VideoSearchIndex,
	embedder SemanticQueryEmbedder,
	exact SemanticVideoIndex,
	loader PublicVideoLoader,
	contract domainembedding.MultimodalContractIdentity,
	now time.Time,
) *Service {
	t.Helper()
	config, err := NewHybridVideoSearchConfig(
		contract, domainembedding.MultimodalHybridMergeVersionV1, domainsearch.MaxLimit+1, 1, 1, time.Minute,
	)
	if err != nil {
		t.Fatal(err)
	}
	service := New(videos, &userIndexStub{}, WithHybridVideoSearch(embedder, exact, loader, config))
	service.hybrid.now = func() time.Time { return now }
	return service
}

func searchHybridContract(t testing.TB) domainembedding.MultimodalContractIdentity {
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

func searchHybridVideo(id int64, publishedAt time.Time) *domainvideo.Video {
	video := domainvideo.RestoreVideoWithMedia(
		id, id, "title", "description", "https://media.example/video", "https://media.example/cover",
		domainvideo.StatusPublished, domainvideo.VisibilityPublic, 0, 0, 0,
		&publishedAt, publishedAt, publishedAt, "key", id+100,
		domainmedia.MediaStatusReady, "", nil, id+200,
	)
	return video
}

func hybridCandidateIDs(candidates []*hybridVideoCandidate) []int64 {
	ids := make([]int64, 0, len(candidates))
	for _, candidate := range candidates {
		ids = append(ids, candidate.item.ID)
	}
	return ids
}
