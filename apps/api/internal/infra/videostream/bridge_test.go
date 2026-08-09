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
	domainfeed "github.com/shiyudesu/frux/internal/domain/feed"
	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
	infrakafka "github.com/shiyudesu/frux/internal/infra/kafka"
)

type rabbitVideoPublisherStub struct {
	calls int
	err   error
}

type fanoutParityStub struct {
	present bool
}

func (fanoutParityStub) CountFollowers(context.Context, int64) (int, error) {
	return 1, nil
}
func (fanoutParityStub) ListFollowerIDs(
	context.Context, int64, int64, int,
) ([]int64, error) {
	return []int64{9}, nil
}
func (s fanoutParityStub) HasFollowingFanout(
	context.Context, int64, []int64, *domainfeed.FeedPageItem, bool,
) (bool, error) {
	return s.present, nil
}

type embeddingParityStub struct {
	present bool
	matches bool
}

func (s embeddingParityStub) PublicationIntakeParity(
	context.Context, int64, string, string,
) (bool, bool, error) {
	return s.present, s.matches, nil
}

type mediaParityStub struct {
	job *domainmedia.MediaProcessingJob
}

func (s mediaParityStub) FindProcessingJobByAsset(
	context.Context, int64,
) (*domainmedia.MediaProcessingJob, error) {
	return s.job, nil
}

func TestVideoWorkflowParityDistinguishesPendingMismatchAndMatch(t *testing.T) {
	now := time.Now().UTC()
	publication := applicationeventstream.Event{Payload: &infrakafka.VideoPublishedPayload{
		VideoID: 1, AuthorID: 2, Title: "title", PublishedAt: now,
	}}
	fanoutPending, _ := (FanoutParityChecker{
		Reader: fanoutParityStub{}, Index: fanoutParityStub{},
	}).Compare(context.Background(), publication)
	fanoutMatch, _ := (FanoutParityChecker{
		Reader: fanoutParityStub{present: true}, Index: fanoutParityStub{present: true},
	}).Compare(context.Background(), publication)
	embeddingMismatch, _ := (EmbeddingParityChecker{
		Reader: embeddingParityStub{present: true},
	}).Compare(context.Background(), publication)
	embeddingMatch, _ := (EmbeddingParityChecker{
		Reader: embeddingParityStub{present: true, matches: true},
	}).Compare(context.Background(), publication)
	mediaPending, _ := (MediaWakeupParityChecker{
		Reader: mediaParityStub{},
	}).Compare(context.Background(), applicationeventstream.Event{
		Payload: &infrakafka.MediaProcessingRequestedPayload{
			AssetID: 3, ProfileVersion: "v1",
		},
	})
	mediaMismatch, _ := (MediaWakeupParityChecker{
		Reader: mediaParityStub{job: &domainmedia.MediaProcessingJob{ProfileVersion: "v2"}},
	}).Compare(context.Background(), applicationeventstream.Event{
		Payload: &infrakafka.MediaProcessingRequestedPayload{
			AssetID: 3, ProfileVersion: "v1",
		},
	})
	if fanoutPending != applicationeventstream.ParityPending ||
		fanoutMatch != applicationeventstream.ParityMatch ||
		embeddingMismatch != applicationeventstream.ParityMismatch ||
		embeddingMatch != applicationeventstream.ParityMatch ||
		mediaPending != applicationeventstream.ParityPending ||
		mediaMismatch != applicationeventstream.ParityMismatch {
		t.Fatalf(
			"fanout=%s/%s embedding=%s/%s media=%s/%s",
			fanoutPending, fanoutMatch, embeddingMismatch, embeddingMatch,
			mediaPending, mediaMismatch,
		)
	}
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

type poisonEmbeddingIntakeStub struct{}

func (poisonEmbeddingIntakeStub) HandleVideoPublished(
	context.Context, *applicationvideo.PublishedEvent,
) error {
	return domainembedding.ErrInvalidSemanticText
}

func TestEmbeddingHandlerTreatsCanonicalizationPoisonAsTerminal(t *testing.T) {
	outcome, err := NewEmbeddingHandler(poisonEmbeddingIntakeStub{}).Handle(
		context.Background(),
		applicationeventstream.Event{Payload: &infrakafka.VideoPublishedPayload{
			VideoID: 1, AuthorID: 2, Title: "\x00",
		}},
	)
	if outcome != applicationeventstream.OutcomeTerminal ||
		!errors.Is(err, domainembedding.ErrInvalidSemanticText) {
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
