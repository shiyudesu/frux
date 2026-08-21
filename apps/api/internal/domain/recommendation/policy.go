package domainrecommendation

import (
	"hash/fnv"
	"math"
	"sort"
	"strings"
	"time"
)

const (
	MaxPolicyVersion           = 1_000_000_000
	MaxFeatureWeight           = 100
	MaxTotalFeatureWeight      = 1_000
	MaxRecallBudget            = 500
	MinPolicyPreRankCandidates = 50
	MaxPolicyPreRankCandidates = MaxRequestLogCandidates
	MaxProviderDeadlineMS      = 30_000
	MaxFreshnessHalfLifeHours  = 8_760
	MaxProfileHalfLifeHours    = 8_760
	MaxExposureWindowHours     = 8_760
	MaxSnapshotTTLSeconds      = 86_400
	MaxRetentionDays           = 3_650
	MaxSamplingRatePPM         = 1_000_000
	MaxDiversityPerAuthor      = 100
	MaxDiversityAuthorGap      = 100
	MaxDiversityContentGap     = 100
	MaxMinimumFallbackPool     = 500
	MaxSuppressionHours        = 8_760
)

const (
	FeatureContentSimilarity  = "content_similarity"
	FeatureSessionSimilarity  = "session_similarity"
	FeatureHotness            = "hotness"
	FeatureFreshness          = "freshness"
	FeatureAuthorAffinity     = "author_affinity"
	FeatureFollowRelation     = "follow_relation"
	FeatureNegativePenalty    = "negative_penalty"
	FeatureExposurePenalty    = "exposure_penalty"
	FeatureSemanticSimilarity = "semantic_similarity"

	RecallProviderFresh               = "fresh"
	RecallProviderHot                 = "hot"
	RecallProviderContentSimilarity   = "content_similarity"
	RecallProviderFollowedAuthor      = "followed_author"
	RecallProviderSessionContinuation = "session_continuation"
	RecallProviderSemanticSession     = "semantic_session"
)

type DiversityRules struct {
	MaxPerAuthor  int `json:"max_per_author"`
	MinAuthorGap  int `json:"min_author_gap"`
	MinContentGap int `json:"min_content_gap"`
}

type PolicyConfiguration struct {
	FeatureWeights               map[string]float64                  `json:"feature_weights"`
	RecallBudgets                map[string]int                      `json:"recall_budgets"`
	ProviderDeadlinesMS          map[string]int                      `json:"provider_deadlines_ms"`
	PreRankPoolLimit             int                                 `json:"pre_rank_pool_limit,omitempty"`
	RecallProviderOrder          []string                            `json:"recall_provider_order,omitempty"`
	RecallProviderReservations   map[string]int                      `json:"recall_provider_reservations,omitempty"`
	FreshnessHalfLifeHours       int                                 `json:"freshness_half_life_hours"`
	ProfileLongTermHalfLifeHours int                                 `json:"profile_long_term_half_life_hours"`
	ProfileRecentHalfLifeHours   int                                 `json:"profile_recent_half_life_hours"`
	ExposureWindowHours          int                                 `json:"exposure_window_hours"`
	Diversity                    DiversityRules                      `json:"diversity"`
	RolloutPercentage            int                                 `json:"rollout_percentage"`
	SnapshotTTLSeconds           int                                 `json:"snapshot_ttl_seconds"`
	SamplingRatePPM              int                                 `json:"sampling_rate_ppm"`
	RetentionDays                int                                 `json:"retention_days"`
	MinimumFallbackPool          int                                 `json:"minimum_fallback_pool"`
	HardSuppressExposures        bool                                `json:"hard_suppress_exposures"`
	SuppressionHours             map[string]int                      `json:"suppression_hours"`
	SessionSemantic              *SessionSemanticPolicyConfiguration `json:"session_semantic,omitempty"`
}

type Policy struct {
	ID        int64
	Scene     string
	Version   int
	Enabled   bool
	Config    PolicyConfiguration
	CreatedAt time.Time
	UpdatedAt time.Time
}

func NewPolicy(scene string, version int, enabled bool, config PolicyConfiguration, now time.Time) (*Policy, error) {
	scene = strings.ToLower(strings.TrimSpace(scene))
	if scene == "" {
		return nil, ErrEmptyScene
	}
	if len(scene) > MaxSceneLength {
		return nil, ErrSceneTooLong
	}
	if version <= 0 || version > MaxPolicyVersion {
		return nil, ErrInvalidPolicyVersion
	}
	if now.IsZero() {
		return nil, ErrInvalidCreatedAt
	}
	normalized, err := normalizePolicyConfiguration(config)
	if err != nil {
		return nil, err
	}
	return &Policy{
		Scene: scene, Version: version, Enabled: enabled, Config: normalized,
		CreatedAt: now.UTC(), UpdatedAt: now.UTC(),
	}, nil
}

