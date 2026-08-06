package inframetrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestRateLimitObserverBoundsLabels(t *testing.T) {
	before := testutil.ToFloat64(RateLimitDecisionsTotal.WithLabelValues("unknown", "unknown", "unknown"))
	RateLimitObserver{}.Observe("dynamic", "dynamic", "dynamic")
	after := testutil.ToFloat64(RateLimitDecisionsTotal.WithLabelValues("unknown", "unknown", "unknown"))
	if after-before != 1 {
		t.Fatalf("unknown bounded metric delta=%v", after-before)
	}
}
