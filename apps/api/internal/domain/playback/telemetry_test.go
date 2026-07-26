package domainplayback

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestNewTelemetryBatchNormalizesVersionedContract(t *testing.T) {
	firstFrameMs := int64(180)
	mediaDurationMs := int64(60_000)
	batch, err := NewTelemetryBatch(NewTelemetryBatchInput{
		UserID:            42,
		SchemaVersion:     TelemetrySchemaVersionV1,
		BatchID:           " batch-1 ",
		PlaybackSessionID: " playback-1 ",
		ClientSentAt:      time.Date(2026, 7, 26, 12, 0, 0, 123456789, time.FixedZone("test", 8*60*60)),
		Context: TelemetryContext{
			VideoID:       7,
			Scene:         " Recommend ",
			RequestID:     " req-1 ",
			PlayerAdapter: " NATIVE_MP4 ",
			SourceType:    " MP4 ",
			CodecFamily:   " H264 ",
			NetworkClass:  " WIFI ",
			BrowserFamily: " Chrome ",
			BrowserMajor:  126,
			OSFamily:      " Linux ",
			ViewportClass: " LARGE ",
			CDNHost:       " CDN.EXAMPLE.COM ",
		},
		Events: []NewTelemetryEventInput{
			{EventID: " load-1 ", EventType: " LOAD_START ", OffsetMs: 0, MediaDurationMs: &mediaDurationMs},
			{
				EventID: "frame-1", EventType: TelemetryEventFirstRenderedFrame, OffsetMs: 180,
				MediaDurationMs: &mediaDurationMs, FirstFrameMs: &firstFrameMs,
				MeasurementMethod: TelemetryMeasurementVideoFrameCallback,
			},
		},
	})
	if err != nil {
		t.Fatalf("new telemetry batch: %v", err)
	}
	if batch.BatchID != "batch-1" || batch.PlaybackSessionID != "playback-1" {
		t.Fatalf("batch ids were not normalized: %+v", batch)
	}
	if batch.Context.Scene != "recommend" ||
		batch.Context.PlayerAdapter != TelemetryPlayerAdapterNativeMP4 ||
		batch.Context.NetworkClass != TelemetryNetworkWiFi ||
		batch.Context.CDNHost != "cdn.example.com" {
		t.Fatalf("context was not normalized: %+v", batch.Context)
	}
	if batch.ClientSentAt.Location() != time.UTC || batch.ClientSentAt.Nanosecond() != 123456000 {
		t.Fatalf("client sent time was not normalized: %s", batch.ClientSentAt)
	}
	if batch.Events[0].EventID != "load-1" || batch.Events[1].MeasurementMethod != TelemetryMeasurementVideoFrameCallback {
		t.Fatalf("events were not normalized: %+v", batch.Events)
	}
}

func TestNewTelemetryBatchRejectsContractViolations(t *testing.T) {
	valid := func() NewTelemetryBatchInput {
		return NewTelemetryBatchInput{
			UserID: 1, SchemaVersion: TelemetrySchemaVersionV1,
			BatchID: "batch-1", PlaybackSessionID: "playback-1", ClientSentAt: time.Now(),
			Context: TelemetryContext{
				VideoID: 1, Scene: "recommend", PlayerAdapter: TelemetryPlayerAdapterNativeMP4,
				SourceType: TelemetrySourceMP4,
			},
			Events: []NewTelemetryEventInput{{EventID: "event-1", EventType: TelemetryEventLoadStart}},
		}
	}

	tests := []struct {
		name   string
		mutate func(*NewTelemetryBatchInput)
		target error
	}{
		{
			name: "unsupported version",
			mutate: func(input *NewTelemetryBatchInput) {
				input.SchemaVersion = 2
			},
			target: ErrUnsupportedTelemetryVersion,
		},
		{
			name: "anonymous without server session",
			mutate: func(input *NewTelemetryBatchInput) {
				input.UserID = 0
			},
			target: ErrInvalidTelemetryReporter,
		},
		{
			name: "too many events",
			mutate: func(input *NewTelemetryBatchInput) {
				input.Events = make([]NewTelemetryEventInput, MaxTelemetryEventsPerBatch+1)
			},
			target: ErrInvalidTelemetryEventCount,
		},
		{
			name: "duplicate event id",
			mutate: func(input *NewTelemetryBatchInput) {
				input.Events = append(input.Events, input.Events[0])
			},
			target: ErrDuplicateTelemetryEventID,
		},
		{
			name: "events out of order",
			mutate: func(input *NewTelemetryBatchInput) {
				input.Events = []NewTelemetryEventInput{
					{EventID: "event-1", EventType: TelemetryEventLoadStart, OffsetMs: 10},
					{EventID: "event-2", EventType: TelemetryEventMetadataReady, OffsetMs: 9},
				}
			},
			target: ErrTelemetryEventsOutOfOrder,
		},
		{
			name: "invalid dimension",
			mutate: func(input *NewTelemetryBatchInput) {
				input.Context.CDNHost = "https://cdn.example.com/video.mp4?token=secret"
			},
			target: ErrInvalidTelemetryDimension,
		},
		{
			name: "oversized scene",
			mutate: func(input *NewTelemetryBatchInput) {
				input.Context.Scene = strings.Repeat("x", MaxTelemetrySceneLength+1)
			},
			target: ErrTelemetryStringTooLong,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := valid()
			test.mutate(&input)
			_, err := NewTelemetryBatch(input)
			if !errors.Is(err, test.target) {
				t.Fatalf("expected %v, got %v", test.target, err)
			}
		})
	}
}

