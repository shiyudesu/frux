package inframetrics

import (
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	playbackStartupDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "frux",
			Name:      "playback_startup_duration_seconds",
			Help:      "Playback startup duration in seconds.",
			Buckets:   []float64{0.05, 0.1, 0.2, 0.3, 0.5, 0.75, 1, 1.5, 2, 3, 5, 8, 13, 20, 30, 60},
		},
		[]string{"scene", "network", "player", "measurement_method"},
	)

	playbackFirstFrameDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "frux",
			Name:      "playback_first_frame_duration_seconds",
			Help:      "Time from source load start to the first rendered frame in seconds.",
			Buckets:   []float64{0.05, 0.1, 0.2, 0.3, 0.5, 0.75, 1, 1.5, 2, 3, 5, 8, 13, 20, 30, 60},
		},
		[]string{"scene", "network", "player", "measurement_method"},
	)

	playbackRebufferCount = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "frux",
			Name:      "playback_rebuffer_count_total",
			Help:      "Total ordinary rebuffer intervals, excluding intentional seeks and pauses.",
		},
		[]string{"scene", "network", "player"},
	)

	playbackRebufferDuration = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "frux",
			Name:      "playback_rebuffer_duration_seconds_total",
			Help:      "Total ordinary rebuffer duration in seconds.",
		},
		[]string{"scene", "network", "player"},
	)

	playbackObservedDuration = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "frux",
			Name:      "playback_observed_duration_seconds_total",
			Help:      "Total playback duration used as the rebuffer-ratio denominator.",
		},
		[]string{"scene", "network", "player"},
	)

	playbackRebufferRatio = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "frux",
			Name:      "playback_rebuffer_ratio",
			Help:      "Per-playback ratio of rebuffer duration to observed playback duration.",
			Buckets:   []float64{0.001, 0.0025, 0.005, 0.01, 0.02, 0.05, 0.1, 0.2, 0.4, 0.75, 1},
		},
		[]string{"scene", "network", "player"},
	)

	playbackAttemptsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "frux",
			Name:      "playback_attempts_total",
			Help:      "Playback attempts by success or failure result.",
		},
		[]string{"scene", "network", "player", "result"},
	)

	playbackFailuresTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "frux",
			Name:      "playback_failures_total",
			Help:      "Playback failures by bounded error category.",
		},
		[]string{"scene", "network", "player", "error_category"},
	)

	playbackRecoveriesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "frux",
			Name:      "playback_recoveries_total",
			Help:      "Playback or rebuffer recovery outcomes.",
		},
		[]string{"scene", "network", "player", "outcome"},
	)

	playbackQualitySelectionsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "frux",
			Name:      "playback_quality_selections_total",
			Help:      "Selected playback quality buckets.",
		},
		[]string{"scene", "player", "quality"},
	)

	playbackSourceSelectionsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "frux",
			Name:      "playback_source_selections_total",
			Help:      "Selected playback source types.",
		},
		[]string{"scene", "player", "source"},
	)

	playbackTelemetryBatchesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "frux",
			Name:      "playback_telemetry_batches_total",
			Help:      "Playback telemetry batches by ingestion result.",
		},
		[]string{"result"},
	)

	playbackTelemetryEventsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "frux",
			Name:      "playback_telemetry_events_total",
			Help:      "Playback telemetry events by accepted, rejected, or duplicate result.",
		},
		[]string{"result"},
	)

	playbackTelemetryDeliveryDelay = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Namespace: "frux",
			Name:      "playback_telemetry_delivery_delay_seconds",
			Help:      "Delay from the client batch timestamp to server ingestion in seconds.",
			Buckets:   []float64{0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10, 30, 60, 120, 300, 600, 1800},
		},
	)

	playbackTelemetryCleanupRunsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "frux",
			Name:      "playback_telemetry_cleanup_runs_total",
			Help:      "Playback telemetry retention cleanup runs by result.",
		},
		[]string{"result"},
	)

	playbackTelemetryCleanupDeletedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "frux",
			Name:      "playback_telemetry_cleanup_deleted_total",
			Help:      "Playback telemetry rows deleted by retention cleanup.",
		},
		[]string{"kind"},
	)
)

type PlaybackDimensions struct {
	Scene   string
	Network string
	Player  string
}

type PlaybackTimingObservation struct {
	PlaybackDimensions
	MeasurementMethod string
	Duration          time.Duration
}

