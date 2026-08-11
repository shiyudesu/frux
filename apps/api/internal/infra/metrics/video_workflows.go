package inframetrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	VideoWorkflowPublicationTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "frux",
			Name:      "video_workflow_publication_total",
			Help:      "Video workflow publications by bounded workflow, role, transport, and result.",
		},
		[]string{"workflow", "role", "transport", "result"},
	)
	VideoPublicationOutboxPending = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "frux",
			Name:      "video_publication_outbox_pending",
			Help:      "Pending durable video-publication outbox rows.",
		},
	)
	VideoPublicationOutboxLagSeconds = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "frux",
			Name:      "video_publication_outbox_lag_seconds",
			Help:      "Age of the oldest pending video-publication outbox row.",
		},
	)
	VideoPublicationDispatchTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "frux",
			Name:      "video_publication_dispatch_total",
			Help:      "Durable video-publication outbox outcomes.",
		},
		[]string{"result"},
	)
	VideoPublicationStatsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "frux",
			Name:      "video_publication_outbox_stats_total",
			Help:      "Video-publication outbox statistics query outcomes.",
		},
		[]string{"result"},
	)
	VideoPublicationCleanupTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "frux",
			Name:      "video_publication_outbox_cleanup_total",
			Help:      "Bounded cleanup runs for dispatched video-publication outbox rows.",
		},
		[]string{"result"},
	)
	VideoPublicationCleanupDeletedTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: "frux",
			Name:      "video_publication_outbox_cleanup_deleted_total",
			Help:      "Dispatched video-publication outbox rows removed after the replay window.",
		},
	)
	MediaWakeupsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "frux",
			Name:      "media_processing_wakeups_total",
			Help:      "Media processing wakeup outcomes.",
		},
		[]string{"result"},
	)
)

func init() {
	prometheus.MustRegister(
		VideoWorkflowPublicationTotal,
		VideoPublicationOutboxPending,
		VideoPublicationOutboxLagSeconds,
		VideoPublicationDispatchTotal,
		VideoPublicationStatsTotal,
		VideoPublicationCleanupTotal,
		VideoPublicationCleanupDeletedTotal,
		MediaWakeupsTotal,
	)
}

type VideoWorkflowObserver struct{}

func (VideoWorkflowObserver) ObserveVideoWorkflowPublication(
	workflow, role, transport, result string,
) {
	VideoWorkflowPublicationTotal.WithLabelValues(
		videoWorkflowLabel(workflow),
		publicationRoleLabel(role),
		transportLabel(transport),
		publicationResultLabel(result),
	).Inc()
}

func (VideoWorkflowObserver) ObservePublicationOutbox(
	pending int64,
	oldest *time.Time,
	err error,
) {
	if err != nil {
		return
	}
	VideoPublicationOutboxPending.Set(float64(pending))
	lag := 0.0
	if err == nil && pending > 0 && oldest != nil {
		lag = time.Since(oldest.UTC()).Seconds()
		if lag < 0 {
			lag = 0
		}
	}
	VideoPublicationOutboxLagSeconds.Set(lag)
}

func (VideoWorkflowObserver) ObservePublicationStats(result string) {
	if result != "success" {
		result = "failure"
	}
	VideoPublicationStatsTotal.WithLabelValues(result).Inc()
}

func (VideoWorkflowObserver) ObservePublicationDispatch(result string) {
	VideoPublicationDispatchTotal.WithLabelValues(publicationResultLabel(result)).Inc()
}

func (VideoWorkflowObserver) ObservePublicationCleanup(result string, deleted int64) {
	if result != "success" {
		result = "failure"
	}
	VideoPublicationCleanupTotal.WithLabelValues(result).Inc()
	if deleted > 0 {
		VideoPublicationCleanupDeletedTotal.Add(float64(deleted))
	}
}

func ObserveMediaWakeup(result string) {
	switch result {
	case "signaled", "capacity_full", "missing_job", "stale", "validation_failed",
		"publish_failed", "polling_recovery":
	default:
		result = "unknown"
	}
	MediaWakeupsTotal.WithLabelValues(result).Inc()
}

func videoWorkflowLabel(value string) string {
	switch value {
	case "publication", "media_wakeup":
		return value
	default:
		return "unknown"
	}
}

func publicationRoleLabel(value string) string {
	switch value {
	case "primary":
		return value
	default:
		return "unknown"
	}
}

func transportLabel(value string) string {
	switch value {
	case "kafka":
		return value
	default:
		return "unknown"
	}
}

func publicationResultLabel(value string) string {
	switch value {
	case "success", "failure", "uncertain", "transport", "timeout", "canceled":
		return value
	default:
		return "unknown"
	}
}
