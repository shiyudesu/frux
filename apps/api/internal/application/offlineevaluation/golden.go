package applicationofflineevaluation

import (
	"errors"
	"math"
	"sort"
	"strings"
)

const (
	GoldenVersion    = "human-golden-v1"
	GoldenRubric     = "semantic-0-3-v1"
	GoldenProvenance = "frux_blinded_human"

	GoldenKindQuery       = "query_relevance"
	GoldenKindSimilar     = "similar_video"
	GoldenKindDirection   = "session_direction"
	GoldenKindSuppression = "negative_suppression"
)

var ErrInvalidGolden = errors.New("invalid human Golden Set")

type GoldenCandidate struct {
	Key         string `json:"key"`
	Judgments   []int  `json:"judgments"`
	Adjudicated *int   `json:"adjudicated,omitempty"`
}

type GoldenRanking struct {
	Name      string   `json:"name"`
	Order     []string `json:"order"`
	Direction string   `json:"direction,omitempty"`
}

type GoldenCase struct {
	Name                   string            `json:"name"`
	Kind                   string            `json:"kind"`
	Candidates             []GoldenCandidate `json:"candidates"`
	Rankings               []GoldenRanking   `json:"rankings"`
	ExpectedDirection      string            `json:"expected_direction,omitempty"`
	SuppressedCandidateKey string            `json:"suppressed_candidate_key,omitempty"`
	SuppressionCutoff      int               `json:"suppression_cutoff,omitempty"`
}

type GoldenBundle struct {
	Version    string       `json:"version"`
	Rubric     string       `json:"rubric"`
	Provenance string       `json:"provenance"`
	Cases      []GoldenCase `json:"cases"`
}

type GoldenMetric struct {
	Available   bool    `json:"available"`
	Value       float64 `json:"value,omitempty"`
	Numerator   float64 `json:"numerator"`
	Denominator int     `json:"denominator"`
}

type GoldenTopK struct {
	K    int          `json:"k"`
	NDCG GoldenMetric `json:"ndcg"`
}

type GoldenRankingMetrics struct {
	Name                string       `json:"name"`
	SemanticCases       int          `json:"semantic_cases"`
	TopK                []GoldenTopK `json:"top_k"`
	DirectionAccuracy   GoldenMetric `json:"direction_accuracy"`
	SuppressionAccuracy GoldenMetric `json:"suppression_accuracy"`
}

type GoldenEvaluation struct {
	Version       string                 `json:"version"`
	Rubric        string                 `json:"rubric"`
	Cases         int                    `json:"cases"`
	Candidates    int                    `json:"candidates"`
	LabelCoverage GoldenMetric           `json:"label_coverage"`
	Agreement     GoldenMetric           `json:"agreement"`
	Rankings      []GoldenRankingMetrics `json:"rankings"`
}

