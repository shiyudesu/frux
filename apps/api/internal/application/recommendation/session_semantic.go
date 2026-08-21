package applicationrecommendation

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"math"
	"sort"
	"time"

	domainembedding "github.com/shiyudesu/frux/internal/domain/embedding"
	domainrecommendation "github.com/shiyudesu/frux/internal/domain/recommendation"
)

var ErrSessionSemanticUnavailable = errors.New("session semantic recommendation unavailable")

const (
	sessionSemanticPositiveSaturation = 4.0
	sessionSemanticNegativeMassCap    = 0.75
	sessionSemanticVectorEpsilon      = 1e-9
)

type SessionSemanticFact struct {
	VideoID       int64
	EncounteredAt time.Time
	Signals       []domainrecommendation.SessionSemanticSignal
}

type SessionSemanticFactSource interface {
	LoadSessionSemanticFacts(
		context.Context,
		int64,
		[]int64,
		time.Time,
		time.Time,
	) ([]SessionSemanticFact, error)
}

type SessionSemanticVectorSource interface {
	LoadSessionSemanticVectors(
		context.Context,
		[]int64,
		domainembedding.MultimodalContractIdentity,
	) (map[int64]*domainembedding.MultimodalVectorFact, error)
}

type SessionSemanticBuildRequest struct {
	UserID   int64
	Context  *domainrecommendation.RecommendationContext
	Policy   *domainrecommendation.SessionSemanticPolicyConfiguration
	Contract domainembedding.MultimodalContractIdentity
	Budget   int
	Now      time.Time
}

type SessionSemanticInterest struct {
	Vector      []float64
	Confidence  float64
	Band        domainrecommendation.SessionSemanticConfidenceBand
	Exclusions  []int64
	OutputLimit int
	Evidence    *domainrecommendation.SessionSemanticEvidence
}

func (i *SessionSemanticInterest) Available() bool {
	return i != nil && len(i.Vector) > 0 && i.Confidence > 0 && i.OutputLimit > 0 &&
		i.Evidence != nil && i.Evidence.Result == domainrecommendation.SessionSemanticResultSuccess
}

func (i *SessionSemanticInterest) Clone() *SessionSemanticInterest {
	if i == nil {
		return nil
	}
	cloned := *i
	cloned.Vector = append([]float64(nil), i.Vector...)
	cloned.Exclusions = append([]int64(nil), i.Exclusions...)
	cloned.Evidence = i.Evidence.Clone()
	return &cloned
}

type SessionSemanticBuilder struct {
	facts   SessionSemanticFactSource
	vectors SessionSemanticVectorSource
}

func NewSessionSemanticBuilder(
	facts SessionSemanticFactSource,
	vectors SessionSemanticVectorSource,
) (*SessionSemanticBuilder, error) {
	if facts == nil || vectors == nil {
		return nil, ErrSessionSemanticUnavailable
	}
	return &SessionSemanticBuilder{facts: facts, vectors: vectors}, nil
}

