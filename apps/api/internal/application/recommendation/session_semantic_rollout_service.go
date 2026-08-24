package applicationrecommendation

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"time"

	domainembedding "github.com/shiyudesu/frux/internal/domain/embedding"
	domainrecommendation "github.com/shiyudesu/frux/internal/domain/recommendation"
)

const DevelopmentSessionSemanticSourceVersion = 2
const DevelopmentSessionSemanticPolicyVersion = 4

type SessionSemanticRolloutLifecycleInput struct {
	SourceVersion     int
	TargetVersion     int
	RolloutPercentage int
	AllowFullRollout  bool
	Contract          domainembedding.MultimodalContractIdentity
	EvidenceAccepted  bool
	RuntimeReady      bool
}

type SessionSemanticRolloutState struct {
	Plan             *SessionSemanticRolloutPlan
	Source           *domainrecommendation.Policy
	Target           *domainrecommendation.Policy
	TargetExists     bool
	TargetCompatible bool
	BaselineReady    bool
}

func (s *SessionSemanticRolloutState) Clone() *SessionSemanticRolloutState {
	if s == nil {
		return nil
	}
	return &SessionSemanticRolloutState{
		Plan: s.Plan.Clone(), Source: s.Source.Clone(), Target: s.Target.Clone(),
		TargetExists: s.TargetExists, TargetCompatible: s.TargetCompatible, BaselineReady: s.BaselineReady,
	}
}

type SessionSemanticRolloutMutation struct {
	State    *SessionSemanticRolloutState
	Policy   *domainrecommendation.Policy
	Replayed bool
}

type SessionSemanticRolloutService struct {
	repo domainrecommendation.RolloutPolicyRepository
	now  func() time.Time
}

func NewSessionSemanticRolloutService(
	repo domainrecommendation.RolloutPolicyRepository,
	now func() time.Time,
) *SessionSemanticRolloutService {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &SessionSemanticRolloutService{repo: repo, now: now}
}

// EnsureDevelopmentFullSessionSemanticPolicy makes the accepted Session Semantic policy the
// effective policy for every local/test request without rewriting an existing policy version.
func EnsureDevelopmentFullSessionSemanticPolicy(
	ctx context.Context,
	repo domainrecommendation.RolloutPolicyRepository,
	contract domainembedding.MultimodalContractIdentity,
	now func() time.Time,
) (*domainrecommendation.Policy, error) {
	service := NewSessionSemanticRolloutService(repo, now)
	input := SessionSemanticRolloutLifecycleInput{
		SourceVersion:     DevelopmentSessionSemanticSourceVersion,
		TargetVersion:     DevelopmentSessionSemanticPolicyVersion,
		RolloutPercentage: FullSessionSemanticRolloutPercentage,
		AllowFullRollout:  true,
		Contract:          contract,
		EvidenceAccepted:  true,
		RuntimeReady:      true,
	}
	if _, err := service.Create(ctx, input); err != nil {
		return nil, err
	}
	activated, err := service.Activate(ctx, input)
	if err != nil {
		return nil, err
	}
	if activated == nil || activated.Policy == nil {
		return nil, ErrSessionSemanticRolloutConflict
	}
	policies, err := repo.ListPolicies(ctx, domainrecommendation.RecommendationRequestLogScene)
	if err != nil {
		return nil, ErrRecommendationPolicyRepositoryUnavailable
	}
	for _, policy := range policies {
		if policy == nil || policy.Version == DevelopmentSessionSemanticPolicyVersion ||
			!policy.Enabled || !IsSessionSemanticRolloutPolicy(policy) {
			continue
		}
		if _, _, err := repo.DisablePolicy(
			ctx, domainrecommendation.RecommendationRequestLogScene, policy.Version,
		); err != nil {
			return nil, err
		}
	}
	return activated.Policy.Clone(), nil
}

