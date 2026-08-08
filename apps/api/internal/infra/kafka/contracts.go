package infrakafka

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"time"
)

type EventType string
type ContractFailureCode string

const (
	EnvelopeVersion1 = 1

	EventTypeBackboneProbe EventType = "frux.platform.backbone_probe"

	ContractMalformedJSON      ContractFailureCode = "malformed_json"
	ContractTrailingData       ContractFailureCode = "trailing_data"
	ContractOversizedRecord    ContractFailureCode = "oversized_record"
	ContractUnknownEvent       ContractFailureCode = "unknown_event"
	ContractUnsupportedVersion ContractFailureCode = "unsupported_version"
	ContractInvalidEnvelope    ContractFailureCode = "invalid_envelope"
	ContractInvalidKey         ContractFailureCode = "invalid_key"
	ContractInvalidPayload     ContractFailureCode = "invalid_payload"
)

var (
	ErrContractFailure = errors.New("kafka contract failure")
	eventIDPattern     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
)

type EventSpec struct {
	Type            EventType
	Topic           TopicID
	SchemaVersions  []int
	KeyKind         KeyKind
	MaxPayloadBytes int
	NewPayload      func(version int) (any, bool)
}

type EventMetadata struct {
	EventID       string
	Type          EventType
	SchemaVersion int
	OccurredAt    time.Time
	ProducedAt    time.Time
	Producer      ProducerID
	CorrelationID string
}

type Envelope struct {
	EnvelopeVersion int             `json:"envelope_version"`
	EventID         string          `json:"event_id"`
	EventType       EventType       `json:"event_type"`
	SchemaVersion   int             `json:"schema_version"`
	OccurredAt      time.Time       `json:"occurred_at"`
	ProducedAt      time.Time       `json:"produced_at"`
	Producer        ProducerID      `json:"producer"`
	CorrelationID   string          `json:"correlation_id,omitempty"`
	Payload         json.RawMessage `json:"payload"`
}

type DecodedEvent struct {
	Envelope Envelope
	Payload  any
}

type BackboneProbePayload struct {
	ProbeID string `json:"probe_id"`
	Source  string `json:"source"`
}

type ContractError struct {
	Code ContractFailureCode
	Err  error
}

func (e *ContractError) Error() string {
	if e == nil {
		return ErrContractFailure.Error()
	}
	if e.Err == nil {
		return fmt.Sprintf("%s: %s", ErrContractFailure, e.Code)
	}
	return fmt.Sprintf("%s: %s: %v", ErrContractFailure, e.Code, e.Err)
}

func (e *ContractError) Unwrap() error {
	return ErrContractFailure
}

func (e *ContractError) Terminal() bool {
	return true
}

var events = [...]EventSpec{
	{
		Type: EventTypeBackboneProbe, Topic: TopicBackboneProbe, SchemaVersions: []int{1},
		KeyKind: KeyKindProbeID, MaxPayloadBytes: 4 << 10,
		NewPayload: func(version int) (any, bool) {
			if version != 1 {
				return nil, false
			}
			return &BackboneProbePayload{}, true
		},
	},
}

func Events() []EventSpec {
	return append([]EventSpec(nil), events[:]...)
}

func Event(eventType EventType) (EventSpec, error) {
	for _, spec := range events {
		if spec.Type == eventType {
			return spec, nil
		}
	}
	return EventSpec{}, contractError(ContractUnknownEvent, nil)
}

func EncodeEvent(topicID TopicID, key []byte, metadata EventMetadata, payload any) ([]byte, error) {
	spec, err := validateMetadata(topicID, key, metadata, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return nil, contractError(ContractInvalidPayload, err)
	}
	if len(payloadJSON) == 0 || len(payloadJSON) > spec.MaxPayloadBytes ||
		bytes.Equal(payloadJSON, []byte("null")) {
		return nil, contractError(ContractInvalidPayload, nil)
	}
	typedPayload, supported := spec.NewPayload(metadata.SchemaVersion)
	if !supported {
		return nil, contractError(ContractUnsupportedVersion, nil)
	}
	if err := decodeStrict(payloadJSON, typedPayload); err != nil {
		return nil, contractError(ContractInvalidPayload, err)
	}
	envelope := Envelope{
		EnvelopeVersion: EnvelopeVersion1,
		EventID:         metadata.EventID, EventType: metadata.Type,
		SchemaVersion: metadata.SchemaVersion,
		OccurredAt:    metadata.OccurredAt.UTC(), ProducedAt: metadata.ProducedAt.UTC(),
		Producer: metadata.Producer, CorrelationID: metadata.CorrelationID,
		Payload: payloadJSON,
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		return nil, contractError(ContractInvalidEnvelope, err)
	}
	topic, err := Topic(topicID)
	if err != nil {
		return nil, err
	}
	if len(encoded) > topic.MaxRecordBytes {
		return nil, contractError(ContractOversizedRecord, nil)
	}
	return encoded, nil
}

