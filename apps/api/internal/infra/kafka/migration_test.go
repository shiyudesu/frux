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

func TestBehaviorMigrationModesAreAvailable(t *testing.T) {
	for _, stream := range []infraconfig.KafkaStreamMigrationConfig{
		{ProducerMode: "rabbit_with_kafka_mirror", ConsumerMode: "rabbit"},
		{ProducerMode: "rabbit_with_kafka_mirror", ConsumerMode: "kafka_shadow"},
	} {
		_, err := MigrationPlan(infraconfig.KafkaConfig{
			Enabled: true,
			Migration: infraconfig.KafkaMigrationConfig{
				ActionChanged: stream,
			},
		})
		if err != nil {
			t.Fatalf("behavior migration rejected: %+v: %v", stream, err)
		}
	}
}

func TestMigrationPlanRejectsProducerConsumerGaps(t *testing.T) {
	tests := []infraconfig.KafkaStreamMigrationConfig{
		{ProducerMode: "kafka", ConsumerMode: "rabbit"},
		{ProducerMode: "rabbit", ConsumerMode: "kafka_shadow"},
		{
			ProducerMode: "rabbit_with_kafka_mirror", ConsumerMode: "kafka",
			CutoverBoundary: "2026-08-09T00:00:00Z",
		},
	}
	for _, stream := range tests {
		_, err := MigrationPlan(infraconfig.KafkaConfig{
			Enabled: true,
			Migration: infraconfig.KafkaMigrationConfig{
				ViewEventRecorded: stream,
			},
		})
		if err == nil {
			t.Fatalf("unsafe stream pair accepted: %+v", stream)
		}
	}
}

func TestMigrationPlanEnforcesViewFirstAndCutoverBoundary(t *testing.T) {
	_, err := MigrationPlan(infraconfig.KafkaConfig{
		Enabled: true,
		Migration: infraconfig.KafkaMigrationConfig{
			ActionChanged: infraconfig.KafkaStreamMigrationConfig{
				ProducerMode: "rabbit", ConsumerMode: "kafka",
				CutoverBoundary: "2026-08-09T01:00:00Z",
			},
		},
	})
	if err == nil {
		t.Fatal("action cutover before view was accepted")
	}
	_, err = MigrationPlan(infraconfig.KafkaConfig{
		Enabled: true,
		Migration: infraconfig.KafkaMigrationConfig{
			ViewEventRecorded: infraconfig.KafkaStreamMigrationConfig{
				ProducerMode: "kafka", ConsumerMode: "kafka",
				CutoverBoundary: "2026-08-09T02:00:00Z",
			},
			ActionChanged: infraconfig.KafkaStreamMigrationConfig{
				ProducerMode: "kafka", ConsumerMode: "kafka",
				CutoverBoundary: "2026-08-09T01:00:00Z",
			},
		},
	})
	if err == nil {
		t.Fatal("action boundary before view boundary was accepted")
	}
	plan, err := MigrationPlan(infraconfig.KafkaConfig{
		Enabled: true,
		Migration: infraconfig.KafkaMigrationConfig{
			ViewEventRecorded: infraconfig.KafkaStreamMigrationConfig{
				ProducerMode: "kafka", ConsumerMode: "kafka",
				CutoverBoundary: "2026-08-09T00:00:00Z",
			},
			ActionChanged: infraconfig.KafkaStreamMigrationConfig{
				ProducerMode: "kafka", ConsumerMode: "kafka",
				CutoverBoundary: "2026-08-09T01:00:00Z",
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	action, _ := MigrationFor(plan, ResponsibilityActionChanged)
	if action.CutoverBoundary != "2026-08-09T01:00:00Z" {
		t.Fatalf("boundary = %q", action.CutoverBoundary)
	}
}

func TestNonBehaviorKafkaMigrationRemainsUnavailable(t *testing.T) {
	_, err := MigrationPlan(infraconfig.KafkaConfig{
		Enabled: true,
		Migration: infraconfig.KafkaMigrationConfig{
			VideoPublished: infraconfig.KafkaStreamMigrationConfig{
				ProducerMode: "kafka", ConsumerMode: "rabbit",
			},
		},
	})
	if err == nil {
		t.Fatal("video Kafka migration was accepted")
	}
}