func RestorePolicy(id int64, scene string, version int, enabled bool, config PolicyConfiguration, createdAt time.Time, updatedAt time.Time) *Policy {
	normalized, err := normalizePolicyConfiguration(config)
	if err != nil {
		return nil
	}
	return &Policy{
		ID: id, Scene: strings.ToLower(strings.TrimSpace(scene)), Version: version, Enabled: enabled,
		Config: normalized, CreatedAt: createdAt.UTC(), UpdatedAt: updatedAt.UTC(),
	}
}

func (p *Policy) Clone() *Policy {
	if p == nil {
		return nil
	}
	cloned := *p
	cloned.Config = clonePolicyConfiguration(p.Config)
	return &cloned
}

func SelectPolicy(policies []*Policy, userID int64, requestID string) *Policy {
	enabled := make([]*Policy, 0, len(policies))
	for _, policy := range policies {
		if policy != nil && policy.Enabled {
			enabled = append(enabled, policy.Clone())
		}
	}
	sort.SliceStable(enabled, func(i, j int) bool { return enabled[i].Version > enabled[j].Version })
	for _, policy := range enabled {
		if policy.Config.RolloutPercentage >= 100 || cohortPercent(userID, policy.Scene, requestID) < policy.Config.RolloutPercentage {
			return policy
		}
	}
	return nil
}

func cohortPercent(userID int64, scene string, requestID string) int {
	hasher := fnv.New64a()
	_, _ = hasher.Write([]byte(strings.TrimSpace(scene)))
	_, _ = hasher.Write([]byte("|"))
	_, _ = hasher.Write([]byte(strings.TrimSpace(requestID)))
	_, _ = hasher.Write([]byte("|"))
	_, _ = hasher.Write([]byte(strings.TrimSpace(int64String(userID))))
	return int(hasher.Sum64() % 100)
}

func int64String(value int64) string {
	if value == 0 {
		return "0"
	}
	const digits = "0123456789"
	negative := value < 0
	var output [20]byte
	index := len(output)
	for value != 0 {
		remainder := value % 10
		if remainder < 0 {
			remainder = -remainder
		}
		index--
		output[index] = digits[remainder]
		value /= 10
	}
	if negative {
		index--
		output[index] = '-'
	}
	return string(output[index:])
}

