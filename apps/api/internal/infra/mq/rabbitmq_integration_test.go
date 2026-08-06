package inframq

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	applicationinteraction "github.com/shiyudesu/frux/internal/application/interaction"
	domaindeadletter "github.com/shiyudesu/frux/internal/domain/deadletter"
	infraconfig "github.com/shiyudesu/frux/internal/infra/config"

	amqp "github.com/rabbitmq/amqp091-go"
)

func TestRabbitMQDeadLettersPoisonAndRetainsWhenTargetUnavailable(t *testing.T) {
	url := os.Getenv("FRUX_RABBITMQ_TEST_URL")
	if url == "" {
		t.Skip("FRUX_RABBITMQ_TEST_URL is not set")
	}
	cfg := integrationRabbitMQConfig(url, fmt.Sprintf("frux.test.dlx.%d", time.Now().UnixNano()))
	cfg.DeadLetter.ActionChangedMode = MigrationNew
	client, err := NewRabbitMQ(cfg)
	if err != nil {
		t.Fatalf("NewRabbitMQ() error = %v", err)
	}
	defer cleanupIntegrationTopology(t, client)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	spec, _ := client.queueSpec(ConsumerActionChanged)
	publishIntegrationMessage(
		t, client, spec.DeadExchange, spec.DeadRoutingKey,
		"direct-dlq-injection", []byte(`{"event_id":"direct-dlq-injection"}`),
	)
	manager := NewDeadLetterManager(client, cfg)
	if _, err := manager.ClaimDeadLetter(
		ctx, spec.DeadQueue, "direct-dlq-injection",
	); !errors.Is(err, domaindeadletter.ErrReplayFailed) {
		t.Fatalf("direct DLQ injection claim error = %v, want replay failure", err)
	}
	direct := waitForQueueMessage(t, client, spec.DeadQueue, 5*time.Second)
	if direct.MessageId != "direct-dlq-injection" {
		t.Fatalf("unexpected directly injected message = %q", direct.MessageId)
	}
	if err := client.ConsumeActionChanged(ctx, func(context.Context, *applicationinteraction.ActionChangedEvent) error {
		return errors.New("postgres unavailable")
	}); err != nil {
		t.Fatalf("ConsumeActionChanged() error = %v", err)
	}

	publishIntegrationMessage(t, client, cfg.InteractionExchange, cfg.ActionChangedRouting, "poison-json", []byte("{"))
	assertQueueDepthStaysZero(t, client, spec.LegacyQueue, 500*time.Millisecond)
	if cfg.ManagementURL != "" {
		preview := waitForDeadLetterPreview(t, manager, spec.DeadQueue, 10*time.Second)
		if preview.MessageID != "poison-json" || preview.JSONValid ||
			preview.PayloadBytes != 1 {
			t.Fatalf("unexpected poison preview: %+v", preview)
		}
	}
	poison := waitForQueueMessage(t, client, spec.DeadQueue, 10*time.Second)
	if poison.MessageId != "poison-json" {
		t.Fatalf("poison message id = %q", poison.MessageId)
	}

	if _, err := client.publishChannel.QueueDelete(spec.DeadQueue, false, false, false); err != nil {
		t.Fatalf("delete DLQ: %v", err)
	}
	body, _ := json.Marshal(applicationinteraction.ActionChangedEvent{
		EventID: "retry-without-dlq", UserID: 7, VideoID: 9,
		ActionType: "LIKE", Active: true, IdempotencyKey: "retry-without-dlq",
		Version: 1, OccurredAt: time.Now().UTC(),
	})
	publishIntegrationMessage(
		t, client, cfg.InteractionExchange, cfg.ActionChangedRouting,
		"retry-without-dlq", body,
	)
	waitForQueueDepth(t, client, spec.SourceQueue, 1, 10*time.Second)

	if _, err := client.publishChannel.QueueDeclare(
		spec.DeadQueue, true, false, false, false,
		amqp.Table{
			"x-queue-type": "quorum",
			"x-max-length": cfg.DeadLetter.DeadLetterMaxLength,
			"x-overflow":   "reject-publish",
		},
	); err != nil {
		t.Fatalf("redeclare DLQ: %v", err)
	}
	if err := client.publishChannel.QueueBind(
		spec.DeadQueue, spec.DeadRoutingKey, spec.DeadExchange, false, nil,
	); err != nil {
		t.Fatalf("rebind DLQ: %v", err)
	}
	waitForQueueDepth(t, client, spec.DeadQueue, 1, 10*time.Second)

	if err := client.publishChannel.QueueUnbind(
		spec.SourceQueue, spec.RoutingKey, spec.Exchange, nil,
	); err != nil {
		t.Fatalf("unbind replay target: %v", err)
	}
	claim, err := manager.ClaimDeadLetter(ctx, spec.DeadQueue, "retry-without-dlq")
	if err != nil {
		t.Fatalf("ClaimDeadLetter() error = %v", err)
	}
	if err := claim.Publish(ctx, "replay-integration"); err == nil {
		t.Fatal("replay unexpectedly succeeded without a bound target")
	}
	if err := claim.Nack(); err != nil {
		t.Fatalf("Nack() error = %v", err)
	}
	waitForQueueDepth(t, client, spec.DeadQueue, 1, 5*time.Second)
}

