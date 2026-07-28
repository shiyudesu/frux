package applicationrecommendation

import (
	domainembedding "GCFeed/internal/domain/embedding"
	domainrecommendation "GCFeed/internal/domain/recommendation"
	"context"
	"errors"
	"math"
	"sort"
	"strings"
	"sync"
	"time"
)

const defaultRecallTotalPool = 500

// RecallProvider is deliberately application-level. Implementations receive
// only the bounded request facts needed to produce candidates.
type RecallProvider interface {
	Name() string
	Recall(ctx context.Context, request RecallRequest) ([]*domainrecommendation.Candidate, error)
}

type RecallRequest struct {
	UserID  int64
	Scene   string
	Context *domainrecommendation.RecommendationContext
	Budget  int
	Now     time.Time
}

type ProviderDegradation struct {
	Provider string
	Reason   string
}

type CandidateCatalog interface {
	ListFreshCandidates(ctx context.Context, limit int) ([]*domainrecommendation.Candidate, error)
	ListHotCandidates(ctx context.Context, limit int) ([]*domainrecommendation.Candidate, error)
	ListPublicCandidatesByAuthors(ctx context.Context, authorIDs []int64, limit int) ([]*domainrecommendation.Candidate, error)
	ListEmbeddingCandidates(ctx context.Context, model string, limit int) ([]*domainrecommendation.Candidate, error)
}

type CandidateVisibilityFilter interface {
	ListVisibleCandidates(ctx context.Context, videoIDs []int64) ([]*domainrecommendation.Candidate, error)
}

type FollowedAuthorSource interface {
	ListFollowedAuthorIDs(ctx context.Context, userID int64, limit int) ([]int64, error)
}

// VersionedEmbeddingSource keeps models replaceable. Callers request models in
// preference order and must include the supported hash model as a fallback.
type VersionedEmbeddingSource interface {
	LoadVectors(ctx context.Context, videoIDs []int64, model string) (map[int64][]float64, error)
}

type InterestVectorSource interface {
	LoadUserInterestVector(ctx context.Context, userID int64) ([]float64, bool, error)
}

type FreshContentProvider struct{ catalog CandidateCatalog }

func NewFreshContentProvider(catalog CandidateCatalog) *FreshContentProvider {
	return &FreshContentProvider{catalog: catalog}
}

func (p *FreshContentProvider) Name() string { return domainrecommendation.RecallProviderFresh }

func (p *FreshContentProvider) Recall(ctx context.Context, request RecallRequest) ([]*domainrecommendation.Candidate, error) {
	if p == nil || p.catalog == nil {
		return nil, errors.New("fresh candidate catalog unavailable")
	}
	candidates, err := p.catalog.ListFreshCandidates(ctx, request.Budget)
	return boundedRecallCandidates(annotateRecall(candidates, p.Name(), func(candidate *domainrecommendation.Candidate) float64 {
		return float64(candidate.PublishedAt.Unix())
	}), request.Budget), err
}

type HotContentProvider struct{ catalog CandidateCatalog }

func NewHotContentProvider(catalog CandidateCatalog) *HotContentProvider {
	return &HotContentProvider{catalog: catalog}
}

func (p *HotContentProvider) Name() string { return domainrecommendation.RecallProviderHot }

func (p *HotContentProvider) Recall(ctx context.Context, request RecallRequest) ([]*domainrecommendation.Candidate, error) {
	if p == nil || p.catalog == nil {
		return nil, errors.New("hot candidate catalog unavailable")
	}
	candidates, err := p.catalog.ListHotCandidates(ctx, request.Budget)
	return boundedRecallCandidates(annotateRecall(candidates, p.Name(), func(candidate *domainrecommendation.Candidate) float64 {
		return float64(candidate.HotScore)
	}), request.Budget), err
}

type ContentSimilarityProvider struct {
	catalog         CandidateCatalog
	vectors         VersionedEmbeddingSource
	interests       InterestVectorSource
	preferredModels []string
}

