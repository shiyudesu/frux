package applicationrecommendation

import (
	"fmt"
	"testing"

	domainrecommendation "github.com/shiyudesu/frux/internal/domain/recommendation"
)

func TestSessionSemanticShadowSamplerBoundariesAndEligibility(t *testing.T) {
	for _, test := range []struct {
		name      string
		ppm       int
		userID    int64
		scene     string
		requestID string
		firstPage bool
		want      bool
	}{
		{name: "disabled", ppm: 0, userID: 1, scene: "recommend", requestID: "request", firstPage: true},
		{name: "full", ppm: domainrecommendation.MaxSamplingRatePPM, userID: 1, scene: "recommend", requestID: "request", firstPage: true, want: true},
		{name: "normalized", ppm: domainrecommendation.MaxSamplingRatePPM, userID: 1, scene: " Recommend ", requestID: " request ", firstPage: true, want: true},
		{name: "anonymous", ppm: domainrecommendation.MaxSamplingRatePPM, userID: 0, scene: "recommend", requestID: "request", firstPage: true},
		{name: "other scene", ppm: domainrecommendation.MaxSamplingRatePPM, userID: 1, scene: "hot", requestID: "request", firstPage: true},
		{name: "missing request", ppm: domainrecommendation.MaxSamplingRatePPM, userID: 1, scene: "recommend", firstPage: true},
		{name: "cursor page", ppm: domainrecommendation.MaxSamplingRatePPM, userID: 1, scene: "recommend", requestID: "request", firstPage: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := ShouldSampleSessionSemanticShadow(test.ppm, test.userID, test.scene, test.requestID, test.firstPage); got != test.want {
				t.Fatalf("sample=%v want=%v", got, test.want)
			}
		})
	}
}

func TestSessionSemanticShadowSamplerIsStableAndDistributes(t *testing.T) {
	const ppm = 500_000
	selected := 0
	for index := 0; index < 1000; index++ {
		requestID := fmt.Sprintf("request-%d", index)
		first := ShouldSampleSessionSemanticShadow(ppm, 42, "recommend", requestID, true)
		second := ShouldSampleSessionSemanticShadow(ppm, 42, "recommend", requestID, true)
		if first != second {
			t.Fatalf("request %q was not stable", requestID)
		}
		if first {
			selected++
		}
	}
	if selected < 350 || selected > 650 {
		t.Fatalf("selected=%d, sampler distribution is implausibly skewed", selected)
	}
}

func TestSessionSemanticShadowSamplerUsesLengthDelimitedFields(t *testing.T) {
	left := ShouldSampleSessionSemanticShadow(500_000, 12, "recommend", "3", true)
	right := ShouldSampleSessionSemanticShadow(500_000, 1, "recommend", "23", true)
	if left == right {
		// One equality can happen by chance, so compare a second threshold that
		// exposes distinct fixed buckets without relying on private hash output.
		left = ShouldSampleSessionSemanticShadow(250_000, 12, "recommend", "3", true)
		right = ShouldSampleSessionSemanticShadow(250_000, 1, "recommend", "23", true)
	}
	if left == right {
		t.Fatal("distinct length-delimited identities unexpectedly shared both selection boundaries")
	}
}
