package applicationrecommendation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	domainembedding "github.com/shiyudesu/frux/internal/domain/embedding"
	domainrecommendation "github.com/shiyudesu/frux/internal/domain/recommendation"
)

const SessionSemanticRolloutProfileV1 = "session-semantic-rollout-v1"
const MinSessionSemanticRolloutPercentage = 1
const MaxSessionSemanticRolloutPercentage = 5

var ErrInvalidSessionSemanticRollout = errors.New("invalid session semantic rollout")
var ErrSessionSemanticRolloutConflict = errors.New("session semantic rollout conflict")
var ErrSessionSemanticRolloutBlocked = errors.New("session semantic rollout blocked")

type SessionSemanticRolloutOptions struct {
	TargetVersion     int
	RolloutPercentage int
	Contract          domainembedding.MultimodalContractIdentity
	Now               time.Time
}

type SessionSemanticRolloutPlan struct {
	Profile       string
	SourceVersion int
	TargetVersion int
	Policy        *domainrecommendation.Policy
	ConfigDigest  string
	Changes       []string
	Cohort        SessionSemanticRolloutCohortSummary
}

func (p *SessionSemanticRolloutPlan) Clone() *SessionSemanticRolloutPlan {
	if p == nil {
		return nil
	}
	return &SessionSemanticRolloutPlan{
		Profile: p.Profile, SourceVersion: p.SourceVersion, TargetVersion: p.TargetVersion,
		Policy: p.Policy.Clone(), ConfigDigest: p.ConfigDigest,
		Changes: append([]string(nil), p.Changes...), Cohort: p.Cohort.Clone(),
	}
}

type SessionSemanticRolloutCohortSummary struct {
	Samples  int
	Selected int
	Fallback int
	Buckets  []int
}

func (s SessionSemanticRolloutCohortSummary) Clone() SessionSemanticRolloutCohortSummary {
	s.Buckets = append([]int(nil), s.Buckets...)
	return s
}

func BuildSessionSemanticRolloutPolicy(
	source *domainrecommendation.Policy,
	options SessionSemanticRolloutOptions,
) (*SessionSemanticRolloutPlan, error) {
	contract, contractErr := domainembedding.NewMultimodalContractIdentity(
		options.Contract.ProviderAlias, options.Contract.ModelAlias, options.Contract.RevisionAlias,
		options.Contract.Dimension, options.Contract.TextCanonicalizer,
		options.Contract.FrameSamplingPolicy, options.Contract.ImagePreprocessingPolicy,
		options.Contract.FusionPolicy,
	)
	if source == nil || strings.ToLower(strings.TrimSpace(source.Scene)) != domainrecommendation.RecommendationRequestLogScene ||
		options.TargetVersion <= source.Version || options.TargetVersion > domainrecommendation.MaxPolicyVersion ||
		options.RolloutPercentage < MinSessionSemanticRolloutPercentage ||
		options.RolloutPercentage > MaxSessionSemanticRolloutPercentage || options.Now.IsZero() ||
		contractErr != nil || !contract.Equal(options.Contract) || !rolloutBaselineCompatible(source.Config) {
		return nil, ErrInvalidSessionSemanticRollout
	}

	config := source.Clone().Config
	config.RecallBudgets[domainrecommendation.RecallProviderSemanticSession] = 50
	config.ProviderDeadlinesMS[domainrecommendation.RecallProviderSemanticSession] = 250
	config.FeatureWeights[domainrecommendation.FeatureSemanticSimilarity] = 0.25
	config.PreRankPoolLimit = domainrecommendation.MaxPolicyPreRankCandidates
	config.RecallProviderOrder = []string{
		domainrecommendation.RecallProviderFresh,
		domainrecommendation.RecallProviderHot,
		domainrecommendation.RecallProviderContentSimilarity,
		domainrecommendation.RecallProviderFollowedAuthor,
		domainrecommendation.RecallProviderSessionContinuation,
		domainrecommendation.RecallProviderSemanticSession,
	}
	config.RecallProviderReservations = map[string]int{
		domainrecommendation.RecallProviderFresh:               0,
		domainrecommendation.RecallProviderHot:                 0,
		domainrecommendation.RecallProviderContentSimilarity:   0,
		domainrecommendation.RecallProviderFollowedAuthor:      0,
		domainrecommendation.RecallProviderSessionContinuation: 0,
		domainrecommendation.RecallProviderSemanticSession:     10,
	}
	config.RolloutPercentage = options.RolloutPercentage
	config.SamplingRatePPM = domainrecommendation.MaxSamplingRatePPM
	config.SessionSemantic = &domainrecommendation.SessionSemanticPolicyConfiguration{
		BuilderVersion: domainrecommendation.SessionSemanticBuilderV1,
		ContractKey:    options.Contract.Key(), LookbackSeconds: 24 * 60 * 60,
		MaxSeeds:           domainrecommendation.MaxSessionSemanticSeeds,
		MinPositiveSignals: 2, MinConfidence: 0.25,
	}
	policy, err := domainrecommendation.NewPolicy(
		domainrecommendation.RecommendationRequestLogScene,
		options.TargetVersion,
		false,
		config,
		options.Now.UTC(),
	)
	if err != nil {
		return nil, ErrInvalidSessionSemanticRollout
	}
	digest, err := RecommendationPolicyConfigDigest(policy.Config)
	if err != nil {
		return nil, err
	}
	return &SessionSemanticRolloutPlan{
		Profile:       SessionSemanticRolloutProfileV1,
		SourceVersion: source.Version,
		TargetVersion: options.TargetVersion,
		Policy:        policy,
		ConfigDigest:  digest,
		Changes: []string{
			"add semantic_session recall budget=50 deadline_ms=250",
			"add semantic_similarity weight=0.25",
			"enable quota merge pool=500 semantic_reservation=10",
			"bind session-semantic-v1 to active multimodal contract",
			fmt.Sprintf("set rollout_percentage=%d", options.RolloutPercentage),
			"set request-log sampling_ppm=1000000",
		},
		Cohort: BuildSessionSemanticRolloutCohortSummary(options.RolloutPercentage, 10_000),
	}, nil
}

