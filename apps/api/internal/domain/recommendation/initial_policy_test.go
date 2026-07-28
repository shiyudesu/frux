package domainrecommendation

import (
	"testing"
	"time"
)

func TestInitialRecommendationPoliciesAreValidatedAndStaged(t *testing.T) {
	policies, err := InitialRecommendationPolicies(time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(policies) != 2 || policies[0].Version != 1 || policies[1].Version != 2 {
		t.Fatalf("unexpected bootstrap policies: %#v", policies)
	}
	if policies[0].Config.RolloutPercentage != 100 || policies[1].Config.RolloutPercentage != 5 {
		t.Fatalf("unexpected rollout configuration: v1=%d v2=%d", policies[0].Config.RolloutPercentage, policies[1].Config.RolloutPercentage)
	}
	if policies[0].Config.SnapshotTTLSeconds != 300 || policies[0].Config.SamplingRatePPM != 10_000 ||
		policies[0].Config.RetentionDays != 30 {
		t.Fatalf("unexpected operational defaults: %#v", policies[0].Config)
	}
}
