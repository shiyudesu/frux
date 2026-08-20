package domainembedding

import (
	"testing"
	"time"
)

func TestMultimodalJobAndFactDefensiveRestoration(t *testing.T) {
	now := time.Date(2026, 8, 20, 8, 0, 0, 0, time.UTC)
	contract := testMultimodalContract(t, MinMultimodalDimension)
	sourceHash := MultimodalSourceHash([]byte("source"))
	job, err := NewMultimodalEmbeddingJob(9, contract, sourceHash, 5, now)
	if err != nil {
		t.Fatal(err)
	}
	if job.State != MultimodalJobStatePending || job.Attempts != 0 ||
		job.MaxAttempts != 5 || !job.NextAttemptAt.Equal(now) {
		t.Fatalf("unexpected job: %#v", job)
	}
	leaseUntil := now.Add(time.Minute)
	restored := RestoreMultimodalEmbeddingJob(
		3, 9, contract, sourceHash, MultimodalJobStateLeased, 1, 5,
		"claim", &leaseUntil, now, "", now, now, nil,
	)
	if restored == nil || restored.ClaimToken != "claim" || restored.LeaseUntil == &leaseUntil {
		t.Fatalf("leased job was not restored defensively: %#v", restored)
	}
	clone := restored.Clone()
	clone.Contract.ModelAlias = "changed"
	*clone.LeaseUntil = now
	if restored.Contract.ModelAlias != contract.ModelAlias || restored.LeaseUntil.Equal(now) {
		t.Fatal("job clone aliased original")
	}

	values := make([]float64, contract.Dimension)
	values[0] = 1
	vector := &MultimodalVector{
		Identity: MultimodalVectorIdentity{
			Contract: contract, SourceHash: sourceHash, VectorDigest: MultimodalVectorDigest(values),
		},
		Values: values,
	}
	fact, err := NewMultimodalVectorFact(9, vector, now)
	if err != nil {
		t.Fatal(err)
	}
	values[0] = 0
	if fact.Values[0] != 1 {
		t.Fatal("fact aliased vector values")
	}
	projection, err := NewMultimodalProjection(fact, now.Add(-time.Hour), now)
	if err != nil || projection.VideoID != fact.VideoID || projection.Identity != fact.Identity {
		t.Fatalf("projection=%#v err=%v", projection, err)
	}
}

func TestMultimodalJobRejectsInvalidStateAndLeaseShapes(t *testing.T) {
	now := time.Date(2026, 8, 20, 8, 0, 0, 0, time.UTC)
	contract := testMultimodalContract(t, MinMultimodalDimension)
	sourceHash := MultimodalSourceHash([]byte("source"))
	leaseUntil := now.Add(time.Minute)
	completedAt := now
	tests := []struct {
		name      string
		state     string
		claim     string
		lease     *time.Time
		completed *time.Time
		failure   string
	}{
		{name: "unknown", state: "unknown"},
		{name: "leased without token", state: MultimodalJobStateLeased, lease: &leaseUntil},
		{name: "pending with lease", state: MultimodalJobStatePending, claim: "claim", lease: &leaseUntil},
		{name: "succeeded without completion", state: MultimodalJobStateSucceeded},
		{name: "pending completed", state: MultimodalJobStatePending, completed: &completedAt},
		{name: "raw failure", state: MultimodalJobStateRetry, failure: "provider said secret"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if job := RestoreMultimodalEmbeddingJob(
				1, 9, contract, sourceHash, test.state, 1, 5,
				test.claim, test.lease, now, test.failure, now, now, test.completed,
			); job != nil {
				t.Fatalf("invalid job restored: %#v", job)
			}
		})
	}
}
