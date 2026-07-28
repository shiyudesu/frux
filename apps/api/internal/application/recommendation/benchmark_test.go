package applicationrecommendation

import (
	domainrecommendation "GCFeed/internal/domain/recommendation"
	"context"
	"testing"
	"time"
)

// BenchmarkRecommendBoundedPool is the repeatable pre-release load check. It
// exercises policy selection, ranking, diversity, and cursor construction on
// a bounded 100-candidate pool without external services.
func BenchmarkRecommendBoundedPool(b *testing.B) {
	now := time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC)
	pool := make([]*domainrecommendation.Candidate, 0, 100)
	vectors := make(map[int64][]float64, 100)
	for id := int64(1); id <= 100; id++ {
		pool = append(pool, rankerCandidate(id, (id%10)+1, int(id), now.Add(-time.Duration(id)*time.Minute), domainrecommendation.RecallProviderHot))
		vectors[id] = []float64{float64(id % 3), float64((id + 1) % 5)}
	}
	repo := &rankerTestRepo{pool: pool, vectors: vectors, features: emptyRankingFeatures()}
	policy := rankerPolicy(b, 1, defaultRecommendationPolicyConfiguration().FeatureWeights)
	service := New(repo, WithNow(func() time.Time { return now }), WithPolicySelector(rankerPolicySelector{policy: policy}))
	request := CandidateRequest{UserID: 42, Scene: "recommend", RequestID: "benchmark", Limit: 20}

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if _, err := service.Recommend(context.Background(), request); err != nil {
				b.Fatal(err)
			}
		}
	})
}
