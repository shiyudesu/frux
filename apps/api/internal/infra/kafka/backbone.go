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
	client           *Client
	admin            *Administrator
	cutover          *CutoverAdministrator
	publisher        *Publisher
	plan             []StreamMigration
	environment      string
	healthObserver   BrokerHealthObserver
	mu               sync.RWMutex
	diagnostics      Diagnostics
	supervisorCancel context.CancelFunc
	supervisorDone   chan struct{}
	closed           bool
}

func StartSupervised(
	ctx context.Context,
	cfg infraconfig.KafkaConfig,
	topologyObserver TopologyObserver,
	produceObserver ProduceObserver,
) (*Backbone, error) {
	plan, err := MigrationPlan(cfg)
	if err != nil {
		return nil, err
	}
	if cfg.Enabled {
		if _, err := clientOptions(cfg); err != nil {
			return nil, err
		}
	}
	publisher, publisherErr := NewSupervisedPublisher(cfg, produceObserver)
	if publisherErr != nil && cfg.Enabled {
		return nil, publisherErr
	}
	supervisorCtx, cancel := context.WithCancel(ctx)
	backbone := &Backbone{
		plan: plan, environment: cfg.Environment, publisher: publisher,
		supervisorCancel: cancel, supervisorDone: make(chan struct{}),
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
		close(backbone.supervisorDone)
		return backbone, nil
	}
	backbone.observeHealth(false)
	go func() {
		defer close(backbone.supervisorDone)
		backbone.runConnectionSupervisor(supervisorCtx, cfg, topologyObserver)
	}()
	return backbone, nil
}

func (b *Backbone) runConnectionSupervisor(
	ctx context.Context,
	cfg infraconfig.KafkaConfig,
	topologyObserver TopologyObserver,
) {
	backoff := 100 * time.Millisecond
	for ctx.Err() == nil {
		client, err := NewClient(ctx, cfg)
		var results []TopicValidation
		if err == nil {
			admin := NewAdministrator(client, cfg, topologyObserver)
			results, err = admin.EnsureTopics(ctx)
			if err == nil {
				b.mu.Lock()
				if b.closed || ctx.Err() != nil {
					b.mu.Unlock()
					_ = client.Close(context.Background())
					return
				}
				b.client = client
				b.admin = admin
				b.cutover = NewCutoverAdministrator(client, cfg)
				b.diagnostics.LastValidatedAt = time.Now().UTC()
				b.diagnostics.ValidationResults = append(
					[]TopicValidation(nil), results...,
				)
				b.diagnostics.Healthy = true
				b.diagnostics.FailureCode = ""
				b.mu.Unlock()
				b.publisher.setClient(client)
				b.observeHealth(true)
				return
			}
			_ = client.Close(context.Background())
		}
		b.mu.Lock()
		b.diagnostics.LastValidatedAt = time.Now().UTC()
		b.diagnostics.ValidationResults = append(
			[]TopicValidation(nil), results...,
		)
		b.diagnostics.Healthy = false
		b.diagnostics.FailureCode = sanitizeKafkaError(err)
		b.mu.Unlock()
		b.observeHealth(false)
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		if backoff < 30*time.Second {
			backoff *= 2
			if backoff > 30*time.Second {
				backoff = 30 * time.Second
			}
		}
	}
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
	if b == nil {
		return "", ErrKafkaUnavailable
	}
	b.mu.RLock()
	cutover := b.cutover
	b.mu.RUnlock()
	if cutover == nil {
		return "", ErrKafkaUnavailable
	}
	return cutover.Apply(ctx, group, boundary, mode)
}

func (b *Backbone) ConsumerCutoverInitialized(
	ctx context.Context,
	group ConsumerGroupID,
) (bool, error) {
	if b == nil {
		return false, ErrKafkaUnavailable
	}
	b.mu.RLock()
	cutover := b.cutover
	b.mu.RUnlock()
	if cutover == nil {
		return false, ErrKafkaUnavailable
	}
	return cutover.Initialized(ctx, group)
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
	b.mu.RLock()
	client := b.client
	b.mu.RUnlock()
	if client == nil {
		b.observeHealth(false)
		return ErrKafkaUnavailable
	}
	if err := client.Ping(ctx); err != nil {
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
	if b == nil {
		return nil
	}
	if b.supervisorCancel != nil {
		b.supervisorCancel()
	}
	b.mu.Lock()
	b.closed = true
	client := b.client
	b.client = nil
	done := b.supervisorDone
	b.mu.Unlock()
	if done != nil {
		select {
		case <-done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if client == nil {
		return nil
	}
	err := client.Close(ctx)
	if errors.Is(err, ErrKafkaShutdown) {
		b.mu.Lock()
		b.diagnostics.Healthy = false
		b.diagnostics.FailureCode = "shutdown_failed"
		b.mu.Unlock()
	}
	return err
}
