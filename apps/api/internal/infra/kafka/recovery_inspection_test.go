package infrakafka

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

type fakeDLQInspectionBackend struct {
	offsets        map[string]map[int32]dlqPartitionOffsets
	offsetSequence []map[string]map[int32]dlqPartitionOffsets
	records        []brokerRecord
	recordSequence [][]brokerRecord
	offsetErr      error
	readErr        error
	offsetCalls    int
	readCalls      int
	lastTopics     []string
	lastRanges     []dlqReadRange
	offsetsBlock   bool
}

func (f *fakeDLQInspectionBackend) PartitionOffsets(
	ctx context.Context,
	topics []string,
	_ time.Time,
) (map[string]map[int32]dlqPartitionOffsets, error) {
	f.offsetCalls++
	f.lastTopics = append([]string(nil), topics...)
	if f.offsetsBlock {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if len(f.offsetSequence) > 0 {
		index := f.offsetCalls - 1
		if index >= len(f.offsetSequence) {
			index = len(f.offsetSequence) - 1
		}
		return f.offsetSequence[index], f.offsetErr
	}
	return f.offsets, f.offsetErr
}

func (f *fakeDLQInspectionBackend) ReadRecords(
	ctx context.Context,
	ranges []dlqReadRange,
) ([]brokerRecord, error) {
	f.readCalls++
	f.lastRanges = append([]dlqReadRange(nil), ranges...)
	if f.readErr != nil {
		return nil, f.readErr
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	if len(f.recordSequence) > 0 {
		index := f.readCalls - 1
		if index >= len(f.recordSequence) {
			index = len(f.recordSequence) - 1
		}
		return append([]brokerRecord(nil), f.recordSequence[index]...), nil
	}
	return append([]brokerRecord(nil), f.records...), nil
}

func TestDLQInspectorListsAllowlistedOffsetAndAgeSummaries(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	backend, record, feedTopic, embeddingTopic := dlqInspectionFixture(t, now)
	inspector := testDLQInspector(backend, now)

	summaries, err := inspector.ListDLQTopics(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 2 || len(backend.lastTopics) != 2 ||
		backend.readCalls != 1 || len(backend.lastRanges) != 1 {
		t.Fatalf(
			"summaries=%+v topics=%v reads=%d ranges=%+v",
			summaries,
			backend.lastTopics,
			backend.readCalls,
			backend.lastRanges,
		)
	}
	feed := summaryByTopic(t, summaries, feedTopic)
	if feed.ConsumerGroup != GroupFeedVideoPublishedActive ||
		feed.PartitionCount != 2 || feed.RetainedEstimate != 3 ||
		feed.EndOffset != 8 ||
		feed.EndOffsetGrowth != 2 || feed.RecentIngress != 2 ||
		!feed.OldestRecordAt.Equal(record.Timestamp) ||
		feed.OldestAge != 2*time.Hour {
		t.Fatalf("feed summary = %+v", feed)
	}
	if len(feed.Partitions) != 2 ||
		feed.Partitions[0].RetainedStartOffset != 5 ||
		feed.Partitions[0].EndOffset != 8 ||
		feed.Partitions[0].RetainedEstimate != 3 ||
		feed.Partitions[0].RecentIngress != 2 {
		t.Fatalf("feed partitions = %+v", feed.Partitions)
	}
	embedding := summaryByTopic(t, summaries, embeddingTopic)
	if embedding.ConsumerGroup != GroupEmbeddingVideoPublishedActive ||
		embedding.RetainedEstimate != 0 || !embedding.OldestRecordAt.IsZero() {
		t.Fatalf("embedding summary = %+v", embedding)
	}
}

func TestDLQInspectorReadsExactOffsetsWithRedactedDiagnostics(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	backend, record, feedTopic, _ := dlqInspectionFixture(t, now)
	inspector := testDLQInspector(backend, now)

	diagnostics, err := inspector.ReadDLQRecords(
		context.Background(),
		feedTopic,
		0,
		5,
		2,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(diagnostics) != 1 || len(backend.lastRanges) != 1 {
		t.Fatalf("diagnostics=%+v ranges=%+v", diagnostics, backend.lastRanges)
	}
	request := backend.lastRanges[0]
	if request.Topic != feedTopic || request.Partition != 0 ||
		request.Start != 5 || request.End != 8 || request.Limit != 2 {
		t.Fatalf("read range = %+v", request)
	}
	diagnostic := diagnostics[0]
	if diagnostic.Topic != feedTopic || diagnostic.Partition != 0 ||
		diagnostic.Offset != 5 || diagnostic.SourceOffset != 91 ||
		diagnostic.ConsumerGroup != GroupFeedVideoPublishedActive ||
		diagnostic.FailureClass != FailureTerminalDomain ||
		diagnostic.Attempt != 1 || diagnostic.KeyBytes != len(record.Key) ||
		diagnostic.PayloadBytes != len(record.Value) ||
		diagnostic.PayloadSHA256 != PayloadSHA256(record.Value) ||
		diagnostic.ContentType != "application/json" || !diagnostic.JSONValid ||
		len(diagnostic.JSONFields) == 0 {
		t.Fatalf("diagnostic = %+v", diagnostic)
	}
	encoded, err := json.Marshal(diagnostic)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "secret-video-title") ||
		strings.Contains(string(encoded), string(record.Key)) {
		t.Fatalf("diagnostic exposed business bytes: %s", encoded)
	}
	diagnosticType := reflect.TypeOf(DLQRecordDiagnostic{})
	for _, forbidden := range []string{"Key", "Value", "Payload", "Headers", "Error", "RawError"} {
		if _, found := diagnosticType.FieldByName(forbidden); found {
			t.Fatalf("diagnostic exposes forbidden field %q", forbidden)
		}
	}
}

func TestDLQInspectorFetchesOneExactRetainedRecordForReplay(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	backend, expected, feedTopic, _ := dlqInspectionFixture(t, now)
	inspector := testDLQInspector(backend, now)

	record, metadata, err := inspector.FetchDLQRecord(
		context.Background(), feedTopic, 0, 5,
	)
	if err != nil {
		t.Fatal(err)
	}
	if record.Topic != feedTopic || record.Partition != 0 || record.Offset != 5 ||
		string(record.Key) != string(expected.Key) ||
		string(record.Value) != string(expected.Value) ||
		metadata.ConsumerGroup != GroupFeedVideoPublishedActive ||
		metadata.SourceOffset != 91 || metadata.PayloadSHA256 != PayloadSHA256(record.Value) {
		t.Fatalf("record=%+v metadata=%+v", record, metadata)
	}
	backend.records[0].Value[0] = 'x'
	if string(record.Value) == string(backend.records[0].Value) {
		t.Fatal("fetched replay bytes alias the inspection backend")
	}
}

func TestDLQInspectorRejectsUnauthorizedAndInvalidCoordinates(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	backend, _, feedTopic, _ := dlqInspectionFixture(t, now)
	inspector := testDLQInspector(backend, now)

	if _, err := inspector.ReadDLQRecords(
		context.Background(),
		"frux.video.published.v1",
		0,
		5,
		1,
	); !errors.Is(err, ErrDLQTopicNotAllowed) || backend.offsetCalls != 0 {
		t.Fatalf("unauthorized error=%v offset_calls=%d", err, backend.offsetCalls)
	}
	tests := []struct {
		name      string
		partition int32
		offset    int64
		limit     int
		want      error
	}{
		{name: "negative partition", partition: -1, offset: 5, limit: 1, want: ErrDLQInvalidPartition},
		{name: "negative offset", partition: 0, offset: -1, limit: 1, want: ErrDLQInvalidOffset},
		{name: "zero limit", partition: 0, offset: 5, limit: 0, want: ErrDLQInvalidLimit},
		{name: "oversized limit", partition: 0, offset: 5, limit: MaxDLQReadLimit + 1, want: ErrDLQInvalidLimit},
		{name: "unknown partition", partition: 9, offset: 5, limit: 1, want: ErrDLQInvalidPartition},
		{name: "expired offset", partition: 0, offset: 4, limit: 1, want: ErrDLQOffsetExpired},
		{name: "end offset", partition: 0, offset: 8, limit: 1, want: ErrDLQOffsetUnavailable},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := inspector.ReadDLQRecords(
				context.Background(),
				feedTopic,
				test.partition,
				test.offset,
				test.limit,
			)
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestDLQInspectorRejectsCompactedOrInvalidRecords(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	backend, record, feedTopic, _ := dlqInspectionFixture(t, now)
	inspector := testDLQInspector(backend, now)

	compacted := record
	compacted.Offset = 6
	backend.records = []brokerRecord{compacted}
	if _, err := inspector.ReadDLQRecords(
		context.Background(),
		feedTopic,
		0,
		5,
		1,
	); !errors.Is(err, ErrDLQOffsetUnavailable) {
		t.Fatalf("compacted offset error = %v", err)
	}
	if _, err := inspector.ListDLQTopics(context.Background()); !errors.Is(err, ErrDLQOffsetUnavailable) {
		t.Fatalf("compacted oldest error = %v", err)
	}

	invalid := record
	invalid.Headers = nil
	backend.records = []brokerRecord{invalid}
	if _, err := inspector.ReadDLQRecords(
		context.Background(),
		feedTopic,
		0,
		5,
		1,
	); !errors.Is(err, ErrDLQRecordInvalid) {
		t.Fatalf("invalid metadata error = %v", err)
	}
}

func TestDLQInspectorCancellationAndBrokerFailuresAreSafe(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	t.Run("cancellation", func(t *testing.T) {
		backend, _, _, _ := dlqInspectionFixture(t, now)
		backend.offsetsBlock = true
		inspector := testDLQInspector(backend, now)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := inspector.ListDLQTopics(ctx); !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("offset broker failure", func(t *testing.T) {
		backend, _, _, _ := dlqInspectionFixture(t, now)
		backend.offsetErr = errors.New("broker password=secret")
		inspector := testDLQInspector(backend, now)
		_, err := inspector.ListDLQTopics(context.Background())
		if !errors.Is(err, ErrDLQInspectionFailed) ||
			strings.Contains(err.Error(), "secret") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("read broker failure", func(t *testing.T) {
		backend, _, feedTopic, _ := dlqInspectionFixture(t, now)
		backend.readErr = errors.New("broker address=private")
		inspector := testDLQInspector(backend, now)
		_, err := inspector.ReadDLQRecords(
			context.Background(),
			feedTopic,
			0,
			5,
			1,
		)
		if !errors.Is(err, ErrDLQInspectionFailed) ||
			strings.Contains(err.Error(), "private") {
			t.Fatalf("error = %v", err)
		}
	})
}

func TestBoundedJSONDiagnosticsLimitsFieldNames(t *testing.T) {
	object := make(map[string]int, MaxDLQJSONFields+5)
	for index := 0; index < MaxDLQJSONFields+4; index++ {
		object[fmt.Sprintf("field_%02d", index)] = index
	}
	object[strings.Repeat("x", MaxDLQJSONFieldLength+1)] = 99
	encoded, err := json.Marshal(object)
	if err != nil {
		t.Fatal(err)
	}
	fields, valid := boundedJSONDiagnostics(encoded)
	if !valid || len(fields) != MaxDLQJSONFields {
		t.Fatalf("valid=%t fields=%v", valid, fields)
	}
	for _, field := range fields {
		if len(field) > MaxDLQJSONFieldLength {
			t.Fatalf("oversized field returned: %q", field)
		}
	}
}

func TestDLQInspectionShowsNonReplayableRecoveryMetadataQuarantine(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	recovery, err := Recovery(GroupFeedVideoPublishedActive)
	if err != nil {
		t.Fatal(err)
	}
	dlqTopic, err := TopicName("dev", recovery.DLQTopic)
	if err != nil {
		t.Fatal(err)
	}
	retryTopic, err := TopicName("dev", TopicFeedVideoPublishedRetry5s)
	if err != nil {
		t.Fatal(err)
	}
	key, value := recoveryBusinessRecord(t)
	headers, err := EncodeRecoveryQuarantineHeaders(
		"dev",
		recovery.DLQTopic,
		RecoveryQuarantineMetadata{
			ConsumedTopic:     retryTopic,
			ConsumedPartition: 3,
			ConsumedOffset:    91,
			ConsumerGroup:     recovery.Group,
			FailureClass:      FailureRecoveryMetadataInvalid,
			MetadataCode:      RecoveryMetadataInvalidTopic,
			QuarantinedAt:     now,
			PayloadSHA256:     PayloadSHA256(value),
			KeySHA256:         PayloadSHA256(key),
			NonReplayable:     true,
		},
		key,
		value,
	)
	if err != nil {
		t.Fatal(err)
	}
	backend := &fakeDLQInspectionBackend{
		offsets: map[string]map[int32]dlqPartitionOffsets{
			dlqTopic: {0: {Start: 5, End: 6, RecentStart: 5}},
		},
		records: []brokerRecord{{
			Topic: dlqTopic, Partition: 0, Offset: 5, Timestamp: now,
			Key: key, Value: value, Headers: headers,
		}},
	}
	inspector := testDLQInspector(backend, now)
	inspector.prefix = "dev"
	diagnostics, err := inspector.ReadDLQRecords(
		context.Background(), dlqTopic, 0, 5, 1,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(diagnostics) != 1 ||
		diagnostics[0].FailureClass != FailureRecoveryMetadataInvalid ||
		diagnostics[0].ConsumedTopic != retryTopic ||
		diagnostics[0].ConsumedPartition != 3 ||
		diagnostics[0].ConsumedOffset != 91 ||
		diagnostics[0].MetadataCode != RecoveryMetadataInvalidTopic ||
		diagnostics[0].Replayable {
		t.Fatalf("diagnostics=%+v", diagnostics)
	}
	_, metadata, err := inspector.FetchDLQRecord(
		context.Background(), dlqTopic, 0, 5,
	)
	if err != nil || !metadata.NonReplayable ||
		metadata.FailureClass != FailureRecoveryMetadataInvalid {
		t.Fatalf("metadata=%+v err=%v", metadata, err)
	}
}

func testDLQInspector(backend dlqInspectionBackend, now time.Time) *DLQInspector {
	return &DLQInspector{
		backend: backend, timeout: time.Second,
		recentWindow: defaultDLQRecentWindow,
		now:          func() time.Time { return now },
	}
}

func dlqInspectionFixture(
	t *testing.T,
	now time.Time,
) (*fakeDLQInspectionBackend, brokerRecord, string, string) {
	t.Helper()
	key, err := EncodeKey(KeyKindVideoID, VideoKey{VideoID: 42})
	if err != nil {
		t.Fatal(err)
	}
	value, err := EncodeEvent(
		TopicVideoPublished,
		key,
		EventMetadata{
			EventID: "event-video-42", Type: EventTypeVideoPublished,
			SchemaVersion: 1, OccurredAt: now.Add(-3 * time.Hour),
			ProducedAt: now.Add(-3 * time.Hour), Producer: ProducerVideoWorker,
		},
		VideoPublishedPayload{
			EventID: "event-video-42", VideoID: 42, AuthorID: 7,
			Title:       "secret-video-title",
			PublishedAt: now.Add(-3 * time.Hour), OccurredAt: now.Add(-3 * time.Hour),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	sourceTopic, err := TopicName("", TopicVideoPublished)
	if err != nil {
		t.Fatal(err)
	}
	metadata := RecoveryMetadata{
		SourceTopic: sourceTopic, SourcePartition: 3, SourceOffset: 91,
		EventID: "event-video-42", SchemaVersion: 1,
		ConsumerGroup: GroupFeedVideoPublishedActive,
		Attempt:       1, Tier: 0, FailureClass: FailureTerminalDomain,
		FirstFailureAt:  now.Add(-2 * time.Hour),
		LatestFailureAt: now.Add(-2 * time.Hour),
		NotBefore:       now.Add(-2 * time.Hour), PayloadSHA256: PayloadSHA256(value),
	}
	headers, err := EncodeRecoveryHeaders(
		"",
		TopicFeedVideoPublishedDLQ,
		metadata,
		key,
		value,
	)
	if err != nil {
		t.Fatal(err)
	}
	feedTopic, err := TopicName("", TopicFeedVideoPublishedDLQ)
	if err != nil {
		t.Fatal(err)
	}
	embeddingTopic, err := TopicName("", TopicEmbeddingVideoPublishedDLQ)
	if err != nil {
		t.Fatal(err)
	}
	record := brokerRecord{
		Topic: feedTopic, Partition: 0, Offset: 5,
		Timestamp: now.Add(-2 * time.Hour),
		Key:       key, Value: value, Headers: headers,
	}
	return &fakeDLQInspectionBackend{
		offsets: map[string]map[int32]dlqPartitionOffsets{
			feedTopic: {
				0: {Start: 5, End: 8, RecentStart: 6},
				1: {Start: 0, End: 0, RecentStart: 0},
			},
			embeddingTopic: {
				0: {Start: 0, End: 0, RecentStart: 0},
			},
		},
		records: []brokerRecord{record},
	}, record, feedTopic, embeddingTopic
}

func summaryByTopic(
	t *testing.T,
	summaries []DLQTopicSummary,
	topic string,
) DLQTopicSummary {
	t.Helper()
	for _, summary := range summaries {
		if summary.Topic == topic {
			return summary
		}
	}
	t.Fatalf("topic %q not found in %+v", topic, summaries)
	return DLQTopicSummary{}
}
