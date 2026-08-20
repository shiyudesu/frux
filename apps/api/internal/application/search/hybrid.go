package applicationsearch

import (
	"context"
	"math"
	"sort"
	"strings"
	"time"

	domainembedding "github.com/shiyudesu/frux/internal/domain/embedding"
	domainsearch "github.com/shiyudesu/frux/internal/domain/search"
	domainvideo "github.com/shiyudesu/frux/internal/domain/video"
	inframetrics "github.com/shiyudesu/frux/internal/infra/metrics"
)

const (
	HybridReasonLexical  = "lexical"
	HybridReasonSemantic = "semantic"
)

type SemanticQueryEmbedder interface {
	EmbedPublicQuery(context.Context, string) (*domainembedding.MultimodalQueryVector, error)
}

type SemanticVideoIndex interface {
	ExactMultimodalSearch(context.Context, domainembedding.MultimodalContractIdentity, []float64, []int64, int) ([]domainembedding.MultimodalExactCandidate, error)
}

type PublicVideoLoader interface {
	BatchGetReadable(context.Context, int64, []int64, bool) (map[int64]*domainvideo.Video, error)
}

type HybridVideoSearchConfig struct {
	Contract            domainembedding.MultimodalContractIdentity
	Version             string
	PoolLimit           int
	LexicalReservation  int
	SemanticReservation int
	CursorTTL           time.Duration
}

type hybridVideoSearch struct {
	embedder SemanticQueryEmbedder
	exact    SemanticVideoIndex
	loader   PublicVideoLoader
	config   HybridVideoSearchConfig
	now      func() time.Time
}

type hybridVideoCandidate struct {
	item          *domainsearch.VideoIndexItem
	score         float64
	lexicalRank   int
	semanticRank  int
	semanticScore float64
	reasons       []string
}

func NewHybridVideoSearchConfig(
	contract domainembedding.MultimodalContractIdentity,
	version string,
	poolLimit int,
	lexicalReservation int,
	semanticReservation int,
	cursorTTL time.Duration,
) (HybridVideoSearchConfig, error) {
	validated, err := domainembedding.NewMultimodalContractIdentity(
		contract.ProviderAlias, contract.ModelAlias, contract.RevisionAlias, contract.Dimension,
		contract.TextCanonicalizer, contract.FrameSamplingPolicy,
		contract.ImagePreprocessingPolicy, contract.FusionPolicy,
	)
	version = strings.ToLower(strings.TrimSpace(version))
	if err != nil || !validated.Equal(contract) || version != domainembedding.MultimodalHybridMergeVersionV1 ||
		poolLimit < domainsearch.MaxLimit+1 || poolLimit > 500 || lexicalReservation < 0 || semanticReservation < 0 ||
		lexicalReservation+semanticReservation > poolLimit || cursorTTL < time.Minute || cursorTTL > 24*time.Hour {
		return HybridVideoSearchConfig{}, ErrInvalidHybridSearchConfig
	}
	return HybridVideoSearchConfig{
		Contract: contract, Version: version, PoolLimit: poolLimit,
		LexicalReservation: lexicalReservation, SemanticReservation: semanticReservation,
		CursorTTL: cursorTTL,
	}, nil
}

func WithHybridVideoSearch(
	embedder SemanticQueryEmbedder,
	exact SemanticVideoIndex,
	loader PublicVideoLoader,
	config HybridVideoSearchConfig,
) Option {
	return func(service *Service) {
		if service == nil || embedder == nil || exact == nil || loader == nil || config.Version == "" {
			return
		}
		service.hybrid = &hybridVideoSearch{
			embedder: embedder, exact: exact, loader: loader, config: config,
			now: func() time.Time { return time.Now().UTC() },
		}
	}
}

