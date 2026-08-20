package inframetrics

import (
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	MultimodalEmbeddingJobs = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{Namespace: "frux", Name: "multimodal_embedding_jobs", Help: "Multimodal embedding jobs by bounded state."},
		[]string{"state"},
	)
	MultimodalEmbeddingOldestAgeSeconds = prometheus.NewGauge(
		prometheus.GaugeOpts{Namespace: "frux", Name: "multimodal_embedding_oldest_age_seconds", Help: "Age of the oldest pending or retryable multimodal embedding job."},
	)
	MultimodalProviderCallsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{Namespace: "frux", Name: "multimodal_provider_calls_total", Help: "Multimodal provider calls by bounded operation and result."},
		[]string{"operation", "result"},
	)
	MultimodalProviderDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{Namespace: "frux", Name: "multimodal_provider_duration_seconds", Help: "Multimodal provider duration by bounded operation and result."},
		[]string{"operation", "result"},
	)
	MultimodalProviderAdmissionTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{Namespace: "frux", Name: "multimodal_provider_admission_total", Help: "Multimodal provider admission outcomes by bounded operation."},
		[]string{"operation", "result"},
	)
	MultimodalProviderTransportTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{Namespace: "frux", Name: "multimodal_provider_transport_total", Help: "Multimodal provider HTTP transport calls by bounded operation and result."},
		[]string{"operation", "result"},
	)
	MultimodalProviderTransportDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{Namespace: "frux", Name: "multimodal_provider_transport_duration_seconds", Help: "Multimodal provider HTTP transport duration by bounded operation and result."},
		[]string{"operation", "result"},
	)
	MultimodalCoverageVideos = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{Namespace: "frux", Name: "multimodal_coverage_videos", Help: "Readable videos by bounded multimodal coverage result."},
		[]string{"result"},
	)
	MultimodalProjectionTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{Namespace: "frux", Name: "multimodal_projection_total", Help: "Multimodal projection reconciliation outcomes."},
		[]string{"result"},
	)
	MultimodalExactQueryDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{Namespace: "frux", Name: "multimodal_exact_query_duration_seconds", Help: "Exact multimodal query duration by bounded result."},
		[]string{"result"},
	)
	MultimodalExactQueryResultsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{Namespace: "frux", Name: "multimodal_exact_query_results_total", Help: "Results returned by exact multimodal queries."},
		[]string{"result"},
	)
	MultimodalQueryCacheTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{Namespace: "frux", Name: "multimodal_query_cache_total", Help: "Multimodal query-vector cache outcomes."},
		[]string{"outcome"},
	)
	MultimodalHybridRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{Namespace: "frux", Name: "multimodal_hybrid_requests_total", Help: "Public video search retrieval mode and bounded result."},
		[]string{"mode", "result"},
	)
	MultimodalHybridCandidatesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{Namespace: "frux", Name: "multimodal_hybrid_candidates_total", Help: "Hybrid-search candidates by bounded contribution."},
		[]string{"contribution"},
	)
	MultimodalSimilarRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{Namespace: "frux", Name: "multimodal_similar_requests_total", Help: "Similar-video outcomes by bounded result."},
		[]string{"result"},
	)
)

func init() {
	prometheus.MustRegister(
		MultimodalEmbeddingJobs,
		MultimodalEmbeddingOldestAgeSeconds,
		MultimodalProviderCallsTotal,
		MultimodalProviderDuration,
		MultimodalProviderAdmissionTotal,
		MultimodalProviderTransportTotal,
		MultimodalProviderTransportDuration,
		MultimodalCoverageVideos,
		MultimodalProjectionTotal,
		MultimodalExactQueryDuration,
		MultimodalExactQueryResultsTotal,
		MultimodalQueryCacheTotal,
		MultimodalHybridRequestsTotal,
		MultimodalHybridCandidatesTotal,
		MultimodalSimilarRequestsTotal,
	)
}

