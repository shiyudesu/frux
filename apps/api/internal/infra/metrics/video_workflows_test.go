package inframetrics

import (
	"errors"
	"testing"
	"time"

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

func TestPublicationStatsFailurePreservesGaugesAndReportsError(t *testing.T) {
	observer := VideoWorkflowObserver{}
	oldest := time.Now().UTC().Add(-2 * time.Minute)
	observer.ObservePublicationOutbox(9, &oldest, nil)
	pendingBefore := testutil.ToFloat64(VideoPublicationOutboxPending)
	lagBefore := testutil.ToFloat64(VideoPublicationOutboxLagSeconds)
	failuresBefore := testutil.ToFloat64(
		VideoPublicationStatsTotal.WithLabelValues("failure"),
	)

	observer.ObservePublicationOutbox(0, nil, errors.New("stats unavailable"))
	observer.ObservePublicationStats("failure")

	if got := testutil.ToFloat64(VideoPublicationOutboxPending); got != pendingBefore {
		t.Fatalf("pending gauge changed from %v to %v", pendingBefore, got)
	}
	if got := testutil.ToFloat64(VideoPublicationOutboxLagSeconds); got != lagBefore {
		t.Fatalf("lag gauge changed from %v to %v", lagBefore, got)
	}
	if got := testutil.ToFloat64(
		VideoPublicationStatsTotal.WithLabelValues("failure"),
	) - failuresBefore; got != 1 {
		t.Fatalf("stats failure delta = %v", got)
	}
}
