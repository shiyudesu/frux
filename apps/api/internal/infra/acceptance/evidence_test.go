package infraacceptance

import (
	"errors"
	"testing"

	domainembedding "github.com/shiyudesu/frux/internal/domain/embedding"
	multimodalprofile "github.com/shiyudesu/frux/internal/infra/multimodalprofile"
)

func TestValidateDatabaseEvidence(t *testing.T) {
	profile, err := multimodalprofile.Resolve(multimodalprofile.TongyiFlashSnapshotProfile)
	if err != nil {
		t.Fatal(err)
	}
	valid := DatabaseEvidence{
		JobState: "succeeded", FactPresent: true, ProjectionPresent: true,
		Contract: profile.Contract, VectorLength: profile.Dimension, VectorNorm: 1,
		FactDigest: "digest", ProjectionDigest: "digest",
		FactSourceHash: "source", ProjectionSourceHash: "source",
	}
	if err := ValidateDatabaseEvidence(valid, profile.Contract); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		code   EvidenceFailureCode
		mutate func(*DatabaseEvidence)
	}{
		{EvidenceJobTerminal, func(e *DatabaseEvidence) { e.JobState = "terminal" }},
		{EvidenceFactMissing, func(e *DatabaseEvidence) { e.FactPresent = false }},
		{EvidenceProjectionMissing, func(e *DatabaseEvidence) { e.ProjectionPresent = false }},
		{EvidenceContractMismatch, func(e *DatabaseEvidence) { e.Contract.RevisionAlias = "other" }},
		{EvidenceVectorInvalid, func(e *DatabaseEvidence) { e.VectorNorm = 0.5 }},
		{EvidenceDigestMismatch, func(e *DatabaseEvidence) { e.ProjectionDigest = "other" }},
		{EvidenceSourceMismatch, func(e *DatabaseEvidence) { e.ProjectionSourceHash = "other" }},
	}
	for _, test := range tests {
		evidence := valid
		test.mutate(&evidence)
		err := ValidateDatabaseEvidence(evidence, profile.Contract)
		var failure *EvidenceError
		if !errors.As(err, &failure) || failure.Code != test.code {
			t.Fatalf("error=%v want=%s", err, test.code)
		}
	}
}

func TestValidateDatabaseEvidenceRejectsUnknownContract(t *testing.T) {
	expected, _ := domainembedding.NewMultimodalContractIdentity("p", "m", "r", 32, "t", "f", "i", "x")
	err := ValidateDatabaseEvidence(DatabaseEvidence{JobState: "succeeded"}, expected)
	if err == nil {
		t.Fatal("expected evidence error")
	}
}