func (b *SessionSemanticBuilder) Build(
	ctx context.Context,
	request SessionSemanticBuildRequest,
) (*SessionSemanticInterest, error) {
	policy, err := domainrecommendation.ValidateSessionSemanticPolicyConfiguration(request.Policy)
	if b == nil || b.facts == nil || b.vectors == nil || err != nil || request.UserID <= 0 ||
		request.Context == nil || request.Budget <= 0 || request.Budget > domainrecommendation.MaxRecallBudget ||
		request.Now.IsZero() || policy.ContractKey != request.Contract.Key() {
		return nil, ErrSessionSemanticUnavailable
	}
	seedIDs := boundedSessionSemanticSeedIDs(request.Context, policy.MaxSeeds)
	if len(seedIDs) == 0 {
		return sessionSemanticUnavailableInterest(
			policy, request.Contract, domainrecommendation.SessionSemanticResultInsufficientEvidence,
			nil, 0, 0, 0, 0, 0, "",
		)
	}
	exclusions := append([]int64(nil), seedIDs...)
	cutoff := request.Now.UTC().Add(-time.Duration(policy.LookbackSeconds) * time.Second)
	facts, err := b.facts.LoadSessionSemanticFacts(
		ctx, request.UserID, seedIDs, cutoff, request.Now.UTC(),
	)
	if err != nil {
		return nil, err
	}
	canonical := canonicalSessionSemanticSignals(facts, request.Context.CurrentVideoID, seedIDs, cutoff, request.Now.UTC())
	positiveCount, negativeCount := sessionSemanticSignalCounts(canonical)
	inputDigest := sessionSemanticInputDigest(policy, request.Contract, request.Now.UTC(), canonical)
	if positiveCount < policy.MinPositiveSignals {
		return sessionSemanticUnavailableInterest(
			policy, request.Contract, domainrecommendation.SessionSemanticResultInsufficientEvidence,
			exclusions, len(canonicalFacts(canonical)), positiveCount, negativeCount, 0, len(exclusions), inputDigest,
		)
	}
	vectorIDs := directionalSessionSemanticVideoIDs(canonical)
	loaded, err := b.vectors.LoadSessionSemanticVectors(ctx, vectorIDs, request.Contract)
	if err != nil {
		return nil, err
	}
	composed := composeSessionSemanticVector(
		policy, request.Contract, request.Now.UTC(), canonical, loaded,
	)
	if composed.result != domainrecommendation.SessionSemanticResultSuccess {
		return sessionSemanticUnavailableInterest(
			policy, request.Contract, composed.result, exclusions,
			len(canonicalFacts(canonical)), positiveCount, negativeCount,
			composed.compatibleCount, len(exclusions), inputDigest,
		)
	}
	if composed.confidence < policy.MinConfidence {
		return sessionSemanticUnavailableInterest(
			policy, request.Contract, domainrecommendation.SessionSemanticResultLowConfidence,
			exclusions, len(canonicalFacts(canonical)), positiveCount, negativeCount,
			composed.compatibleCount, len(exclusions), inputDigest,
		)
	}
	band := sessionSemanticConfidenceBand(composed.confidence)
	outputLimit := sessionSemanticOutputLimit(request.Budget, composed.confidence, band)
	evidence, err := domainrecommendation.NewSessionSemanticEvidence(domainrecommendation.SessionSemanticEvidence{
		BuilderVersion: policy.BuilderVersion, ContractKey: request.Contract.Key(),
		Result:     domainrecommendation.SessionSemanticResultSuccess,
		Confidence: composed.confidence, ConfidenceBand: band,
		EligibleCount: len(canonicalFacts(canonical)), PositiveCount: positiveCount,
		NegativeCount: negativeCount, CompatibleCount: composed.compatibleCount,
		ExcludedCount: len(exclusions), InputDigest: inputDigest,
	})
	if err != nil {
		return nil, ErrSessionSemanticUnavailable
	}
	return &SessionSemanticInterest{
		Vector: append([]float64(nil), composed.vector...), Confidence: composed.confidence,
		Band: band, Exclusions: exclusions, OutputLimit: outputLimit, Evidence: evidence,
	}, nil
}

type weightedSessionSemanticSignal struct {
	videoID    int64
	kind       domainrecommendation.SessionSemanticSignalKind
	occurredAt time.Time
}

func boundedSessionSemanticSeedIDs(
	recommendationContext *domainrecommendation.RecommendationContext,
	limit int,
) []int64 {
	if recommendationContext == nil || limit <= 0 {
		return nil
	}
	values := make([]int64, 0, min(limit, len(recommendationContext.RecentVideoIDs)+1))
	seen := make(map[int64]struct{}, cap(values))
	appendID := func(videoID int64) {
		if videoID <= 0 || len(values) >= limit {
			return
		}
		if _, exists := seen[videoID]; exists {
			return
		}
		seen[videoID] = struct{}{}
		values = append(values, videoID)
	}
	appendID(recommendationContext.CurrentVideoID)
	for _, videoID := range recommendationContext.RecentVideoIDs {
		appendID(videoID)
	}
	return values
}

