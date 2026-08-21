package domainofflineevaluation

import (
	"math"
	"sort"
	"time"
)

type RankedItem struct {
	ItemKey string
	Score   float64
}

type Ranking struct {
	Baseline  Baseline
	Available bool
	Reason    ExclusionCode
	Items     []RankedItem
	Work      RankingWork
}

type RankingWork struct {
	CandidateScores  int64 `json:"candidate_scores"`
	VectorComponents int64 `json:"vector_components"`
	Comparisons      int64 `json:"comparisons"`
}

func Rank(dataset *Dataset, evaluationCase EvaluationCase, profile CaseProfile, baseline Baseline) Ranking {
	result := Ranking{Baseline: baseline}
	if dataset == nil || !profile.Valid() || !ValidBaseline(baseline) || len(evaluationCase.CandidateKeys) == 0 {
		result.Reason = ExclusionMissingFeature
		return result
	}
	scores := make(map[string]float64, len(evaluationCase.CandidateKeys))
	var ok bool
	switch baseline {
	case BaselinePopularity:
		scores, ok = popularityScores(dataset, evaluationCase, profile)
	case BaselineRecent:
		scores, ok = recentScores(dataset, evaluationCase)
	case BaselineCategory:
		scores, ok = categoryScores(dataset, evaluationCase, profile)
	case BaselineText:
		scores, ok = featureScores(dataset, evaluationCase, profile, FeatureText, false)
	case BaselineImage:
		scores, ok = featureScores(dataset, evaluationCase, profile, FeatureImage, false)
	case BaselineMultimodal:
		scores, ok = featureScores(dataset, evaluationCase, profile, FeatureMultimodal, false)
	case BaselineMultimodalSession:
		scores, ok = featureScores(dataset, evaluationCase, profile, FeatureMultimodal, true)
	}
	if !ok {
		result.Reason = baselineMissingReason(baseline)
		return result
	}
	result.Available = true
	result.Work.CandidateScores = int64(len(evaluationCase.CandidateKeys))
	if baseline == BaselineCategory {
		for _, interaction := range evaluationCase.History {
			result.Work.VectorComponents += int64(len(dataset.Items[interaction.ItemKey].Categories))
		}
		for _, itemKey := range evaluationCase.CandidateKeys {
			result.Work.VectorComponents += int64(len(dataset.Items[itemKey].Categories))
		}
	}
	if baseline == BaselineText || baseline == BaselineImage || baseline == BaselineMultimodal || baseline == BaselineMultimodalSession {
		dimension := int64(dataset.FeatureDimensions[baselineFeatureChannel(baseline)])
		result.Work.VectorComponents = dimension * int64(len(evaluationCase.Session)+len(evaluationCase.CandidateKeys))
	}
	result.Items = make([]RankedItem, 0, len(evaluationCase.CandidateKeys))
	for _, itemKey := range evaluationCase.CandidateKeys {
		result.Items = append(result.Items, RankedItem{ItemKey: itemKey, Score: scores[itemKey]})
	}
	sort.SliceStable(result.Items, func(i, j int) bool {
		result.Work.Comparisons++
		if result.Items[i].Score != result.Items[j].Score {
			return result.Items[i].Score > result.Items[j].Score
		}
		return result.Items[i].ItemKey < result.Items[j].ItemKey
	})
	return result
}

func baselineFeatureChannel(baseline Baseline) FeatureChannel {
	switch baseline {
	case BaselineText:
		return FeatureText
	case BaselineImage:
		return FeatureImage
	default:
		return FeatureMultimodal
	}
}

func popularityScores(dataset *Dataset, evaluationCase EvaluationCase, profile CaseProfile) (map[string]float64, bool) {
	scores := candidateScoreMap(evaluationCase.CandidateKeys)
	for _, interaction := range dataset.Interactions {
		if !interaction.OccurredAt.Before(evaluationCase.Cutoff) || profile.Classify(interaction.WatchRatio) != FeedbackPositive {
			continue
		}
		if _, exists := scores[interaction.ItemKey]; exists {
			scores[interaction.ItemKey]++
		}
	}
	return scores, true
}

func recentScores(dataset *Dataset, evaluationCase EvaluationCase) (map[string]float64, bool) {
	scores := candidateScoreMap(evaluationCase.CandidateKeys)
	latest := make(map[string]time.Time, len(scores))
	for _, interaction := range dataset.Interactions {
		if !interaction.OccurredAt.Before(evaluationCase.Cutoff) {
			continue
		}
		if _, exists := scores[interaction.ItemKey]; !exists {
			continue
		}
		if interaction.OccurredAt.After(latest[interaction.ItemKey]) {
			latest[interaction.ItemKey] = interaction.OccurredAt
		}
	}
	for itemKey, occurredAt := range latest {
		scores[itemKey] = float64(occurredAt.UnixNano())
	}
	return scores, true
}

