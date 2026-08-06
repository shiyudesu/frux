package inframq

import (
	"fmt"
	"strings"
	"time"

	infraconfig "github.com/shiyudesu/frux/internal/infra/config"

	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	ConsumerActionChanged     = "action_changed"
	ConsumerVideoPublished    = "video_published"
	ConsumerVideoEmbedding    = "video_embedding"
	ConsumerViewEventRecorded = "view_event_recorded"
	ConsumerMediaProcessing   = "media_processing"

	MigrationLegacy = "legacy"
	MigrationDual   = "dual"
	MigrationNew    = "new"
)

type queueSpec struct {
	Consumer       string
	Exchange       string
	RoutingKey     string
	LegacyQueue    string
	SourceQueue    string
	DeadExchange   string
	DeadRoutingKey string
	DeadQueue      string
	Critical       bool
	Mode           string
}

func normalizeDeadLetterConfig(cfg *infraconfig.RabbitMQConfig) {
	cfg.ManagementURL = strings.TrimRight(strings.TrimSpace(cfg.ManagementURL), "/")
	cfg.ManagementUsername = strings.TrimSpace(cfg.ManagementUsername)
	cfg.ManagementPassword = strings.TrimSpace(cfg.ManagementPassword)
	cfg.ManagementTimeout = defaultString(cfg.ManagementTimeout, "2s")
	dead := &cfg.DeadLetter
	dead.VersionSuffix = defaultString(dead.VersionSuffix, ".q2")
	dead.ExchangeSuffix = defaultString(dead.ExchangeSuffix, ".dlx.q2")
	dead.QueueSuffix = defaultString(dead.QueueSuffix, ".dlq.q2")
	if dead.DeliveryLimit <= 0 {
		dead.DeliveryLimit = 5
	}
	if dead.SourceMaxLength <= 0 {
		dead.SourceMaxLength = 100_000
	}
	if dead.DeadLetterMaxLength <= 0 {
		dead.DeadLetterMaxLength = 10_000
	}
	if dead.PreviewLimit <= 0 {
		dead.PreviewLimit = 20
	}
	if dead.PreviewLimit > 100 {
		dead.PreviewLimit = 100
	}
	dead.ReplayTimeout = defaultString(dead.ReplayTimeout, "5s")
	dead.ActionChangedMode = migrationMode(dead.ActionChangedMode)
	dead.VideoPublishedMode = migrationMode(dead.VideoPublishedMode)
	dead.VideoEmbeddingMode = migrationMode(dead.VideoEmbeddingMode)
	dead.ViewEventRecordedMode = migrationMode(dead.ViewEventRecordedMode)
	dead.MediaProcessingMode = migrationMode(dead.MediaProcessingMode)
}

func defaultString(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func migrationMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case MigrationDual:
		return MigrationDual
	case MigrationNew:
		return MigrationNew
	default:
		return MigrationLegacy
	}
}

func (r *RabbitMQ) queueSpecs() []queueSpec {
	cfg := r.config
	return []queueSpec{
		newQueueSpec(
			ConsumerActionChanged, cfg.InteractionExchange, cfg.ActionChangedRouting,
			cfg.ActionChangedQueue, cfg.DeadLetter.ActionChangedMode, true, cfg.DeadLetter,
		),
		newQueueSpec(
			ConsumerVideoPublished, cfg.VideoExchange, cfg.VideoPublishedRouting,
			cfg.VideoPublishedQueue, cfg.DeadLetter.VideoPublishedMode, true, cfg.DeadLetter,
		),
		newQueueSpec(
			ConsumerVideoEmbedding, cfg.VideoExchange, cfg.VideoPublishedRouting,
			cfg.VideoEmbeddingQueue, cfg.DeadLetter.VideoEmbeddingMode, false, cfg.DeadLetter,
		),
		newQueueSpec(
			ConsumerViewEventRecorded, cfg.ExposureExchange, cfg.ViewEventRecordedRouting,
			cfg.ViewEventRecordedQueue, cfg.DeadLetter.ViewEventRecordedMode, true, cfg.DeadLetter,
		),
		newQueueSpec(
			ConsumerMediaProcessing, cfg.MediaExchange, cfg.MediaProcessingRouting,
			cfg.MediaProcessingQueue, cfg.DeadLetter.MediaProcessingMode, false, cfg.DeadLetter,
		),
	}
}

