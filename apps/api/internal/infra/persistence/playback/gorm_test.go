package infraplayback

import (
	domainplayback "GCFeed/internal/domain/playback"
	"testing"
	"time"
)

func TestTelemetryEventPayloadHashIgnoresBatchDeliveryEnvelope(t *testing.T) {
	first := validTelemetryBatch(t)
	second := validTelemetryBatch(t)
	second.BatchID = "batch-2"
	second.ClientSentAt = first.ClientSentAt.Add(time.Minute)

	firstHash, err := telemetryEventPayloadHash(first, first.Events[0])
	if err != nil {
		t.Fatalf("hash first event: %v", err)
	}
	secondHash, err := telemetryEventPayloadHash(second, second.Events[0])
	if err != nil {
		t.Fatalf("hash second event: %v", err)
	}
	if firstHash != secondHash {
		t.Fatalf("event retry hash changed with delivery envelope: %s != %s", firstHash, secondHash)
	}
}

func TestTelemetryBatchPayloadHashIgnoresRetryDeliveryTime(t *testing.T) {
	first := validTelemetryBatch(t)
	second := validTelemetryBatch(t)
	second.ClientSentAt = first.ClientSentAt.Add(time.Minute)

	firstHash, err := telemetryBatchPayloadHash(first)
	if err != nil {
		t.Fatalf("hash first batch: %v", err)
	}
	secondHash, err := telemetryBatchPayloadHash(second)
	if err != nil {
		t.Fatalf("hash second batch: %v", err)
	}
	if firstHash != secondHash {
		t.Fatalf("batch retry hash changed with delivery time: %s != %s", firstHash, secondHash)
	}
}

func TestTelemetryEventModelAppliesSourceOverrides(t *testing.T) {
	batch := validTelemetryBatch(t)
	event := batch.Events[0]
	event.SourceType = domainplayback.TelemetrySourceDASH
	event.RenditionLabel = "1080p"
	event.CodecFamily = domainplayback.TelemetryCodecAV1
	event.CDNHost = "edge.example.com"

	model := telemetryEventModelFromDomain(9, batch, event, "hash")
	if model.BatchRecordID != 9 ||
		model.SourceType != string(domainplayback.TelemetrySourceDASH) ||
		model.RenditionLabel != "1080p" ||
		model.CodecFamily != string(domainplayback.TelemetryCodecAV1) ||
		model.CDNHost == nil ||
		*model.CDNHost != "edge.example.com" {
		t.Fatalf("event source overrides were not persisted: %+v", model)
	}
}

func validTelemetryBatch(t *testing.T) *domainplayback.TelemetryBatch {
	t.Helper()
	batch, err := domainplayback.NewTelemetryBatch(domainplayback.NewTelemetryBatchInput{
		UserID: 1, SchemaVersion: domainplayback.TelemetrySchemaVersionV1,
		BatchID: "batch-1", PlaybackSessionID: "playback-1", ClientSentAt: time.Now().UTC(),
		Context: domainplayback.TelemetryContext{
			VideoID: 1, Scene: "recommend",
			PlayerAdapter:  domainplayback.TelemetryPlayerAdapterNativeMP4,
			SourceType:     domainplayback.TelemetrySourceMP4,
			RenditionLabel: "720p",
			CodecFamily:    domainplayback.TelemetryCodecH264,
			CDNHost:        "cdn.example.com",
		},
		Events: []domainplayback.NewTelemetryEventInput{{
			EventID: "event-1", EventType: domainplayback.TelemetryEventLoadStart,
		}},
	})
	if err != nil {
		t.Fatalf("new telemetry batch: %v", err)
	}
	return batch
}
