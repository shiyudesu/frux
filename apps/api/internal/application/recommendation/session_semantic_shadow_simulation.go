package applicationrecommendation

import (
	"context"
	"strings"

	domainrecommendation "github.com/shiyudesu/frux/internal/domain/recommendation"
)

func (s *Service) SimulateSessionSemanticShadow(
	ctx context.Context,
	input SessionSemanticShadowSimulationRequest,
) (*SessionSemanticShadowSimulationResult, error) {
	if s == nil || input.ShadowPolicy == nil || !input.Request.valid() ||
		input.ComparisonLimit < 1 || input.ComparisonLimit > domainrecommendation.MaxPolicyPreRankCandidates ||
		input.TopK < 1 || input.TopK > input.ShadowPolicy.Config.PreRankPoolLimit {
		return nil, ErrSessionSemanticShadowSimulation
	}
	active := uniqueShadowCandidates(input.Request.ActiveCandidates, input.ComparisonLimit)
	semantic := uniqueShadowCandidates(input.SemanticCandidates, input.ComparisonLimit)
	semantic, err := s.visibleSessionSemanticShadowCandidates(ctx, semantic)
	if err != nil {
		return nil, ErrSessionSemanticShadowSimulation
	}

	providers := make(map[string][]*domainrecommendation.Candidate, len(input.ShadowPolicy.Config.RecallProviderOrder))
	for _, provider := range input.ShadowPolicy.Config.RecallProviderOrder {
		provider = strings.ToLower(strings.TrimSpace(provider))
		budget := input.ShadowPolicy.Config.RecallBudgets[provider]
		if provider == domainrecommendation.RecallProviderSemanticSession {
			providers[provider] = normalizeProviderCandidates(provider, semantic, budget)
			continue
		}
		sequence := make([]*domainrecommendation.Candidate, 0, len(active))
		for _, candidate := range active {
			if _, ok := providerCandidateScore(candidate, provider); ok {
				sequence = append(sequence, candidate.Clone())
			}
		}
		providers[provider] = normalizeProviderCandidates(provider, sequence, budget)
	}

	mixed, err := mixQuotaCandidates(input.ShadowPolicy.Config, providers)
	if err != nil || mixed == nil {
		return nil, ErrSessionSemanticShadowSimulation
	}
	ranked, err := s.rankCandidates(
		ctx,
		input.Request.UserID,
		input.Request.Context,
		cloneCandidates(mixed.Candidates),
		input.ShadowPolicy,
	)
	if err != nil {
		return nil, ErrSessionSemanticShadowSimulation
	}
	ranked = diversifyCandidates(ranked, input.ShadowPolicy.Config.Diversity)
	comparison := CompareSessionSemanticShadow(active, semantic, mixed.Candidates, ranked, input.TopK)
	if !comparison.valid() {
		return nil, ErrSessionSemanticShadowSimulation
	}
	return &SessionSemanticShadowSimulationResult{
		Mixed: cloneCandidates(mixed.Candidates), Ranked: cloneCandidates(ranked), Comparison: comparison,
	}, nil
}

func (s *Service) visibleSessionSemanticShadowCandidates(
	ctx context.Context,
	candidates []*domainrecommendation.Candidate,
) ([]*domainrecommendation.Candidate, error) {
	if len(candidates) == 0 {
		return []*domainrecommendation.Candidate{}, nil
	}
	if s.visibility == nil {
		return cloneCandidates(candidates), nil
	}
	visible, err := s.visibility.ListVisibleCandidates(ctx, candidateIDs(candidates))
	if err != nil {
		return nil, err
	}
	byID := make(map[int64]*domainrecommendation.Candidate, len(visible))
	for _, candidate := range visible {
		if candidate != nil && candidate.VideoID > 0 {
			byID[candidate.VideoID] = candidate
		}
	}
	output := make([]*domainrecommendation.Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		current := byID[candidate.VideoID]
		if current == nil {
			continue
		}
		cloned := candidate.Clone()
		cloned.AuthorID = current.AuthorID
		cloned.PublishedAt = current.PublishedAt
		cloned.HotScore = current.HotScore
		output = append(output, cloned)
	}
	return output, nil
}

var _ SessionSemanticShadowSimulator = (*Service)(nil)
