package infrakafka

import "testing"

func TestRegistryIsClosedAndNamesAreStable(t *testing.T) {
	topics := Topics()
	if len(topics) != 1 {
		t.Fatalf("topic count = %d, want 1", len(topics))
	}
	name, err := TopicName("dev", TopicBackboneProbe)
	if err != nil {
		t.Fatal(err)
	}
	if name != "dev.frux.platform.backbone_probe.v1" {
		t.Fatalf("topic name = %q", name)
	}
	group, err := GroupName("", GroupBackboneProbeShadow)
	if err != nil {
		t.Fatal(err)
	}
	if group != "frux.platform.backbone_probe.shadow.v1" {
		t.Fatalf("group name = %q", group)
	}
	if _, err := Topic("arbitrary"); err == nil {
		t.Fatal("unknown topic was accepted")
	}
	if _, err := GroupName("invalid prefix!", GroupBackboneProbeActive); err == nil {
		t.Fatal("invalid prefix was accepted")
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