type PlaybackRebufferObservation struct {
	PlaybackDimensions
	Count            int
	Duration         time.Duration
	PlaybackDuration time.Duration
}

type PlaybackRebufferSummaryObservation struct {
	PlaybackDimensions
	Duration         time.Duration
	PlaybackDuration time.Duration
}

type PlaybackAttemptObservation struct {
	PlaybackDimensions
	Success       bool
	ErrorCategory string
}

type PlaybackRecoveryObservation struct {
	PlaybackDimensions
	Outcome string
}

type PlaybackSelectionObservation struct {
	Scene   string
	Player  string
	Quality string
	Source  string
}

type TelemetryIngestionObservation struct {
	Result          string
	AcceptedEvents  int
	RejectedEvents  int
	DuplicateEvents int
	DeliveryDelay   time.Duration
}

type TelemetryCleanupObservation struct {
	Success        bool
	DeletedEvents  int64
	DeletedBatches int64
}

type PlaybackMetricsRecorder struct{}

var PlaybackMetrics PlaybackMetricsRecorder

func init() {
	prometheus.MustRegister(
		playbackStartupDuration,
		playbackFirstFrameDuration,
		playbackRebufferCount,
		playbackRebufferDuration,
		playbackObservedDuration,
		playbackRebufferRatio,
		playbackAttemptsTotal,
		playbackFailuresTotal,
		playbackRecoveriesTotal,
		playbackQualitySelectionsTotal,
		playbackSourceSelectionsTotal,
		playbackTelemetryBatchesTotal,
		playbackTelemetryEventsTotal,
		playbackTelemetryDeliveryDelay,
		playbackTelemetryCleanupRunsTotal,
		playbackTelemetryCleanupDeletedTotal,
	)
}

func (PlaybackMetricsRecorder) ObserveStartup(observation PlaybackTimingObservation) {
	if observation.Duration < 0 {
		return
	}
	scene, network, player := playbackLabels(observation.PlaybackDimensions)
	method := boundedLabel(observation.MeasurementMethod, measurementMethods, "unknown")
	playbackStartupDuration.WithLabelValues(scene, network, player, method).Observe(observation.Duration.Seconds())
}

func (PlaybackMetricsRecorder) ObserveFirstFrame(observation PlaybackTimingObservation) {
	if observation.Duration < 0 {
		return
	}
	scene, network, player := playbackLabels(observation.PlaybackDimensions)
	method := boundedLabel(observation.MeasurementMethod, measurementMethods, "unknown")
	playbackFirstFrameDuration.WithLabelValues(scene, network, player, method).Observe(observation.Duration.Seconds())
}

func (PlaybackMetricsRecorder) ObserveRebuffer(observation PlaybackRebufferObservation) {
	if observation.Count < 0 || observation.Duration < 0 || observation.PlaybackDuration < 0 {
		return
	}
	scene, network, player := playbackLabels(observation.PlaybackDimensions)
	if observation.Count > 0 {
		playbackRebufferCount.WithLabelValues(scene, network, player).Add(float64(observation.Count))
	}
	playbackRebufferDuration.WithLabelValues(scene, network, player).Add(observation.Duration.Seconds())
	if observation.PlaybackDuration <= 0 {
		return
	}
	playbackObservedDuration.WithLabelValues(scene, network, player).Add(observation.PlaybackDuration.Seconds())
	ratio := observation.Duration.Seconds() / observation.PlaybackDuration.Seconds()
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	playbackRebufferRatio.WithLabelValues(scene, network, player).Observe(ratio)
}

func (PlaybackMetricsRecorder) ObserveRebufferSummary(observation PlaybackRebufferSummaryObservation) {
	if observation.Duration < 0 || observation.PlaybackDuration <= 0 {
		return
	}
	scene, network, player := playbackLabels(observation.PlaybackDimensions)
	playbackObservedDuration.WithLabelValues(scene, network, player).Add(observation.PlaybackDuration.Seconds())
	ratio := observation.Duration.Seconds() / observation.PlaybackDuration.Seconds()
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	playbackRebufferRatio.WithLabelValues(scene, network, player).Observe(ratio)
}

func (PlaybackMetricsRecorder) ObserveAttempt(observation PlaybackAttemptObservation) {
	scene, network, player := playbackLabels(observation.PlaybackDimensions)
	result := "success"
	if !observation.Success {
		result = "failure"
	}
	playbackAttemptsTotal.WithLabelValues(scene, network, player, result).Inc()
	if observation.Success {
		return
	}
	category := boundedLabel(observation.ErrorCategory, errorCategories, "unknown")
	playbackFailuresTotal.WithLabelValues(scene, network, player, category).Inc()
}

