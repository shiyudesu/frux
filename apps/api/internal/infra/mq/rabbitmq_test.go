package inframq

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

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
			func(amqp.Delivery) {
				handled <- struct{}{}
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
