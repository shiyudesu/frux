package infrabehaviorstream

import (
	"context"
	"errors"
	"fmt"
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

type RabbitActionPublisher interface {
	PublishActionChanged(context.Context, *applicationinteraction.ActionChangedEvent) error
}

type RabbitViewPublisher interface {
	PublishViewEventRecorded(context.Context, *applicationexposure.ViewEventRecordedEvent) error
}

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
	mode     infrakafka.ProducerMode
	rabbit   RabbitActionPublisher
	kafka    KafkaPublisher
	observer PublicationObserver
	now      func() time.Time
}

func NewActionPublisher(
	mode infrakafka.ProducerMode,
	rabbit RabbitActionPublisher,
	kafka KafkaPublisher,
	observer PublicationObserver,
) (*ActionPublisher, error) {
	if err := validateTransports(mode, rabbit != nil, kafka != nil); err != nil {
		return nil, err
	}
	return &ActionPublisher{
		mode: mode, rabbit: rabbit, kafka: kafka, observer: observer,
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
	primary, mirror := transports(p.mode)
	primaryErr := p.publish(ctx, primary, event)
	p.observe(StreamAction, "primary", primary, primaryErr)
	if mirror == "" {
		return primaryErr
	}
	mirrorErr := p.publish(ctx, mirror, event)
	p.observe(StreamAction, "mirror", mirror, mirrorErr)
	combinedErr := errors.Join(primaryErr, mirrorErr)
	p.observe(StreamAction, "combined", "dual", combinedErr)
	return combinedErr
}

func (p *ActionPublisher) publish(
	ctx context.Context,
	transport string,
	event *applicationinteraction.ActionChangedEvent,
) error {
	switch transport {
	case "rabbit":
		return p.rabbit.PublishActionChanged(ctx, event)
	case "kafka":
		key, err := infrakafka.EncodeKey(infrakafka.KeyKindActionState, infrakafka.ActionStateKey{
			UserID: event.UserID, VideoID: event.VideoID, ActionType: event.ActionType,
		})
		if err != nil {
			return err
		}
		_, err = p.kafka.Publish(ctx, infrakafka.TopicActionChanged, key, infrakafka.EventMetadata{
			EventID: event.EventID, Type: infrakafka.EventTypeActionChanged, SchemaVersion: 1,
			OccurredAt: event.OccurredAt, ProducedAt: p.now(),
			Producer: infrakafka.ProducerInteractionAPI,
		}, actionPayload(event))
		return err
	default:
		return errors.New("behavior transport unavailable")
	}
}

func (p *ActionPublisher) observe(stream, role, transport string, err error) {
	if p.observer != nil {
		p.observer.ObserveBehaviorPublication(stream, role, transport, publicationResult(err))
	}
}

type ViewPublisher struct {
	mode     infrakafka.ProducerMode
	rabbit   RabbitViewPublisher
	kafka    KafkaPublisher
	observer PublicationObserver
	now      func() time.Time
}

func NewViewPublisher(
	mode infrakafka.ProducerMode,
	rabbit RabbitViewPublisher,
	kafka KafkaPublisher,
	observer PublicationObserver,
) (*ViewPublisher, error) {
	if err := validateTransports(mode, rabbit != nil, kafka != nil); err != nil {
		return nil, err
	}
	return &ViewPublisher{
		mode: mode, rabbit: rabbit, kafka: kafka, observer: observer,
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
	primary, mirror := transports(p.mode)
	primaryErr := p.publish(ctx, primary, event)
	p.observe(StreamView, "primary", primary, primaryErr)
	if mirror == "" {
		return primaryErr
	}
	mirrorErr := p.publish(ctx, mirror, event)
	p.observe(StreamView, "mirror", mirror, mirrorErr)
	combinedErr := errors.Join(primaryErr, mirrorErr)
	p.observe(StreamView, "combined", "dual", combinedErr)
	return combinedErr
}

func (p *ViewPublisher) publish(
	ctx context.Context,
	transport string,
	event *applicationexposure.ViewEventRecordedEvent,
) error {
	switch transport {
	case "rabbit":
		return p.rabbit.PublishViewEventRecorded(ctx, event)
	case "kafka":
		key, err := infrakafka.EncodeKey(
			infrakafka.KeyKindUserID,
			infrakafka.UserKey{UserID: event.UserID},
		)
		if err != nil {
			return err
		}
		_, err = p.kafka.Publish(ctx, infrakafka.TopicViewEventRecorded, key, infrakafka.EventMetadata{
			EventID: event.EventID, Type: infrakafka.EventTypeViewEventRecorded, SchemaVersion: 1,
			OccurredAt: event.OccurredAt, ProducedAt: p.now(),
			Producer: infrakafka.ProducerExposureWorker,
		}, viewPayload(event))
		return err
	default:
		return errors.New("behavior transport unavailable")
	}
}

func (p *ViewPublisher) observe(stream, role, transport string, err error) {
	if p.observer != nil {
		p.observer.ObserveBehaviorPublication(stream, role, transport, publicationResult(err))
	}
}

func validateTransports(mode infrakafka.ProducerMode, rabbit, kafka bool) error {
	primary, mirror := transports(mode)
	if primary == "" || primary == "rabbit" && !rabbit || primary == "kafka" && !kafka ||
		mirror == "rabbit" && !rabbit || mirror == "kafka" && !kafka {
		return fmt.Errorf("%w: behavior publisher transport", infrakafka.ErrUnknownRegistryValue)
	}
	return nil
}

func transports(mode infrakafka.ProducerMode) (string, string) {
	switch mode {
	case infrakafka.ProducerModeRabbit:
		return "rabbit", ""
	case infrakafka.ProducerModeRabbitWithKafkaMirror:
		return "rabbit", "kafka"
	case infrakafka.ProducerModeKafkaWithRabbitMirror:
		return "kafka", "rabbit"
	case infrakafka.ProducerModeKafka:
		return "kafka", ""
	default:
		return "", ""
	}
}

func publicationResult(err error) string {
	switch {
	case err == nil:
		return "success"
	case errors.Is(err, infrakafka.ErrProduceUncertain):
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

type ActionParityReader interface {
	CompareAcceptedActionEvent(
		context.Context,
		*applicationinteraction.ActionChangedEvent,
	) (found, match bool, err error)
}

type ViewParityReader interface {
	CompareBehaviorEvent(
		context.Context,
		*applicationexposure.ViewEventRecordedEvent,
	) (found, match bool, err error)
}

type ActionParityChecker struct{ Reader ActionParityReader }

func (c ActionParityChecker) Compare(
	ctx context.Context,
	event applicationeventstream.Event,
) (applicationeventstream.ParityResult, error) {
	payload, ok := event.Payload.(*infrakafka.ActionChangedPayload)
	if !ok || c.Reader == nil {
		return applicationeventstream.ParityMismatch, nil
	}
	found, match, err := c.Reader.CompareAcceptedActionEvent(ctx, actionEvent(payload))
	if err != nil {
		return "", err
	}
	if found && match {
		return applicationeventstream.ParityMatch, nil
	}
	if !found {
		return applicationeventstream.ParityPending, nil
	}
	return applicationeventstream.ParityMismatch, nil
}

type ViewParityChecker struct{ Reader ViewParityReader }

func (c ViewParityChecker) Compare(
	ctx context.Context,
	event applicationeventstream.Event,
) (applicationeventstream.ParityResult, error) {
	payload, ok := event.Payload.(*infrakafka.ViewEventRecordedPayload)
	if !ok || c.Reader == nil {
		return applicationeventstream.ParityMismatch, nil
	}
	found, match, err := c.Reader.CompareBehaviorEvent(ctx, viewEvent(payload))
	if err != nil {
		return "", err
	}
	if found && match {
		return applicationeventstream.ParityMatch, nil
	}
	if !found {
		return applicationeventstream.ParityPending, nil
	}
	return applicationeventstream.ParityMismatch, nil
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
