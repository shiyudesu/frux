package inframetrics

import (
	"strings"
	"time"

	domaingovernance "github.com/shiyudesu/frux/internal/domain/governance"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	GovernanceActiveRevision = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "frux",
			Name:      "governance_active_revision",
			Help:      "Locally applied degradation-control revision by process and registered key.",
		},
		[]string{"process", "key"},
	)
	GovernancePollTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "frux",
			Name:      "governance_poll_total",
			Help:      "Degradation-control snapshot polls by process and bounded result.",
		},
		[]string{"process", "result"},
	)
	GovernanceSnapshotAgeSeconds = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "frux",
			Name:      "governance_snapshot_age_seconds",
			Help:      "Age of the last valid local degradation-control snapshot.",
		},
		[]string{"process", "key"},
	)
	GovernanceInvalidControlsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "frux",
			Name:      "governance_invalid_controls_total",
			Help:      "Rejected degradation-control snapshot data by process and bounded reason.",
		},
		[]string{"process", "reason"},
	)
	GovernanceEvaluationFallbackTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "frux",
			Name:      "governance_evaluation_fallback_total",
			Help:      "Local degradation-control evaluations using a registered fallback.",
		},
		[]string{"process", "key", "reason"},
	)
)

func init() {
	prometheus.MustRegister(
		GovernanceActiveRevision,
		GovernancePollTotal,
		GovernanceSnapshotAgeSeconds,
		GovernanceInvalidControlsTotal,
		GovernanceEvaluationFallbackTotal,
	)
}

type GovernanceObserver struct{}

func (GovernanceObserver) ObservePoll(process domaingovernance.Process, result string) {
	GovernancePollTotal.WithLabelValues(processLabel(process), pollResultLabel(result)).Inc()
}

func (GovernanceObserver) ObserveApplied(
	process domaingovernance.Process,
	key domaingovernance.Key,
	revision int64,
) {
	if revision < 0 {
		revision = 0
	}
	GovernanceActiveRevision.WithLabelValues(
		processLabel(process), controlKeyLabel(key),
	).Set(float64(revision))
}

func (GovernanceObserver) ObserveSnapshotAge(
	process domaingovernance.Process,
	key domaingovernance.Key,
	age time.Duration,
) {
	if age < 0 {
		age = 0
	}
	GovernanceSnapshotAgeSeconds.WithLabelValues(
		processLabel(process), controlKeyLabel(key),
	).Set(age.Seconds())
}

func (GovernanceObserver) ObserveInvalid(process domaingovernance.Process, reason string) {
	GovernanceInvalidControlsTotal.WithLabelValues(
		processLabel(process), invalidReasonLabel(reason),
	).Inc()
}

func (GovernanceObserver) ObserveFallback(
	process domaingovernance.Process,
	key domaingovernance.Key,
	reason string,
) {
	GovernanceEvaluationFallbackTotal.WithLabelValues(
		processLabel(process), controlKeyLabel(key), fallbackReasonLabel(reason),
	).Inc()
}

func processLabel(process domaingovernance.Process) string {
	switch process {
	case domaingovernance.ProcessAPI, domaingovernance.ProcessWorker:
		return string(process)
	default:
		return "unknown"
	}
}

func controlKeyLabel(key domaingovernance.Key) string {
	switch key {
	case domaingovernance.FeedPreloadEnabled:
		return string(key)
	default:
		return "unknown"
	}
}

func pollResultLabel(result string) string {
	switch strings.TrimSpace(strings.ToLower(result)) {
	case "success", "failure", "invalid":
		return strings.TrimSpace(strings.ToLower(result))
	default:
		return "unknown"
	}
}

func invalidReasonLabel(reason string) string {
	switch strings.TrimSpace(strings.ToLower(reason)) {
	case "nil_revision", "unknown_key", "invalid_value", "duplicate_key", "unsupported_process", "invalid_source":
		return strings.TrimSpace(strings.ToLower(reason))
	default:
		return "unknown"
	}
}

func fallbackReasonLabel(reason string) string {
	switch strings.TrimSpace(strings.ToLower(reason)) {
	case "not_loaded", "missing", "expired", "stale", "invalid", "unsupported_process":
		return strings.TrimSpace(strings.ToLower(reason))
	default:
		return "unknown"
	}
}
