package applicationembedding

import (
	"errors"
	"math"
	"sort"
	"strings"
)

const MultimodalGoldenSetVersionV1 = "multimodal-golden-v1"

var ErrInvalidMultimodalGoldenSet = errors.New("invalid multimodal golden set")

type MultimodalGoldenSet struct {
	Version       string                 `json:"version"`
	ModelContract string                 `json:"model_contract"`
	MergeVersion  string                 `json:"merge_version"`
	Cutoff        int                    `json:"cutoff"`
	Cases         []MultimodalGoldenCase `json:"cases"`
}

type MultimodalGoldenCase struct {
	QueryID   string                             `json:"query_id"`
	Relevant  map[int64]int                      `json:"relevant"`
	Baselines map[string]MultimodalGoldenRanking `json:"baselines"`
}

type MultimodalGoldenRanking struct {
	RankedVideoIDs []int64 `json:"ranked_video_ids"`
	LatencyMS      float64 `json:"latency_ms"`
}

type MultimodalEvaluationReport struct {
	GoldenSetVersion string                                  `json:"golden_set_version"`
	ModelContract    string                                  `json:"model_contract"`
	MergeVersion     string                                  `json:"merge_version"`
	Cutoff           int                                     `json:"cutoff"`
	QueryCount       int                                     `json:"query_count"`
	RelevantCount    int                                     `json:"relevant_count"`
	Baselines        map[string]MultimodalBaselineEvaluation `json:"baselines"`
}

type MultimodalBaselineEvaluation struct {
	AvailableQueries int     `json:"available_queries"`
	RecallAtK        float64 `json:"recall_at_k"`
	NDCGAtK          float64 `json:"ndcg_at_k"`
	MRR              float64 `json:"mrr"`
	LexicalOverlap   float64 `json:"lexical_overlap"`
	LatencyP50MS     float64 `json:"latency_p50_ms"`
	LatencyP95MS     float64 `json:"latency_p95_ms"`
}

func EvaluateMultimodalGoldenSet(golden MultimodalGoldenSet) (*MultimodalEvaluationReport, error) {
	if golden.Version != MultimodalGoldenSetVersionV1 || strings.TrimSpace(golden.ModelContract) == "" ||
		strings.TrimSpace(golden.MergeVersion) == "" || golden.Cutoff < 1 || golden.Cutoff > 100 ||
		len(golden.Cases) == 0 || len(golden.Cases) > 1000 {
		return nil, ErrInvalidMultimodalGoldenSet
	}
	baselineNames := map[string]struct{}{}
	seenQueries := map[string]struct{}{}
	relevantCount := 0
	for _, evaluationCase := range golden.Cases {
		if strings.TrimSpace(evaluationCase.QueryID) == "" || len(evaluationCase.QueryID) > 64 ||
			len(evaluationCase.Relevant) == 0 || len(evaluationCase.Baselines) == 0 {
			return nil, ErrInvalidMultimodalGoldenSet
		}
		if _, duplicate := seenQueries[evaluationCase.QueryID]; duplicate {
			return nil, ErrInvalidMultimodalGoldenSet
		}
		seenQueries[evaluationCase.QueryID] = struct{}{}
		for videoID, grade := range evaluationCase.Relevant {
			if videoID <= 0 || grade < 1 || grade > 3 {
				return nil, ErrInvalidMultimodalGoldenSet
			}
			relevantCount++
		}
		for name, ranking := range evaluationCase.Baselines {
			if !validGoldenBaseline(name) || len(ranking.RankedVideoIDs) == 0 ||
				len(ranking.RankedVideoIDs) > 500 || math.IsNaN(ranking.LatencyMS) ||
				math.IsInf(ranking.LatencyMS, 0) || ranking.LatencyMS < 0 {
				return nil, ErrInvalidMultimodalGoldenSet
			}
			seen := map[int64]struct{}{}
			for _, videoID := range ranking.RankedVideoIDs {
				if videoID <= 0 {
					return nil, ErrInvalidMultimodalGoldenSet
				}
				if _, duplicate := seen[videoID]; duplicate {
					return nil, ErrInvalidMultimodalGoldenSet
				}
				seen[videoID] = struct{}{}
			}
			baselineNames[name] = struct{}{}
		}
	}
	if _, exists := baselineNames["lexical"]; !exists {
		return nil, ErrInvalidMultimodalGoldenSet
	}
	report := &MultimodalEvaluationReport{
		GoldenSetVersion: golden.Version, ModelContract: golden.ModelContract,
		MergeVersion: golden.MergeVersion, Cutoff: golden.Cutoff,
		QueryCount: len(golden.Cases), RelevantCount: relevantCount,
		Baselines: map[string]MultimodalBaselineEvaluation{},
	}
	for name := range baselineNames {
		var recallSum, ndcgSum, mrrSum, overlapSum float64
		latencies := []float64{}
		available := 0
		for _, evaluationCase := range golden.Cases {
			ranking, exists := evaluationCase.Baselines[name]
			if !exists {
				continue
			}
			available++
			latencies = append(latencies, ranking.LatencyMS)
			recallSum += recallAtK(evaluationCase.Relevant, ranking.RankedVideoIDs, golden.Cutoff)
			ndcgSum += ndcgAtK(evaluationCase.Relevant, ranking.RankedVideoIDs, golden.Cutoff)
			mrrSum += reciprocalRank(evaluationCase.Relevant, ranking.RankedVideoIDs)
			lexical := evaluationCase.Baselines["lexical"].RankedVideoIDs
			overlapSum += rankingOverlap(lexical, ranking.RankedVideoIDs, golden.Cutoff)
		}
		if available == 0 {
			continue
		}
		sort.Float64s(latencies)
		report.Baselines[name] = MultimodalBaselineEvaluation{
			AvailableQueries: available,
			RecallAtK:        recallSum / float64(available),
			NDCGAtK:          ndcgSum / float64(available),
			MRR:              mrrSum / float64(available),
			LexicalOverlap:   overlapSum / float64(available),
			LatencyP50MS:     percentile(latencies, 0.50),
			LatencyP95MS:     percentile(latencies, 0.95),
		}
	}
	return report, nil
}

