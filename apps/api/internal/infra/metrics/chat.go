package inframetrics

import (
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

var ChatOperationsTotal = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Namespace: "frux",
		Name:      "chat_operations_total",
		Help:      "Chat operations by closed operation, message kind, outcome, and error class.",
	},
	[]string{"operation", "kind", "outcome", "error_class"},
)

var ChatOperationDuration = prometheus.NewHistogramVec(
	prometheus.HistogramOpts{
		Namespace: "frux",
		Name:      "chat_operation_duration_seconds",
		Help:      "Chat operation latency by closed operation and message kind.",
	},
	[]string{"operation", "kind"},
)

func init() {
	prometheus.MustRegister(ChatOperationsTotal, ChatOperationDuration)
}

type ChatObserver struct{}

func (ChatObserver) Observe(operation, kind, outcome, errorClass string, latency time.Duration) {
	ChatOperationsTotal.WithLabelValues(
		chatOperationLabel(operation),
		chatKindLabel(kind),
		chatOutcomeLabel(outcome),
		chatErrorClassLabel(errorClass),
	).Inc()
	ChatOperationDuration.WithLabelValues(
		chatOperationLabel(operation),
		chatKindLabel(kind),
	).Observe(latency.Seconds())
}

func chatOperationLabel(value string) string {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "eligibility", "create_conversation", "list_recipients", "list_conversations",
		"history", "send", "mark_read", "inbox_unread", "read":
		return strings.TrimSpace(strings.ToLower(value))
	default:
		return "unknown"
	}
}

func chatKindLabel(value string) string {
	switch strings.TrimSpace(strings.ToUpper(value)) {
	case "TEXT", "VIDEO":
		return strings.TrimSpace(strings.ToUpper(value))
	default:
		return "none"
	}
}

func chatOutcomeLabel(value string) string {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "success", "error":
		return strings.TrimSpace(strings.ToLower(value))
	default:
		return "unknown"
	}
}

func chatErrorClassLabel(value string) string {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "", "none":
		return "none"
	case "not_eligible", "account_unavailable", "idempotency_conflict",
		"video_unavailable", "conversation_not_found", "internal":
		return strings.TrimSpace(strings.ToLower(value))
	default:
		return "unknown"
	}
}
