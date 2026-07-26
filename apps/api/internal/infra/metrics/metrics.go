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
			Namespace: "gcfeed",
			Name:      "http_requests_total",
			Help:      "Total HTTP requests handled by the API.",
		},
		[]string{"method", "route", "status"},
	)

	HTTPRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "gcfeed",
			Name:      "http_request_duration_seconds",
			Help:      "HTTP request duration in seconds.",
			Buckets:   []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5},
		},
		[]string{"method", "route", "status"},
	)

	FeedRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "gcfeed",
			Name:      "feed_requests_total",
			Help:      "Total feed requests by scene and result.",
		},
		[]string{"scene", "result"},
	)

	FeedRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "gcfeed",
			Name:      "feed_request_duration_seconds",
			Help:      "Feed request duration in seconds by scene.",
			Buckets:   []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5},
		},
		[]string{"scene", "result"},
	)

	FeedItemsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "gcfeed",
			Name:      "feed_items_total",
			Help:      "Total feed items returned by scene.",
		},
		[]string{"scene"},
	)

	FeedCacheRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "gcfeed",
			Name:      "feed_cache_requests_total",
			Help:      "Feed cache reads by cache area and result.",
		},
		[]string{"area", "result"},
	)

	FeedCacheWritesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "gcfeed",
			Name:      "feed_cache_writes_total",
			Help:      "Feed cache writes by cache area and result.",
		},
		[]string{"area", "result"},
	)

	VideoUploadTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "gcfeed",
			Name:      "video_upload_total",
			Help:      "Upload requests by kind and result.",
		},
		[]string{"kind", "result"},
	)

	VideoUploadDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "gcfeed",
			Name:      "video_upload_duration_seconds",
			Help:      "Upload request processing duration in seconds.",
			Buckets:   []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120},
		},
		[]string{"kind", "result"},
	)

	VideoProcessingDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "gcfeed",
			Name:      "video_processing_duration_seconds",
			Help:      "Video processing step duration in seconds.",
			Buckets:   []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120},
		},
		[]string{"step", "result"},
	)

	WorkerJobsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "gcfeed",
			Name:      "worker_jobs_total",
			Help:      "Worker jobs handled by job name and result.",
		},
		[]string{"job", "result"},
	)

	WorkerJobDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "gcfeed",
			Name:      "worker_job_duration_seconds",
			Help:      "Worker job processing duration in seconds.",
			Buckets:   []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		},
		[]string{"job", "result"},
	)

	ViewEventOutboxPending = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "gcfeed",
			Name:      "view_event_outbox_pending",
			Help:      "Pending view-event outbox rows.",
		},
	)

	ViewEventOutboxLagSeconds = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "gcfeed",
			Name:      "view_event_outbox_lag_seconds",
			Help:      "Age in seconds of the oldest pending view-event outbox row.",
		},
	)

	ViewEventOutboxDispatchTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "gcfeed",
			Name:      "view_event_outbox_dispatch_total",
			Help:      "View-event outbox dispatch observations by result.",
		},
		[]string{"result"},
	)

	MediaObjectOperationsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "gcfeed",
			Name:      "media_object_operations_total",
			Help:      "Object-storage operations by operation, backend, and result.",
		},
		[]string{"operation", "backend", "result"},
	)

	MediaObjectOperationDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "gcfeed",
			Name:      "media_object_operation_duration_seconds",
			Help:      "Object-storage operation duration in seconds.",
			Buckets:   []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30},
		},
		[]string{"operation", "backend", "result"},
	)

	MediaProcessingResultsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "gcfeed",
			Name:      "media_processing_results_total",
			Help:      "Media processing state transitions.",
		},
		[]string{"state", "error_code"},
	)

	MediaRenditionsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "gcfeed",
			Name:      "media_renditions_total",
			Help:      "Generated media renditions by result.",
		},
		[]string{"result"},
	)

	MediaReconciliationIssuesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "gcfeed",
			Name:      "media_reconciliation_issues_total",
			Help:      "Media reconciliation findings by issue type and result.",
		},
		[]string{"issue", "result"},
	)

	MediaCleanupBacklog = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "gcfeed",
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
		MediaObjectOperationsTotal,
		MediaObjectOperationDuration,
		MediaProcessingResultsTotal,
		MediaRenditionsTotal,
		MediaReconciliationIssuesTotal,
		MediaCleanupBacklog,
	)
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
