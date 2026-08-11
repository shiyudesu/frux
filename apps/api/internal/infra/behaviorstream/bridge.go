package infrabehaviorstream

import (
	"context"
	"errors"
	"time"

	applicationeventstream "github.com/shiyudesu/frux/internal/application/eventstream"
	applicationexposure "github.com/shiyudesu/frux/internal/application/exposure"
	applicationinteraction "github.com/shiyudesu/frux/internal/application/interaction"
	applicationrecommendation "github.com/shiyudesu/frux/internal/application/recommendation"
	infrakafka "github.com/shiyudesu/frux/internal/infra/kafka"
)

const (
	StreamAction = "action"
	StreamView   = "view"
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

type PublicationObserver interface {
	ObserveBehaviorPublication(stream, role, transport, result string)
}

type ActionPublisher struct {
	kafka    KafkaPublisher
	observer PublicationObserver
	now      func() time.Time
}

func NewActionPublisher(
	kafka KafkaPublisher,
	observer PublicationObserver,
) (*ActionPublisher, error) {
	if kafka == nil {
		return nil, infrakafka.ErrKafkaUnavailable
	}
	return &ActionPublisher{
		kafka: kafka, observer: observer,
		now: func() time.Time { return time.Now().UTC() },
	}, nil
}

func (p *ActionPublisher) PublishActionChanged(
	ctx context.Context,
	event *applicationinteraction.ActionChangedEvent,
) error {
	if event == nil {
		return nil
	}
	key, err := infrakafka.EncodeKey(infrakafka.KeyKindActionState, infrakafka.ActionStateKey{
		UserID: event.UserID, VideoID: event.VideoID, ActionType: event.ActionType,
	})
	if err == nil {
		_, err = p.kafka.Publish(ctx, infrakafka.TopicActionChanged, key, infrakafka.EventMetadata{
			EventID: event.EventID, Type: infrakafka.EventTypeActionChanged, SchemaVersion: 1,
			OccurredAt: event.OccurredAt, ProducedAt: p.now(),
			Producer: infrakafka.ProducerInteractionAPI,
		}, actionPayload(event))
	}
	p.observe(StreamAction, "primary", "kafka", err)
	return err
}

func (p *ActionPublisher) observe(stream, role, transport string, err error) {
	if p.observer != nil {
		p.observer.ObserveBehaviorPublication(stream, role, transport, publicationResult(err))
	}
}

type ViewPublisher struct {
	kafka    KafkaPublisher
	observer PublicationObserver
	now      func() time.Time
}

func NewViewPublisher(
	kafka KafkaPublisher,
	observer PublicationObserver,
) (*ViewPublisher, error) {
	if kafka == nil {
		return nil, infrakafka.ErrKafkaUnavailable
	}
	return &ViewPublisher{
		kafka: kafka, observer: observer,
		now: func() time.Time { return time.Now().UTC() },
	}, nil
}

func (p *ViewPublisher) PublishViewEventRecorded(
	ctx context.Context,
	event *applicationexposure.ViewEventRecordedEvent,
) error {
	if event == nil {
		return nil
	}
	key, err := infrakafka.EncodeKey(
		infrakafka.KeyKindUserID,
		infrakafka.UserKey{UserID: event.UserID},
	)
	if err == nil {
		_, err = p.kafka.Publish(ctx, infrakafka.TopicViewEventRecorded, key, infrakafka.EventMetadata{
			EventID: event.EventID, Type: infrakafka.EventTypeViewEventRecorded, SchemaVersion: 1,
			OccurredAt: event.OccurredAt, ProducedAt: p.now(),
			Producer: infrakafka.ProducerExposureWorker,
		}, viewPayload(event))
	}
	p.observe(StreamView, "primary", "kafka", err)
	return err
}

func (p *ViewPublisher) observe(stream, role, transport string, err error) {
	if p.observer != nil {
		p.observer.ObserveBehaviorPublication(stream, role, transport, publicationResult(err))
	}
}

func publicationResult(err error) string {
	switch {
	case err == nil:
		return "success"
	case errors.Is(err, infrakafka.ErrProduceUncertain),
		applicationeventstream.MayHaveTransportAcknowledgement(err):
		return "uncertain"
	default:
		return "failure"
	}
}

type ActionHandler struct {
	worker interface {
		HandleActionChanged(context.Context, *applicationinteraction.ActionChangedEvent) error
	}
}

func NewActionHandler(worker interface {
	HandleActionChanged(context.Context, *applicationinteraction.ActionChangedEvent) error
}) *ActionHandler {
	return &ActionHandler{worker: worker}
}

func (h *ActionHandler) Handle(
	ctx context.Context,
	event applicationeventstream.Event,
) (applicationeventstream.Outcome, error) {
	payload, ok := event.Payload.(*infrakafka.ActionChangedPayload)
	if !ok || h == nil || h.worker == nil {
		return applicationeventstream.OutcomeTerminal, nil
	}
	err := h.worker.HandleActionChanged(ctx, actionEvent(payload))
	if err == nil {
		return applicationeventstream.OutcomeDurableSuccess, nil
	}
	if applicationinteraction.IsTerminalActionEventError(err) {
		return applicationeventstream.OutcomeTerminal, nil
	}
	return applicationeventstream.OutcomeRetryable, err
}

type ViewHandler struct {
	worker interface {
		Handle(context.Context, *applicationexposure.ViewEventRecordedEvent) error
	}
}

func NewViewHandler(worker interface {
	Handle(context.Context, *applicationexposure.ViewEventRecordedEvent) error
}) *ViewHandler {
	return &ViewHandler{worker: worker}
}

func (h *ViewHandler) Handle(
	ctx context.Context,
	event applicationeventstream.Event,
) (applicationeventstream.Outcome, error) {
	payload, ok := event.Payload.(*infrakafka.ViewEventRecordedPayload)
	if !ok || h == nil || h.worker == nil {
		return applicationeventstream.OutcomeTerminal, nil
	}
	err := h.worker.Handle(ctx, viewEvent(payload))
	if err == nil {
		return applicationeventstream.OutcomeDurableSuccess, nil
	}
	if applicationrecommendation.IsTerminalBehaviorEventError(err) {
		return applicationeventstream.OutcomeTerminal, nil
	}
	return applicationeventstream.OutcomeRetryable, err
}

func actionPayload(event *applicationinteraction.ActionChangedEvent) infrakafka.ActionChangedPayload {
	return infrakafka.ActionChangedPayload{
		EventID: event.EventID, UserID: event.UserID, VideoID: event.VideoID,
		ActionType: event.ActionType, Active: event.Active, IdempotencyKey: event.IdempotencyKey,
		RecommendationRequestID: event.RecommendationRequestID,
		Version:                 event.Version, OccurredAt: event.OccurredAt,
	}
}

func actionEvent(payload *infrakafka.ActionChangedPayload) *applicationinteraction.ActionChangedEvent {
	return &applicationinteraction.ActionChangedEvent{
		EventID: payload.EventID, UserID: payload.UserID, VideoID: payload.VideoID,
		ActionType: payload.ActionType, Active: payload.Active, IdempotencyKey: payload.IdempotencyKey,
		RecommendationRequestID: payload.RecommendationRequestID,
		Version:                 payload.Version, OccurredAt: payload.OccurredAt,
	}
}

func viewPayload(event *applicationexposure.ViewEventRecordedEvent) infrakafka.ViewEventRecordedPayload {
	return infrakafka.ViewEventRecordedPayload{
		EventID: event.EventID, ViewEventID: event.ViewEventID, UserID: event.UserID,
		VideoID: event.VideoID, Scene: event.Scene, RequestID: event.RequestID,
		EventType: event.EventType, PlaybackSessionID: event.PlaybackSessionID,
		Sequence: event.Sequence, PositionMs: event.PositionMs, WatchMs: event.WatchMs,
		DurationMs: event.DurationMs, Completed: event.Completed, RecordedAt: event.RecordedAt,
		OccurredAt: event.OccurredAt, ExposureCount: event.ExposureCount,
	}
}

func viewEvent(payload *infrakafka.ViewEventRecordedPayload) *applicationexposure.ViewEventRecordedEvent {
	return &applicationexposure.ViewEventRecordedEvent{
		EventID: payload.EventID, ViewEventID: payload.ViewEventID, UserID: payload.UserID,
		VideoID: payload.VideoID, Scene: payload.Scene, RequestID: payload.RequestID,
		EventType: payload.EventType, PlaybackSessionID: payload.PlaybackSessionID,
		Sequence: payload.Sequence, PositionMs: payload.PositionMs, WatchMs: payload.WatchMs,
		DurationMs: payload.DurationMs, Completed: payload.Completed, RecordedAt: payload.RecordedAt,
		OccurredAt: payload.OccurredAt, ExposureCount: payload.ExposureCount,
	}
}
