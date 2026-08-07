package inframetrics

import "github.com/prometheus/client_golang/prometheus"

var VideoLifecycleNotificationsTotal = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Namespace: "frux",
		Name:      "video_lifecycle_notifications_total",
		Help:      "Video lifecycle notification delivery outcomes.",
	},
	[]string{"result"},
)

func init() {
	prometheus.MustRegister(VideoLifecycleNotificationsTotal)
}

type VideoLifecycleNotificationObserver struct{}

func (VideoLifecycleNotificationObserver) ObserveLifecycleNotification(result string) {
	switch result {
	case "delivered", "retry", "terminal":
	default:
		result = "unknown"
	}
	VideoLifecycleNotificationsTotal.WithLabelValues(result).Inc()
}
