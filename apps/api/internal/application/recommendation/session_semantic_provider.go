package applicationrecommendation

import (
	"context"
	"errors"
	"math"
	"strings"
	"time"

	domainembedding "github.com/shiyudesu/frux/internal/domain/embedding"
	domainrecommendation "github.com/shiyudesu/frux/internal/domain/recommendation"
	inframetrics "github.com/shiyudesu/frux/internal/infra/metrics"
)

type SessionSemanticInterestBuilder interface {
	Build(context.Context, SessionSemanticBuildRequest) (*SessionSemanticInterest, error)
}

type SessionSemanticExactIndex interface {
	ExactMultimodalSearch(
		context.Context,
		domainembedding.MultimodalContractIdentity,
		[]float64,
		[]int64,
		int,
	) ([]domainembedding.MultimodalExactCandidate, error)
}

type SemanticSessionProvider struct {
	builder  SessionSemanticInterestBuilder
	exact    SessionSemanticExactIndex
	contract domainembedding.MultimodalContractIdentity
}

func NewSemanticSessionProvider(
	builder SessionSemanticInterestBuilder,
	exact SessionSemanticExactIndex,
	contract domainembedding.MultimodalContractIdentity,
) (*SemanticSessionProvider, error) {
	validated, err := domainembedding.NewMultimodalContractIdentity(
		contract.ProviderAlias, contract.ModelAlias, contract.RevisionAlias, contract.Dimension,
		contract.TextCanonicalizer, contract.FrameSamplingPolicy,
		contract.ImagePreprocessingPolicy, contract.FusionPolicy,
	)
	if builder == nil || exact == nil || err != nil || !validated.Equal(contract) {
		return nil, ErrSessionSemanticUnavailable
	}
	return &SemanticSessionProvider{builder: builder, exact: exact, contract: contract}, nil
}

func (*SemanticSessionProvider) Name() string {
	return domainrecommendation.RecallProviderSemanticSession
}

func (p *SemanticSessionProvider) Recall(
	ctx context.Context,
	request RecallRequest,
) ([]*domainrecommendation.Candidate, error) {
	candidates, _, err := p.RecallWithSessionSemanticEvidence(ctx, request)
	return candidates, err
}

func (p *SemanticSessionProvider) RecallWithSessionSemanticEvidence(
	ctx context.Context,
	request RecallRequest,
) ([]*domainrecommendation.Candidate, *domainrecommendation.SessionSemanticEvidence, error) {
	if p == nil || p.builder == nil || p.exact == nil || request.Policy == nil || request.Policy.Config.SessionSemantic == nil {
		return nil, nil, ErrSessionSemanticUnavailable
	}
	if request.Policy.Config.SessionSemantic.ContractKey != p.contract.Key() {
		evidence := sessionSemanticFailureEvidence(
			request.Policy, "contract_mismatch",
		)
		observeSessionSemantic("builder", evidence, 0, 0)
		return []*domainrecommendation.Candidate{}, evidence, nil
	}
	builderStarted := time.Now()
	interest, err := p.builder.Build(ctx, SessionSemanticBuildRequest{
		UserID: request.UserID, Context: request.Context,
		Policy: request.Policy.Config.SessionSemantic, Contract: p.contract,
		Budget: request.Budget, Now: request.Now,
	})
	if err != nil {
		observeSessionSemantic("builder", nil, 0, time.Since(builderStarted))
		return nil, nil, err
	}
	if interest == nil || interest.Evidence == nil {
		observeSessionSemantic("builder", nil, 0, time.Since(builderStarted))
		return nil, nil, ErrSessionSemanticUnavailable
	}
	observeSessionSemantic("builder", interest.Evidence, 0, time.Since(builderStarted))
	if !interest.Available() {
		observeSessionSemantic("provider", interest.Evidence, 0, 0)
		return []*domainrecommendation.Candidate{}, interest.Evidence.Clone(), nil
	}
	providerStarted := time.Now()
	exact, err := p.exact.ExactMultimodalSearch(
		ctx, p.contract, interest.Vector, interest.Exclusions, interest.OutputLimit,
	)
	if err != nil {
		reason := "error"
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			reason = "timeout"
		}
		evidence := sessionSemanticFailureEvidence(request.Policy, reason)
		observeSessionSemantic("provider", evidence, 0, time.Since(providerStarted))
		return nil, evidence, err
	}
	candidates := make([]*domainrecommendation.Candidate, 0, min(interest.OutputLimit, len(exact)))
	for _, result := range exact {
		if result.VideoID <= 0 || result.PublishedAt.IsZero() || math.IsNaN(result.Similarity) ||
			math.IsInf(result.Similarity, 0) || result.Similarity <= 0 {
			continue
		}
		score := boundedUnit(result.Similarity * interest.Confidence)
		if score <= 0 {
			continue
		}
		candidate := domainrecommendation.RestoreCandidate(
			result.VideoID, 0, 0, 0, 0, 0, "", result.PublishedAt,
		)
		candidates = append(candidates, annotateCandidate(candidate, p.Name(), score))
		if len(candidates) == interest.OutputLimit {
			break
		}
	}
	sortRecallCandidates(candidates)
	observeSessionSemantic("provider", interest.Evidence, len(candidates), time.Since(providerStarted))
	return candidates, interest.Evidence.Clone(), nil
}

func observeSessionSemantic(
	stage string,
	evidence *domainrecommendation.SessionSemanticEvidence,
	candidates int,
	duration time.Duration,
) {
	if evidence == nil {
		inframetrics.ObserveRecommendationSessionSemantic(
			stage, "error", "none", 0, 0, 0, 0, 0, 0, candidates, duration,
		)
		return
	}
	inframetrics.ObserveRecommendationSessionSemantic(
		stage, string(evidence.Result), string(evidence.ConfidenceBand), evidence.Confidence,
		evidence.EligibleCount, evidence.PositiveCount, evidence.NegativeCount,
		evidence.CompatibleCount, evidence.ExcludedCount, candidates, duration,
	)
}

func sessionSemanticFailureEvidence(
	policy *domainrecommendation.Policy,
	reason string,
) *domainrecommendation.SessionSemanticEvidence {
	if policy == nil || policy.Config.SessionSemantic == nil {
		return nil
	}
	result := domainrecommendation.SessionSemanticResultUnavailable
	switch strings.ToLower(strings.TrimSpace(reason)) {
	case "timeout":
		result = domainrecommendation.SessionSemanticResultTimeout
	case "contract_mismatch":
		result = domainrecommendation.SessionSemanticResultContractMismatch
	}
	evidence, err := domainrecommendation.NewSessionSemanticEvidence(domainrecommendation.SessionSemanticEvidence{
		BuilderVersion: policy.Config.SessionSemantic.BuilderVersion,
		ContractKey:    policy.Config.SessionSemantic.ContractKey,
		Result:         result, ConfidenceBand: domainrecommendation.SessionSemanticConfidenceNone,
	})
	if err != nil {
		return nil
	}
	return evidence
}

var _ RecallProvider = (*SemanticSessionProvider)(nil)
var _ SessionSemanticEvidenceProvider = (*SemanticSessionProvider)(nil)
