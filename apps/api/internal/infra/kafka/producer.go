package infrakafka

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
)

var (
	ErrProduceFailed    = errors.New("kafka produce failed")
	ErrProduceUncertain = errors.New("kafka produce result uncertain")
	ErrProduceCanceled  = errors.New("kafka produce canceled")
)

type syncProducer interface {
	ProduceSync(ctx context.Context, records ...*kgo.Record) kgo.ProduceResults
}

type ProduceMetadata struct {
	Topic     TopicID
	Partition int32
	Offset    int64
	Timestamp time.Time
}

type ProduceObserver interface {
	ObserveProduce(topic TopicID, producer ProducerID, result string, duration time.Duration)
}

type Publisher struct {
	producer syncProducer
	prefix   string
	timeout  time.Duration
	observer ProduceObserver
}

func NewPublisher(client *Client, observer ProduceObserver) *Publisher {
	return &Publisher{
		producer: client.kgoClient, prefix: client.topicPrefix,
		timeout: client.produceTimeout, observer: observer,
	}
}

func (p *Publisher) Publish(
	ctx context.Context,
	topicID TopicID,
	key []byte,
	metadata EventMetadata,
	payload any,
) (ProduceMetadata, error) {
	started := time.Now()
	resultLabel := "failed"
	defer func() {
		if p.observer != nil {
			p.observer.ObserveProduce(topicID, metadata.Producer, resultLabel, time.Since(started))
		}
	}()
	if err := ctx.Err(); err != nil {
		resultLabel = "canceled"
		return ProduceMetadata{}, fmt.Errorf("%w: %v", ErrProduceCanceled, err)
	}
	value, err := EncodeEvent(topicID, key, metadata, payload)
	if err != nil {
		resultLabel = "contract"
		return ProduceMetadata{}, err
	}
	topicName, err := TopicName(p.prefix, topicID)
	if err != nil {
		resultLabel = "contract"
		return ProduceMetadata{}, err
	}
	produceContext, cancel := boundedContext(ctx, p.timeout)
	defer cancel()
	record := &kgo.Record{
		Topic: topicName, Key: append([]byte(nil), key...),
		Value: value, Timestamp: metadata.ProducedAt.UTC(),
	}
	results := p.producer.ProduceSync(produceContext, record)
	if len(results) != 1 || results[0].Record == nil {
		resultLabel = "uncertain"
		return ProduceMetadata{}, ErrProduceUncertain
	}
	result := results[0]
	if result.Err != nil {
		switch {
		case errors.Is(result.Err, context.Canceled):
			resultLabel = "canceled"
			return ProduceMetadata{}, fmt.Errorf("%w: canceled", ErrProduceCanceled)
		case errors.Is(result.Err, context.DeadlineExceeded) ||
			errors.Is(produceContext.Err(), context.DeadlineExceeded):
			resultLabel = "uncertain"
			return ProduceMetadata{}, fmt.Errorf("%w: deadline", ErrProduceUncertain)
		default:
			resultLabel = "failed"
			return ProduceMetadata{}, fmt.Errorf("%w: %s", ErrProduceFailed, sanitizeKafkaError(result.Err))
		}
	}
	resultLabel = "acknowledged"
	return ProduceMetadata{
		Topic: topicID, Partition: result.Record.Partition,
		Offset: result.Record.Offset, Timestamp: result.Record.Timestamp,
	}, nil
}

func boundedContext(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return context.WithCancel(parent)
	}
	deadline := time.Now().Add(timeout)
	if parentDeadline, exists := parent.Deadline(); exists && parentDeadline.Before(deadline) {
		return context.WithCancel(parent)
	}
	return context.WithDeadline(parent, deadline)
}
