package infrakafka

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	applicationeventstream "github.com/shiyudesu/frux/internal/application/eventstream"
	domainkafkafailure "github.com/shiyudesu/frux/internal/domain/kafkafailure"
	infraconfig "github.com/shiyudesu/frux/internal/infra/config"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
)

const (
	integrationInitialRetryDelay = 3 * time.Second
	integrationNextRetryDelay    = 250 * time.Millisecond
)

func TestKafkaFailureRecoveryIntegration(t *testing.T) {
	brokersValue := strings.TrimSpace(os.Getenv("FRUX_KAFKA_TEST_BROKERS"))
	if brokersValue == "" {
		t.Skip("FRUX_KAFKA_TEST_BROKERS is not set; run against the Compose listener at 127.0.0.1:29092")
	}
	prefix := fmt.Sprintf("recoveryitest%d", time.Now().UnixNano())
	cfg := integrationKafkaConfig(strings.Split(brokersValue, ","), prefix)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	backbone, err := Start(ctx, cfg, nil, nil)
	if err != nil {
		t.Fatalf("start Kafka backbone: %v", err)
	}
	t.Cleanup(func() {
		cleanupIntegrationKafka(t, backbone, prefix)
	})
	admin := kadm.NewClient(backbone.client.kgoClient)

	t.Run("provisions registered recovery retention", func(t *testing.T) {
		assertIntegrationRecoveryTopology(t, ctx, admin, prefix)
	})

	var feedRecord domainkafkafailure.RetainedRecord
	t.Run("routes acknowledged retries through delay tiers and inspects exact DLQ records", func(t *testing.T) {
		feedRecord = runIntegrationRetryFlow(t, ctx, cfg, backbone, admin)
	})

	t.Run("routes terminal poison records to the registered DLQ", func(t *testing.T) {
		runIntegrationPoisonFlow(t, ctx, cfg, backbone, admin)
	})

	t.Run("routes broker accepted video records above 96 KiB with topic bounds", func(t *testing.T) {
		runIntegrationLargeVideoRecordFlow(t, ctx, cfg, backbone, admin)
	})

	t.Run("enforces the smaller media source broker boundary", func(t *testing.T) {
		runIntegrationSmallSourceBoundary(t, ctx, cfg)
	})

	t.Run("publishes concurrent small and large topic batches", func(t *testing.T) {
		runIntegrationConcurrentTopicBatchPublication(t, ctx, cfg, backbone)
	})

	t.Run("replays unchanged bytes without deleting the DLQ record", func(t *testing.T) {
		runIntegrationReplayFlow(t, ctx, cfg, backbone, feedRecord)
	})
}

func runIntegrationConcurrentTopicBatchPublication(
	t *testing.T,
	ctx context.Context,
	cfg infraconfig.KafkaConfig,
	backbone *Backbone,
) {
	t.Helper()
	type publication struct {
		topic TopicID
		key   []byte
		value []byte
	}
	publications := []publication{
		{
			topic: TopicMediaProcessingRequested,
			key:   []byte("asset:concurrent-small"),
			value: integrationBoundaryBytes(4 << 10),
		},
		{
			topic: TopicBackboneProbe,
			key:   []byte("probe:concurrent-large"),
			value: integrationBoundaryBytes(128 << 10),
		},
	}
	start := make(chan struct{})
	results := make(chan error, len(publications))
	var wait sync.WaitGroup
	for _, publication := range publications {
		publication := publication
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			topic, err := TopicName(cfg.TopicPrefix, publication.topic)
			if err == nil {
				_, err = produceRecordSync(
					ctx,
					backbone.client.kgoClient,
					backbone.client.produceTimeout,
					&kgo.Record{
						Topic: topic,
						Key:   publication.key,
						Value: publication.value,
					},
				)
			}
			results <- err
		}()
	}
	close(start)
	wait.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatalf("concurrent topic publication: %v", err)
		}
	}
}

