package interfaceshttprouter

import (
	domainplayback "github.com/shiyudesu/frux/internal/domain/playback"
	inframetrics "github.com/shiyudesu/frux/internal/infra/metrics"
	"time"
)

type playbackMetricsAdapter struct{}

func (playbackMetricsAdapter) RecordTelemetryBatch(batch *domainplayback.TelemetryBatch, summary *domainplayback.TelemetryBatchWriteResult, receivedAt time.Time) {
	result := "accepted"
	acceptedEvents := summary.AcceptedCount
	duplicateEvents := summary.DuplicateCount
	if !summary.Created {
		result = "duplicate"
		acceptedEvents = 0
		duplicateEvents = len(batch.Events)
	} else if acceptedEvents == 0 && duplicateEvents > 0 {
		result = "duplicate"
	}
	deliveryDelay := receivedAt.Sub(batch.ClientSentAt)
	if deliveryDelay < 0 {
		deliveryDelay = 0
	}
	inframetrics.PlaybackMetrics.ObserveTelemetryIngestion(inframetrics.TelemetryIngestionObservation{
		Result:          result,
		AcceptedEvents:  acceptedEvents,
		DuplicateEvents: duplicateEvents,
		DeliveryDelay:   deliveryDelay,
	})
	if !summary.Created || len(summary.AcceptedEventIDs) == 0 {
		return
	}

	accepted := make(map[string]struct{}, len(summary.AcceptedEventIDs))
	for _, eventID := range summary.AcceptedEventIDs {
		accepted[eventID] = struct{}{}
	}
	dimensions := inframetrics.PlaybackDimensions{
		Scene:   batch.Context.Scene,
		Network: string(batch.Context.NetworkClass),
		Player:  string(batch.Context.PlayerAdapter),
	}
	for _, event := range batch.Events {
		if _, ok := accepted[event.EventID]; !ok {
			continue
		}
		switch event.EventType {
		case domainplayback.TelemetryEventLoadStart:
			inframetrics.PlaybackMetrics.ObserveSelection(inframetrics.PlaybackSelectionObservation{
				Scene: batch.Context.Scene, Player: string(batch.Context.PlayerAdapter),
				Quality: batch.Context.RenditionLabel, Source: string(batch.Context.SourceType),
			})
		case domainplayback.TelemetryEventFirstRenderedFrame:
			if event.FirstFrameMs != nil {
				observation := inframetrics.PlaybackTimingObservation{
					PlaybackDimensions: dimensions,
					MeasurementMethod:  string(event.MeasurementMethod),
					Duration:           time.Duration(*event.FirstFrameMs) * time.Millisecond,
				}
				inframetrics.PlaybackMetrics.ObserveStartup(observation)
				inframetrics.PlaybackMetrics.ObserveFirstFrame(observation)
			}
		case domainplayback.TelemetryEventPlaySuccess:
			inframetrics.PlaybackMetrics.ObserveAttempt(inframetrics.PlaybackAttemptObservation{
				PlaybackDimensions: dimensions,
				Success:            true,
			})
		case domainplayback.TelemetryEventPlayFailure:
			inframetrics.PlaybackMetrics.ObserveAttempt(inframetrics.PlaybackAttemptObservation{
				PlaybackDimensions: dimensions,
				ErrorCategory:      string(event.ErrorCategory),
			})
		case domainplayback.TelemetryEventRebufferEnd:
			if event.IntervalDurationMs != nil {
				inframetrics.PlaybackMetrics.ObserveRebuffer(inframetrics.PlaybackRebufferObservation{
					PlaybackDimensions: dimensions,
					Count:              1,
					Duration:           time.Duration(*event.IntervalDurationMs) * time.Millisecond,
				})
			}
			inframetrics.PlaybackMetrics.ObserveRecovery(inframetrics.PlaybackRecoveryObservation{
				PlaybackDimensions: dimensions,
				Outcome:            string(event.RecoveryOutcome),
			})
		case domainplayback.TelemetryEventSourceChange:
			inframetrics.PlaybackMetrics.ObserveSelection(inframetrics.PlaybackSelectionObservation{
				Scene: batch.Context.Scene, Player: string(batch.Context.PlayerAdapter),
				Source: string(event.SourceType),
			})
		case domainplayback.TelemetryEventQualityChange:
			inframetrics.PlaybackMetrics.ObserveSelection(inframetrics.PlaybackSelectionObservation{
				Scene: batch.Context.Scene, Player: string(batch.Context.PlayerAdapter),
				Quality: event.RenditionLabel,
			})
		case domainplayback.TelemetryEventEnd, domainplayback.TelemetryEventTerminalError:
			if event.EventType == domainplayback.TelemetryEventTerminalError {
				inframetrics.PlaybackMetrics.ObserveAttempt(inframetrics.PlaybackAttemptObservation{
					PlaybackDimensions: dimensions,
					ErrorCategory:      string(event.ErrorCategory),
				})
			}
			if event.RebufferDurationMs != nil && event.MediaPositionMs > 0 {
				inframetrics.PlaybackMetrics.ObserveRebufferSummary(inframetrics.PlaybackRebufferSummaryObservation{
					PlaybackDimensions: dimensions,
					Duration:           time.Duration(*event.RebufferDurationMs) * time.Millisecond,
					PlaybackDuration:   time.Duration(event.MediaPositionMs) * time.Millisecond,
				})
			}
		}
	}
}

func (playbackMetricsAdapter) RecordTelemetryRejection(eventCount int) {
	inframetrics.PlaybackMetrics.ObserveTelemetryIngestion(inframetrics.TelemetryIngestionObservation{
		Result:         "rejected",
		RejectedEvents: eventCount,
	})
}

func (playbackMetricsAdapter) RecordTelemetryCleanup(result *domainplayback.TelemetryCleanupResult) {
	inframetrics.PlaybackMetrics.ObserveTelemetryCleanup(inframetrics.TelemetryCleanupObservation{
		Success: true, DeletedEvents: result.DeletedEvents, DeletedBatches: result.DeletedBatches,
	})
}

func (playbackMetricsAdapter) RecordTelemetryCleanupFailure() {
	inframetrics.PlaybackMetrics.ObserveTelemetryCleanup(inframetrics.TelemetryCleanupObservation{Success: false})
}
