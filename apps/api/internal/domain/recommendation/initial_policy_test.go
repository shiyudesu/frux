package domainrecommendation

import (
	"encoding/json"
	"strings"
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
	expectedJSON := []string{
		`{"feature_weights":{"author_affinity":0.15,"content_similarity":0.7,"exposure_penalty":-0.4,"follow_relation":0.1,"freshness":0.1,"hotness":0.2,"negative_penalty":-0.75,"session_similarity":0.25},"recall_budgets":{"content_similarity":100,"followed_author":100,"fresh":100,"hot":100,"session_continuation":100},"provider_deadlines_ms":{"content_similarity":250,"followed_author":200,"fresh":150,"hot":150,"session_continuation":250},"freshness_half_life_hours":72,"profile_long_term_half_life_hours":720,"profile_recent_half_life_hours":24,"exposure_window_hours":168,"diversity":{"max_per_author":10,"min_author_gap":1,"min_content_gap":1},"rollout_percentage":100,"snapshot_ttl_seconds":300,"sampling_rate_ppm":10000,"retention_days":30,"minimum_fallback_pool":1,"hard_suppress_exposures":true,"suppression_hours":{"already_seen":168,"not_interested":720,"reduce_author":336}}`,
		`{"feature_weights":{"author_affinity":0.15,"content_similarity":0.6,"exposure_penalty":-0.4,"follow_relation":0.1,"freshness":0.15,"hotness":0.2,"negative_penalty":-0.75,"session_similarity":0.3},"recall_budgets":{"content_similarity":100,"followed_author":100,"fresh":100,"hot":100,"session_continuation":100},"provider_deadlines_ms":{"content_similarity":250,"followed_author":200,"fresh":150,"hot":150,"session_continuation":250},"freshness_half_life_hours":72,"profile_long_term_half_life_hours":720,"profile_recent_half_life_hours":24,"exposure_window_hours":168,"diversity":{"max_per_author":6,"min_author_gap":1,"min_content_gap":1},"rollout_percentage":5,"snapshot_ttl_seconds":300,"sampling_rate_ppm":10000,"retention_days":30,"minimum_fallback_pool":1,"hard_suppress_exposures":true,"suppression_hours":{"already_seen":168,"not_interested":720,"reduce_author":336}}`,
	}
	for index, policy := range policies {
		if got := totalRecallBudget(policy.Config.RecallBudgets); got != MaxPolicyPreRankCandidates {
			t.Fatalf("policy v%d recall budget total = %d, want %d", policy.Version, got, MaxPolicyPreRankCandidates)
		}
		encoded, err := json.Marshal(policy.Config)
		if err != nil {
			t.Fatalf("marshal policy v%d: %v", policy.Version, err)
		}
		if string(encoded) != expectedJSON[index] {
			t.Fatalf("policy v%d serialization changed:\n got: %s\nwant: %s", policy.Version, encoded, expectedJSON[index])
		}
		for _, forbidden := range []string{"pre_rank_pool_limit", "recall_provider_order", "recall_provider_reservations"} {
			if strings.Contains(string(encoded), forbidden) {
				t.Fatalf("policy v%d unexpectedly serialized quota field %q: %s", policy.Version, forbidden, encoded)
			}
		}
	}
}
