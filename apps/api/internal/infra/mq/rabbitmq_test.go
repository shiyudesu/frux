package inframq

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	applicationinteraction "github.com/shiyudesu/frux/internal/application/interaction"
	infraconfig "github.com/shiyudesu/frux/internal/infra/config"

	amqp "github.com/rabbitmq/amqp091-go"
)

func TestUncertainPublishErrorMayHaveAcknowledged(t *testing.T) {
	cause := context.DeadlineExceeded
	err := &UncertainPublishError{cause: cause}
	if !err.MayHaveAcknowledged() || !errors.Is(err, cause) {
		t.Fatalf("error = %v", err)
	}
}

func TestActionDeliveryRequeuesOnlyInfrastructureFailures(t *testing.T) {
	if shouldRequeueActionDelivery(applicationinteraction.ErrTerminalActionEvent) {
		t.Fatal("terminal poison action event was requeued")
	}
	if !shouldRequeueActionDelivery(errors.New("postgres unavailable")) {
		t.Fatal("infrastructure failure was not left for supervised retry")
	}
}

func TestDeadLetterTopologyUsesNewQuorumQueueNames(t *testing.T) {
	cfg := normalizeRabbitMQConfig(testRabbitMQConfig())
	cfg.DeadLetter.Enabled = true
	cfg.DeadLetter.ActionChangedMode = MigrationDual
	rabbit := &RabbitMQ{config: cfg}
	spec, ok := rabbit.queueSpec(ConsumerActionChanged)
	if !ok {
		t.Fatal("action queue spec not found")
	}
	if spec.SourceQueue != cfg.ActionChangedQueue+".q2" ||
		spec.DeadQueue != cfg.ActionChangedQueue+".dlq.q2" ||
		spec.SourceQueue == spec.LegacyQueue {
		t.Fatalf("unexpected versioned topology: %+v", spec)
	}
	queues := rabbit.consumerQueues(ConsumerActionChanged)
	if len(queues) != 2 || queues[0] != spec.LegacyQueue || queues[1] != spec.SourceQueue {
		t.Fatalf("unexpected dual-drain queues: %#v", queues)
	}
}

func TestNewModeNeverBindsLegacyQueue(t *testing.T) {
	cfg := normalizeRabbitMQConfig(testRabbitMQConfig())
	cfg.DeadLetter.Enabled = true
	cfg.DeadLetter.ActionChangedMode = MigrationNew
	rabbit := &RabbitMQ{config: cfg}
	if rabbit.shouldBindLegacyQueue(ConsumerActionChanged) {
		t.Fatal("new mode must not bind the legacy queue")
	}
	cfg.DeadLetter.ActionChangedMode = MigrationDual
	rabbit.config = cfg
	if !rabbit.shouldBindLegacyQueue(ConsumerActionChanged) {
		t.Fatal("dual mode must retain the legacy binding")
	}
}

func TestAllNewModeConsumersSelectProtectedQuorumQueue(t *testing.T) {
	cfg := normalizeRabbitMQConfig(testRabbitMQConfig())
	cfg.DeadLetter.Enabled = true
	cfg.DeadLetter.ActionChangedMode = MigrationNew
	cfg.DeadLetter.VideoPublishedMode = MigrationNew
	cfg.DeadLetter.VideoEmbeddingMode = MigrationNew
	cfg.DeadLetter.ViewEventRecordedMode = MigrationNew
	cfg.DeadLetter.MediaProcessingMode = MigrationNew
	rabbit := &RabbitMQ{config: cfg}
	for _, consumer := range []string{
		ConsumerActionChanged,
		ConsumerVideoPublished,
		ConsumerVideoEmbedding,
		ConsumerViewEventRecorded,
		ConsumerMediaProcessing,
	} {
		queues := rabbit.consumerQueues(consumer)
		if len(queues) != 1 || !rabbit.isProtectedQueue(queues[0]) {
			t.Fatalf("%s queues are not protected: %#v", consumer, queues)
		}
	}
}

