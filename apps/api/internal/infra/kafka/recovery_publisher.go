package infrakafka

import (
	"context"
	"time"

	applicationeventstream "github.com/shiyudesu/frux/internal/application/eventstream"

	"github.com/twmb/franz-go/pkg/kgo"
)

type recoveryPublisher interface {
	PublishRecovery(
		ctx context.Context,
		destination TopicID,
		key, value []byte,
		headers []applicationeventstream.Header,
	) error
}

type franzRecoveryPublisher struct {
	producer syncProducer
	prefix   string
	timeout  time.Duration
}

func (p *franzRecoveryPublisher) PublishRecovery(
	ctx context.Context,
	destination TopicID,
	key, value []byte,
	headers []applicationeventstream.Header,
) error {
	topic, err := TopicName(p.prefix, destination)
	if err != nil {
		return err
	}
	recordHeaders := make([]kgo.RecordHeader, 0, len(headers))
	for _, header := range headers {
		recordHeaders = append(recordHeaders, kgo.RecordHeader{
			Key: header.Key, Value: append([]byte(nil), header.Value...),
		})
	}
	record := &kgo.Record{
		Topic:   topic,
		Key:     append([]byte(nil), key...),
		Value:   append([]byte(nil), value...),
		Headers: recordHeaders,
	}
	if err := validateTopicRecordSize(destination, record); err != nil {
		return err
	}
	_, err = produceRecordSync(ctx, p.producer, p.timeout, record)
	return err
}
