package applicationrecommendation

import (
	"reflect"
	"testing"
	"time"

	domainrecommendation "github.com/shiyudesu/frux/internal/domain/recommendation"
)

func TestBuildSessionSemanticRolloutPolicyIsDeterministicAndImmutable(t *testing.T) {
	source := sessionSemanticRolloutBaseline(t)
	before := source.Clone()
	contract := sessionSemanticTestContract(t, "rollout-revision")
	options := SessionSemanticRolloutOptions{
		TargetVersion: 3, RolloutPercentage: 1, Contract: contract, Now: time.Unix(100, 0).UTC(),
	}
	first, err := BuildSessionSemanticRolloutPolicy(source, options)
	if err != nil {
		t.Fatal(err)
	}
	second, err := BuildSessionSemanticRolloutPolicy(source, options)
	if err != nil {
		t.Fatal(err)
	}
	if first.ConfigDigest != second.ConfigDigest || !reflect.DeepEqual(first.Policy.Config, second.Policy.Config) {
		t.Fatalf("first=%#v second=%#v", first, second)
	}
	if first.Policy.Enabled || first.Policy.Version != 3 || first.Policy.Config.RolloutPercentage != 1 ||
		first.Policy.Config.RecallBudgets[domainrecommendation.RecallProviderSemanticSession] != 50 ||
		first.Policy.Config.ProviderDeadlinesMS[domainrecommendation.RecallProviderSemanticSession] != 250 ||
		first.Policy.Config.FeatureWeights[domainrecommendation.FeatureSemanticSimilarity] != 0.25 ||
		first.Policy.Config.RecallProviderReservations[domainrecommendation.RecallProviderSemanticSession] != 10 ||
		first.Policy.Config.SessionSemantic.ContractKey != contract.Key() {
		t.Fatalf("plan=%#v", first)
	}
	if !reflect.DeepEqual(source, before) {
		t.Fatal("source policy was mutated")
	}
	initial := domainrecommendation.InitialRecommendationPolicyConfiguration()
	if initial.SessionSemantic != nil || initial.PreRankPoolLimit != 0 ||
		initial.FeatureWeights[domainrecommendation.FeatureSemanticSimilarity] != 0 {
		t.Fatal("bootstrap policy configuration changed")
	}
}

func TestBuildSessionSemanticRolloutPolicyRejectsUnsafeInputs(t *testing.T) {
	contract := sessionSemanticTestContract(t, "rollout-revision")
	source := sessionSemanticRolloutBaseline(t)
	for _, test := range []struct {
		name   string
		mutate func(*domainrecommendation.Policy, *SessionSemanticRolloutOptions)
	}{
		{name: "same version", mutate: func(_ *domainrecommendation.Policy, options *SessionSemanticRolloutOptions) {
			options.TargetVersion = 1
		}},
		{name: "zero rollout", mutate: func(_ *domainrecommendation.Policy, options *SessionSemanticRolloutOptions) {
			options.RolloutPercentage = 0
		}},
		{name: "wide rollout", mutate: func(_ *domainrecommendation.Policy, options *SessionSemanticRolloutOptions) {
			options.RolloutPercentage = 6
		}},
		{name: "wrong scene", mutate: func(policy *domainrecommendation.Policy, _ *SessionSemanticRolloutOptions) { policy.Scene = "feed" }},
		{name: "semantic source", mutate: func(policy *domainrecommendation.Policy, _ *SessionSemanticRolloutOptions) {
			policy.Config.RecallBudgets[domainrecommendation.RecallProviderSemanticSession] = 1
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			policy := source.Clone()
			options := SessionSemanticRolloutOptions{
				TargetVersion: 3, RolloutPercentage: 1, Contract: contract, Now: time.Unix(100, 0).UTC(),
			}
			test.mutate(policy, &options)
			if _, err := BuildSessionSemanticRolloutPolicy(policy, options); err != ErrInvalidSessionSemanticRollout {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestSessionSemanticRolloutCohortSelectsTargetAndFallsBack(t *testing.T) {
	contract := sessionSemanticTestContract(t, "rollout-revision")
	plan, err := BuildSessionSemanticRolloutPolicy(sessionSemanticRolloutBaseline(t), SessionSemanticRolloutOptions{
		TargetVersion: 3, RolloutPercentage: 1, Contract: contract, Now: time.Unix(100, 0).UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	target, err := domainrecommendation.NewPolicy("recommend", 3, true, plan.Policy.Config, time.Unix(100, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	baseline := sessionSemanticRolloutBaseline(t)
	var matched, fallback bool
	for index := 0; index < 10_000 && (!matched || !fallback); index++ {
		requestID := "selection-" + intString(index)
		selected := domainrecommendation.SelectPolicy([]*domainrecommendation.Policy{baseline, target}, 42, requestID)
		if selected == nil {
			t.Fatal("no policy selected despite 100% baseline")
		}
		if domainrecommendation.PolicyCohortPercent(42, "recommend", requestID) < 1 {
			matched = selected.Version == 3
		} else {
			fallback = selected.Version == 1
		}
	}
	if !matched || !fallback {
		t.Fatalf("matched=%v fallback=%v", matched, fallback)
	}
	summary := BuildSessionSemanticRolloutCohortSummary(1, 10_000)
	if summary.Samples != 10_000 || summary.Selected+summary.Fallback != 10_000 ||
		summary.Selected < 50 || summary.Selected > 150 || len(summary.Buckets) != 100 {
		t.Fatalf("summary=%#v", summary)
	}
}

func sessionSemanticRolloutBaseline(t testing.TB) *domainrecommendation.Policy {
	t.Helper()
	policies, err := domainrecommendation.InitialRecommendationPolicies(time.Unix(1, 0).UTC())
	if err != nil || len(policies) == 0 {
		t.Fatalf("initial policies=%#v error=%v", policies, err)
	}
	return policies[0]
}

func intString(value int) string {
	if value == 0 {
		return "0"
	}
	var buffer [20]byte
	index := len(buffer)
	for value > 0 {
		index--
		buffer[index] = byte('0' + value%10)
		value /= 10
	}
	return string(buffer[index:])
}
