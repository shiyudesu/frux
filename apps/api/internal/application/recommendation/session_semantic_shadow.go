package applicationrecommendation

import (
	"context"
	"errors"
	"math"
	"sort"
	"strings"
	"time"

	domainembedding "github.com/shiyudesu/frux/internal/domain/embedding"
	domainrecommendation "github.com/shiyudesu/frux/internal/domain/recommendation"
)

const SessionSemanticShadowMixV1 = "shadow-mix-v1"

var ErrSessionSemanticShadowConfiguration = errors.New("invalid session semantic shadow configuration")
var ErrSessionSemanticShadowSimulation = errors.New("session semantic shadow simulation failed")

type SessionSemanticShadowTerminalResult string

const (
	SessionSemanticShadowResultSuccess  SessionSemanticShadowTerminalResult = "success"
	SessionSemanticShadowResultEmpty    SessionSemanticShadowTerminalResult = "empty"
	SessionSemanticShadowResultTimeout  SessionSemanticShadowTerminalResult = "timeout"
	SessionSemanticShadowResultError    SessionSemanticShadowTerminalResult = "error"
	SessionSemanticShadowResultCapacity SessionSemanticShadowTerminalResult = "capacity"
	SessionSemanticShadowResultPanic    SessionSemanticShadowTerminalResult = "panic"
	SessionSemanticShadowResultClosed   SessionSemanticShadowTerminalResult = "closed"
)

func validSessionSemanticShadowTerminalResult(value SessionSemanticShadowTerminalResult) bool {
	switch value {
	case SessionSemanticShadowResultSuccess, SessionSemanticShadowResultEmpty,
		SessionSemanticShadowResultTimeout, SessionSemanticShadowResultError,
		SessionSemanticShadowResultCapacity, SessionSemanticShadowResultPanic,
		SessionSemanticShadowResultClosed:
		return true
	default:
		return false
	}
}

type SessionSemanticShadowRuntimeConfig struct {
	Enabled             bool
	SamplePPM           int
	Budget              int
	Deadline            time.Duration
	MaxInFlight         int
	ComparisonLimit     int
	SimulatedPoolLimit  int
	SimulatedTopK       int
	SemanticReservation int
	SemanticWeight      float64
	ShutdownTimeout     time.Duration
	Contract            domainembedding.MultimodalContractIdentity
	SessionPolicy       *domainrecommendation.SessionSemanticPolicyConfiguration
}

func (c SessionSemanticShadowRuntimeConfig) Validate() error {
	contract, contractErr := domainembedding.NewMultimodalContractIdentity(
		c.Contract.ProviderAlias, c.Contract.ModelAlias, c.Contract.RevisionAlias, c.Contract.Dimension,
		c.Contract.TextCanonicalizer, c.Contract.FrameSamplingPolicy,
		c.Contract.ImagePreprocessingPolicy, c.Contract.FusionPolicy,
	)
	policy, policyErr := domainrecommendation.ValidateSessionSemanticPolicyConfiguration(c.SessionPolicy)
	if c.SamplePPM < 0 || c.SamplePPM > domainrecommendation.MaxSamplingRatePPM ||
		c.Budget < 1 || c.Budget > 100 || c.Deadline < 25*time.Millisecond || c.Deadline > 500*time.Millisecond ||
		c.MaxInFlight < 1 || c.MaxInFlight > 16 ||
		c.ComparisonLimit < 1 || c.ComparisonLimit > domainrecommendation.MaxPolicyPreRankCandidates ||
		c.SimulatedPoolLimit < domainrecommendation.MinPolicyPreRankCandidates ||
		c.SimulatedPoolLimit > domainrecommendation.MaxPolicyPreRankCandidates ||
		c.SimulatedTopK < 1 || c.SimulatedTopK > c.SimulatedPoolLimit ||
		c.SemanticReservation < 0 || c.SemanticReservation > c.Budget ||
		c.SemanticReservation > c.SimulatedPoolLimit ||
		math.IsNaN(c.SemanticWeight) || math.IsInf(c.SemanticWeight, 0) ||
		c.SemanticWeight <= 0 || c.SemanticWeight > domainrecommendation.MaxFeatureWeight ||
		c.ShutdownTimeout < 100*time.Millisecond || c.ShutdownTimeout > 10*time.Second ||
		contractErr != nil || !contract.Equal(c.Contract) || policyErr != nil ||
		policy == nil || policy.ContractKey != c.Contract.Key() {
		return ErrSessionSemanticShadowConfiguration
	}
	return nil
}

type SessionSemanticShadowRequest struct {
	UserID           int64
	Scene            string
	RequestID        string
	Context          *domainrecommendation.RecommendationContext
	ActivePolicy     *domainrecommendation.Policy
	ActiveCandidates []*domainrecommendation.Candidate
	BaselineState    string
	Now              time.Time
}

