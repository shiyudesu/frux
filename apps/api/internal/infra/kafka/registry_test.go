package infrakafka

import (
	"testing"
	"time"
)

func TestRegistryIsClosedAndNamesAreStable(t *testing.T) {
	topics := Topics()
	if len(topics) != 3 {
		t.Fatalf("topic count = %d, want 3", len(topics))
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
