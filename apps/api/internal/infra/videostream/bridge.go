package infravideostream

import (
	"context"
	"errors"
	"time"

	applicationeventstream "github.com/shiyudesu/frux/internal/application/eventstream"
	applicationmedia "github.com/shiyudesu/frux/internal/application/media"
	applicationvideo "github.com/shiyudesu/frux/internal/application/video"
	domainembedding "github.com/shiyudesu/frux/internal/domain/embedding"
	infrakafka "github.com/shiyudesu/frux/internal/infra/kafka"
)

type KafkaPublisher interface {
	Publish(
		context.Context,
		infrakafka.TopicID,
		[]byte,
		infrakafka.EventMetadata,
		any,
	) (infrakafka.ProduceMetadata, error)
}

type Observer interface {
	ObserveVideoWorkflowPublication(workflow, role, transport, result string)
}

type VideoPublisher struct {
	kafka    KafkaPublisher
	observer Observer
	now      func() time.Time
}

func NewVideoPublisher(
	kafka KafkaPublisher,
	observer Observer,
) (*VideoPublisher, error) {
	if kafka == nil {
		return nil, infrakafka.ErrKafkaUnavailable
	}
	return &VideoPublisher{
		kafka: kafka, observer: observer,
		now: func() time.Time { return time.Now().UTC() },
	}, nil
}

func (p *VideoPublisher) PublishVideoPublished(
	ctx context.Context,
	event *applicationvideo.PublishedEvent,
) error {
	if event == nil {
		return nil
	}
	key, err := infrakafka.EncodeKey(
		infrakafka.KeyKindVideoID,
		infrakafka.VideoKey{VideoID: event.VideoID},
	)
	if err == nil {
		_, err = p.kafka.Publish(ctx, infrakafka.TopicVideoPublished, key, infrakafka.EventMetadata{
			EventID: event.EventID, Type: infrakafka.EventTypeVideoPublished,
			SchemaVersion: 1, OccurredAt: event.OccurredAt, ProducedAt: p.now(),
			Producer: infrakafka.ProducerVideoWorker,
		}, videoPayload(event))
	}
	p.observe("publication", "primary", "kafka", err)
	return err
}

func (p *VideoPublisher) observe(workflow, role, transport string, err error) {
	if p != nil && p.observer != nil {
		p.observer.ObserveVideoWorkflowPublication(
			workflow, role, transport, publicationResult(err),
		)
	}
}

type MediaPublisher struct {
	kafka    KafkaPublisher
	observer Observer
	now      func() time.Time
}

func NewMediaPublisher(
	kafka KafkaPublisher,
	observer Observer,
) (*MediaPublisher, error) {
	if kafka == nil {
		return nil, infrakafka.ErrKafkaUnavailable
	}
	return &MediaPublisher{
		kafka: kafka, observer: observer,
		now: func() time.Time { return time.Now().UTC() },
	}, nil
}

func (p *MediaPublisher) PublishMediaProcessingRequested(
	ctx context.Context,
	event *applicationmedia.ProcessingRequestedEvent,
) error {
	if event == nil {
		return nil
	}
	key, err := infrakafka.EncodeKey(
		infrakafka.KeyKindAssetID,
		infrakafka.AssetKey{AssetID: event.AssetID},
	)
	if err == nil {
		_, err = p.kafka.Publish(
			ctx,
			infrakafka.TopicMediaProcessingRequested,
			key,
			infrakafka.EventMetadata{
				EventID: event.EventID, Type: infrakafka.EventTypeMediaProcessingRequested,
				SchemaVersion: 1, OccurredAt: event.OccurredAt, ProducedAt: p.now(),
				Producer: infrakafka.ProducerMediaAPI,
			},
			infrakafka.MediaProcessingRequestedPayload{
				EventID: event.EventID, AssetID: event.AssetID,
				ProfileVersion: event.ProfileVersion, OccurredAt: event.OccurredAt,
			},
		)
	}
	if p.observer != nil {
		p.observer.ObserveVideoWorkflowPublication(
			"media_wakeup", "primary", "kafka", publicationResult(err),
		)
	}
	return err
}

type FanoutHandler struct {
	worker interface {
		HandleVideoPublished(context.Context, *applicationvideo.PublishedEvent) error
	}
}

