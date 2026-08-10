package inframetrics

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestMediaLifecycleMetricsUseBoundedResultsAndBacklog(t *testing.T) {
	failed := MediaLifecycleTasksTotal.WithLabelValues("failed")
	before := testutil.ToFloat64(failed)
	ObserveMediaLifecycleTask("video:42")
	if testutil.ToFloat64(failed)-before != 1 {
		t.Fatal("unknown lifecycle result was not folded")
	}

	oldest := time.Now().UTC().Add(-time.Minute)
	ObserveMediaLifecycleBacklog(3, &oldest, nil)
	if got := testutil.ToFloat64(MediaLifecycleBacklog); got != 3 {
		t.Fatalf("backlog=%v", got)
	}
	if got := testutil.ToFloat64(MediaLifecycleBacklogOldestSeconds); got < 59 || got > 61 {
		t.Fatalf("oldest age=%v", got)
	}

	ObserveMediaLifecycleBacklog(0, nil, nil)
	if got := testutil.ToFloat64(MediaLifecycleBacklogOldestSeconds); got != 0 {
		t.Fatalf("empty oldest age=%v", got)
	}
}
