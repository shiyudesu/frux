package inframetrics

import "github.com/prometheus/client_golang/prometheus"

var AccountLifecycleNotificationsTotal = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Namespace: "frux",
		Name:      "account_lifecycle_notifications_total",
		Help:      "Account lifecycle notification delivery outcomes.",
	},
	[]string{"result"},
)

func init() {
	prometheus.MustRegister(AccountLifecycleNotificationsTotal)
}

type AccountNotificationObserver struct{}

func (AccountNotificationObserver) ObserveAccountNotification(result string) {
	switch result {
	case "delivered", "retry", "terminal":
	default:
		result = "unknown"
	}
	AccountLifecycleNotificationsTotal.WithLabelValues(result).Inc()
}
