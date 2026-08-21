package applicationofflineevaluation

import (
	"errors"
	"math"
	"reflect"
	"sort"
	"time"

	domainrecommendation "github.com/shiyudesu/frux/internal/domain/recommendation"
)

const (
	ReplayVersion     = "linear-replay-v1"
	ReplayScopeFull   = "full_pool_fixture"
	ReplayScopeSubset = "served_subset"
)

var ErrInvalidReplay = errors.New("invalid production replay")

type NamedPolicy struct {
	Name             string
	InputSHA256      string
	NormalizedSHA256 string
	Config           domainrecommendation.PolicyConfiguration
}

type PolicyDifference struct {
	Path       string `json:"path"`
	Replayable bool   `json:"replayable"`
}

type ReplayCandidate struct {
	VideoID         int64
	AuthorKey       string
	PublishedAt     time.Time
	RecallProviders []string
	ScoreComponents map[string]float64
}

type replayScoredCandidate struct {
	ReplayCandidate
	score float64
}

type ReplayCase struct {
	Name          string
	Candidates    []ReplayCandidate
	ExpectedOrder []int64
}

type ReplayBundle struct {
	Version string
	Scope   string
	Cases   []ReplayCase
}

type ReplayTopK struct {
	K       int     `json:"k"`
	Overlap float64 `json:"overlap"`
}

type ReplayPolicyMetrics struct {
	Name                  string       `json:"name"`
	NormalizedSHA256      string       `json:"normalized_sha256"`
	Cases                 int          `json:"cases"`
	MeanAbsoluteRankShift float64      `json:"mean_absolute_rank_shift"`
	TopK                  []ReplayTopK `json:"top_k"`
}

type ReplayEvaluation struct {
	Version              string                        `json:"version"`
	Scope                string                        `json:"scope"`
	Cases                int                           `json:"cases"`
	BaselineName         string                        `json:"baseline_name"`
	BaselineSHA256       string                        `json:"baseline_sha256"`
	BaselineParity       bool                          `json:"baseline_parity"`
	ComparativeAvailable bool                          `json:"comparative_available"`
	DiagnosticOnly       bool                          `json:"diagnostic_only"`
	Differences          map[string][]PolicyDifference `json:"differences,omitempty"`
	Candidates           []ReplayPolicyMetrics         `json:"candidates,omitempty"`
}

func EvaluateReplay(
	bundle ReplayBundle,
	baseline NamedPolicy,
	candidates []NamedPolicy,
	kValues []int,
	diagnosticOnly bool,
) (*ReplayEvaluation, error) {
	if bundle.Version != ReplayVersion || (bundle.Scope != ReplayScopeFull && bundle.Scope != ReplayScopeSubset) ||
		len(bundle.Cases) == 0 || len(bundle.Cases) > 1000 || baseline.Name == "" || baseline.NormalizedSHA256 == "" ||
		len(candidates) == 0 || len(candidates) > 20 || !validReplayK(kValues) {
		return nil, ErrInvalidReplay
	}
	result := &ReplayEvaluation{
		Version: ReplayVersion, Scope: bundle.Scope, Cases: len(bundle.Cases),
		BaselineName: baseline.Name, BaselineSHA256: baseline.NormalizedSHA256,
		BaselineParity: true, ComparativeAvailable: true, DiagnosticOnly: diagnosticOnly,
		Differences: make(map[string][]PolicyDifference),
	}
	seenHashes := map[string]struct{}{baseline.NormalizedSHA256: {}}
	for _, candidate := range candidates {
		if candidate.Name == "" || candidate.NormalizedSHA256 == "" {
			return nil, ErrInvalidReplay
		}
		if _, duplicate := seenHashes[candidate.NormalizedSHA256]; duplicate {
			return nil, ErrInvalidReplay
		}
		seenHashes[candidate.NormalizedSHA256] = struct{}{}
		differences := ComparePolicyConfigurations(baseline.Config, candidate.Config)
		result.Differences[candidate.Name] = differences
		for _, difference := range differences {
			if !difference.Replayable {
				result.ComparativeAvailable = false
			}
		}
	}
	if !result.ComparativeAvailable {
		if diagnosticOnly {
			return result, nil
		}
		return nil, ErrInvalidReplay
	}
	baselineOrders := make([][]int64, len(bundle.Cases))
	for index, replayCase := range bundle.Cases {
		order, err := replayOrder(replayCase, baseline.Config)
		if err != nil {
			return nil, err
		}
		baselineOrders[index] = order
		if !slicesEqualInt64(order, replayCase.ExpectedOrder) {
			result.BaselineParity = false
		}
	}
	if !result.BaselineParity {
		return nil, ErrInvalidReplay
	}
	for _, candidate := range candidates {
		metrics := ReplayPolicyMetrics{Name: candidate.Name, NormalizedSHA256: candidate.NormalizedSHA256, Cases: len(bundle.Cases)}
		shiftSum := 0.0
		shiftCount := 0
		overlaps := make([]float64, len(kValues))
		for caseIndex, replayCase := range bundle.Cases {
			order, err := replayOrder(replayCase, candidate.Config)
			if err != nil {
				return nil, err
			}
			baselineRanks := rankPositions(baselineOrders[caseIndex])
			for rank, videoID := range order {
				shiftSum += math.Abs(float64(rank + 1 - baselineRanks[videoID]))
				shiftCount++
			}
			for index, k := range kValues {
				overlaps[index] += topKOverlap(baselineOrders[caseIndex], order, k)
			}
		}
		if shiftCount > 0 {
			metrics.MeanAbsoluteRankShift = shiftSum / float64(shiftCount)
		}
		for index, k := range kValues {
			metrics.TopK = append(metrics.TopK, ReplayTopK{K: k, Overlap: overlaps[index] / float64(len(bundle.Cases))})
		}
		result.Candidates = append(result.Candidates, metrics)
	}
	return result, nil
}

