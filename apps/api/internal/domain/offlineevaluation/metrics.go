package domainofflineevaluation

import (
	"math"
	"sort"
)

type Metric struct {
	Availability Availability  `json:"availability"`
	Value        *float64      `json:"value,omitempty"`
	Numerator    float64       `json:"numerator"`
	Denominator  int64         `json:"denominator"`
	Reason       ExclusionCode `json:"reason,omitempty"`
}

type TopKMetrics struct {
	K       int    `json:"k"`
	Recall  Metric `json:"recall"`
	NDCG    Metric `json:"ndcg"`
	HitRate Metric `json:"hit_rate"`
}

type GroupMetrics struct {
	Coverage          Metric `json:"coverage"`
	LargestGroupShare Metric `json:"largest_group_share"`
	Concentration     Metric `json:"concentration"`
	RepeatedRuns      int64  `json:"repeated_runs"`
}

type BaselineMetrics struct {
	Baseline         Baseline              `json:"baseline"`
	CasesTotal       int                   `json:"cases_total"`
	CasesAvailable   int                   `json:"cases_available"`
	Availability     Metric                `json:"case_coverage"`
	Exclusions       map[ExclusionCode]int `json:"exclusions,omitempty"`
	TopK             []TopKMetrics         `json:"top_k"`
	MRR              Metric                `json:"mrr"`
	CatalogCoverage  Metric                `json:"catalog_coverage"`
	Category         GroupMetrics          `json:"category"`
	Author           GroupMetrics          `json:"author"`
	RepeatedItemRuns int64                 `json:"repeated_item_runs"`
	Work             RankingWork           `json:"work"`
}

func AggregateBaselineMetrics(
	dataset *Dataset,
	cases []EvaluationCase,
	rankings []Ranking,
	kValues []int,
) (BaselineMetrics, error) {
	result := BaselineMetrics{CasesTotal: len(cases), Exclusions: make(map[ExclusionCode]int)}
	if dataset == nil || len(cases) == 0 || len(rankings) != len(cases) || !ValidK(kValues) {
		return result, ErrInvalidEvaluationDataset
	}
	result.Baseline = rankings[0].Baseline
	available := make([]int, 0, len(rankings))
	for index, ranking := range rankings {
		if ranking.Baseline != result.Baseline {
			return BaselineMetrics{}, ErrInvalidEvaluationDataset
		}
		if !ranking.Available {
			result.Exclusions[ranking.Reason]++
			continue
		}
		available = append(available, index)
		result.Work.CandidateScores += ranking.Work.CandidateScores
		result.Work.VectorComponents += ranking.Work.VectorComponents
		result.Work.Comparisons += ranking.Work.Comparisons
	}
	result.CasesAvailable = len(available)
	result.Availability = ratioMetric(int64(result.CasesAvailable), int64(result.CasesTotal), "")
	if len(available) == 0 {
		result.MRR = unavailableMetric(ExclusionMissingFeature)
		result.CatalogCoverage = unavailableMetric(ExclusionMissingFeature)
		result.Category = unavailableGroupMetrics(ExclusionMissingCategory)
		result.Author = unavailableGroupMetrics(ExclusionUnsupportedMetric)
		for _, k := range kValues {
			result.TopK = append(result.TopK, TopKMetrics{K: k, Recall: unavailableMetric(ExclusionMissingFeature), NDCG: unavailableMetric(ExclusionMissingFeature), HitRate: unavailableMetric(ExclusionMissingFeature)})
		}
		return result, nil
	}
	maxK := kValues[len(kValues)-1]
	selected := make([]Item, 0, len(available)*maxK)
	selectedKeys := make(map[string]struct{})
	reciprocalSum := 0.0
	hits := make([]int64, len(kValues))
	ndcg := make([]float64, len(kValues))
	for _, index := range available {
		ranking := rankings[index]
		targetRank := 0
		for rank, item := range ranking.Items {
			if item.ItemKey == cases[index].TargetItemKey {
				targetRank = rank + 1
				break
			}
		}
		if targetRank > 0 {
			reciprocalSum += 1 / float64(targetRank)
		}
		for metricIndex, k := range kValues {
			if targetRank > 0 && targetRank <= k {
				hits[metricIndex]++
				ndcg[metricIndex] += 1 / math.Log2(float64(targetRank)+1)
			}
		}
		limit := min(maxK, len(ranking.Items))
		for _, ranked := range ranking.Items[:limit] {
			item, exists := dataset.Items[ranked.ItemKey]
			if !exists {
				return BaselineMetrics{}, ErrInvalidEvaluationDataset
			}
			selected = append(selected, item)
			selectedKeys[item.Key] = struct{}{}
		}
	}
	denominator := int64(len(available))
	for index, k := range kValues {
		result.TopK = append(result.TopK, TopKMetrics{
			K:       k,
			Recall:  ratioMetric(hits[index], denominator, ""),
			NDCG:    valueMetric(ndcg[index]/float64(denominator), ndcg[index], denominator),
			HitRate: ratioMetric(hits[index], denominator, ""),
		})
	}
	result.MRR = valueMetric(reciprocalSum/float64(denominator), reciprocalSum, denominator)
	result.CatalogCoverage = ratioMetric(int64(len(selectedKeys)), int64(len(dataset.Items)), "")
	result.RepeatedItemRuns = repeatedItemRuns(selected)
	result.Category = aggregateGroupMetrics(dataset, selected, true)
	result.Author = aggregateGroupMetrics(dataset, selected, false)
	return result, nil
}

