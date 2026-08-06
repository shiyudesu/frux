package inframetrics

import (
	"strings"

	"github.com/prometheus/client_golang/prometheus"
)

var RateLimitDecisionsTotal = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Namespace: "frux",
		Name:      "rate_limit_decisions_total",
		Help:      "Rate-limit decisions by registered endpoint group, enforcement layer, and bounded result.",
	},
	[]string{"endpoint_group", "layer", "result"},
)

func init() {
	prometheus.MustRegister(RateLimitDecisionsTotal)
}

type RateLimitObserver struct{}

func (RateLimitObserver) Observe(endpointGroup, layer, result string) {
	RateLimitDecisionsTotal.WithLabelValues(
		rateLimitGroupLabel(endpointGroup),
		rateLimitLayerLabel(layer),
		rateLimitResultLabel(result),
	).Inc()
}

func rateLimitGroupLabel(value string) string {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "playback_telemetry", "public_search", "upload_session":
		return strings.TrimSpace(strings.ToLower(value))
	default:
		return "unknown"
	}
}

func rateLimitLayerLabel(value string) string {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "local", "distributed", "fallback":
		return strings.TrimSpace(strings.ToLower(value))
	default:
		return "unknown"
	}
}

func rateLimitResultLabel(value string) string {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "allow", "reject", "fallback", "saturation", "backend_error":
		return strings.TrimSpace(strings.ToLower(value))
	default:
		return "unknown"
	}
}
