package infraofflineevaluation

import (
	"math"
	"strconv"
	"strings"

	domainofflineevaluation "github.com/shiyudesu/frux/internal/domain/offlineevaluation"
)

type PerformanceMetric struct {
	Name           string  `json:"name"`
	Unit           string  `json:"unit"`
	Value          float64 `json:"value"`
	SampleCount    int64   `json:"sample_count"`
	MachineProfile string  `json:"machine_profile"`
}

func LoadPerformanceEvidence(loaded *LoadedManifest) ([]PerformanceMetric, error) {
	if loaded == nil {
		return nil, &InputError{Code: FailureManifest}
	}
	path := loaded.Files[domainofflineevaluation.RoleThroughput]
	if path == "" {
		return nil, nil
	}
	metrics := make([]PerformanceMetric, 0, 3)
	err := readCSV(path, domainofflineevaluation.RoleThroughput,
		[]string{"metric", "unit", "value", "sample_count", "machine_profile"},
		func(record []string, _ int64) error {
			metric := strings.ToLower(strings.TrimSpace(record[0]))
			unit := strings.ToLower(strings.TrimSpace(record[1]))
			value, valueErr := strconv.ParseFloat(strings.TrimSpace(record[2]), 64)
			samples, sampleErr := strconv.ParseInt(strings.TrimSpace(record[3]), 10, 64)
			machine := normalizedDatasetToken(record[4], 128)
			if valueErr != nil || sampleErr != nil || math.IsNaN(value) || math.IsInf(value, 0) || value <= 0 ||
				samples < 1 || machine == "" || !validPerformanceMetric(metric, unit) {
				return &InputError{Code: FailureValue, Role: domainofflineevaluation.RoleThroughput}
			}
			for _, existing := range metrics {
				if existing.Name == metric {
					return &InputError{Code: FailureDuplicate, Role: domainofflineevaluation.RoleThroughput}
				}
			}
			metrics = append(metrics, PerformanceMetric{Name: metric, Unit: unit, Value: value, SampleCount: samples, MachineProfile: machine})
			return nil
		})
	return metrics, err
}

func validPerformanceMetric(metric, unit string) bool {
	switch metric {
	case "exact_latency_p50", "exact_latency_p95":
		return unit == "ns"
	case "embedding_throughput":
		return unit == "items_per_second"
	default:
		return false
	}
}