func normalizePolicyConfiguration(config PolicyConfiguration) (PolicyConfiguration, error) {
	if len(config.FeatureWeights) == 0 || len(config.RecallBudgets) == 0 {
		return PolicyConfiguration{}, ErrInvalidPolicyConfiguration
	}
	normalized := PolicyConfiguration{
		FeatureWeights:               make(map[string]float64, len(config.FeatureWeights)),
		RecallBudgets:                make(map[string]int, len(config.RecallBudgets)),
		ProviderDeadlinesMS:          make(map[string]int, len(config.ProviderDeadlinesMS)),
		PreRankPoolLimit:             config.PreRankPoolLimit,
		Diversity:                    config.Diversity,
		FreshnessHalfLifeHours:       config.FreshnessHalfLifeHours,
		ProfileLongTermHalfLifeHours: config.ProfileLongTermHalfLifeHours,
		ProfileRecentHalfLifeHours:   config.ProfileRecentHalfLifeHours,
		ExposureWindowHours:          config.ExposureWindowHours,
		RolloutPercentage:            config.RolloutPercentage,
		SnapshotTTLSeconds:           config.SnapshotTTLSeconds,
		SamplingRatePPM:              config.SamplingRatePPM,
		RetentionDays:                config.RetentionDays,
		MinimumFallbackPool:          config.MinimumFallbackPool,
		HardSuppressExposures:        config.HardSuppressExposures,
		SuppressionHours:             make(map[string]int, len(config.SuppressionHours)),
	}
	var err error
	if normalized.SessionSemantic, err = normalizeSessionSemanticPolicyConfiguration(config.SessionSemantic); err != nil {
		return PolicyConfiguration{}, err
	}
	if normalized.MinimumFallbackPool == 0 {
		normalized.MinimumFallbackPool = 1
	}
	if normalized.ProfileLongTermHalfLifeHours == 0 {
		normalized.ProfileLongTermHalfLifeHours = int(DefaultProfileLongTermHalfLife.Hours())
	}
	if normalized.ProfileRecentHalfLifeHours == 0 {
		normalized.ProfileRecentHalfLifeHours = int(DefaultProfileRecentHalfLife.Hours())
	}
	for feedbackType, hours := range config.SuppressionHours {
		feedbackType = normalizePolicyToken(feedbackType)
		if !ValidFeedbackType(feedbackType) || hours <= 0 || hours > MaxSuppressionHours {
			return PolicyConfiguration{}, ErrInvalidPolicyBound
		}
		normalized.SuppressionHours[feedbackType] = hours
	}
	for feedbackType, hours := range defaultSuppressionHours() {
		if normalized.SuppressionHours[feedbackType] == 0 {
			normalized.SuppressionHours[feedbackType] = hours
		}
	}
	var totalWeight float64
	for name, weight := range config.FeatureWeights {
		name = normalizePolicyToken(name)
		if !validPolicyFeature(name) {
			return PolicyConfiguration{}, ErrUnknownPolicyFeature
		}
		if math.IsNaN(weight) || math.IsInf(weight, 0) || math.Abs(weight) > MaxFeatureWeight {
			return PolicyConfiguration{}, ErrInvalidPolicyBound
		}
		normalized.FeatureWeights[name] = weight
		totalWeight += math.Abs(weight)
	}
	if totalWeight > MaxTotalFeatureWeight {
		return PolicyConfiguration{}, ErrInvalidPolicyBound
	}
	totalRecallBudget := 0
	for provider, budget := range config.RecallBudgets {
		provider = normalizePolicyToken(provider)
		if !validRecallProvider(provider) {
			return PolicyConfiguration{}, ErrUnknownRecallProvider
		}
		if budget <= 0 || budget > MaxRecallBudget {
			return PolicyConfiguration{}, ErrInvalidPolicyBound
		}
		totalRecallBudget += budget
		normalized.RecallBudgets[provider] = budget
	}
	if totalRecallBudget <= 0 {
		return PolicyConfiguration{}, ErrInvalidPolicyBound
	}
	semanticBudget, semanticProviderSelected := normalized.RecallBudgets[RecallProviderSemanticSession]
	semanticWeight, semanticFeatureSelected := normalized.FeatureWeights[FeatureSemanticSimilarity]
	semanticConfigured := normalized.SessionSemantic != nil
	if semanticProviderSelected || semanticFeatureSelected || semanticConfigured {
		if !semanticProviderSelected || semanticBudget <= 0 || !semanticFeatureSelected || semanticWeight <= 0 || !semanticConfigured {
			return PolicyConfiguration{}, ErrInvalidSessionSemanticPolicy
		}
	}
	if len(config.ProviderDeadlinesMS) != len(normalized.RecallBudgets) {
		return PolicyConfiguration{}, ErrInvalidPolicyConfiguration
	}
	for provider, deadline := range config.ProviderDeadlinesMS {
		provider = normalizePolicyToken(provider)
		if !validRecallProvider(provider) {
			return PolicyConfiguration{}, ErrUnknownRecallProvider
		}
		if _, exists := normalized.RecallBudgets[provider]; !exists || deadline <= 0 || deadline > MaxProviderDeadlineMS {
			return PolicyConfiguration{}, ErrInvalidPolicyBound
		}
		normalized.ProviderDeadlinesMS[provider] = deadline
	}
	if quotaMergeConfigured(config) {
		if err := normalizeQuotaMergeConfiguration(config, &normalized); err != nil {
			return PolicyConfiguration{}, err
		}
	} else if totalRecallBudget > MaxPolicyPreRankCandidates {
		return PolicyConfiguration{}, ErrInvalidPolicyBound
	}
	if normalized.FreshnessHalfLifeHours <= 0 || normalized.FreshnessHalfLifeHours > MaxFreshnessHalfLifeHours ||
		normalized.ProfileLongTermHalfLifeHours <= 0 || normalized.ProfileLongTermHalfLifeHours > MaxProfileHalfLifeHours ||
		normalized.ProfileRecentHalfLifeHours <= 0 || normalized.ProfileRecentHalfLifeHours > MaxProfileHalfLifeHours ||
		normalized.ExposureWindowHours <= 0 || normalized.ExposureWindowHours > MaxExposureWindowHours ||
		normalized.Diversity.MaxPerAuthor <= 0 || normalized.Diversity.MaxPerAuthor > MaxDiversityPerAuthor ||
		normalized.Diversity.MinAuthorGap < 0 || normalized.Diversity.MinAuthorGap > MaxDiversityAuthorGap ||
		normalized.Diversity.MinContentGap < 0 || normalized.Diversity.MinContentGap > MaxDiversityContentGap ||
		normalized.RolloutPercentage < 0 || normalized.RolloutPercentage > 100 ||
		normalized.SnapshotTTLSeconds <= 0 || normalized.SnapshotTTLSeconds > MaxSnapshotTTLSeconds ||
		normalized.SamplingRatePPM < 0 || normalized.SamplingRatePPM > MaxSamplingRatePPM ||
		normalized.RetentionDays <= 0 || normalized.RetentionDays > MaxRetentionDays ||
		normalized.MinimumFallbackPool <= 0 || normalized.MinimumFallbackPool > MaxMinimumFallbackPool {
		return PolicyConfiguration{}, ErrInvalidPolicyBound
	}
	return normalized, nil
}

