package infrakafka

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	applicationeventstream "github.com/shiyudesu/frux/internal/application/eventstream"
	domainkafkafailure "github.com/shiyudesu/frux/internal/domain/kafkafailure"
	infraconfig "github.com/shiyudesu/frux/internal/infra/config"

	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
)

func TestRecoveryAdapterResolvesOnlyRegisteredDLQRoutesAndContracts(t *testing.T) {
	adapter := NewRecoveryAdapter(nil, infraconfig.KafkaConfig{TopicPrefix: "dev"})
	dlq, err := TopicName("dev", TopicFeedVideoPublishedDLQ)
	if err != nil {
		t.Fatal(err)
	}

	source, err := TopicName("dev", TopicVideoPublished)
	if err != nil {
		t.Fatal(err)
	}
	firstRetry, err := TopicName("dev", TopicFeedVideoPublishedRetry5s)
	if err != nil {
		t.Fatal(err)
	}
	route, err := adapter.RouteForDLQ(dlq)
	if err != nil {
		t.Fatal(err)
	}
	if route.DLQTopic != dlq ||
		route.ConsumerGroup != string(GroupFeedVideoPublishedActive) ||
		route.SourceTopic != source || route.ReplayTopic != firstRetry ||
		route.ReplayTier != 1 || route.MaxAttempt != 6 ||
		route.Retention != 30*24*time.Hour {
		t.Fatalf("route=%+v", route)
	}
	if _, err := adapter.RouteForDLQ(source); !errors.Is(err, domainkafkafailure.ErrTopicNotAllowed) {
		t.Fatalf("arbitrary topic error=%v", err)
	}

	key, value := recoveryBusinessRecord(t)
	eventID, schema, err := adapter.Validate(source, key, value)
	if err != nil || eventID != "event-video-42" || schema != 1 {
		t.Fatalf("contract event=%q schema=%d err=%v", eventID, schema, err)
	}
	if _, _, err := adapter.Validate("dev.frux.unknown.v1", key, value); !errors.Is(
		err, domainkafkafailure.ErrInvalidProvenance,
	) {
		t.Fatalf("unknown source contract error=%v", err)
	}
}

