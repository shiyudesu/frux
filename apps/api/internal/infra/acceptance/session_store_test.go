package infraacceptance

import (
	"testing"
	"time"

	applicationrecommendation "github.com/shiyudesu/frux/internal/application/recommendation"
	domainrecommendation "github.com/shiyudesu/frux/internal/domain/recommendation"
	multimodalprofile "github.com/shiyudesu/frux/internal/infra/multimodalprofile"
)

func TestSessionAcceptanceCohortRequestIDIsStableAndSelected(t *testing.T) {
	first, err := sessionAcceptanceCohortRequestID("session-acceptance-run", 42)
	if err != nil {
		t.Fatal(err)
	}
	second, err := sessionAcceptanceCohortRequestID("session-acceptance-run", 42)
	if err != nil {
		t.Fatal(err)
	}
	if first != second || len(first) > domainrecommendation.MaxRequestIDLength ||
		domainrecommendation.PolicyCohortPercent(42, "recommend", first) >= 1 {
		t.Fatalf("first=%q second=%q cohort=%d", first, second, domainrecommendation.PolicyCohortPercent(42, "recommend", first))
	}
}

func TestSessionAcceptanceFullRolloutNeedsNoFallbackRequest(t *testing.T) {
	policies, err := domainrecommendation.InitialRecommendationPolicies(time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	baseline := policies[0]
	profile, err := multimodalprofile.Resolve(multimodalprofile.TongyiFlashSnapshotProfile)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := applicationrecommendation.BuildSessionSemanticRolloutPolicy(
		baseline,
		applicationrecommendation.SessionSemanticRolloutOptions{
			TargetVersion:     applicationrecommendation.DevelopmentSessionSemanticPolicyVersion,
			RolloutPercentage: applicationrecommendation.FullSessionSemanticRolloutPercentage,
			AllowFullRollout:  true,
			Contract:          profile.Contract,
			Now:               time.Unix(2, 0).UTC(),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	target, err := domainrecommendation.NewPolicy(
		domainrecommendation.RecommendationRequestLogScene,
		plan.Policy.Version,
		true,
		plan.Policy.Config,
		time.Unix(2, 0).UTC(),
	)
	if err != nil {
		t.Fatal(err)
	}
	targetRequestID, fallbackRequestID, fallbackVersion, err := sessionAcceptanceRolloutRequestIDs(
		"full-rollout", 42, target.Version, []*domainrecommendation.Policy{target, baseline},
	)
	if err != nil || targetRequestID == "" || fallbackRequestID != "" || fallbackVersion != 0 {
		t.Fatalf("target=%q fallback=%q version=%d error=%v", targetRequestID, fallbackRequestID, fallbackVersion, err)
	}
	if selected := domainrecommendation.SelectPolicy(
		[]*domainrecommendation.Policy{target, baseline}, 42, targetRequestID,
	); selected == nil || selected.Version != target.Version {
		t.Fatalf("selected=%#v", selected)
	}
}