func NewContentSimilarityProvider(catalog CandidateCatalog, vectors VersionedEmbeddingSource, interests InterestVectorSource, preferredModels ...string) *ContentSimilarityProvider {
	return &ContentSimilarityProvider{
		catalog: catalog, vectors: vectors, interests: interests,
		preferredModels: normalizedEmbeddingModels(preferredModels),
	}
}

func (p *ContentSimilarityProvider) Name() string {
	return domainrecommendation.RecallProviderContentSimilarity
}

func (p *ContentSimilarityProvider) Recall(ctx context.Context, request RecallRequest) ([]*domainrecommendation.Candidate, error) {
	if p == nil || p.catalog == nil || p.vectors == nil || p.interests == nil {
		return nil, errors.New("content similarity dependencies unavailable")
	}
	interest, ok, err := p.interests.LoadUserInterestVector(ctx, request.UserID)
	if err != nil || !ok || len(interest) == 0 {
		return []*domainrecommendation.Candidate{}, err
	}
	return p.recallForVector(ctx, request, interest, nil)
}

func (p *ContentSimilarityProvider) recallForVector(ctx context.Context, request RecallRequest, target []float64, excluded map[int64]struct{}) ([]*domainrecommendation.Candidate, error) {
	output := make([]*domainrecommendation.Candidate, 0, request.Budget)
	seen := map[int64]struct{}{}
	for _, model := range p.preferredModels {
		if len(output) >= request.Budget {
			break
		}
		candidates, err := p.catalog.ListEmbeddingCandidates(ctx, model, request.Budget-len(output))
		if err != nil {
			return nil, err
		}
		ids := candidateIDs(candidates)
		vectors, err := p.vectors.LoadVectors(ctx, ids, model)
		if err != nil {
			return nil, err
		}
		for _, candidate := range candidates {
			if candidate == nil || candidate.VideoID <= 0 {
				continue
			}
			if _, skip := seen[candidate.VideoID]; skip {
				continue
			}
			if _, skip := excluded[candidate.VideoID]; skip {
				continue
			}
			similarity, err := domainembedding.CosineSimilarity(target, vectors[candidate.VideoID])
			if err != nil || similarity <= 0 {
				continue
			}
			seen[candidate.VideoID] = struct{}{}
			output = append(output, annotateCandidate(candidate, p.Name(), similarity))
			if len(output) == request.Budget {
				break
			}
		}
	}
	sortRecallCandidates(output)
	return output, nil
}

type FollowedAuthorProvider struct {
	follows FollowedAuthorSource
	catalog CandidateCatalog
}

func NewFollowedAuthorProvider(follows FollowedAuthorSource, catalog CandidateCatalog) *FollowedAuthorProvider {
	return &FollowedAuthorProvider{follows: follows, catalog: catalog}
}

func (p *FollowedAuthorProvider) Name() string {
	return domainrecommendation.RecallProviderFollowedAuthor
}

func (p *FollowedAuthorProvider) Recall(ctx context.Context, request RecallRequest) ([]*domainrecommendation.Candidate, error) {
	if p == nil || p.follows == nil || p.catalog == nil {
		return nil, errors.New("followed author dependencies unavailable")
	}
	authors, err := p.follows.ListFollowedAuthorIDs(ctx, request.UserID, request.Budget)
	if err != nil || len(authors) == 0 {
		return []*domainrecommendation.Candidate{}, err
	}
	candidates, err := p.catalog.ListPublicCandidatesByAuthors(ctx, authors, request.Budget)
	return boundedRecallCandidates(annotateRecall(candidates, p.Name(), func(candidate *domainrecommendation.Candidate) float64 {
		return float64(candidate.PublishedAt.Unix())
	}), request.Budget), err
}

type SessionContinuationProvider struct {
	catalog         CandidateCatalog
	vectors         VersionedEmbeddingSource
	preferredModels []string
}