func TestRecoveryAdapterReplayProgressesThroughAllTiersToFinalDLQ(t *testing.T) {
	fake := &fakeSyncProducer{}
	backbone := &Backbone{publisher: &Publisher{
		producer: fake, prefix: "dev", timeout: time.Second,
	}}
	adapter := NewRecoveryAdapter(backbone, infraconfig.KafkaConfig{TopicPrefix: "dev"})
	dlq, err := TopicName("dev", TopicFeedVideoPublishedDLQ)
	if err != nil {
		t.Fatal(err)
	}
	route, err := adapter.RouteForDLQ(dlq)
	if err != nil {
		t.Fatal(err)
	}
	key, value := recoveryBusinessRecord(t)
	failedAt := time.Now().UTC().Add(-time.Hour).Truncate(time.Millisecond)
	record := domainkafkafailure.RetainedRecord{
		Coordinate: domainkafkafailure.Coordinate{
			Topic: dlq, Partition: 2, Offset: 41,
		},
		Key: key, Value: value,
		Metadata: domainkafkafailure.RecoveryMetadata{
			SourceTopic: route.SourceTopic, SourcePartition: 1, SourceOffset: 29,
			EventID: "event-video-42", SchemaVersion: 1,
			ConsumerGroup: route.ConsumerGroup, Attempt: route.MaxAttempt, Tier: 0,
			FailureClass:   string(FailureLocalRetryExhausted),
			FirstFailureAt: failedAt, LatestFailureAt: failedAt,
			NotBefore: failedAt, PayloadSHA256: PayloadSHA256(value),
		},
	}
	if err := adapter.PublishReplay(
		context.Background(),
		route,
		record,
		"replay-0123456789abcdef0123456789abcdef",
	); err != nil {
		t.Fatal(err)
	}
	current := brokerRecord{
		Topic: fake.record.Topic, Partition: fake.record.Partition,
		Key:     append([]byte(nil), fake.record.Key...),
		Value:   append([]byte(nil), fake.record.Value...),
		Headers: recordHeaders(fake.record),
	}
	recovery, err := Recovery(GroupFeedVideoPublishedActive)
	if err != nil {
		t.Fatal(err)
	}
	for tier := 1; tier <= len(recovery.RetryTiers); tier++ {
		publisher := &fakeRecoveryPublisher{}
		consumer := testRecoveryConsumer(
			t,
			&fakeConsumerSource{},
			GroupFeedVideoPublishedActive,
			tier,
			handlerFunc(func(context.Context, applicationeventstream.Event) (applicationeventstream.Outcome, error) {
				return applicationeventstream.OutcomeRetryable, errors.New("still unavailable")
			}),
			publisher,
		)
		consumer.topicPrefix = "dev"
		var clockCalls atomic.Int32
		consumer.now = func() time.Time {
			if clockCalls.Add(1) == 1 {
				return time.Now().UTC().Add(31 * time.Minute)
			}
			return time.Now().UTC()
		}
		result := consumer.processPartition(context.Background(), []brokerRecord{current})
		if result.err != nil || result.eligible == nil || len(publisher.records) != 1 {
			t.Fatalf("tier=%d result=%+v published=%+v", tier, result, publisher.records)
		}
		published := publisher.records[0]
		wantDestination := recovery.DLQTopic
		wantTier := 0
		if tier < len(recovery.RetryTiers) {
			next, _ := recovery.RetryTier(tier + 1)
			wantDestination = next.Topic
			wantTier = tier + 1
		}
		if published.destination != wantDestination {
			t.Fatalf(
				"tier=%d destination=%s want=%s",
				tier, published.destination, wantDestination,
			)
		}
		metadata, err := DecodeRecoveryHeaders(
			"dev", published.destination,
			published.headers, published.key, published.value,
		)
		if err != nil {
			t.Fatalf("tier=%d metadata error=%v", tier, err)
		}
		if metadata.Attempt != tier+1 || metadata.Tier != wantTier ||
			metadata.Attempt > route.MaxAttempt {
			t.Fatalf("tier=%d metadata=%+v", tier, metadata)
		}
		topicName, err := TopicName("dev", published.destination)
		if err != nil {
			t.Fatal(err)
		}
		current = brokerRecord{
			Topic: topicName, Partition: 0, Offset: int64(tier),
			Key: published.key, Value: published.value, Headers: published.headers,
		}
	}
}

func TestRecoveryAdapterPublishesReplayOnlyToOwningGroupsFirstRetry(t *testing.T) {
	for _, test := range []struct {
		name       string
		dlq        TopicID
		firstRetry TopicID
		group      ConsumerGroupID
	}{
		{
			name: "feed", dlq: TopicFeedVideoPublishedDLQ,
			firstRetry: TopicFeedVideoPublishedRetry5s,
			group:      GroupFeedVideoPublishedActive,
		},
		{
			name: "embedding", dlq: TopicEmbeddingVideoPublishedDLQ,
			firstRetry: TopicEmbeddingVideoRetry5s,
			group:      GroupEmbeddingVideoPublishedActive,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			fake := &fakeSyncProducer{}
			backbone := &Backbone{publisher: &Publisher{
				producer: fake, prefix: "dev", timeout: time.Second,
			}}
			adapter := NewRecoveryAdapter(backbone, infraconfig.KafkaConfig{TopicPrefix: "dev"})
			dlq, err := TopicName("dev", test.dlq)
			if err != nil {
				t.Fatal(err)
			}
			route, err := adapter.RouteForDLQ(dlq)
			if err != nil {
				t.Fatal(err)
			}
			key, value := recoveryBusinessRecord(t)
			now := time.Now().UTC().Add(-time.Hour).Truncate(time.Millisecond)
			record := domainkafkafailure.RetainedRecord{
				Coordinate: domainkafkafailure.Coordinate{
					Topic: dlq, Partition: 2, Offset: 41,
				},
				Key: key, Value: value,
				Metadata: domainkafkafailure.RecoveryMetadata{
					SourceTopic: route.SourceTopic, SourcePartition: 1, SourceOffset: 29,
					EventID: "event-video-42", SchemaVersion: 1,
					ConsumerGroup: route.ConsumerGroup, Attempt: 6, Tier: 0,
					FailureClass:   string(FailureLocalRetryExhausted),
					FirstFailureAt: now, LatestFailureAt: now,
					NotBefore: now, PayloadSHA256: PayloadSHA256(value),
				},
			}
			replayID := "replay-0123456789abcdef0123456789abcdef"
			if err := adapter.PublishReplay(
				context.Background(), route, record, replayID,
			); err != nil {
				t.Fatal(err)
			}
			if fake.calls != 1 || fake.record == nil ||
				string(fake.record.Key) != string(key) ||
				string(fake.record.Value) != string(value) {
				t.Fatalf("produced calls=%d record=%+v", fake.calls, fake.record)
			}
			firstRetry, err := TopicName("dev", test.firstRetry)
			if err != nil {
				t.Fatal(err)
			}
			source, err := TopicName("dev", TopicVideoPublished)
			if err != nil {
				t.Fatal(err)
			}
			if fake.record.Topic != firstRetry || fake.record.Topic == source {
				t.Fatalf("replay topic=%q source=%q", fake.record.Topic, source)
			}
			topicSpec, err := Topic(test.firstRetry)
			if err != nil {
				t.Fatal(err)
			}
			if len(topicSpec.AllowedGroups) != 1 || topicSpec.AllowedGroups[0] != test.group {
				t.Fatalf("retry groups=%v", topicSpec.AllowedGroups)
			}
			metadata, err := DecodeRecoveryHeaders(
				"dev", test.firstRetry, recordHeaders(fake.record), key, value,
			)
			if err != nil || metadata.Attempt != 1 || metadata.Tier != 1 ||
				metadata.ReplayID != replayID ||
				metadata.SourcePartition != 1 || metadata.SourceOffset != 29 {
				t.Fatalf("metadata=%+v err=%v", metadata, err)
			}
		})
	}
}

