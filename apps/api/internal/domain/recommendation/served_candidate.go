package domainrecommendation

import (
	"strings"
	"time"
)

const (
	MaxServedCandidateEvidence = MaxRequestLogCandidates
	// ServedCandidateEvidenceMinimumTTL covers the bounded retry window used
	// to attribute durable action, follow, and view outcomes.
	ServedCandidateEvidenceMinimumTTL = OutcomeAttributionEvidenceWindow
	// ServedCandidateEvidenceDeliveryGrace retains expired evidence long
	// enough for the longest bounded MQ/outbox attribution delivery path.
	// It is deliberately longer than OutcomeAttributionEvidenceWindow and
	// the workers' one-minute maximum retry delay. It does not extend the
	// interval in which an outcome is attributable.
	ServedCandidateEvidenceDeliveryGrace = 5 * time.Minute
)

type ServedCandidateEvidenceItem struct {
	VideoID  int64
	Position int
}

// ServedCandidateEvidence is a server-issued, bounded membership record for
// one delivered recommendation page. It intentionally contains no client
// supplied playback or view-event data.
type ServedCandidateEvidence struct {
	UserID        int64
	RequestID     string
	Scene         string
	PolicyVersion int
	ServedAt      time.Time
	ExpiresAt     time.Time
	Candidates    []ServedCandidateEvidenceItem
}

type ServedCandidateEvidenceInput struct {
	UserID        int64
	RequestID     string
	Scene         string
	PolicyVersion int
	ServedAt      time.Time
	ExpiresAt     time.Time
	Candidates    []ServedCandidateEvidenceItem
}

// ServedCandidateEvidenceCleanupResult reports bounded request groups
// separately from their candidate rows. One expired request can legitimately
// contain the full candidate bound, so row count must not drive batch control.
type ServedCandidateEvidenceCleanupResult struct {
	RequestGroups int
	CandidateRows int64
}

// ServedCandidateEvidenceCleanupCutoff is the expiry time before which
// evidence is beyond its bounded delivery grace and may be deleted.
func ServedCandidateEvidenceCleanupCutoff(now time.Time) time.Time {
	if now.IsZero() {
		return time.Time{}
	}
	return now.UTC().Add(-ServedCandidateEvidenceDeliveryGrace)
}

func NewServedCandidateEvidence(input ServedCandidateEvidenceInput) (*ServedCandidateEvidence, error) {
	requestID := strings.TrimSpace(input.RequestID)
	scene := strings.ToLower(strings.TrimSpace(input.Scene))
	if input.UserID <= 0 || requestID == "" || len(requestID) > MaxRequestIDLength ||
		scene != RecommendationRequestLogScene || input.PolicyVersion <= 0 ||
		input.PolicyVersion > MaxPolicyVersion || input.ServedAt.IsZero() ||
		input.ExpiresAt.IsZero() || !input.ExpiresAt.After(input.ServedAt) ||
		len(input.Candidates) > MaxServedCandidateEvidence {
		return nil, ErrInvalidServedCandidateEvidence
	}

	candidates := make([]ServedCandidateEvidenceItem, 0, len(input.Candidates))
	seen := make(map[int64]struct{}, len(input.Candidates))
	for index, candidate := range input.Candidates {
		if candidate.VideoID <= 0 || candidate.Position != index || candidate.Position >= MaxServedCandidateEvidence {
			return nil, ErrInvalidServedCandidateEvidence
		}
		if _, exists := seen[candidate.VideoID]; exists {
			return nil, ErrInvalidServedCandidateEvidence
		}
		seen[candidate.VideoID] = struct{}{}
		candidates = append(candidates, candidate)
	}

	return &ServedCandidateEvidence{
		UserID:        input.UserID,
		RequestID:     requestID,
		Scene:         scene,
		PolicyVersion: input.PolicyVersion,
		ServedAt:      input.ServedAt.UTC(),
		ExpiresAt:     input.ExpiresAt.UTC(),
		Candidates:    candidates,
	}, nil
}
