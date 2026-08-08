package infrakafka

import (
	"testing"

	infraconfig "github.com/shiyudesu/frux/internal/infra/config"
)

func TestFoundationMigrationPlanKeepsEveryBusinessStreamOnRabbitMQ(t *testing.T) {
	plan, err := MigrationPlan(infraconfig.KafkaConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if !RabbitMQActiveFoundation(plan) {
		t.Fatalf("unsafe foundation plan: %+v", plan)
	}
}

func TestMigrationPlanRejectsUnregisteredDualActiveMode(t *testing.T) {
	_, err := MigrationPlan(infraconfig.KafkaConfig{
		Enabled: true,
		Migration: infraconfig.KafkaMigrationConfig{
			ActionChanged: infraconfig.KafkaStreamMigrationConfig{
				ProducerMode: "rabbit", ConsumerMode: "rabbit_and_kafka",
			},
		},
	})
	if err == nil {
		t.Fatal("dual-active consumer mode was accepted")
	}
}

func TestFoundationRejectsKafkaModesUntilBusinessPathsAreImplemented(t *testing.T) {
	for _, stream := range []infraconfig.KafkaStreamMigrationConfig{
		{ProducerMode: "rabbit_with_kafka_mirror", ConsumerMode: "rabbit"},
		{ProducerMode: "rabbit", ConsumerMode: "kafka_shadow"},
		{ProducerMode: "kafka", ConsumerMode: "kafka"},
	} {
		_, err := MigrationPlan(infraconfig.KafkaConfig{
			Enabled: true,
			Migration: infraconfig.KafkaMigrationConfig{
				ActionChanged: stream,
			},
		})
		if err == nil {
			t.Fatalf("unsupported foundation migration was accepted: %+v", stream)
		}
	}
}