func TestRabbitMQDualBindingDeliversStableEventIDIdempotently(t *testing.T) {
	url := os.Getenv("FRUX_RABBITMQ_TEST_URL")
	if url == "" {
		t.Skip("FRUX_RABBITMQ_TEST_URL is not set")
	}
	cfg := integrationRabbitMQConfig(url, fmt.Sprintf("frux.test.dual.%d", time.Now().UnixNano()))
	cfg.DeadLetter.ActionChangedMode = MigrationDual
	client, err := NewRabbitMQ(cfg)
	if err != nil {
		t.Fatalf("NewRabbitMQ() error = %v", err)
	}
	defer cleanupIntegrationTopology(t, client)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var calls atomic.Int32
	ids := make(chan string, 2)
	if err := client.ConsumeActionChanged(ctx, func(_ context.Context, event *applicationinteraction.ActionChangedEvent) error {
		calls.Add(1)
		ids <- event.EventID
		return nil
	}); err != nil {
		t.Fatalf("ConsumeActionChanged() error = %v", err)
	}
	body, _ := json.Marshal(applicationinteraction.ActionChangedEvent{
		EventID: "stable-event-id", UserID: 7, VideoID: 9,
		ActionType: "LIKE", Active: true, IdempotencyKey: "stable-event-id",
		Version: 1, OccurredAt: time.Now().UTC(),
	})
	publishIntegrationMessage(
		t, client, cfg.InteractionExchange, cfg.ActionChangedRouting,
		"stable-event-id", body,
	)
	for range 2 {
		select {
		case eventID := <-ids:
			if eventID != "stable-event-id" {
				t.Fatalf("duplicate delivery changed event id: %q", eventID)
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("dual delivery calls = %d, want 2", calls.Load())
		}
	}
}

func TestRabbitMQTopologyDeclarationIsParallelSafe(t *testing.T) {
	url := os.Getenv("FRUX_RABBITMQ_TEST_URL")
	if url == "" {
		t.Skip("FRUX_RABBITMQ_TEST_URL is not set")
	}
	cfg := integrationRabbitMQConfig(url, fmt.Sprintf("frux.test.parallel.%d", time.Now().UnixNano()))
	cfg.DeadLetter.ActionChangedMode = MigrationDual
	type result struct {
		client *RabbitMQ
		err    error
	}
	results := make(chan result, 2)
	for range 2 {
		go func() {
			client, err := NewRabbitMQ(cfg)
			results <- result{client: client, err: err}
		}()
	}
	clients := make([]*RabbitMQ, 0, 2)
	for range 2 {
		item := <-results
		if item.err != nil {
			for _, client := range clients {
				_ = client.Close()
			}
			t.Fatalf("parallel NewRabbitMQ() error = %v", item.err)
		}
		clients = append(clients, item.client)
	}
	cleanupIntegrationTopology(t, clients[0])
	_ = clients[1].Close()
}

func integrationRabbitMQConfig(url, prefix string) infraconfig.RabbitMQConfig {
	cfg := testRabbitMQConfig()
	cfg.URL = url
	cfg.InteractionExchange = prefix + ".interaction"
	cfg.ActionChangedQueue = prefix + ".action"
	cfg.VideoExchange = prefix + ".video"
	cfg.VideoPublishedQueue = prefix + ".video.published"
	cfg.VideoEmbeddingQueue = prefix + ".video.embedding"
	cfg.ExposureExchange = prefix + ".exposure"
	cfg.ViewEventRecordedQueue = prefix + ".view"
	cfg.MediaExchange = prefix + ".media"
	cfg.MediaProcessingQueue = prefix + ".media.processing"
	cfg.DeadLetter = infraconfig.RabbitMQDeadLetterConfig{
		Enabled: true, DeliveryLimit: 2,
		SourceMaxLength: 100, DeadLetterMaxLength: 100,
		VersionSuffix: ".q2", ExchangeSuffix: ".dlx.q2", QueueSuffix: ".dlq.q2",
		ReplayTimeout: "2s",
	}
	if managementURL := os.Getenv("FRUX_RABBITMQ_MANAGEMENT_TEST_URL"); managementURL != "" {
		cfg.ManagementURL = managementURL
		cfg.ManagementUsername = "guest"
		cfg.ManagementPassword = "guest"
		cfg.ManagementTimeout = "2s"
	}
	return cfg
}

func publishIntegrationMessage(
	t *testing.T,
	client *RabbitMQ,
	exchange, routingKey, messageID string,
	body []byte,
) {
	t.Helper()
	confirmation, err := client.publishChannel.PublishWithDeferredConfirmWithContext(
		context.Background(), exchange, routingKey, false, false,
		amqp.Publishing{
			ContentType: "application/json", DeliveryMode: amqp.Persistent,
			MessageId: messageID, Body: body,
		},
	)
	if err != nil {
		t.Fatalf("publish integration message: %v", err)
	}
	if confirmation == nil {
		t.Fatal("publisher confirmation unavailable")
	}
	acknowledged, err := confirmation.WaitContext(context.Background())
	if err != nil || !acknowledged {
		t.Fatalf("publisher confirmation acknowledged=%v error=%v", acknowledged, err)
	}
}

func waitForQueueMessage(
	t *testing.T,
	client *RabbitMQ,
	queue string,
	timeout time.Duration,
) amqp.Delivery {
	t.Helper()
	channel, err := client.conn.Channel()
	if err != nil {
		t.Fatalf("open inspection channel: %v", err)
	}
	defer channel.Close()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		delivery, found, err := channel.Get(queue, true)
		if err != nil {
			t.Fatalf("get queue %s: %v", queue, err)
		}
		if found {
			return delivery
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("queue %s did not receive a message", queue)
	return amqp.Delivery{}
}

func waitForQueueDepth(
	t *testing.T,
	client *RabbitMQ,
	queue string,
	minimum int,
	timeout time.Duration,
) {
	t.Helper()
	channel, err := client.conn.Channel()
	if err != nil {
		t.Fatalf("open inspection channel: %v", err)
	}
	defer channel.Close()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		inspection, err := channel.QueueInspect(queue)
		if err == nil && inspection.Messages >= minimum {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("queue %s depth did not reach %d", queue, minimum)
}

func assertQueueDepthStaysZero(
	t *testing.T,
	client *RabbitMQ,
	queue string,
	duration time.Duration,
) {
	t.Helper()
	channel, err := client.conn.Channel()
	if err != nil {
		t.Fatalf("open inspection channel: %v", err)
	}
	defer channel.Close()
	deadline := time.Now().Add(duration)
	for time.Now().Before(deadline) {
		inspection, err := channel.QueueInspect(queue)
		if err != nil {
			t.Fatalf("inspect queue %s: %v", queue, err)
		}
		if inspection.Messages != 0 {
			t.Fatalf("queue %s depth = %d, want 0", queue, inspection.Messages)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func waitForDeadLetterPreview(
	t *testing.T,
	manager *DeadLetterManager,
	queue string,
	timeout time.Duration,
) domaindeadletter.MessagePreview {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		items, err := manager.PreviewDeadLetterQueue(context.Background(), queue, 1)
		if err == nil && len(items) == 1 {
			return items[0]
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("queue %s did not produce a management preview", queue)
	return domaindeadletter.MessagePreview{}
}

func cleanupIntegrationTopology(t *testing.T, client *RabbitMQ) {
	t.Helper()
	if client == nil {
		return
	}
	channel, err := client.conn.Channel()
	if err == nil {
		for _, spec := range client.queueSpecs() {
			_, _ = channel.QueueDelete(spec.LegacyQueue, false, false, false)
			_, _ = channel.QueueDelete(spec.SourceQueue, false, false, false)
			_, _ = channel.QueueDelete(spec.DeadQueue, false, false, false)
			_ = channel.ExchangeDelete(spec.DeadExchange, false, false)
		}
		exchanges := map[string]struct{}{}
		for _, spec := range client.queueSpecs() {
			exchanges[spec.Exchange] = struct{}{}
		}
		for exchange := range exchanges {
			_ = channel.ExchangeDelete(exchange, false, false)
		}
		_ = channel.Close()
	}
	_ = client.Close()
}