func TestRecoveryAdapterPreservesUncertainReplayAcknowledgement(t *testing.T) {
	fake := &fakeSyncProducer{
		results: kgo.ProduceResults{{Err: kerr.RequestTimedOut}},
	}
	backbone := &Backbone{publisher: &Publisher{
		producer: fake, prefix: "dev", timeout: time.Second,
	}}
	adapter := NewRecoveryAdapter(backbone, infraconfig.KafkaConfig{TopicPrefix: "dev"})
	dlq, err := TopicName("dev", TopicFeedVideoPublishedDLQ)
	if err != nil {
		t.Fatal(err)
	}
	route, err := adapter.RouteForDLQ(dlq)
	if err != nil {
		t.Fatal(err)
	}
	key, value := recoveryBusinessRecord(t)
	now := time.Now().UTC().Truncate(time.Millisecond)
	record := domainkafkafailure.RetainedRecord{
		Coordinate: domainkafkafailure.Coordinate{
			Topic: dlq, Partition: 2, Offset: 41,
		},
		Key: key, Value: value,
		Metadata: domainkafkafailure.RecoveryMetadata{
			SourceTopic: route.SourceTopic, SourcePartition: 1, SourceOffset: 29,
			EventID: "event-video-42", SchemaVersion: 1,
			ConsumerGroup: route.ConsumerGroup, Attempt: route.MaxAttempt, Tier: 0,
			FailureClass:   string(FailureLocalRetryExhausted),
			FirstFailureAt: now, LatestFailureAt: now,
			NotBefore: now, PayloadSHA256: PayloadSHA256(value),
		},
	}
	err = adapter.PublishReplay(
		context.Background(), route, record,
		"replay-0123456789abcdef0123456789abcdef",
	)
	if !errors.Is(err, domainkafkafailure.ErrReplayPublishUncertain) ||
		!applicationeventstream.MayHaveTransportAcknowledgement(err) ||
		fake.calls != 1 {
		t.Fatalf("error=%v calls=%d", err, fake.calls)
	}
}

