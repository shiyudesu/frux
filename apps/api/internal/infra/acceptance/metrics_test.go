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
