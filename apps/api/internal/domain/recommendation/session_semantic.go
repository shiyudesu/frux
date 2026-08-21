package domainrecommendation

import (
	"crypto/sha256"
	"encoding/hex"
	"math"
	"strings"
	"time"
)

const (
	SessionSemanticBuilderV1 = "session-semantic-v1"

	MinSessionSemanticLookbackSeconds = 60
	MaxSessionSemanticLookbackSeconds = 24 * 60 * 60
	MaxSessionSemanticSeeds           = MaxRecentVideoIDs + 1
	MaxSessionSemanticSignalCount     = MaxSessionSemanticSeeds * 8
	SessionSemanticDigestHexLength    = sha256.Size * 2
)

type SessionSemanticSignalKind string

const (
	SessionSemanticSignalCurrent       SessionSemanticSignalKind = "current"
	SessionSemanticSignalComplete      SessionSemanticSignalKind = "complete"
	SessionSemanticSignalSustained     SessionSemanticSignalKind = "sustained"
	SessionSemanticSignalLike          SessionSemanticSignalKind = "like"
	SessionSemanticSignalFavorite      SessionSemanticSignalKind = "favorite"
	SessionSemanticSignalEarlySkip     SessionSemanticSignalKind = "early_skip"
	SessionSemanticSignalNotInterested SessionSemanticSignalKind = "not_interested"
	SessionSemanticSignalAlreadySeen   SessionSemanticSignalKind = "already_seen"
)

func ValidSessionSemanticSignalKind(kind SessionSemanticSignalKind) bool {
	switch kind {
	case SessionSemanticSignalCurrent, SessionSemanticSignalComplete,
		SessionSemanticSignalSustained, SessionSemanticSignalLike,
		SessionSemanticSignalFavorite, SessionSemanticSignalEarlySkip,
		SessionSemanticSignalNotInterested, SessionSemanticSignalAlreadySeen:
		return true
	default:
		return false
	}
}

type SessionSemanticResult string

const (
	SessionSemanticResultSuccess              SessionSemanticResult = "success"
	SessionSemanticResultInsufficientEvidence SessionSemanticResult = "insufficient_evidence"
	SessionSemanticResultNoCompatibleVectors  SessionSemanticResult = "no_compatible_vectors"
	SessionSemanticResultLowConfidence        SessionSemanticResult = "low_confidence"
	SessionSemanticResultContractMismatch     SessionSemanticResult = "contract_mismatch"
	SessionSemanticResultInvalidVector        SessionSemanticResult = "invalid_vector"
	SessionSemanticResultTimeout              SessionSemanticResult = "timeout"
	SessionSemanticResultUnavailable          SessionSemanticResult = "unavailable"
)

func ValidSessionSemanticResult(result SessionSemanticResult) bool {
	switch result {
	case SessionSemanticResultSuccess, SessionSemanticResultInsufficientEvidence,
		SessionSemanticResultNoCompatibleVectors, SessionSemanticResultLowConfidence,
		SessionSemanticResultContractMismatch, SessionSemanticResultInvalidVector,
		SessionSemanticResultTimeout, SessionSemanticResultUnavailable:
		return true
	default:
		return false
	}
}

type SessionSemanticConfidenceBand string

const (
	SessionSemanticConfidenceNone   SessionSemanticConfidenceBand = "none"
	SessionSemanticConfidenceLow    SessionSemanticConfidenceBand = "low"
	SessionSemanticConfidenceMedium SessionSemanticConfidenceBand = "medium"
	SessionSemanticConfidenceHigh   SessionSemanticConfidenceBand = "high"
)

func ValidSessionSemanticConfidenceBand(band SessionSemanticConfidenceBand) bool {
	switch band {
	case SessionSemanticConfidenceNone, SessionSemanticConfidenceLow,
		SessionSemanticConfidenceMedium, SessionSemanticConfidenceHigh:
		return true
	default:
		return false
	}
}

type SessionSemanticPolicyConfiguration struct {
	BuilderVersion     string  `json:"builder_version"`
	ContractKey        string  `json:"contract_key"`
	LookbackSeconds    int     `json:"lookback_seconds"`
	MaxSeeds           int     `json:"max_seeds"`
	MinPositiveSignals int     `json:"min_positive_signals"`
	MinConfidence      float64 `json:"min_confidence"`
}

func (c *SessionSemanticPolicyConfiguration) Clone() *SessionSemanticPolicyConfiguration {
	if c == nil {
		return nil
	}
	cloned := *c
	return &cloned
}