func NewSessionContinuationProvider(catalog CandidateCatalog, vectors VersionedEmbeddingSource, preferredModels ...string) *SessionContinuationProvider {
	return &SessionContinuationProvider{
		catalog: catalog, vectors: vectors, preferredModels: normalizedEmbeddingModels(preferredModels),
	}
}

func (p *SessionContinuationProvider) Name() string {
	return domainrecommendation.RecallProviderSessionContinuation
}

func (p *SessionContinuationProvider) Recall(ctx context.Context, request RecallRequest) ([]*domainrecommendation.Candidate, error) {
	if p == nil || p.catalog == nil || p.vectors == nil {
		return nil, errors.New("session continuation dependencies unavailable")
	}
	seedIDs := sessionSeedIDs(request.Context)
	if len(seedIDs) == 0 {
		return []*domainrecommendation.Candidate{}, nil
	}
	excluded := make(map[int64]struct{}, len(seedIDs))
	for _, id := range seedIDs {
		excluded[id] = struct{}{}
	}
	for _, model := range p.preferredModels {
		vectors, err := p.vectors.LoadVectors(ctx, seedIDs, model)
		if err != nil {
			return nil, err
		}
		seed := averageVector(vectors, seedIDs)
		if len(seed) == 0 {
			continue
		}
		content := &ContentSimilarityProvider{
			catalog: p.catalog, vectors: p.vectors, preferredModels: []string{model},
		}
		candidates, err := content.recallForVector(ctx, request, seed, excluded)
		if err != nil {
			return nil, err
		}
		for _, candidate := range candidates {
			candidate.RecallReasons[0].Provider = p.Name()
			delete(candidate.SourceScores, domainrecommendation.RecallProviderContentSimilarity)
			candidate.SourceScores[p.Name()] = candidate.RecallReasons[0].Score
		}
		if len(candidates) > 0 {
			return candidates, nil
		}
	}
	return []*domainrecommendation.Candidate{}, nil
}

func normalizedEmbeddingModels(models []string) []string {
	seen := map[string]struct{}{}
	output := make([]string, 0, len(models)+1)
	for _, model := range append(models, domainembedding.HashNgramModel) {
		model = strings.TrimSpace(model)
		if model == "" {
			continue
		}
		if _, exists := seen[model]; exists {
			continue
		}
		seen[model] = struct{}{}
		output = append(output, model)
	}
	return output
}

