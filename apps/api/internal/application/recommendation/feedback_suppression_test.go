package applicationrecommendation

import (
	"testing"
	"time"

	domainrecommendation "github.com/shiyudesu/frux/internal/domain/recommendation"
)

func TestFeedbackSuppressionHonorsScopeAndFallbackPool(t *testing.T) {
	candidates := []*domainrecommendation.Candidate{
		rankerCandidate(1, 10, 1, time.Now(), domainrecommendation.RecallProviderFresh),
		rankerCandidate(2, 20, 1, time.Now(), domainrecommendation.RecallProviderFresh),
		rankerCandidate(3, 20, 1, time.Now(), domainrecommendation.RecallProviderFresh),
	}
	features := &domainrecommendation.RankingFeatures{SuppressedVideos: map[int64]bool{1: true}, SuppressedAuthors: map[int64]bool{}}
	config := defaultRecommendationPolicyConfiguration()
	config.MinimumFallbackPool = 1
	if got := applyFeedbackSuppression(candidates, features, config); len(got) != 2 {
		t.Fatalf("expected video suppression, got %#v", got)
	}
	features.SuppressedVideos = map[int64]bool{}
	features.SuppressedAuthors = map[int64]bool{20: true}
	if got := applyFeedbackSuppression(candidates, features, config); len(got) != 1 || got[0].AuthorID != 10 {
		t.Fatalf("expected author suppression, got %#v", got)
	}
	config.MinimumFallbackPool = 2
	if got := applyFeedbackSuppression(candidates, features, config); len(got) != len(candidates) {
		t.Fatalf("suppression must not over-filter below fallback pool: %#v", got)
	}
}

func TestFeedbackSuppressionPoliciesHaveBoundedExpiryPerType(t *testing.T) {
	policy, err := domainrecommendation.NewPolicy("recommend", 2, true, defaultRecommendationPolicyConfiguration(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, feedbackType := range []string{domainrecommendation.FeedbackTypeNotInterested, domainrecommendation.FeedbackTypeReduceAuthor, domainrecommendation.FeedbackTypeAlreadySeen} {
		if policy.Config.SuppressionHours[feedbackType] <= 0 || policy.Config.SuppressionHours[feedbackType] > domainrecommendation.MaxSuppressionHours {
			t.Fatalf("invalid bounded expiry for %s", feedbackType)
		}
	}
}

func TestRecommendationOutcomeCarriesOnlyDurableJoinFields(t *testing.T) {
	outcome, err := domainrecommendation.NewOutcome("view:event-1", "request-1", 7, 9, "complete", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if outcome.RequestID != "request-1" || outcome.OutcomeType != "complete" || outcome.UserID != 7 || outcome.VideoID != 9 {
		t.Fatalf("unexpected outcome: %#v", outcome)
	}
}