func normalizeSessionSemanticPolicyConfiguration(
	config *SessionSemanticPolicyConfiguration,
) (*SessionSemanticPolicyConfiguration, error) {
	if config == nil {
		return nil, nil
	}
	normalized := &SessionSemanticPolicyConfiguration{
		BuilderVersion:     strings.ToLower(strings.TrimSpace(config.BuilderVersion)),
		ContractKey:        strings.ToLower(strings.TrimSpace(config.ContractKey)),
		LookbackSeconds:    config.LookbackSeconds,
		MaxSeeds:           config.MaxSeeds,
		MinPositiveSignals: config.MinPositiveSignals,
		MinConfidence:      config.MinConfidence,
	}
	if normalized.BuilderVersion != SessionSemanticBuilderV1 ||
		!validSessionSemanticContractKey(normalized.ContractKey) ||
		normalized.LookbackSeconds < MinSessionSemanticLookbackSeconds ||
		normalized.LookbackSeconds > MaxSessionSemanticLookbackSeconds ||
		normalized.MaxSeeds < 1 || normalized.MaxSeeds > MaxSessionSemanticSeeds ||
		normalized.MinPositiveSignals < 1 || normalized.MinPositiveSignals > normalized.MaxSeeds*5 ||
		math.IsNaN(normalized.MinConfidence) || math.IsInf(normalized.MinConfidence, 0) ||
		normalized.MinConfidence <= 0 || normalized.MinConfidence > 1 {
		return nil, ErrInvalidSessionSemanticPolicy
	}
	return normalized, nil
}

func ValidateSessionSemanticPolicyConfiguration(
	config *SessionSemanticPolicyConfiguration,
) (*SessionSemanticPolicyConfiguration, error) {
	return normalizeSessionSemanticPolicyConfiguration(config)
}

func validSessionSemanticContractKey(value string) bool {
	if len(value) != SessionSemanticDigestHexLength {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && value == strings.ToLower(value)
}

type SessionSemanticSignal struct {
	VideoID    int64
	Kind       SessionSemanticSignalKind
	OccurredAt time.Time
}

func (s SessionSemanticSignal) Valid() bool {
	return s.VideoID > 0 && ValidSessionSemanticSignalKind(s.Kind) && !s.OccurredAt.IsZero()
}

type SessionSemanticEvidence struct {
	BuilderVersion  string                        `json:"builder_version"`
	ContractKey     string                        `json:"contract_key"`
	Result          SessionSemanticResult         `json:"result"`
	Confidence      float64                       `json:"confidence"`
	ConfidenceBand  SessionSemanticConfidenceBand `json:"confidence_band"`
	EligibleCount   int                           `json:"eligible_count"`
	PositiveCount   int                           `json:"positive_count"`
	NegativeCount   int                           `json:"negative_count"`
	CompatibleCount int                           `json:"compatible_count"`
	ExcludedCount   int                           `json:"excluded_count"`
	InputDigest     string                        `json:"input_digest,omitempty"`
}

func NewSessionSemanticEvidence(value SessionSemanticEvidence) (*SessionSemanticEvidence, error) {
	value.BuilderVersion = strings.ToLower(strings.TrimSpace(value.BuilderVersion))
	value.ContractKey = strings.ToLower(strings.TrimSpace(value.ContractKey))
	value.InputDigest = strings.ToLower(strings.TrimSpace(value.InputDigest))
	if value.BuilderVersion != SessionSemanticBuilderV1 || !validSessionSemanticContractKey(value.ContractKey) ||
		!ValidSessionSemanticResult(value.Result) || !ValidSessionSemanticConfidenceBand(value.ConfidenceBand) ||
		math.IsNaN(value.Confidence) || math.IsInf(value.Confidence, 0) || value.Confidence < 0 || value.Confidence > 1 ||
		value.EligibleCount < 0 || value.EligibleCount > MaxSessionSemanticSeeds ||
		value.PositiveCount < 0 || value.PositiveCount > MaxSessionSemanticSignalCount ||
		value.NegativeCount < 0 || value.NegativeCount > MaxSessionSemanticSignalCount ||
		value.CompatibleCount < 0 || value.CompatibleCount > MaxSessionSemanticSeeds ||
		value.ExcludedCount < 0 || value.ExcludedCount > MaxSessionSemanticSeeds ||
		(value.InputDigest != "" && !validSessionSemanticContractKey(value.InputDigest)) {
		return nil, ErrInvalidSessionSemanticEvidence
	}
	if value.Result == SessionSemanticResultSuccess {
		if value.Confidence <= 0 || value.ConfidenceBand == SessionSemanticConfidenceNone || value.InputDigest == "" {
			return nil, ErrInvalidSessionSemanticEvidence
		}
	} else if value.Confidence == 0 {
		value.ConfidenceBand = SessionSemanticConfidenceNone
	}
	cloned := value
	return &cloned, nil
}

func (e *SessionSemanticEvidence) Clone() *SessionSemanticEvidence {
	if e == nil {
		return nil
	}
	cloned := *e
	return &cloned
}