func TestRecoveryAdapterVerifiesReplayEvidenceAndBoundedAbsence(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	adapter := NewRecoveryAdapter(nil, infraconfig.KafkaConfig{TopicPrefix: "dev"})
	adapter.sleep = func(context.Context, time.Duration) error { return nil }
	dlq, err := TopicName("dev", TopicFeedVideoPublishedDLQ)
	if err != nil {
		t.Fatal(err)
	}
	route, err := adapter.RouteForDLQ(dlq)
	if err != nil {
		t.Fatal(err)
	}
	key, value := recoveryBusinessRecord(t)
	replayID := "replay-0123456789abcdef0123456789abcdef"
	metadata := RecoveryMetadata{
		SourceTopic: route.SourceTopic, SourcePartition: 1, SourceOffset: 29,
		EventID: "event-video-42", SchemaVersion: 1,
		ConsumerGroup: GroupFeedVideoPublishedActive,
		Attempt:       1, Tier: 1, FailureClass: FailureTerminalDomain,
		FirstFailureAt: now, LatestFailureAt: now,
		NotBefore: now.Add(5 * time.Second), PayloadSHA256: PayloadSHA256(value),
		ReplayID: replayID,
	}
	headers, err := EncodeRecoveryHeaders(
		"dev", TopicFeedVideoPublishedRetry5s, metadata, key, value,
	)
	if err != nil {
		t.Fatal(err)
	}
	backend := &fakeDLQInspectionBackend{
		offsets: map[string]map[int32]dlqPartitionOffsets{
			route.ReplayTopic: {0: {Start: 10, End: 11, RecentStart: 10}},
		},
		records: []brokerRecord{{
			Topic: route.ReplayTopic, Partition: 0, Offset: 10,
			Timestamp: now, Key: key, Value: value, Headers: headers,
		}},
	}
	adapter.backbone = &Backbone{dlqInspector: testDLQInspector(backend, now)}
	evidence, err := adapter.VerifyReplay(
		context.Background(), route, replayID, now.Add(-time.Minute),
	)
	if err != nil ||
		evidence.Status != domainkafkafailure.ReplayEvidenceFound ||
		evidence.ReplayID != replayID ||
		evidence.DestinationTopic != route.ReplayTopic ||
		evidence.PayloadSHA256 != PayloadSHA256(value) {
		t.Fatalf("evidence=%+v err=%v", evidence, err)
	}

	unrelatedMetadata := metadata
	unrelatedMetadata.ReplayID = "replay-fedcba9876543210fedcba9876543210"
	unrelatedHeaders, err := EncodeRecoveryHeaders(
		"dev", TopicFeedVideoPublishedRetry5s, unrelatedMetadata, key, value,
	)
	if err != nil {
		t.Fatal(err)
	}
	backend.records = []brokerRecord{{
		Topic: route.ReplayTopic, Partition: 0, Offset: 11,
		Timestamp: now, Key: key, Value: value, Headers: unrelatedHeaders,
	}}
	backend.offsets[route.ReplayTopic][0] = dlqPartitionOffsets{
		Start: 11, End: 12, RecentStart: 11,
	}
	evidence, err = adapter.VerifyReplay(
		context.Background(), route, replayID, now.Add(-time.Hour),
	)
	if err != nil || evidence.Status != domainkafkafailure.ReplayEvidenceAbsent ||
		backend.readCalls != 3 {
		t.Fatalf(
			"absence=%+v err=%v reads=%d",
			evidence, err, backend.readCalls,
		)
	}
	_, err = adapter.VerifyReplay(
		context.Background(), route, replayID, now.Add(-8*24*time.Hour),
	)
	if !errors.Is(err, domainkafkafailure.ErrReplayEvidenceExpired) {
		t.Fatalf("expired error=%v", err)
	}
}

func TestRecoveryAdapterReplayEvidenceGrowthRestartsSettlementAndFindsAppend(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	adapter, route, key, value, headers := replayEvidenceFixture(
		t, now, "replay-0123456789abcdef0123456789abcdef",
	)
	adapter.sleep = func(context.Context, time.Duration) error { return nil }
	unrelated := brokerRecord{
		Topic: route.ReplayTopic, Partition: 0, Offset: 10,
		Timestamp: now, Key: key, Value: value,
	}
	match := brokerRecord{
		Topic: route.ReplayTopic, Partition: 0, Offset: 11,
		Timestamp: now, Key: key, Value: value, Headers: headers,
	}
	backend := &fakeDLQInspectionBackend{
		offsetSequence: []map[string]map[int32]dlqPartitionOffsets{
			{route.ReplayTopic: {0: {Start: 10, End: 11, RecentStart: 10}}},
			{route.ReplayTopic: {0: {Start: 10, End: 12, RecentStart: 10}}},
		},
		recordSequence: [][]brokerRecord{{unrelated}, {unrelated, match}},
	}
	adapter.backbone = &Backbone{dlqInspector: testDLQInspector(backend, now)}
	evidence, err := adapter.VerifyReplay(
		context.Background(),
		route,
		"replay-0123456789abcdef0123456789abcdef",
		now.Add(-time.Hour),
	)
	if err != nil || evidence.Status != domainkafkafailure.ReplayEvidenceFound ||
		backend.offsetCalls != 2 {
		t.Fatalf(
			"evidence=%+v err=%v offset_calls=%d",
			evidence,
			err,
			backend.offsetCalls,
		)
	}
}

