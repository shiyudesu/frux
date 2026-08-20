package inframetrics

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	HTTPRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "frux",
			Name:      "http_requests_total",
			Help:      "Total HTTP requests handled by the API.",
		},
		[]string{"method", "route", "status"},
	)

	HTTPRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "frux",
			Name:      "http_request_duration_seconds",
			Help:      "HTTP request duration in seconds.",
			Buckets:   []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5},
		},
		[]string{"method", "route", "status"},
	)

	FeedRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "frux",
			Name:      "feed_requests_total",
			Help:      "Total feed requests by scene and result.",
		},
		[]string{"scene", "result"},
	)

	FeedRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "frux",
			Name:      "feed_request_duration_seconds",
			Help:      "Feed request duration in seconds by scene.",
			Buckets:   []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5},
		},
		[]string{"scene", "result"},
	)

	FeedItemsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "frux",
			Name:      "feed_items_total",
			Help:      "Total feed items returned by scene.",
		},
		[]string{"scene"},
	)

	FeedCacheRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "frux",
			Name:      "feed_cache_requests_total",
			Help:      "Feed cache reads by cache area and result.",
		},
		[]string{"area", "result"},
	)

	FeedCacheWritesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "frux",
			Name:      "feed_cache_writes_total",
			Help:      "Feed cache writes by cache area and result.",
		},
		[]string{"area", "result"},
	)

	VideoUploadTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "frux",
			Name:      "video_upload_total",
			Help:      "Upload requests by kind and result.",
		},
		[]string{"kind", "result"},
	)

	VideoUploadDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "frux",
			Name:      "video_upload_duration_seconds",
			Help:      "Upload request processing duration in seconds.",
			Buckets:   []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120},
		},
		[]string{"kind", "result"},
	)

	VideoProcessingDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "frux",
			Name:      "video_processing_duration_seconds",
			Help:      "Video processing step duration in seconds.",
			Buckets:   []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120},
		},
		[]string{"step", "result"},
	)

	WorkerJobsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "frux",
			Name:      "worker_jobs_total",
			Help:      "Worker jobs handled by job name and result.",
		},
		[]string{"job", "result"},
	)

	WorkerJobDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "frux",
			Name:      "worker_job_duration_seconds",
			Help:      "Worker job processing duration in seconds.",
			Buckets:   []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		},
		[]string{"job", "result"},
	)

	ViewEventOutboxPending = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "frux",
			Name:      "view_event_outbox_pending",
			Help:      "Pending view-event outbox rows.",
		},
	)

	ViewEventOutboxLagSeconds = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "frux",
			Name:      "view_event_outbox_lag_seconds",
			Help:      "Age in seconds of the oldest pending view-event outbox row.",
		},
	)

	ProfileWorkerLagSeconds = prometheus.NewGauge(
		prometheus.GaugeOpts{Namespace: "frux", Name: "recommendation_profile_worker_lag_seconds", Help: "Age of the latest processed recommendation profile signal."},
	)
	ProfileWorkerEventsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{Namespace: "frux", Name: "recommendation_profile_worker_events_total", Help: "Recommendation profile projection events by result."},
		[]string{"result"},
	)
	RecommendationRecallCandidatesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{Namespace: "frux", Name: "recommendation_recall_candidates_total", Help: "Candidates retained by bounded recall provider."},
		[]string{"provider"},
	)
	RecommendationCandidatePoolCandidatesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{Namespace: "frux", Name: "recommendation_candidate_pool_candidates_total", Help: "Candidates observed at bounded recommendation pool stages."},
		[]string{"stage", "provider"},
	)
	RecommendationPolicyRejectionsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{Namespace: "frux", Name: "recommendation_policy_rejections_total", Help: "Recommendation policies rejected by bounded reason."},
		[]string{"reason"},
	)
	RecommendationDegradedRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{Namespace: "frux", Name: "recommendation_degraded_requests_total", Help: "Degraded recommendation requests by bounded provider reason."},
		[]string{"provider", "reason"},
	)
	RecommendationSnapshotOperationsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{Namespace: "frux", Name: "recommendation_snapshot_operations_total", Help: "Recommendation snapshot operation outcomes."},
		[]string{"result"},
	)
	RecommendationRequestLogFailuresTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{Namespace: "frux", Name: "recommendation_request_log_failures_total", Help: "Sampled recommendation request-log failures by bounded stage."},
		[]string{"stage"},
	)
	RecommendationDeliveryFailuresTotal = prometheus.NewCounter(
		prometheus.CounterOpts{Namespace: "frux", Name: "recommendation_delivery_failures_total", Help: "Recommendation delivery evidence writes that failed after Feed assembly."},
	)
	RecommendationActivePolicyVersion = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{Namespace: "frux", Name: "recommendation_active_policy_version", Help: "Active recommendation policy version by scene."},
		[]string{"scene"},
	)
	RecommendationOutcomesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{Namespace: "frux", Name: "recommendation_outcomes_total", Help: "Recommendation outcomes by bounded type."},
		[]string{"outcome"},
	)
	RecommendationInvalidAttributionsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{Namespace: "frux", Name: "recommendation_invalid_attributions_total", Help: "Rejected recommendation attributions by bounded outcome type."},
		[]string{"outcome"},
	)
	ReviewEventsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{Namespace: "frux", Name: "review_events_total", Help: "Automated review events by bounded stage and result."},
		[]string{"stage", "result"},
	)

	ViewEventOutboxDispatchTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "frux",
			Name:      "view_event_outbox_dispatch_total",
			Help:      "View-event outbox dispatch observations by result.",
		},
		[]string{"result"},
	)

	MediaObjectOperationsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "frux",
			Name:      "media_object_operations_total",
			Help:      "Object-storage operations by operation, backend, and result.",
		},
		[]string{"operation", "backend", "result"},
	)

	MediaObjectOperationDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "frux",
			Name:      "media_object_operation_duration_seconds",
			Help:      "Object-storage operation duration in seconds.",
			Buckets:   []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30},
		},
		[]string{"operation", "backend", "result"},
	)

	MediaProcessingResultsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "frux",
			Name:      "media_processing_results_total",
			Help:      "Media processing state transitions.",
		},
		[]string{"state", "error_code"},
	)

	MediaRenditionsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "frux",
			Name:      "media_renditions_total",
			Help:      "Generated media renditions by result.",
		},
		[]string{"result"},
	)

	MediaReconciliationIssuesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "frux",
			Name:      "media_reconciliation_issues_total",
			Help:      "Media reconciliation findings by issue type and result.",
		},
		[]string{"issue", "result"},
	)

	MediaCleanupBacklog = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "frux",
			Name:      "media_cleanup_backlog",
			Help:      "Observed media cleanup backlog.",
		},
	)
)

