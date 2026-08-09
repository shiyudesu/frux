package infravideostream

import (
	"context"
	"errors"
	"testing"
	"time"

	applicationeventstream "github.com/shiyudesu/frux/internal/application/eventstream"
	applicationmedia "github.com/shiyudesu/frux/internal/application/media"
	applicationvideo "github.com/shiyudesu/frux/internal/application/video"
	infrakafka "github.com/shiyudesu/frux/internal/infra/kafka"
)

type rabbitVideoPublisherStub struct {
	calls int
	err   error
}

func (p *rabbitVideoPublisherStub) PublishVideoPublished(
	context.Context, *applicationvideo.PublishedEvent,
) error {
	p.calls++
	return p.err
}

type kafkaPublisherStub struct {
	calls   int
	topic   infrakafka.TopicID
	payload any
	err     error
}

func (p *kafkaPublisherStub) Publish(
	_ context.Context,
	topic infrakafka.TopicID,
	_ []byte,
	_ infrakafka.EventMetadata,
	payload any,
) (infrakafka.ProduceMetadata, error) {
	p.calls++
	p.topic = topic
	p.payload = payload
	return infrakafka.ProduceMetadata{}, p.err
}

func TestVideoPublisherRequiresBothDualAcknowledgements(t *testing.T) {
	rabbit := &rabbitVideoPublisherStub{}
	kafka := &kafkaPublisherStub{err: errors.New("kafka down")}
	publisher, err := NewVideoPublisher(
		infrakafka.ProducerModeRabbitWithKafkaMirror,
		rabbit,
		kafka,
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	err = publisher.PublishVideoPublished(context.Background(), &applicationvideo.PublishedEvent{
		EventID: "video-published:1:1", VideoID: 1, AuthorID: 2,
		PublishedAt: now.Add(-time.Minute), OccurredAt: now,
	})
	if err == nil || rabbit.calls != 1 || kafka.calls != 1 {
		t.Fatalf("err=%v rabbit=%d kafka=%d", err, rabbit.calls, kafka.calls)
	}
	var publicationErr *PublicationError
	if !errors.As(err, &publicationErr) ||
		!publicationErr.TransportAcknowledged("rabbit") ||
		publicationErr.TransportAcknowledged("kafka") {
		t.Fatalf("acknowledgements = %#v", err)
	}
}

type fanoutStub struct {
	publishedAt time.Time
}

func (s *fanoutStub) HandleVideoPublished(
	_ context.Context,
	event *applicationvideo.PublishedEvent,
) error {
	s.publishedAt = event.PublishedAt
	return nil
}

func TestFeedHandlerPreservesOriginalPublicationTime(t *testing.T) {
	publishedAt := time.Now().UTC().Add(-24 * time.Hour).Truncate(time.Millisecond)
	stub := &fanoutStub{}
	handler := NewFanoutHandler(stub)
	outcome, err := handler.Handle(context.Background(), applicationeventstream.Event{
		Payload: &infrakafka.VideoPublishedPayload{
			EventID: "video-published:1:1", VideoID: 1, AuthorID: 2,
			PublishedAt: publishedAt, OccurredAt: time.Now().UTC(),
		},
	})
	if err != nil || outcome != applicationeventstream.OutcomeDurableSuccess ||
		!stub.publishedAt.Equal(publishedAt) {
		t.Fatalf("outcome=%s time=%v err=%v", outcome, stub.publishedAt, err)
	}
}

type mediaWakeupStub struct {
	calls int
}

func (s *mediaWakeupStub) SignalRequested(
	context.Context,
	*applicationmedia.ProcessingRequestedEvent,
) error {
	s.calls++
	return nil
}

func TestMediaHandlerCommitsAfterSignalBoundary(t *testing.T) {
	stub := &mediaWakeupStub{}
	handler := NewMediaWakeupHandler(stub)
	outcome, err := handler.Handle(context.Background(), applicationeventstream.Event{
		Payload: &infrakafka.MediaProcessingRequestedPayload{
			EventID: "media-processing:5:v1", AssetID: 5,
			ProfileVersion: "v1", OccurredAt: time.Now().UTC(),
		},
	})
	if err != nil || outcome != applicationeventstream.OutcomeDurableSuccess || stub.calls != 1 {
		t.Fatalf("outcome=%s calls=%d err=%v", outcome, stub.calls, err)
	}
}
