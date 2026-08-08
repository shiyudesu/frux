package infrakafka

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	applicationeventstream "github.com/shiyudesu/frux/internal/application/eventstream"
	infraconfig "github.com/shiyudesu/frux/internal/infra/config"

	"github.com/twmb/franz-go/pkg/kadm"
)

func TestKafkaBackboneProvisionsProducesAndConsumesAfterClientRestart(t *testing.T) {
	brokersValue := strings.TrimSpace(os.Getenv("FRUX_KAFKA_TEST_BROKERS"))
	if brokersValue == "" {
		t.Skip("FRUX_KAFKA_TEST_BROKERS is not set; run against the Compose listener at 127.0.0.1:29092")
	}
	prefix := fmt.Sprintf("itest%d", time.Now().UnixNano())
	cfg := integrationKafkaConfig(strings.Split(brokersValue, ","), prefix)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	first, err := Start(ctx, cfg, nil, nil)
	if err != nil {
		t.Fatalf("start first Kafka client: %v", err)
	}
	now := time.Now().UTC()
	key := []byte("probe:persisted")
	_, err = first.Publisher().Publish(ctx, TopicBackboneProbe, key, EventMetadata{
		EventID: "event-persisted", Type: EventTypeBackboneProbe, SchemaVersion: 1,
		OccurredAt: now, ProducedAt: now, Producer: ProducerPlatformWorker,
	}, BackboneProbePayload{ProbeID: "persisted", Source: "integration"})
	if err != nil {
		_ = first.Close(context.Background())
		t.Fatalf("produce probe: %v", err)
	}
	if err := first.Close(ctx); err != nil {
		t.Fatalf("close first Kafka client: %v", err)
	}

	second, err := Start(ctx, cfg, nil, nil)
	if err != nil {
		t.Fatalf("restart Kafka client: %v", err)
	}
	defer func() {
		topicName, _ := TopicName(prefix, TopicBackboneProbe)
		_, _ = kadm.NewClient(second.client.kgoClient).DeleteTopics(context.Background(), topicName)
		_ = second.Close(context.Background())
	}()
	received := make(chan applicationeventstream.Event, 1)
	consumeContext, stopConsume := context.WithCancel(ctx)
	consumer, err := NewConsumer(
		consumeContext, cfg, GroupBackboneProbeActive,
		handlerFunc(func(_ context.Context, event applicationeventstream.Event) (applicationeventstream.Outcome, error) {
			received <- event
			stopConsume()
			return applicationeventstream.OutcomeDurableSuccess, nil
		}),
		nil,
	)
	if err != nil {
		t.Fatalf("create consumer: %v", err)
	}
	if err := consumer.Run(consumeContext); err != nil {
		t.Fatalf("consume persisted probe: %v", err)
	}
	select {
	case event := <-received:
		if event.EventID != "event-persisted" || event.Metadata.Group != prefix+".frux.platform.backbone_probe.active.v1" {
			t.Fatalf("unexpected event: %+v", event)
		}
	default:
		t.Fatal("persisted probe was not consumed after client restart")
	}
}

func integrationKafkaConfig(brokers []string, prefix string) infraconfig.KafkaConfig {
	return infraconfig.KafkaConfig{
		Enabled: true, Environment: "test", Brokers: brokers,
		ClientID: "frux-kafka-integration", TopicPrefix: prefix,
		AllowLocalProvisioning: true,
		Authentication:         infraconfig.KafkaAuthenticationConfig{Mechanism: "none"},
		Timeouts: infraconfig.KafkaTimeoutConfig{
			Dial: "5s", Request: "10s", Produce: "10s", Admin: "10s", Shutdown: "10s",
		},
		Consumer: infraconfig.KafkaConsumerConfig{
			MaxPollRecords: 10, MaxPollBytes: 1 << 20,
			PartitionConcurrency: 2, DrainTimeout: "5s",
		},
		ProductionValidation: infraconfig.KafkaProductionValidationConfig{
			ReplicationFactor: 1, MinInSyncReplicas: 1,
		},
	}
}
