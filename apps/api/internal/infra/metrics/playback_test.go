package inframetrics

import (
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestPlaybackRecorderBoundsMetricLabels(t *testing.T) {
	before := testutil.ToFloat64(playbackAttemptsTotal.WithLabelValues("unknown", "unknown", "unknown", "failure"))
	PlaybackMetrics.ObserveAttempt(PlaybackAttemptObservation{
		PlaybackDimensions: PlaybackDimensions{
			Scene:   "user-42",
			Network: "request-123",
			Player:  "session-abc",
		},
		ErrorCategory: "video-99",
	})

	if got := testutil.ToFloat64(playbackAttemptsTotal.WithLabelValues("unknown", "unknown", "unknown", "failure")) - before; got != 1 {
		t.Fatalf("expected unknown bounded labels to receive one attempt, got %v", got)
	}
	if got := testutil.ToFloat64(playbackFailuresTotal.WithLabelValues("unknown", "unknown", "unknown", "unknown")); got < 1 {
		t.Fatalf("expected unknown error category to be recorded, got %v", got)
	}
}

func TestPlaybackRecorderRecordsRebufferAndIngestion(t *testing.T) {
	dimensions := PlaybackDimensions{Scene: "recommend", Network: "wifi", Player: "native_mp4"}
	rebufferBefore := testutil.ToFloat64(playbackRebufferCount.WithLabelValues("recommend", "wifi", "native_mp4"))
	acceptedBefore := testutil.ToFloat64(playbackTelemetryEventsTotal.WithLabelValues("accepted"))
	duplicateBefore := testutil.ToFloat64(playbackTelemetryEventsTotal.WithLabelValues("duplicate"))

	PlaybackMetrics.ObserveRebuffer(PlaybackRebufferObservation{
		PlaybackDimensions: dimensions,
		Count:              2,
		Duration:           2 * time.Second,
		PlaybackDuration:   20 * time.Second,
	})
	PlaybackMetrics.ObserveTelemetryIngestion(TelemetryIngestionObservation{
		Result:          "accepted",
		AcceptedEvents:  8,
		DuplicateEvents: 2,
		DeliveryDelay:   3 * time.Second,
	})

	if got := testutil.ToFloat64(playbackRebufferCount.WithLabelValues("recommend", "wifi", "native_mp4")) - rebufferBefore; got != 2 {
		t.Fatalf("expected two rebuffers, got %v", got)
	}

	if got := testutil.ToFloat64(playbackTelemetryEventsTotal.WithLabelValues("accepted")) - acceptedBefore; got != 8 {
		t.Fatalf("expected eight accepted events, got %v", got)
	}
	if got := testutil.ToFloat64(playbackTelemetryEventsTotal.WithLabelValues("duplicate")) - duplicateBefore; got != 2 {
		t.Fatalf("expected two duplicate events, got %v", got)
	}
}

func TestPlaybackRecorderRecordsTelemetryCleanup(t *testing.T) {
	successBefore := testutil.ToFloat64(playbackTelemetryCleanupRunsTotal.WithLabelValues("success"))
	eventsBefore := testutil.ToFloat64(playbackTelemetryCleanupDeletedTotal.WithLabelValues("event"))
	batchesBefore := testutil.ToFloat64(playbackTelemetryCleanupDeletedTotal.WithLabelValues("batch"))

	PlaybackMetrics.ObserveTelemetryCleanup(TelemetryCleanupObservation{
		Success: true, DeletedEvents: 12, DeletedBatches: 3,
	})

	if got := testutil.ToFloat64(playbackTelemetryCleanupRunsTotal.WithLabelValues("success")) - successBefore; got != 1 {
		t.Fatalf("expected one successful cleanup, got %v", got)
	}
	if got := testutil.ToFloat64(playbackTelemetryCleanupDeletedTotal.WithLabelValues("event")) - eventsBefore; got != 12 {
		t.Fatalf("expected twelve deleted events, got %v", got)
	}
	if got := testutil.ToFloat64(playbackTelemetryCleanupDeletedTotal.WithLabelValues("batch")) - batchesBefore; got != 3 {
		t.Fatalf("expected three deleted batches, got %v", got)
	}
}

func TestPlaybackCollectorsExcludeIdentifierLabels(t *testing.T) {
	dimensions := PlaybackDimensions{Scene: "recommend", Network: "4g", Player: "dash"}
	timing := PlaybackTimingObservation{
		PlaybackDimensions: dimensions,
		MeasurementMethod:  "video_frame_callback",
		Duration:           250 * time.Millisecond,
	}
	PlaybackMetrics.ObserveStartup(timing)
	PlaybackMetrics.ObserveFirstFrame(timing)
	PlaybackMetrics.ObserveRebuffer(PlaybackRebufferObservation{
		PlaybackDimensions: dimensions,
		Count:              1,
		Duration:           time.Second,
		PlaybackDuration:   10 * time.Second,
	})
	PlaybackMetrics.ObserveAttempt(PlaybackAttemptObservation{
		PlaybackDimensions: dimensions,
		ErrorCategory:      "network",
	})
	PlaybackMetrics.ObserveRecovery(PlaybackRecoveryObservation{
		PlaybackDimensions: dimensions,
		Outcome:            "resumed",
	})
	PlaybackMetrics.ObserveSelection(PlaybackSelectionObservation{
		Scene: "recommend", Player: "dash", Quality: "720p", Source: "dash",
	})
	PlaybackMetrics.ObserveTelemetryIngestion(TelemetryIngestionObservation{
		Result: "accepted", AcceptedEvents: 1, DeliveryDelay: time.Second,
	})

	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	prohibited := map[string]struct{}{
		"user_id": {}, "video_id": {}, "request_id": {}, "session_id": {}, "playback_session_id": {},
	}
	allowed := map[string]struct{}{
		"scene": {}, "network": {}, "player": {}, "measurement_method": {}, "result": {},
		"error_category": {}, "outcome": {}, "quality": {}, "source": {}, "kind": {},
	}
	for _, family := range families {
		if !strings.HasPrefix(family.GetName(), "frux_playback_") {
			continue
		}
		for _, metric := range family.Metric {
			for _, label := range metric.Label {
				if _, exists := prohibited[label.GetName()]; exists {
					t.Fatalf("metric %s exposes prohibited label %s", family.GetName(), label.GetName())
				}
				if _, exists := allowed[label.GetName()]; !exists {
					t.Fatalf("metric %s exposes unreviewed label %s", family.GetName(), label.GetName())
				}
			}
		}
	}
}
