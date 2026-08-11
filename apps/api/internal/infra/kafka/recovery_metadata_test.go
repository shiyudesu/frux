package infrakafka

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	applicationeventstream "github.com/shiyudesu/frux/internal/application/eventstream"
)

func TestRecoveryMetadataRoundTripPreservesBusinessBytes(t *testing.T) {
	key, value := recoveryBusinessRecord(t)
	now := time.Now().UTC().Truncate(time.Millisecond)
	metadata := validRecoveryMetadata(t, now, value)
	originalKey := append([]byte(nil), key...)
	originalValue := append([]byte(nil), value...)

	headers, err := EncodeRecoveryHeaders(
		"dev",
		TopicFeedVideoPublishedRetry5s,
		metadata,
		key,
		value,
	)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeRecoveryHeaders(
		"dev",
		TopicFeedVideoPublishedRetry5s,
		headers,
		key,
		value,
	)
	if err != nil {
		t.Fatal(err)
	}
	if decoded != metadata {
		t.Fatalf("decoded metadata = %+v, want %+v", decoded, metadata)
	}
	if string(key) != string(originalKey) || string(value) != string(originalValue) {
		t.Fatal("metadata codec changed the business key or value")
	}
}

func TestRecoveryMetadataRejectsInvalidBoundsAndAllowlists(t *testing.T) {
	key, value := recoveryBusinessRecord(t)
	now := time.Now().UTC().Truncate(time.Millisecond)
	valid := validRecoveryMetadata(t, now, value)
	validHeaders, err := EncodeRecoveryHeaders(
		"dev", TopicFeedVideoPublishedRetry5s, valid, key, value,
	)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name        string
		destination TopicID
		headers     []applicationeventstream.Header
		key         []byte
		value       []byte
		code        RecoveryMetadataCode
	}{
		{
			name: "unsupported group", destination: TopicFeedVideoPublishedRetry5s,
			headers: recoveryHeadersWith(t, valid, func(item *RecoveryMetadata) {
				item.ConsumerGroup = ConsumerGroupID("unsupported")
			}),
			key: key, value: value, code: RecoveryMetadataUnsupportedGroup,
		},
		{
			name: "invalid source topic", destination: TopicFeedVideoPublishedRetry5s,
			headers: recoveryHeadersWith(t, valid, func(item *RecoveryMetadata) {
				item.SourceTopic = "dev.frux.exposure.view-event-recorded.v1"
			}),
			key: key, value: value, code: RecoveryMetadataInvalidSource,
		},
		{
			name: "attempt above registry", destination: TopicFeedVideoPublishedRetry5s,
			headers: recoveryHeadersWith(t, valid, func(item *RecoveryMetadata) {
				item.Attempt = 7
			}),
			key: key, value: value, code: RecoveryMetadataInvalidAttempt,
		},
		{
			name: "payload hash mismatch", destination: TopicFeedVideoPublishedRetry5s,
			headers: validHeaders, key: key, value: append(value, '\n'),
			code: RecoveryMetadataHashMismatch,
		},
		{
			name: "topic outside tier allowlist", destination: TopicEmbeddingVideoRetry5s,
			headers: validHeaders, key: key, value: value,
			code: RecoveryMetadataInvalidTopic,
		},
		{
			name: "oversized metadata", destination: TopicFeedVideoPublishedRetry5s,
			headers: []applicationeventstream.Header{{
				Key: RecoveryHeaderKey, Value: []byte(strings.Repeat("x", MaxRecoveryHeaderBytes+1)),
			}},
			key: key, value: value, code: RecoveryMetadataOversized,
		},
		{
			name: "unknown recovery header", destination: TopicFeedVideoPublishedRetry5s,
			headers: append(validHeaders, applicationeventstream.Header{
				Key: "frux.recovery.future", Value: []byte("x"),
			}),
			key: key, value: value, code: RecoveryMetadataUnknownHeader,
		},
		{
			name: "invalid source key", destination: TopicFeedVideoPublishedRetry5s,
			headers: validHeaders, key: []byte("video:0"), value: value,
			code: RecoveryMetadataInvalidSource,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := DecodeRecoveryHeaders(
				"dev", test.destination, test.headers, test.key, test.value,
			)
			var metadataErr *RecoveryMetadataError
			if !errors.As(err, &metadataErr) || metadataErr.Code != test.code {
				t.Fatalf("error = %v, want code %s", err, test.code)
			}
		})
	}
}

func TestRecoveryMetadataRejectsUnknownJSONFields(t *testing.T) {
	key, value := recoveryBusinessRecord(t)
	metadata := validRecoveryMetadata(t, time.Now().UTC().Truncate(time.Millisecond), value)
	encoded, err := json.Marshal(metadata)
	if err != nil {
		t.Fatal(err)
	}
	encoded[len(encoded)-1] = ','
	encoded = append(encoded, []byte(`"unknown":true}`)...)
	_, err = DecodeRecoveryHeaders(
		"dev",
		TopicFeedVideoPublishedRetry5s,
		[]applicationeventstream.Header{{Key: RecoveryHeaderKey, Value: encoded}},
		key,
		value,
	)
	var metadataErr *RecoveryMetadataError
	if !errors.As(err, &metadataErr) || metadataErr.Code != RecoveryMetadataInvalid {
		t.Fatalf("error = %v", err)
	}
}

