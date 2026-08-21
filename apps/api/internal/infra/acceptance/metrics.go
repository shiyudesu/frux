package infraacceptance

import (
	"fmt"
	"math"
	"sort"
	"strings"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	prometheusmodel "github.com/prometheus/common/model"
	applicationacceptance "github.com/shiyudesu/frux/internal/application/acceptance"
)

type MetricSnapshot map[string]float64

var acceptanceMetricNames = map[string]struct{}{
	"frux_tongyi_provider_operations_total":     {},
	"frux_tongyi_provider_tokens_total":         {},
	"frux_multimodal_provider_transport_total":  {},
	"frux_multimodal_projection_total":          {},
	"frux_multimodal_similar_requests_total":    {},
	"frux_multimodal_hybrid_requests_total":     {},
	"frux_multimodal_exact_query_results_total": {},
}

func ParseMetricSnapshot(content []byte) (MetricSnapshot, error) {
	parser := expfmt.NewTextParser(prometheusmodel.LegacyValidation)
	families, err := parser.TextToMetricFamilies(bytesReader(content))
	if err != nil {
		return nil, ErrInvalidAcceptanceConfig
	}
	snapshot := MetricSnapshot{}
	for name := range acceptanceMetricNames {
		family := families[name]
		if family == nil {
			continue
		}
		for _, metric := range family.Metric {
			value, ok := metricValue(family.GetType(), metric)
			if !ok || math.IsNaN(value) || math.IsInf(value, 0) {
				return nil, ErrInvalidAcceptanceConfig
			}
			snapshot[metricKey(name, metric.Label)] = value
		}
	}
	return snapshot, nil
}

func MetricDeltas(before, after MetricSnapshot) []applicationacceptance.MetricDelta {
	keys := make([]string, 0, len(after))
	for key := range after {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	deltas := make([]applicationacceptance.MetricDelta, 0, len(keys))
	for _, key := range keys {
		afterValue := after[key]
		beforeValue, exists := before[key]
		available := exists && afterValue >= beforeValue
		value := int64(0)
		if available {
			value = int64(math.Round(afterValue - beforeValue))
		}
		name, labels := splitMetricKey(key)
		deltas = append(deltas, applicationacceptance.MetricDelta{
			Operation: labels["operation"], Kind: metricKind(name, labels),
			Value: value, Available: available,
		})
	}
	return deltas
}

func metricValue(metricType dto.MetricType, metric *dto.Metric) (float64, bool) {
	switch metricType {
	case dto.MetricType_COUNTER:
		return metric.GetCounter().GetValue(), metric.Counter != nil
	case dto.MetricType_GAUGE:
		return metric.GetGauge().GetValue(), metric.Gauge != nil
	default:
		return 0, false
	}
}

func metricKey(name string, labels []*dto.LabelPair) string {
	parts := make([]string, 0, len(labels))
	for _, label := range labels {
		parts = append(parts, label.GetName()+"="+label.GetValue())
	}
	sort.Strings(parts)
	return name + "{" + strings.Join(parts, ",") + "}"
}

func splitMetricKey(key string) (string, map[string]string) {
	name, raw, _ := strings.Cut(key, "{")
	raw = strings.TrimSuffix(raw, "}")
	labels := map[string]string{}
	for _, part := range strings.Split(raw, ",") {
		label, value, ok := strings.Cut(part, "=")
		if ok {
			labels[label] = value
		}
	}
	return name, labels
}

func metricKind(name string, labels map[string]string) string {
	if tokenType := labels["token_type"]; tokenType != "" {
		return "token_" + tokenType
	}
	if result := labels["result"]; result != "" {
		return fmt.Sprintf("%s_%s", strings.TrimPrefix(name, "frux_"), result)
	}
	return strings.TrimPrefix(name, "frux_")
}

func bytesReader(content []byte) *strings.Reader {
	return strings.NewReader(string(content))
}
