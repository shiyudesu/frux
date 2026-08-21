package applicationofflineevaluation

import (
	"errors"

	domainofflineevaluation "github.com/shiyudesu/frux/internal/domain/offlineevaluation"
)

var ErrInvalidPublicEvaluation = errors.New("invalid public dataset evaluation")

type ExclusionCount struct {
	Code  domainofflineevaluation.ExclusionCode `json:"code"`
	Count int                                   `json:"count"`
}

type DatasetSummary struct {
	Dataset      domainofflineevaluation.DatasetKind `json:"dataset"`
	Release      string                              `json:"release"`
	Schema       string                              `json:"schema"`
	Users        int                                 `json:"users"`
	Items        int                                 `json:"items"`
	Interactions int                                 `json:"interactions"`
	Cases        int                                 `json:"cases"`
	Neutral      int                                 `json:"neutral_interactions"`
	Missing      int                                 `json:"missing_watch_ratio"`
	Exclusions   []ExclusionCount                    `json:"case_exclusions,omitempty"`
}

type BaselineDelta struct {
	Baseline     domainofflineevaluation.Baseline `json:"baseline"`
	K            int                              `json:"k"`
	Available    bool                             `json:"available"`
	RecallDelta  float64                          `json:"recall_delta,omitempty"`
	NDCGDelta    float64                          `json:"ndcg_delta,omitempty"`
	HitRateDelta float64                          `json:"hit_rate_delta,omitempty"`
	MRRDelta     float64                          `json:"mrr_delta,omitempty"`
}

type PublicEvaluation struct {
	Profile   domainofflineevaluation.CaseProfile       `json:"profile"`
	K         []int                                     `json:"k"`
	Summary   DatasetSummary                            `json:"summary"`
	Baselines []domainofflineevaluation.BaselineMetrics `json:"baselines"`
	Deltas    []BaselineDelta                           `json:"popularity_deltas"`
}

func EvaluatePublicDataset(
	dataset *domainofflineevaluation.Dataset,
	profile domainofflineevaluation.CaseProfile,
	kValues []int,
	maxCases int,
) (*PublicEvaluation, error) {
	if dataset == nil || !profile.Valid() || !domainofflineevaluation.ValidK(kValues) {
		return nil, ErrInvalidPublicEvaluation
	}
	build, err := domainofflineevaluation.BuildCases(dataset, profile, maxCases)
	if err != nil || len(build.Cases) == 0 {
		return nil, errors.Join(ErrInvalidPublicEvaluation, err)
	}
	result := &PublicEvaluation{
		Profile: profile, K: append([]int(nil), kValues...),
		Summary: DatasetSummary{
			Dataset: dataset.Kind, Release: dataset.Release, Schema: dataset.Schema,
			Users: build.Users, Items: len(dataset.Items), Interactions: len(dataset.Interactions),
			Cases: len(build.Cases), Neutral: build.Neutral, Missing: build.Missing,
		},
		Baselines: make([]domainofflineevaluation.BaselineMetrics, 0, len(domainofflineevaluation.Baselines())),
	}
	for _, code := range domainofflineevaluation.SortedExclusionCodes(build.Exclusions) {
		result.Summary.Exclusions = append(result.Summary.Exclusions, ExclusionCount{Code: code, Count: build.Exclusions[code]})
	}
	for _, baseline := range domainofflineevaluation.Baselines() {
		rankings := make([]domainofflineevaluation.Ranking, len(build.Cases))
		for index, evaluationCase := range build.Cases {
			rankings[index] = domainofflineevaluation.Rank(dataset, evaluationCase, profile, baseline)
		}
		metrics, metricErr := domainofflineevaluation.AggregateBaselineMetrics(dataset, build.Cases, rankings, kValues)
		if metricErr != nil {
			return nil, errors.Join(ErrInvalidPublicEvaluation, metricErr)
		}
		result.Baselines = append(result.Baselines, metrics)
	}
	result.Deltas = popularityDeltas(result.Baselines)
	return result, nil
}

func popularityDeltas(metrics []domainofflineevaluation.BaselineMetrics) []BaselineDelta {
	if len(metrics) == 0 || metrics[0].Baseline != domainofflineevaluation.BaselinePopularity {
		return nil
	}
	baseline := metrics[0]
	deltas := make([]BaselineDelta, 0, (len(metrics)-1)*len(baseline.TopK))
	for _, candidate := range metrics[1:] {
		for index, candidateK := range candidate.TopK {
			delta := BaselineDelta{Baseline: candidate.Baseline, K: candidateK.K}
			if index < len(baseline.TopK) && metricsAvailable(
				baseline.TopK[index].Recall, baseline.TopK[index].NDCG, baseline.TopK[index].HitRate, baseline.MRR,
				candidateK.Recall, candidateK.NDCG, candidateK.HitRate, candidate.MRR,
			) {
				delta.Available = true
				delta.RecallDelta = *candidateK.Recall.Value - *baseline.TopK[index].Recall.Value
				delta.NDCGDelta = *candidateK.NDCG.Value - *baseline.TopK[index].NDCG.Value
				delta.HitRateDelta = *candidateK.HitRate.Value - *baseline.TopK[index].HitRate.Value
				delta.MRRDelta = *candidate.MRR.Value - *baseline.MRR.Value
			}
			deltas = append(deltas, delta)
		}
	}
	return deltas
}

func metricsAvailable(metrics ...domainofflineevaluation.Metric) bool {
	for _, metric := range metrics {
		if metric.Availability != domainofflineevaluation.AvailabilityAvailable || metric.Value == nil {
			return false
		}
	}
	return true
}
