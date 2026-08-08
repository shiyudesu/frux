package inframetrics

import (
	"strings"
	"testing"
	"time"

	infrakafka "github.com/shiyudesu/frux/internal/infra/kafka"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestKafkaMetricsUseOnlyRegisteredBoundedLabels(t *testing.T) {
	observer := KafkaObserver{}
	produce := KafkaProduceTotal.WithLabelValues("unknown", "unknown", "unknown")
	consume := KafkaConsumedTotal.WithLabelValues("unknown", "unknown", "unknown")
	topology := KafkaTopologyValidationTotal.WithLabelValues("unknown", "unknown")
	before := []float64{
		testutil.ToFloat64(produce),
		testutil.ToFloat64(consume),
		testutil.ToFloat64(topology),
	}
	observer.ObserveProduce("user-42", "producer-42", "raw broker error", time.Millisecond)
	observer.ObserveConsume("video-42", "group-42", "offset-42", time.Millisecond, time.Second)
	observer.ObserveTopology("topic-42", "arbitrary")
	if testutil.ToFloat64(produce)-before[0] != 1 ||
		testutil.ToFloat64(consume)-before[1] != 1 ||
		testutil.ToFloat64(topology)-before[2] != 1 {
		t.Fatal("unknown Kafka labels were not folded")
	}
}

func TestKafkaMetricDescriptorsExcludeUnboundedDimensions(t *testing.T) {
	description := KafkaConsumedTotal.WithLabelValues(
		string(infrakafka.TopicBackboneProbe),
		string(infrakafka.GroupBackboneProbeActive),
		"durable_success",
	).Desc().String()
	for _, forbidden := range []string{"event_id", "user_id", "video_id", "partition", "offset", "payload", "error"} {
		if strings.Contains(description, forbidden) {
			t.Fatalf("descriptor contains forbidden label %q: %s", forbidden, description)
		}
	}
}
