package infrakafka

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestProbeKeyCodecStableFixture(t *testing.T) {
	key, err := EncodeKey(KeyKindProbeID, ProbeKey{ProbeID: "startup-01"})
	if err != nil {
		t.Fatal(err)
	}
	fixture := []byte("probe:startup-01")
	if !bytes.Equal(key, fixture) {
		t.Fatalf("key = %q, want %q", key, fixture)
	}
	decoded, err := DecodeKey(KeyKindProbeID, fixture)
	if err != nil {
		t.Fatal(err)
	}
	if decoded != (ProbeKey{ProbeID: "startup-01"}) {
		t.Fatalf("decoded key = %#v", decoded)
	}
}

func TestEnvelopeVersionOneRoundTrip(t *testing.T) {
	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	key, _ := EncodeKey(KeyKindProbeID, ProbeKey{ProbeID: "probe-1"})
	encoded, err := EncodeEvent(TopicBackboneProbe, key, EventMetadata{
		EventID: "event-1", Type: EventTypeBackboneProbe, SchemaVersion: 1,
		OccurredAt: now.Add(-time.Second), ProducedAt: now,
		Producer: ProducerPlatformWorker, CorrelationID: "request-1",
	}, BackboneProbePayload{ProbeID: "probe-1", Source: "integration"})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeEvent(TopicBackboneProbe, key, encoded, now)
	if err != nil {
		t.Fatal(err)
	}
	payload, ok := decoded.Payload.(*BackboneProbePayload)
	if !ok || payload.ProbeID != "probe-1" || payload.Source != "integration" {
		t.Fatalf("payload = %#v", decoded.Payload)
	}
}

func TestEnvelopeRejectsTerminalContractFailures(t *testing.T) {
	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	key := []byte("probe:probe-1")
	valid := `{"envelope_version":1,"event_id":"event-1","event_type":"frux.platform.backbone_probe","schema_version":1,"occurred_at":"2026-08-08T11:59:59Z","produced_at":"2026-08-08T12:00:00Z","producer":"platform_worker","payload":{"probe_id":"probe-1","source":"test"}}`
	tests := []struct {
		name string
		key  []byte
		body string
		code ContractFailureCode
	}{
		{name: "unknown event", key: key, body: strings.Replace(valid, "frux.platform.backbone_probe", "frux.unknown", 1), code: ContractUnknownEvent},
		{name: "unsupported envelope", key: key, body: strings.Replace(valid, `"envelope_version":1`, `"envelope_version":2`, 1), code: ContractUnsupportedVersion},
		{name: "unsupported schema", key: key, body: strings.Replace(valid, `"schema_version":1`, `"schema_version":2`, 1), code: ContractUnsupportedVersion},
		{name: "malformed", key: key, body: "{", code: ContractMalformedJSON},
		{name: "trailing", key: key, body: valid + `{}`, code: ContractTrailingData},
		{name: "unknown envelope field", key: key, body: strings.Replace(valid, `"payload":`, `"extra":true,"payload":`, 1), code: ContractInvalidEnvelope},
		{name: "unknown payload field", key: key, body: strings.Replace(valid, `"source":"test"`, `"source":"test","extra":true`, 1), code: ContractInvalidPayload},
		{name: "invalid key", key: []byte("user:1"), body: valid, code: ContractInvalidKey},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := DecodeEvent(TopicBackboneProbe, test.key, []byte(test.body), now)
			var contract *ContractError
			if !errors.As(err, &contract) || contract.Code != test.code || !contract.Terminal() {
				t.Fatalf("error = %#v, want terminal %s", err, test.code)
			}
		})
	}
}

func TestEnvelopeRejectsOversizedAndInvalidMetadata(t *testing.T) {
	key := []byte("probe:probe-1")
	topic, _ := Topic(TopicBackboneProbe)
	_, err := DecodeEvent(TopicBackboneProbe, key, bytes.Repeat([]byte("x"), topic.MaxRecordBytes+1), time.Now())
	var contract *ContractError
	if !errors.As(err, &contract) || contract.Code != ContractOversizedRecord {
		t.Fatalf("oversized error = %v", err)
	}
	_, err = EncodeEvent(TopicBackboneProbe, key, EventMetadata{
		EventID: "bad id!", Type: EventTypeBackboneProbe, SchemaVersion: 1,
		OccurredAt: time.Now(), ProducedAt: time.Now(), Producer: ProducerPlatformAPI,
	}, BackboneProbePayload{ProbeID: "probe-1", Source: "test"})
	if !errors.As(err, &contract) || contract.Code != ContractInvalidEnvelope {
		t.Fatalf("metadata error = %v", err)
	}
}