func (PlaybackMetricsRecorder) ObserveRecovery(observation PlaybackRecoveryObservation) {
	scene, network, player := playbackLabels(observation.PlaybackDimensions)
	outcome := boundedLabel(observation.Outcome, recoveryOutcomes, "unknown")
	playbackRecoveriesTotal.WithLabelValues(scene, network, player, outcome).Inc()
}

func (PlaybackMetricsRecorder) ObserveSelection(observation PlaybackSelectionObservation) {
	scene := boundedLabel(observation.Scene, scenes, "unknown")
	player := boundedLabel(observation.Player, players, "unknown")
	if strings.TrimSpace(observation.Quality) != "" {
		quality := boundedLabel(observation.Quality, qualities, "other")
		playbackQualitySelectionsTotal.WithLabelValues(scene, player, quality).Inc()
	}
	if strings.TrimSpace(observation.Source) != "" {
		source := boundedLabel(observation.Source, sources, "other")
		playbackSourceSelectionsTotal.WithLabelValues(scene, player, source).Inc()
	}
}

func (PlaybackMetricsRecorder) ObserveTelemetryIngestion(observation TelemetryIngestionObservation) {
	result := boundedLabel(observation.Result, ingestionResults, "rejected")
	playbackTelemetryBatchesTotal.WithLabelValues(result).Inc()
	addPositive(playbackTelemetryEventsTotal.WithLabelValues("accepted"), observation.AcceptedEvents)
	addPositive(playbackTelemetryEventsTotal.WithLabelValues("rejected"), observation.RejectedEvents)
	addPositive(playbackTelemetryEventsTotal.WithLabelValues("duplicate"), observation.DuplicateEvents)
	if observation.DeliveryDelay > 0 {
		playbackTelemetryDeliveryDelay.Observe(observation.DeliveryDelay.Seconds())
	}
}

func (PlaybackMetricsRecorder) ObserveTelemetryCleanup(observation TelemetryCleanupObservation) {
	result := "success"
	if !observation.Success {
		result = "failure"
	}
	playbackTelemetryCleanupRunsTotal.WithLabelValues(result).Inc()
	if observation.DeletedEvents > 0 {
		playbackTelemetryCleanupDeletedTotal.WithLabelValues("event").Add(float64(observation.DeletedEvents))
	}
	if observation.DeletedBatches > 0 {
		playbackTelemetryCleanupDeletedTotal.WithLabelValues("batch").Add(float64(observation.DeletedBatches))
	}
}

func playbackLabels(dimensions PlaybackDimensions) (string, string, string) {
	return boundedLabel(dimensions.Scene, scenes, "unknown"),
		boundedLabel(dimensions.Network, networks, "unknown"),
		boundedLabel(dimensions.Player, players, "unknown")
}

func boundedLabel(value string, allowed map[string]struct{}, fallback string) string {
	value = normalizeLabel(value, fallback)
	if _, ok := allowed[value]; ok {
		return value
	}
	return fallback
}

func addPositive(counter prometheus.Counter, value int) {
	if value > 0 {
		counter.Add(float64(value))
	}
}

func labelSet(values ...string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}

var (
	scenes = labelSet(
		"timeline", "recommend", "following", "hot", "profile", "detail",
		"library", "search", "history", "favorites", "watch_later", "unknown", "other",
	)
	networks = labelSet(
		"offline", "slow_2g", "2g", "3g", "4g", "5g", "wifi", "ethernet", "unknown",
	)
	players            = labelSet("native_mp4", "dash", "hls", "unknown", "other")
	measurementMethods = labelSet("video_frame_callback", "advancing_time", "playing", "unknown")
	errorCategories    = labelSet("aborted", "network", "decode", "unsupported", "autoplay", "timeout", "unknown")
	recoveryOutcomes   = labelSet("resumed", "paused", "seeked", "source_changed", "ended", "failed", "unknown")
	qualities          = labelSet(
		"auto", "source", "audio", "144p", "240p", "360p", "480p", "540p",
		"720p", "1080p", "1440p", "2160p", "unknown", "other",
	)
	sources          = labelSet("mp4", "dash", "hls", "unknown", "other")
	ingestionResults = labelSet("accepted", "rejected", "duplicate")
)
