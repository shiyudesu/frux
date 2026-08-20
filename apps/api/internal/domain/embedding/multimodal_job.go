package domainembedding

import (
	"strings"
	"time"
)

const (
	MultimodalJobStatePending   = "pending"
	MultimodalJobStateLeased    = "leased"
	MultimodalJobStateRetry     = "retry"
	MultimodalJobStateSucceeded = "succeeded"
	MultimodalJobStateTerminal  = "terminal"

	MultimodalFailureNone              = ""
	MultimodalFailureAdmission         = "admission"
	MultimodalFailureTimeout           = "timeout"
	MultimodalFailureProviderRetryable = "provider_retryable"
	MultimodalFailureProviderTerminal  = "provider_terminal"
	MultimodalFailureInvalidInput      = "invalid_input"
	MultimodalFailureInvalidVector     = "invalid_vector"
	MultimodalFailureStaleSource       = "stale_source"
	MultimodalFailureLeaseLost         = "lease_lost"

	MaxMultimodalJobAttempts = 10
	MaxMultimodalClaimToken  = 128
	MaxMultimodalFailureCode = 64
)

type MultimodalEmbeddingJob struct {
	ID            int64
	VideoID       int64
	Contract      MultimodalContractIdentity
	SourceHash    string
	State         string
	Attempts      int
	MaxAttempts   int
	ClaimToken    string
	LeaseUntil    *time.Time
	NextAttemptAt time.Time
	FailureCode   string
	CreatedAt     time.Time
	UpdatedAt     time.Time
	CompletedAt   *time.Time
}

func NewMultimodalEmbeddingJob(
	videoID int64,
	contract MultimodalContractIdentity,
	sourceHash string,
	maxAttempts int,
	now time.Time,
) (*MultimodalEmbeddingJob, error) {
	if videoID <= 0 || !validMultimodalContract(contract) || !validSHA256Hex(sourceHash) ||
		maxAttempts < 1 || maxAttempts > MaxMultimodalJobAttempts || now.IsZero() {
		return nil, ErrInvalidMultimodalJob
	}
	now = now.UTC()
	return &MultimodalEmbeddingJob{
		VideoID: videoID, Contract: contract, SourceHash: strings.ToLower(strings.TrimSpace(sourceHash)),
		State: MultimodalJobStatePending, MaxAttempts: maxAttempts,
		NextAttemptAt: now, CreatedAt: now, UpdatedAt: now,
	}, nil
}

func RestoreMultimodalEmbeddingJob(
	id int64,
	videoID int64,
	contract MultimodalContractIdentity,
	sourceHash string,
	state string,
	attempts int,
	maxAttempts int,
	claimToken string,
	leaseUntil *time.Time,
	nextAttemptAt time.Time,
	failureCode string,
	createdAt time.Time,
	updatedAt time.Time,
	completedAt *time.Time,
) *MultimodalEmbeddingJob {
	state = strings.ToLower(strings.TrimSpace(state))
	failureCode = strings.ToLower(strings.TrimSpace(failureCode))
	claimToken = strings.TrimSpace(claimToken)
	if id <= 0 || videoID <= 0 || !validMultimodalContract(contract) || !validSHA256Hex(sourceHash) ||
		!ValidMultimodalJobState(state) || attempts < 0 || attempts > maxAttempts ||
		maxAttempts < 1 || maxAttempts > MaxMultimodalJobAttempts ||
		len(claimToken) > MaxMultimodalClaimToken || !ValidMultimodalFailureCode(failureCode) ||
		nextAttemptAt.IsZero() || createdAt.IsZero() || updatedAt.IsZero() {
		return nil
	}
	if state == MultimodalJobStateLeased {
		if claimToken == "" || leaseUntil == nil || leaseUntil.IsZero() || completedAt != nil {
			return nil
		}
	} else if claimToken != "" || leaseUntil != nil {
		return nil
	}
	if state == MultimodalJobStateSucceeded || state == MultimodalJobStateTerminal {
		if completedAt == nil || completedAt.IsZero() {
			return nil
		}
	} else if completedAt != nil {
		return nil
	}
	if (state == MultimodalJobStateRetry || state == MultimodalJobStateTerminal) == (failureCode == "") {
		return nil
	}
	job := &MultimodalEmbeddingJob{
		ID: id, VideoID: videoID, Contract: contract,
		SourceHash: strings.ToLower(strings.TrimSpace(sourceHash)), State: state,
		Attempts: attempts, MaxAttempts: maxAttempts, ClaimToken: claimToken,
		NextAttemptAt: nextAttemptAt.UTC(), FailureCode: failureCode,
		CreatedAt: createdAt.UTC(), UpdatedAt: updatedAt.UTC(),
	}
	job.LeaseUntil = cloneTime(leaseUntil)
	job.CompletedAt = cloneTime(completedAt)
	return job
}

