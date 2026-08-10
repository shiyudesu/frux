package infrakafka

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	infraconfig "github.com/shiyudesu/frux/internal/infra/config"

	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
)

var (
	ErrProduceFailed    = errors.New("kafka produce failed")
	ErrProduceUncertain = errors.New("kafka produce result uncertain")
	ErrProduceCanceled  = errors.New("kafka produce canceled")
)

type UncertainProduceError struct {
	cause error
}

func (e *UncertainProduceError) Error() string {
	if e == nil || e.cause == nil {
		return ErrProduceUncertain.Error()
	}
	return fmt.Sprintf("%s: %v", ErrProduceUncertain, e.cause)
}

func (e *UncertainProduceError) Unwrap() []error {
	return []error{ErrProduceUncertain, e.cause}
}

func (*UncertainProduceError) MayHaveAcknowledged() bool {
	return true
}

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
	mu       sync.RWMutex
	producer syncProducer
	prefix   string
	timeout  time.Duration
	observer ProduceObserver
}

func NewSupervisedPublisher(
	cfg infraconfig.KafkaConfig,
	observer ProduceObserver,
) (*Publisher, error) {
	timeout, err := time.ParseDuration(cfg.Timeouts.Produce)
	if err != nil || timeout <= 0 {
		return nil, ErrKafkaUnavailable
	}
	if _, err := TopicName(cfg.TopicPrefix, TopicVideoPublished); err != nil {
		return nil, err
	}
	return &Publisher{
		prefix: cfg.TopicPrefix, timeout: timeout, observer: observer,
	}, nil
}

func (p *Publisher) setClient(client *Client) {
	if p == nil || client == nil {
		return
	}
	p.mu.Lock()
	p.producer = client.kgoClient
	p.prefix = client.topicPrefix
	p.timeout = client.produceTimeout
	p.mu.Unlock()
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
	p.mu.RLock()
	producer := p.producer
	prefix := p.prefix
	timeout := p.timeout
	p.mu.RUnlock()
	if producer == nil {
		return ProduceMetadata{}, ErrKafkaUnavailable
	}
	value, err := EncodeEvent(topicID, key, metadata, payload)
	if err != nil {
		resultLabel = "contract"
		return ProduceMetadata{}, err
	}
	topicName, err := TopicName(prefix, topicID)
	if err != nil {
		resultLabel = "contract"
		return ProduceMetadata{}, err
	}
	produceContext, cancel := boundedContext(ctx, timeout)
	defer cancel()
	record := &kgo.Record{
		Topic: topicName, Key: append([]byte(nil), key...),
		Value: value,
	}
	results := producer.ProduceSync(produceContext, record)
	if len(results) != 1 || results[0].Record == nil {
		resultLabel = "uncertain"
		return ProduceMetadata{}, &UncertainProduceError{
			cause: errors.New("missing produce result"),
		}
	}
	result := results[0]
	if result.Err != nil {
		switch {
		case errors.Is(result.Err, context.Canceled),
			errors.Is(result.Err, context.DeadlineExceeded),
			errors.Is(produceContext.Err(), context.DeadlineExceeded):
			resultLabel = "uncertain"
			return ProduceMetadata{}, &UncertainProduceError{
				cause: errors.New("canceled or deadline"),
			}
		default:
			if produceResultMayHaveAcknowledged(result.Err) {
				resultLabel = "uncertain"
				return ProduceMetadata{}, &UncertainProduceError{
					cause: result.Err,
				}
			}
			resultLabel = "failed"
			return ProduceMetadata{}, fmt.Errorf(
				"%w: %w",
				ErrProduceFailed,
				result.Err,
			)
		}
	}
	resultLabel = "acknowledged"
	return ProduceMetadata{
		Topic: topicID, Partition: result.Record.Partition,
		Offset: result.Record.Offset, Timestamp: result.Record.Timestamp,
	}, nil
}

func produceResultMayHaveAcknowledged(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, kerr.RequestTimedOut) ||
		errors.Is(err, kerr.NotEnoughReplicasAfterAppend) ||
		errors.Is(err, kerr.DuplicateSequenceNumber) {
		return true
	}
	var kafkaError *kerr.Error
	if errors.As(err, &kafkaError) {
		return false
	}
	return true
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
