package infrakafka

import (
	"fmt"
	"time"

	infraconfig "github.com/shiyudesu/frux/internal/infra/config"
)

type StreamMigration struct {
	Responsibility  ResponsibilityID
	Producer        ProducerMode
	Consumer        ConsumerMode
	CutoverBoundary string
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
		if stream.CutoverBoundary != "" {
			if _, err := time.Parse(time.RFC3339, stream.CutoverBoundary); err != nil {
				return nil, fmt.Errorf("%w: cutover boundary", ErrUnknownRegistryValue)
			}
		}
		if consumer == ConsumerModeKafka && stream.CutoverBoundary == "" {
			return nil, fmt.Errorf("%w: active Kafka consumer requires cutover boundary", ErrUnknownRegistryValue)
		}
		if registered.KafkaProducerAvailable && registered.KafkaConsumerAvailable &&
			!validStreamPair(producer, consumer) {
			return nil, fmt.Errorf(
				"%w: producer and consumer modes do not share one active delivery path for %s",
				ErrUnknownRegistryValue,
				registered.Responsibility,
			)
		}
		plan = append(plan, StreamMigration{
			Responsibility: registered.Responsibility,
			Producer:       producer, Consumer: consumer, CutoverBoundary: stream.CutoverBoundary,
		})
	}
	action, _ := MigrationFor(plan, ResponsibilityActionChanged)
	view, _ := MigrationFor(plan, ResponsibilityViewEventRecorded)
	if action.Consumer == ConsumerModeKafka && view.Consumer != ConsumerModeKafka {
		return nil, fmt.Errorf("%w: view consumer must cut over before action", ErrUnknownRegistryValue)
	}
	if action.Consumer == ConsumerModeKafka && view.Consumer == ConsumerModeKafka {
		actionBoundary, actionErr := time.Parse(time.RFC3339, action.CutoverBoundary)
		viewBoundary, viewErr := time.Parse(time.RFC3339, view.CutoverBoundary)
		if actionErr != nil || viewErr != nil || !actionBoundary.After(viewBoundary) {
			return nil, fmt.Errorf("%w: action cutover boundary must be strictly after view", ErrUnknownRegistryValue)
		}
	}
	if kafkaPrimaryMode(action.Producer) && !kafkaPrimaryMode(view.Producer) {
		return nil, fmt.Errorf("%w: view producer must cut over before action", ErrUnknownRegistryValue)
	}
	return plan, nil
}

func kafkaPrimaryMode(mode ProducerMode) bool {
	return mode == ProducerModeKafka || mode == ProducerModeKafkaWithRabbitMirror
}

func validStreamPair(producer ProducerMode, consumer ConsumerMode) bool {
	switch consumer {
	case ConsumerModeRabbit:
		return producer == ProducerModeRabbit ||
			producer == ProducerModeRabbitWithKafkaMirror
	case ConsumerModeKafkaShadow:
		return producer == ProducerModeRabbitWithKafkaMirror
	case ConsumerModeKafka:
		return producer == ProducerModeKafka ||
			producer == ProducerModeKafkaWithRabbitMirror
	default:
		return false
	}
}

func MigrationFor(plan []StreamMigration, responsibility ResponsibilityID) (StreamMigration, error) {
	for _, stream := range plan {
		if stream.Responsibility == responsibility {
			return stream, nil
		}
	}
	return StreamMigration{}, fmt.Errorf(
		"%w: migration responsibility %q",
		ErrUnknownRegistryValue,
		responsibility,
	)
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
