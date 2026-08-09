package inframetrics

import (
	"time"

	domainembedding "github.com/shiyudesu/frux/internal/domain/embedding"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	SemanticClientRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "frux",
			Name:      "semantic_embedding_client_requests_total",
			Help:      "Semantic embedding client requests by bounded operation and result.",
		},
		[]string{"operation", "result"},
	)
	SemanticClientRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "frux",
			Name:      "semantic_embedding_client_request_duration_seconds",
			Help:      "Semantic embedding client request duration.",
		},
		[]string{"operation", "result"},
	)
	VideoEmbeddingVectorsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "frux",
			Name:      "video_embedding_vectors_total",
			Help:      "Video embedding outcomes by fixed model and source.",
		},
		[]string{"model", "source", "outcome"},
	)
	SemanticJobCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "frux",
			Name:      "video_embedding_semantic_jobs",
			Help:      "Semantic embedding jobs by bounded state.",
		},
		[]string{"state"},
	)
	SemanticJobOldestSeconds = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "frux",
			Name:      "video_embedding_semantic_job_oldest_seconds",
			Help:      "Age of the oldest semantic embedding job by bounded state.",
		},
		[]string{"state"},
	)
	VideoEmbeddingCoverage = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "frux",
			Name:      "video_embedding_coverage_videos",
			Help:      "Readable video embedding coverage by fixed model and state.",
		},
		[]string{"model", "state"},
	)
	SemanticLeaseTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "frux",
			Name:      "video_embedding_semantic_lease_total",
			Help:      "Semantic embedding lease heartbeat and fencing outcomes.",
		},
		[]string{"outcome"},
	)
)

func init() {
	prometheus.MustRegister(
		SemanticClientRequestsTotal,
		SemanticClientRequestDuration,
		VideoEmbeddingVectorsTotal,
		SemanticJobCount,
		SemanticJobOldestSeconds,
		VideoEmbeddingCoverage,
		SemanticLeaseTotal,
	)
}

func ObserveSemanticLease(outcome string) {
	switch outcome {
	case "extended", "lost":
	default:
		outcome = "lost"
	}
	SemanticLeaseTotal.WithLabelValues(outcome).Inc()
}

func ObserveSemanticCoverage(present, missing int64) {
	VideoEmbeddingCoverage.WithLabelValues("semantic", "present").Set(float64(present))
	VideoEmbeddingCoverage.WithLabelValues("semantic", "missing").Set(float64(missing))
}

func ObserveSemanticClient(operation, result string, duration time.Duration) {
	operation = semanticOperation(operation)
	result = semanticResult(result)
	SemanticClientRequestsTotal.WithLabelValues(operation, result).Inc()
	SemanticClientRequestDuration.WithLabelValues(operation, result).Observe(duration.Seconds())
}

func ObserveSemanticVector(outcome string) {
	switch outcome {
	case "generated", "skipped", "retried", "failed":
	default:
		outcome = "failed"
	}
	VideoEmbeddingVectorsTotal.WithLabelValues("semantic", "event", outcome).Inc()
}

func ObserveHashVector(outcome string) {
	switch outcome {
	case "generated", "skipped", "failed":
	default:
		outcome = "failed"
	}
	VideoEmbeddingVectorsTotal.WithLabelValues("hash", "event", outcome).Inc()
}

func ObserveSemanticBacklog(rows []domainembedding.SemanticBacklog, now time.Time) {
	states := []string{
		domainembedding.SemanticJobPending,
		domainembedding.SemanticJobProcessing,
		domainembedding.SemanticJobRetry,
		domainembedding.SemanticJobSuspended,
		domainembedding.SemanticJobCompleted,
		domainembedding.SemanticJobFailed,
	}
	for _, state := range states {
		SemanticJobCount.WithLabelValues(state).Set(0)
		SemanticJobOldestSeconds.WithLabelValues(state).Set(0)
	}
	for _, row := range rows {
		state := semanticJobState(row.State)
		SemanticJobCount.WithLabelValues(state).Set(float64(row.Count))
		if row.Count > 0 && row.OldestAt != nil {
			age := now.UTC().Sub(row.OldestAt.UTC()).Seconds()
			if age < 0 {
				age = 0
			}
			SemanticJobOldestSeconds.WithLabelValues(state).Set(age)
		}
	}
}

func semanticOperation(value string) string {
	switch value {
	case "metadata", "embed":
		return value
	default:
		return "embed"
	}
}

func semanticResult(value string) string {
	switch value {
	case "success", "canceled", "timeout", "over_capacity", "auth",
		"unavailable", "contract", "internal":
		return value
	default:
		return "internal"
	}
}

func semanticJobState(value string) string {
	switch value {
	case domainembedding.SemanticJobPending,
		domainembedding.SemanticJobProcessing,
		domainembedding.SemanticJobRetry,
		domainembedding.SemanticJobSuspended,
		domainembedding.SemanticJobCompleted,
		domainembedding.SemanticJobFailed:
		return value
	default:
		return domainembedding.SemanticJobFailed
	}
}
