package inframetrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestAccountNotificationMetricUsesBoundedLabels(t *testing.T) {
	before := testutil.ToFloat64(
		AccountLifecycleNotificationsTotal.WithLabelValues("unknown"),
	)
	AccountNotificationObserver{}.ObserveAccountNotification("unexpected")
	if got := testutil.ToFloat64(
		AccountLifecycleNotificationsTotal.WithLabelValues("unknown"),
	) - before; got != 1 {
		t.Fatalf("unknown metric delta = %v", got)
	}
}