func newQueueSpec(
	consumer, exchange, routingKey, legacyQueue, mode string,
	critical bool,
	config infraconfig.RabbitMQDeadLetterConfig,
) queueSpec {
	source := legacyQueue + config.VersionSuffix
	return queueSpec{
		Consumer: consumer, Exchange: exchange, RoutingKey: routingKey,
		LegacyQueue: legacyQueue, SourceQueue: source,
		DeadExchange:   legacyQueue + config.ExchangeSuffix,
		DeadRoutingKey: source + ".dead",
		DeadQueue:      legacyQueue + config.QueueSuffix,
		Critical:       critical, Mode: mode,
	}
}

func (r *RabbitMQ) queueSpec(consumer string) (queueSpec, bool) {
	for _, spec := range r.queueSpecs() {
		if spec.Consumer == consumer {
			return spec, true
		}
	}
	return queueSpec{}, false
}

func (r *RabbitMQ) ensureDeadLetterTopology() error {
	if !r.config.DeadLetter.Enabled {
		return nil
	}
	for _, spec := range r.queueSpecs() {
		if err := r.declareProtectedQueue(spec); err != nil {
			return fmt.Errorf("declare %s dead-letter topology: %w", spec.Consumer, err)
		}
	}
	return nil
}

func (r *RabbitMQ) declareProtectedQueue(spec queueSpec) error {
	channel := r.publishChannel
	if err := channel.ExchangeDeclare(spec.DeadExchange, "direct", true, false, false, false, nil); err != nil {
		return err
	}
	if _, err := channel.QueueDeclare(
		spec.DeadQueue, true, false, false, false,
		amqp.Table{
			"x-queue-type": "quorum",
			"x-max-length": r.config.DeadLetter.DeadLetterMaxLength,
			"x-overflow":   "reject-publish",
		},
	); err != nil {
		return err
	}
	if err := channel.QueueBind(spec.DeadQueue, spec.DeadRoutingKey, spec.DeadExchange, false, nil); err != nil {
		return err
	}
	sourceArgs := amqp.Table{
		"x-queue-type":              "quorum",
		"x-delivery-limit":          r.config.DeadLetter.DeliveryLimit,
		"x-max-length":              r.config.DeadLetter.SourceMaxLength,
		"x-overflow":                "reject-publish",
		"x-dead-letter-exchange":    spec.DeadExchange,
		"x-dead-letter-routing-key": spec.DeadRoutingKey,
	}
	if spec.Critical {
		sourceArgs["x-dead-letter-strategy"] = "at-least-once"
	}
	if _, err := channel.QueueDeclare(spec.SourceQueue, true, false, false, false, sourceArgs); err != nil {
		return err
	}
	switch spec.Mode {
	case MigrationDual, MigrationNew:
		if err := channel.QueueBind(spec.SourceQueue, spec.RoutingKey, spec.Exchange, false, nil); err != nil {
			return err
		}
	default:
		if err := channel.QueueUnbind(spec.SourceQueue, spec.RoutingKey, spec.Exchange, nil); err != nil {
			return err
		}
	}
	if spec.Mode == MigrationNew {
		if err := channel.QueueUnbind(spec.LegacyQueue, spec.RoutingKey, spec.Exchange, nil); err != nil {
			return err
		}
	}
	return nil
}

func (r *RabbitMQ) consumerQueues(consumer string) []string {
	spec, ok := r.queueSpec(consumer)
	if !ok || !r.config.DeadLetter.Enabled {
		if ok {
			return []string{spec.LegacyQueue}
		}
		return nil
	}
	switch spec.Mode {
	case MigrationDual:
		return []string{spec.LegacyQueue, spec.SourceQueue}
	case MigrationNew:
		return []string{spec.SourceQueue}
	default:
		return []string{spec.LegacyQueue}
	}
}

func (r *RabbitMQ) shouldBindLegacyQueue(consumer string) bool {
	spec, ok := r.queueSpec(consumer)
	if !ok || !r.config.DeadLetter.Enabled {
		return true
	}
	return spec.Mode != MigrationNew
}

func (r *RabbitMQ) replayTimeout() time.Duration {
	value, err := time.ParseDuration(r.config.DeadLetter.ReplayTimeout)
	if err != nil || value <= 0 {
		return 5 * time.Second
	}
	return value
}
