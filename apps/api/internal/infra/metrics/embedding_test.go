package inframetrics

import (
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestHashEmbeddingMetricsFoldUnknownOutcome(t *testing.T) {
	counter := VideoEmbeddingVectorsTotal.WithLabelValues("hash", "event", "failed")
	before := testutil.ToFloat64(counter)
	ObserveHashVector("video:42")
	if testutil.ToFloat64(counter)-before != 1 {
		t.Fatal("unknown hash outcome was not folded")
	}
}

func TestMultimodalMetricsUseOnlyClosedLabels(t *testing.T) {
	provider := MultimodalProviderCallsTotal.WithLabelValues("video", "success")
	unknownProvider := MultimodalProviderCallsTotal.WithLabelValues("unknown", "unknown")
	transport := MultimodalProviderTransportTotal.WithLabelValues("readiness", "success")
	unknownTransport := MultimodalProviderTransportTotal.WithLabelValues("unknown", "unknown")
	cache := MultimodalQueryCacheTotal.WithLabelValues("hit")
	unknownCache := MultimodalQueryCacheTotal.WithLabelValues("unknown")
	hybrid := MultimodalHybridRequestsTotal.WithLabelValues("hybrid", "fallback")
	unknownHybrid := MultimodalHybridRequestsTotal.WithLabelValues("unknown", "unknown")
	before := []float64{
		testutil.ToFloat64(provider), testutil.ToFloat64(unknownProvider),
		testutil.ToFloat64(transport), testutil.ToFloat64(unknownTransport),
		testutil.ToFloat64(cache), testutil.ToFloat64(unknownCache),
		testutil.ToFloat64(hybrid), testutil.ToFloat64(unknownHybrid),
	}
	ObserveMultimodalProvider("video", "success", time.Millisecond)
	ObserveMultimodalProvider("video:42", "raw provider error", time.Millisecond)
	ObserveMultimodalProviderTransport("readiness", "success", time.Millisecond)
	ObserveMultimodalProviderTransport("https://secret.example.com", "raw provider error", time.Millisecond)
	ObserveMultimodalQueryCache("hit")
	ObserveMultimodalQueryCache("query text")
	ObserveMultimodalHybrid("hybrid", "fallback")
	ObserveMultimodalHybrid("request-id", "map[payload:true]")
	if testutil.ToFloat64(provider)-before[0] != 1 || testutil.ToFloat64(unknownProvider)-before[1] != 1 ||
		testutil.ToFloat64(transport)-before[2] != 1 || testutil.ToFloat64(unknownTransport)-before[3] != 1 ||
		testutil.ToFloat64(cache)-before[4] != 1 || testutil.ToFloat64(unknownCache)-before[5] != 1 ||
		testutil.ToFloat64(hybrid)-before[6] != 1 || testutil.ToFloat64(unknownHybrid)-before[7] != 1 {
		t.Fatal("multimodal labels were not folded to closed values")
	}
	for _, descriptor := range []string{
		provider.Desc().String(), transport.Desc().String(), MultimodalProjectionTotal.WithLabelValues("upserted").Desc().String(),
		MultimodalExactQueryResultsTotal.WithLabelValues("success").Desc().String(),
		MultimodalSimilarRequestsTotal.WithLabelValues("success").Desc().String(),
	} {
		labels := descriptor[strings.LastIndex(descriptor, "variableLabels:"):]
		for _, forbidden := range []string{"video_id", "user_id", "request_id", "session_id", "query", "vector", "raw_error", "model"} {
			if strings.Contains(labels, forbidden) {
				t.Fatalf("descriptor contains forbidden label %q: %s", forbidden, descriptor)
			}
		}
	}
}

func TestHashEmbeddingMetricExcludesHighCardinalityLabels(t *testing.T) {
	description := VideoEmbeddingVectorsTotal.WithLabelValues(
		"hash", "event", "generated",
	).Desc().String()
	for _, forbidden := range []string{
		"video_id", "text", "url", "error", "attempt", "token", "model_key",
	} {
		if strings.Contains(description, forbidden) {
			t.Fatalf("descriptor contains forbidden label %q: %s", forbidden, description)
		}
	}
}
