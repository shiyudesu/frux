package domainrecommendation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"hash/fnv"
	"math"
	"strings"
	"time"
)

const (
	MaxRequestLogCandidates        = 500
	MaxRequestLogReasons           = 8
	MaxRequestLogReasonLength      = 64
	MaxRequestLogScoreComponents   = 9
	MaxRequestLogRecallDiagnostics = 64
	MaxRequestLogDiagnosticCount   = MaxRecallBudget * 16
	// MaxRequestLogPayloadBytes accommodates every bounded entry in a normal
	// 500-candidate pool (eight 64-byte reasons and nine score components per
	// candidate) without compacting its ordered explanation prefix.
	MaxRequestLogPayloadBytes     = 1024 * 1024
	RecommendationRequestLogScene = "recommend"
	// OutcomeAttributionEvidenceWindow bounds retries while a durable view
	// event is propagating to recommendation attribution checks.
	OutcomeAttributionEvidenceWindow = 2 * time.Minute
	maxOutcomeIDLength               = 128
)

type RequestLogControl struct {
	SamplingRatePPM int
	RetentionDays   int
}

type LoggedCandidate struct {
	VideoID         int64              `json:"video_id"`
	Reasons         []string           `json:"reasons"`
	ScoreComponents map[string]float64 `json:"score_components"`
}

type RecallDiagnostic struct {
	Phase    string `json:"phase"`
	Provider string `json:"provider"`
	Result   string `json:"result"`
	Reason   string `json:"reason,omitempty"`
	Count    int    `json:"count"`
}

type RequestLogInput struct {
	RequestID         string
	UserID            int64
	Scene             string
	PolicyVersion     int
	Context           *RecommendationContext
	Candidates        []LoggedCandidate
	Degraded          bool
	Snapshot          bool
	DegradedProviders []string
	RecallDiagnostics []RecallDiagnostic
	SessionSemantic   *SessionSemanticEvidence
	CreatedAt         time.Time
}

type RecommendationRequestLog struct {
	ID                int64
	RequestID         string
	UserID            int64
	Scene             string
	PolicyVersion     int
	Context           *RecommendationContext
	Candidates        []LoggedCandidate
	Degraded          bool
	Snapshot          bool
	DegradedProviders []string
	RecallDiagnostics []RecallDiagnostic
	SessionSemantic   *SessionSemanticEvidence
	CreatedAt         time.Time
}

type Outcome struct {
	ID          string
	RequestID   string
	UserID      int64
	VideoID     int64
	OutcomeType string
	// OccurredAt is the source-event time used by profile ordering and decay.
	OccurredAt time.Time
	// RecordedAt is the trusted server receipt time used for attribution.
	RecordedAt time.Time
}

func NewOutcome(id, requestID string, userID, videoID int64, outcomeType string, occurredAt time.Time) (*Outcome, error) {
	return NewOutcomeWithRecordedAt(id, requestID, userID, videoID, outcomeType, occurredAt, occurredAt)
}

// NewOutcomeWithRecordedAt keeps the source occurrence time separate from the
// server-recorded time used to decide whether a served-candidate window covers
// the event.
func NewOutcomeWithRecordedAt(id, requestID string, userID, videoID int64, outcomeType string, occurredAt, recordedAt time.Time) (*Outcome, error) {
	id, requestID, outcomeType = strings.TrimSpace(id), strings.TrimSpace(requestID), strings.ToLower(strings.TrimSpace(outcomeType))
	if id == "" || len(id) > maxOutcomeIDLength || requestID == "" || len(requestID) > MaxRequestIDLength || userID <= 0 || videoID <= 0 || occurredAt.IsZero() || recordedAt.IsZero() {
		return nil, ErrInvalidRequestLog
	}
	switch outcomeType {
	case "exposed", "play", "progress", "complete", "skip", "like", "favorite", "follow", FeedbackTypeNotInterested, FeedbackTypeReduceAuthor, FeedbackTypeAlreadySeen:
	default:
		return nil, ErrInvalidRequestLog
	}
	return &Outcome{
		ID: id, RequestID: requestID, UserID: userID, VideoID: videoID, OutcomeType: outcomeType,
		OccurredAt: occurredAt.UTC(), RecordedAt: recordedAt.UTC(),
	}, nil
}

// OutcomeID derives a stable, bounded idempotency key from a durable source
// event. Normal IDs remain readable; only overlong values use SHA-256.
func OutcomeID(prefix, eventID string) string {
	prefix = strings.ToLower(strings.TrimSpace(prefix))
	eventID = strings.TrimSpace(eventID)
	value := prefix + ":" + eventID
	if len(value) <= maxOutcomeIDLength {
		return value
	}
	sum := sha256.Sum256([]byte(value))
	if len(prefix)+1+hex.EncodedLen(len(sum)) <= maxOutcomeIDLength {
		return prefix + ":" + hex.EncodeToString(sum[:])
	}
	return "outcome:" + hex.EncodeToString(sum[:])
}