func ObserveMultimodalJobState(state string, count int, oldestAge time.Duration) {
	if count < 0 {
		count = 0
	}
	if oldestAge < 0 {
		oldestAge = 0
	}
	MultimodalEmbeddingJobs.WithLabelValues(multimodalJobState(state)).Set(float64(count))
	MultimodalEmbeddingOldestAgeSeconds.Set(oldestAge.Seconds())
}

func ObserveMultimodalProvider(operation, result string, duration time.Duration) {
	operation = multimodalOperation(operation)
	result = multimodalProviderResult(result)
	MultimodalProviderCallsTotal.WithLabelValues(operation, result).Inc()
	MultimodalProviderDuration.WithLabelValues(operation, result).Observe(max(duration.Seconds(), 0))
}

func ObserveMultimodalAdmission(operation, result string) {
	MultimodalProviderAdmissionTotal.WithLabelValues(multimodalOperation(operation), multimodalAdmissionResult(result)).Inc()
}

func ObserveMultimodalProviderTransport(operation, result string, duration time.Duration) {
	operation = multimodalTransportOperation(operation)
	result = multimodalProviderResult(result)
	MultimodalProviderTransportTotal.WithLabelValues(operation, result).Inc()
	MultimodalProviderTransportDuration.WithLabelValues(operation, result).Observe(max(duration.Seconds(), 0))
}

func ObserveMultimodalProjection(result string, count int) {
	if count > 0 {
		MultimodalProjectionTotal.WithLabelValues(multimodalProjectionResult(result)).Add(float64(count))
	}
}

func ObserveMultimodalExactQuery(result string, duration time.Duration, count int) {
	result = multimodalQueryResult(result)
	MultimodalExactQueryDuration.WithLabelValues(result).Observe(max(duration.Seconds(), 0))
	if count > 0 {
		MultimodalExactQueryResultsTotal.WithLabelValues(result).Add(float64(count))
	}
}

func ObserveMultimodalQueryCache(outcome string) {
	MultimodalQueryCacheTotal.WithLabelValues(multimodalCacheOutcome(outcome)).Inc()
}

func ObserveMultimodalHybrid(mode, result string) {
	MultimodalHybridRequestsTotal.WithLabelValues(multimodalHybridMode(mode), multimodalHybridResult(result)).Inc()
}

func ObserveMultimodalHybridCandidates(contribution string, count int) {
	if count > 0 {
		MultimodalHybridCandidatesTotal.WithLabelValues(multimodalContribution(contribution)).Add(float64(count))
	}
}

func ObserveMultimodalSimilar(result string) {
	MultimodalSimilarRequestsTotal.WithLabelValues(multimodalSimilarResult(result)).Inc()
}

func multimodalJobState(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "pending", "leased", "retry", "succeeded", "terminal":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "unknown"
	}
}

func multimodalOperation(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "video", "query":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "unknown"
	}
}

func multimodalTransportOperation(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "readiness", "video", "query":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "unknown"
	}
}

func multimodalProviderResult(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "success", "timeout", "retryable", "terminal", "invalid", "cancelled":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "unknown"
	}
}

func multimodalAdmissionResult(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "accepted", "saturated":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "unknown"
	}
}

func multimodalProjectionResult(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "upserted", "deleted", "unchanged", "source_stale", "unreadable", "missing_fact":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "unknown"
	}
}

func multimodalQueryResult(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "success", "empty", "error", "cancelled":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "unknown"
	}
}

func multimodalCacheOutcome(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "hit", "miss", "write", "expired", "invalid":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "unknown"
	}
}

func multimodalHybridMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "hybrid", "lexical":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "unknown"
	}
}

func multimodalHybridResult(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "success", "fallback", "retryable", "error", "empty":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "unknown"
	}
}

func multimodalContribution(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "lexical_only", "semantic_only", "overlap":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "unknown"
	}
}

func multimodalSimilarResult(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "success", "empty", "unavailable", "source_unreadable", "filtered", "error":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "unknown"
	}
}
