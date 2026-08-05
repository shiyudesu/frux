package inframetrics

import "github.com/prometheus/client_golang/prometheus"

var AdminAuditWritesTotal = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Namespace: "frux",
		Name:      "admin_audit_writes_total",
		Help:      "Admin audit write attempts by bounded outcome and result.",
	},
	[]string{"outcome", "result"},
)

func init() {
	prometheus.MustRegister(AdminAuditWritesTotal)
}

func ObserveAdminAuditWrite(outcome, result string) {
	AdminAuditWritesTotal.WithLabelValues(adminAuditOutcomeLabel(outcome), adminAuditResultLabel(result)).Inc()
}

func adminAuditOutcomeLabel(outcome string) string {
	switch outcome {
	case "success", "denied":
		return outcome
	default:
		return "unknown"
	}
}

func adminAuditResultLabel(result string) string {
	switch result {
	case "committed", "dropped", "failed":
		return result
	default:
		return "unknown"
	}
}
