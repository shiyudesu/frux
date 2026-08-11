package infrakafka

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	applicationeventstream "github.com/shiyudesu/frux/internal/application/eventstream"
)

const (
	RecoveryHeaderKey           = "frux.recovery.v1"
	RecoveryQuarantineHeaderKey = "frux.recovery.quarantine.v1"
	MaxRecoveryHeaderBytes      = 4 << 10
	MaxRecoveryHeaders          = 16
	MaxRecoveryTotalHeaderBytes = 8 << 10
)

var ErrRecoveryMetadata = errors.New("invalid kafka recovery metadata")

type RecoveryMetadataCode string

const (
	RecoveryMetadataMissing          RecoveryMetadataCode = "missing"
	RecoveryMetadataUnknownHeader    RecoveryMetadataCode = "unknown_header"
	RecoveryMetadataOversized        RecoveryMetadataCode = "oversized"
	RecoveryMetadataInvalidSource    RecoveryMetadataCode = "invalid_source"
	RecoveryMetadataUnsupportedGroup RecoveryMetadataCode = "unsupported_group"
	RecoveryMetadataInvalidAttempt   RecoveryMetadataCode = "invalid_attempt"
	RecoveryMetadataHashMismatch     RecoveryMetadataCode = "hash_mismatch"
	RecoveryMetadataInvalidTopic     RecoveryMetadataCode = "invalid_topic"
	RecoveryMetadataInvalid          RecoveryMetadataCode = "invalid"
)

type RecoveryMetadataError struct {
	Code RecoveryMetadataCode
}

func (e *RecoveryMetadataError) Error() string {
	if e == nil {
		return ErrRecoveryMetadata.Error()
	}
	return fmt.Sprintf("%s: %s", ErrRecoveryMetadata, e.Code)
}

func (e *RecoveryMetadataError) Unwrap() error {
	return ErrRecoveryMetadata
}

type RecoveryMetadata struct {
	SourceTopic       string               `json:"source_topic"`
	SourcePartition   int32                `json:"source_partition"`
	SourceOffset      int64                `json:"source_offset"`
	EventID           string               `json:"event_id"`
	SchemaVersion     int                  `json:"schema_version"`
	ConsumerGroup     ConsumerGroupID      `json:"consumer_group"`
	Attempt           int                  `json:"attempt"`
	Tier              int                  `json:"tier"`
	FailureClass      FailureClass         `json:"failure_class"`
	FirstFailureAt    time.Time            `json:"first_failure_at"`
	LatestFailureAt   time.Time            `json:"latest_failure_at"`
	NotBefore         time.Time            `json:"not_before"`
	PayloadSHA256     string               `json:"payload_sha256"`
	ReplayID          string               `json:"replay_id,omitempty"`
	ConsumedTopic     string               `json:"consumed_topic,omitempty"`
	ConsumedPartition int32                `json:"consumed_partition,omitempty"`
	ConsumedOffset    int64                `json:"consumed_offset,omitempty"`
	KeySHA256         string               `json:"key_sha256,omitempty"`
	MetadataCode      RecoveryMetadataCode `json:"metadata_code,omitempty"`
	NonReplayable     bool                 `json:"non_replayable,omitempty"`
}

type RecoveryQuarantineMetadata struct {
	ConsumedTopic     string               `json:"consumed_topic"`
	ConsumedPartition int32                `json:"consumed_partition"`
	ConsumedOffset    int64                `json:"consumed_offset"`
	ConsumerGroup     ConsumerGroupID      `json:"consumer_group"`
	FailureClass      FailureClass         `json:"failure_class"`
	MetadataCode      RecoveryMetadataCode `json:"metadata_code"`
	QuarantinedAt     time.Time            `json:"quarantined_at"`
	PayloadSHA256     string               `json:"payload_sha256"`
	KeySHA256         string               `json:"key_sha256"`
	NonReplayable     bool                 `json:"non_replayable"`
}

func EncodeRecoveryHeaders(
	prefix string,
	destination TopicID,
	metadata RecoveryMetadata,
	key, value []byte,
) ([]applicationeventstream.Header, error) {
	return encodeRecoveryHeaders(prefix, destination, metadata, key, value, false)
}