func DecodeEvent(topicID TopicID, key, record []byte, now time.Time) (DecodedEvent, error) {
	topic, err := Topic(topicID)
	if err != nil {
		return DecodedEvent{}, err
	}
	if len(record) == 0 {
		return DecodedEvent{}, contractError(ContractMalformedJSON, io.ErrUnexpectedEOF)
	}
	if len(record) > topic.MaxRecordBytes {
		return DecodedEvent{}, contractError(ContractOversizedRecord, nil)
	}
	var envelope Envelope
	if err := decodeStrict(record, &envelope); err != nil {
		var syntax *json.SyntaxError
		if errors.As(err, &syntax) || errors.Is(err, io.ErrUnexpectedEOF) {
			return DecodedEvent{}, contractError(ContractMalformedJSON, err)
		}
		if errors.Is(err, errTrailingJSON) {
			return DecodedEvent{}, contractError(ContractTrailingData, err)
		}
		return DecodedEvent{}, contractError(ContractInvalidEnvelope, err)
	}
	if envelope.EnvelopeVersion != EnvelopeVersion1 {
		return DecodedEvent{}, contractError(ContractUnsupportedVersion, nil)
	}
	metadata := EventMetadata{
		EventID: envelope.EventID, Type: envelope.EventType,
		SchemaVersion: envelope.SchemaVersion,
		OccurredAt:    envelope.OccurredAt, ProducedAt: envelope.ProducedAt,
		Producer: envelope.Producer, CorrelationID: envelope.CorrelationID,
	}
	spec, err := validateMetadata(topicID, key, metadata, now.UTC())
	if err != nil {
		return DecodedEvent{}, err
	}
	if len(envelope.Payload) == 0 || len(envelope.Payload) > spec.MaxPayloadBytes ||
		bytes.Equal(envelope.Payload, []byte("null")) {
		return DecodedEvent{}, contractError(ContractInvalidPayload, nil)
	}
	payload, supported := spec.NewPayload(envelope.SchemaVersion)
	if !supported {
		return DecodedEvent{}, contractError(ContractUnsupportedVersion, nil)
	}
	if err := decodeStrict(envelope.Payload, payload); err != nil {
		return DecodedEvent{}, contractError(ContractInvalidPayload, err)
	}
	return DecodedEvent{Envelope: envelope, Payload: payload}, nil
}

func validateMetadata(
	topicID TopicID,
	key []byte,
	metadata EventMetadata,
	now time.Time,
) (EventSpec, error) {
	spec, err := Event(metadata.Type)
	if err != nil {
		return EventSpec{}, err
	}
	if spec.Topic != topicID {
		return EventSpec{}, contractError(ContractUnknownEvent, nil)
	}
	if metadata.SchemaVersion < 1 || !containsInt(spec.SchemaVersions, metadata.SchemaVersion) {
		return EventSpec{}, contractError(ContractUnsupportedVersion, nil)
	}
	if !eventIDPattern.MatchString(metadata.EventID) ||
		(metadata.CorrelationID != "" && !eventIDPattern.MatchString(metadata.CorrelationID)) ||
		metadata.OccurredAt.IsZero() || metadata.ProducedAt.IsZero() ||
		metadata.OccurredAt.After(metadata.ProducedAt.Add(5*time.Minute)) ||
		metadata.ProducedAt.After(now.Add(5*time.Minute)) ||
		metadata.ProducedAt.Before(metadata.OccurredAt.Add(-365*24*time.Hour)) ||
		!producerAllowed(topicID, metadata.Producer) {
		return EventSpec{}, contractError(ContractInvalidEnvelope, nil)
	}
	if err := ValidateKey(spec.KeyKind, key); err != nil {
		return EventSpec{}, contractError(ContractInvalidKey, err)
	}
	return spec, nil
}

var errTrailingJSON = errors.New("trailing JSON data")

func decodeStrict(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errTrailingJSON
		}
		return err
	}
	return nil
}

func contractError(code ContractFailureCode, err error) error {
	return &ContractError{Code: code, Err: err}
}

func containsInt(values []int, target int) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func producerAllowed(topicID TopicID, producer ProducerID) bool {
	topic, err := Topic(topicID)
	if err != nil {
		return false
	}
	for _, allowed := range topic.AllowedProducers {
		if allowed == producer {
			return true
		}
	}
	return false
}
