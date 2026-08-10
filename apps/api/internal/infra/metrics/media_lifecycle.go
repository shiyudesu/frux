package inframetrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	MediaLifecycleTasksTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "frux",
			Name:      "media_video_lifecycle_tasks_total",
			Help:      "Durable media lifecycle task outcomes.",
		},
		[]string{"result"},
	)
	MediaLifecycleBacklog = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "frux",
			Name:      "media_video_lifecycle_backlog",
			Help:      "Pending, processing, or retryable media lifecycle tasks.",
		},
	)
	MediaLifecycleBacklogOldestSeconds = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "frux",
			Name:      "media_video_lifecycle_backlog_oldest_seconds",
			Help:      "Age of the oldest active media lifecycle task.",
		},
	)
)

func init() {
	prometheus.MustRegister(
		MediaLifecycleTasksTotal,
		MediaLifecycleBacklog,
		MediaLifecycleBacklogOldestSeconds,
	)
}

func ObserveMediaLifecycleTask(result string) {
	switch result {
	case "completed", "superseded", "retryable", "failed", "lease_lost":
	default:
		result = "failed"
	}
	MediaLifecycleTasksTotal.WithLabelValues(result).Inc()
}

func ObserveMediaLifecycleBacklog(
	count int64,
	oldest *time.Time,
	err error,
) {
	if err != nil {
		return
	}
	MediaLifecycleBacklog.Set(float64(count))
	age := 0.0
	if count > 0 && oldest != nil {
		age = time.Since(oldest.UTC()).Seconds()
		if age < 0 {
			age = 0
		}
	}
	MediaLifecycleBacklogOldestSeconds.Set(age)
}
