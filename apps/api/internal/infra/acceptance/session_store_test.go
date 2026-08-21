package infraacceptance

import (
	"testing"

	domainrecommendation "github.com/shiyudesu/frux/internal/domain/recommendation"
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