func sessionSeedIDs(recommendationContext *domainrecommendation.RecommendationContext) []int64 {
	if recommendationContext == nil {
		return nil
	}
	ids := make([]int64, 0, len(recommendationContext.RecentVideoIDs)+1)
	seen := map[int64]struct{}{}
	if recommendationContext.CurrentVideoID > 0 {
		ids = append(ids, recommendationContext.CurrentVideoID)
		seen[recommendationContext.CurrentVideoID] = struct{}{}
	}
	for _, id := range recommendationContext.RecentVideoIDs {
		if id <= 0 {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids
}

func averageVector(vectors map[int64][]float64, ids []int64) []float64 {
	var result []float64
	count := 0
	for _, id := range ids {
		vector := vectors[id]
		if len(vector) == 0 {
			continue
		}
		if result == nil {
			result = make([]float64, len(vector))
		}
		if len(vector) != len(result) {
			continue
		}
		for index := range vector {
			result[index] += vector[index]
		}
		count++
	}
	if count == 0 {
		return nil
	}
	for index := range result {
		result[index] /= float64(count)
	}
	var magnitude float64
	for _, value := range result {
		magnitude += value * value
	}
	if magnitude == 0 {
		return nil
	}
	magnitude = math.Sqrt(magnitude)
	for index := range result {
		result[index] /= magnitude
	}
	return result
}

func annotateRecall(candidates []*domainrecommendation.Candidate, provider string, score func(*domainrecommendation.Candidate) float64) []*domainrecommendation.Candidate {
	output := make([]*domainrecommendation.Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate != nil {
			output = append(output, annotateCandidate(candidate, provider, score(candidate)))
		}
	}
	return output
}

func annotateCandidate(candidate *domainrecommendation.Candidate, provider string, score float64) *domainrecommendation.Candidate {
	cloned := candidate.Clone()
	if cloned == nil {
		return nil
	}
	if cloned.SourceScores == nil {
		cloned.SourceScores = map[string]float64{}
	}
	cloned.SourceScores[provider] = score
	cloned.RecallReasons = append(cloned.RecallReasons, domainrecommendation.RecallReason{Provider: provider, Score: score})
	return cloned
}

func candidateIDs(candidates []*domainrecommendation.Candidate) []int64 {
	ids := make([]int64, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate != nil && candidate.VideoID > 0 {
			ids = append(ids, candidate.VideoID)
		}
	}
	return ids
}

func sortRecallCandidates(candidates []*domainrecommendation.Candidate) {
	sort.SliceStable(candidates, func(i, j int) bool {
		left := candidates[i]
		right := candidates[j]
		leftScore := left.SourceScores[left.RecallReasons[len(left.RecallReasons)-1].Provider]
		rightScore := right.SourceScores[right.RecallReasons[len(right.RecallReasons)-1].Provider]
		if leftScore != rightScore {
			return leftScore > rightScore
		}
		if !left.PublishedAt.Equal(right.PublishedAt) {
			return left.PublishedAt.After(right.PublishedAt)
		}
		return left.VideoID > right.VideoID
	})
}

func boundedRecallCandidates(candidates []*domainrecommendation.Candidate, budget int) []*domainrecommendation.Candidate {
	if budget <= 0 || len(candidates) == 0 {
		return []*domainrecommendation.Candidate{}
	}
	if len(candidates) > budget {
		return candidates[:budget]
	}
	return candidates
}

type recallExecution struct {
	candidates []*domainrecommendation.Candidate
	degraded   []ProviderDegradation
	healthy    int
}

func (s *Service) recallCandidates(ctx context.Context, req *domainrecommendation.CandidateRequest, totalLimit int, policies ...*domainrecommendation.Policy) (*recallExecution, error) {
	if len(s.providers) == 0 {
		return nil, nil
	}
	type result struct {
		provider   string
		candidates []*domainrecommendation.Candidate
		err        error
	}
	results := make(chan result, len(s.providers))
	var group sync.WaitGroup
	for _, provider := range s.providers {
		provider := provider
		if provider == nil {
			continue
		}
		budget, deadline, ok := s.recallBudget(provider.Name(), policies...)
		if !ok {
			continue
		}
		if !s.acquireRecallSlot() {
			results <- result{provider: provider.Name(), err: errRecallProviderCapacity}
			continue
		}
		group.Add(1)
		go func() {
			defer group.Done()
			providerCtx, cancel := context.WithTimeout(ctx, deadline)
			defer cancel()
			done := make(chan result, 1)
			go func() {
				defer s.releaseRecallSlot()
				candidates, err := provider.Recall(providerCtx, RecallRequest{
					UserID: req.UserID, Scene: req.Scene, Context: req.Context, Budget: budget, Now: s.now(),
				})
				done <- result{provider: provider.Name(), candidates: candidates, err: err}
			}()
			select {
			case completed := <-done:
				results <- completed
			case <-providerCtx.Done():
				results <- result{provider: provider.Name(), err: providerCtx.Err()}
			}
		}()
	}
	group.Wait()
	close(results)

	execution := &recallExecution{}
	merged := map[int64]*domainrecommendation.Candidate{}
	for result := range results {
		if result.err != nil {
			reason := "error"
			if errors.Is(result.err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
				reason = "timeout"
			} else if errors.Is(result.err, errRecallProviderCapacity) {
				reason = "capacity"
			}
			execution.degraded = append(execution.degraded, ProviderDegradation{Provider: result.provider, Reason: reason})
			continue
		}
		execution.healthy++
		for _, candidate := range result.candidates {
			mergeRecalledCandidate(merged, candidate)
		}
	}
	if execution.healthy == 0 {
		for _, degradation := range execution.degraded {
			if degradation.Reason != "capacity" {
				return nil, ErrLoadRecommendationFailed
			}
		}
		if len(execution.degraded) == 0 {
			return nil, ErrLoadRecommendationFailed
		}
	}
	pool := make([]*domainrecommendation.Candidate, 0, len(merged))
	for _, candidate := range merged {
		pool = append(pool, candidate)
	}
	sort.Slice(pool, func(i, j int) bool {
		if !pool[i].PublishedAt.Equal(pool[j].PublishedAt) {
			return pool[i].PublishedAt.After(pool[j].PublishedAt)
		}
		return pool[i].VideoID > pool[j].VideoID
	})
	if s.visibility != nil && len(pool) > 0 {
		visible, err := s.visibility.ListVisibleCandidates(ctx, candidateIDs(pool))
		if err != nil {
			return nil, ErrLoadRecommendationFailed
		}
		byID := make(map[int64]*domainrecommendation.Candidate, len(visible))
		for _, candidate := range visible {
			if candidate != nil {
				byID[candidate.VideoID] = candidate
			}
		}
		filtered := pool[:0]
		for _, candidate := range pool {
			current := byID[candidate.VideoID]
			if current == nil {
				continue
			}
			candidate.AuthorID = current.AuthorID
			candidate.PublishedAt = current.PublishedAt
			candidate.HotScore = current.HotScore
			filtered = append(filtered, candidate)
		}
		pool = filtered
	}
	// Keep direct legacy callers safe while normal recommendation requests
	// defer suppression to the policy-aware ranker.
	if len(policies) == 0 && len(pool) > 0 {
		exposures, err := s.repo.ListRecentExposures(ctx, req.UserID, candidateIDs(pool), s.now().Add(-s.exposureWindow))
		if err != nil {
			return nil, ErrLoadRecommendationFailed
		}
		recent := make(map[int64]struct{}, len(exposures))
		for _, exposure := range exposures {
			if exposure != nil {
				recent[exposure.VideoID] = struct{}{}
			}
		}
		filtered := pool[:0]
		for _, candidate := range pool {
			if _, found := recent[candidate.VideoID]; !found {
				filtered = append(filtered, candidate)
			}
		}
		pool = filtered
	}
	if len(pool) > totalLimit {
		pool = pool[:totalLimit]
	}
	execution.candidates = pool
	return execution, nil
}

var errRecallProviderCapacity = errors.New("recall provider capacity exhausted")

func (s *Service) acquireRecallSlot() bool {
	if s == nil || s.recallSlots == nil {
		return true
	}
	select {
	case s.recallSlots <- struct{}{}:
		return true
	default:
		return false
	}
}

func (s *Service) releaseRecallSlot() {
	if s == nil || s.recallSlots == nil {
		return
	}
	<-s.recallSlots
}

func mergeRecalledCandidate(merged map[int64]*domainrecommendation.Candidate, candidate *domainrecommendation.Candidate) {
	if candidate == nil || candidate.VideoID <= 0 {
		return
	}
	existing := merged[candidate.VideoID]
	if existing == nil {
		merged[candidate.VideoID] = candidate.Clone()
		return
	}
	for _, reason := range candidate.RecallReasons {
		found := false
		for _, current := range existing.RecallReasons {
			if current.Provider == reason.Provider {
				found = true
				break
			}
		}
		if !found {
			existing.RecallReasons = append(existing.RecallReasons, reason)
		}
	}
	if existing.SourceScores == nil {
		existing.SourceScores = map[string]float64{}
	}
	for provider, score := range candidate.SourceScores {
		if current, exists := existing.SourceScores[provider]; !exists || score > current {
			existing.SourceScores[provider] = score
		}
	}
	sort.Slice(existing.RecallReasons, func(i, j int) bool {
		if existing.RecallReasons[i].Provider != existing.RecallReasons[j].Provider {
			return existing.RecallReasons[i].Provider < existing.RecallReasons[j].Provider
		}
		return existing.RecallReasons[i].Score > existing.RecallReasons[j].Score
	})
}