func TestKafkaRetryOffsetInitializationIntegration(t *testing.T) {
	brokersValue := strings.TrimSpace(os.Getenv("FRUX_KAFKA_TEST_BROKERS"))
	if brokersValue == "" {
		t.Skip("FRUX_KAFKA_TEST_BROKERS is not set; run against the Compose listener at 127.0.0.1:29092")
	}
	prefix := fmt.Sprintf("retryoffsetitest%d", time.Now().UnixNano())
	cfg := integrationKafkaConfig(strings.Split(brokersValue, ","), prefix)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	backbone, err := Start(ctx, cfg, nil, nil)
	if err != nil {
		t.Fatalf("start Kafka backbone: %v", err)
	}
	t.Cleanup(func() {
		cleanupIntegrationKafka(t, backbone, prefix)
	})
	admin := kadm.NewClient(backbone.client.kgoClient)
	topicID := TopicEmbeddingVideoRetry5s
	topic, err := TopicName(prefix, topicID)
	if err != nil {
		t.Fatal(err)
	}
	group, err := RecoveryConsumerGroupName(
		prefix, GroupEmbeddingVideoPublishedActive, 1,
	)
	if err != nil {
		t.Fatal(err)
	}
	retryOffsetStore := newMemoryRetryOffsetStore()
	waitIntegrationCondition(t, ctx, "retry topic metadata", func() (bool, error) {
		starts, listErr := admin.ListStartOffsets(ctx, topic)
		return len(starts[topic]) > 0, errors.Join(listErr, starts.Error())
	})
	produced, err := produceRecordSync(
		ctx,
		backbone.client.kgoClient,
		backbone.client.produceTimeout,
		&kgo.Record{
			Topic: topic, Key: []byte("video:offset-init"),
			Value: []byte(`{"retained":true}`),
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	first := newIntegrationRetryOffsetConsumer(
		t, ctx, cfg, GroupEmbeddingVideoPublishedActive, 1, retryOffsetStore,
	)
	closeIntegrationConsumer(t, first)
	assertIntegrationOffsetsAtStarts(t, ctx, admin, group, topic)

	offsets := make(kadm.Offsets)
	offsets.Add(kadm.Offset{
		Topic: topic, Partition: produced.Partition, At: produced.Offset + 1,
		LeaderEpoch: -1,
	})
	if err := admin.CommitAllOffsets(ctx, group, offsets); err != nil {
		t.Fatal(err)
	}
	second := newIntegrationRetryOffsetConsumer(
		t, ctx, cfg, GroupEmbeddingVideoPublishedActive, 1, retryOffsetStore,
	)
	closeIntegrationConsumer(t, second)
	if offset, err := integrationCommittedOffset(
		ctx, admin, group, topic, produced.Partition,
	); err != nil || offset != produced.Offset+1 {
		t.Fatalf("restart offset=%d error=%v", offset, err)
	}

	responses, err := admin.CreatePartitions(ctx, 1, topic)
	if err != nil || responses.Error() != nil {
		t.Fatalf("add retry partition: %v", errors.Join(err, responses.Error()))
	}
	waitIntegrationCondition(t, ctx, "retry partition addition", func() (bool, error) {
		starts, listErr := admin.ListStartOffsets(ctx, topic)
		return len(starts[topic]) == 13, errors.Join(listErr, starts.Error())
	})
	third := newIntegrationRetryOffsetConsumer(
		t, ctx, cfg, GroupEmbeddingVideoPublishedActive, 1, retryOffsetStore,
	)
	closeIntegrationConsumer(t, third)
	starts, err := admin.ListStartOffsets(ctx, topic)
	if err != nil || starts.Error() != nil {
		t.Fatal(errors.Join(err, starts.Error()))
	}
	addedStart, found := starts.Lookup(topic, 12)
	if !found {
		t.Fatal("added retry partition start offset is missing")
	}
	if offset, err := integrationCommittedOffset(
		ctx, admin, group, topic, 12,
	); err != nil || offset != addedStart.Offset {
		t.Fatalf("added partition offset=%d start=%d error=%v", offset, addedStart.Offset, err)
	}
	if offset, err := integrationCommittedOffset(
		ctx, admin, group, topic, produced.Partition,
	); err != nil || offset != produced.Offset+1 {
		t.Fatalf("existing partition changed to %d error=%v", offset, err)
	}

	rewound := make(kadm.Offsets)
	rewound.Add(kadm.Offset{
		Topic: topic, Partition: produced.Partition, At: produced.Offset,
		LeaderEpoch: -1,
	})
	if err := admin.CommitAllOffsets(ctx, group, rewound); err != nil {
		t.Fatal(err)
	}
	deletions := make(kadm.Offsets)
	deletions.Add(kadm.Offset{
		Topic: topic, Partition: produced.Partition, At: produced.Offset + 1,
	})
	deleted, err := admin.DeleteRecords(ctx, deletions)
	if err != nil || deleted.Error() != nil {
		t.Fatalf("advance retry log start: %v", errors.Join(err, deleted.Error()))
	}
	_, err = NewRetryTierConsumer(
		ctx,
		cfg,
		GroupEmbeddingVideoPublishedActive,
		1,
		handlerFunc(func(
			context.Context,
			applicationeventstream.Event,
		) (applicationeventstream.Outcome, error) {
			return applicationeventstream.OutcomeDurableSuccess, nil
		}),
		nil,
		WithRetryOffsetInitializationStore(retryOffsetStore),
	)
	if !errors.Is(err, ErrConsumerDataLoss) {
		t.Fatalf("out-of-range retry offset error=%v", err)
	}

	missingTopicID := TopicEmbeddingVideoRetry30s
	missingTopic, err := TopicName(prefix, missingTopicID)
	if err != nil {
		t.Fatal(err)
	}
	missingGroup, err := RecoveryConsumerGroupName(
		prefix, GroupEmbeddingVideoPublishedActive, 2,
	)
	if err != nil {
		t.Fatal(err)
	}
	missingConsumer := newIntegrationRetryOffsetConsumer(
		t, ctx, cfg, GroupEmbeddingVideoPublishedActive, 2, retryOffsetStore,
	)
	closeIntegrationConsumer(t, missingConsumer)
	var deletedOffsets kadm.TopicsSet
	deletedOffsets.Add(missingTopic, 0)
	deletedResponse, err := admin.DeleteOffsets(ctx, missingGroup, deletedOffsets)
	if err != nil || deletedResponse.Error() != nil {
		t.Fatalf("delete established retry offset: %v", errors.Join(err, deletedResponse.Error()))
	}
	_, err = NewRetryTierConsumer(
		ctx,
		cfg,
		GroupEmbeddingVideoPublishedActive,
		2,
		handlerFunc(func(
			context.Context,
			applicationeventstream.Event,
		) (applicationeventstream.Outcome, error) {
			return applicationeventstream.OutcomeDurableSuccess, nil
		}),
		nil,
		WithRetryOffsetInitializationStore(retryOffsetStore),
	)
	if !errors.Is(err, ErrConsumerDataLoss) {
		t.Fatalf("missing established retry offset error=%v", err)
	}
}

func runIntegrationLargeVideoRecordFlow(
	t *testing.T,
	ctx context.Context,
	cfg infraconfig.KafkaConfig,
	backbone *Backbone,
	admin *kadm.Client,
) {
	t.Helper()
	sourceTopic, err := TopicName(cfg.TopicPrefix, TopicVideoPublished)
	if err != nil {
		t.Fatal(err)
	}
	groupName, err := GroupName(cfg.TopicPrefix, GroupEmbeddingVideoPublishedActive)
	if err != nil {
		t.Fatal(err)
	}
	observer := newIntegrationConsumerObserver()
	consumer, err := NewConsumer(
		ctx,
		cfg,
		GroupEmbeddingVideoPublishedActive,
		handlerFunc(func(
			context.Context,
			applicationeventstream.Event,
		) (applicationeventstream.Outcome, error) {
			t.Fatal("oversized malformed source record must route before handler invocation")
			return "", nil
		}),
		observer,
	)
	if err != nil {
		t.Fatal(err)
	}
	tuneIntegrationRecovery(consumer, integrationNextRetryDelay)
	run := startIntegrationConsumer(t, ctx, consumer)
	waitIntegrationAssignment(t, ctx, consumer)
	waitIntegrationGroupCaughtUp(t, ctx, admin, groupName, sourceTopic)

	key, err := EncodeKey(KeyKindVideoID, VideoKey{VideoID: 42003})
	if err != nil {
		t.Fatal(err)
	}
	sourceSpec, err := Topic(TopicVideoPublished)
	if err != nil {
		t.Fatal(err)
	}
	value, produced := integrationBrokerRecordBoundary(
		t, ctx, cfg, sourceSpec, sourceTopic, key,
	)
	if len(key)+len(value) <= sourceSpec.MaxRecordBytes {
		t.Fatalf("broker boundary record bytes=%d", len(key)+len(value))
	}
	waitIntegrationGroupCaughtUp(t, ctx, admin, groupName, sourceTopic)
	if err := stopIntegrationRun(ctx, run); err != nil {
		t.Fatalf("stop large-record consumer: %v", err)
	}

	dlqTopic, err := TopicName(cfg.TopicPrefix, TopicEmbeddingVideoPublishedDLQ)
	if err != nil {
		t.Fatal(err)
	}
	records, err := integrationReadTopicRecords(ctx, cfg, admin, dlqTopic)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, record := range records {
		metadata, decodeErr := DecodeRecoveryHeaders(
			cfg.TopicPrefix,
			TopicEmbeddingVideoPublishedDLQ,
			record.Headers,
			record.Key,
			record.Value,
		)
		if decodeErr == nil &&
			metadata.SourcePartition == produced.Partition &&
			metadata.SourceOffset == produced.Offset {
			if !bytes.Equal(record.Key, key) || !bytes.Equal(record.Value, value) {
				t.Fatal("large DLQ record changed key/value")
			}
			found = true
			break
		}
	}
	if !found {
		t.Fatal("large broker-accepted video record was not retained in the DLQ")
	}

	headers := []applicationeventstream.Header{{
		Key: RecoveryHeaderKey, Value: []byte(`{"bounded":true}`),
	}}
	err = backbone.Publisher().PublishRecovery(
		ctx,
		TopicEmbeddingVideoPublishedDLQ,
		key,
		bytes.Repeat(
			[]byte("x"),
			brokerMaxMessageBytes(sourceSpec)-len(key)+1,
		),
		headers,
	)
	if !errors.Is(err, ErrContractFailure) {
		t.Fatalf("above source broker record error=%v", err)
	}
}

func runIntegrationSmallSourceBoundary(
	t *testing.T,
	ctx context.Context,
	cfg infraconfig.KafkaConfig,
) {
	t.Helper()
	topicSpec, err := Topic(TopicMediaProcessingRequested)
	if err != nil {
		t.Fatal(err)
	}
	topic, err := TopicName(cfg.TopicPrefix, topicSpec.ID)
	if err != nil {
		t.Fatal(err)
	}
	key, err := EncodeKey(KeyKindAssetID, AssetKey{AssetID: 42004})
	if err != nil {
		t.Fatal(err)
	}
	value, _ := integrationBrokerRecordBoundary(
		t, ctx, cfg, topicSpec, topic, key,
	)
	if len(key)+len(value) <= topicSpec.MaxRecordBytes {
		t.Fatalf("smaller broker boundary record bytes=%d", len(key)+len(value))
	}
}

func integrationBrokerRecordBoundary(
	t *testing.T,
	ctx context.Context,
	cfg infraconfig.KafkaConfig,
	spec TopicSpec,
	topic string,
	key []byte,
) ([]byte, ProduceMetadata) {
	t.Helper()
	options, err := clientOptions(cfg)
	if err != nil {
		t.Fatal(err)
	}
	options = append(options, kgo.ProducerBatchCompression(kgo.NoCompression()))
	client, err := kgo.NewClient(options...)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if err := client.Ping(ctx); err != nil {
		t.Fatal(err)
	}

	low := spec.MaxRecordBytes - len(key)
	high := brokerMaxMessageBytes(spec) + brokerRecordHeadroomBytes - len(key)
	var accepted ProduceMetadata
	for high-low > 1 {
		candidate := low + (high-low)/2
		value := integrationBoundaryBytes(candidate)
		produced, produceErr := produceRecordSync(
			ctx,
			client,
			10*time.Second,
			&kgo.Record{Topic: topic, Key: key, Value: value},
		)
		if produceErr == nil {
			low = candidate
			accepted = produced
			continue
		}
		if !errors.Is(produceErr, kerr.MessageTooLarge) {
			t.Fatalf("boundary produce bytes=%d: %v", candidate, produceErr)
		}
		high = candidate
	}
	if low < spec.MaxRecordBytes-len(key) || accepted.Offset < 0 {
		t.Fatalf("no broker-accepted boundary record for %s", spec.ID)
	}
	if len(key)+low > brokerMaxMessageBytes(spec) {
		t.Fatalf(
			"broker accepted %d source bytes above configured max %d",
			len(key)+low,
			brokerMaxMessageBytes(spec),
		)
	}
	_, err = produceRecordSync(
		ctx,
		client,
		10*time.Second,
		&kgo.Record{
			Topic: topic, Key: key, Value: integrationBoundaryBytes(low + 1),
		},
	)
	if !errors.Is(err, kerr.MessageTooLarge) {
		t.Fatalf("above broker boundary bytes=%d error=%v", low+1, err)
	}
	_, err = produceRecordSync(
		ctx,
		client,
		10*time.Second,
		&kgo.Record{
			Topic: topic,
			Key:   key,
			Value: integrationBoundaryBytes(
				brokerMaxMessageBytes(spec) - len(key) + 1,
			),
		},
	)
	if !errors.Is(err, kerr.MessageTooLarge) {
		t.Fatalf(
			"above configured source broker max bytes=%d error=%v",
			brokerMaxMessageBytes(spec)+1,
			err,
		)
	}
	return integrationBoundaryBytes(low), accepted
}

func integrationBoundaryBytes(size int) []byte {
	value := make([]byte, size)
	var state uint32 = 0x9e3779b9
	for index := range value {
		state ^= state << 13
		state ^= state >> 17
		state ^= state << 5
		value[index] = byte(state)
	}
	return value
}

func newIntegrationRetryOffsetConsumer(
	t *testing.T,
	ctx context.Context,
	cfg infraconfig.KafkaConfig,
	group ConsumerGroupID,
	tier int,
	store RetryOffsetInitializationStore,
) *Consumer {
	t.Helper()
	consumer, err := NewRetryTierConsumer(
		ctx,
		cfg,
		group,
		tier,
		handlerFunc(func(
			context.Context,
			applicationeventstream.Event,
		) (applicationeventstream.Outcome, error) {
			return applicationeventstream.OutcomeDurableSuccess, nil
		}),
		nil,
		WithRetryOffsetInitializationStore(store),
	)
	if err != nil {
		t.Fatal(err)
	}
	return consumer
}

func closeIntegrationConsumer(t *testing.T, consumer *Consumer) {
	t.Helper()
	closeContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := consumer.source.Close(closeContext); err != nil {
		t.Fatal(err)
	}
}

func assertIntegrationOffsetsAtStarts(
	t *testing.T,
	ctx context.Context,
	admin *kadm.Client,
	group string,
	topic string,
) {
	t.Helper()
	starts, err := admin.ListStartOffsets(ctx, topic)
	if err != nil || starts.Error() != nil {
		t.Fatal(errors.Join(err, starts.Error()))
	}
	committed, err := admin.FetchOffsetsForTopics(ctx, group, topic)
	if err != nil || committed.Error() != nil {
		t.Fatal(errors.Join(err, committed.Error()))
	}
	for partition, start := range starts[topic] {
		offset, found := committed.Lookup(topic, partition)
		if !found || offset.Err != nil || offset.At != start.Offset {
			t.Fatalf(
				"partition=%d committed=%+v retained_start=%d",
				partition, offset, start.Offset,
			)
		}
	}
}

func runIntegrationRetryFlow(
	t *testing.T,
	ctx context.Context,
	cfg infraconfig.KafkaConfig,
	backbone *Backbone,
	admin *kadm.Client,
) domainkafkafailure.RetainedRecord {
	t.Helper()
	initializeIntegrationCutover(
		t, ctx, backbone, GroupFeedVideoPublishedActive,
		time.Now().UTC().Add(-time.Second),
	)
	sourceTopic, err := TopicName(cfg.TopicPrefix, TopicVideoPublished)
	if err != nil {
		t.Fatal(err)
	}
	groupName, err := GroupName(cfg.TopicPrefix, GroupFeedVideoPublishedActive)
	if err != nil {
		t.Fatal(err)
	}
	key, err := EncodeKey(KeyKindVideoID, VideoKey{VideoID: 42001})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	eventID := fmt.Sprintf("recovery-integration-%d", now.UnixNano())
	metadata := EventMetadata{
		EventID: eventID, Type: EventTypeVideoPublished, SchemaVersion: 1,
		OccurredAt: now, ProducedAt: now, Producer: ProducerVideoWorker,
	}
	payload := VideoPublishedPayload{
		EventID: eventID, VideoID: 42001, AuthorID: 7,
		Title: "Kafka recovery integration", Description: "unchanged retry payload",
		MediaURL: "/uploads/recovery.mp4", CoverURL: "/uploads/recovery.jpg",
		PublishedAt: now.Add(-time.Minute), OccurredAt: now,
	}
	value, err := EncodeEvent(TopicVideoPublished, key, metadata, payload)
	if err != nil {
		t.Fatal(err)
	}

	retryRuntimes := make([]*integrationRetryRuntime, 0, 5)
	for tier := 2; tier <= 5; tier++ {
		retryRuntimes = append(retryRuntimes, startIntegrationRetryConsumer(
			t, ctx, cfg, tier, key, value,
		))
	}

	var sourceCalls atomic.Int32
	handler := handlerFunc(func(
		context.Context,
		applicationeventstream.Event,
	) (applicationeventstream.Outcome, error) {
		sourceCalls.Add(1)
		return applicationeventstream.OutcomeRetryable, errors.New("integration retryable failure")
	})

	first, err := NewConsumer(
		ctx, cfg, GroupFeedVideoPublishedActive, handler, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	tuneIntegrationRecovery(first, integrationInitialRetryDelay)
	firstCheck := make(chan integrationPublicationCheck, 1)
	first.recoveryWriter = &integrationOffsetCheckingPublisher{
		delegate: first.recoveryWriter, admin: admin, prefix: cfg.TopicPrefix,
		groupName: groupName, checks: firstCheck,
	}
	first.source = &integrationCommitFailingSource{
		consumerSource: first.source, err: errors.New("simulated commit response loss"),
	}
	firstRun := startIntegrationConsumer(t, ctx, first)
	waitIntegrationAssignment(t, ctx, first)

	produced, err := backbone.Publisher().Publish(
		ctx, TopicVideoPublished, key, metadata, payload,
	)
	if err != nil {
		t.Fatalf("publish source event: %v", err)
	}
	if produced.Partition < 0 || produced.Offset < 0 {
		t.Fatalf("invalid source coordinate: %+v", produced)
	}
	firstErr := waitIntegrationRun(t, ctx, firstRun)
	if !errors.Is(firstErr, ErrCommitUncertain) {
		t.Fatalf("first consumer error = %v, want %v", firstErr, ErrCommitUncertain)
	}
	assertIntegrationPublicationCheck(
		t, ctx, firstCheck, TopicFeedVideoPublishedRetry5s,
		produced.Partition, produced.Offset,
	)

	commitObserver := newIntegrationConsumerObserver()
	second, err := NewConsumer(
		ctx, cfg, GroupFeedVideoPublishedActive, handler, commitObserver,
	)
	if err != nil {
		t.Fatal(err)
	}
	tuneIntegrationRecovery(second, integrationInitialRetryDelay)
	secondCheck := make(chan integrationPublicationCheck, 1)
	second.recoveryWriter = &integrationOffsetCheckingPublisher{
		delegate: second.recoveryWriter, admin: admin, prefix: cfg.TopicPrefix,
		groupName: groupName, checks: secondCheck,
	}
	secondRun := startIntegrationConsumer(t, ctx, second)
	waitIntegrationAssignment(t, ctx, second)
	waitIntegrationCommit(t, ctx, commitObserver)
	assertIntegrationPublicationCheck(
		t, ctx, secondCheck, TopicFeedVideoPublishedRetry5s,
		produced.Partition, produced.Offset,
	)
	if err := stopIntegrationRun(ctx, secondRun); err != nil {
		t.Fatalf("stop restarted source consumer: %v", err)
	}
	committed, err := integrationCommittedOffset(
		ctx, admin, groupName, sourceTopic, produced.Partition,
	)
	if err != nil {
		t.Fatal(err)
	}
	if committed != produced.Offset+1 {
		t.Fatalf("source committed offset = %d, want %d", committed, produced.Offset+1)
	}
	if sourceCalls.Load() != 2 {
		t.Fatalf("source handler calls = %d, want 2 safe duplicate deliveries", sourceCalls.Load())
	}

	firstRetryTopic, err := TopicName(cfg.TopicPrefix, TopicFeedVideoPublishedRetry5s)
	if err != nil {
		t.Fatal(err)
	}
	waitIntegrationTopicCount(t, ctx, admin, firstRetryTopic, 2)
	firstRetryRecords, err := integrationReadTopicRecords(ctx, cfg, admin, firstRetryTopic)
	if err != nil {
		t.Fatal(err)
	}
	if len(firstRetryRecords) != 2 {
		t.Fatalf("first retry records = %d, want 2", len(firstRetryRecords))
	}
	var duplicateIdentity RecoveryMetadata
	for index, record := range firstRetryRecords {
		if !bytes.Equal(record.Key, key) || !bytes.Equal(record.Value, value) {
			t.Fatalf("retry %d changed key/value", index)
		}
		recoveryMetadata, err := DecodeRecoveryHeaders(
			cfg.TopicPrefix, TopicFeedVideoPublishedRetry5s,
			record.Headers, record.Key, record.Value,
		)
		if err != nil {
			t.Fatalf("decode retry %d metadata: %v", index, err)
		}
		if recoveryMetadata.SourcePartition != produced.Partition ||
			recoveryMetadata.SourceOffset != produced.Offset ||
			recoveryMetadata.EventID != eventID ||
			recoveryMetadata.Attempt != 1 ||
			recoveryMetadata.Tier != 1 ||
			recoveryMetadata.PayloadSHA256 != PayloadSHA256(value) {
			t.Fatalf("retry %d metadata = %+v", index, recoveryMetadata)
		}
		if index == 0 {
			duplicateIdentity = recoveryMetadata
			continue
		}
		if recoveryMetadata.EventID != duplicateIdentity.EventID ||
			recoveryMetadata.SourceTopic != duplicateIdentity.SourceTopic ||
			recoveryMetadata.SourcePartition != duplicateIdentity.SourcePartition ||
			recoveryMetadata.SourceOffset != duplicateIdentity.SourceOffset ||
			recoveryMetadata.PayloadSHA256 != duplicateIdentity.PayloadSHA256 {
			t.Fatalf(
				"duplicate recovery identity changed: first=%+v second=%+v",
				duplicateIdentity, recoveryMetadata,
			)
		}
	}
	if !time.Now().UTC().Before(firstRetryRecords[1].Timestamp.Add(integrationInitialRetryDelay)) &&
		!time.Now().UTC().Before(duplicateIdentity.NotBefore) {
		t.Fatalf("first retry delay elapsed before live delay consumer could verify it")
	}

	tierOne := startIntegrationRetryConsumer(t, ctx, cfg, 1, key, value)
	retryRuntimes = append(retryRuntimes, tierOne)

	dlqTopic, err := TopicName(cfg.TopicPrefix, TopicFeedVideoPublishedDLQ)
	if err != nil {
		t.Fatal(err)
	}
	waitIntegrationTopicCount(t, ctx, admin, dlqTopic, 2)
	finalGroup, err := RecoveryConsumerGroupName(
		cfg.TopicPrefix, GroupFeedVideoPublishedActive, 5,
	)
	if err != nil {
		t.Fatal(err)
	}
	finalTopic, err := TopicName(cfg.TopicPrefix, TopicFeedVideoPublishedRetry30m)
	if err != nil {
		t.Fatal(err)
	}
	waitIntegrationGroupCaughtUp(t, ctx, admin, finalGroup, finalTopic)
	for _, runtime := range retryRuntimes {
		if err := stopIntegrationRun(ctx, runtime.run); err != nil {
			t.Fatalf("stop retry tier %d: %v", runtime.tier, err)
		}
		if runtime.calls.Load() != 2 {
			t.Fatalf(
				"retry tier %d handler calls = %d, want 2",
				runtime.tier, runtime.calls.Load(),
			)
		}
		assertNoIntegrationRetryErrors(t, runtime)
	}
	if tierOne.source.pauses.Load() < 1 ||
		tierOne.source.pauses.Load() != tierOne.source.resumes.Load() ||
		tierOne.source.pauses.Load() > tierOne.calls.Load() {
		t.Fatalf(
			"tier 1 pause/resume/calls = %d/%d/%d",
			tierOne.source.pauses.Load(),
			tierOne.source.resumes.Load(),
			tierOne.calls.Load(),
		)
	}

	adapter := NewRecoveryAdapter(backbone, cfg)
	summaries, err := adapter.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var feedSummary *domainkafkafailure.TopicSummary
	for index := range summaries {
		if summaries[index].Topic == dlqTopic {
			feedSummary = &summaries[index]
			break
		}
	}
	if feedSummary == nil || feedSummary.Retention != recoveryDLQRetention ||
		feedSummary.RetainedEstimate < 2 {
		t.Fatalf("feed DLQ summary = %+v", feedSummary)
	}
	var coordinate domainkafkafailure.Coordinate
	for _, partition := range feedSummary.Partitions {
		if partition.RetainedEstimate > 0 {
			coordinate = domainkafkafailure.Coordinate{
				Topic: dlqTopic, Partition: partition.Partition,
				Offset: partition.RetainedStartOffset,
			}
			break
		}
	}
	if coordinate.Topic == "" {
		t.Fatal("feed DLQ summary did not expose a retained coordinate")
	}
	diagnostics, err := adapter.Inspect(
		ctx, coordinate.Topic, coordinate.Partition, coordinate.Offset, 2,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(diagnostics) == 0 || diagnostics[0].Coordinate != coordinate ||
		diagnostics[0].EventID != eventID ||
		diagnostics[0].Attempt != 6 ||
		diagnostics[0].FailureClass != string(FailureLocalRetryExhausted) ||
		diagnostics[0].PayloadSHA256 != PayloadSHA256(value) {
		t.Fatalf("exact DLQ diagnostics = %+v", diagnostics)
	}
	record, err := adapter.Fetch(ctx, coordinate)
	if err != nil {
		t.Fatal(err)
	}
	if record.Coordinate != coordinate ||
		!bytes.Equal(record.Key, key) ||
		!bytes.Equal(record.Value, value) ||
		record.Metadata.EventID != eventID ||
		record.Metadata.Attempt != 6 ||
		record.Metadata.Tier != 0 {
		t.Fatalf("exact fetched DLQ record = %+v", record)
	}
	return record
}

func runIntegrationPoisonFlow(
	t *testing.T,
	ctx context.Context,
	cfg infraconfig.KafkaConfig,
	backbone *Backbone,
	admin *kadm.Client,
) {
	t.Helper()
	initializeIntegrationCutover(
		t, ctx, backbone, GroupEmbeddingVideoPublishedActive, time.Now().UTC(),
	)
	sourceTopic, err := TopicName(cfg.TopicPrefix, TopicVideoPublished)
	if err != nil {
		t.Fatal(err)
	}
	groupName, err := GroupName(cfg.TopicPrefix, GroupEmbeddingVideoPublishedActive)
	if err != nil {
		t.Fatal(err)
	}
	observer := newIntegrationConsumerObserver()
	consumer, err := NewConsumer(
		ctx,
		cfg,
		GroupEmbeddingVideoPublishedActive,
		handlerFunc(func(
			context.Context,
			applicationeventstream.Event,
		) (applicationeventstream.Outcome, error) {
			return applicationeventstream.OutcomeDurableSuccess, nil
		}),
		observer,
	)
	if err != nil {
		t.Fatal(err)
	}
	tuneIntegrationRecovery(consumer, integrationNextRetryDelay)
	checks := make(chan integrationPublicationCheck, 1)
	consumer.recoveryWriter = &integrationOffsetCheckingPublisher{
		delegate: consumer.recoveryWriter, admin: admin, prefix: cfg.TopicPrefix,
		groupName: groupName, checks: checks,
	}
	run := startIntegrationConsumer(t, ctx, consumer)
	waitIntegrationAssignment(t, ctx, consumer)
	waitIntegrationGroupCaughtUp(t, ctx, admin, groupName, sourceTopic)

	_, poison := recoveryBusinessRecord(t)
	key := []byte("video:0")
	produced, err := produceRecordSync(
		ctx,
		backbone.client.kgoClient,
		backbone.client.produceTimeout,
		&kgo.Record{Topic: sourceTopic, Key: key, Value: poison},
	)
	if err != nil {
		t.Fatalf("publish poison record: %v", err)
	}
	waitIntegrationCommit(t, ctx, observer)
	assertIntegrationPublicationCheck(
		t, ctx, checks, TopicEmbeddingVideoPublishedDLQ,
		produced.Partition, produced.Offset,
	)
	waitIntegrationGroupOffset(
		t, ctx, admin, groupName, sourceTopic,
		produced.Partition, produced.Offset+1,
	)
	if err := stopIntegrationRun(ctx, run); err != nil {
		t.Fatalf("stop embedding source consumer: %v", err)
	}

	dlqTopic, err := TopicName(cfg.TopicPrefix, TopicEmbeddingVideoPublishedDLQ)
	if err != nil {
		t.Fatal(err)
	}
	waitIntegrationTopicCount(t, ctx, admin, dlqTopic, 1)
	adapter := NewRecoveryAdapter(backbone, cfg)
	summaries, err := adapter.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var coordinate domainkafkafailure.Coordinate
	for _, summary := range summaries {
		if summary.Topic != dlqTopic {
			continue
		}
		for _, partition := range summary.Partitions {
			if partition.RetainedEstimate > 0 {
				coordinate = domainkafkafailure.Coordinate{
					Topic: dlqTopic, Partition: partition.Partition,
					Offset: partition.RetainedStartOffset,
				}
				break
			}
		}
	}
	if coordinate.Topic == "" {
		t.Fatal("embedding DLQ did not retain the terminal poison record")
	}
	record, err := adapter.Fetch(ctx, coordinate)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(record.Key, key) ||
		!bytes.Equal(record.Value, poison) ||
		record.Metadata.SchemaVersion != 1 ||
		record.Metadata.Tier != 0 ||
		record.Metadata.Attempt != 1 ||
		record.Metadata.FailureClass != string(FailureTerminalContract) ||
		record.Metadata.EventID != "event-video-42" {
		t.Fatalf("terminal poison DLQ record = %+v", record)
	}
}

func runIntegrationReplayFlow(
	t *testing.T,
	ctx context.Context,
	cfg infraconfig.KafkaConfig,
	backbone *Backbone,
	record domainkafkafailure.RetainedRecord,
) {
	t.Helper()
	if record.Coordinate.Topic == "" {
		t.Fatal("retry flow did not return a retained DLQ record")
	}
	adapter := NewRecoveryAdapter(backbone, cfg)
	route, err := adapter.RouteForDLQ(record.Coordinate.Topic)
	if err != nil {
		t.Fatal(err)
	}
	const replayID = "replay-11111111111111111111111111111111"
	requestedAt := time.Now().UTC()
	if err := adapter.PublishReplay(ctx, route, record, replayID); err != nil {
		t.Fatal(err)
	}

	retryRecords, err := integrationReadTopicRecords(
		ctx, cfg, kadm.NewClient(backbone.client.kgoClient), route.ReplayTopic,
	)
	if err != nil {
		t.Fatal(err)
	}
	replays := make([]brokerRecord, 0, 1)
	for _, retryRecord := range retryRecords {
		if !containsRecoveryHeader(retryRecord.Headers) {
			continue
		}
		metadata, err := DecodeRecoveryHeaders(
			cfg.TopicPrefix, TopicFeedVideoPublishedRetry5s,
			retryRecord.Headers, retryRecord.Key, retryRecord.Value,
		)
		if err == nil && metadata.ReplayID == replayID {
			replays = append(replays, retryRecord)
		}
	}
	if len(replays) != 1 ||
		!bytes.Equal(replays[0].Key, record.Key) ||
		!bytes.Equal(replays[0].Value, record.Value) {
		t.Fatalf("replayed source records = %+v", replays)
	}
	replayMetadata, err := DecodeRecoveryHeaders(
		cfg.TopicPrefix, TopicFeedVideoPublishedRetry5s,
		replays[0].Headers, replays[0].Key, replays[0].Value,
	)
	if err != nil ||
		replayMetadata.ReplayID != replayID ||
		replayMetadata.Attempt != 1 ||
		replayMetadata.Tier != route.ReplayTier ||
		replayMetadata.EventID != record.Metadata.EventID ||
		replayMetadata.SourcePartition != record.Metadata.SourcePartition ||
		replayMetadata.SourceOffset != record.Metadata.SourceOffset {
		t.Fatalf("replay metadata = %+v err=%v", replayMetadata, err)
	}
	evidence, err := adapter.VerifyReplay(
		ctx, route, replayID, requestedAt,
	)
	if err != nil ||
		domainkafkafailure.ValidateReplayEvidence(
			route, record, replayID, evidence,
		) != nil {
		t.Fatalf("replay evidence = %+v err=%v", evidence, err)
	}
	retained, err := adapter.Fetch(ctx, record.Coordinate)
	if err != nil {
		t.Fatalf("DLQ record was not retained after replay: %v", err)
	}
	if retained.Coordinate != record.Coordinate ||
		!bytes.Equal(retained.Key, record.Key) ||
		!bytes.Equal(retained.Value, record.Value) {
		t.Fatalf("retained DLQ record changed after replay: %+v", retained)
	}
	absentEvidence, err := adapter.VerifyReplay(
		ctx,
		route,
		"replay-22222222222222222222222222222222",
		time.Now().UTC().Add(-time.Minute),
	)
	if err != nil ||
		absentEvidence.Status != domainkafkafailure.ReplayEvidenceAbsent {
		t.Fatalf("stable replay absence evidence=%+v err=%v", absentEvidence, err)
	}
}

type integrationPublicationCheck struct {
	destination TopicID
	metadata    RecoveryMetadata
	committed   int64
	err         error
}

type integrationOffsetCheckingPublisher struct {
	delegate  recoveryPublisher
	admin     *kadm.Client
	prefix    string
	groupName string
	checks    chan<- integrationPublicationCheck
}

func (p *integrationOffsetCheckingPublisher) PublishRecovery(
	ctx context.Context,
	destination TopicID,
	key, value []byte,
	headers []applicationeventstream.Header,
) error {
	if err := p.delegate.PublishRecovery(ctx, destination, key, value, headers); err != nil {
		return err
	}
	check := integrationPublicationCheck{destination: destination}
	check.metadata, check.err = DecodeRecoveryHeaders(
		p.prefix, destination, headers, key, value,
	)
	if check.err == nil {
		check.committed, check.err = integrationCommittedOffset(
			ctx,
			p.admin,
			p.groupName,
			check.metadata.SourceTopic,
			check.metadata.SourcePartition,
		)
	}
	if check.err == nil && check.committed > check.metadata.SourceOffset {
		check.err = fmt.Errorf(
			"source offset advanced to %d before acknowledged next-hop publication returned for offset %d",
			check.committed, check.metadata.SourceOffset,
		)
	}
	p.checks <- check
	return nil
}

type integrationCommitFailingSource struct {
	consumerSource
	err error
}

func (s *integrationCommitFailingSource) Commit(
	context.Context,
	[]brokerRecord,
) error {
	return s.err
}

type integrationTrackingSource struct {
	consumerSource
	pauses  atomic.Int32
	resumes atomic.Int32
}

func (s *integrationTrackingSource) Pause(topic string, partition int32) {
	s.pauses.Add(1)
	s.consumerSource.Pause(topic, partition)
}

func (s *integrationTrackingSource) Resume(topic string, partition int32) {
	s.resumes.Add(1)
	s.consumerSource.Resume(topic, partition)
}

type integrationRetryRuntime struct {
	tier   int
	run    *integrationConsumerRun
	source *integrationTrackingSource
	calls  atomic.Int32
	errs   chan error
}

func startIntegrationRetryConsumer(
	t *testing.T,
	ctx context.Context,
	cfg infraconfig.KafkaConfig,
	tier int,
	key, value []byte,
) *integrationRetryRuntime {
	t.Helper()
	runtime := &integrationRetryRuntime{
		tier: tier, errs: make(chan error, 4),
	}
	tierSpec, ok := func() (RetryTierSpec, bool) {
		recovery, err := Recovery(GroupFeedVideoPublishedActive)
		if err != nil {
			return RetryTierSpec{}, false
		}
		return recovery.RetryTier(tier)
	}()
	if !ok {
		t.Fatalf("retry tier %d is not registered", tier)
	}
	handler := handlerFunc(func(
		_ context.Context,
		event applicationeventstream.Event,
	) (applicationeventstream.Outcome, error) {
		runtime.calls.Add(1)
		recoveryMetadata, decodeErr := DecodeRecoveryHeaders(
			cfg.TopicPrefix, tierSpec.Topic,
			event.Metadata.Headers, key, value,
		)
		if decodeErr != nil {
			runtime.errs <- decodeErr
		} else if time.Now().UTC().Before(
			recoveryMetadata.NotBefore.Add(-5 * time.Millisecond),
		) {
			runtime.errs <- fmt.Errorf(
				"tier %d handler ran at %s before not_before %s",
				tier, time.Now().UTC(), recoveryMetadata.NotBefore,
			)
		}
		return applicationeventstream.OutcomeRetryable,
			errors.New("integration tier retryable failure")
	})
	var consumer *Consumer
	var err error
	retryOffsetStore := newMemoryRetryOffsetStore()
	for {
		consumer, err = NewRetryTierConsumer(
			ctx,
			cfg,
			GroupFeedVideoPublishedActive,
			tier,
			handler,
			nil,
			WithRetryOffsetInitializationStore(retryOffsetStore),
		)
		if err == nil {
			break
		}
		if !RetryableConsumerError(err) {
			t.Fatalf("retry tier %d: %v", tier, err)
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			t.Fatalf("retry tier %d: %v", tier, errors.Join(err, ctx.Err()))
		case <-timer.C:
		}
	}
	tuneIntegrationRecovery(consumer, integrationNextRetryDelay)
	runtime.source = &integrationTrackingSource{consumerSource: consumer.source}
	consumer.source = runtime.source
	runtime.run = startIntegrationConsumer(t, ctx, consumer)
	waitIntegrationAssignment(t, ctx, consumer)
	return runtime
}

func tuneIntegrationRecovery(consumer *Consumer, delay time.Duration) {
	consumer.recovery.LocalRetry = LocalRetrySpec{MaxAttempts: 1}
	for index := range consumer.recovery.RetryTiers {
		consumer.recovery.RetryTiers[index].Delay = delay
	}
}

type integrationConsumerObserver struct {
	commits chan struct{}
}

func newIntegrationConsumerObserver() *integrationConsumerObserver {
	return &integrationConsumerObserver{commits: make(chan struct{}, 16)}
}

func (*integrationConsumerObserver) ObserveConsume(
	_ TopicID,
	_ ConsumerGroupID,
	_ string,
	_ time.Duration,
	_ time.Duration,
) {
}

func (o *integrationConsumerObserver) ObserveCommit(
	_ TopicID,
	_ ConsumerGroupID,
	result string,
) {
	if result == "success" {
		o.commits <- struct{}{}
	}
}

func (*integrationConsumerObserver) ObserveRebalance(
	_ ConsumerGroupID,
	_ string,
) {
}

func (*integrationConsumerObserver) ObserveContract(
	_ TopicID,
	_ ConsumerGroupID,
	_ ContractFailureCode,
) {
}

func (*integrationConsumerObserver) ObserveLag(
	_ TopicID,
	_ ConsumerGroupID,
	_ ConsumerStage,
	_ int64,
) {
}

func (*integrationConsumerObserver) ObserveDataLoss(
	_ TopicID,
	_ ConsumerGroupID,
) {
}

type integrationConsumerRun struct {
	cancel context.CancelFunc
	done   chan struct{}
	mu     sync.Mutex
	err    error
}

func startIntegrationConsumer(
	t *testing.T,
	parent context.Context,
	consumer *Consumer,
) *integrationConsumerRun {
	t.Helper()
	runContext, cancel := context.WithCancel(parent)
	run := &integrationConsumerRun{cancel: cancel, done: make(chan struct{})}
	go func() {
		err := consumer.Run(runContext)
		run.mu.Lock()
		run.err = err
		run.mu.Unlock()
		close(run.done)
	}()
	t.Cleanup(func() {
		cleanupContext, cleanupCancel := context.WithTimeout(
			context.Background(), 10*time.Second,
		)
		defer cleanupCancel()
		_ = stopIntegrationRun(cleanupContext, run)
	})
	return run
}

func stopIntegrationRun(
	ctx context.Context,
	run *integrationConsumerRun,
) error {
	run.cancel()
	return waitIntegrationRunResult(ctx, run)
}

func waitIntegrationRun(
	t *testing.T,
	ctx context.Context,
	run *integrationConsumerRun,
) error {
	t.Helper()
	err, waitErr := waitIntegrationRunResultWithError(ctx, run)
	if waitErr != nil {
		t.Fatal(waitErr)
	}
	return err
}

func waitIntegrationRunResult(
	ctx context.Context,
	run *integrationConsumerRun,
) error {
	err, waitErr := waitIntegrationRunResultWithError(ctx, run)
	if waitErr != nil {
		return waitErr
	}
	return err
}

func waitIntegrationRunResultWithError(
	ctx context.Context,
	run *integrationConsumerRun,
) (error, error) {
	select {
	case <-run.done:
		run.mu.Lock()
		defer run.mu.Unlock()
		return run.err, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func waitIntegrationAssignment(
	t *testing.T,
	ctx context.Context,
	consumer *Consumer,
) {
	t.Helper()
	select {
	case <-consumer.AssignmentReady():
	case <-ctx.Done():
		t.Fatalf("wait for consumer assignment: %v", ctx.Err())
	}
}

func waitIntegrationCommit(
	t *testing.T,
	ctx context.Context,
	observer *integrationConsumerObserver,
) {
	t.Helper()
	select {
	case <-observer.commits:
	case <-ctx.Done():
		t.Fatalf("wait for offset commit: %v", ctx.Err())
	}
}

func assertIntegrationPublicationCheck(
	t *testing.T,
	ctx context.Context,
	checks <-chan integrationPublicationCheck,
	destination TopicID,
	partition int32,
	offset int64,
) {
	t.Helper()
	select {
	case check := <-checks:
		if check.err != nil {
			t.Fatal(check.err)
		}
		if check.destination != destination ||
			check.metadata.SourcePartition != partition ||
			check.metadata.SourceOffset != offset ||
			check.committed > offset {
			t.Fatalf("publication check = %+v", check)
		}
	case <-ctx.Done():
		t.Fatalf("wait for acknowledged publication check: %v", ctx.Err())
	}
}

func assertNoIntegrationRetryErrors(
	t *testing.T,
	runtime *integrationRetryRuntime,
) {
	t.Helper()
	for {
		select {
		case err := <-runtime.errs:
			t.Fatalf("retry tier %d: %v", runtime.tier, err)
		default:
			return
		}
	}
}

func initializeIntegrationCutover(
	t *testing.T,
	ctx context.Context,
	backbone *Backbone,
	group ConsumerGroupID,
	boundary time.Time,
) {
	t.Helper()
	var lastErr error
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		result, err := backbone.ApplyConsumerCutover(
			ctx,
			group,
			boundary.UTC().Format(time.RFC3339Nano),
			CutoverInitializeOnly,
		)
		if err == nil &&
			(result == CutoverInitialized || result == CutoverPreserved) {
			return
		}
		lastErr = err
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatalf("initialize %s cutover: %v", group, errors.Join(lastErr, ctx.Err()))
		}
	}
}

func assertIntegrationRecoveryTopology(
	t *testing.T,
	ctx context.Context,
	admin *kadm.Client,
	prefix string,
) {
	t.Helper()
	names := make([]string, 0, len(recoveryTopics))
	specs := make(map[string]TopicSpec, len(recoveryTopics))
	for _, spec := range recoveryTopics {
		name, err := TopicName(prefix, spec.ID)
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
		specs[name] = spec
	}
	var states map[string]TopicState
	waitIntegrationCondition(t, ctx, "recovery topic metadata", func() (bool, error) {
		var err error
		states, err = (&franzAdminBackend{client: admin}).TopicStates(ctx, names)
		return len(states) == len(names), err
	})
	for name, spec := range specs {
		state, exists := states[name]
		if !exists {
			t.Fatalf("recovery topic %s was not provisioned", name)
		}
		if state.Retention != spec.Retention ||
			state.CleanupPolicy != CleanupDelete ||
			state.MessageTimestamp != MessageTimestampLogAppendTime {
			t.Fatalf("recovery topic %s state = %+v, spec = %+v", name, state, spec)
		}
		if spec.ReplayAllowed && state.Retention != recoveryDLQRetention {
			t.Fatalf("DLQ %s retention = %s", name, state.Retention)
		}
		if !spec.ReplayAllowed && state.Retention != recoveryRetryRetention {
			t.Fatalf("retry topic %s retention = %s", name, state.Retention)
		}
	}
}

func integrationCommittedOffset(
	ctx context.Context,
	admin *kadm.Client,
	groupName string,
	topic string,
	partition int32,
) (int64, error) {
	offsets, err := admin.FetchOffsetsForTopics(ctx, groupName, topic)
	if err != nil || offsets.Error() != nil {
		return 0, errors.Join(err, offsets.Error())
	}
	offset, found := offsets.Lookup(topic, partition)
	if !found || offset.Err != nil {
		return 0, errors.Join(ErrKafkaUnavailable, offset.Err)
	}
	return offset.At, nil
}

func waitIntegrationGroupOffset(
	t *testing.T,
	ctx context.Context,
	admin *kadm.Client,
	groupName string,
	topic string,
	partition int32,
	want int64,
) {
	t.Helper()
	waitIntegrationCondition(t, ctx, "group offset", func() (bool, error) {
		offset, err := integrationCommittedOffset(
			ctx, admin, groupName, topic, partition,
		)
		return offset >= want, err
	})
}

func waitIntegrationGroupCaughtUp(
	t *testing.T,
	ctx context.Context,
	admin *kadm.Client,
	groupName string,
	topic string,
) {
	t.Helper()
	waitIntegrationCondition(t, ctx, "consumer group catch-up", func() (bool, error) {
		starts, err := admin.ListStartOffsets(ctx, topic)
		if err != nil || starts.Error() != nil {
			return false, errors.Join(err, starts.Error())
		}
		ends, err := admin.ListEndOffsets(ctx, topic)
		if err != nil || ends.Error() != nil {
			return false, errors.Join(err, ends.Error())
		}
		committed, err := admin.FetchOffsetsForTopics(ctx, groupName, topic)
		if err != nil || committed.Error() != nil {
			return false, errors.Join(err, committed.Error())
		}
		for partition, end := range ends[topic] {
			offset, found := committed.Lookup(topic, partition)
			start, startFound := starts.Lookup(topic, partition)
			if !startFound || start.Err != nil {
				return false, nil
			}
			if end.Offset == start.Offset &&
				(!found || offset.Err != nil || offset.At < 0) {
				continue
			}
			if !found || offset.Err != nil || offset.At < end.Offset {
				return false, nil
			}
		}
		return true, nil
	})
}

func waitIntegrationTopicCount(
	t *testing.T,
	ctx context.Context,
	admin *kadm.Client,
	topic string,
	want int64,
) {
	t.Helper()
	waitIntegrationCondition(t, ctx, "topic record count", func() (bool, error) {
		count, err := integrationTopicCount(ctx, admin, topic)
		return count >= want, err
	})
}

func waitIntegrationCondition(
	t *testing.T,
	ctx context.Context,
	description string,
	condition func() (bool, error),
) {
	t.Helper()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	var lastErr error
	for {
		ready, err := condition()
		if err == nil && ready {
			return
		}
		lastErr = err
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatalf("wait for %s: %v", description, errors.Join(lastErr, ctx.Err()))
		}
	}
}

func integrationTopicCount(
	ctx context.Context,
	admin *kadm.Client,
	topic string,
) (int64, error) {
	starts, err := admin.ListStartOffsets(ctx, topic)
	if err != nil || starts.Error() != nil {
		return 0, errors.Join(err, starts.Error())
	}
	ends, err := admin.ListEndOffsets(ctx, topic)
	if err != nil || ends.Error() != nil {
		return 0, errors.Join(err, ends.Error())
	}
	var count int64
	for partition, start := range starts[topic] {
		end, found := ends.Lookup(topic, partition)
		if !found || start.Err != nil || end.Err != nil || end.Offset < start.Offset {
			return 0, ErrKafkaUnavailable
		}
		count += end.Offset - start.Offset
	}
	return count, nil
}

func integrationReadTopicRecords(
	ctx context.Context,
	cfg infraconfig.KafkaConfig,
	admin *kadm.Client,
	topic string,
) ([]brokerRecord, error) {
	backend := &franzDLQInspectionBackend{admin: admin, config: cfg}
	offsets, err := backend.PartitionOffsets(
		ctx, []string{topic}, time.Now().UTC().Add(-time.Hour),
	)
	if err != nil {
		return nil, err
	}
	ranges := make([]dlqReadRange, 0)
	for partition, item := range offsets[topic] {
		if item.End <= item.Start {
			continue
		}
		count := item.End - item.Start
		if count > MaxDLQReadLimit {
			return nil, fmt.Errorf("integration topic %s has %d retained records", topic, count)
		}
		ranges = append(ranges, dlqReadRange{
			Topic: topic, Partition: partition,
			Start: item.Start, End: item.End, Limit: int(count),
		})
	}
	records, err := backend.ReadRecords(ctx, ranges)
	if err != nil {
		return nil, err
	}
	sort.Slice(records, func(left, right int) bool {
		if records[left].Partition != records[right].Partition {
			return records[left].Partition < records[right].Partition
		}
		return records[left].Offset < records[right].Offset
	})
	return records, nil
}

func cleanupIntegrationKafka(
	t *testing.T,
	backbone *Backbone,
	prefix string,
) {
	t.Helper()
	if backbone == nil || backbone.client == nil ||
		backbone.client.kgoClient == nil {
		return
	}
	admin := kadm.NewClient(backbone.client.kgoClient)
	groupContext, cancelGroups := context.WithTimeout(
		context.Background(),
		20*time.Second,
	)
	listed, listErr := admin.ListGroups(groupContext)
	if listErr != nil {
		t.Errorf("list integration Kafka groups: %v", listErr)
	} else {
		known := make(map[string]struct{})
		for _, group := range integrationGroupNames(prefix) {
			known[group] = struct{}{}
		}
		groups := make([]string, 0)
		for _, group := range listed.Groups() {
			if _, ok := known[group]; ok {
				groups = append(groups, group)
			}
		}
		if len(groups) > 0 {
			if responses, err := admin.DeleteGroups(groupContext, groups...); err != nil {
				t.Errorf("delete integration Kafka groups: %v", err)
			} else {
				for _, response := range responses.Sorted() {
					if response.Err != nil &&
						!errors.Is(response.Err, kerr.GroupIDNotFound) {
						t.Errorf(
							"delete integration Kafka group %s: %v",
							response.Group,
							response.Err,
						)
					}
				}
			}
		}
	}
	cancelGroups()
	topics := make([]string, 0, len(Topics()))
	for _, topic := range Topics() {
		name, err := TopicName(prefix, topic.ID)
		if err == nil {
			topics = append(topics, name)
		}
	}
	topicContext, cancelTopics := context.WithTimeout(
		context.Background(),
		20*time.Second,
	)
	if responses, err := admin.DeleteTopics(topicContext, topics...); err != nil {
		t.Errorf("delete integration Kafka topics: %v", err)
	} else {
		for _, response := range responses.Sorted() {
			if response.Err != nil && !errors.Is(response.Err, kerr.UnknownTopicOrPartition) {
				t.Errorf("delete integration Kafka topic %s: %v", response.Topic, response.Err)
			}
		}
	}
	cancelTopics()
	closeContext, cancelClose := context.WithTimeout(
		context.Background(),
		10*time.Second,
	)
	defer cancelClose()
	if err := backbone.Close(closeContext); err != nil {
		t.Errorf("close integration Kafka backbone: %v", err)
	}
}

func integrationGroupNames(prefix string) []string {
	names := make(map[string]struct{})
	for _, group := range ConsumerGroups() {
		if group.Shadow {
			continue
		}
		if name, err := GroupName(prefix, group.ID); err == nil {
			names[name] = struct{}{}
		}
	}
	for _, recovery := range Recoveries() {
		for _, tier := range recovery.RetryTiers {
			if name, err := RecoveryConsumerGroupName(
				prefix, recovery.Group, tier.Tier,
			); err == nil {
				names[name] = struct{}{}
			}
		}
	}
	result := make([]string, 0, len(names))
	for name := range names {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}
