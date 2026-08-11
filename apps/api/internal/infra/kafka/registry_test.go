package infrakafka

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestRegistryIsClosedAndNamesAreStable(t *testing.T) {
	topics := Topics()
	if len(topics) != 17 {
		t.Fatalf("topic count = %d, want 17", len(topics))
	}

	name, err := TopicName("dev", TopicBackboneProbe)
	if err != nil {
		t.Fatal(err)
	}

	if name != "dev.frux.platform.backbone_probe.v1" {
		t.Fatalf("topic name = %q", name)
	}
	group, err := ResolvedGroupName("", "blue", GroupBackboneProbeShadow)
	if err != nil {
		t.Fatal(err)
	}
	if group != "frux.platform.backbone_probe.active.v1.shadow.blue" {
		t.Fatalf("group name = %q", group)
	}
	if _, err := Topic("arbitrary"); err == nil {
		t.Fatal("unknown topic was accepted")
	}
	if _, err := GroupName("invalid prefix!", GroupBackboneProbeActive); err == nil {
		t.Fatal("invalid prefix was accepted")
	}
}

func TestRecoveryRegistryPoliciesAndFixedTiersAreClosed(t *testing.T) {
	wantPolicies := map[ConsumerGroupID]RecoveryPolicy{
		GroupBackboneProbeActive:           RecoveryBlockAndRetry,
		GroupPersistActionActive:           RecoveryBlockAndRetry,
		GroupConsumeViewActive:             RecoveryBlockAndRetry,
		GroupFeedVideoPublishedActive:      RecoveryRetryTopics,
		GroupEmbeddingVideoPublishedActive: RecoveryRetryTopics,
		GroupMediaProcessingActive:         RecoveryDurableJob,
	}
	if len(Recoveries()) != len(wantPolicies) {
		t.Fatalf("recovery count = %d", len(Recoveries()))
	}
	for group, want := range wantPolicies {
		spec, err := Recovery(group)
		if err != nil {
			t.Fatal(err)
		}
		if spec.Policy != want || spec.LocalRetry.MaxAttempts < 1 ||
			spec.LocalRetry.MaxTotalDelay <= 0 {
			t.Fatalf("recovery %s = %+v", group, spec)
		}
	}
	for _, shadow := range []ConsumerGroupID{
		GroupBackboneProbeShadow,
		GroupPersistActionShadow,
		GroupConsumeViewShadow,
		GroupFeedVideoPublishedShadow,
		GroupEmbeddingVideoPublishedShadow,
		GroupMediaProcessingShadow,
	} {
		if _, err := Recovery(shadow); err == nil {
			t.Fatalf("shadow group %s received a recovery policy", shadow)
		}
	}

	wantDelays := []time.Duration{
		5 * time.Second, 30 * time.Second, 2 * time.Minute,
		10 * time.Minute, 30 * time.Minute,
	}
	for _, group := range []ConsumerGroupID{
		GroupFeedVideoPublishedActive,
		GroupEmbeddingVideoPublishedActive,
	} {
		spec, err := Recovery(group)
		if err != nil {
			t.Fatal(err)
		}
		if len(spec.RetryTiers) != len(wantDelays) ||
			spec.DLQTopic == "" || spec.DLQRetention != 30*24*time.Hour ||
			spec.ReplayDestination != ReplayToFirstRetry {
			t.Fatalf("retry recovery %s = %+v", group, spec)
		}
		for index, tier := range spec.RetryTiers {
			topic, err := Topic(tier.Topic)
			if err != nil {
				t.Fatal(err)
			}
			if tier.Tier != index+1 || tier.Delay != wantDelays[index] ||
				topic.Retention != 7*24*time.Hour ||
				topic.MessageTimestamp != MessageTimestampLogAppendTime {
				t.Fatalf("tier %d = %+v topic=%+v", index, tier, topic)
			}
		}
		dlq, err := Topic(spec.DLQTopic)
		if err != nil {
			t.Fatal(err)
		}
		if !dlq.ReplayAllowed || dlq.Retention != spec.DLQRetention {
			t.Fatalf("DLQ = %+v", dlq)
		}
	}
}

func TestRecoveryNamesAndDLQAllowlistArePrefixAware(t *testing.T) {
	name, err := TopicName("stage", TopicFeedVideoPublishedRetry5s)
	if err != nil {
		t.Fatal(err)
	}
	if name != "stage.frux.feed.video-published.retry-5s.v1" {
		t.Fatalf("retry topic name = %q", name)
	}
	group, err := RecoveryConsumerGroupName("stage", GroupFeedVideoPublishedActive, 1)
	if err != nil {
		t.Fatal(err)
	}
	if group != "stage.frux.feed.video-published.v1.recovery.5s" {
		t.Fatalf("retry group name = %q", group)
	}
	dlq, err := TopicName("stage", TopicFeedVideoPublishedDLQ)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := DLQTopicAllowed("stage", dlq)
	if err != nil || spec.Group != GroupFeedVideoPublishedActive {
		t.Fatalf("allowlisted DLQ spec=%+v err=%v", spec, err)
	}
	if _, err := DLQTopicAllowed("stage", "stage.frux.video.published.v1"); err == nil {
		t.Fatal("business topic was accepted as a DLQ")
	}
}