func (j *MultimodalEmbeddingJob) Clone() *MultimodalEmbeddingJob {
	if j == nil {
		return nil
	}
	cloned := *j
	cloned.LeaseUntil = cloneTime(j.LeaseUntil)
	cloned.CompletedAt = cloneTime(j.CompletedAt)
	return &cloned
}

type MultimodalVectorFact struct {
	ID        int64
	VideoID   int64
	Identity  MultimodalVectorIdentity
	Values    []float64
	CreatedAt time.Time
	UpdatedAt time.Time
}

func NewMultimodalVectorFact(videoID int64, vector *MultimodalVector, now time.Time) (*MultimodalVectorFact, error) {
	if videoID <= 0 || vector == nil || now.IsZero() {
		return nil, ErrInvalidMultimodalVectorFact
	}
	validated, err := ValidateMultimodalVector(
		vector.Identity.Contract, vector.Identity.SourceHash, vector.Identity, vector.Values,
	)
	if err != nil {
		return nil, ErrInvalidMultimodalVectorFact
	}
	now = now.UTC()
	return &MultimodalVectorFact{
		VideoID: videoID, Identity: validated.Identity,
		Values: append([]float64(nil), validated.Values...), CreatedAt: now, UpdatedAt: now,
	}, nil
}

func RestoreMultimodalVectorFact(
	id int64,
	videoID int64,
	identity MultimodalVectorIdentity,
	values []float64,
	createdAt time.Time,
	updatedAt time.Time,
) *MultimodalVectorFact {
	if id <= 0 || videoID <= 0 || createdAt.IsZero() || updatedAt.IsZero() {
		return nil
	}
	validated, err := ValidateMultimodalVector(identity.Contract, identity.SourceHash, identity, values)
	if err != nil {
		return nil
	}
	return &MultimodalVectorFact{
		ID: id, VideoID: videoID, Identity: validated.Identity,
		Values:    append([]float64(nil), validated.Values...),
		CreatedAt: createdAt.UTC(), UpdatedAt: updatedAt.UTC(),
	}
}

func (f *MultimodalVectorFact) Clone() *MultimodalVectorFact {
	if f == nil {
		return nil
	}
	cloned := *f
	cloned.Values = append([]float64(nil), f.Values...)
	return &cloned
}

type MultimodalProjection struct {
	VideoID     int64
	Identity    MultimodalVectorIdentity
	Values      []float64
	PublishedAt time.Time
	UpdatedAt   time.Time
}

func NewMultimodalProjection(fact *MultimodalVectorFact, publishedAt, now time.Time) (*MultimodalProjection, error) {
	if fact == nil || fact.VideoID <= 0 || publishedAt.IsZero() || now.IsZero() {
		return nil, ErrInvalidMultimodalProjection
	}
	return &MultimodalProjection{
		VideoID: fact.VideoID, Identity: fact.Identity,
		Values:      append([]float64(nil), fact.Values...),
		PublishedAt: publishedAt.UTC(), UpdatedAt: now.UTC(),
	}, nil
}

func ValidMultimodalJobState(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case MultimodalJobStatePending, MultimodalJobStateLeased, MultimodalJobStateRetry,
		MultimodalJobStateSucceeded, MultimodalJobStateTerminal:
		return true
	default:
		return false
	}
}

func ValidMultimodalFailureCode(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case MultimodalFailureNone, MultimodalFailureAdmission, MultimodalFailureTimeout,
		MultimodalFailureProviderRetryable, MultimodalFailureProviderTerminal,
		MultimodalFailureInvalidInput, MultimodalFailureInvalidVector,
		MultimodalFailureStaleSource, MultimodalFailureLeaseLost:
		return true
	default:
		return false
	}
}

func validMultimodalContract(contract MultimodalContractIdentity) bool {
	validated, err := NewMultimodalContractIdentity(
		contract.ProviderAlias, contract.ModelAlias, contract.RevisionAlias, contract.Dimension,
		contract.TextCanonicalizer, contract.FrameSamplingPolicy,
		contract.ImagePreprocessingPolicy, contract.FusionPolicy,
	)
	return err == nil && validated.Equal(contract)
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := value.UTC()
	return &cloned
}