func (s *SessionSemanticRolloutService) Plan(
	ctx context.Context,
	input SessionSemanticRolloutLifecycleInput,
) (*SessionSemanticRolloutState, error) {
	if s == nil || s.repo == nil {
		return nil, ErrRecommendationPolicyRepositoryUnavailable
	}
	policies, err := s.repo.ListPolicies(ctx, domainrecommendation.RecommendationRequestLogScene)
	if err != nil {
		return nil, ErrRecommendationPolicyRepositoryUnavailable
	}
	source := policyByVersion(policies, input.SourceVersion)
	if source == nil {
		return nil, domainrecommendation.ErrPolicyNotFound
	}
	plan, err := BuildSessionSemanticRolloutPolicy(source, SessionSemanticRolloutOptions{
		TargetVersion: input.TargetVersion, RolloutPercentage: input.RolloutPercentage,
		AllowFullRollout: input.AllowFullRollout, Contract: input.Contract, Now: s.now().UTC(),
	})
	if err != nil {
		return nil, err
	}
	target := policyByVersion(policies, input.TargetVersion)
	compatible := false
	if target != nil {
		digest, digestErr := RecommendationPolicyConfigDigest(target.Config)
		compatible = digestErr == nil && digest == plan.ConfigDigest && IsSessionSemanticRolloutPolicy(target)
	}
	return &SessionSemanticRolloutState{
		Plan: plan, Source: source.Clone(), Target: target.Clone(), TargetExists: target != nil,
		TargetCompatible: compatible,
		BaselineReady:    hasEnabledFullBaseline(policies, input.TargetVersion),
	}, nil
}

func (s *SessionSemanticRolloutService) Create(
	ctx context.Context,
	input SessionSemanticRolloutLifecycleInput,
) (*SessionSemanticRolloutMutation, error) {
	if !input.EvidenceAccepted {
		return nil, ErrSessionSemanticRolloutBlocked
	}
	state, err := s.Plan(ctx, input)
	if err != nil {
		return nil, err
	}
	if state.TargetExists {
		if !state.TargetCompatible {
			return nil, ErrSessionSemanticRolloutConflict
		}
		return &SessionSemanticRolloutMutation{State: state, Policy: state.Target.Clone(), Replayed: true}, nil
	}
	created, err := s.repo.CreatePolicy(ctx, state.Plan.Policy.Clone())
	if err != nil {
		if errors.Is(err, domainrecommendation.ErrInvalidPolicyVersion) {
			reloaded, reloadErr := s.Plan(ctx, input)
			if reloadErr == nil && reloaded.TargetExists && reloaded.TargetCompatible {
				return &SessionSemanticRolloutMutation{State: reloaded, Policy: reloaded.Target.Clone(), Replayed: true}, nil
			}
		}
		return nil, err
	}
	if created == nil || created.Enabled || !IsSessionSemanticRolloutPolicy(created) {
		return nil, ErrSessionSemanticRolloutConflict
	}
	state.Target = created.Clone()
	state.TargetExists = true
	state.TargetCompatible = true
	return &SessionSemanticRolloutMutation{State: state, Policy: created.Clone()}, nil
}

func (s *SessionSemanticRolloutService) Activate(
	ctx context.Context,
	input SessionSemanticRolloutLifecycleInput,
) (*SessionSemanticRolloutMutation, error) {
	if !input.EvidenceAccepted || !input.RuntimeReady {
		return nil, ErrSessionSemanticRolloutBlocked
	}
	state, err := s.Plan(ctx, input)
	if err != nil {
		return nil, err
	}
	if !state.TargetExists || !state.TargetCompatible {
		return nil, ErrSessionSemanticRolloutConflict
	}
	if !state.BaselineReady {
		return nil, ErrSessionSemanticRolloutBlocked
	}
	if state.Target.Enabled {
		return &SessionSemanticRolloutMutation{State: state, Policy: state.Target.Clone(), Replayed: true}, nil
	}
	activated, err := s.repo.ActivatePolicy(ctx, domainrecommendation.RecommendationRequestLogScene, input.TargetVersion)
	if err != nil {
		return nil, err
	}
	if activated == nil || !activated.Enabled || !IsSessionSemanticRolloutPolicy(activated) {
		return nil, ErrSessionSemanticRolloutConflict
	}
	state.Target = activated.Clone()
	return &SessionSemanticRolloutMutation{State: state, Policy: activated.Clone()}, nil
}

func (s *SessionSemanticRolloutService) Status(
	ctx context.Context,
	input SessionSemanticRolloutLifecycleInput,
) (*SessionSemanticRolloutState, error) {
	return s.Plan(ctx, input)
}

