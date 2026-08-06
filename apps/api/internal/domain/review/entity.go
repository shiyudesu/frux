package domainreview

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"strings"
	"time"
	"unicode"
)

const (
	CaseStatusOpen         = "open"
	CaseStatusPendingHuman = "pending_human"
	CaseStatusApproved     = "approved"
	CaseStatusRejected     = "rejected"
	CaseStatusCancelled    = "cancelled"
	CaseStatusSuperseded   = "superseded"

	OutcomeApprove = "approve"
	OutcomeReject  = "reject"
	OutcomeHuman   = "human"

	MaxResultIdentityLength = 128
	MaxProviderLength       = 64
	MaxModelVersionLength   = 128
	MaxSignalLabelLength    = 64
	MaxSignalsPerResult     = 32
	MaxEvidenceRefs         = 8
	MaxEvidenceRefLength    = 512
	MaxEvidenceBytes        = 2048
)

type ReviewCase struct {
	ID                 int64
	VideoID            int64
	ReviewVersion      int
	Status             string
	PolicyVersion      int
	Priority           int
	Version            int
	AssignedReviewerID int64
	LeaseTokenHash     string
	LeaseExpiresAt     *time.Time
	CreatedAt          time.Time
	UpdatedAt          time.Time
	ClosedAt           *time.Time
}

func NewCase(videoID int64, reviewVersion, policyVersion int, now time.Time) (*ReviewCase, error) {
	if videoID <= 0 {
		return nil, ErrInvalidVideoID
	}
	if reviewVersion <= 0 {
		return nil, ErrInvalidReviewVersion
	}
	if policyVersion <= 0 {
		return nil, ErrInvalidPolicyVersion
	}
	if now.IsZero() {
		return nil, ErrInvalidSignal
	}
	now = now.UTC().Truncate(time.Microsecond)
	return &ReviewCase{
		VideoID: videoID, ReviewVersion: reviewVersion, PolicyVersion: policyVersion,
		Status: CaseStatusOpen, Version: 1, CreatedAt: now, UpdatedAt: now,
	}, nil
}

func RestoreCase(id, videoID int64, reviewVersion int, status string, policyVersion int, createdAt, updatedAt time.Time, closedAt *time.Time) *ReviewCase {
	return &ReviewCase{
		ID: id, VideoID: videoID, ReviewVersion: reviewVersion,
		Status: normalizeToken(status), PolicyVersion: policyVersion,
		Version: 1, CreatedAt: createdAt.UTC(), UpdatedAt: updatedAt.UTC(), ClosedAt: closedAt,
	}
}

func RestoreHumanCase(
	id, videoID int64,
	reviewVersion int,
	status string,
	policyVersion, priority, version int,
	assignedReviewerID int64,
	leaseTokenHash string,
	leaseExpiresAt *time.Time,
	createdAt, updatedAt time.Time,
	closedAt *time.Time,
) *ReviewCase {
	reviewCase := RestoreCase(id, videoID, reviewVersion, status, policyVersion, createdAt, updatedAt, closedAt)
	reviewCase.Priority = priority
	reviewCase.Version = version
	reviewCase.AssignedReviewerID = assignedReviewerID
	reviewCase.LeaseTokenHash = strings.TrimSpace(leaseTokenHash)
	reviewCase.LeaseExpiresAt = leaseExpiresAt
	return reviewCase
}

func ValidCaseStatus(status string) bool {
	switch normalizeToken(status) {
	case CaseStatusOpen, CaseStatusPendingHuman, CaseStatusApproved, CaseStatusRejected,
		CaseStatusCancelled, CaseStatusSuperseded:
		return true
	default:
		return false
	}
}

type MachineSignal struct {
	Label        string
	Confidence   float64
	EvidenceRefs []string
}

type MachineResultInput struct {
	CaseID        int64
	VideoID       int64
	ReviewVersion int
	ResultID      string
	Provider      string
	ModelVersion  string
	PolicyVersion int
	Signals       []MachineSignal
	ReceivedAt    time.Time
}

type MachineResult struct {
	CaseID        int64
	VideoID       int64
	ReviewVersion int
	ResultID      string
	Provider      string
	ModelVersion  string
	PolicyVersion int
	Signals       []MachineSignal
	ReceivedAt    time.Time
	PayloadHash   string
}