func init() {
	prometheus.MustRegister(
		HTTPRequestsTotal,
		HTTPRequestDuration,
		FeedRequestsTotal,
		FeedRequestDuration,
		FeedItemsTotal,
		FeedCacheRequestsTotal,
		FeedCacheWritesTotal,
		VideoUploadTotal,
		VideoUploadDuration,
		VideoProcessingDuration,
		WorkerJobsTotal,
		WorkerJobDuration,
		ViewEventOutboxPending,
		ViewEventOutboxLagSeconds,
		ViewEventOutboxDispatchTotal,
		ProfileWorkerLagSeconds,
		ProfileWorkerEventsTotal,
		RecommendationRecallCandidatesTotal,
		RecommendationCandidatePoolCandidatesTotal,
		RecommendationPolicyRejectionsTotal,
		RecommendationDegradedRequestsTotal,
		RecommendationSnapshotOperationsTotal,
		RecommendationRequestLogFailuresTotal,
		RecommendationDeliveryFailuresTotal,
		RecommendationActivePolicyVersion,
		RecommendationOutcomesTotal,
		RecommendationInvalidAttributionsTotal,
		ReviewEventsTotal,
		MediaObjectOperationsTotal,
		MediaObjectOperationDuration,
		MediaProcessingResultsTotal,
		MediaRenditionsTotal,
		MediaReconciliationIssuesTotal,
		MediaCleanupBacklog,
	)
}

func ObserveReview(stage, result string) {
	ReviewEventsTotal.WithLabelValues(reviewStageLabel(stage), reviewResultLabel(result)).Inc()
}

func reviewStageLabel(value string) string {
	switch normalizeLabel(value, "unknown") {
	case "intake", "provider_result", "routing", "reconciliation":
		return normalizeLabel(value, "unknown")
	default:
		return "unknown"
	}
}

func reviewResultLabel(value string) string {
	switch normalizeLabel(value, "unknown") {
	case "created", "existing", "accepted", "approve", "reject", "human",
		"duplicate", "invalid", "conflict", "retry", "success":
		return normalizeLabel(value, "unknown")
	default:
		return "unknown"
	}
}