func ComparePolicyConfigurations(
	baseline domainrecommendation.PolicyConfiguration,
	candidate domainrecommendation.PolicyConfiguration,
) []PolicyDifference {
	differences := make([]PolicyDifference, 0, 3)
	if !reflect.DeepEqual(baseline.FeatureWeights, candidate.FeatureWeights) {
		differences = append(differences, PolicyDifference{Path: "feature_weights", Replayable: true})
	}
	if baseline.Diversity != candidate.Diversity {
		differences = append(differences, PolicyDifference{Path: "diversity", Replayable: true})
	}
	left := baseline
	right := candidate
	left.FeatureWeights = nil
	right.FeatureWeights = nil
	left.Diversity = domainrecommendation.DiversityRules{}
	right.Diversity = domainrecommendation.DiversityRules{}
	if !reflect.DeepEqual(left, right) {
		differences = append(differences, PolicyDifference{Path: "non_replayable_configuration", Replayable: false})
	}
	return differences
}

func replayOrder(replayCase ReplayCase, config domainrecommendation.PolicyConfiguration) ([]int64, error) {
	if replayCase.Name == "" || len(replayCase.Candidates) == 0 || len(replayCase.Candidates) > 500 ||
		len(replayCase.ExpectedOrder) != len(replayCase.Candidates) {
		return nil, ErrInvalidReplay
	}
	features := replayFeatureOrder()
	scored := make([]replayScoredCandidate, 0, len(replayCase.Candidates))
	seen := make(map[int64]struct{}, len(replayCase.Candidates))
	for _, candidate := range replayCase.Candidates {
		if candidate.VideoID <= 0 || candidate.AuthorKey == "" || candidate.PublishedAt.IsZero() ||
			len(candidate.RecallProviders) == 0 || len(candidate.ScoreComponents) != len(features) {
			return nil, ErrInvalidReplay
		}
		if _, duplicate := seen[candidate.VideoID]; duplicate {
			return nil, ErrInvalidReplay
		}
		seen[candidate.VideoID] = struct{}{}
		value := replayScoredCandidate{ReplayCandidate: candidate}
		for _, feature := range features {
			component, exists := candidate.ScoreComponents[feature]
			if !exists || math.IsNaN(component) || math.IsInf(component, 0) || component < 0 || component > 1 {
				return nil, ErrInvalidReplay
			}
			value.score += component * config.FeatureWeights[feature]
		}
		scored = append(scored, value)
	}
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		if !scored[i].PublishedAt.Equal(scored[j].PublishedAt) {
			return scored[i].PublishedAt.After(scored[j].PublishedAt)
		}
		return scored[i].VideoID > scored[j].VideoID
	})
	scored = diversifyReplay(scored, config.Diversity)
	order := make([]int64, len(scored))
	for index, candidate := range scored {
		order[index] = candidate.VideoID
	}
	return order, nil
}