func TestReplayHeadersPreserveOriginalIdentityAndReplaceReplayID(t *testing.T) {
	headers, err := replayHeaders(amqp.Table{
		"x-frux-event-id":  "event-1",
		"x-frux-replay-id": "old-replay",
		"x-death":          []any{map[string]any{"count": int64(5)}},
	}, "event-1", "replay-new")
	if err != nil {
		t.Fatalf("replayHeaders() error = %v", err)
	}
	if headers["x-frux-event-id"] != "event-1" ||
		headers["x-frux-original-event-id"] != "event-1" ||
		headers["x-frux-replay-id"] != "replay-new" {
		t.Fatalf("unexpected replay headers: %#v", headers)
	}
	if _, exists := headers["x-death"]; exists {
		t.Fatal("broker death history was copied to replay")
	}
}

func TestDeliveryCountSupportsQuorumHeaderTypes(t *testing.T) {
	for _, value := range []any{int64(4), int32(4), int(4)} {
		if got := deliveryCount(amqp.Delivery{Headers: amqp.Table{"x-delivery-count": value}}); got != 4 {
			t.Fatalf("deliveryCount(%T) = %d, want 4", value, got)
		}
	}
}

func TestValidOriginalRouteRequiresBrokerDeathProvenance(t *testing.T) {
	cfg := normalizeRabbitMQConfig(testRabbitMQConfig())
	rabbit := &RabbitMQ{config: cfg}
	spec, _ := rabbit.queueSpec(ConsumerActionChanged)
	valid := amqp.Table{
		"x-first-death-queue":    spec.SourceQueue,
		"x-first-death-exchange": spec.Exchange,
		"x-death": []any{amqp.Table{
			"queue": spec.SourceQueue, "exchange": spec.Exchange,
			"routing-keys": []any{spec.RoutingKey},
		}},
	}
	if !validOriginalRoute(valid, spec) {
		t.Fatal("valid broker death provenance was rejected")
	}
	for name, headers := range map[string]amqp.Table{
		"direct injection": {},
		"wrong source queue": {
			"x-first-death-queue": spec.DeadQueue, "x-first-death-exchange": spec.Exchange,
			"x-death": []any{amqp.Table{
				"queue": spec.DeadQueue, "exchange": spec.Exchange,
				"routing-keys": []any{spec.RoutingKey},
			}},
		},
		"wrong exchange": {
			"x-first-death-queue": spec.SourceQueue, "x-first-death-exchange": "other",
			"x-death": []any{amqp.Table{
				"queue": spec.SourceQueue, "exchange": "other",
				"routing-keys": []any{spec.RoutingKey},
			}},
		},
		"wrong routing key": {
			"x-first-death-queue": spec.SourceQueue, "x-first-death-exchange": spec.Exchange,
			"x-death": []any{amqp.Table{
				"queue": spec.SourceQueue, "exchange": spec.Exchange,
				"routing-keys": []any{"other"},
			}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			if validOriginalRoute(headers, spec) {
				t.Fatalf("invalid provenance accepted: %#v", headers)
			}
		})
	}
}

func TestConsumerRetryDelayIsCapped(t *testing.T) {
	delay := time.Second
	for range 10 {
		delay = boundedConsumerRetryDelay(delay)
	}
	if delay != maxConsumerRetryDelay {
		t.Fatalf("bounded retry delay = %s, want %s", delay, maxConsumerRetryDelay)
	}
}

func testRabbitMQConfig() infraconfig.RabbitMQConfig {
	return infraconfig.RabbitMQConfig{
		InteractionExchange:      "frux.interaction",
		ActionChangedQueue:       "frux.interaction.action_changed",
		ActionChangedRouting:     "interaction.action_changed",
		VideoExchange:            "frux.video",
		VideoPublishedQueue:      "frux.video.published",
		VideoEmbeddingQueue:      "frux.video.embedding",
		VideoPublishedRouting:    "video.published",
		ExposureExchange:         "frux.exposure",
		ViewEventRecordedQueue:   "frux.exposure.view_event_recorded",
		ViewEventRecordedRouting: "exposure.view_event_recorded",
		MediaExchange:            "frux.media",
		MediaProcessingQueue:     "frux.media.processing",
		MediaProcessingRouting:   "media.processing.requested",
	}
}

