package inframetrics

import (
	"strings"
	"testing"

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