func EvaluateGolden(bundle GoldenBundle, kValues []int) (*GoldenEvaluation, error) {
	if bundle.Version != GoldenVersion || bundle.Rubric != GoldenRubric || bundle.Provenance != GoldenProvenance ||
		len(bundle.Cases) == 0 || len(bundle.Cases) > 500 || !validGoldenK(kValues) {
		return nil, ErrInvalidGolden
	}
	result := &GoldenEvaluation{Version: bundle.Version, Rubric: bundle.Rubric, Cases: len(bundle.Cases)}
	rankingNames := []string{}
	rankingSeen := map[string]struct{}{}
	agreementNumerator := 0.0
	agreementDenominator := 0
	labels := make(map[string]map[string]int, len(bundle.Cases))
	for _, goldenCase := range bundle.Cases {
		caseName := normalizedGoldenToken(goldenCase.Name, 128)
		if !validGoldenCaseKind(goldenCase.Kind) || caseName == "" || caseName != goldenCase.Name ||
			len(goldenCase.Candidates) < 2 || len(goldenCase.Candidates) > 500 || len(goldenCase.Rankings) == 0 || len(goldenCase.Rankings) > 20 {
			return nil, ErrInvalidGolden
		}
		caseLabels := make(map[string]int, len(goldenCase.Candidates))
		for _, candidate := range goldenCase.Candidates {
			key := normalizedGoldenToken(candidate.Key, 128)
			if key == "" || key != candidate.Key || len(candidate.Judgments) < 2 || len(candidate.Judgments) > 10 {
				return nil, ErrInvalidGolden
			}
			if _, duplicate := caseLabels[key]; duplicate {
				return nil, ErrInvalidGolden
			}
			minimum, maximum := 3, 0
			allEqual := true
			for index, judgment := range candidate.Judgments {
				if judgment < 0 || judgment > 3 {
					return nil, ErrInvalidGolden
				}
				minimum = min(minimum, judgment)
				maximum = max(maximum, judgment)
				if index > 0 && judgment != candidate.Judgments[0] {
					allEqual = false
				}
			}
			if maximum-minimum >= 2 && candidate.Adjudicated == nil {
				return nil, ErrInvalidGolden
			}
			label := medianJudgment(candidate.Judgments)
			if candidate.Adjudicated != nil {
				if *candidate.Adjudicated < 0 || *candidate.Adjudicated > 3 {
					return nil, ErrInvalidGolden
				}
				label = *candidate.Adjudicated
			}
			caseLabels[key] = label
			result.Candidates++
			agreementDenominator++
			if allEqual {
				agreementNumerator++
			}
		}
		labels[goldenCase.Name] = caseLabels
		caseRankingNames := make(map[string]struct{}, len(goldenCase.Rankings))
		for _, ranking := range goldenCase.Rankings {
			name := normalizedGoldenToken(ranking.Name, 64)
			if name == "" || name != ranking.Name || len(ranking.Order) != len(caseLabels) {
				return nil, ErrInvalidGolden
			}
			if _, duplicate := caseRankingNames[name]; duplicate {
				return nil, ErrInvalidGolden
			}
			caseRankingNames[name] = struct{}{}
			if !sameGoldenKeys(ranking.Order, caseLabels) {
				return nil, ErrInvalidGolden
			}
			if goldenCase.Kind == GoldenKindDirection && ranking.Direction != "toward" && ranking.Direction != "away" {
				return nil, ErrInvalidGolden
			}
			if _, exists := rankingSeen[name]; !exists {
				rankingSeen[name] = struct{}{}
				rankingNames = append(rankingNames, name)
			}
		}
		if goldenCase.Kind == GoldenKindDirection && goldenCase.ExpectedDirection != "toward" && goldenCase.ExpectedDirection != "away" {
			return nil, ErrInvalidGolden
		}
		if goldenCase.Kind == GoldenKindSuppression {
			if _, exists := caseLabels[goldenCase.SuppressedCandidateKey]; !exists || goldenCase.SuppressionCutoff < 1 || goldenCase.SuppressionCutoff >= len(caseLabels) {
				return nil, ErrInvalidGolden
			}
		}
	}
	sort.Strings(rankingNames)
	result.LabelCoverage = goldenRatio(float64(result.Candidates), result.Candidates)
	result.Agreement = goldenRatio(agreementNumerator, agreementDenominator)
	for _, rankingName := range rankingNames {
		metrics := GoldenRankingMetrics{Name: rankingName}
		ndcgSums := make([]float64, len(kValues))
		directionCorrect, directionTotal := 0.0, 0
		suppressionCorrect, suppressionTotal := 0.0, 0
		for _, goldenCase := range bundle.Cases {
			ranking, exists := goldenRankingByName(goldenCase.Rankings, rankingName)
			if !exists {
				return nil, ErrInvalidGolden
			}
			switch goldenCase.Kind {
			case GoldenKindQuery, GoldenKindSimilar:
				metrics.SemanticCases++
				for index, k := range kValues {
					ndcgSums[index] += goldenNDCG(ranking.Order, labels[goldenCase.Name], k)
				}
			case GoldenKindDirection:
				directionTotal++
				if ranking.Direction == goldenCase.ExpectedDirection {
					directionCorrect++
				}
			case GoldenKindSuppression:
				suppressionTotal++
				if goldenRank(ranking.Order, goldenCase.SuppressedCandidateKey) > goldenCase.SuppressionCutoff {
					suppressionCorrect++
				}
			}
		}
		for index, k := range kValues {
			metric := GoldenMetric{}
			if metrics.SemanticCases > 0 {
				metric = goldenRatio(ndcgSums[index], metrics.SemanticCases)
			}
			metrics.TopK = append(metrics.TopK, GoldenTopK{K: k, NDCG: metric})
		}
		metrics.DirectionAccuracy = goldenRatio(directionCorrect, directionTotal)
		metrics.SuppressionAccuracy = goldenRatio(suppressionCorrect, suppressionTotal)
		result.Rankings = append(result.Rankings, metrics)
	}
	return result, nil
}

func goldenNDCG(order []string, labels map[string]int, k int) float64 {
	limit := min(k, len(order))
	dcg := 0.0
	for index, key := range order[:limit] {
		dcg += (math.Pow(2, float64(labels[key])) - 1) / math.Log2(float64(index)+2)
	}
	ideal := make([]int, 0, len(labels))
	for _, label := range labels {
		ideal = append(ideal, label)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(ideal)))
	idcg := 0.0
	for index, label := range ideal[:min(k, len(ideal))] {
		idcg += (math.Pow(2, float64(label)) - 1) / math.Log2(float64(index)+2)
	}
	if idcg == 0 {
		return 0
	}
	return dcg / idcg
}

func goldenRatio(numerator float64, denominator int) GoldenMetric {
	if denominator <= 0 {
		return GoldenMetric{}
	}
	return GoldenMetric{Available: true, Value: numerator / float64(denominator), Numerator: numerator, Denominator: denominator}
}

func validGoldenCaseKind(value string) bool {
	return value == GoldenKindQuery || value == GoldenKindSimilar || value == GoldenKindDirection || value == GoldenKindSuppression
}

func validGoldenK(values []int) bool {
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

func normalizedGoldenToken(value string, maximum int) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > maximum || strings.ContainsAny(value, "\r\n\x00") {
		return ""
	}
	return value
}

func medianJudgment(values []int) int {
	cloned := append([]int(nil), values...)
	sort.Ints(cloned)
	return cloned[(len(cloned)-1)/2]
}

func sameGoldenKeys(order []string, labels map[string]int) bool {
	seen := make(map[string]struct{}, len(order))
	for _, key := range order {
		if _, exists := labels[key]; !exists {
			return false
		}
		if _, duplicate := seen[key]; duplicate {
			return false
		}
		seen[key] = struct{}{}
	}
	return len(seen) == len(labels)
}

func goldenRankingByName(rankings []GoldenRanking, name string) (GoldenRanking, bool) {
	for _, ranking := range rankings {
		if ranking.Name == name {
			return ranking, true
		}
	}
	return GoldenRanking{}, false
}

func goldenRank(order []string, target string) int {
	for index, key := range order {
		if key == target {
			return index + 1
		}
	}
	return 0
}