func (r SessionSemanticShadowRequest) clone(limit int) SessionSemanticShadowRequest {
	cloned := r
	cloned.Scene = strings.ToLower(strings.TrimSpace(r.Scene))
	cloned.RequestID = strings.TrimSpace(r.RequestID)
	cloned.Context = r.Context.Clone()
	cloned.ActivePolicy = r.ActivePolicy.Clone()
	cloned.BaselineState = normalizeSessionSemanticShadowBaseline(r.BaselineState)
	if limit <= 0 || limit > len(r.ActiveCandidates) {
		limit = len(r.ActiveCandidates)
	}
	cloned.ActiveCandidates = cloneCandidates(r.ActiveCandidates[:limit])
	cloned.Now = r.Now.UTC()
	return cloned
}

func (r SessionSemanticShadowRequest) valid() bool {
	return r.UserID > 0 && strings.ToLower(strings.TrimSpace(r.Scene)) == domainrecommendation.RecommendationRequestLogScene &&
		strings.TrimSpace(r.RequestID) != "" && len(strings.TrimSpace(r.RequestID)) <= domainrecommendation.MaxRequestIDLength &&
		r.ActivePolicy != nil && !r.Now.IsZero()
}

func normalizeSessionSemanticShadowBaseline(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "providers":
		return "providers"
	case "repository_fallback":
		return "repository_fallback"
	default:
		return "unknown"
	}
}

type SessionSemanticShadowSimulationRequest struct {
	Request            SessionSemanticShadowRequest
	ShadowPolicy       *domainrecommendation.Policy
	SemanticCandidates []*domainrecommendation.Candidate
	ComparisonLimit    int
	TopK               int
}

type SessionSemanticShadowComparison struct {
	ActiveCount              int
	SemanticCount            int
	IntersectionCount        int
	UniqueSemanticCount      int
	MixedCount               int
	SemanticPoolSurvival     int
	SimulatedTopKCount       int
	SemanticRankSurvival     int
	UniqueRankSurvival       int
	ActiveTopKDisplaced      int
	SemanticAuthorCount      int
	SimulatedAuthorCount     int
	OverlapRatio             float64
	UniqueContributionRatio  float64
	PoolSurvivalRatio        float64
	RankSurvivalRatio        float64
	UniqueRankSurvivalRatio  float64
	ActiveTopKDisplacedRatio float64
}

func (c SessionSemanticShadowComparison) valid() bool {
	counts := []int{
		c.ActiveCount, c.SemanticCount, c.IntersectionCount, c.UniqueSemanticCount,
		c.MixedCount, c.SemanticPoolSurvival, c.SimulatedTopKCount,
		c.SemanticRankSurvival, c.UniqueRankSurvival, c.ActiveTopKDisplaced,
		c.SemanticAuthorCount, c.SimulatedAuthorCount,
	}
	for _, count := range counts {
		if count < 0 || count > domainrecommendation.MaxPolicyPreRankCandidates {
			return false
		}
	}
	for _, ratio := range []float64{
		c.OverlapRatio, c.UniqueContributionRatio, c.PoolSurvivalRatio,
		c.RankSurvivalRatio, c.UniqueRankSurvivalRatio, c.ActiveTopKDisplacedRatio,
	} {
		if math.IsNaN(ratio) || math.IsInf(ratio, 0) || ratio < 0 || ratio > 1 {
			return false
		}
	}
	return c.IntersectionCount+c.UniqueSemanticCount <= c.SemanticCount &&
		c.SemanticPoolSurvival <= c.SemanticCount &&
		c.SemanticRankSurvival <= c.SemanticCount &&
		c.UniqueRankSurvival <= c.UniqueSemanticCount &&
		c.ActiveTopKDisplaced <= c.SimulatedTopKCount
}

type SessionSemanticShadowSimulationResult struct {
	Mixed      []*domainrecommendation.Candidate
	Ranked     []*domainrecommendation.Candidate
	Comparison SessionSemanticShadowComparison
}

func (r *SessionSemanticShadowSimulationResult) clone() *SessionSemanticShadowSimulationResult {
	if r == nil {
		return nil
	}
	return &SessionSemanticShadowSimulationResult{
		Mixed: cloneCandidates(r.Mixed), Ranked: cloneCandidates(r.Ranked), Comparison: r.Comparison,
	}
}

type SessionSemanticShadowObservation struct {
	Result         SessionSemanticShadowTerminalResult
	BaselineState  string
	ConfidenceBand domainrecommendation.SessionSemanticConfidenceBand
	Comparison     SessionSemanticShadowComparison
	Duration       time.Duration
}