func (s *SessionSemanticRolloutService) Disable(
	ctx context.Context,
	targetVersion int,
) (*SessionSemanticRolloutMutation, error) {
	if s == nil || s.repo == nil || targetVersion <= 0 {
		return nil, ErrInvalidSessionSemanticRollout
	}
	policies, err := s.repo.ListPolicies(ctx, domainrecommendation.RecommendationRequestLogScene)
	if err != nil {
		return nil, ErrRecommendationPolicyRepositoryUnavailable
	}
	target := policyByVersion(policies, targetVersion)
	if target == nil {
		return nil, domainrecommendation.ErrPolicyNotFound
	}
	if !IsSessionSemanticRolloutPolicy(target) {
		return nil, ErrSessionSemanticRolloutConflict
	}
	disabled, replayed, err := s.repo.DisablePolicy(
		ctx, domainrecommendation.RecommendationRequestLogScene, targetVersion,
	)
	if err != nil {
		return nil, err
	}
	if disabled == nil || disabled.Enabled || !IsSessionSemanticRolloutPolicy(disabled) {
		return nil, ErrSessionSemanticRolloutConflict
	}
	return &SessionSemanticRolloutMutation{
		State: &SessionSemanticRolloutState{
			Target: disabled.Clone(), TargetExists: true, TargetCompatible: true,
			BaselineReady: hasEnabledFullBaseline(policies, targetVersion),
		},
		Policy: disabled.Clone(), Replayed: replayed,
	}, nil
}

func IsSessionSemanticRolloutPolicy(policy *domainrecommendation.Policy) bool {
	if policy == nil || strings.ToLower(strings.TrimSpace(policy.Scene)) != domainrecommendation.RecommendationRequestLogScene ||
		!validPersistedSessionSemanticRolloutPercentage(policy.Config.RolloutPercentage) ||
		policy.Config.PreRankPoolLimit != domainrecommendation.MaxPolicyPreRankCandidates ||
		policy.Config.RecallBudgets[domainrecommendation.RecallProviderSemanticSession] != 50 ||
		policy.Config.ProviderDeadlinesMS[domainrecommendation.RecallProviderSemanticSession] != 250 ||
		policy.Config.FeatureWeights[domainrecommendation.FeatureSemanticSimilarity] != 0.25 ||
		policy.Config.SamplingRatePPM != domainrecommendation.MaxSamplingRatePPM ||
		policy.Config.SessionSemantic == nil ||
		policy.Config.SessionSemantic.BuilderVersion != domainrecommendation.SessionSemanticBuilderV1 ||
		policy.Config.SessionSemantic.LookbackSeconds != 24*60*60 ||
		policy.Config.SessionSemantic.MaxSeeds != domainrecommendation.MaxSessionSemanticSeeds ||
		policy.Config.SessionSemantic.MinPositiveSignals != 2 ||
		policy.Config.SessionSemantic.MinConfidence != 0.25 ||
		policy.Config.RecallProviderReservations[domainrecommendation.RecallProviderSemanticSession] != 10 {
		return false
	}
	wantOrder := []string{
		domainrecommendation.RecallProviderFresh,
		domainrecommendation.RecallProviderHot,
		domainrecommendation.RecallProviderContentSimilarity,
		domainrecommendation.RecallProviderFollowedAuthor,
		domainrecommendation.RecallProviderSessionContinuation,
		domainrecommendation.RecallProviderSemanticSession,
	}
	if !reflect.DeepEqual(policy.Config.RecallProviderOrder, wantOrder) {
		return false
	}
	for _, provider := range wantOrder[:len(wantOrder)-1] {
		if policy.Config.RecallProviderReservations[provider] != 0 {
			return false
		}
	}
	return true
}

func validPersistedSessionSemanticRolloutPercentage(percentage int) bool {
	return validSessionSemanticRolloutPercentage(percentage, true)
}

func policyByVersion(policies []*domainrecommendation.Policy, version int) *domainrecommendation.Policy {
	for _, policy := range policies {
		if policy != nil && policy.Version == version {
			return policy.Clone()
		}
	}
	return nil
}

func hasEnabledFullBaseline(policies []*domainrecommendation.Policy, targetVersion int) bool {
	for _, policy := range policies {
		if policy == nil || policy.Version == targetVersion || !policy.Enabled ||
			policy.Config.RolloutPercentage != 100 || IsSessionSemanticRolloutPolicy(policy) {
			continue
		}
		return true
	}
	return false
}
