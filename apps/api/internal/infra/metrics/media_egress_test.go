package inframetrics

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestMediaObjectOutboundBytesUsesBoundedSourceLabel(t *testing.T) {
	ObserveMediaObjectOutboundBytes("processing_source", 12)
	ObserveMediaObjectOutboundBytes("video:42/object/key", 8)

	if value := testutil.ToFloat64(
		MediaObjectOutboundBytesTotal.WithLabelValues("processing_source"),
	); value < 12 {
		t.Fatalf("processing source bytes = %v", value)
	}
	if value := testutil.ToFloat64(
		MediaObjectOutboundBytesTotal.WithLabelValues("unknown"),
	); value < 8 {
		t.Fatalf("unknown source bytes = %v", value)
	}

	description := MediaObjectOutboundBytesTotal.WithLabelValues("unknown").Desc().String()
	for _, forbidden := range []string{"user", "video", "asset", "url", "object_key"} {
		if strings.Contains(description, forbidden+"=") {
			t.Fatalf("metric description contains forbidden label %q: %s", forbidden, description)
		}
	}
}
