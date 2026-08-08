package infrakafka

import (
	"fmt"

	infraconfig "github.com/shiyudesu/frux/internal/infra/config"
)

type StreamMigration struct {
	Responsibility ResponsibilityID
	Producer       ProducerMode
	Consumer       ConsumerMode
}

func MigrationPlan(cfg infraconfig.KafkaConfig) ([]StreamMigration, error) {
	configured := map[ResponsibilityID]infraconfig.KafkaStreamMigrationConfig{
		ResponsibilityActionChanged:     cfg.Migration.ActionChanged,
		ResponsibilityVideoPublished:    cfg.Migration.VideoPublished,
		ResponsibilityVideoEmbedding:    cfg.Migration.VideoEmbedding,
		ResponsibilityViewEventRecorded: cfg.Migration.ViewEventRecorded,
		ResponsibilityMediaProcessing:   cfg.Migration.MediaProcessing,
	}
	plan := make([]StreamMigration, 0, len(migrations))
	for _, registered := range migrations {
		stream := configured[registered.Responsibility]
		producer := ProducerMode(stream.ProducerMode)
		consumer := ConsumerMode(stream.ConsumerMode)
		if producer == "" {
			producer = registered.DefaultProducer
		}
		if consumer == "" {
			consumer = registered.DefaultConsumer
		}
		if !ValidProducerMode(producer) || !ValidConsumerMode(consumer) {
			return nil, fmt.Errorf("%w: migration mode", ErrUnknownRegistryValue)
		}
		if producer != ProducerModeRabbit && !registered.KafkaProducerAvailable {
			return nil, fmt.Errorf(
				"%w: Kafka producer is not implemented for %s",
				ErrUnknownRegistryValue,
				registered.Responsibility,
			)
		}
		if consumer != ConsumerModeRabbit && !registered.KafkaConsumerAvailable {
			return nil, fmt.Errorf(
				"%w: Kafka consumer is not implemented for %s",
				ErrUnknownRegistryValue,
				registered.Responsibility,
			)
		}
		if !cfg.Enabled && (producer != ProducerModeRabbit || consumer != ConsumerModeRabbit) {
			return nil, fmt.Errorf("%w: Kafka migration while disabled", ErrUnknownRegistryValue)
		}
		plan = append(plan, StreamMigration{
			Responsibility: registered.Responsibility,
			Producer:       producer, Consumer: consumer,
		})
	}
	return plan, nil
}

func RabbitMQActiveFoundation(plan []StreamMigration) bool {
	if len(plan) != len(migrations) {
		return false
	}
	for _, stream := range plan {
		if stream.Producer != ProducerModeRabbit || stream.Consumer != ConsumerModeRabbit {
			return false
		}
	}
	return true
}