func clonePolicyConfiguration(config PolicyConfiguration) PolicyConfiguration {
	cloned := config
	cloned.FeatureWeights = make(map[string]float64, len(config.FeatureWeights))
	for key, value := range config.FeatureWeights {
		cloned.FeatureWeights[key] = value
	}
	cloned.RecallBudgets = make(map[string]int, len(config.RecallBudgets))
	for key, value := range config.RecallBudgets {
		cloned.RecallBudgets[key] = value
	}
	cloned.ProviderDeadlinesMS = make(map[string]int, len(config.ProviderDeadlinesMS))
	for key, value := range config.ProviderDeadlinesMS {
		cloned.ProviderDeadlinesMS[key] = value
	}
	if config.RecallProviderOrder != nil {
		cloned.RecallProviderOrder = append([]string(nil), config.RecallProviderOrder...)
	}
	if config.RecallProviderReservations != nil {
		cloned.RecallProviderReservations = make(map[string]int, len(config.RecallProviderReservations))
		for key, value := range config.RecallProviderReservations {
			cloned.RecallProviderReservations[key] = value
		}
	}
	cloned.SuppressionHours = make(map[string]int, len(config.SuppressionHours))
	for key, value := range config.SuppressionHours {
		cloned.SuppressionHours[key] = value
	}
	cloned.SessionSemantic = config.SessionSemantic.Clone()
	return cloned
}

func quotaMergeConfigured(config PolicyConfiguration) bool {
	return config.PreRankPoolLimit != 0 || config.RecallProviderOrder != nil || config.RecallProviderReservations != nil
}

func normalizeQuotaMergeConfiguration(config PolicyConfiguration, normalized *PolicyConfiguration) error {
	if normalized == nil || config.PreRankPoolLimit < MinPolicyPreRankCandidates ||
		config.PreRankPoolLimit > MaxPolicyPreRankCandidates ||
		len(config.RecallProviderOrder) != len(normalized.RecallBudgets) ||
		len(config.RecallProviderReservations) != len(normalized.RecallBudgets) {
		return ErrInvalidPolicyConfiguration
	}

	normalized.RecallProviderOrder = make([]string, 0, len(config.RecallProviderOrder))
	seenOrder := make(map[string]struct{}, len(config.RecallProviderOrder))
	for _, rawProvider := range config.RecallProviderOrder {
		provider := normalizePolicyToken(rawProvider)
		if !validRecallProvider(provider) {
			return ErrUnknownRecallProvider
		}
		if _, selected := normalized.RecallBudgets[provider]; !selected {
			return ErrInvalidPolicyConfiguration
		}
		if _, duplicate := seenOrder[provider]; duplicate {
			return ErrInvalidPolicyConfiguration
		}
		seenOrder[provider] = struct{}{}
		normalized.RecallProviderOrder = append(normalized.RecallProviderOrder, provider)
	}

	normalized.RecallProviderReservations = make(map[string]int, len(config.RecallProviderReservations))
	totalReservations := 0
	for rawProvider, reservation := range config.RecallProviderReservations {
		provider := normalizePolicyToken(rawProvider)
		if !validRecallProvider(provider) {
			return ErrUnknownRecallProvider
		}
		budget, selected := normalized.RecallBudgets[provider]
		if !selected {
			return ErrInvalidPolicyConfiguration
		}
		if _, duplicate := normalized.RecallProviderReservations[provider]; duplicate {
			return ErrInvalidPolicyConfiguration
		}
		if reservation < 0 || reservation > budget || totalReservations > config.PreRankPoolLimit-reservation {
			return ErrInvalidPolicyBound
		}
		totalReservations += reservation
		normalized.RecallProviderReservations[provider] = reservation
	}
	if len(seenOrder) != len(normalized.RecallBudgets) ||
		len(normalized.RecallProviderReservations) != len(normalized.RecallBudgets) ||
		totalReservations > config.PreRankPoolLimit {
		return ErrInvalidPolicyConfiguration
	}
	return nil
}