func encodeTerminalContractDLQHeaders(
	prefix string,
	destination TopicID,
	metadata RecoveryMetadata,
	key, value []byte,
) ([]applicationeventstream.Header, error) {
	return encodeRecoveryHeaders(prefix, destination, metadata, key, value, true)
}

func encodeRecoveryHeaders(
	prefix string,
	destination TopicID,
	metadata RecoveryMetadata,
	key, value []byte,
	allowTerminalContractInvalidKey bool,
) ([]applicationeventstream.Header, error) {
	if err := validateRecoveryMetadata(
		prefix,
		destination,
		metadata,
		key,
		value,
		allowTerminalContractInvalidKey,
	); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(metadata)
	if err != nil || len(encoded) == 0 || len(encoded) > MaxRecoveryHeaderBytes {
		return nil, recoveryMetadataError(RecoveryMetadataOversized)
	}
	return []applicationeventstream.Header{{
		Key: RecoveryHeaderKey, Value: encoded,
	}}, nil
}

func DecodeRecoveryHeaders(
	prefix string,
	currentTopic TopicID,
	headers []applicationeventstream.Header,
	key, value []byte,
) (RecoveryMetadata, error) {
	var encoded []byte
	totalBytes := 0
	for _, header := range headers {
		totalBytes += len(header.Key) + len(header.Value)
		if len(headers) > MaxRecoveryHeaders || totalBytes > MaxRecoveryTotalHeaderBytes {
			return RecoveryMetadata{}, recoveryMetadataError(RecoveryMetadataOversized)
		}
		if header.Key == RecoveryHeaderKey {
			if encoded != nil {
				return RecoveryMetadata{}, recoveryMetadataError(RecoveryMetadataInvalid)
			}
			encoded = header.Value
			continue
		}
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(header.Key)), "frux.recovery") {
			return RecoveryMetadata{}, recoveryMetadataError(RecoveryMetadataUnknownHeader)
		}
	}
	if len(encoded) == 0 {
		return RecoveryMetadata{}, recoveryMetadataError(RecoveryMetadataMissing)
	}
	if len(encoded) > MaxRecoveryHeaderBytes {
		return RecoveryMetadata{}, recoveryMetadataError(RecoveryMetadataOversized)
	}
	var metadata RecoveryMetadata
	if err := decodeStrict(encoded, &metadata); err != nil {
		return RecoveryMetadata{}, recoveryMetadataError(RecoveryMetadataInvalid)
	}
	if err := validateRecoveryMetadata(
		prefix,
		currentTopic,
		metadata,
		key,
		value,
		isTerminalContractDirectDLQ(metadata, currentTopic),
	); err != nil {
		return RecoveryMetadata{}, err
	}
	return metadata, nil
}

func EncodeRecoveryQuarantineHeaders(
	prefix string,
	destination TopicID,
	metadata RecoveryQuarantineMetadata,
	key, value []byte,
) ([]applicationeventstream.Header, error) {
	if err := validateRecoveryQuarantineMetadata(
		prefix, destination, metadata, key, value,
	); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(metadata)
	if err != nil || len(encoded) == 0 || len(encoded) > MaxRecoveryHeaderBytes {
		return nil, recoveryMetadataError(RecoveryMetadataOversized)
	}
	return []applicationeventstream.Header{{
		Key: RecoveryQuarantineHeaderKey, Value: encoded,
	}}, nil
}