func (s *Service) searchHybridVideos(ctx context.Context, query, cursorValue string, limit int) (*VideoPage, error) {
	h := s.hybrid
	cursor, err := DecodeHybridVideoCursor(
		cursorValue, query, h.config.Version, h.config.Contract.Key(), h.now(),
	)
	if err != nil {
		inframetrics.ObserveMultimodalHybrid(VideoRetrievalModeHybrid, "error")
		return nil, err
	}
	if cursor != nil && cursor.Mode == VideoRetrievalModeLexical {
		return s.searchLexicalFallback(ctx, query, cursor, limit, nil)
	}
	lexical, err := s.videos.SearchVideos(ctx, query, nil, h.config.PoolLimit)
	if err != nil {
		inframetrics.ObserveMultimodalHybrid(VideoRetrievalModeHybrid, "error")
		return nil, ErrSearchFailed
	}
	queryVector, embedErr := h.embedder.EmbedPublicQuery(ctx, query)
	if embedErr != nil {
		if cursor != nil && cursor.Mode == VideoRetrievalModeHybrid {
			inframetrics.ObserveMultimodalHybrid(VideoRetrievalModeHybrid, "retryable")
			return nil, ErrSemanticContinuationUnavailable
		}
		return s.searchLexicalFallback(ctx, query, nil, limit, lexical)
	}
	if queryVector == nil || !queryVector.Contract.Equal(h.config.Contract) {
		if cursor != nil {
			inframetrics.ObserveMultimodalHybrid(VideoRetrievalModeHybrid, "retryable")
			return nil, ErrSemanticContinuationUnavailable
		}
		return s.searchLexicalFallback(ctx, query, nil, limit, lexical)
	}
	semantic, err := h.exact.ExactMultimodalSearch(
		ctx, h.config.Contract, queryVector.Values, nil, h.config.PoolLimit,
	)
	if err != nil {
		if cursor != nil {
			inframetrics.ObserveMultimodalHybrid(VideoRetrievalModeHybrid, "retryable")
			return nil, ErrSemanticContinuationUnavailable
		}
		return s.searchLexicalFallback(ctx, query, nil, limit, lexical)
	}
	semanticIDs := make([]int64, 0, len(semantic))
	for _, candidate := range semantic {
		semanticIDs = append(semanticIDs, candidate.VideoID)
	}
	semanticVideos, err := h.loader.BatchGetReadable(ctx, 0, semanticIDs, true)
	if err != nil {
		inframetrics.ObserveMultimodalHybrid(VideoRetrievalModeHybrid, "error")
		return nil, ErrSearchFailed
	}
	mixed := mixHybridVideoCandidates(lexical, semantic, semanticVideos, h.config)
	if cursor != nil {
		filtered := mixed[:0]
		for _, candidate := range mixed {
			if hybridCandidateAfterCursor(candidate, cursor) {
				filtered = append(filtered, candidate)
			}
		}
		mixed = filtered
	}
	selectedIDs := make([]int64, 0, len(mixed))
	for _, candidate := range mixed {
		selectedIDs = append(selectedIDs, candidate.item.ID)
	}
	current, err := h.loader.BatchGetReadable(ctx, 0, selectedIDs, true)
	if err != nil {
		inframetrics.ObserveMultimodalHybrid(VideoRetrievalModeHybrid, "error")
		return nil, ErrSearchFailed
	}
	visible := mixed[:0]
	for _, candidate := range mixed {
		video := current[candidate.item.ID]
		item := videoIndexItemFromDomain(video)
		if item == nil {
			continue
		}
		candidate.item = item
		visible = append(visible, candidate)
	}
	mixed = visible
	for _, candidate := range mixed {
		contribution := "semantic_only"
		if candidate.lexicalRank >= 0 && candidate.semanticRank >= 0 {
			contribution = "overlap"
		} else if candidate.lexicalRank >= 0 {
			contribution = "lexical_only"
		}
		inframetrics.ObserveMultimodalHybridCandidates(contribution, 1)
	}
	if len(mixed) > limit+1 {
		mixed = mixed[:limit+1]
	}
	hasMore := len(mixed) > limit
	if hasMore {
		mixed = mixed[:limit]
	}
	page := &VideoPage{Items: make([]VideoResult, 0, len(mixed)), HasMore: hasMore}
	for _, candidate := range mixed {
		result := videoResultFromIndexItem(candidate.item)
		result.HybridScore = candidate.score
		result.RetrievalReasons = append([]string(nil), candidate.reasons...)
		page.Items = append(page.Items, result)
	}
	if hasMore && len(mixed) > 0 {
		last := mixed[len(mixed)-1]
		page.NextCursor = EncodeHybridVideoCursor(query, &HybridVideoCursor{
			Mode: VideoRetrievalModeHybrid, RankingVersion: h.config.Version,
			ContractKey: h.config.Contract.Key(), HybridScore: last.score,
			PublishedAt: last.item.PublishedAt, VideoID: last.item.ID,
			ExpiresAt: h.now().Add(h.config.CursorTTL),
		})
	}
	result := "success"
	if len(page.Items) == 0 {
		result = "empty"
	}
	inframetrics.ObserveMultimodalHybrid(VideoRetrievalModeHybrid, result)
	return page, nil
}

