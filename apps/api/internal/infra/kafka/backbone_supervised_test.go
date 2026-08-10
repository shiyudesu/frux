package infrakafka

import (
	"context"
	"testing"
	"time"

	infraconfig "github.com/shiyudesu/frux/internal/infra/config"
)

func TestSupervisedBackboneStartupDoesNotWaitForBroker(t *testing.T) {
	cfg := infraconfig.KafkaConfig{
		Enabled:     true,
		Environment: "test",
		Brokers:     []string{"127.0.0.1:1"},
		ClientID:    "frux-test",
		Timeouts: infraconfig.KafkaTimeoutConfig{
			Dial: "50ms", Request: "50ms", Produce: "50ms",
			Admin: "50ms", Shutdown: "50ms",
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	started := time.Now()
	backbone, err := StartSupervised(ctx, cfg, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
		t.Fatalf("supervised startup waited for broker: %v", elapsed)
	}
	if backbone.Publisher() == nil {
		t.Fatal("durable dispatch did not receive a retryable publisher")
	}
	if _, err := backbone.Publisher().Publish(
		context.Background(),
		TopicVideoPublished,
		[]byte("1"),
		EventMetadata{},
		struct{}{},
	); err == nil {
		t.Fatal("unavailable publisher reported success")
	}
	cancel()
	if err := backbone.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestDisabledSupervisedBackboneClosesPromptly(t *testing.T) {
	backbone, err := StartSupervised(
		context.Background(), infraconfig.KafkaConfig{}, nil, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := backbone.Close(ctx); err != nil {
		t.Fatal(err)
	}
}