func NewMachineResult(input MachineResultInput) (*MachineResult, error) {
	if input.CaseID <= 0 {
		return nil, ErrInvalidCaseID
	}
	if input.VideoID <= 0 {
		return nil, ErrInvalidVideoID
	}
	if input.ReviewVersion <= 0 {
		return nil, ErrInvalidReviewVersion
	}
	if input.PolicyVersion <= 0 {
		return nil, ErrInvalidPolicyVersion
	}
	resultID := strings.TrimSpace(input.ResultID)
	provider := normalizeToken(input.Provider)
	modelVersion := strings.TrimSpace(input.ModelVersion)
	if resultID == "" || len(resultID) > MaxResultIdentityLength {
		return nil, ErrInvalidResultIdentity
	}
	if provider == "" || len(provider) > MaxProviderLength {
		return nil, ErrInvalidProvider
	}
	if modelVersion == "" || len(modelVersion) > MaxModelVersionLength {
		return nil, ErrInvalidModelVersion
	}
	if len(input.Signals) == 0 || len(input.Signals) > MaxSignalsPerResult {
		return nil, ErrTooManySignals
	}
	signals := make([]MachineSignal, 0, len(input.Signals))
	for _, signal := range input.Signals {
		normalized, err := normalizeSignal(signal)
		if err != nil {
			return nil, err
		}
		signals = append(signals, normalized)
	}
	receivedAt := input.ReceivedAt
	if receivedAt.IsZero() {
		receivedAt = time.Now().UTC()
	}
	result := &MachineResult{
		CaseID: input.CaseID, VideoID: input.VideoID, ReviewVersion: input.ReviewVersion,
		ResultID: resultID, Provider: provider, ModelVersion: modelVersion,
		PolicyVersion: input.PolicyVersion, Signals: signals,
		ReceivedAt: receivedAt.UTC().Truncate(time.Microsecond),
	}
	payload, err := json.Marshal(struct {
		CaseID        int64           `json:"case_id"`
		VideoID       int64           `json:"video_id"`
		ReviewVersion int             `json:"review_version"`
		ResultID      string          `json:"result_id"`
		Provider      string          `json:"provider"`
		ModelVersion  string          `json:"model_version"`
		PolicyVersion int             `json:"policy_version"`
		Signals       []MachineSignal `json:"signals"`
	}{
		result.CaseID, result.VideoID, result.ReviewVersion, result.ResultID,
		result.Provider, result.ModelVersion, result.PolicyVersion, result.Signals,
	})
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(payload)
	result.PayloadHash = hex.EncodeToString(sum[:])
	return result, nil
}

func normalizeSignal(signal MachineSignal) (MachineSignal, error) {
	label := NormalizeLabel(signal.Label)
	if label == "" || len(label) > MaxSignalLabelLength {
		return MachineSignal{}, ErrInvalidSignal
	}
	if math.IsNaN(signal.Confidence) || math.IsInf(signal.Confidence, 0) || signal.Confidence < 0 || signal.Confidence > 1 {
		return MachineSignal{}, ErrInvalidConfidence
	}
	if len(signal.EvidenceRefs) > MaxEvidenceRefs {
		return MachineSignal{}, ErrTooManyEvidenceRefs
	}
	refs := make([]string, 0, len(signal.EvidenceRefs))
	total := 0
	for _, ref := range signal.EvidenceRefs {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			return MachineSignal{}, ErrInvalidSignal
		}
		if len(ref) > MaxEvidenceRefLength {
			return MachineSignal{}, ErrEvidenceRefTooLong
		}
		total += len(ref)
		if total > MaxEvidenceBytes {
			return MachineSignal{}, ErrEvidenceTooLarge
		}
		refs = append(refs, ref)
	}
	return MachineSignal{Label: label, Confidence: signal.Confidence, EvidenceRefs: refs}, nil
}

func NormalizeLabel(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	var builder strings.Builder
	underscore := false
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			builder.WriteRune(r)
			underscore = false
			continue
		}
		if builder.Len() > 0 && !underscore {
			builder.WriteByte('_')
			underscore = true
		}
	}
	return strings.Trim(builder.String(), "_")
}

type AutomatedDecision struct {
	ID            int64
	CaseID        int64
	ResultID      string
	Outcome       string
	PolicyVersion int
	CreatedAt     time.Time
}

func ValidOutcome(outcome string) bool {
	switch normalizeToken(outcome) {
	case OutcomeApprove, OutcomeReject, OutcomeHuman:
		return true
	default:
		return false
	}
}

type ProcessingResult struct {
	Case             *ReviewCase
	Decision         *AutomatedDecision
	Duplicate        bool
	ApplySideEffects bool
	MediaAssetID     int64
	CoverAssetID     int64
}

type ReconciliationStats struct {
	Scanned  int
	Created  int
	Existing int
	Failed   int
}

func normalizeToken(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}