func ObserveRecommendationRecall(provider string, count int) {
	if count > 0 {
		RecommendationRecallCandidatesTotal.WithLabelValues(recommendationProviderLabel(provider)).Add(float64(count))
	}
}
func ObserveRecommendationCandidatePool(stage, provider string, count int) {
	if count > 0 {
		RecommendationCandidatePoolCandidatesTotal.WithLabelValues(
			recommendationCandidatePoolStage(stage), recommendationCandidatePoolProvider(provider),
		).Add(float64(count))
	}
}
func ObserveRecommendationPolicyRejection(reason string) {
	RecommendationPolicyRejectionsTotal.WithLabelValues(recommendationPolicyRejectionReason(reason)).Inc()
}
func ObserveRecommendationDegraded(provider, reason string) {
	RecommendationDegradedRequestsTotal.WithLabelValues(recommendationProviderLabel(provider), recommendationReasonLabel(reason)).Inc()
}
func ObserveRecommendationSnapshot(result string) {
	RecommendationSnapshotOperationsTotal.WithLabelValues(recommendationSnapshotResult(result)).Inc()
}
func ObserveRecommendationRequestLogFailure(stage string) {
	RecommendationRequestLogFailuresTotal.WithLabelValues(recommendationRequestLogStage(stage)).Inc()
}
func ObserveRecommendationDeliveryFailure() {
	RecommendationDeliveryFailuresTotal.Inc()
}
func ObserveRecommendationPolicy(scene string, version int) {
	if version > 0 {
		RecommendationActivePolicyVersion.WithLabelValues(normalizeLabel(scene, "unknown")).Set(float64(version))
	}
}
func ObserveRecommendationOutcome(outcome string) {
	RecommendationOutcomesTotal.WithLabelValues(recommendationOutcomeLabel(outcome)).Inc()
}
func ObserveRecommendationInvalidAttribution(outcome string) {
	RecommendationInvalidAttributionsTotal.WithLabelValues(recommendationOutcomeLabel(outcome)).Inc()
}

func recommendationProviderLabel(value string) string {
	switch normalizeLabel(value, "unknown") {
	case "fresh", "hot", "content_similarity", "followed_author", "session_continuation":
		return normalizeLabel(value, "unknown")
	default:
		return "unknown"
	}
}
func recommendationCandidatePoolProvider(value string) string {
	if normalizeLabel(value, "unknown") == "all" {
		return "all"
	}
	return recommendationProviderLabel(value)
}
func recommendationCandidatePoolStage(value string) string {
	switch normalizeLabel(value, "unknown") {
	case "provider_returned", "unique_merged", "visibility_filtered", "ranker_input":
		return normalizeLabel(value, "unknown")
	default:
		return "unknown"
	}
}
func recommendationPolicyRejectionReason(value string) string {
	switch normalizeLabel(value, "unknown") {
	case "pre_rank_pool":
		return "pre_rank_pool"
	default:
		return "unknown"
	}
}
func recommendationReasonLabel(value string) string {
	switch normalizeLabel(value, "unknown") {
	case "timeout", "error", "snapshot_unavailable":
		return normalizeLabel(value, "unknown")
	default:
		return "unknown"
	}
}
func recommendationSnapshotResult(value string) string {
	switch normalizeLabel(value, "unknown") {
	case "hit", "miss", "read_failure", "write_success", "write_failure", "maintenance_failure", "degraded_fallback":
		return normalizeLabel(value, "unknown")
	default:
		return "unknown"
	}
}
func recommendationRequestLogStage(value string) string {
	switch normalizeLabel(value, "unknown") {
	case "validation", "storage":
		return normalizeLabel(value, "unknown")
	default:
		return "unknown"
	}
}
func recommendationOutcomeLabel(value string) string {
	switch normalizeLabel(value, "unknown") {
	case "exposed", "play", "progress", "complete", "skip", "like", "favorite", "follow", "not_interested", "reduce_author", "already_seen":
		return normalizeLabel(value, "unknown")
	default:
		return "unknown"
	}
}

// HTTPMiddleware records request count and latency with stable route labels.
func HTTPMiddleware() app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		start := time.Now()
		c.Next(ctx)

		route := c.FullPath()
		if route == "" {
			route = string(c.Path())
		}
		status := strconv.Itoa(c.Response.StatusCode())
		method := string(c.Method())
		duration := time.Since(start).Seconds()

		HTTPRequestsTotal.WithLabelValues(method, route, status).Inc()
		HTTPRequestDuration.WithLabelValues(method, route, status).Observe(duration)
	}
}

func Handler() http.Handler {
	return promhttp.Handler()
}