func TestTelemetryDimensionNormalizationIsBounded(t *testing.T) {
	if got := NormalizeTelemetryCodecFamily("avc1.640028"); got != TelemetryCodecH264 {
		t.Fatalf("unexpected codec normalization: %s", got)
	}
	if got := NormalizeTelemetryBrowserFamily("Unknown Browser Build"); got != TelemetryBrowserOther {
		t.Fatalf("unexpected browser normalization: %s", got)
	}
	if got := NormalizeTelemetryRenditionLabel("987p-custom"); got != "other" {
		t.Fatalf("unexpected rendition normalization: %s", got)
	}
	host, err := NormalizeTelemetryCDNHost(" CDN.Example.COM. ")
	if err != nil || host != "cdn.example.com" {
		t.Fatalf("unexpected CDN host normalization: host=%q err=%v", host, err)
	}
}

func TestNewTelemetryBatchValidatesTypedEventFields(t *testing.T) {
	input := NewTelemetryBatchInput{
		UserID: 1, SchemaVersion: TelemetrySchemaVersionV1,
		BatchID: "batch-1", PlaybackSessionID: "playback-1", ClientSentAt: time.Now(),
		Context: TelemetryContext{
			VideoID: 1, Scene: "recommend", PlayerAdapter: TelemetryPlayerAdapterNativeMP4,
			SourceType: TelemetrySourceMP4,
		},
		Events: []NewTelemetryEventInput{{
			EventID: "event-1", EventType: TelemetryEventFirstRenderedFrame,
		}},
	}

	_, err := NewTelemetryBatch(input)
	if !errors.Is(err, ErrMissingTelemetryField) {
		t.Fatalf("expected typed field validation, got %v", err)
	}

	rebufferDurationMs := int64(250)
	input.Events = []NewTelemetryEventInput{{
		EventID: "event-2", EventType: TelemetryEventRebufferEnd,
		IntervalDurationMs: &rebufferDurationMs, RecoveryOutcome: TelemetryRecoveryResumed,
	}}
	if _, err := NewTelemetryBatch(input); err != nil {
		t.Fatalf("valid rebuffer event was rejected: %v", err)
	}

	rebufferCount := 1
	input.Events = []NewTelemetryEventInput{{
		EventID: "event-3", EventType: TelemetryEventLoadStart,
		RebufferCount: &rebufferCount,
	}}
	if _, err := NewTelemetryBatch(input); !errors.Is(err, ErrUnexpectedTelemetryField) {
		t.Fatalf("expected non-terminal rebuffer summary rejection, got %v", err)
	}

	input.Events = []NewTelemetryEventInput{{
		EventID: "event-4", EventType: TelemetryEventEnd,
		RebufferCount: &rebufferCount,
	}}
	if _, err := NewTelemetryBatch(input); !errors.Is(err, ErrMissingTelemetryField) {
		t.Fatalf("expected partial rebuffer summary rejection, got %v", err)
	}
}
