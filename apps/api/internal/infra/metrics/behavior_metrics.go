package inframetrics

import (
	"strings"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	BehaviorPublicationTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "frux", Name: "behavior_publication_total",
			Help: "Kafka behavior-stream publication outcomes.",
		},
		[]string{"stream", "role", "transport", "result"},
	)
	BehaviorActionFallbackTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "frux", Name: "behavior_action_fallback_total",
			Help: "Synchronous PostgreSQL action fallback outcomes.",
		},
		[]string{"result"},
	)
	BehaviorActionRollbackTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "frux", Name: "behavior_action_rollback_total",
			Help: "Conditional Redis action rollback outcomes.",
		},
		[]string{"result"},
	)
	BehaviorConsumptionTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "frux", Name: "behavior_consumption_total",
			Help: "Durable behavior consumption outcomes including duplicates and supersession.",
		},
		[]string{"stream", "result"},
	)
)

func init() {
	prometheus.MustRegister(
		BehaviorPublicationTotal,
		BehaviorActionFallbackTotal,
		BehaviorActionRollbackTotal,
		BehaviorConsumptionTotal,
	)
}

type BehaviorObserver struct{}

func (BehaviorObserver) ObserveBehaviorPublication(stream, role, transport, result string) {
	BehaviorPublicationTotal.WithLabelValues(
		behaviorStreamLabel(stream),
		boundedBehaviorLabel(role, "primary"),
		boundedBehaviorLabel(transport, "kafka"),
		boundedBehaviorLabel(result, "success", "failure", "uncertain"),
	).Inc()
}

func (BehaviorObserver) ObserveActionFallback(result string) {
	BehaviorActionFallbackTotal.WithLabelValues(
		boundedBehaviorLabel(result, "success", "failure", "invalid"),
	).Inc()
}

func (BehaviorObserver) ObserveActionRollback(result string) {
	BehaviorActionRollbackTotal.WithLabelValues(
		boundedBehaviorLabel(result, "success", "failure", "superseded", "not_applicable"),
	).Inc()
}

func (BehaviorObserver) ObserveActionConsumption(result string) {
	BehaviorConsumptionTotal.WithLabelValues(
		"action",
		boundedBehaviorLabel(result, "applied", "duplicate", "superseded", "terminal", "retryable"),
	).Inc()
}

func (BehaviorObserver) ObserveViewConsumption(result string) {
	BehaviorConsumptionTotal.WithLabelValues(
		"view",
		boundedBehaviorLabel(result, "applied", "duplicate", "terminal", "retryable"),
	).Inc()
}

func behaviorStreamLabel(value string) string {
	return boundedBehaviorLabel(value, "action", "view")
}

func boundedBehaviorLabel(value string, allowed ...string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	for _, candidate := range allowed {
		if value == candidate {
			return value
		}
	}
	return "unknown"
}
