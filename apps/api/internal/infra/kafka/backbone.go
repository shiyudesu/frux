package infrakafka

import (
	"context"
	"errors"
	"sync"
	"time"

	infraconfig "github.com/shiyudesu/frux/internal/infra/config"
)

type Diagnostics struct {
	Enabled           bool
	Healthy           bool
	Environment       string
	RegisteredTopics  int
	LastValidatedAt   time.Time
	ValidationResults []TopicValidation
	FailureCode       string
}

type Backbone struct {
	client         *Client
	admin          *Administrator
	cutover        *CutoverAdministrator
	publisher      *Publisher
	plan           []StreamMigration
	environment    string
	healthObserver BrokerHealthObserver
	mu             sync.RWMutex
	diagnostics    Diagnostics
}

type BrokerHealthObserver interface {
	ObserveBrokerHealth(healthy bool)
}

func Start(
	ctx context.Context,
	cfg infraconfig.KafkaConfig,
	topologyObserver TopologyObserver,
	produceObserver ProduceObserver,
) (*Backbone, error) {
	plan, err := MigrationPlan(cfg)
	if err != nil {
		return nil, err
	}
	backbone := &Backbone{
		plan: plan, environment: cfg.Environment,
		diagnostics: Diagnostics{
			Enabled: cfg.Enabled, Environment: cfg.Environment,
			RegisteredTopics: len(topics),
		},
	}
	if observer, ok := topologyObserver.(BrokerHealthObserver); ok {
		backbone.healthObserver = observer
	}
	if !cfg.Enabled {
		backbone.diagnostics.Healthy = true
		backbone.observeHealth(false)
		return backbone, nil
	}
	client, err := NewClient(ctx, cfg)
	if err != nil {
		backbone.diagnostics.FailureCode = sanitizeKafkaError(err)
		backbone.observeHealth(false)
		return nil, err
	}
	backbone.client = client
	backbone.admin = NewAdministrator(client, cfg, topologyObserver)
	backbone.cutover = NewCutoverAdministrator(client, cfg)
	backbone.publisher = NewPublisher(client, produceObserver)
	results, err := backbone.admin.EnsureTopics(ctx)
	backbone.mu.Lock()
	backbone.diagnostics.LastValidatedAt = time.Now().UTC()
	backbone.diagnostics.ValidationResults = append([]TopicValidation(nil), results...)
	backbone.diagnostics.Healthy = err == nil
	if err != nil {
		backbone.diagnostics.FailureCode = "topology_invalid"
	}
	backbone.mu.Unlock()
	backbone.observeHealth(err == nil)
	if err != nil {
		_ = client.Close(context.Background())
		return nil, err
	}
	return backbone, nil
}

func (b *Backbone) ApplyConsumerCutover(
	ctx context.Context,
	group ConsumerGroupID,
	boundary string,
	mode CutoverMode,
) (CutoverResult, error) {
	if b == nil || b.cutover == nil {
		return "", ErrKafkaUnavailable
	}
	return b.cutover.Apply(ctx, group, boundary, mode)
}

func (b *Backbone) ConsumerCutoverInitialized(
	ctx context.Context,
	group ConsumerGroupID,
) (bool, error) {
	if b == nil || b.cutover == nil {
		return false, ErrKafkaUnavailable
	}
	return b.cutover.Initialized(ctx, group)
}

func (b *Backbone) Publisher() *Publisher {
	if b == nil {
		return nil
	}
	return b.publisher
}

func (b *Backbone) MigrationPlan() []StreamMigration {
	if b == nil {
		return nil
	}
	return append([]StreamMigration(nil), b.plan...)
}

func (b *Backbone) Health(ctx context.Context) error {
	if b == nil {
		return ErrKafkaUnavailable
	}
	b.mu.RLock()
	enabled := b.diagnostics.Enabled
	b.mu.RUnlock()
	if !enabled {
		return nil
	}
	if err := b.client.Ping(ctx); err != nil {
		b.mu.Lock()
		b.diagnostics.Healthy = false
		b.diagnostics.FailureCode = sanitizeKafkaError(err)
		b.mu.Unlock()
		b.observeHealth(false)
		return err
	}
	b.mu.Lock()
	b.diagnostics.Healthy = true
	b.diagnostics.FailureCode = ""
	b.mu.Unlock()
	b.observeHealth(true)
	return nil
}

func (b *Backbone) observeHealth(healthy bool) {
	if b != nil && b.healthObserver != nil {
		b.healthObserver.ObserveBrokerHealth(healthy)
	}
}

func (b *Backbone) Diagnostics() Diagnostics {
	if b == nil {
		return Diagnostics{}
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	result := b.diagnostics
	result.ValidationResults = append([]TopicValidation(nil), b.diagnostics.ValidationResults...)
	return result
}

func (b *Backbone) RunHealthObserver(
	ctx context.Context,
	interval time.Duration,
	timeout time.Duration,
) {
	if b == nil {
		return
	}
	if interval <= 0 {
		interval = 15 * time.Second
	}
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			healthContext, cancel := context.WithTimeout(ctx, timeout)
			_ = b.Health(healthContext)
			cancel()
		}
	}
}

func (b *Backbone) Close(ctx context.Context) error {
	if b == nil || b.client == nil {
		return nil
	}
	err := b.client.Close(ctx)
	if errors.Is(err, ErrKafkaShutdown) {
		b.mu.Lock()
		b.diagnostics.Healthy = false
		b.diagnostics.FailureCode = "shutdown_failed"
		b.mu.Unlock()
	}
	return err
}