func (s *Service) searchLexicalFallback(
	ctx context.Context,
	query string,
	cursor *HybridVideoCursor,
	limit int,
	prefetched []*domainsearch.VideoIndexItem,
) (*VideoPage, error) {
	var lexicalCursor *domainsearch.VideoCursor
	if cursor != nil {
		lexicalCursor = &domainsearch.VideoCursor{
			Relevance: cursor.Relevance, PublishedAt: cursor.PublishedAt, VideoID: cursor.VideoID,
		}
	}
	items := prefetched
	var err error
	if items == nil {
		items, err = s.videos.SearchVideos(ctx, query, lexicalCursor, limit+1)
		if err != nil {
			inframetrics.ObserveMultimodalHybrid(VideoRetrievalModeLexical, "error")
			return nil, ErrSearchFailed
		}
	}
	if len(items) > limit+1 {
		items = items[:limit+1]
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	page := &VideoPage{Items: make([]VideoResult, 0, len(items)), HasMore: hasMore}
	for _, item := range items {
		if item == nil || item.ID <= 0 || item.PublishedAt.IsZero() || !domainsearch.ValidVideoRelevance(item.Relevance) {
			return nil, ErrSearchFailed
		}
		page.Items = append(page.Items, videoResultFromIndexItem(item))
	}
	if hasMore && len(items) > 0 {
		last := items[len(items)-1]
		page.NextCursor = EncodeHybridVideoCursor(query, &HybridVideoCursor{
			Mode: VideoRetrievalModeLexical, RankingVersion: s.hybrid.config.Version,
			Relevance: last.Relevance, PublishedAt: last.PublishedAt, VideoID: last.ID,
			ExpiresAt: s.hybrid.now().Add(s.hybrid.config.CursorTTL),
		})
	}
	result := "success"
	if cursor == nil {
		result = "fallback"
	}
	if len(page.Items) == 0 {
		result = "empty"
	}
	inframetrics.ObserveMultimodalHybrid(VideoRetrievalModeLexical, result)
	return page, nil
}

func mixHybridVideoCandidates(
	lexical []*domainsearch.VideoIndexItem,
	semantic []domainembedding.MultimodalExactCandidate,
	semanticVideos map[int64]*domainvideo.Video,
	config HybridVideoSearchConfig,
) []*hybridVideoCandidate {
	candidates := make(map[int64]*hybridVideoCandidate, len(lexical)+len(semantic))
	lexicalSequence := make([]int64, 0, len(lexical))
	semanticSequence := make([]int64, 0, len(semantic))
	for index, item := range lexical {
		if item == nil || item.ID <= 0 || item.PublishedAt.IsZero() || !domainsearch.ValidVideoRelevance(item.Relevance) {
			continue
		}
		candidate := candidates[item.ID]
		if candidate == nil {
			candidate = &hybridVideoCandidate{item: item, lexicalRank: -1, semanticRank: -1}
			candidates[item.ID] = candidate
		}
		if candidate.lexicalRank < 0 {
			candidate.lexicalRank = index
			candidate.reasons = append(candidate.reasons, HybridReasonLexical)
			lexicalSequence = append(lexicalSequence, item.ID)
		}
	}
	for index, semanticCandidate := range semantic {
		if semanticCandidate.VideoID <= 0 || semanticCandidate.Similarity <= 0 ||
			math.IsNaN(semanticCandidate.Similarity) || math.IsInf(semanticCandidate.Similarity, 0) {
			continue
		}
		item := videoIndexItemFromDomain(semanticVideos[semanticCandidate.VideoID])
		if item == nil {
			continue
		}
		candidate := candidates[item.ID]
		if candidate == nil {
			candidate = &hybridVideoCandidate{item: item, lexicalRank: -1, semanticRank: -1}
			candidates[item.ID] = candidate
		}
		if candidate.semanticRank < 0 {
			candidate.semanticRank = index
			candidate.semanticScore = semanticCandidate.Similarity
			candidate.reasons = append(candidate.reasons, HybridReasonSemantic)
			semanticSequence = append(semanticSequence, item.ID)
		}
	}
	selected := make([]*hybridVideoCandidate, 0, config.PoolLimit)
	selectedIDs := make(map[int64]struct{}, config.PoolLimit)
	representedLexical := make(map[int64]struct{}, config.LexicalReservation)
	representedSemantic := make(map[int64]struct{}, config.SemanticReservation)
	lexicalCursor, semanticCursor := 0, 0
	selectID := func(videoID int64) {
		candidate := candidates[videoID]
		if candidate == nil {
			return
		}
		if _, exists := selectedIDs[videoID]; !exists && len(selected) < config.PoolLimit {
			selectedIDs[videoID] = struct{}{}
			selected = append(selected, candidate)
		}
		if _, exists := selectedIDs[videoID]; exists {
			if candidate.lexicalRank >= 0 {
				representedLexical[videoID] = struct{}{}
			}
			if candidate.semanticRank >= 0 {
				representedSemantic[videoID] = struct{}{}
			}
		}
	}
	for len(selected) < config.PoolLimit &&
		(len(representedLexical) < config.LexicalReservation || len(representedSemantic) < config.SemanticReservation) {
		progressed := false
		if len(representedLexical) < config.LexicalReservation && lexicalCursor < len(lexicalSequence) {
			selectID(lexicalSequence[lexicalCursor])
			lexicalCursor++
			progressed = true
		}
		if len(representedSemantic) < config.SemanticReservation && semanticCursor < len(semanticSequence) {
			selectID(semanticSequence[semanticCursor])
			semanticCursor++
			progressed = true
		}
		if !progressed {
			break
		}
	}
	for len(selected) < config.PoolLimit {
		progressed := false
		if lexicalCursor < len(lexicalSequence) {
			selectID(lexicalSequence[lexicalCursor])
			lexicalCursor++
			progressed = true
		}
		if semanticCursor < len(semanticSequence) && len(selected) < config.PoolLimit {
			selectID(semanticSequence[semanticCursor])
			semanticCursor++
			progressed = true
		}
		if !progressed {
			break
		}
	}
	for _, candidate := range selected {
		if candidate.lexicalRank >= 0 {
			candidate.score += float64(5-candidate.item.Relevance) + 1/float64(60+candidate.lexicalRank+1)
		}
		if candidate.semanticRank >= 0 {
			candidate.score += candidate.semanticScore + 1/float64(60+candidate.semanticRank+1)
		}
		sort.Strings(candidate.reasons)
	}
	sort.SliceStable(selected, func(i, j int) bool {
		if selected[i].score != selected[j].score {
			return selected[i].score > selected[j].score
		}
		if !selected[i].item.PublishedAt.Equal(selected[j].item.PublishedAt) {
			return selected[i].item.PublishedAt.After(selected[j].item.PublishedAt)
		}
		return selected[i].item.ID > selected[j].item.ID
	})
	return selected
}

func hybridCandidateAfterCursor(candidate *hybridVideoCandidate, cursor *HybridVideoCursor) bool {
	if candidate.score != cursor.HybridScore {
		return candidate.score < cursor.HybridScore
	}
	if !candidate.item.PublishedAt.Equal(cursor.PublishedAt) {
		return candidate.item.PublishedAt.Before(cursor.PublishedAt)
	}
	return candidate.item.ID < cursor.VideoID
}
