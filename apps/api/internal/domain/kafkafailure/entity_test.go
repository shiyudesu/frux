package domainkafkafailure

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestReplayCommandNormalizesAndFingerprintsIdentity(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	command, err := NewReplayCommand(
		" frux.feed.video-published.dlq.v1 ", 2, 41, 9,
		"operator_retry", " retry-key ", "replay-0123456789abcdef0123456789abcdef", now,
	)
	if err != nil {
		t.Fatal(err)
	}
	if command.Coordinate.Topic != "frux.feed.video-published.dlq.v1" ||
		command.IdempotencyFingerprint == "retry-key" ||
		!strings.HasPrefix(command.IdempotencyFingerprint, "sha256:") ||
		len(command.RequestFingerprint) != 64 {
		t.Fatalf("unexpected command: %+v", command)
	}
	repeated, err := NewReplayCommand(
		command.Coordinate.Topic, 2, 41, 9,
		"operator_retry", "retry-key", command.ReplayID, now,
	)
	if err != nil || repeated.IdempotencyFingerprint != command.IdempotencyFingerprint ||
		repeated.RequestFingerprint != command.RequestFingerprint {
		t.Fatalf("repeat command=%+v err=%v", repeated, err)
	}
}

func TestReplayCommandRejectsBoundsAndUnregisteredReasons(t *testing.T) {
	now := time.Now().UTC()
	tests := []struct {
		name      string
		topic     string
		partition int32
		offset    int64
		actor     int64
		reason    string
		key       string
		want      error
	}{
		{name: "topic", partition: 0, actor: 1, reason: "operator_retry", key: "k", want: ErrInvalidTopic},
		{name: "partition", topic: "frux.dlq", partition: -1, actor: 1, reason: "operator_retry", key: "k", want: ErrInvalidPartition},
		{name: "offset", topic: "frux.dlq", offset: -1, actor: 1, reason: "operator_retry", key: "k", want: ErrInvalidOffset},
		{name: "actor", topic: "frux.dlq", partition: 0, actor: 0, reason: "operator_retry", key: "k", want: ErrInvalidActor},
		{name: "reason", topic: "frux.dlq", partition: 0, actor: 1, reason: "free form", key: "k", want: ErrInvalidReason},
		{name: "key missing", topic: "frux.dlq", partition: 0, actor: 1, reason: "operator_retry", want: ErrIdempotencyKeyRequired},
		{name: "key long", topic: "frux.dlq", partition: 0, actor: 1, reason: "operator_retry", key: strings.Repeat("x", 129), want: ErrIdempotencyKeyTooLong},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewReplayCommand(
				test.topic, test.partition, test.offset, test.actor, test.reason,
				test.key, "replay-0123456789abcdef0123456789abcdef", now,
			)
			if !errors.Is(err, test.want) {
				t.Fatalf("error=%v want=%v", err, test.want)
			}
		})
	}
}

func TestValidateRecordRequiresRegisteredProvenanceAndPayloadHash(t *testing.T) {
	now := time.Now().UTC()
	value := []byte(`{"event_id":"event-1"}`)
	route := RecoveryRoute{
		DLQTopic:      "frux.feed.video-published.dlq.v1",
		ConsumerGroup: "feed_video_published_active",
		SourceTopic:   "frux.video.published.v1",
		ReplayTopic:   "frux.video.published.v1",
		MaxAttempt:    6, Retention: 30 * 24 * time.Hour,
	}
	record := RetainedRecord{
		Coordinate: Coordinate{Topic: route.DLQTopic, Partition: 1, Offset: 8},
		Value:      value,
		Metadata: RecoveryMetadata{
			SourceTopic: route.SourceTopic, SourcePartition: 2, SourceOffset: 7,
			EventID: "event-1", SchemaVersion: 1, ConsumerGroup: route.ConsumerGroup,
			Attempt: 2, Tier: 0, FailureClass: "terminal_domain",
			FirstFailureAt: now.Add(-time.Minute), LatestFailureAt: now.Add(-time.Second),
			NotBefore: now, PayloadSHA256: PayloadSHA256(value),
		},
	}
	if err := ValidateRecord(route, record); err != nil {
		t.Fatalf("valid record rejected: %v", err)
	}
	record.Metadata.PayloadSHA256 = strings.Repeat("0", 64)
	if !errors.Is(ValidateRecord(route, record), ErrInvalidProvenance) {
		t.Fatal("payload hash mismatch was accepted")
	}
	record.Metadata.PayloadSHA256 = PayloadSHA256(value)
	record.Metadata.NonReplayable = true
	record.Metadata.FailureClass = "recovery_metadata_invalid"
	if !errors.Is(ValidateRecord(route, record), ErrInvalidProvenance) {
		t.Fatal("non-replayable quarantine record was accepted")
	}
}
