package inframetrics

import "github.com/prometheus/client_golang/prometheus"

var MediaObjectOutboundBytesTotal = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Namespace: "frux",
		Name:      "media_object_outbound_bytes_total",
		Help:      "Observed or estimated object-storage outbound bytes by bounded source.",
	},
	[]string{"source"},
)

var mediaOutboundSources = []string{
	"processing_source",
	"cover_compatibility",
	"promotion",
	"protection",
	"legacy_repair",
	"moderation_source",
	"protected_preview_estimate",
	"public_full_estimate",
	"public_range_estimate",
	"public_manifest",
}

func init() {
	prometheus.MustRegister(MediaObjectOutboundBytesTotal)
	for _, source := range mediaOutboundSources {
		MediaObjectOutboundBytesTotal.WithLabelValues(source)
	}
}

func ObserveMediaObjectOutboundBytes(source string, bytes int64) {
	if bytes <= 0 {
		return
	}
	MediaObjectOutboundBytesTotal.WithLabelValues(mediaOutboundSourceLabel(source)).Add(float64(bytes))
}

func mediaOutboundSourceLabel(value string) string {
	switch value {
	case "processing_source", "cover_compatibility", "promotion", "protection",
		"legacy_repair", "public_full_estimate", "public_range_estimate",
		"public_manifest", "protected_preview_estimate", "moderation_source":
		return value
	}
	return "unknown"
}