func canonicalSessionSemanticSignals(
	facts []SessionSemanticFact,
	currentVideoID int64,
	seedIDs []int64,
	cutoff time.Time,
	now time.Time,
) []weightedSessionSemanticSignal {
	seedSet := make(map[int64]struct{}, len(seedIDs))
	for _, videoID := range seedIDs {
		seedSet[videoID] = struct{}{}
	}
	type factState struct {
		encounteredAt time.Time
		signals       map[domainrecommendation.SessionSemanticSignalKind]time.Time
	}
	states := make(map[int64]*factState, len(seedIDs))
	for _, fact := range facts {
		if _, selected := seedSet[fact.VideoID]; !selected || fact.EncounteredAt.Before(cutoff) || fact.EncounteredAt.After(now) {
			continue
		}
		state := states[fact.VideoID]
		if state == nil {
			state = &factState{signals: map[domainrecommendation.SessionSemanticSignalKind]time.Time{}}
			states[fact.VideoID] = state
		}
		if fact.EncounteredAt.After(state.encounteredAt) {
			state.encounteredAt = fact.EncounteredAt.UTC()
		}
		for _, signal := range fact.Signals {
			if signal.VideoID != fact.VideoID || !signal.Valid() || signal.OccurredAt.Before(cutoff) || signal.OccurredAt.After(now) {
				continue
			}
			if previous := state.signals[signal.Kind]; signal.OccurredAt.After(previous) {
				state.signals[signal.Kind] = signal.OccurredAt.UTC()
			}
		}
	}
	values := make([]weightedSessionSemanticSignal, 0, len(states)*4)
	for videoID, state := range states {
		if state == nil || state.encounteredAt.IsZero() {
			continue
		}
		if occurredAt, negative := state.signals[domainrecommendation.SessionSemanticSignalNotInterested]; negative {
			values = append(values, weightedSessionSemanticSignal{
				videoID: videoID, kind: domainrecommendation.SessionSemanticSignalNotInterested, occurredAt: occurredAt,
			})
			if alreadySeenAt, exists := state.signals[domainrecommendation.SessionSemanticSignalAlreadySeen]; exists {
				values = append(values, weightedSessionSemanticSignal{
					videoID: videoID, kind: domainrecommendation.SessionSemanticSignalAlreadySeen, occurredAt: alreadySeenAt,
				})
			}
			continue
		}
		if videoID == currentVideoID {
			values = append(values, weightedSessionSemanticSignal{
				videoID: videoID, kind: domainrecommendation.SessionSemanticSignalCurrent, occurredAt: state.encounteredAt,
			})
		}
		playbackKind := domainrecommendation.SessionSemanticSignalKind("")
		if _, exists := state.signals[domainrecommendation.SessionSemanticSignalComplete]; exists {
			playbackKind = domainrecommendation.SessionSemanticSignalComplete
		} else if _, exists := state.signals[domainrecommendation.SessionSemanticSignalSustained]; exists {
			playbackKind = domainrecommendation.SessionSemanticSignalSustained
		} else if _, exists := state.signals[domainrecommendation.SessionSemanticSignalEarlySkip]; exists {
			playbackKind = domainrecommendation.SessionSemanticSignalEarlySkip
		}
		if playbackKind != "" {
			values = append(values, weightedSessionSemanticSignal{
				videoID: videoID, kind: playbackKind, occurredAt: state.signals[playbackKind],
			})
		}
		for _, kind := range []domainrecommendation.SessionSemanticSignalKind{
			domainrecommendation.SessionSemanticSignalLike,
			domainrecommendation.SessionSemanticSignalFavorite,
			domainrecommendation.SessionSemanticSignalAlreadySeen,
		} {
			if occurredAt, exists := state.signals[kind]; exists {
				values = append(values, weightedSessionSemanticSignal{videoID: videoID, kind: kind, occurredAt: occurredAt})
			}
		}
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].videoID != values[j].videoID {
			return values[i].videoID < values[j].videoID
		}
		return values[i].kind < values[j].kind
	})
	return values
}

func sessionSemanticSignalCounts(values []weightedSessionSemanticSignal) (int, int) {
	positive, negative := 0, 0
	for _, value := range values {
		weight := sessionSemanticBaseWeight(value.kind)
		if weight > 0 {
			positive++
		} else if weight < 0 {
			negative++
		}
	}
	return positive, negative
}

func canonicalFacts(values []weightedSessionSemanticSignal) map[int64]struct{} {
	facts := make(map[int64]struct{}, len(values))
	for _, value := range values {
		facts[value.videoID] = struct{}{}
	}
	return facts
}

