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

func TestBehaviorKeyCodecsStableFixtures(t *testing.T) {
	action, err := EncodeKey(KeyKindActionState, ActionStateKey{
		UserID: 42, VideoID: 99, ActionType: "LIKE",
	})
	if err != nil {
		t.Fatal(err)
	}

	if string(action) != "action:42:99:LIKE" {
		t.Fatalf("action key = %q", action)
	}
	user, err := EncodeKey(KeyKindUserID, UserKey{UserID: 42})
	if err != nil {
		t.Fatal(err)
	}
	if string(user) != "user:42" {
		t.Fatalf("user key = %q", user)
	}
}

func TestBehaviorKeyCodecsRejectNonCanonicalAliases(t *testing.T) {
	tests := []struct {
		name string
		kind KeyKind
		key  string
	}{
		{name: "action leading user zero", kind: KeyKindActionState, key: "action:042:99:LIKE"},
		{name: "action leading video zero", kind: KeyKindActionState, key: "action:42:099:LIKE"},
		{name: "action lowercase type", kind: KeyKindActionState, key: "action:42:99:like"},
		{name: "action signed user", kind: KeyKindActionState, key: "action:+42:99:LIKE"},
		{name: "user leading zero", kind: KeyKindUserID, key: "user:042"},
		{name: "user signed", kind: KeyKindUserID, key: "user:+42"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := DecodeKey(test.kind, []byte(test.key)); !errors.Is(err, ErrInvalidEventKey) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestBehaviorEnvelopeRoundTripsExistingPayloads(t *testing.T) {
	now := time.Date(2026, 8, 9, 2, 0, 0, 0, time.UTC)
	actionKey := []byte("action:42:99:LIKE")
	actionPayload := ActionChangedPayload{
		EventID: "action-event-1", UserID: 42, VideoID: 99, ActionType: "LIKE",
		Active: true, IdempotencyKey: "like-1", Version: 7, OccurredAt: now.Add(-time.Second),
	}
	actionRecord, err := EncodeEvent(TopicActionChanged, actionKey, EventMetadata{
		EventID: actionPayload.EventID, Type: EventTypeActionChanged, SchemaVersion: 1,
		OccurredAt: actionPayload.OccurredAt, ProducedAt: now, Producer: ProducerInteractionAPI,
	}, actionPayload)
	if err != nil {
		t.Fatal(err)
	}
	decodedAction, err := DecodeEvent(TopicActionChanged, actionKey, actionRecord, now)
	if err != nil {
		t.Fatal(err)
	}
	if decodedAction.Payload.(*ActionChangedPayload).Version != 7 {
		t.Fatalf("action payload = %#v", decodedAction.Payload)
	}

	viewKey := []byte("user:42")
	duration := 30_000
	viewPayload := ViewEventRecordedPayload{
		EventID: "view-event-1", ViewEventID: 101, UserID: 42, VideoID: 99,
		Scene: "recommend", EventType: "progress", PositionMs: 10_000, WatchMs: 9_000,
		DurationMs: &duration, RecordedAt: now, OccurredAt: now.Add(-2 * time.Second),
	}
	viewRecord, err := EncodeEvent(TopicViewEventRecorded, viewKey, EventMetadata{
		EventID: viewPayload.EventID, Type: EventTypeViewEventRecorded, SchemaVersion: 1,
		OccurredAt: viewPayload.OccurredAt, ProducedAt: now, Producer: ProducerExposureWorker,
	}, viewPayload)
	if err != nil {
		t.Fatal(err)
	}
	decodedView, err := DecodeEvent(TopicViewEventRecorded, viewKey, viewRecord, now)
	if err != nil {
		t.Fatal(err)
	}
	if decodedView.Payload.(*ViewEventRecordedPayload).ViewEventID != 101 {
		t.Fatalf("view payload = %#v", decodedView.Payload)
	}
}

func TestBehaviorContractsRejectMalformedIdentityVersionKeySizeAndTime(t *testing.T) {
	now := time.Date(2026, 8, 9, 2, 0, 0, 0, time.UTC)
	payload := ActionChangedPayload{
		EventID: "action-event-1", UserID: 42, VideoID: 99, ActionType: "LIKE",
		Active: true, Version: 1, OccurredAt: now.Add(-time.Second),
	}
	base := EventMetadata{
		EventID: payload.EventID, Type: EventTypeActionChanged, SchemaVersion: 1,
		OccurredAt: payload.OccurredAt, ProducedAt: now, Producer: ProducerInteractionAPI,
	}
	tests := []struct {
		name     string
		key      []byte
		metadata EventMetadata
		payload  ActionChangedPayload
		code     ContractFailureCode
	}{
		{name: "untrimmed id", key: []byte("action:42:99:LIKE"), metadata: func() EventMetadata {
			value := base
			value.EventID = " bad id! "
			return value
		}(), payload: payload, code: ContractInvalidEnvelope},
		{name: "unsupported version", key: []byte("action:42:99:LIKE"), metadata: func() EventMetadata {
			value := base
			value.SchemaVersion = 2
			return value
		}(), payload: payload, code: ContractUnsupportedVersion},
		{name: "key mismatch", key: []byte("action:43:99:LIKE"), metadata: base, payload: payload, code: ContractInvalidPayload},
		{name: "key alias lowercase", key: []byte("action:42:99:like"), metadata: base, payload: payload, code: ContractInvalidKey},
		{name: "key alias leading zero", key: []byte("action:042:99:LIKE"), metadata: base, payload: payload, code: ContractInvalidKey},
		{name: "payload id mismatch", key: []byte("action:42:99:LIKE"), metadata: base, payload: func() ActionChangedPayload {
			value := payload
			value.EventID = "other-event"
			return value
		}(), code: ContractInvalidPayload},
		{name: "future occurrence", key: []byte("action:42:99:LIKE"), metadata: func() EventMetadata {
			value := base
			value.OccurredAt = now.Add(6 * time.Minute)
			return value
		}(), payload: payload, code: ContractInvalidEnvelope},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := EncodeEvent(TopicActionChanged, test.key, test.metadata, test.payload)
			var contract *ContractError
			if !errors.As(err, &contract) || contract.Code != test.code {
				t.Fatalf("error = %v, want %s", err, test.code)
			}
		})
	}
	topic, _ := Topic(TopicActionChanged)
	_, err := DecodeEvent(
		TopicActionChanged,
		[]byte("action:42:99:LIKE"),
		bytes.Repeat([]byte("x"), topic.MaxRecordBytes+1),
		now,
	)
	var contract *ContractError
	if !errors.As(err, &contract) || contract.Code != ContractOversizedRecord {
		t.Fatalf("oversized error = %v", err)
	}
}

func TestViewContractRejectsNonCanonicalUserKey(t *testing.T) {
	now := time.Date(2026, 8, 9, 2, 0, 0, 0, time.UTC)
	payload := ViewEventRecordedPayload{
		EventID: "view-event-alias", ViewEventID: 101, UserID: 42, VideoID: 99,
		Scene: "recommend", EventType: "play", RecordedAt: now, OccurredAt: now,
	}
	_, err := EncodeEvent(TopicViewEventRecorded, []byte("user:042"), EventMetadata{
		EventID: payload.EventID, Type: EventTypeViewEventRecorded, SchemaVersion: 1,
		OccurredAt: now, ProducedAt: now, Producer: ProducerExposureWorker,
	}, payload)
	var contract *ContractError
	if !errors.As(err, &contract) || contract.Code != ContractInvalidKey {
		t.Fatalf("error=%v", err)
	}
}

func TestViewContractAcceptsDomainCompatibleEventIDCharacters(t *testing.T) {
	now := time.Now().UTC()
	payload := ViewEventRecordedPayload{
		EventID: "playback/123 with punctuation!", ViewEventID: 101,
		UserID: 42, VideoID: 99, Scene: "recommend", EventType: "play",
		RecordedAt: now, OccurredAt: now,
	}
	key, err := EncodeKey(KeyKindUserID, UserKey{UserID: payload.UserID})
	if err != nil {
		t.Fatal(err)
	}
	_, err = EncodeEvent(TopicViewEventRecorded, key, EventMetadata{
		EventID: payload.EventID, Type: EventTypeViewEventRecorded, SchemaVersion: 1,
		OccurredAt: now, ProducedAt: now, Producer: ProducerExposureWorker,
	}, payload)
	if err != nil {
		t.Fatalf("domain-compatible event ID rejected: %v", err)
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
		EventID: " bad id! ", Type: EventTypeBackboneProbe, SchemaVersion: 1,
		OccurredAt: time.Now(), ProducedAt: time.Now(), Producer: ProducerPlatformAPI,
	}, BackboneProbePayload{ProbeID: "probe-1", Source: "test"})
	if !errors.As(err, &contract) || contract.Code != ContractInvalidEnvelope {
		t.Fatalf("metadata error = %v", err)
	}
}