func rolloutBaselineCompatible(config domainrecommendation.PolicyConfiguration) bool {
	if config.SessionSemantic != nil || config.PreRankPoolLimit != 0 ||
		config.RecallProviderOrder != nil || config.RecallProviderReservations != nil {
		return false
	}
	if _, exists := config.RecallBudgets[domainrecommendation.RecallProviderSemanticSession]; exists {
		return false
	}
	if _, exists := config.ProviderDeadlinesMS[domainrecommendation.RecallProviderSemanticSession]; exists {
		return false
	}
	if _, exists := config.FeatureWeights[domainrecommendation.FeatureSemanticSimilarity]; exists {
		return false
	}
	for _, provider := range []string{
		domainrecommendation.RecallProviderFresh,
		domainrecommendation.RecallProviderHot,
		domainrecommendation.RecallProviderContentSimilarity,
		domainrecommendation.RecallProviderFollowedAuthor,
		domainrecommendation.RecallProviderSessionContinuation,
	} {
		if config.RecallBudgets[provider] <= 0 || config.ProviderDeadlinesMS[provider] <= 0 {
			return false
		}
	}
	return true
}

func RecommendationPolicyConfigDigest(config domainrecommendation.PolicyConfiguration) (string, error) {
	normalized, err := domainrecommendation.ValidatePolicyConfiguration(config)
	if err != nil {
		return "", ErrInvalidSessionSemanticRollout
	}
	payload, err := json.Marshal(normalized)
	if err != nil {
		return "", ErrInvalidSessionSemanticRollout
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

func BuildSessionSemanticRolloutCohortSummary(
	rolloutPercentage int,
	samples int,
) SessionSemanticRolloutCohortSummary {
	if rolloutPercentage < 0 || rolloutPercentage > 100 || samples <= 0 || samples > 100_000 {
		return SessionSemanticRolloutCohortSummary{}
	}
	summary := SessionSemanticRolloutCohortSummary{Samples: samples, Buckets: make([]int, 100)}
	for index := 0; index < samples; index++ {
		userID := int64(index%997 + 1)
		requestID := fmt.Sprintf("rollout-probe-%05d", index)
		bucket := domainrecommendation.PolicyCohortPercent(
			userID, domainrecommendation.RecommendationRequestLogScene, requestID,
		)
		summary.Buckets[bucket]++
		if bucket < rolloutPercentage {
			summary.Selected++
		} else {
			summary.Fallback++
		}
	}
	return summary
}
