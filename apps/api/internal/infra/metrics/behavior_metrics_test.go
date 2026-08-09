package inframetrics

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestBehaviorMetricsFoldUnregisteredLabels(t *testing.T) {
	observer := BehaviorObserver{}
	publication := BehaviorPublicationTotal.WithLabelValues("unknown", "unknown", "unknown", "unknown")
	consumption := BehaviorConsumptionTotal.WithLabelValues("action", "unknown")
	beforePublication := testutil.ToFloat64(publication)
	beforeConsumption := testutil.ToFloat64(consumption)
	observer.ObserveBehaviorPublication("user-7", "event-1", "partition-2", "raw error")
	observer.ObserveActionConsumption("event-1")
	if testutil.ToFloat64(publication)-beforePublication != 1 ||
		testutil.ToFloat64(consumption)-beforeConsumption != 1 {
		t.Fatal("unregistered behavior labels were not folded")
	}
}

func TestBehaviorMetricDescriptorsExcludeBusinessIdentity(t *testing.T) {
	description := BehaviorPublicationTotal.WithLabelValues(
		"action", "primary", "kafka", "success",
	).Desc().String()
	for _, forbidden := range []string{
		"event_id", "user_id", "video_id", "key", "partition", "offset", "payload", "error",
	} {
		if strings.Contains(description, forbidden) {
			t.Fatalf("descriptor contains forbidden label %q: %s", forbidden, description)
		}
	}
}

func TestBehaviorMetricsAcceptCombinedDualOutcome(t *testing.T) {
	metric := BehaviorPublicationTotal.WithLabelValues("view", "combined", "dual", "failure")
	before := testutil.ToFloat64(metric)
	BehaviorObserver{}.ObserveBehaviorPublication("view", "combined", "dual", "failure")
	if testutil.ToFloat64(metric)-before != 1 {
		t.Fatal("combined dual publication outcome was not recorded")
	}
}