func diversifyReplay(candidates []replayScoredCandidate, rules domainrecommendation.DiversityRules) []replayScoredCandidate {
	if len(candidates) < 2 {
		return candidates
	}
	remaining := append([]replayScoredCandidate(nil), candidates...)
	output := make([]replayScoredCandidate, 0, len(candidates))
	authorCount := make(map[string]int)
	for len(remaining) > 0 {
		index := replayDiversityIndex(remaining, output, authorCount, rules, true)
		if index < 0 {
			index = replayDiversityIndex(remaining, output, authorCount, rules, false)
		}
		if index < 0 {
			output = append(output, remaining...)
			break
		}
		candidate := remaining[index]
		output = append(output, candidate)
		authorCount[candidate.AuthorKey]++
		remaining = append(remaining[:index], remaining[index+1:]...)
	}
	return output
}

func replayDiversityIndex(remaining, output []replayScoredCandidate, authorCount map[string]int, rules domainrecommendation.DiversityRules, enforceGaps bool) int {
	for index, candidate := range remaining {
		if authorCount[candidate.AuthorKey] >= rules.MaxPerAuthor {
			continue
		}
		if enforceGaps && (replayRecentAuthor(output, candidate.AuthorKey, rules.MinAuthorGap) ||
			replayRecentBucket(output, replayContentBucket(candidate.RecallProviders), rules.MinContentGap)) {
			continue
		}
		return index
	}
	return -1
}

func replayRecentAuthor(candidates []replayScoredCandidate, author string, gap int) bool {
	for index := len(candidates) - 1; index >= 0 && len(candidates)-index <= gap; index-- {
		if candidates[index].AuthorKey == author {
			return true
		}
	}
	return false
}

func replayRecentBucket(candidates []replayScoredCandidate, bucket string, gap int) bool {
	if gap <= 0 || bucket == "" {
		return false
	}
	for index := len(candidates) - 1; index >= 0 && len(candidates)-index <= gap; index-- {
		if replayContentBucket(candidates[index].RecallProviders) == bucket {
			return true
		}
	}
	return false
}

func replayContentBucket(providers []string) string {
	if len(providers) == 0 {
		return ""
	}
	bucket := providers[0]
	for _, provider := range providers[1:] {
		if provider < bucket {
			bucket = provider
		}
	}
	return bucket
}

func replayFeatureOrder() []string {
	return []string{
		domainrecommendation.FeatureContentSimilarity,
		domainrecommendation.FeatureSessionSimilarity,
		domainrecommendation.FeatureSemanticSimilarity,
		domainrecommendation.FeatureHotness,
		domainrecommendation.FeatureFreshness,
		domainrecommendation.FeatureAuthorAffinity,
		domainrecommendation.FeatureFollowRelation,
		domainrecommendation.FeatureNegativePenalty,
		domainrecommendation.FeatureExposurePenalty,
	}
}

func validReplayK(values []int) bool {
	if len(values) == 0 {
		return false
	}
	previous := 0
	for _, value := range values {
		if value < 1 || value > 100 || value <= previous {
			return false
		}
		previous = value
	}
	return true
}

func rankPositions(order []int64) map[int64]int {
	positions := make(map[int64]int, len(order))
	for index, videoID := range order {
		positions[videoID] = index + 1
	}
	return positions
}

func topKOverlap(left, right []int64, k int) float64 {
	limit := min(k, len(left), len(right))
	if limit == 0 {
		return 0
	}
	seen := make(map[int64]struct{}, limit)
	for _, value := range left[:limit] {
		seen[value] = struct{}{}
	}
	intersection := 0
	for _, value := range right[:limit] {
		if _, exists := seen[value]; exists {
			intersection++
		}
	}
	return float64(intersection) / float64(limit)
}

func slicesEqualInt64(left, right []int64) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
