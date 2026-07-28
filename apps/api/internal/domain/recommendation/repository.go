package domainrecommendation

import (
	"context"
	"time"
)

type Repository interface {
	ListCandidatePool(ctx context.Context, userID int64, limit int) ([]*Candidate, error)
	LoadUserInterestVector(ctx context.Context, userID int64) ([]float64, bool, error)
	LoadVideoVectors(ctx context.Context, videoIDs []int64) (map[int64][]float64, error)
	ListRecentExposures(ctx context.Context, userID int64, videoIDs []int64, since time.Time) ([]*Exposure, error)
	SaveExposures(ctx context.Context, exposures []*ExposureWrite) ([]*Exposure, error)
	FindFeedbackByUserAndIdempotencyKey(ctx context.Context, userID int64, idempotencyKey string) (*Feedback, error)
	SaveFeedback(ctx context.Context, feedback *Feedback) (*Feedback, bool, error)
}

type PolicyRepository interface {
	CreatePolicy(ctx context.Context, policy *Policy) (*Policy, error)
	ActivatePolicy(ctx context.Context, scene string, version int) (*Policy, error)
	RollbackPolicy(ctx context.Context, scene string, version int) (*Policy, error)
	ListEnabledPolicies(ctx context.Context, scene string) ([]*Policy, error)
	ListPolicies(ctx context.Context, scene string) ([]*Policy, error)
}

type ProfileRepository interface {
	ApplyProfileEvent(ctx context.Context, event *ProfileEvent) (*UserInterestProfile, bool, error)
}

type RequestLogRepository interface {
	SaveRequestLog(ctx context.Context, log *RecommendationRequestLog) (*RecommendationRequestLog, bool, error)
	DeleteRequestLogsBefore(ctx context.Context, cutoff time.Time, limit int) (int64, error)
}

// ServedCandidateEvidenceRepository records only candidates returned to the
// client. A recording failure leaves the page un-attributable and must be
// observed without allowing omitted candidates to become eligible.
type ServedCandidateEvidenceRepository interface {
	SaveServedCandidateEvidence(ctx context.Context, evidence *ServedCandidateEvidence) (replayed bool, err error)
	AppendServedCandidateEvidence(ctx context.Context, evidence *ServedCandidateEvidence) (replayed bool, err error)
	HasServedCandidateEvidence(ctx context.Context, userID int64, requestID string, videoID int64, recordedAt time.Time) (bool, error)
	DeleteServedCandidateEvidenceBefore(ctx context.Context, cutoff time.Time, requestLimit int) (ServedCandidateEvidenceCleanupResult, error)
}
