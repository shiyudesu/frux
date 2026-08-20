package applicationsearch

import (
	"context"
	"errors"
	"math"
	"time"

	domainembedding "github.com/shiyudesu/frux/internal/domain/embedding"
	domainsearch "github.com/shiyudesu/frux/internal/domain/search"
	domainvideo "github.com/shiyudesu/frux/internal/domain/video"
)

type SimilarVideoRepository interface {
	FindMultimodalVectorFact(context.Context, int64, domainembedding.MultimodalContractIdentity) (*domainembedding.MultimodalVectorFact, error)
	ExactMultimodalSearch(context.Context, domainembedding.MultimodalContractIdentity, []float64, []int64, int) ([]domainembedding.MultimodalExactCandidate, error)
}

type SimilarVideoService struct {
	repository SimilarVideoRepository
	loader     PublicVideoLoader
	contract   domainembedding.MultimodalContractIdentity
	poolLimit  int
	cursorTTL  time.Duration
	now        func() time.Time
}

type SimilarVideoRequest struct {
	SourceVideoID int64
	Cursor        string
	Limit         int
}

type SimilarVideoPage struct {
	Items             []VideoResult
	NextCursor        string
	HasMore           bool
	SemanticAvailable bool
}

func NewSimilarVideoService(
	repository SimilarVideoRepository,
	loader PublicVideoLoader,
	contract domainembedding.MultimodalContractIdentity,
	poolLimit int,
	cursorTTL time.Duration,
) (*SimilarVideoService, error) {
	validated, err := domainembedding.NewMultimodalContractIdentity(
		contract.ProviderAlias, contract.ModelAlias, contract.RevisionAlias, contract.Dimension,
		contract.TextCanonicalizer, contract.FrameSamplingPolicy,
		contract.ImagePreprocessingPolicy, contract.FusionPolicy,
	)
	if repository == nil || loader == nil || err != nil || !validated.Equal(contract) ||
		poolLimit < domainsearch.MaxLimit+1 || poolLimit > 500 ||
		cursorTTL < time.Minute || cursorTTL > 24*time.Hour {
		return nil, ErrSemanticVideoUnavailable
	}
	return &SimilarVideoService{
		repository: repository, loader: loader, contract: contract,
		poolLimit: poolLimit, cursorTTL: cursorTTL,
		now: func() time.Time { return time.Now().UTC() },
	}, nil
}

func (s *SimilarVideoService) Search(ctx context.Context, request SimilarVideoRequest) (*SimilarVideoPage, error) {
	if s == nil || request.SourceVideoID <= 0 {
		return nil, domainvideo.ErrVideoNotFound
	}
	limit := request.Limit
	if limit == 0 {
		limit = DefaultLimit
	}
	if limit < 1 || limit > domainsearch.MaxLimit {
		return nil, ErrInvalidHybridSearchConfig
	}
	cursor, err := DecodeSimilarVideoCursor(
		request.Cursor, request.SourceVideoID, s.contract.Key(), s.now(),
	)
	if err != nil {
		return nil, err
	}
	source, err := s.loader.BatchGetReadable(ctx, 0, []int64{request.SourceVideoID}, true)
	if err != nil {
		return nil, ErrSearchFailed
	}
	if source[request.SourceVideoID] == nil {
		return nil, domainvideo.ErrVideoNotFound
	}
	fact, err := s.repository.FindMultimodalVectorFact(ctx, request.SourceVideoID, s.contract)
	if errors.Is(err, domainembedding.ErrMultimodalVectorFactNotFound) {
		if cursor != nil {
			return nil, ErrSemanticContinuationUnavailable
		}
		return &SimilarVideoPage{Items: []VideoResult{}, SemanticAvailable: false}, nil
	}
	if err != nil {
		return nil, ErrSemanticVideoUnavailable
	}
	candidates, err := s.repository.ExactMultimodalSearch(
		ctx, s.contract, fact.Values, []int64{request.SourceVideoID}, s.poolLimit,
	)
	if err != nil {
		return nil, ErrSemanticVideoUnavailable
	}
	if cursor != nil {
		filtered := candidates[:0]
		for _, candidate := range candidates {
			if similarCandidateAfterCursor(candidate, cursor) {
				filtered = append(filtered, candidate)
			}
		}
		candidates = filtered
	}
	videoIDs := make([]int64, 0, len(candidates))
	for _, candidate := range candidates {
		videoIDs = append(videoIDs, candidate.VideoID)
	}
	visible, err := s.loader.BatchGetReadable(ctx, 0, videoIDs, true)
	if err != nil {
		return nil, ErrSearchFailed
	}
	filtered := candidates[:0]
	for _, candidate := range candidates {
		video := visible[candidate.VideoID]
		if video == nil || video.PublishedAt == nil || !video.PublishedAt.Equal(candidate.PublishedAt) {
			continue
		}
		filtered = append(filtered, candidate)
	}
	candidates = filtered
	if len(candidates) > limit+1 {
		candidates = candidates[:limit+1]
	}
	hasMore := len(candidates) > limit
	if hasMore {
		candidates = candidates[:limit]
	}
	page := &SimilarVideoPage{
		Items: make([]VideoResult, 0, len(candidates)), HasMore: hasMore, SemanticAvailable: true,
	}
	for _, candidate := range candidates {
		item := videoIndexItemFromDomain(visible[candidate.VideoID])
		if item == nil {
			continue
		}
		result := videoResultFromIndexItem(item)
		result.HybridScore = candidate.Similarity
		result.RetrievalReasons = []string{HybridReasonSemantic}
		page.Items = append(page.Items, result)
	}
	if hasMore && len(candidates) > 0 {
		last := candidates[len(candidates)-1]
		page.NextCursor = EncodeSimilarVideoCursor(request.SourceVideoID, &SimilarVideoCursor{
			ContractKey: s.contract.Key(), Similarity: last.Similarity,
			PublishedAt: last.PublishedAt, VideoID: last.VideoID,
			ExpiresAt: s.now().Add(s.cursorTTL),
		})
	}
	return page, nil
}

func similarCandidateAfterCursor(candidate domainembedding.MultimodalExactCandidate, cursor *SimilarVideoCursor) bool {
	if math.Abs(candidate.Similarity-cursor.Similarity) > 1e-15 {
		return candidate.Similarity < cursor.Similarity
	}
	if !candidate.PublishedAt.Equal(cursor.PublishedAt) {
		return candidate.PublishedAt.Before(cursor.PublishedAt)
	}
	return candidate.VideoID < cursor.VideoID
}