func RunServer(ctx context.Context, addr string) error {
	server := &http.Server{
		Addr:    addr,
		Handler: Handler(),
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	err := server.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func ObserveFeed(scene string, duration time.Duration, itemCount int, err error) {
	scene = normalizeLabel(scene, "unknown")
	result := resultLabel(err)
	FeedRequestsTotal.WithLabelValues(scene, result).Inc()
	FeedRequestDuration.WithLabelValues(scene, result).Observe(duration.Seconds())
	if err == nil && itemCount > 0 {
		FeedItemsTotal.WithLabelValues(scene).Add(float64(itemCount))
	}
}

func ObserveCacheRead(area string, requested int, hit int, err error) {
	area = normalizeLabel(area, "unknown")
	if err != nil {
		FeedCacheRequestsTotal.WithLabelValues(area, "error").Inc()
		return
	}
	if hit > 0 {
		FeedCacheRequestsTotal.WithLabelValues(area, "hit").Add(float64(hit))
	}
	miss := requested - hit
	if miss > 0 {
		FeedCacheRequestsTotal.WithLabelValues(area, "miss").Add(float64(miss))
	}
}

func ObserveCacheWrite(area string, count int, err error) {
	area = normalizeLabel(area, "unknown")
	if count <= 0 {
		count = 1
	}
	FeedCacheWritesTotal.WithLabelValues(area, resultLabel(err)).Add(float64(count))
}

func ObserveUpload(kind string, duration time.Duration, err error) {
	kind = normalizeLabel(kind, "unknown")
	result := resultLabel(err)
	VideoUploadTotal.WithLabelValues(kind, result).Inc()
	VideoUploadDuration.WithLabelValues(kind, result).Observe(duration.Seconds())
}

func ObserveVideoProcessing(step string, duration time.Duration, err error) {
	step = normalizeLabel(step, "unknown")
	VideoProcessingDuration.WithLabelValues(step, resultLabel(err)).Observe(duration.Seconds())
}

func ObserveWorkerJob(job string, duration time.Duration, err error) {
	job = normalizeLabel(job, "unknown")
	result := resultLabel(err)
	WorkerJobsTotal.WithLabelValues(job, result).Inc()
	WorkerJobDuration.WithLabelValues(job, result).Observe(duration.Seconds())
}

// ObserveProfileWorker records bounded worker outcomes without event identifiers as labels.
func ObserveProfileWorker(occurredAt time.Time, duplicate bool, err error) {
	result := "updated"
	if err != nil {
		result = "failure"
	} else if duplicate {
		result = "duplicate"
	}
	ProfileWorkerEventsTotal.WithLabelValues(result).Inc()
	if err == nil && !occurredAt.IsZero() {
		lag := time.Since(occurredAt).Seconds()
		if lag < 0 {
			lag = 0
		}
		ProfileWorkerLagSeconds.Set(lag)
	}
}

func ObserveViewEventOutbox(pending int64, oldest time.Time, now time.Time, err error) {
	ViewEventOutboxPending.Set(float64(pending))
	lag := 0.0
	if pending > 0 && !oldest.IsZero() {
		lag = now.Sub(oldest).Seconds()
		if lag < 0 {
			lag = 0
		}
	}
	ViewEventOutboxLagSeconds.Set(lag)
	ViewEventOutboxDispatchTotal.WithLabelValues(resultLabel(err)).Inc()
}

func ObserveMediaObject(operation, backend string, duration time.Duration, err error) {
	operation = normalizeLabel(operation, "unknown")
	backend = normalizeLabel(backend, "unknown")
	result := resultLabel(err)
	MediaObjectOperationsTotal.WithLabelValues(operation, backend, result).Inc()
	MediaObjectOperationDuration.WithLabelValues(operation, backend, result).Observe(duration.Seconds())
}

func ObserveMediaProcessing(state, errorCode string) {
	state = normalizeLabel(state, "unknown")
	errorCode = normalizeLabel(errorCode, "none")
	MediaProcessingResultsTotal.WithLabelValues(state, errorCode).Inc()
}

func ObserveMediaRenditions(count int, err error) {
	if count <= 0 {
		count = 1
	}
	MediaRenditionsTotal.WithLabelValues(resultLabel(err)).Add(float64(count))
}

func ObserveMediaReconciliation(issue string, count int, err error) {
	if count <= 0 {
		count = 1
	}
	MediaReconciliationIssuesTotal.WithLabelValues(normalizeLabel(issue, "unknown"), resultLabel(err)).Add(float64(count))
}

func ObserveMediaCleanupBacklog(count int64) {
	if count < 0 {
		count = 0
	}
	MediaCleanupBacklog.Set(float64(count))
}

func resultLabel(err error) string {
	if err != nil {
		return "error"
	}
	return "success"
}

func normalizeLabel(value string, fallback string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return fallback
	}
	return value
}