func TestVideoWorkflowRegistryContractsAreStable(t *testing.T) {
	publication, err := Topic(TopicVideoPublished)
	if err != nil {
		t.Fatal(err)
	}
	if publication.BaseName != "frux.video.published.v1" ||
		publication.KeyKind != KeyKindVideoID ||
		publication.Retention != 30*24*time.Hour ||
		!publication.ReplayAllowed ||
		publication.MessageTimestamp != MessageTimestampLogAppendTime {
		t.Fatalf("publication topic = %+v", publication)
	}
	media, err := Topic(TopicMediaProcessingRequested)
	if err != nil {
		t.Fatal(err)
	}
	if media.BaseName != "frux.media.processing-requested.v1" ||
		media.Class != TopicClassCommand ||
		media.KeyKind != KeyKindAssetID ||
		media.Retention != 6*time.Hour {
		t.Fatalf("media topic = %+v", media)
	}
	feed, _ := GroupName("", GroupFeedVideoPublishedActive)
	embedding, _ := GroupName("", GroupEmbeddingVideoPublishedActive)
	if feed != "frux.feed.video-published.v1" ||
		embedding != "frux.embedding.video-published.v1" ||
		feed == embedding {
		t.Fatalf("independent groups = %q %q", feed, embedding)
	}
}

func TestBehaviorRegistryContractsAreStable(t *testing.T) {
	action, err := Topic(TopicActionChanged)
	if err != nil {
		t.Fatal(err)
	}
	if action.BaseName != "frux.interaction.action-changed.v1" ||
		action.KeyKind != KeyKindActionState ||
		action.Retention != 7*24*time.Hour ||
		action.CleanupPolicy != CleanupDelete ||
		action.MessageTimestamp != MessageTimestampLogAppendTime {
		t.Fatalf("action topic = %+v", action)
	}
	view, err := Topic(TopicViewEventRecorded)
	if err != nil {
		t.Fatal(err)
	}
	if view.BaseName != "frux.exposure.view-event-recorded.v1" ||
		view.KeyKind != KeyKindUserID ||
		view.Retention != 7*24*time.Hour ||
		view.MessageTimestamp != MessageTimestampLogAppendTime {
		t.Fatalf("view topic = %+v", view)
	}
	actionGroup, _ := ResolvedGroupName("", "green", GroupPersistActionShadow)
	viewGroup, _ := ResolvedGroupName("", "green", GroupConsumeViewShadow)
	if actionGroup != "frux.interaction.persist-action.v1.shadow.green" ||
		viewGroup != "frux.recommendation.consume-view.v1.shadow.green" {
		t.Fatalf("shadow groups = %q %q", actionGroup, viewGroup)
	}
}

func TestRetainedEventTopicsUseBrokerAppendTime(t *testing.T) {
	for _, topic := range Topics() {
		if topic.Class == TopicClassEvent &&
			topic.MessageTimestamp != MessageTimestampLogAppendTime {
			t.Fatalf("topic %s timestamp type = %q", topic.ID, topic.MessageTimestamp)
		}

	}
}

func TestProducerBatchBoundsMatchResolvedTopicsConcurrently(t *testing.T) {
	const prefix = "batch-bounds"
	batchMaxBytes, err := producerBatchMaxBytesFn(prefix)
	if err != nil {
		t.Fatal(err)
	}
	topics := Topics()
	var conservative int32
	for _, topic := range topics {
		limit := int32(brokerMaxMessageBytes(topic))
		if conservative == 0 || limit < conservative {
			conservative = limit
		}
		name, err := TopicName(prefix, topic.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got := batchMaxBytes(name); got != limit {
			t.Fatalf("batch max for %s = %d, want %d", topic.ID, got, limit)
		}
	}
	if got := batchMaxBytes("unregistered.topic"); got != conservative {
		t.Fatalf("unknown topic batch max = %d, want conservative %d", got, conservative)
	}

	var wait sync.WaitGroup
	errs := make(chan string, len(topics)*32)
	for iteration := 0; iteration < 32; iteration++ {
		for _, topic := range topics {
			topic := topic
			wait.Add(1)
			go func() {
				defer wait.Done()
				name, nameErr := TopicName(prefix, topic.ID)
				if nameErr != nil {
					errs <- nameErr.Error()
					return
				}
				want := int32(brokerMaxMessageBytes(topic))
				if got := batchMaxBytes(name); got != want {
					errs <- fmt.Sprintf("%s=%d want %d", topic.ID, got, want)
				}
			}()
		}
	}
	wait.Wait()
	close(errs)
	for message := range errs {
		t.Error(message)
	}
}

func TestRecoveryTopicsInheritSourceRecordAndHeaderCapacity(t *testing.T) {
	source, err := Topic(TopicVideoPublished)
	if err != nil {
		t.Fatal(err)
	}
	for _, recovery := range Recoveries() {
		if recovery.Policy != RecoveryRetryTopics {
			continue
		}
		topics := make([]TopicID, 0, len(recovery.RetryTiers)+1)
		for _, tier := range recovery.RetryTiers {
			topics = append(topics, tier.Topic)
		}
		topics = append(topics, recovery.DLQTopic)
		for _, topicID := range topics {
			topic, topicErr := Topic(topicID)
			if topicErr != nil {
				t.Fatal(topicErr)
			}
			want := recoveryMaxRecordBytes(source)
			if topic.RecoverySource != source.ID || topic.MaxRecordBytes != want {
				t.Fatalf(
					"recovery topic %s source=%s max=%d want=%d",
					topicID,
					topic.RecoverySource,
					topic.MaxRecordBytes,
					want,
				)
			}
		}
	}
}

func TestMigrationRegistryKeepsRabbitMQActiveByDefault(t *testing.T) {
	for _, spec := range Migrations() {
		if spec.DefaultProducer != ProducerModeRabbit || spec.DefaultConsumer != ConsumerModeRabbit {
			t.Fatalf("unsafe default for %s: %+v", spec.Responsibility, spec)
		}
	}
	if ValidProducerMode("dual") || ValidConsumerMode("rabbit_and_kafka") {
		t.Fatal("unregistered dual-active mode was accepted")
	}
}