func directionalSessionSemanticVideoIDs(values []weightedSessionSemanticSignal) []int64 {
	seen := map[int64]struct{}{}
	ids := make([]int64, 0, len(values))
	for _, value := range values {
		if sessionSemanticBaseWeight(value.kind) == 0 {
			continue
		}
		if _, exists := seen[value.videoID]; exists {
			continue
		}
		seen[value.videoID] = struct{}{}
		ids = append(ids, value.videoID)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

type sessionSemanticComposition struct {
	vector          []float64
	confidence      float64
	compatibleCount int
	result          domainrecommendation.SessionSemanticResult
}

func composeSessionSemanticVector(
	policy *domainrecommendation.SessionSemanticPolicyConfiguration,
	contract domainembedding.MultimodalContractIdentity,
	now time.Time,
	signals []weightedSessionSemanticSignal,
	vectors map[int64]*domainembedding.MultimodalVectorFact,
) sessionSemanticComposition {
	var positive, negative []float64
	positiveMass, negativeMass := 0.0, 0.0
	eligibleMass, compatibleMass := 0.0, 0.0
	positiveBaseMass, positiveFreshMass := 0.0, 0.0
	compatibleVideos := map[int64]struct{}{}
	halfLife := time.Duration(policy.LookbackSeconds) * time.Second / 2
	for _, signal := range signals {
		baseWeight := sessionSemanticBaseWeight(signal.kind)
		if baseWeight == 0 {
			continue
		}
		decay := sessionSemanticDecay(now.Sub(signal.occurredAt), halfLife)
		weight := baseWeight * decay
		eligibleMass += math.Abs(weight)
		fact := vectors[signal.videoID]
		if fact == nil || !fact.Identity.Contract.Equal(contract) {
			continue
		}
		vector, err := domainembedding.ValidateMultimodalQueryVector(contract, fact.Values)
		if err != nil {
			continue
		}
		compatibleVideos[signal.videoID] = struct{}{}
		compatibleMass += math.Abs(weight)
		if baseWeight > 0 {
			if positive == nil {
				positive = make([]float64, contract.Dimension)
			}
			for index := range vector {
				positive[index] += vector[index] * weight
			}
			positiveMass += weight
			positiveBaseMass += baseWeight
			positiveFreshMass += baseWeight * decay
		} else {
			if negative == nil {
				negative = make([]float64, contract.Dimension)
			}
			absolute := math.Abs(weight)
			for index := range vector {
				negative[index] += vector[index] * absolute
			}
			negativeMass += absolute
		}
	}
	if len(compatibleVideos) == 0 || positiveMass <= 0 || len(positive) == 0 {
		return sessionSemanticComposition{
			compatibleCount: len(compatibleVideos), result: domainrecommendation.SessionSemanticResultNoCompatibleVectors,
		}
	}
	positiveNorm := vectorNorm(positive)
	if positiveNorm <= sessionSemanticVectorEpsilon {
		return sessionSemanticComposition{
			compatibleCount: len(compatibleVideos), result: domainrecommendation.SessionSemanticResultInvalidVector,
		}
	}
	combined := append([]float64(nil), positive...)
	if negativeMass > 0 && len(negative) == len(combined) {
		negativeScale := math.Min(1, positiveMass*sessionSemanticNegativeMassCap/negativeMass)
		for index := range combined {
			combined[index] -= negative[index] * negativeScale
		}
	}
	combinedNorm := vectorNorm(combined)
	if combinedNorm <= sessionSemanticVectorEpsilon || math.IsNaN(combinedNorm) || math.IsInf(combinedNorm, 0) {
		return sessionSemanticComposition{
			compatibleCount: len(compatibleVideos), result: domainrecommendation.SessionSemanticResultInvalidVector,
		}
	}
	for index := range combined {
		combined[index] /= combinedNorm
	}
	if _, err := domainembedding.ValidateMultimodalQueryVector(contract, combined); err != nil {
		return sessionSemanticComposition{
			compatibleCount: len(compatibleVideos), result: domainrecommendation.SessionSemanticResultInvalidVector,
		}
	}
	coverage := boundedUnit(compatibleMass / math.Max(eligibleMass, sessionSemanticVectorEpsilon))
	strength := boundedUnit(positiveMass / sessionSemanticPositiveSaturation)
	coherence := boundedUnit(positiveNorm / math.Max(positiveMass, sessionSemanticVectorEpsilon))
	freshness := boundedUnit(positiveFreshMass / math.Max(positiveBaseMass, sessionSemanticVectorEpsilon))
	confidence := boundedUnit(coverage * strength * coherence * freshness)
	return sessionSemanticComposition{
		vector: combined, confidence: confidence, compatibleCount: len(compatibleVideos),
		result: domainrecommendation.SessionSemanticResultSuccess,
	}
}

func sessionSemanticBaseWeight(kind domainrecommendation.SessionSemanticSignalKind) float64 {
	switch kind {
	case domainrecommendation.SessionSemanticSignalCurrent:
		return 1
	case domainrecommendation.SessionSemanticSignalComplete:
		return 2
	case domainrecommendation.SessionSemanticSignalSustained:
		return 1.25
	case domainrecommendation.SessionSemanticSignalLike:
		return 1.5
	case domainrecommendation.SessionSemanticSignalFavorite:
		return 2
	case domainrecommendation.SessionSemanticSignalEarlySkip:
		return -1
	case domainrecommendation.SessionSemanticSignalNotInterested:
		return -2
	default:
		return 0
	}
}

func sessionSemanticDecay(age, halfLife time.Duration) float64 {
	if age <= 0 {
		return 1
	}
	if halfLife <= 0 {
		return 0
	}
	return math.Pow(0.5, float64(age)/float64(halfLife))
}

func sessionSemanticConfidenceBand(confidence float64) domainrecommendation.SessionSemanticConfidenceBand {
	switch {
	case confidence <= 0:
		return domainrecommendation.SessionSemanticConfidenceNone
	case confidence < 0.5:
		return domainrecommendation.SessionSemanticConfidenceLow
	case confidence < 0.8:
		return domainrecommendation.SessionSemanticConfidenceMedium
	default:
		return domainrecommendation.SessionSemanticConfidenceHigh
	}
}

func sessionSemanticOutputLimit(
	budget int,
	confidence float64,
	band domainrecommendation.SessionSemanticConfidenceBand,
) int {
	scale := 0.0
	switch band {
	case domainrecommendation.SessionSemanticConfidenceLow:
		scale = 0.25
	case domainrecommendation.SessionSemanticConfidenceMedium:
		scale = 0.6
	case domainrecommendation.SessionSemanticConfidenceHigh:
		scale = 1
	}
	scale = math.Min(scale, confidence+0.25)
	limit := int(math.Floor(float64(budget) * scale))
	if limit < 1 && budget > 0 && confidence > 0 {
		return 1
	}
	if limit > budget {
		return budget
	}
	return limit
}

func sessionSemanticUnavailableInterest(
	policy *domainrecommendation.SessionSemanticPolicyConfiguration,
	contract domainembedding.MultimodalContractIdentity,
	result domainrecommendation.SessionSemanticResult,
	exclusions []int64,
	eligibleCount int,
	positiveCount int,
	negativeCount int,
	compatibleCount int,
	excludedCount int,
	inputDigest string,
) (*SessionSemanticInterest, error) {
	evidence, err := domainrecommendation.NewSessionSemanticEvidence(domainrecommendation.SessionSemanticEvidence{
		BuilderVersion: policy.BuilderVersion, ContractKey: contract.Key(), Result: result,
		ConfidenceBand: domainrecommendation.SessionSemanticConfidenceNone,
		EligibleCount:  eligibleCount, PositiveCount: positiveCount, NegativeCount: negativeCount,
		CompatibleCount: compatibleCount, ExcludedCount: excludedCount, InputDigest: inputDigest,
	})
	if err != nil {
		return nil, ErrSessionSemanticUnavailable
	}
	return &SessionSemanticInterest{
		Band:       domainrecommendation.SessionSemanticConfidenceNone,
		Exclusions: append([]int64(nil), exclusions...), Evidence: evidence,
	}, nil
}

func sessionSemanticInputDigest(
	policy *domainrecommendation.SessionSemanticPolicyConfiguration,
	contract domainembedding.MultimodalContractIdentity,
	now time.Time,
	values []weightedSessionSemanticSignal,
) string {
	hasher := sha256.New()
	_, _ = hasher.Write([]byte(policy.BuilderVersion))
	_, _ = hasher.Write([]byte(contract.Key()))
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], uint64(now.UnixNano()))
	_, _ = hasher.Write(encoded[:])
	for _, value := range values {
		binary.BigEndian.PutUint64(encoded[:], uint64(value.videoID))
		_, _ = hasher.Write(encoded[:])
		_, _ = hasher.Write([]byte(value.kind))
		binary.BigEndian.PutUint64(encoded[:], uint64(value.occurredAt.UnixNano()))
		_, _ = hasher.Write(encoded[:])
	}
	return hex.EncodeToString(hasher.Sum(nil))
}

func vectorNorm(values []float64) float64 {
	var squared float64
	for _, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return math.NaN()
		}
		squared += value * value
	}
	return math.Sqrt(squared)
}

func boundedUnit(value float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) || value <= 0 {
		return 0
	}
	if value >= 1 {
		return 1
	}
	return value
}
