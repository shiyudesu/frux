package inframetrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestObserveAdminAuditWriteUsesBoundedLabels(t *testing.T) {
	success := AdminAuditWritesTotal.WithLabelValues("success", "committed")
	denied := AdminAuditWritesTotal.WithLabelValues("denied", "committed")
	failed := AdminAuditWritesTotal.WithLabelValues("success", "failed")
	dropped := AdminAuditWritesTotal.WithLabelValues("denied", "dropped")
	unknown := AdminAuditWritesTotal.WithLabelValues("unknown", "unknown")
	before := []float64{
		testutil.ToFloat64(success),
		testutil.ToFloat64(denied),
		testutil.ToFloat64(failed),
		testutil.ToFloat64(dropped),
		testutil.ToFloat64(unknown),
	}
	ObserveAdminAuditWrite("success", "committed")
	ObserveAdminAuditWrite("denied", "committed")
	ObserveAdminAuditWrite("success", "failed")
	ObserveAdminAuditWrite("denied", "dropped")
	ObserveAdminAuditWrite("custom", "custom")
	after := []float64{
		testutil.ToFloat64(success),
		testutil.ToFloat64(denied),
		testutil.ToFloat64(failed),
		testutil.ToFloat64(dropped),
		testutil.ToFloat64(unknown),
	}
	for index := range before {
		if after[index]-before[index] != 1 {
			t.Fatalf("metric %d delta = %v, want 1", index, after[index]-before[index])
		}
	}
}