func validGoldenBaseline(name string) bool {
	switch name {
	case "lexical", "text", "image", "multimodal", "hybrid":
		return true
	default:
		return false
	}
}

func recallAtK(relevant map[int64]int, ranked []int64, cutoff int) float64 {
	hits := 0
	for _, videoID := range ranked[:min(len(ranked), cutoff)] {
		if relevant[videoID] > 0 {
			hits++
		}
	}
	return float64(hits) / float64(len(relevant))
}

func ndcgAtK(relevant map[int64]int, ranked []int64, cutoff int) float64 {
	dcg := 0.0
	for index, videoID := range ranked[:min(len(ranked), cutoff)] {
		grade := relevant[videoID]
		if grade > 0 {
			dcg += (math.Pow(2, float64(grade)) - 1) / math.Log2(float64(index)+2)
		}
	}
	grades := make([]int, 0, len(relevant))
	for _, grade := range relevant {
		grades = append(grades, grade)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(grades)))
	idcg := 0.0
	for index, grade := range grades[:min(len(grades), cutoff)] {
		idcg += (math.Pow(2, float64(grade)) - 1) / math.Log2(float64(index)+2)
	}
	if idcg == 0 {
		return 0
	}
	return dcg / idcg
}

func reciprocalRank(relevant map[int64]int, ranked []int64) float64 {
	for index, videoID := range ranked {
		if relevant[videoID] > 0 {
			return 1 / float64(index+1)
		}
	}
	return 0
}

func rankingOverlap(left, right []int64, cutoff int) float64 {
	left = left[:min(len(left), cutoff)]
	right = right[:min(len(right), cutoff)]
	if len(left) == 0 && len(right) == 0 {
		return 1
	}
	leftSet := make(map[int64]struct{}, len(left))
	for _, videoID := range left {
		leftSet[videoID] = struct{}{}
	}
	overlap := 0
	for _, videoID := range right {
		if _, found := leftSet[videoID]; found {
			overlap++
		}
	}
	return float64(overlap) / float64(max(len(left), len(right)))
}

func percentile(values []float64, value float64) float64 {
	if len(values) == 0 {
		return 0
	}
	index := int(math.Ceil(value*float64(len(values)))) - 1
	return values[max(0, min(index, len(values)-1))]
}