func defaultSuppressionHours() map[string]int {
	return map[string]int{
		FeedbackTypeNotInterested: 24 * 30,
		FeedbackTypeReduceAuthor:  24 * 14,
		FeedbackTypeAlreadySeen:   24 * 7,
	}
}

// InitialRecommendationPolicies is the immutable bootstrap set used only
// when a scene/version has never been created. Operators remain free to
// create, activate, or roll back later versions without startup overwriting
// their choices.
func InitialRecommendationPolicies(now time.Time) ([]*Policy, error) {
	v1Config := InitialRecommendationPolicyConfiguration()
	v1, err := NewPolicy("recommend", 1, true, v1Config, now)
	if err != nil {
		return nil, err
	}

	// Version 2 changes only ranking/diversity defaults and is deliberately
	// cohort-limited. It remains independently reversible through the policy
	// repository rollback operation.
	v2Config := clonePolicyConfiguration(v1Config)
	v2Config.RolloutPercentage = 5
	v2Config.FeatureWeights[FeatureContentSimilarity] = 0.60
	v2Config.FeatureWeights[FeatureSessionSimilarity] = 0.30
	v2Config.FeatureWeights[FeatureFreshness] = 0.15
	v2Config.Diversity = DiversityRules{MaxPerAuthor: 6, MinAuthorGap: 1, MinContentGap: 1}
	v2, err := NewPolicy("recommend", 2, true, v2Config, now)
	if err != nil {
		return nil, err
	}
	return []*Policy{v1, v2}, nil
}

// InitialRecommendationPolicyConfiguration preserves the current ranking
// defaults for policy version 1. It is shared by the in-process fallback and
// the durable bootstrap policy so an empty policy table has no behavior gap.
func InitialRecommendationPolicyConfiguration() PolicyConfiguration {
	return PolicyConfiguration{
		FeatureWeights: map[string]float64{
			FeatureContentSimilarity: 0.70,
			FeatureHotness:           0.20,
			FeatureFreshness:         0.10,
			FeatureSessionSimilarity: 0.25,
			FeatureAuthorAffinity:    0.15,
			FeatureFollowRelation:    0.10,
			FeatureNegativePenalty:   -0.75,
			FeatureExposurePenalty:   -0.40,
		},
		RecallBudgets: map[string]int{
			RecallProviderFresh:               100,
			RecallProviderHot:                 100,
			RecallProviderContentSimilarity:   100,
			RecallProviderFollowedAuthor:      100,
			RecallProviderSessionContinuation: 100,
		},
		ProviderDeadlinesMS: map[string]int{
			RecallProviderFresh:               150,
			RecallProviderHot:                 150,
			RecallProviderContentSimilarity:   250,
			RecallProviderFollowedAuthor:      200,
			RecallProviderSessionContinuation: 250,
		},
		FreshnessHalfLifeHours:       72,
		ProfileLongTermHalfLifeHours: int(DefaultProfileLongTermHalfLife.Hours()),
		ProfileRecentHalfLifeHours:   int(DefaultProfileRecentHalfLife.Hours()),
		ExposureWindowHours:          int(RecentExposureWindow.Hours()),
		Diversity:                    DiversityRules{MaxPerAuthor: 10, MinAuthorGap: 1, MinContentGap: 1},
		RolloutPercentage:            100,
		SnapshotTTLSeconds:           300,
		SamplingRatePPM:              10_000,
		RetentionDays:                30,
		MinimumFallbackPool:          1,
		HardSuppressExposures:        true,
		SuppressionHours:             defaultSuppressionHours(),
	}
}

func normalizePolicyToken(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func validPolicyFeature(value string) bool {
	switch value {
	case FeatureContentSimilarity, FeatureSessionSimilarity, FeatureSemanticSimilarity, FeatureHotness, FeatureFreshness,
		FeatureAuthorAffinity, FeatureFollowRelation, FeatureNegativePenalty, FeatureExposurePenalty:
		return true
	default:
		return false
	}
}

func validRecallProvider(value string) bool {
	switch value {
	case RecallProviderFresh, RecallProviderHot, RecallProviderContentSimilarity,
		RecallProviderFollowedAuthor, RecallProviderSessionContinuation, RecallProviderSemanticSession:
		return true
	default:
		return false
	}
}
