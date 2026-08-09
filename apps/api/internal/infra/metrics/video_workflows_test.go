package inframetrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestPublicationCleanupMetricsRemainBounded(t *testing.T) {
	successBefore := testutil.ToFloat64(
		VideoPublicationCleanupTotal.WithLabelValues("success"),
	)
	failureBefore := testutil.ToFloat64(
		VideoPublicationCleanupTotal.WithLabelValues("failure"),
	)
	deletedBefore := testutil.ToFloat64(VideoPublicationCleanupDeletedTotal)

	observer := VideoWorkflowObserver{}
	observer.ObservePublicationCleanup("success", 7)
	observer.ObservePublicationCleanup("unexpected", 0)

	if got := testutil.ToFloat64(
		VideoPublicationCleanupTotal.WithLabelValues("success"),
	) - successBefore; got != 1 {
		t.Fatalf("success cleanup delta = %v", got)
	}
	if got := testutil.ToFloat64(
		VideoPublicationCleanupTotal.WithLabelValues("failure"),
	) - failureBefore; got != 1 {
		t.Fatalf("failure cleanup delta = %v", got)
	}
	if got := testutil.ToFloat64(
		VideoPublicationCleanupDeletedTotal,
	) - deletedBefore; got != 7 {
		t.Fatalf("deleted cleanup delta = %v", got)
	}
}