func aggregateGroupMetrics(dataset *Dataset, selected []Item, category bool) GroupMetrics {
	universe := make(map[string]struct{})
	for _, item := range dataset.Items {
		values := itemGroupValues(item, category)
		for _, value := range values {
			universe[value] = struct{}{}
		}
	}
	counts := make(map[string]int64)
	ordered := make([]string, 0, len(selected))
	for _, item := range selected {
		values := itemGroupValues(item, category)
		if len(values) == 0 {
			return unavailableGroupMetrics(ExclusionUnsupportedMetric)
		}
		primary := values[0]
		counts[primary]++
		ordered = append(ordered, primary)
	}
	if len(universe) == 0 || len(ordered) == 0 {
		return unavailableGroupMetrics(ExclusionUnsupportedMetric)
	}
	largest := int64(0)
	hhi := 0.0
	for _, count := range counts {
		largest = max(largest, count)
		share := float64(count) / float64(len(ordered))
		hhi += share * share
	}
	return GroupMetrics{
		Coverage:          ratioMetric(int64(len(counts)), int64(len(universe)), ""),
		LargestGroupShare: ratioMetric(largest, int64(len(ordered)), ""),
		Concentration:     valueMetric(hhi, hhi, int64(len(ordered))),
		RepeatedRuns:      repeatedStringRuns(ordered),
	}
}

func itemGroupValues(item Item, category bool) []string {
	if category {
		return item.Categories
	}
	if item.AuthorKey == "" {
		return nil
	}
	return []string{item.AuthorKey}
}

func repeatedItemRuns(items []Item) int64 {
	keys := make([]string, len(items))
	for index, item := range items {
		keys[index] = item.Key
	}
	return repeatedStringRuns(keys)
}

func repeatedStringRuns(values []string) int64 {
	var repeated int64
	for index := 1; index < len(values); index++ {
		if values[index] == values[index-1] {
			repeated++
		}
	}
	return repeated
}

func ratioMetric(numerator, denominator int64, reason ExclusionCode) Metric {
	if denominator <= 0 {
		return unavailableMetric(reason)
	}
	return valueMetric(float64(numerator)/float64(denominator), float64(numerator), denominator)
}

func valueMetric(value float64, numerator float64, denominator int64) Metric {
	cloned := value
	return Metric{Availability: AvailabilityAvailable, Value: &cloned, Numerator: numerator, Denominator: denominator}
}

func unavailableMetric(reason ExclusionCode) Metric {
	if reason == "" {
		reason = ExclusionUnsupportedMetric
	}
	return Metric{Availability: AvailabilityUnavailable, Reason: reason}
}

func unavailableGroupMetrics(reason ExclusionCode) GroupMetrics {
	return GroupMetrics{
		Coverage: unavailableMetric(reason), LargestGroupShare: unavailableMetric(reason),
		Concentration: unavailableMetric(reason),
	}
}

func SortedExclusionCodes(values map[ExclusionCode]int) []ExclusionCode {
	codes := make([]ExclusionCode, 0, len(values))
	for code := range values {
		codes = append(codes, code)
	}
	sort.Slice(codes, func(i, j int) bool { return codes[i] < codes[j] })
	return codes
}