func TestRecoveryAdapterReplayEvidenceStableOnlyAfterUncertaintyWindow(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	current := now
	adapter, route, _, _, _ := replayEvidenceFixture(
		t, now, "replay-0123456789abcdef0123456789abcdef",
	)
	adapter.uncertaintyWindow = 2 * time.Second
	adapter.settlementWindow = 5 * time.Second
	adapter.settlementInterval = time.Second
	adapter.sleep = func(_ context.Context, duration time.Duration) error {
		current = current.Add(duration)
		return nil
	}
	backend := &fakeDLQInspectionBackend{
		offsets: map[string]map[int32]dlqPartitionOffsets{
			route.ReplayTopic: {0: {Start: 10, End: 10, RecentStart: 10}},
		},
	}
	inspector := testDLQInspector(backend, now)
	inspector.now = func() time.Time { return current }
	adapter.backbone = &Backbone{dlqInspector: inspector}

	evidence, err := adapter.VerifyReplay(
		context.Background(),
		route,
		"replay-0123456789abcdef0123456789abcdef",
		now,
	)
	if err != nil || evidence.Status != domainkafkafailure.ReplayEvidenceAbsent ||
		backend.offsetCalls != 4 {
		t.Fatalf(
			"evidence=%+v err=%v offset_calls=%d current=%s",
			evidence,
			err,
			backend.offsetCalls,
			current,
		)
	}
}

func TestRecoveryAdapterReplayEvidenceUnstableBoundsRemainUnavailable(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	current := now
	adapter, route, _, _, _ := replayEvidenceFixture(
		t, now, "replay-0123456789abcdef0123456789abcdef",
	)
	adapter.uncertaintyWindow = 0
	adapter.settlementWindow = 2 * time.Second
	adapter.settlementInterval = time.Second
	adapter.sleep = func(_ context.Context, duration time.Duration) error {
		current = current.Add(duration)
		return nil
	}
	backend := &fakeDLQInspectionBackend{
		offsetSequence: []map[string]map[int32]dlqPartitionOffsets{
			{route.ReplayTopic: {0: {Start: 10, End: 10, RecentStart: 10}}},
			{route.ReplayTopic: {0: {Start: 10, End: 11, RecentStart: 10}}},
			{route.ReplayTopic: {0: {Start: 10, End: 12, RecentStart: 10}}},
		},
	}
	inspector := testDLQInspector(backend, now)
	inspector.now = func() time.Time { return current }
	adapter.backbone = &Backbone{dlqInspector: inspector}

	evidence, err := adapter.VerifyReplay(
		context.Background(),
		route,
		"replay-0123456789abcdef0123456789abcdef",
		now.Add(-time.Hour),
	)
	if evidence.Status != "" ||
		!errors.Is(err, domainkafkafailure.ErrReplayEvidenceUnavailable) ||
		backend.offsetCalls != 3 {
		t.Fatalf(
			"evidence=%+v err=%v offset_calls=%d",
			evidence,
			err,
			backend.offsetCalls,
		)
	}
}

func TestRecoveryAdapterReplayEvidenceCancellationRemainsPending(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	adapter, route, _, _, _ := replayEvidenceFixture(
		t, now, "replay-0123456789abcdef0123456789abcdef",
	)
	adapter.uncertaintyWindow = time.Hour
	backend := &fakeDLQInspectionBackend{
		offsets: map[string]map[int32]dlqPartitionOffsets{
			route.ReplayTopic: {0: {Start: 10, End: 10, RecentStart: 10}},
		},
	}
	adapter.backbone = &Backbone{dlqInspector: testDLQInspector(backend, now)}
	ctx, cancel := context.WithCancel(context.Background())
	adapter.sleep = func(context.Context, time.Duration) error {
		cancel()
		return context.Canceled
	}

	evidence, err := adapter.VerifyReplay(
		ctx,
		route,
		"replay-0123456789abcdef0123456789abcdef",
		now,
	)
	if evidence.Status != "" || !errors.Is(err, context.Canceled) {
		t.Fatalf("evidence=%+v err=%v", evidence, err)
	}
}