func DecodeRecoveryQuarantineHeaders(
	prefix string,
	destination TopicID,
	headers []applicationeventstream.Header,
	key, value []byte,
) (RecoveryQuarantineMetadata, error) {
	var encoded []byte
	totalBytes := 0
	for _, header := range headers {
		totalBytes += len(header.Key) + len(header.Value)
		if len(headers) > MaxRecoveryHeaders || totalBytes > MaxRecoveryTotalHeaderBytes {
			return RecoveryQuarantineMetadata{}, recoveryMetadataError(RecoveryMetadataOversized)
		}
		if header.Key == RecoveryQuarantineHeaderKey {
			if encoded != nil {
				return RecoveryQuarantineMetadata{}, recoveryMetadataError(RecoveryMetadataInvalid)
			}
			encoded = header.Value
			continue
		}
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(header.Key)), "frux.recovery") {
			return RecoveryQuarantineMetadata{}, recoveryMetadataError(RecoveryMetadataUnknownHeader)
		}
	}
	if len(encoded) == 0 {
		return RecoveryQuarantineMetadata{}, recoveryMetadataError(RecoveryMetadataMissing)
	}
	if len(encoded) > MaxRecoveryHeaderBytes {
		return RecoveryQuarantineMetadata{}, recoveryMetadataError(RecoveryMetadataOversized)
	}
	var metadata RecoveryQuarantineMetadata
	if err := decodeStrict(encoded, &metadata); err != nil {
		return RecoveryQuarantineMetadata{}, recoveryMetadataError(RecoveryMetadataInvalid)
	}
	if err := validateRecoveryQuarantineMetadata(
		prefix, destination, metadata, key, value,
	); err != nil {
		return RecoveryQuarantineMetadata{}, err
	}
	return metadata, nil
}

func validateRecoveryMetadata(
	prefix string,
	destination TopicID,
	metadata RecoveryMetadata,
	key, value []byte,
	allowTerminalContractInvalidKey bool,
) error {
	recovery, err := Recovery(metadata.ConsumerGroup)
	if err != nil || recovery.Policy != RecoveryRetryTopics {
		return recoveryMetadataError(RecoveryMetadataUnsupportedGroup)
	}
	sourceName, err := TopicName(prefix, recovery.SourceTopic)
	if err != nil || metadata.SourceTopic != sourceName ||
		metadata.SourcePartition < 0 || metadata.SourceOffset < 0 {
		return recoveryMetadataError(RecoveryMetadataInvalidSource)
	}
	source, err := Topic(recovery.SourceTopic)
	if err != nil {
		return recoveryMetadataError(RecoveryMetadataInvalidSource)
	}
	if ValidateKey(source.KeyKind, key) != nil &&
		(!allowTerminalContractInvalidKey ||
			!isTerminalContractDirectDLQ(metadata, destination)) {
		return recoveryMetadataError(RecoveryMetadataInvalidSource)
	}
	if !recovery.AllowsFailure(metadata.FailureClass) {
		return recoveryMetadataError(RecoveryMetadataInvalid)
	}
	if metadata.ConsumedTopic != "" || metadata.ConsumedPartition != 0 ||
		metadata.ConsumedOffset != 0 || metadata.KeySHA256 != "" ||
		metadata.MetadataCode != "" || metadata.NonReplayable {
		return recoveryMetadataError(RecoveryMetadataInvalid)
	}
	if !validRecoveryEventIdentity(metadata.EventID, metadata.SchemaVersion, metadata.FailureClass) ||
		(metadata.ReplayID != "" && !validEnvelopeIdentity(metadata.ReplayID)) {
		return recoveryMetadataError(RecoveryMetadataInvalid)
	}
	if metadata.Attempt < 1 || metadata.Attempt > len(recovery.RetryTiers)+1 {
		return recoveryMetadataError(RecoveryMetadataInvalidAttempt)
	}
	if metadata.Tier == 0 {
		operatorReplayToSource := metadata.ReplayID != "" &&
			destination == recovery.SourceTopic
		if destination != recovery.DLQTopic && !operatorReplayToSource {
			return recoveryMetadataError(RecoveryMetadataInvalidTopic)
		}
	} else {
		tier, ok := recovery.RetryTier(metadata.Tier)
		if !ok || metadata.Attempt < metadata.Tier {
			return recoveryMetadataError(RecoveryMetadataInvalidAttempt)
		}
		if tier.Topic != destination {
			return recoveryMetadataError(RecoveryMetadataInvalidTopic)
		}
	}
	if metadata.FirstFailureAt.IsZero() || metadata.LatestFailureAt.IsZero() ||
		metadata.NotBefore.IsZero() ||
		metadata.FirstFailureAt.After(metadata.LatestFailureAt) ||
		metadata.NotBefore.Before(metadata.LatestFailureAt) ||
		metadata.LatestFailureAt.After(time.Now().UTC().Add(5*time.Minute)) ||
		metadata.NotBefore.After(metadata.LatestFailureAt.Add(35*time.Minute)) {
		return recoveryMetadataError(RecoveryMetadataInvalid)
	}
	expectedHash := sha256.Sum256(value)
	if metadata.PayloadSHA256 != hex.EncodeToString(expectedHash[:]) {
		return recoveryMetadataError(RecoveryMetadataHashMismatch)
	}
	return nil
}