// ViewOutcomeID keeps client-supplied view event IDs scoped to their owner.
// View event IDs are only unique per user, unlike server-generated action
// events, so their outcome keys must not collide across users.
func ViewOutcomeID(userID int64, eventID string) string {
	payload := "view\x00" + int64String(userID) + "\x00" + strings.TrimSpace(eventID)
	sum := sha256.Sum256([]byte(payload))
	return "view:" + hex.EncodeToString(sum[:])
}

// OutcomeAttributionPending reports whether delayed durable evidence can still
// validate an otherwise unverified outcome.
func OutcomeAttributionPending(recordedAt, now time.Time) bool {
	if recordedAt.IsZero() {
		return false
	}
	return now.UTC().Before(recordedAt.UTC().Add(OutcomeAttributionEvidenceWindow))
}

// LoggedCandidatesFromRanked preserves bounded, internal rank explanations for
// a sampled request log. Feed DTO conversion never calls this helper.
func LoggedCandidatesFromRanked(candidates []*Candidate) []LoggedCandidate {
	logged := make([]LoggedCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate == nil || candidate.VideoID <= 0 {
			continue
		}
		reasons := make([]string, 0, len(candidate.RecallReasons))
		for _, reason := range candidate.RecallReasons {
			if provider := normalizePolicyToken(reason.Provider); provider != "" {
				reasons = append(reasons, provider)
			}
		}
		components := make(map[string]float64, len(candidate.ScoreComponents))
		for name, value := range candidate.ScoreComponents {
			components[normalizePolicyToken(name)] = value
		}
		logged = append(logged, LoggedCandidate{
			VideoID: candidate.VideoID, Reasons: reasons, ScoreComponents: components,
		})
	}
	return logged
}

func NewRequestLogControl(samplingRatePPM int, retentionDays int) (RequestLogControl, error) {
	if samplingRatePPM < 0 || samplingRatePPM > MaxSamplingRatePPM || retentionDays <= 0 || retentionDays > MaxRetentionDays {
		return RequestLogControl{}, ErrInvalidPolicyBound
	}
	return RequestLogControl{SamplingRatePPM: samplingRatePPM, RetentionDays: retentionDays}, nil
}

func ShouldSampleRequestLog(control RequestLogControl, userID int64, scene string, requestID string) bool {
	if control.SamplingRatePPM <= 0 {
		return false
	}
	if control.SamplingRatePPM >= MaxSamplingRatePPM {
		return true
	}
	hasher := fnv.New64a()
	_, _ = hasher.Write([]byte(int64String(userID)))
	_, _ = hasher.Write([]byte("|" + strings.ToLower(strings.TrimSpace(scene)) + "|" + strings.TrimSpace(requestID)))
	return int(hasher.Sum64()%MaxSamplingRatePPM) < control.SamplingRatePPM
}

