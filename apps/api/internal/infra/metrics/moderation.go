package inframetrics

import (
	"strings"

	"github.com/prometheus/client_golang/prometheus"
)

var ModerationOperationsTotal = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Namespace: "frux", Name: "moderation_operations_total",
		Help: "Production moderation operations by bounded operation and result.",
	},
	[]string{"operation", "result"},
)

func init() {
	prometheus.MustRegister(ModerationOperationsTotal)
}

type ModerationObserver struct{}

func (ModerationObserver) ObserveModeration(operation, result string) {
	ModerationOperationsTotal.WithLabelValues(
		boundedModerationOperation(operation),
		boundedModerationResult(result),
	).Inc()
}

func boundedModerationOperation(value string) string {
	switch strings.TrimSpace(value) {
	case "loop", "claim", "extraction", "provider_call", "result_submission",
		"retry", "fallback", "cancellation", "reconciliation":
		return strings.TrimSpace(value)
	default:
		return "unknown"
	}
}

func boundedModerationResult(value string) string {
	switch strings.TrimSpace(value) {
	case "success", "retry", "terminal", "human", "stale", "created",
		"cancelled", "recovered", "noop":
		return strings.TrimSpace(value)
	default:
		return "unknown"
	}
}
