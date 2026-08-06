package inframetrics

import (
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	HumanReviewQueueAvailable = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "frux", Name: "human_review_queue_available",
		Help: "Available pending human review cases.",
	})
	HumanReviewQueueOldestAgeSeconds = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "frux", Name: "human_review_queue_oldest_age_seconds",
		Help: "Age in seconds of the oldest available human review case.",
	})
	HumanReviewOperationsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "frux", Name: "human_review_operations_total",
			Help: "Human review operations by bounded operation and result.",
		},
		[]string{"operation", "result"},
	)
	HumanReviewNotificationsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "frux", Name: "human_review_notifications_total",
			Help: "Human review notification delivery outcomes.",
		},
		[]string{"result"},
	)
)

func init() {
	prometheus.MustRegister(
		HumanReviewQueueAvailable,
		HumanReviewQueueOldestAgeSeconds,
		HumanReviewOperationsTotal,
		HumanReviewNotificationsTotal,
	)
}

func ObserveHumanReview(operation, result string) {
	operation = boundedHumanReviewOperation(operation)
	result = boundedHumanReviewResult(result)
	HumanReviewOperationsTotal.WithLabelValues(operation, result).Inc()
}

func ObserveHumanReviewQueue(available int, oldestAge time.Duration) {
	if available < 0 {
		available = 0
	}
	if oldestAge < 0 {
		oldestAge = 0
	}
	HumanReviewQueueAvailable.Set(float64(available))
	HumanReviewQueueOldestAgeSeconds.Set(oldestAge.Seconds())
}

func ObserveHumanReviewNotification(result string) {
	switch strings.TrimSpace(result) {
	case "delivered", "retry", "terminal":
	default:
		result = "unknown"
	}
	HumanReviewNotificationsTotal.WithLabelValues(result).Inc()
}

func boundedHumanReviewOperation(value string) string {
	switch strings.TrimSpace(value) {
	case "queue", "detail", "claim", "renew", "release", "lease_expiry", "decision":
		return strings.TrimSpace(value)
	default:
		return "unknown"
	}
}

func boundedHumanReviewResult(value string) string {
	switch strings.TrimSpace(value) {
	case "success", "approve", "reject", "invalid", "conflict", "retry", "recovered":
		return strings.TrimSpace(value)
	default:
		return "unknown"
	}
}
