package inframetrics

import (
	"time"

	domainmedia "github.com/shiyudesu/frux/internal/domain/media"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	MediaProgressUpdatesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "frux", Name: "media_progress_updates_total",
			Help: "Fenced durable media progress update outcomes.",
		},
		[]string{"step", "result"},
	)
	MediaAdminProcessingBacklog = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "frux", Name: "media_admin_processing_backlog",
			Help: "Media processing counts exposed to operations.",
		},
		[]string{"state"},
	)
	MediaAdminOldestWaitingSeconds = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "frux", Name: "media_admin_oldest_waiting_seconds",
			Help: "Age of the oldest waiting media task.",
		},
	)
	MediaAdminRetryTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "frux", Name: "media_admin_retry_total",
			Help: "Administrative media retry outcomes.",
		},
		[]string{"result"},
	)
	MediaRetryProjectionTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "frux", Name: "media_retry_projection_total",
			Help: "Media retry projection repair outcomes.",
		},
		[]string{"result"},
	)
	MediaRetryOutboxBacklog = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "frux", Name: "media_retry_outbox_backlog",
			Help: "Pending media retry projection notifications.",
		},
	)
)

func init() {
	prometheus.MustRegister(
		MediaProgressUpdatesTotal,
		MediaAdminProcessingBacklog,
		MediaAdminOldestWaitingSeconds,
		MediaAdminRetryTotal,
		MediaRetryProjectionTotal,
		MediaRetryOutboxBacklog,
	)
}

func ObserveMediaProgress(step string, err error) {
	if !domainmedia.ValidProcessingStep(step) {
		step = "unknown"
	}
	result := "success"
	if err != nil {
		result = "failure"
	}
	MediaProgressUpdatesTotal.WithLabelValues(step, result).Inc()
}

func ObserveMediaAdminOverview(
	summary *domainmedia.AdminProcessingSummary,
	err error,
) {
	if err != nil || summary == nil {
		return
	}
	MediaAdminProcessingBacklog.WithLabelValues("waiting").Set(float64(summary.Waiting))
	MediaAdminProcessingBacklog.WithLabelValues("processing").Set(float64(summary.Processing))
	MediaAdminProcessingBacklog.WithLabelValues("failed").Set(float64(summary.Failed))
	MediaAdminProcessingBacklog.WithLabelValues("completed").Set(float64(summary.Completed))
	age := 0.0
	if summary.OldestWaitingAt != nil {
		age = time.Since(summary.OldestWaitingAt.UTC()).Seconds()
		if age < 0 {
			age = 0
		}
	}
	MediaAdminOldestWaitingSeconds.Set(age)
}

func ObserveMediaAdminRetry(result string) {
	switch result {
	case "retried", "replayed", "conflict", "rejected", "failure":
	default:
		result = "failure"
	}
	MediaAdminRetryTotal.WithLabelValues(result).Inc()
}

func ObserveMediaRetryProjection(result string) {
	switch result {
	case "delivered", "retry", "terminal":
	default:
		result = "retry"
	}
	MediaRetryProjectionTotal.WithLabelValues(result).Inc()
}
