package inframq

import (
	"context"
	"errors"
	"fmt"
	"time"

	inframetrics "github.com/shiyudesu/frux/internal/infra/metrics"

	amqp "github.com/rabbitmq/amqp091-go"
)

const maxConsumerRetryDelay = 30 * time.Second

type terminalConsumerError interface {
	Terminal() bool
}

type queueConsumerSession struct {
	connection *amqp.Connection
	channel    *amqp.Channel
	owned      bool
}

func (s *queueConsumerSession) Close() {
	if s == nil {
		return
	}
	if s.channel != nil {
		_ = s.channel.Close()
	}
	if s.owned && s.connection != nil {
		_ = s.connection.Close()
	}
}

func shouldRetryConsumerError(err error) bool {
	if err == nil {
		return false
	}
	var terminal terminalConsumerError
	return !errors.As(err, &terminal) || !terminal.Terminal()
}

func deliveryCount(delivery amqp.Delivery) int64 {
	value, exists := delivery.Headers["x-delivery-count"]
	if !exists {
		return 0
	}
	switch count := value.(type) {
	case int64:
		return count
	case int32:
		return int64(count)
	case int:
		return int64(count)
	case int16:
		return int64(count)
	case int8:
		return int64(count)
	default:
		return 0
	}
}

func (r *RabbitMQ) rejectDelivery(consumer, queue string, delivery amqp.Delivery, retry bool) {
	if retry {
		inframetrics.ObserveMQRetry(consumer)
		if r.isProtectedQueue(queue) &&
			deliveryCount(delivery) >= r.config.DeadLetter.DeliveryLimit-1 {
			inframetrics.ObserveMQExhaustion(consumer)
		}
	} else {
		inframetrics.ObserveMQTerminal(consumer)
	}
	_ = delivery.Nack(false, retry)
}

func (r *RabbitMQ) isProtectedQueue(queue string) bool {
	for _, spec := range r.queueSpecs() {
		if spec.SourceQueue == queue {
			return true
		}
	}
	return false
}

func (r *RabbitMQ) consumeProtectedQueue(
	ctx context.Context,
	consumer string,
	queue string,
	handler func(amqp.Delivery) bool,
) error {
	session, deliveries, err := r.openQueueConsumer(ctx, queue)
	if err != nil {
		return err
	}
	go func() {
		current := session
		defer func() {
			if current != nil {
				current.Close()
			}
		}()
		superviseDeliveries(
			ctx,
			deliveries,
			func(ctx context.Context) (<-chan amqp.Delivery, error) {
				if current != nil {
					current.Close()
				}
				next, nextDeliveries, err := r.openQueueConsumer(ctx, queue)
				if err != nil {
					current = nil
					return nil, err
				}
				current = next
				return nextDeliveries, nil
			},
			func(delivery amqp.Delivery) bool {
				reconnect := handler(delivery)
				if reconnect && current != nil {
					current.Close()
				}
				return reconnect
			},
			viewEventConsumerRetryDelay,
			"mq_"+consumer+"_reconsume",
		)
	}()
	return nil
}

func (r *RabbitMQ) openQueueConsumer(
	ctx context.Context,
	queue string,
) (*queueConsumerSession, <-chan amqp.Delivery, error) {
	if r == nil {
		return nil, nil, fmt.Errorf("rabbitmq consumer is unavailable")
	}
	connection := r.conn
	owned := false
	if connection == nil || connection.IsClosed() {
		created, err := amqp.Dial(r.config.URL)
		if err != nil {
			return nil, nil, err
		}
		connection = created
		owned = true
	}
	channel, err := connection.Channel()
	if err != nil {
		if owned {
			_ = connection.Close()
		}
		return nil, nil, err
	}
	session := &queueConsumerSession{
		connection: connection,
		channel:    channel,
		owned:      owned,
	}
	if err := channel.Qos(1, 0, false); err != nil {
		session.Close()
		return nil, nil, err
	}
	deliveries, err := channel.ConsumeWithContext(
		ctx, queue, "", false, false, false, false, nil,
	)
	if err != nil {
		session.Close()
		return nil, nil, err
	}
	return session, deliveries, nil
}

func boundedConsumerRetryDelay(current time.Duration) time.Duration {
	if current <= 0 {
		return time.Millisecond
	}
	if current >= maxConsumerRetryDelay/2 {
		return maxConsumerRetryDelay
	}
	return current * 2
}

func waitConsumerRetry(ctx context.Context, delay time.Duration) bool {
	if delay <= 0 {
		delay = time.Millisecond
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func observeConsumerReconnect(job string, err error) {
	inframetrics.ObserveWorkerJob(job, 0, err)
}

func shouldReconnectProtectedDelivery(queue string, retry bool, rabbit *RabbitMQ) bool {
	return retry && rabbit != nil && rabbit.isProtectedQueue(queue)
}