func (o SessionSemanticShadowObservation) valid() bool {
	return validSessionSemanticShadowTerminalResult(o.Result) &&
		normalizeSessionSemanticShadowBaseline(o.BaselineState) == o.BaselineState &&
		domainrecommendation.ValidSessionSemanticConfidenceBand(o.ConfidenceBand) &&
		o.Duration >= 0 && o.Comparison.valid()
}

type SessionSemanticShadowObserver interface {
	ObserveSelection(result string)
	ObserveAdmission(result string)
	ObserveInFlight(delta int)
	ObserveTerminal(SessionSemanticShadowObservation)
}

type noopSessionSemanticShadowObserver struct{}

func (noopSessionSemanticShadowObserver) ObserveSelection(string)                          {}
func (noopSessionSemanticShadowObserver) ObserveAdmission(string)                          {}
func (noopSessionSemanticShadowObserver) ObserveInFlight(int)                              {}
func (noopSessionSemanticShadowObserver) ObserveTerminal(SessionSemanticShadowObservation) {}

type SessionSemanticShadowSimulator interface {
	SimulateSessionSemanticShadow(context.Context, SessionSemanticShadowSimulationRequest) (*SessionSemanticShadowSimulationResult, error)
}

func BuildSessionSemanticShadowPolicy(
	active *domainrecommendation.Policy,
	config SessionSemanticShadowRuntimeConfig,
	now time.Time,
) (*domainrecommendation.Policy, error) {
	if active == nil || now.IsZero() || config.Validate() != nil {
		return nil, ErrSessionSemanticShadowConfiguration
	}
	cloned := active.Clone()
	policyConfig := cloned.Config
	policyConfig.RecallBudgets[domainrecommendation.RecallProviderSemanticSession] = config.Budget
	policyConfig.ProviderDeadlinesMS[domainrecommendation.RecallProviderSemanticSession] = int(config.Deadline / time.Millisecond)
	policyConfig.FeatureWeights[domainrecommendation.FeatureSemanticSimilarity] = config.SemanticWeight
	policyConfig.SessionSemantic = config.SessionPolicy.Clone()
	policyConfig.PreRankPoolLimit = config.SimulatedPoolLimit

	order := shadowProviderOrder(cloned.Config)
	policyConfig.RecallProviderOrder = append(order, domainrecommendation.RecallProviderSemanticSession)
	reservations := make(map[string]int, len(policyConfig.RecallProviderOrder))
	if len(cloned.Config.RecallProviderReservations) > 0 {
		for provider, reservation := range cloned.Config.RecallProviderReservations {
			reservations[provider] = reservation
		}
	}
	for _, provider := range order {
		if _, exists := reservations[provider]; !exists {
			reservations[provider] = 0
		}
	}
	reservations[domainrecommendation.RecallProviderSemanticSession] = config.SemanticReservation
	policyConfig.RecallProviderReservations = reservations

	policy, err := domainrecommendation.NewPolicy(active.Scene, active.Version, false, policyConfig, now.UTC())
	if err != nil {
		return nil, ErrSessionSemanticShadowConfiguration
	}
	return policy, nil
}

func shadowProviderOrder(config domainrecommendation.PolicyConfiguration) []string {
	seen := map[string]struct{}{domainrecommendation.RecallProviderSemanticSession: {}}
	order := make([]string, 0, len(config.RecallBudgets))
	appendProvider := func(provider string) {
		provider = strings.ToLower(strings.TrimSpace(provider))
		if provider == "" {
			return
		}
		if _, selected := config.RecallBudgets[provider]; !selected {
			return
		}
		if _, duplicate := seen[provider]; duplicate {
			return
		}
		seen[provider] = struct{}{}
		order = append(order, provider)
	}
	for _, provider := range config.RecallProviderOrder {
		appendProvider(provider)
	}
	for _, provider := range []string{
		domainrecommendation.RecallProviderFresh,
		domainrecommendation.RecallProviderHot,
		domainrecommendation.RecallProviderContentSimilarity,
		domainrecommendation.RecallProviderFollowedAuthor,
		domainrecommendation.RecallProviderSessionContinuation,
	} {
		appendProvider(provider)
	}
	remaining := make([]string, 0)
	for provider := range config.RecallBudgets {
		if _, exists := seen[provider]; !exists {
			remaining = append(remaining, provider)
		}
	}
	sort.Strings(remaining)
	for _, provider := range remaining {
		appendProvider(provider)
	}
	return order
}