func TestRecoveryAdapterReplayEvidenceSkipsMalformedRecordBeforeMatch(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	adapter, route, key, value, headers := replayEvidenceFixture(
		t, now, "replay-0123456789abcdef0123456789abcdef",
	)
	backend := &fakeDLQInspectionBackend{
		offsets: map[string]map[int32]dlqPartitionOffsets{
			route.ReplayTopic: {0: {Start: 10, End: 12, RecentStart: 10}},
		},
		records: []brokerRecord{
			{
				Topic: route.ReplayTopic, Partition: 0, Offset: 10,
				Timestamp: now, Key: key, Value: value,
				Headers: []applicationeventstream.Header{{
					Key: RecoveryHeaderKey, Value: []byte("{"),
				}},
			},
			{
				Topic: route.ReplayTopic, Partition: 0, Offset: 11,
				Timestamp: now, Key: key, Value: value, Headers: headers,
			},
		},
	}
	adapter.backbone = &Backbone{dlqInspector: testDLQInspector(backend, now)}

	evidence, err := adapter.VerifyReplay(
		context.Background(), route,
		"replay-0123456789abcdef0123456789abcdef",
		now.Add(-time.Minute),
	)
	if err != nil ||
		evidence.Status != domainkafkafailure.ReplayEvidenceFound ||
		evidence.ReplayID != "replay-0123456789abcdef0123456789abcdef" {
		t.Fatalf("evidence=%+v err=%v", evidence, err)
	}
}

func TestRecoveryAdapterReplayEvidenceMalformedWithoutMatchIsUnavailable(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	adapter, route, key, value, _ := replayEvidenceFixture(
		t, now, "replay-0123456789abcdef0123456789abcdef",
	)
	backend := &fakeDLQInspectionBackend{
		offsets: map[string]map[int32]dlqPartitionOffsets{
			route.ReplayTopic: {0: {Start: 10, End: 11, RecentStart: 10}},
		},
		records: []brokerRecord{{
			Topic: route.ReplayTopic, Partition: 0, Offset: 10,
			Timestamp: now, Key: key, Value: value,
			Headers: []applicationeventstream.Header{{
				Key: RecoveryHeaderKey, Value: []byte("{"),
			}},
		}},
	}
	adapter.backbone = &Backbone{dlqInspector: testDLQInspector(backend, now)}

	evidence, err := adapter.VerifyReplay(
		context.Background(), route,
		"replay-0123456789abcdef0123456789abcdef",
		now.Add(-time.Minute),
	)
	if evidence.Status != "" ||
		!errors.Is(err, domainkafkafailure.ErrReplayEvidenceUnavailable) {
		t.Fatalf("evidence=%+v err=%v", evidence, err)
	}
}

func replayEvidenceFixture(
	t *testing.T,
	now time.Time,
	replayID string,
) (
	*RecoveryAdapter,
	domainkafkafailure.RecoveryRoute,
	[]byte,
	[]byte,
	[]applicationeventstream.Header,
) {
	t.Helper()
	adapter := NewRecoveryAdapter(nil, infraconfig.KafkaConfig{TopicPrefix: "dev"})
	dlq, err := TopicName("dev", TopicFeedVideoPublishedDLQ)
	if err != nil {
		t.Fatal(err)
	}
	route, err := adapter.RouteForDLQ(dlq)
	if err != nil {
		t.Fatal(err)
	}
	key, value := recoveryBusinessRecord(t)
	headers, err := EncodeRecoveryHeaders(
		"dev",
		TopicFeedVideoPublishedRetry5s,
		RecoveryMetadata{
			SourceTopic: route.SourceTopic, SourcePartition: 1, SourceOffset: 29,
			EventID: "event-video-42", SchemaVersion: 1,
			ConsumerGroup: GroupFeedVideoPublishedActive,
			Attempt:       1, Tier: 1, FailureClass: FailureTerminalDomain,
			FirstFailureAt: now, LatestFailureAt: now,
			NotBefore: now.Add(5 * time.Second), PayloadSHA256: PayloadSHA256(value),
			ReplayID: replayID,
		},
		key,
		value,
	)
	if err != nil {
		t.Fatal(err)
	}
	return adapter, route, key, value, headers
}