func validateRecoveryQuarantineMetadata(
	prefix string,
	destination TopicID,
	metadata RecoveryQuarantineMetadata,
	key, value []byte,
) error {
	recovery, err := Recovery(metadata.ConsumerGroup)
	if err != nil || recovery.Policy != RecoveryRetryTopics ||
		destination != recovery.DLQTopic ||
		metadata.FailureClass != FailureRecoveryMetadataInvalid ||
		!metadata.NonReplayable ||
		metadata.ConsumedPartition < 0 || metadata.ConsumedOffset < 0 ||
		!validRecoveryMetadataCode(metadata.MetadataCode) {
		return recoveryMetadataError(RecoveryMetadataInvalid)
	}
	registeredConsumedTopic := false
	for _, tier := range recovery.RetryTiers {
		name, nameErr := TopicName(prefix, tier.Topic)
		if nameErr == nil && name == metadata.ConsumedTopic {
			registeredConsumedTopic = true
			break
		}
	}
	if !registeredConsumedTopic || metadata.QuarantinedAt.IsZero() ||
		metadata.QuarantinedAt.After(time.Now().UTC().Add(5*time.Minute)) {
		return recoveryMetadataError(RecoveryMetadataInvalidTopic)
	}
	payloadHash := sha256.Sum256(value)
	keyHash := sha256.Sum256(key)
	if metadata.PayloadSHA256 != hex.EncodeToString(payloadHash[:]) ||
		metadata.KeySHA256 != hex.EncodeToString(keyHash[:]) {
		return recoveryMetadataError(RecoveryMetadataHashMismatch)
	}
	return nil
}

func validRecoveryMetadataCode(code RecoveryMetadataCode) bool {
	switch code {
	case RecoveryMetadataMissing, RecoveryMetadataUnknownHeader,
		RecoveryMetadataOversized, RecoveryMetadataInvalidSource,
		RecoveryMetadataUnsupportedGroup, RecoveryMetadataInvalidAttempt,
		RecoveryMetadataHashMismatch, RecoveryMetadataInvalidTopic,
		RecoveryMetadataInvalid:
		return true
	default:
		return false
	}
}

func recoveryMetadataCode(err error) RecoveryMetadataCode {
	var metadataErr *RecoveryMetadataError
	if errors.As(err, &metadataErr) && validRecoveryMetadataCode(metadataErr.Code) {
		return metadataErr.Code
	}
	return RecoveryMetadataInvalid
}

func isTerminalContractDirectDLQ(
	metadata RecoveryMetadata,
	destination TopicID,
) bool {
	recovery, err := Recovery(metadata.ConsumerGroup)
	return err == nil &&
		recovery.Policy == RecoveryRetryTopics &&
		destination == recovery.DLQTopic &&
		metadata.FailureClass == FailureTerminalContract &&
		metadata.Attempt == 1 &&
		metadata.Tier == 0 &&
		metadata.ReplayID == ""
}

func validRecoveryEventIdentity(
	eventID string,
	schemaVersion int,
	failureClass FailureClass,
) bool {
	if schemaVersion == 0 {
		return failureClass == FailureTerminalContract &&
			strings.HasPrefix(eventID, "sha256:") &&
			len(eventID) == len("sha256:")+sha256.Size*2 &&
			validHex(eventID[len("sha256:"):])
	}
	return schemaVersion > 0 && validEnvelopeIdentity(eventID)
}

func validHex(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}

func RecoveryEventIdentity(value []byte) (string, int) {
	var envelope Envelope
	if len(value) > 0 && decodeStrict(value, &envelope) == nil &&
		validEnvelopeIdentity(envelope.EventID) && envelope.SchemaVersion > 0 {
		return envelope.EventID, envelope.SchemaVersion
	}
	sum := sha256.Sum256(value)
	return "sha256:" + hex.EncodeToString(sum[:]), 0
}

func PayloadSHA256(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

func recoveryMetadataError(code RecoveryMetadataCode) error {
	return &RecoveryMetadataError{Code: code}
}
