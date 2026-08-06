package inframetrics

import "github.com/prometheus/client_golang/prometheus"

var (
	MQRetriesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "frux", Name: "mq_retries_total",
			Help: "RabbitMQ retryable deliveries by bounded consumer name.",
		},
		[]string{"consumer"},
	)
	MQRetryExhaustedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "frux", Name: "mq_retry_exhausted_total",
			Help: "RabbitMQ deliveries expected to exhaust the configured delivery limit.",
		},
		[]string{"consumer"},
	)
	MQTerminalTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "frux", Name: "mq_terminal_total",
			Help: "RabbitMQ terminal deliveries rejected without requeue.",
		},
		[]string{"consumer"},
	)
	MQDeadLetterDepth = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "frux", Name: "mq_dead_letter_depth",
			Help: "Dead-letter queue depth observed through the RabbitMQ management API.",
		},
		[]string{"consumer"},
	)
	MQRoutingFailuresTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "frux", Name: "mq_routing_failures_total",
			Help: "RabbitMQ dead-letter or replay routing failures.",
		},
		[]string{"consumer"},
	)
	MQReplayTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "frux", Name: "mq_replay_total",
			Help: "Dead-letter replay outcomes.",
		},
		[]string{"result"},
	)
)

func init() {
	prometheus.MustRegister(
		MQRetriesTotal,
		MQRetryExhaustedTotal,
		MQTerminalTotal,
		MQDeadLetterDepth,
		MQRoutingFailuresTotal,
		MQReplayTotal,
	)
}

func ObserveMQRetry(consumer string) {
	MQRetriesTotal.WithLabelValues(mqConsumerLabel(consumer)).Inc()
}

func ObserveMQExhaustion(consumer string) {
	MQRetryExhaustedTotal.WithLabelValues(mqConsumerLabel(consumer)).Inc()
}

func ObserveMQTerminal(consumer string) {
	MQTerminalTotal.WithLabelValues(mqConsumerLabel(consumer)).Inc()
}

func ObserveMQDeadLetterDepth(consumer string, depth int64) {
	if depth < 0 {
		depth = 0
	}
	MQDeadLetterDepth.WithLabelValues(mqConsumerLabel(consumer)).Set(float64(depth))
}

func ObserveMQRoutingFailure(consumer string) {
	MQRoutingFailuresTotal.WithLabelValues(mqConsumerLabel(consumer)).Inc()
}

func ObserveMQReplay(result string) {
	MQReplayTotal.WithLabelValues(mqReplayResultLabel(result)).Inc()
}

type DeadLetterObserver struct{}

func (DeadLetterObserver) ObserveReplay(result string) {
	ObserveMQReplay(result)
}

func mqConsumerLabel(value string) string {
	switch value {
	case "action_changed", "video_published", "video_embedding",
		"view_event_recorded", "media_processing", "management_api":
		return value
	default:
		return "unknown"
	}
}

func mqReplayResultLabel(value string) string {
	switch value {
	case "success", "claim_failure", "publish_failure", "timeout",
		"audit_failure", "ack_failure":
		return value
	default:
		return "unknown"
	}
}
