package inframetrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestMQMetricsUseBoundedLabels(t *testing.T) {
	retry := MQRetriesTotal.WithLabelValues("action_changed")
	unknownRetry := MQRetriesTotal.WithLabelValues("unknown")
	replay := MQReplayTotal.WithLabelValues("success")
	unknownReplay := MQReplayTotal.WithLabelValues("unknown")
	before := []float64{
		testutil.ToFloat64(retry),
		testutil.ToFloat64(unknownRetry),
		testutil.ToFloat64(replay),
		testutil.ToFloat64(unknownReplay),
	}
	ObserveMQRetry("action_changed")
	ObserveMQRetry("event-123")
	ObserveMQReplay("success")
	ObserveMQReplay("replay-123")
	after := []float64{
		testutil.ToFloat64(retry),
		testutil.ToFloat64(unknownRetry),
		testutil.ToFloat64(replay),
		testutil.ToFloat64(unknownReplay),
	}
	for index := range before {
		if after[index]-before[index] != 1 {
			t.Fatalf("metric %d delta = %v, want 1", index, after[index]-before[index])
		}
	}
}
