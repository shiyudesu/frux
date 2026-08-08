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