func CompareSessionSemanticShadow(
	active []*domainrecommendation.Candidate,
	semantic []*domainrecommendation.Candidate,
	mixed []*domainrecommendation.Candidate,
	ranked []*domainrecommendation.Candidate,
	topK int,
) SessionSemanticShadowComparison {
	active = uniqueShadowCandidates(active, domainrecommendation.MaxPolicyPreRankCandidates)
	semantic = uniqueShadowCandidates(semantic, domainrecommendation.MaxPolicyPreRankCandidates)
	mixed = uniqueShadowCandidates(mixed, domainrecommendation.MaxPolicyPreRankCandidates)
	ranked = uniqueShadowCandidates(ranked, domainrecommendation.MaxPolicyPreRankCandidates)
	if topK < 0 {
		topK = 0
	}
	if topK > len(ranked) {
		topK = len(ranked)
	}
	activeTopK := topK
	if activeTopK > len(active) {
		activeTopK = len(active)
	}

	activeIDs := shadowCandidateSet(active)
	semanticIDs := shadowCandidateSet(semantic)
	uniqueSemanticIDs := make(map[int64]struct{}, len(semanticIDs))
	intersection := 0
	for videoID := range semanticIDs {
		if _, exists := activeIDs[videoID]; exists {
			intersection++
		} else {
			uniqueSemanticIDs[videoID] = struct{}{}
		}
	}
	mixedIDs := shadowCandidateSet(mixed)
	poolSurvival := countShadowSetIntersection(semanticIDs, mixedIDs)
	rankedTopIDs := shadowCandidateSet(ranked[:topK])
	rankSurvival := countShadowSetIntersection(semanticIDs, rankedTopIDs)
	uniqueRankSurvival := countShadowSetIntersection(uniqueSemanticIDs, rankedTopIDs)
	displaced := 0
	for _, candidate := range active[:activeTopK] {
		if candidate != nil {
			if _, exists := rankedTopIDs[candidate.VideoID]; !exists {
				displaced++
			}
		}
	}

	return SessionSemanticShadowComparison{
		ActiveCount: len(active), SemanticCount: len(semantic), IntersectionCount: intersection,
		UniqueSemanticCount: len(uniqueSemanticIDs), MixedCount: len(mixed),
		SemanticPoolSurvival: poolSurvival, SimulatedTopKCount: topK,
		SemanticRankSurvival: rankSurvival, UniqueRankSurvival: uniqueRankSurvival,
		ActiveTopKDisplaced: displaced, SemanticAuthorCount: shadowAuthorCount(semantic),
		SimulatedAuthorCount:     shadowAuthorCount(ranked[:topK]),
		OverlapRatio:             safeShadowRatio(intersection, len(semantic)),
		UniqueContributionRatio:  safeShadowRatio(len(uniqueSemanticIDs), len(semantic)),
		PoolSurvivalRatio:        safeShadowRatio(poolSurvival, len(semantic)),
		RankSurvivalRatio:        safeShadowRatio(rankSurvival, len(semantic)),
		UniqueRankSurvivalRatio:  safeShadowRatio(uniqueRankSurvival, len(uniqueSemanticIDs)),
		ActiveTopKDisplacedRatio: safeShadowRatio(displaced, activeTopK),
	}
}

func uniqueShadowCandidates(candidates []*domainrecommendation.Candidate, limit int) []*domainrecommendation.Candidate {
	if limit <= 0 {
		return []*domainrecommendation.Candidate{}
	}
	output := make([]*domainrecommendation.Candidate, 0, min(limit, len(candidates)))
	seen := make(map[int64]struct{}, min(limit, len(candidates)))
	for _, candidate := range candidates {
		if candidate == nil || candidate.VideoID <= 0 {
			continue
		}
		if _, duplicate := seen[candidate.VideoID]; duplicate {
			continue
		}
		seen[candidate.VideoID] = struct{}{}
		output = append(output, candidate.Clone())
		if len(output) == limit {
			break
		}
	}
	return output
}

func shadowCandidateSet(candidates []*domainrecommendation.Candidate) map[int64]struct{} {
	set := make(map[int64]struct{}, len(candidates))
	for _, candidate := range candidates {
		if candidate != nil && candidate.VideoID > 0 {
			set[candidate.VideoID] = struct{}{}
		}
	}
	return set
}

func countShadowSetIntersection(left, right map[int64]struct{}) int {
	count := 0
	for value := range left {
		if _, exists := right[value]; exists {
			count++
		}
	}
	return count
}

func shadowAuthorCount(candidates []*domainrecommendation.Candidate) int {
	authors := make(map[int64]struct{})
	for _, candidate := range candidates {
		if candidate != nil && candidate.AuthorID > 0 {
			authors[candidate.AuthorID] = struct{}{}
		}
	}
	return len(authors)
}

func safeShadowRatio(numerator, denominator int) float64 {
	if numerator <= 0 || denominator <= 0 {
		return 0
	}
	value := float64(numerator) / float64(denominator)
	if value > 1 {
		return 1
	}
	return value
}
