package infraacceptance

import "testing"

func TestParseMetricSnapshotAndDeltas(t *testing.T) {
	before, err := ParseMetricSnapshot([]byte(`# TYPE frux_tongyi_provider_tokens_total counter
frux_tongyi_provider_tokens_total{operation="video",token_type="input"} 10
`))
	if err != nil {
		t.Fatal(err)
	}
	after, err := ParseMetricSnapshot([]byte(`# TYPE frux_tongyi_provider_tokens_total counter
frux_tongyi_provider_tokens_total{operation="video",token_type="input"} 25
`))
	if err != nil {
		t.Fatal(err)
	}
	deltas := MetricDeltas(before, after)
	if len(deltas) != 1 || !deltas[0].Available || deltas[0].Value != 15 || deltas[0].Operation != "video" || deltas[0].Kind != "token_input" {
		t.Fatalf("deltas=%#v", deltas)
	}
}

func TestMetricDeltasMarksCounterResetUnavailable(t *testing.T) {
	before := MetricSnapshot{"metric{operation=query}": 10}
	after := MetricSnapshot{"metric{operation=query}": 2}
	deltas := MetricDeltas(before, after)
	if len(deltas) != 1 || deltas[0].Available || deltas[0].Value != 0 {
		t.Fatalf("deltas=%#v", deltas)
	}
}

func TestMetricDeltasTreatsNewCounterSeriesAsStartingAtZero(t *testing.T) {
	deltas := MetricDeltas(MetricSnapshot{}, MetricSnapshot{
		"frux_tongyi_provider_operations_total{operation=query,result=success}": 1,
	})
	if len(deltas) != 1 || !deltas[0].Available || deltas[0].Value != 1 || deltas[0].Operation != "query" {
		t.Fatalf("deltas=%#v", deltas)
	}
}

func TestMetricDeltaSumMatchesBoundedLabelSubset(t *testing.T) {
	before := MetricSnapshot{
		"frux_recommendation_session_semantic_operations_total{confidence_band=high,result=success,stage=builder}": 2,
	}
	after := MetricSnapshot{
		"frux_recommendation_session_semantic_operations_total{confidence_band=high,result=success,stage=builder}":   3,
		"frux_recommendation_session_semantic_operations_total{confidence_band=medium,result=success,stage=builder}": 1,
		"frux_recommendation_session_semantic_operations_total{confidence_band=none,result=timeout,stage=builder}":   9,
	}
	delta, available := MetricDeltaSum(before, after,
		"frux_recommendation_session_semantic_operations_total",
		map[string]string{"stage": "builder", "result": "success"},
	)
	if !available || delta != 2 {
		t.Fatalf("delta=%d available=%v", delta, available)
	}
}