func TestSuperviseViewEventDeliveriesReconsumesClosedChannel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	initial := make(chan amqp.Delivery)
	close(initial)
	replacement := make(chan amqp.Delivery, 1)
	replacement <- amqp.Delivery{Body: []byte(`{"event_id":"event-1"}`)}

	var reconsumeCalls atomic.Int32
	handled := make(chan struct{}, 1)
	done := make(chan struct{})
	go func() {
		superviseViewEventDeliveries(
			ctx,
			initial,
			func(context.Context) (<-chan amqp.Delivery, error) {
				reconsumeCalls.Add(1)
				return replacement, nil
			},
			func(amqp.Delivery) bool {
				handled <- struct{}{}
				return false
			},
			time.Millisecond,
		)
		close(done)
	}()

	select {
	case <-handled:
	case <-time.After(time.Second):
		t.Fatal("consumer did not reconsume after the delivery channel closed")
	}
	if reconsumeCalls.Load() != 1 {
		t.Fatalf("reconsume calls = %d, want 1", reconsumeCalls.Load())
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("supervisor did not stop after context cancellation")
	}
}

func TestSuperviseActionDeliveriesReconsumesClosedChannel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	initial := make(chan amqp.Delivery)
	close(initial)
	replacement := make(chan amqp.Delivery, 1)
	replacement <- amqp.Delivery{Body: []byte(`{"event_id":"event-1"}`)}

	var reconsumeCalls atomic.Int32
	handled := make(chan struct{}, 1)
	done := make(chan struct{})
	go func() {
		superviseActionDeliveries(
			ctx,
			initial,
			func(context.Context) (<-chan amqp.Delivery, error) {
				reconsumeCalls.Add(1)
				return replacement, nil
			},
			func(amqp.Delivery) bool {
				handled <- struct{}{}
				return false
			},
			time.Millisecond,
		)
		close(done)
	}()

	select {
	case <-handled:
	case <-time.After(time.Second):
		t.Fatal("action consumer did not reconsume after the delivery channel closed")
	}
	if reconsumeCalls.Load() != 1 {
		t.Fatalf("reconsume calls = %d, want 1", reconsumeCalls.Load())
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("action supervisor did not stop after context cancellation")
	}
}

func TestSuperviseDeliveriesBacksOffAfterRetryableHandlerFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	initial := make(chan amqp.Delivery, 1)
	initial <- amqp.Delivery{MessageId: "first"}
	replacement := make(chan amqp.Delivery, 1)
	replacement <- amqp.Delivery{MessageId: "second"}
	var reconsumeCalls atomic.Int32
	handled := make(chan string, 2)
	done := make(chan struct{})
	go func() {
		superviseDeliveries(
			ctx,
			initial,
			func(context.Context) (<-chan amqp.Delivery, error) {
				reconsumeCalls.Add(1)
				return replacement, nil
			},
			func(delivery amqp.Delivery) bool {
				handled <- delivery.MessageId
				return delivery.MessageId == "first"
			},
			time.Millisecond,
			"mq_test_reconsume",
		)
		close(done)
	}()
	for _, expected := range []string{"first", "second"} {
		select {
		case actual := <-handled:
			if actual != expected {
				t.Fatalf("handled %q, want %q", actual, expected)
			}
		case <-time.After(time.Second):
			t.Fatalf("delivery %q was not handled", expected)
		}
	}
	if reconsumeCalls.Load() != 1 {
		t.Fatalf("reconsume calls = %d, want 1", reconsumeCalls.Load())
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("supervisor did not stop after context cancellation")
	}
}
