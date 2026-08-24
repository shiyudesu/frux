package applicationrecommendation

import (
	"context"
	"errors"
	"sort"
	"sync"
	"testing"
	"time"

	domainrecommendation "github.com/shiyudesu/frux/internal/domain/recommendation"
)

type sessionSemanticRolloutMemoryRepo struct {
	mu       sync.Mutex
	policies map[int]*domainrecommendation.Policy
}

func newSessionSemanticRolloutMemoryRepo(t testing.TB) *sessionSemanticRolloutMemoryRepo {
	t.Helper()
	policies, err := domainrecommendation.InitialRecommendationPolicies(time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	repo := &sessionSemanticRolloutMemoryRepo{policies: map[int]*domainrecommendation.Policy{}}
	for _, policy := range policies {
		repo.policies[policy.Version] = policy.Clone()
	}
	return repo
}

func (r *sessionSemanticRolloutMemoryRepo) CreatePolicy(_ context.Context, policy *domainrecommendation.Policy) (*domainrecommendation.Policy, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.policies[policy.Version] != nil {
		return nil, domainrecommendation.ErrInvalidPolicyVersion
	}
	r.policies[policy.Version] = policy.Clone()
	return policy.Clone(), nil
}

func (r *sessionSemanticRolloutMemoryRepo) ActivatePolicy(_ context.Context, _ string, version int) (*domainrecommendation.Policy, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	policy := r.policies[version]
	if policy == nil {
		return nil, domainrecommendation.ErrPolicyNotFound
	}
	policy.Enabled = true
	return policy.Clone(), nil
}

func (r *sessionSemanticRolloutMemoryRepo) RollbackPolicy(context.Context, string, int) (*domainrecommendation.Policy, error) {
	return nil, errors.New("broad rollback must not be used")
}

func (r *sessionSemanticRolloutMemoryRepo) DisablePolicy(_ context.Context, _ string, version int) (*domainrecommendation.Policy, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	policy := r.policies[version]
	if policy == nil {
		return nil, false, domainrecommendation.ErrPolicyNotFound
	}
	replayed := !policy.Enabled
	policy.Enabled = false
	return policy.Clone(), replayed, nil
}

func (r *sessionSemanticRolloutMemoryRepo) ListEnabledPolicies(ctx context.Context, scene string) ([]*domainrecommendation.Policy, error) {
	all, err := r.ListPolicies(ctx, scene)
	if err != nil {
		return nil, err
	}
	output := all[:0]
	for _, policy := range all {
		if policy.Enabled {
			output = append(output, policy)
		}
	}
	return output, nil
}

func (r *sessionSemanticRolloutMemoryRepo) ListPolicies(context.Context, string) ([]*domainrecommendation.Policy, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	output := make([]*domainrecommendation.Policy, 0, len(r.policies))
	for _, policy := range r.policies {
		output = append(output, policy.Clone())
	}
	sort.Slice(output, func(i, j int) bool { return output[i].Version > output[j].Version })
	return output, nil
}

func TestSessionSemanticRolloutServiceLifecycleAndExactKillSwitch(t *testing.T) {
	repo := newSessionSemanticRolloutMemoryRepo(t)
	service := NewSessionSemanticRolloutService(repo, func() time.Time { return time.Unix(100, 0).UTC() })
	input := SessionSemanticRolloutLifecycleInput{
		SourceVersion: 2, TargetVersion: 3, RolloutPercentage: 1,
		Contract: sessionSemanticTestContract(t, "rollout-service"), EvidenceAccepted: true, RuntimeReady: true,
	}
	state, err := service.Plan(context.Background(), input)
	if err != nil || state.TargetExists || !state.BaselineReady || state.Plan.SourceVersion != 2 {
		t.Fatalf("state=%#v error=%v", state, err)
	}
	created, err := service.Create(context.Background(), input)
	if err != nil || created.Replayed || created.Policy.Enabled || !created.State.TargetCompatible {
		t.Fatalf("created=%#v error=%v", created, err)
	}
	replayedCreate, err := service.Create(context.Background(), input)
	if err != nil || !replayedCreate.Replayed {
		t.Fatalf("replayed create=%#v error=%v", replayedCreate, err)
	}
	activated, err := service.Activate(context.Background(), input)
	if err != nil || activated.Replayed || !activated.Policy.Enabled {
		t.Fatalf("activated=%#v error=%v", activated, err)
	}
	replayedActivate, err := service.Activate(context.Background(), input)
	if err != nil || !replayedActivate.Replayed {
		t.Fatalf("replayed activate=%#v error=%v", replayedActivate, err)
	}

	enabled, err := repo.ListEnabledPolicies(context.Background(), "recommend")
	if err != nil {
		t.Fatal(err)
	}
	var matchedTarget, matchedBaseline bool
	for index := 0; index < 10_000 && (!matchedTarget || !matchedBaseline); index++ {
		requestID := "rollout-service-" + intString(index)
		selected := domainrecommendation.SelectPolicy(enabled, 42, requestID)
		if selected == nil {
			t.Fatal("enabled 100% baseline did not catch non-cohort request")
		}
		if domainrecommendation.PolicyCohortPercent(42, "recommend", requestID) < 1 {
			matchedTarget = selected.Version == 3
		} else if selected.Version == 1 || selected.Version == 2 {
			matchedBaseline = true
		}
	}
	if !matchedTarget || !matchedBaseline {
		t.Fatalf("target=%v baseline=%v", matchedTarget, matchedBaseline)
	}

	disabled, err := service.Disable(context.Background(), 3)
	if err != nil || disabled.Replayed || disabled.Policy.Enabled {
		t.Fatalf("disabled=%#v error=%v", disabled, err)
	}
	replayedDisable, err := service.Disable(context.Background(), 3)
	if err != nil || !replayedDisable.Replayed {
		t.Fatalf("replayed disable=%#v error=%v", replayedDisable, err)
	}
	policies, _ := repo.ListPolicies(context.Background(), "recommend")
	if len(policies) != 3 || !policyByVersion(policies, 1).Enabled || !policyByVersion(policies, 2).Enabled || policyByVersion(policies, 3).Enabled {
		t.Fatalf("policies=%#v", policies)
	}
}

func TestSessionSemanticRolloutServiceGatesAndConflicts(t *testing.T) {
	repo := newSessionSemanticRolloutMemoryRepo(t)
	service := NewSessionSemanticRolloutService(repo, func() time.Time { return time.Unix(100, 0).UTC() })
	input := SessionSemanticRolloutLifecycleInput{
		SourceVersion: 2, TargetVersion: 3, RolloutPercentage: 1,
		Contract: sessionSemanticTestContract(t, "rollout-service"),
	}
	if _, err := service.Create(context.Background(), input); !errors.Is(err, ErrSessionSemanticRolloutBlocked) {
		t.Fatalf("create error=%v", err)
	}
	input.EvidenceAccepted = true
	created, err := service.Create(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Activate(context.Background(), input); !errors.Is(err, ErrSessionSemanticRolloutBlocked) {
		t.Fatalf("activate error=%v", err)
	}
	if _, err := service.Disable(context.Background(), 2); !errors.Is(err, ErrSessionSemanticRolloutConflict) {
		t.Fatalf("baseline disable error=%v", err)
	}
	repo.mu.Lock()
	repo.policies[3].Config.FeatureWeights[domainrecommendation.FeatureSemanticSimilarity] = 0.5
	repo.mu.Unlock()
	if _, err := service.Create(context.Background(), input); !errors.Is(err, ErrSessionSemanticRolloutConflict) {
		t.Fatalf("conflicting create error=%v created=%#v", err, created)
	}
}

func TestSessionSemanticRolloutActivationRequiresFullBaseline(t *testing.T) {
	repo := newSessionSemanticRolloutMemoryRepo(t)
	repo.mu.Lock()
	repo.policies[1].Enabled = false
	repo.mu.Unlock()
	service := NewSessionSemanticRolloutService(repo, nil)
	input := SessionSemanticRolloutLifecycleInput{
		SourceVersion: 2, TargetVersion: 3, RolloutPercentage: 1,
		Contract: sessionSemanticTestContract(t, "rollout-service"), EvidenceAccepted: true, RuntimeReady: true,
	}
	if _, err := service.Create(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Activate(context.Background(), input); !errors.Is(err, ErrSessionSemanticRolloutBlocked) {
		t.Fatalf("activate error=%v", err)
	}
}

func TestEnsureFullSessionSemanticPolicyIsIdempotentAndRetiresStagedTargets(t *testing.T) {
	repo := newSessionSemanticRolloutMemoryRepo(t)
	service := NewSessionSemanticRolloutService(repo, func() time.Time { return time.Unix(100, 0).UTC() })
	stagedInput := SessionSemanticRolloutLifecycleInput{
		SourceVersion: 2, TargetVersion: 3, RolloutPercentage: 1,
		Contract:         sessionSemanticTestContract(t, "development-full"),
		EvidenceAccepted: true, RuntimeReady: true,
	}
	if _, err := service.Create(context.Background(), stagedInput); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Activate(context.Background(), stagedInput); err != nil {
		t.Fatal(err)
	}
	full, err := EnsureFullSessionSemanticPolicy(
		context.Background(), repo, stagedInput.Contract, func() time.Time { return time.Unix(200, 0).UTC() },
	)
	if err != nil || full == nil || full.Version != FullSessionSemanticPolicyVersion ||
		!full.Enabled || full.Config.RolloutPercentage != FullSessionSemanticRolloutPercentage {
		t.Fatalf("full=%#v error=%v", full, err)
	}
	policies, err := repo.ListPolicies(context.Background(), domainrecommendation.RecommendationRequestLogScene)
	if err != nil || len(policies) != 4 || policyByVersion(policies, 3).Enabled ||
		!policyByVersion(policies, 1).Enabled || !policyByVersion(policies, 2).Enabled {
		t.Fatalf("policies=%#v error=%v", policies, err)
	}
	for index := range 1_000 {
		selected := domainrecommendation.SelectPolicy(policies, int64(index+1), "development-full-"+intString(index))
		if selected == nil || selected.Version != FullSessionSemanticPolicyVersion {
			t.Fatalf("index=%d selected=%#v", index, selected)
		}
	}
	replayed, err := EnsureFullSessionSemanticPolicy(
		context.Background(), repo, stagedInput.Contract, func() time.Time { return time.Unix(300, 0).UTC() },
	)
	if err != nil || replayed == nil || replayed.Version != FullSessionSemanticPolicyVersion {
		t.Fatalf("replayed=%#v error=%v", replayed, err)
	}
	policies, _ = repo.ListPolicies(context.Background(), domainrecommendation.RecommendationRequestLogScene)
	if len(policies) != 4 {
		t.Fatalf("reconcile created duplicate policies: %#v", policies)
	}
}

func TestEnsureFullSessionSemanticPolicyRejectsConflictingV4(t *testing.T) {
	repo := newSessionSemanticRolloutMemoryRepo(t)
	contract := sessionSemanticTestContract(t, "development-conflict")
	plan, err := BuildSessionSemanticRolloutPolicy(
		repo.policies[2],
		SessionSemanticRolloutOptions{
			TargetVersion:     FullSessionSemanticPolicyVersion,
			RolloutPercentage: FullSessionSemanticRolloutPercentage,
			AllowFullRollout:  true,
			Contract:          contract,
			Now:               time.Unix(100, 0).UTC(),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	conflicting := plan.Policy.Clone()
	conflicting.Config.FeatureWeights[domainrecommendation.FeatureSemanticSimilarity] = 0.5
	repo.policies[FullSessionSemanticPolicyVersion] = conflicting
	if _, err := EnsureFullSessionSemanticPolicy(
		context.Background(), repo, contract, func() time.Time { return time.Unix(200, 0).UTC() },
	); !errors.Is(err, ErrSessionSemanticRolloutConflict) {
		t.Fatalf("error=%v", err)
	}
	if repo.policies[FullSessionSemanticPolicyVersion].Config.FeatureWeights[domainrecommendation.FeatureSemanticSimilarity] != 0.5 {
		t.Fatal("conflicting policy was overwritten")
	}
}
