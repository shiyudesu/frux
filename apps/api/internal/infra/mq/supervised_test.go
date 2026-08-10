package inframq

import (
	"context"
	"testing"
	"time"

	applicationmedia "github.com/shiyudesu/frux/internal/application/media"
	infraconfig "github.com/shiyudesu/frux/internal/infra/config"
)

func TestSupervisedRabbitMQConsumerStartupDoesNotWaitForBroker(t *testing.T) {
	supervisor, err := NewSupervisedRabbitMQ(infraconfig.RabbitMQConfig{
		URL: "amqp://guest:guest@127.0.0.1:1/",
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	started := time.Now()
	if err := supervisor.ConsumeMediaProcessingRequested(
		ctx,
		func(context.Context, *applicationmedia.ProcessingRequestedEvent) error {
			return nil
		},
	); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
		t.Fatalf("consumer startup waited for broker: %v", elapsed)
	}
	cancel()
	if err := supervisor.Close(); err != nil {
		t.Fatal(err)
	}
}