func TestRecoveryEventIdentityUsesBoundedHashForMalformedPayload(t *testing.T) {
	eventID, schema := RecoveryEventIdentity([]byte("{"))
	if schema != 0 || !strings.HasPrefix(eventID, "sha256:") || len(eventID) != 71 {
		t.Fatalf("identity=%q schema=%d", eventID, schema)
	}
}

func TestRecoveryMetadataAllowsRegisteredOperatorReplayToSource(t *testing.T) {
	key, value := recoveryBusinessRecord(t)
	now := time.Now().UTC().Truncate(time.Millisecond)
	metadata := validRecoveryMetadata(t, now, value)
	metadata.Tier = 0
	metadata.ReplayID = "replay-0123456789abcdef0123456789abcdef"
	headers, err := EncodeRecoveryHeaders(
		"dev", TopicVideoPublished, metadata, key, value,
	)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeRecoveryHeaders(
		"dev", TopicVideoPublished, headers, key, value,
	)
	if err != nil || decoded.ReplayID != metadata.ReplayID ||
		decoded.Tier != 0 {
		t.Fatalf("decoded=%+v err=%v", decoded, err)
	}
}

func TestTerminalContractDLQMetadataAllowsMalformedSourceKeyOnlyOnDirectDLQ(t *testing.T) {
	_, value := recoveryBusinessRecord(t)
	key := []byte("video:0")
	now := time.Now().UTC().Truncate(time.Millisecond)
	metadata := validRecoveryMetadata(t, now, value)
	metadata.Attempt = 1
	metadata.Tier = 0
	metadata.FailureClass = FailureTerminalContract
	metadata.NotBefore = now

	headers, err := encodeTerminalContractDLQHeaders(
		"dev", TopicFeedVideoPublishedDLQ, metadata, key, value,
	)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeRecoveryHeaders(
		"dev", TopicFeedVideoPublishedDLQ, headers, key, value,
	)
	if err != nil || decoded != metadata {
		t.Fatalf("decoded=%+v err=%v", decoded, err)
	}

	for _, test := range []struct {
		name        string
		destination TopicID
		mutate      func(*RecoveryMetadata)
	}{
		{
			name:        "retry tier",
			destination: TopicFeedVideoPublishedRetry5s,
			mutate: func(item *RecoveryMetadata) {
				item.Tier = 1
				item.FailureClass = FailureLocalRetryExhausted
				item.NotBefore = now.Add(5 * time.Second)
			},
		},
		{
			name:        "operator replay",
			destination: TopicFeedVideoPublishedDLQ,
			mutate: func(item *RecoveryMetadata) {
				item.ReplayID = "replay-0123456789abcdef0123456789abcdef"
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			item := metadata
			test.mutate(&item)
			if _, err := encodeTerminalContractDLQHeaders(
				"dev", test.destination, item, key, value,
			); err == nil {
				t.Fatal("malformed source key bypassed non-terminal direct-DLQ validation")
			}
		})
	}
}

func recoveryBusinessRecord(t *testing.T) ([]byte, []byte) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Millisecond)
	key, err := EncodeKey(KeyKindVideoID, VideoKey{VideoID: 42})
	if err != nil {
		t.Fatal(err)
	}
	value, err := EncodeEvent(
		TopicVideoPublished,
		key,
		EventMetadata{
			EventID: "event-video-42", Type: EventTypeVideoPublished,
			SchemaVersion: 1, OccurredAt: now.Add(-time.Second), ProducedAt: now,
			Producer: ProducerVideoWorker,
		},
		VideoPublishedPayload{
			EventID: "event-video-42", VideoID: 42, AuthorID: 7,
			PublishedAt: now.Add(-time.Second), OccurredAt: now.Add(-time.Second),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	return key, value
}

func validRecoveryMetadata(t *testing.T, now time.Time, value []byte) RecoveryMetadata {
	t.Helper()
	source, err := TopicName("dev", TopicVideoPublished)
	if err != nil {
		t.Fatal(err)
	}
	return RecoveryMetadata{
		SourceTopic: source, SourcePartition: 2, SourceOffset: 9,
		EventID: "event-video-42", SchemaVersion: 1,
		ConsumerGroup: GroupFeedVideoPublishedActive,
		Attempt:       1, Tier: 1, FailureClass: FailureLocalRetryExhausted,
		FirstFailureAt: now, LatestFailureAt: now,
		NotBefore: now.Add(5 * time.Second), PayloadSHA256: PayloadSHA256(value),
	}
}

func recoveryHeadersWith(
	t *testing.T,
	metadata RecoveryMetadata,
	mutate func(*RecoveryMetadata),
) []applicationeventstream.Header {
	t.Helper()
	mutate(&metadata)
	encoded, err := json.Marshal(metadata)
	if err != nil {
		t.Fatal(err)
	}
	return []applicationeventstream.Header{{Key: RecoveryHeaderKey, Value: encoded}}
}