func categoryScores(dataset *Dataset, evaluationCase EvaluationCase, profile CaseProfile) (map[string]float64, bool) {
	user := make(map[string]float64)
	userNorm := 0.0
	for _, interaction := range evaluationCase.History {
		class := profile.Classify(interaction.WatchRatio)
		if class == FeedbackQuickSkip || class == FeedbackMissing {
			continue
		}
		item, exists := dataset.Items[interaction.ItemKey]
		if !exists || len(item.Categories) == 0 {
			return nil, false
		}
		weight := 1.0
		if interaction.WatchRatio != nil {
			weight = math.Min(1, *interaction.WatchRatio)
		}
		for _, category := range item.Categories {
			user[category] += weight
		}
	}
	for _, weight := range user {
		userNorm += weight * weight
	}
	if userNorm <= 0 {
		return nil, false
	}
	userNorm = math.Sqrt(userNorm)
	scores := candidateScoreMap(evaluationCase.CandidateKeys)
	for _, itemKey := range evaluationCase.CandidateKeys {
		item, exists := dataset.Items[itemKey]
		if !exists || len(item.Categories) == 0 {
			return nil, false
		}
		dot := 0.0
		for _, category := range item.Categories {
			dot += user[category]
		}
		scores[itemKey] = dot / (userNorm * math.Sqrt(float64(len(item.Categories))))
	}
	return scores, true
}

func featureScores(
	dataset *Dataset,
	evaluationCase EvaluationCase,
	profile CaseProfile,
	channel FeatureChannel,
	withNegative bool,
) (map[string]float64, bool) {
	dimension := dataset.FeatureDimensions[channel]
	if dimension < 2 {
		return nil, false
	}
	positive := make([]float64, dimension)
	negative := make([]float64, dimension)
	positiveCount := 0
	negativeCount := 0
	for _, interaction := range evaluationCase.Session {
		item, exists := dataset.Items[interaction.ItemKey]
		if !exists {
			return nil, false
		}
		vector, exists := item.Features[channel]
		if !exists || len(vector) != dimension {
			return nil, false
		}
		switch profile.Classify(interaction.WatchRatio) {
		case FeedbackPositive:
			addVector(positive, vector)
			positiveCount++
		case FeedbackQuickSkip:
			if withNegative {
				addVector(negative, vector)
				negativeCount++
			}
		}
	}
	if positiveCount == 0 {
		return nil, false
	}
	scaleVector(positive, 1/float64(positiveCount))
	if withNegative && negativeCount > 0 {
		scaleVector(negative, 1/float64(negativeCount))
		for index := range positive {
			positive[index] -= negative[index]
		}
	}
	if vectorNorm(positive) == 0 {
		return nil, false
	}
	scores := candidateScoreMap(evaluationCase.CandidateKeys)
	for _, itemKey := range evaluationCase.CandidateKeys {
		item, exists := dataset.Items[itemKey]
		if !exists {
			return nil, false
		}
		vector, exists := item.Features[channel]
		if !exists || len(vector) != dimension || vectorNorm(vector) == 0 {
			return nil, false
		}
		scores[itemKey] = cosine(positive, vector)
	}
	return scores, true
}

func candidateScoreMap(candidates []string) map[string]float64 {
	scores := make(map[string]float64, len(candidates))
	for _, itemKey := range candidates {
		scores[itemKey] = 0
	}
	return scores
}

func baselineMissingReason(baseline Baseline) ExclusionCode {
	if baseline == BaselineCategory {
		return ExclusionMissingCategory
	}
	return ExclusionMissingFeature
}

func addVector(target, source []float64) {
	for index := range target {
		target[index] += source[index]
	}
}

func scaleVector(vector []float64, scale float64) {
	for index := range vector {
		vector[index] *= scale
	}
}

func vectorNorm(vector []float64) float64 {
	sum := 0.0
	for _, component := range vector {
		sum += component * component
	}
	return math.Sqrt(sum)
}

func cosine(left, right []float64) float64 {
	dot := 0.0
	for index := range left {
		dot += left[index] * right[index]
	}
	return dot / (vectorNorm(left) * vectorNorm(right))
}