func NewFanoutHandler(worker interface {
	HandleVideoPublished(context.Context, *applicationvideo.PublishedEvent) error
}) *FanoutHandler {
	return &FanoutHandler{worker: worker}
}

func (h *FanoutHandler) Handle(
	ctx context.Context,
	event applicationeventstream.Event,
) (applicationeventstream.Outcome, error) {
	payload, ok := event.Payload.(*infrakafka.VideoPublishedPayload)
	if !ok || h == nil || h.worker == nil {
		return applicationeventstream.OutcomeTerminal, nil
	}
	if err := h.worker.HandleVideoPublished(ctx, publishedEvent(payload)); err != nil {
		return applicationeventstream.OutcomeRetryable, err
	}
	return applicationeventstream.OutcomeDurableSuccess, nil
}

type EmbeddingHandler struct {
	intake interface {
		HandleVideoPublished(context.Context, *applicationvideo.PublishedEvent) error
	}
}

func NewEmbeddingHandler(intake interface {
	HandleVideoPublished(context.Context, *applicationvideo.PublishedEvent) error
}) *EmbeddingHandler {
	return &EmbeddingHandler{intake: intake}
}

func (h *EmbeddingHandler) Handle(
	ctx context.Context,
	event applicationeventstream.Event,
) (applicationeventstream.Outcome, error) {
	payload, ok := event.Payload.(*infrakafka.VideoPublishedPayload)
	if !ok || h == nil || h.intake == nil {
		return applicationeventstream.OutcomeTerminal, nil
	}
	if err := h.intake.HandleVideoPublished(ctx, publishedEvent(payload)); err != nil {
		if errors.Is(err, domainembedding.ErrInvalidHashText) {
			return applicationeventstream.OutcomeTerminal, err
		}
		return applicationeventstream.OutcomeRetryable, err
	}
	return applicationeventstream.OutcomeDurableSuccess, nil
}

type MediaWakeupHandler struct {
	worker interface {
		SignalRequested(context.Context, *applicationmedia.ProcessingRequestedEvent) error
	}
}

func NewMediaWakeupHandler(worker interface {
	SignalRequested(context.Context, *applicationmedia.ProcessingRequestedEvent) error
}) *MediaWakeupHandler {
	return &MediaWakeupHandler{worker: worker}
}

func (h *MediaWakeupHandler) Handle(
	ctx context.Context,
	event applicationeventstream.Event,
) (applicationeventstream.Outcome, error) {
	payload, ok := event.Payload.(*infrakafka.MediaProcessingRequestedPayload)
	if !ok || h == nil || h.worker == nil {
		return applicationeventstream.OutcomeTerminal, nil
	}
	err := h.worker.SignalRequested(ctx, &applicationmedia.ProcessingRequestedEvent{
		EventID: payload.EventID, AssetID: payload.AssetID,
		ProfileVersion: payload.ProfileVersion, OccurredAt: payload.OccurredAt,
	})
	if err != nil {
		return applicationeventstream.OutcomeRetryable, err
	}
	return applicationeventstream.OutcomeDurableSuccess, nil
}

func videoPayload(event *applicationvideo.PublishedEvent) infrakafka.VideoPublishedPayload {
	return infrakafka.VideoPublishedPayload{
		EventID: event.EventID, VideoID: event.VideoID, AuthorID: event.AuthorID,
		Title: event.Title, Description: event.Description,
		MediaURL: event.MediaURL, CoverURL: event.CoverURL,
		PublishedAt: event.PublishedAt, OccurredAt: event.OccurredAt,
	}
}

func publishedEvent(payload *infrakafka.VideoPublishedPayload) *applicationvideo.PublishedEvent {
	return &applicationvideo.PublishedEvent{
		EventID: payload.EventID, VideoID: payload.VideoID, AuthorID: payload.AuthorID,
		Title: payload.Title, Description: payload.Description,
		MediaURL: payload.MediaURL, CoverURL: payload.CoverURL,
		PublishedAt: payload.PublishedAt, OccurredAt: payload.OccurredAt,
	}
}

func publicationResult(err error) string {
	switch {
	case err == nil:
		return "success"
	case applicationeventstream.MayHaveTransportAcknowledgement(err):
		return "uncertain"
	default:
		return "failure"
	}
}
