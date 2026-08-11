package infravideostream

import (
	"context"
	"errors"
	"testing"
	"time"

	applicationeventstream "github.com/shiyudesu/frux/internal/application/eventstream"
	applicationmedia "github.com/shiyudesu/frux/internal/application/media"
	applicationvideo "github.com/shiyudesu/frux/internal/application/video"
	domainembedding "github.com/shiyudesu/frux/internal/domain/embedding"
	infrakafka "github.com/shiyudesu/frux/internal/infra/kafka"
)

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

func TestVideoPublisherUsesKafkaOnly(t *testing.T) {
	kafka := &kafkaPublisherStub{err: errors.New("kafka down")}
	publisher, err := NewVideoPublisher(
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
	if !errors.Is(err, kafka.err) || kafka.calls != 1 ||
		kafka.topic != infrakafka.TopicVideoPublished {
		t.Fatalf("err=%v kafka=%d topic=%s", err, kafka.calls, kafka.topic)
	}
}

func TestVideoPublishersRequireKafka(t *testing.T) {
	if _, err := NewVideoPublisher(nil, nil); !errors.Is(err, infrakafka.ErrKafkaUnavailable) {
		t.Fatalf("video error = %v", err)
	}
	if _, err := NewMediaPublisher(nil, nil); !errors.Is(err, infrakafka.ErrKafkaUnavailable) {
		t.Fatalf("media error = %v", err)
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

func TestFeedHandlerAcceptsEmbeddingIncompatiblePublicationText(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	payload := infrakafka.VideoPublishedPayload{
		EventID: "video-published:1:1", VideoID: 1, AuthorID: 2,
		Title: "video-domain\x00title", PublishedAt: now, OccurredAt: now,
	}
	key, err := infrakafka.EncodeKey(
		infrakafka.KeyKindVideoID,
		infrakafka.VideoKey{VideoID: payload.VideoID},
	)
	if err != nil {
		t.Fatal(err)
	}
	record, err := infrakafka.EncodeEvent(
		infrakafka.TopicVideoPublished,
		key,
		infrakafka.EventMetadata{
			EventID: payload.EventID, Type: infrakafka.EventTypeVideoPublished,
			SchemaVersion: 1, OccurredAt: now, ProducedAt: now,
			Producer: infrakafka.ProducerVideoWorker,
		},
		payload,
	)
	if err != nil {
		t.Fatalf("publication encode: %v", err)
	}
	decoded, err := infrakafka.DecodeEvent(
		infrakafka.TopicVideoPublished, key, record, now,
	)
	if err != nil {
		t.Fatalf("publication decode: %v", err)
	}
	stub := &fanoutStub{}
	outcome, err := NewFanoutHandler(stub).Handle(
		context.Background(),
		applicationeventstream.Event{Payload: decoded.Payload},
	)
	if err != nil || outcome != applicationeventstream.OutcomeDurableSuccess {
		t.Fatalf("feed outcome=%s err=%v", outcome, err)
	}
}

type mediaWakeupStub struct {
	calls int
}

type invalidHashEmbeddingIntakeStub struct{}

func (invalidHashEmbeddingIntakeStub) HandleVideoPublished(
	context.Context,
	*applicationvideo.PublishedEvent,
) error {
	return domainembedding.ErrInvalidHashText
}

func TestEmbeddingHandlerTreatsInvalidHashTextAsTerminal(t *testing.T) {
	outcome, err := NewEmbeddingHandler(invalidHashEmbeddingIntakeStub{}).Handle(
		context.Background(),
		applicationeventstream.Event{Payload: &infrakafka.VideoPublishedPayload{
			VideoID: 1, AuthorID: 2, Title: "title\x00",
		}},
	)
	if outcome != applicationeventstream.OutcomeTerminal ||
		!errors.Is(err, domainembedding.ErrInvalidHashText) {
		t.Fatalf("outcome=%s err=%v", outcome, err)
	}
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
