package inframetrics

import (
	"strings"
	"testing"
	"time"

	domainkafkafailure "github.com/shiyudesu/frux/internal/domain/kafkafailure"
	infrakafka "github.com/shiyudesu/frux/internal/infra/kafka"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestKafkaMetricsUseOnlyRegisteredBoundedLabels(t *testing.T) {
	observer := KafkaObserver{}
	produce := KafkaProduceTotal.WithLabelValues("unknown", "unknown", "unknown")
	consume := KafkaConsumedTotal.WithLabelValues("unknown", "unknown", "unknown")
	topology := KafkaTopologyValidationTotal.WithLabelValues("unknown", "unknown")
	session := KafkaConsumerSessionTotal.WithLabelValues("unknown", "unknown", "unknown")
	before := []float64{
		testutil.ToFloat64(produce),
		testutil.ToFloat64(consume),
		testutil.ToFloat64(topology),
		testutil.ToFloat64(session),
	}
	observer.ObserveProduce("user-42", "producer-42", "raw broker error", time.Millisecond)
	observer.ObserveConsume("video-42", "group-42", "offset-42", time.Millisecond, time.Second)
	observer.ObserveTopology("topic-42", "arbitrary")
	observer.ObserveConsumerSession("group-42", "stage-42", "raw failure")
	if testutil.ToFloat64(produce)-before[0] != 1 ||
		testutil.ToFloat64(consume)-before[1] != 1 ||
		testutil.ToFloat64(topology)-before[2] != 1 ||
		testutil.ToFloat64(session)-before[3] != 1 {
		t.Fatal("unknown Kafka labels were not folded")
	}
}

func TestKafkaMetricDescriptorsExcludeUnboundedDimensions(t *testing.T) {
	description := KafkaConsumedTotal.WithLabelValues(
		string(infrakafka.TopicBackboneProbe),
		string(infrakafka.GroupBackboneProbeActive),
		"durable_success",
	).Desc().String()
	for _, forbidden := range []string{"event_id", "user_id", "video_id", "partition", "offset", "payload", "error"} {
		if strings.Contains(description, forbidden) {
			t.Fatalf("descriptor contains forbidden label %q: %s", forbidden, description)
		}
	}
}

func TestKafkaRebalanceRestartMarksSessionUnhealthy(t *testing.T) {
	group := infrakafka.GroupPersistActionActive
	groupLabel := kafkaGroupLabel(group)
	observer := KafkaObserver{}
	observer.ObserveConsumerSession(group, infrakafka.ConsumerStageSource, "started")
	if got := testutil.ToFloat64(
		KafkaConsumerSessionHealthy.WithLabelValues(groupLabel, "source"),
	); got != 1 {
		t.Fatalf("started health = %v", got)
	}
	observer.ObserveConsumerSession(
		group, infrakafka.ConsumerStageSource, "rebalance_restart",
	)
	if got := testutil.ToFloat64(
		KafkaConsumerSessionHealthy.WithLabelValues(groupLabel, "source"),
	); got != 0 {
		t.Fatalf("rebalance health = %v", got)
	}
}

func TestKafkaWorkflowMetricsAggregateStagesWithoutLastWriterWins(t *testing.T) {
	observer := KafkaObserver{}
	feed := infrakafka.GroupFeedVideoPublishedActive
	feedLabel := kafkaGroupLabel(feed)
	embedding := infrakafka.GroupEmbeddingVideoPublishedActive
	embeddingLabel := kafkaGroupLabel(embedding)
	sourceStage := infrakafka.ConsumerStageSource
	retryStage := infrakafka.ConsumerStage("retry_5s")

	observer.ObserveLag(
		infrakafka.TopicVideoPublished, feed, sourceStage, 275,
	)
	observer.ObserveLag(
		infrakafka.TopicFeedVideoPublishedRetry5s, feed, retryStage, 0,
	)
	if got := testutil.ToFloat64(
		KafkaConsumerWorkflowLag.WithLabelValues(feedLabel),
	); got != 275 {
		t.Fatalf("idle retry tier masked source lag: %v", got)
	}
	if got := testutil.ToFloat64(KafkaConsumerLag.WithLabelValues(
		string(infrakafka.TopicVideoPublished), feedLabel, "source",
	)); got != 275 {
		t.Fatalf("source stage lag=%v", got)
	}
	if got := testutil.ToFloat64(KafkaConsumerLag.WithLabelValues(
		string(infrakafka.TopicFeedVideoPublishedRetry5s), feedLabel, "retry_5s",
	)); got != 0 {
		t.Fatalf("retry stage lag=%v", got)
	}

	observer.ObserveConsumerSession(feed, sourceStage, "started")
	observer.ObserveConsumerSession(feed, retryStage, "started")
	observer.ObserveConsumerSession(feed, sourceStage, "retryable_failure")
	observer.ObserveConsumerSession(feed, retryStage, "started")
	if got := testutil.ToFloat64(
		KafkaConsumerWorkflowHealthy.WithLabelValues(feedLabel),
	); got != 0 {
		t.Fatalf("healthy retry tier masked unhealthy source: %v", got)
	}

	observer.ObserveConsumerSession(embedding, sourceStage, "started")
	if got := testutil.ToFloat64(
		KafkaConsumerWorkflowHealthy.WithLabelValues(embeddingLabel),
	); got != 1 {
		t.Fatalf("unrelated workflow health=%v", got)
	}
	observer.ObserveConsumerSession(feed, sourceStage, "started")
	observer.ObserveConsumerSession(feed, retryStage, "fatal_failure")
	if got := testutil.ToFloat64(
		KafkaConsumerSessionHealthy.WithLabelValues(feedLabel, "retry_5s"),
	); got != 0 {
		t.Fatalf("unhealthy retry tier was not observable: %v", got)
	}
	if got := testutil.ToFloat64(
		KafkaConsumerWorkflowHealthy.WithLabelValues(feedLabel),
	); got != 0 {
		t.Fatalf("unhealthy retry tier did not mark owning workflow: %v", got)
	}
	if got := testutil.ToFloat64(
		KafkaConsumerWorkflowHealthy.WithLabelValues(embeddingLabel),
	); got != 1 {
		t.Fatalf("unhealthy feed tier marked unrelated workflow: %v", got)
	}
}

func TestKafkaFailureRecoveryMetricsFoldLabelsAndExposeRetentionRisk(t *testing.T) {
	observer := KafkaFailureRecoveryObserver{}
	observer.ObserveReplay("actor-42", "raw error")
	if got := testutil.ToFloat64(
		KafkaRecoveryReplayTotal.WithLabelValues("unknown", "unknown"),
	); got < 1 {
		t.Fatalf("unknown replay labels were not folded: %v", got)
	}

	observer.ObserveTopicSummary(domainkafkafailure.TopicSummary{
		Topic:            "dev.frux.feed.video-published.dlq.v1",
		ConsumerGroup:    string(infrakafka.GroupFeedVideoPublishedActive),
		Retention:        100 * time.Second,
		RetainedEstimate: 4,
		EndOffset:        12,
		EndOffsetGrowth:  2,
		OldestRecordAt:   time.Unix(1_700_000_000, 0).UTC(),
		OldestAge:        90 * time.Second,
	})
	topic := string(infrakafka.TopicFeedVideoPublishedDLQ)
	group := string(infrakafka.GroupFeedVideoPublishedActive)
	if got := testutil.ToFloat64(
		KafkaRecoveryRetentionRisk.WithLabelValues(topic, "detected"),
	); got != 1 {
		t.Fatalf("retention risk=%v", got)
	}
	if got := testutil.ToFloat64(
		KafkaRecoveryRetainedEndOffset.WithLabelValues(group, topic),
	); got != 12 {
		t.Fatalf("end offset=%v", got)
	}
	if got := testutil.ToFloat64(
		KafkaRecoveryOldestRecordTimestampSeconds.WithLabelValues(group, topic),
	); got != 1_700_000_000 {
		t.Fatalf("oldest timestamp=%v", got)
	}

	description := KafkaRecoveryReplayTotal.WithLabelValues(group, "succeeded").Desc().String()
	for _, forbidden := range []string{
		"actor", "reason", "partition", "offset", "key", "payload", "error",
	} {
		if strings.Contains(description, forbidden) {
			t.Fatalf("replay descriptor contains forbidden label %q: %s", forbidden, description)
		}
	}

}

func TestKafkaRecoveryNoProgressSignalsCountSuccessfulReplay(t *testing.T) {
	observer := KafkaFailureRecoveryObserver{}
	group := string(infrakafka.GroupFeedVideoPublishedActive)
	topic := string(infrakafka.TopicFeedVideoPublishedDLQ)
	progress := KafkaRecoveryProgressTotal.WithLabelValues(group, "replay")
	beforeProgress := testutil.ToFloat64(progress)
	observer.ObserveTopicSummary(domainkafkafailure.TopicSummary{
		Topic: "dev.frux.feed.video-published.dlq.v1", ConsumerGroup: group,
		RetainedEstimate: 4, EndOffset: 20,
		OldestRecordAt: time.Unix(1_700_000_000, 0).UTC(),
	})
	beforeEnd := testutil.ToFloat64(
		KafkaRecoveryRetainedEndOffset.WithLabelValues(group, topic),
	)
	oldest := testutil.ToFloat64(
		KafkaRecoveryOldestRecordTimestampSeconds.WithLabelValues(group, topic),
	)
	observer.ObserveReplay(group, "succeeded")
	observer.ObserveTopicSummary(domainkafkafailure.TopicSummary{
		Topic: "dev.frux.feed.video-published.dlq.v1", ConsumerGroup: group,
		RetainedEstimate: 5, EndOffset: 21, EndOffsetGrowth: 1,
		OldestRecordAt: time.Unix(1_700_000_000, 0).UTC(),
	})
	endAdvanced := testutil.ToFloat64(
		KafkaRecoveryRetainedEndOffset.WithLabelValues(group, topic),
	) > beforeEnd
	oldestDidNotMove := testutil.ToFloat64(
		KafkaRecoveryOldestRecordTimestampSeconds.WithLabelValues(group, topic),
	) <= oldest
	progressMade := testutil.ToFloat64(progress)-beforeProgress > 0
	if endAdvanced && oldestDidNotMove && !progressMade {
		t.Fatal("successful non-destructive replay would incorrectly alert")
	}
}

func TestKafkaRecoveryNoProgressSignalsDetectSustainedIngress(t *testing.T) {
	observer := KafkaFailureRecoveryObserver{}
	group := string(infrakafka.GroupEmbeddingVideoPublishedActive)
	topic := string(infrakafka.TopicEmbeddingVideoPublishedDLQ)
	progress := KafkaRecoveryProgressTotal.WithLabelValues(group, "replay")
	beforeProgress := testutil.ToFloat64(progress)
	oldestAt := time.Unix(1_700_000_100, 0).UTC()
	observer.ObserveTopicSummary(domainkafkafailure.TopicSummary{
		Topic: "dev.frux.embedding.video-published.dlq.v1", ConsumerGroup: group,
		RetainedEstimate: 6, EndOffset: 30, OldestRecordAt: oldestAt,
	})
	beforeEnd := testutil.ToFloat64(
		KafkaRecoveryRetainedEndOffset.WithLabelValues(group, topic),
	)
	beforeOldest := testutil.ToFloat64(
		KafkaRecoveryOldestRecordTimestampSeconds.WithLabelValues(group, topic),
	)
	observer.ObserveTopicSummary(domainkafkafailure.TopicSummary{
		Topic: "dev.frux.embedding.video-published.dlq.v1", ConsumerGroup: group,
		RetainedEstimate: 9, EndOffset: 33, EndOffsetGrowth: 3,
		OldestRecordAt: oldestAt,
	})
	endAdvanced := testutil.ToFloat64(
		KafkaRecoveryRetainedEndOffset.WithLabelValues(group, topic),
	) > beforeEnd
	oldestDidNotMove := testutil.ToFloat64(
		KafkaRecoveryOldestRecordTimestampSeconds.WithLabelValues(group, topic),
	) <= beforeOldest
	progressMade := testutil.ToFloat64(progress)-beforeProgress > 0
	if !endAdvanced || !oldestDidNotMove || progressMade {
		t.Fatalf(
			"endAdvanced=%t oldestDidNotMove=%t progressMade=%t",
			endAdvanced, oldestDidNotMove, progressMade,
		)
	}
}

func TestKafkaFailureRecoveryCollectionMarksAndClearsStaleGauges(t *testing.T) {
	observer := KafkaFailureRecoveryObserver{}
	failed := KafkaRecoveryCollectionTotal.WithLabelValues("failed")
	before := testutil.ToFloat64(failed)
	observer.ObserveCollection("failed")
	if got := testutil.ToFloat64(KafkaRecoveryMetricsStale); got != 1 {
		t.Fatalf("stale after failure=%v", got)
	}
	if got := testutil.ToFloat64(failed) - before; got != 1 {
		t.Fatalf("failed collections=%v", got)
	}
	observer.ObserveCollection("succeeded")
	if got := testutil.ToFloat64(KafkaRecoveryMetricsStale); got != 0 {
		t.Fatalf("stale after success=%v", got)
	}
}
