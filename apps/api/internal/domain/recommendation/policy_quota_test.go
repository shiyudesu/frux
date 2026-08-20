package domainrecommendation

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"
)

func quotaPolicyConfig() PolicyConfiguration {
	config := validPolicyConfig(100)
	config.RecallBudgets = map[string]int{
		RecallProviderFresh: 400,
		RecallProviderHot:   400,
	}
	config.ProviderDeadlinesMS = map[string]int{
		RecallProviderFresh: 100,
		RecallProviderHot:   100,
	}
	config.PreRankPoolLimit = MaxPolicyPreRankCandidates
	config.RecallProviderOrder = []string{RecallProviderFresh, RecallProviderHot}
	config.RecallProviderReservations = map[string]int{
		RecallProviderFresh: 100,
		RecallProviderHot:   100,
	}
	return config
}

func TestQuotaPolicyValidationAndDefensiveCopies(t *testing.T) {
	now := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	config := quotaPolicyConfig()
	config.RecallProviderOrder[0] = " Fresh "
	config.RecallProviderReservations[" Hot "] = config.RecallProviderReservations[RecallProviderHot]
	delete(config.RecallProviderReservations, RecallProviderHot)
	policy, err := NewPolicy("recommend", 3, false, config, now)
	if err != nil {
		t.Fatalf("valid quota policy was rejected: %v", err)
	}
	if policy.Enabled || policy.Config.PreRankPoolLimit != MaxPolicyPreRankCandidates ||
		!reflect.DeepEqual(policy.Config.RecallProviderOrder, []string{RecallProviderFresh, RecallProviderHot}) ||
		policy.Config.RecallProviderReservations[RecallProviderFresh] != 100 ||
		policy.Config.RecallProviderReservations[RecallProviderHot] != 100 {
		t.Fatalf("quota policy was not normalized: %#v", policy.Config)
	}

	config.RecallProviderOrder[0] = RecallProviderHot
	config.RecallProviderReservations[RecallProviderFresh] = 0
	if policy.Config.RecallProviderOrder[0] != RecallProviderFresh ||
		policy.Config.RecallProviderReservations[RecallProviderFresh] != 100 {
		t.Fatalf("quota policy aliased input: %#v", policy.Config)
	}
	cloned := policy.Clone()
	cloned.Config.RecallProviderOrder[0] = RecallProviderHot
	cloned.Config.RecallProviderReservations[RecallProviderFresh] = 0
	if policy.Config.RecallProviderOrder[0] != RecallProviderFresh ||
		policy.Config.RecallProviderReservations[RecallProviderFresh] != 100 {
		t.Fatalf("quota policy clone aliased original: %#v", policy.Config)
	}
}

func TestQuotaPolicyValidationRejectsInvalidConfigurations(t *testing.T) {
	now := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		mutate func(*PolicyConfiguration)
		want   error
	}{
		{name: "partial pool only", mutate: func(c *PolicyConfiguration) { c.RecallProviderOrder = nil; c.RecallProviderReservations = nil }, want: ErrInvalidPolicyConfiguration},
		{name: "missing order provider", mutate: func(c *PolicyConfiguration) { c.RecallProviderOrder = []string{RecallProviderFresh} }, want: ErrInvalidPolicyConfiguration},
		{name: "duplicate normalized order", mutate: func(c *PolicyConfiguration) { c.RecallProviderOrder = []string{" Fresh ", "fresh"} }, want: ErrInvalidPolicyConfiguration},
		{name: "unknown order provider", mutate: func(c *PolicyConfiguration) { c.RecallProviderOrder = []string{RecallProviderFresh, "unknown"} }, want: ErrUnknownRecallProvider},
		{name: "unselected order provider", mutate: func(c *PolicyConfiguration) {
			c.RecallProviderOrder = []string{RecallProviderFresh, RecallProviderContentSimilarity}
		}, want: ErrInvalidPolicyConfiguration},
		{name: "missing reservation", mutate: func(c *PolicyConfiguration) { delete(c.RecallProviderReservations, RecallProviderHot) }, want: ErrInvalidPolicyConfiguration},
		{name: "unknown reservation provider", mutate: func(c *PolicyConfiguration) {
			delete(c.RecallProviderReservations, RecallProviderHot)
			c.RecallProviderReservations["unknown"] = 1
		}, want: ErrUnknownRecallProvider},
		{name: "negative reservation", mutate: func(c *PolicyConfiguration) { c.RecallProviderReservations[RecallProviderFresh] = -1 }, want: ErrInvalidPolicyBound},
		{name: "reservation over budget", mutate: func(c *PolicyConfiguration) { c.RecallProviderReservations[RecallProviderFresh] = 401 }, want: ErrInvalidPolicyBound},
		{name: "reservation sum over pool", mutate: func(c *PolicyConfiguration) {
			c.RecallProviderReservations[RecallProviderFresh] = 300
			c.RecallProviderReservations[RecallProviderHot] = 300
		}, want: ErrInvalidPolicyBound},
		{name: "pool below minimum", mutate: func(c *PolicyConfiguration) { c.PreRankPoolLimit = MinPolicyPreRankCandidates - 1 }, want: ErrInvalidPolicyConfiguration},
		{name: "pool above maximum", mutate: func(c *PolicyConfiguration) { c.PreRankPoolLimit = MaxPolicyPreRankCandidates + 1 }, want: ErrInvalidPolicyConfiguration},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := quotaPolicyConfig()
			test.mutate(&config)
			if _, err := NewPolicy("recommend", 3, false, config, now); !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestOverBoundBudgetsRequireCompleteQuotaPolicy(t *testing.T) {
	now := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	config := quotaPolicyConfig()
	config.PreRankPoolLimit = 0
	config.RecallProviderOrder = nil
	config.RecallProviderReservations = nil
	if _, err := NewPolicy("recommend", 3, false, config, now); !errors.Is(err, ErrInvalidPolicyBound) {
		t.Fatalf("over-bound policy without quota error = %v, want %v", err, ErrInvalidPolicyBound)
	}
	config = quotaPolicyConfig()
	if _, err := NewPolicy("recommend", 3, false, config, now); err != nil {
		t.Fatalf("over-bound policy with quota was rejected: %v", err)
	}
}

func TestQuotaPolicyJSONRoundTrip(t *testing.T) {
	config := quotaPolicyConfig()
	encoded, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	var decoded PolicyConfiguration
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	policy := RestorePolicy(1, "recommend", 3, false, decoded, time.Unix(1, 0), time.Unix(2, 0))
	if policy == nil || policy.Config.PreRankPoolLimit != MaxPolicyPreRankCandidates ||
		!reflect.DeepEqual(policy.Config.RecallProviderOrder, config.RecallProviderOrder) ||
		!reflect.DeepEqual(policy.Config.RecallProviderReservations, config.RecallProviderReservations) {
		t.Fatalf("quota JSON round trip failed: %s %#v", encoded, policy)
	}
}