func NewRecommendationRequestLog(input RequestLogInput) (*RecommendationRequestLog, error) {
	requestID := strings.TrimSpace(input.RequestID)
	scene := strings.ToLower(strings.TrimSpace(input.Scene))
	if input.UserID <= 0 || requestID == "" || len(requestID) > MaxRequestIDLength || scene != RecommendationRequestLogScene ||
		input.PolicyVersion <= 0 || input.PolicyVersion > MaxPolicyVersion || input.CreatedAt.IsZero() ||
		len(input.Candidates) > MaxRequestLogCandidates {
		return nil, ErrInvalidRequestLog
	}
	var recommendationContext *RecommendationContext
	if input.Context != nil {
		var err error
		recommendationContext, err = NewRecommendationContext(RecommendationContextInput{
			RequestID: input.Context.RequestID, SessionID: input.Context.SessionID, RefreshIndex: input.Context.RefreshIndex,
			RecentVideoIDs: input.Context.RecentVideoIDs, CurrentVideoID: input.Context.CurrentVideoID,
			NetworkClass: input.Context.NetworkClass, SaveData: input.Context.SaveData, ViewportClass: input.Context.ViewportClass,
			PlaybackCapabilities: input.Context.PlaybackCapabilities,
		})
		if err != nil {
			return nil, ErrInvalidRequestLog
		}
	}
	candidates := make([]LoggedCandidate, 0, len(input.Candidates))
	for _, candidate := range input.Candidates {
		if candidate.VideoID <= 0 || len(candidate.Reasons) > MaxRequestLogReasons || len(candidate.ScoreComponents) > MaxRequestLogScoreComponents {
			return nil, ErrInvalidRequestLog
		}
		cloned := LoggedCandidate{VideoID: candidate.VideoID, Reasons: make([]string, 0, len(candidate.Reasons)), ScoreComponents: make(map[string]float64, len(candidate.ScoreComponents))}
		for _, reason := range candidate.Reasons {
			reason = strings.ToLower(strings.TrimSpace(reason))
			if reason == "" || len(reason) > MaxRequestLogReasonLength {
				return nil, ErrInvalidRequestLog
			}
			cloned.Reasons = append(cloned.Reasons, reason)
		}
		for name, value := range candidate.ScoreComponents {
			name = normalizePolicyToken(name)
			if !validPolicyFeature(name) || math.IsNaN(value) || math.IsInf(value, 0) || math.Abs(value) > MaxFeatureWeight {
				return nil, ErrInvalidRequestLog
			}
			cloned.ScoreComponents[name] = value
		}
		candidates = append(candidates, cloned)
	}
	if len(input.RecallDiagnostics) > MaxRequestLogRecallDiagnostics {
		return nil, ErrInvalidRequestLog
	}
	diagnostics := make([]RecallDiagnostic, 0, len(input.RecallDiagnostics))
	for _, diagnostic := range input.RecallDiagnostics {
		diagnostic.Phase = normalizePolicyToken(diagnostic.Phase)
		diagnostic.Provider = normalizePolicyToken(diagnostic.Provider)
		diagnostic.Result = normalizePolicyToken(diagnostic.Result)
		diagnostic.Reason = normalizePolicyToken(diagnostic.Reason)
		if !validRecallDiagnosticPhase(diagnostic.Phase) ||
			(diagnostic.Provider != "all" && !validRecallProvider(diagnostic.Provider)) ||
			!validRecallDiagnosticResult(diagnostic.Result) ||
			!validRecallDiagnosticReason(diagnostic.Reason) ||
			diagnostic.Count < 0 || diagnostic.Count > MaxRequestLogDiagnosticCount {
			return nil, ErrInvalidRequestLog
		}
		diagnostics = append(diagnostics, diagnostic)
	}
	var sessionSemantic *SessionSemanticEvidence
	if input.SessionSemantic != nil {
		var err error
		sessionSemantic, err = NewSessionSemanticEvidence(*input.SessionSemantic)
		if err != nil {
			return nil, ErrInvalidRequestLog
		}
	}
	log := &RecommendationRequestLog{
		RequestID: requestID, UserID: input.UserID, Scene: scene, PolicyVersion: input.PolicyVersion,
		Context: recommendationContext, Candidates: candidates, Degraded: input.Degraded, Snapshot: input.Snapshot,
		DegradedProviders: append([]string(nil), input.DegradedProviders...), RecallDiagnostics: diagnostics,
		SessionSemantic: sessionSemantic, CreatedAt: input.CreatedAt.UTC(),
	}
	if _, err := log.CompactPayload(); err != nil {
		return nil, err
	}
	return log, nil
}

func (l *RecommendationRequestLog) CompactPayload() ([]byte, error) {
	if l == nil {
		return nil, ErrInvalidRequestLog
	}
	payload := struct {
		Context           *RecommendationContext   `json:"context,omitempty"`
		Candidates        []LoggedCandidate        `json:"candidates"`
		Degraded          bool                     `json:"degraded"`
		Snapshot          bool                     `json:"snapshot"`
		DegradedProviders []string                 `json:"degraded_providers,omitempty"`
		RecallDiagnostics []RecallDiagnostic       `json:"recall_diagnostics,omitempty"`
		SessionSemantic   *SessionSemanticEvidence `json:"session_semantic,omitempty"`
	}{
		Context: l.Context.Clone(), Candidates: l.Candidates, Degraded: l.Degraded, Snapshot: l.Snapshot,
		DegradedProviders: l.DegradedProviders, RecallDiagnostics: l.RecallDiagnostics,
		SessionSemantic: l.SessionSemantic.Clone(),
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, ErrInvalidRequestLog
	}
	if len(encoded) > MaxRequestLogPayloadBytes {
		return nil, ErrRequestLogPayloadTooLarge
	}
	return encoded, nil
}

func validRecallDiagnosticPhase(value string) bool {
	switch value {
	case "provider", "normalization", "visibility", "reservation", "fill", "final":
		return true
	default:
		return false
	}
}

func validRecallDiagnosticResult(value string) bool {
	switch value {
	case "returned", "local_unique", "readable", "reserved", "fill_selected", "selected", "represented", "overlap", "exhausted", "underfill":
		return true
	default:
		return false
	}
}

func validRecallDiagnosticReason(value string) bool {
	switch value {
	case "", "none", "insufficient_readable":
		return true
	default:
		return false
	}
}
