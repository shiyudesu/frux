package infrabehaviorstream

import (
	"context"
	"errors"
	"testing"
	"time"

	applicationeventstream "github.com/shiyudesu/frux/internal/application/eventstream"
	applicationexposure "github.com/shiyudesu/frux/internal/application/exposure"
	applicationinteraction "github.com/shiyudesu/frux/internal/application/interaction"
	applicationrecommendation "github.com/shiyudesu/frux/internal/application/recommendation"
	domaininteraction "github.com/shiyudesu/frux/internal/domain/interaction"
	infrakafka "github.com/shiyudesu/frux/internal/infra/kafka"
)

type kafkaPublisherStub struct {
	calls    int
	err      error
	topic    infrakafka.TopicID
	key      []byte
	metadata infrakafka.EventMetadata
	payload  any
}

func (p *kafkaPublisherStub) Publish(
	_ context.Context,
	topic infrakafka.TopicID,
	key []byte,
	metadata infrakafka.EventMetadata,
	payload any,
) (infrakafka.ProduceMetadata, error) {
	p.calls++
	p.topic = topic
	p.key = append([]byte(nil), key...)
	p.metadata = metadata
	p.payload = payload
	return infrakafka.ProduceMetadata{}, p.err
}

func TestKafkaOnlyBehaviorPublishersUseRegisteredRecords(t *testing.T) {
	actionKafka := &kafkaPublisherStub{}
	actionPublisher, err := NewActionPublisher(actionKafka, nil)
	if err != nil {
		t.Fatal(err)
	}
	action := actionFixture()
	if err := actionPublisher.PublishActionChanged(context.Background(), action); err != nil {
		t.Fatal(err)
	}
	if actionKafka.calls != 1 ||
		actionKafka.topic != infrakafka.TopicActionChanged ||
		string(actionKafka.key) != "action:7:11:LIKE" ||
		actionKafka.metadata.EventID != action.EventID ||
		actionKafka.payload.(infrakafka.ActionChangedPayload).Version != action.Version {
		t.Fatalf("action record = %+v %q %#v", actionKafka.metadata, actionKafka.key, actionKafka.payload)
	}

	viewKafka := &kafkaPublisherStub{}
	viewPublisher, err := NewViewPublisher(viewKafka, nil)
	if err != nil {
		t.Fatal(err)
	}
	view := viewFixture()
	if err := viewPublisher.PublishViewEventRecorded(context.Background(), view); err != nil {
		t.Fatal(err)
	}
	if viewKafka.calls != 1 ||
		viewKafka.topic != infrakafka.TopicViewEventRecorded ||
		string(viewKafka.key) != "user:7" ||
		viewKafka.metadata.EventID != view.EventID {
		t.Fatalf("view record = %+v %q %#v", viewKafka.metadata, viewKafka.key, viewKafka.payload)
	}
}

func TestBehaviorPublisherRequiresKafka(t *testing.T) {
	if _, err := NewActionPublisher(nil, nil); !errors.Is(err, infrakafka.ErrKafkaUnavailable) {
		t.Fatalf("action error = %v", err)
	}
	if _, err := NewViewPublisher(nil, nil); !errors.Is(err, infrakafka.ErrKafkaUnavailable) {
		t.Fatalf("view error = %v", err)
	}
}

type publicationObservation struct {
	stream    string
	role      string
	transport string
	result    string
}

type publicationObserverStub struct {
	observations []publicationObservation
}

func (o *publicationObserverStub) ObserveBehaviorPublication(stream, role, transport, result string) {
	o.observations = append(o.observations, publicationObservation{
		stream: stream, role: role, transport: transport, result: result,
	})
}

func TestKafkaPublicationUncertaintyIsObserved(t *testing.T) {
	observer := &publicationObserverStub{}
	publisher, err := NewViewPublisher(
		&kafkaPublisherStub{err: infrakafka.ErrProduceUncertain},
		observer,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := publisher.PublishViewEventRecorded(
		context.Background(),
		viewFixture(),
	); !errors.Is(err, infrakafka.ErrProduceUncertain) {
		t.Fatalf("error = %v", err)
	}
	if len(observer.observations) != 1 {
		t.Fatalf("observations = %#v", observer.observations)
	}
	got := observer.observations[0]
	if got.stream != StreamView || got.role != "primary" ||
		got.transport != "kafka" || got.result != "uncertain" {
		t.Fatalf("observation = %+v", got)
	}
}

type viewOutboxStore struct {
	pending    bool
	leased     bool
	dispatched int
	failed     int
	event      *applicationexposure.ViewEventRecordedEvent
}

func (s *viewOutboxStore) ClaimViewEventOutbox(
	context.Context,
	int,
	time.Time,
	time.Time,
) ([]applicationexposure.OutboxItem, error) {
	if !s.pending || s.leased {
		return nil, nil
	}
	s.leased = true
	return []applicationexposure.OutboxItem{{ID: 1, Event: s.event, Attempts: 1}}, nil
}

func (s *viewOutboxStore) MarkViewEventOutboxDispatched(
	context.Context,
	int64,
	time.Time,
) error {
	s.pending = false
	s.leased = false
	s.dispatched++
	return nil
}

func (s *viewOutboxStore) MarkViewEventOutboxFailed(
	context.Context,
	int64,
	time.Time,
	string,
) error {
	s.leased = false
	s.failed++
	return nil
}

func (s *viewOutboxStore) ViewEventOutboxStats(
	context.Context,
	time.Time,
) (applicationexposure.OutboxStats, error) {
	if s.pending {
		return applicationexposure.OutboxStats{Pending: 1}, nil
	}
	return applicationexposure.OutboxStats{}, nil
}

func TestKafkaViewFailureLeavesOutboxPending(t *testing.T) {
	publishErr := errors.New("Kafka unavailable")
	publisher, err := NewViewPublisher(&kafkaPublisherStub{err: publishErr}, nil)
	if err != nil {
		t.Fatal(err)
	}
	store := &viewOutboxStore{pending: true, event: viewFixture()}
	dispatched, err := applicationexposure.NewOutboxDispatcher(store, publisher).
		DispatchOnce(context.Background())
	if !errors.Is(err, publishErr) || dispatched != 0 || !store.pending ||
		store.dispatched != 0 || store.failed != 1 {
		t.Fatalf("dispatched=%d err=%v store=%+v", dispatched, err, store)
	}
}

type actionWorkerStub struct{ err error }

func (w actionWorkerStub) HandleActionChanged(
	context.Context,
	*applicationinteraction.ActionChangedEvent,
) error {
	return w.err
}

type viewWorkerStub struct{ err error }

func (w viewWorkerStub) Handle(
	context.Context,
	*applicationexposure.ViewEventRecordedEvent,
) error {
	return w.err
}

func TestActiveHandlersExposeDurableTerminalAndRetryableOutcomes(t *testing.T) {
	actionEvent := applicationeventstream.Event{Payload: &infrakafka.ActionChangedPayload{}}
	outcome, err := NewActionHandler(actionWorkerStub{}).Handle(context.Background(), actionEvent)
	if err != nil || outcome != applicationeventstream.OutcomeDurableSuccess {
		t.Fatalf("action outcome=%s err=%v", outcome, err)
	}
	outcome, err = NewActionHandler(actionWorkerStub{
		err: terminalActionError(),
	}).Handle(context.Background(), actionEvent)
	if err != nil || outcome != applicationeventstream.OutcomeTerminal {
		t.Fatalf("terminal action outcome=%s err=%v", outcome, err)
	}
	retryErr := errors.New("database unavailable")
	outcome, err = NewActionHandler(actionWorkerStub{err: retryErr}).Handle(context.Background(), actionEvent)
	if !errors.Is(err, retryErr) || outcome != applicationeventstream.OutcomeRetryable {
		t.Fatalf("retry action outcome=%s err=%v", outcome, err)
	}

	viewEvent := applicationeventstream.Event{Payload: &infrakafka.ViewEventRecordedPayload{}}
	outcome, err = NewViewHandler(viewWorkerStub{}).Handle(context.Background(), viewEvent)
	if err != nil || outcome != applicationeventstream.OutcomeDurableSuccess {
		t.Fatalf("view outcome=%s err=%v", outcome, err)
	}
	outcome, err = NewViewHandler(viewWorkerStub{
		err: terminalBehaviorError{},
	}).Handle(context.Background(), viewEvent)
	if err != nil || outcome != applicationeventstream.OutcomeTerminal {
		t.Fatalf("terminal view outcome=%s err=%v", outcome, err)
	}
}

type idempotentActionRepository struct {
	events map[string]struct{}
	writes int
}

func (r *idempotentActionRepository) PersistAcceptedActionEvent(
	_ context.Context,
	event *domaininteraction.AcceptedActionEvent,
) error {
	if r.events == nil {
		r.events = map[string]struct{}{}
	}
	if _, exists := r.events[event.EventID]; !exists {
		r.events[event.EventID] = struct{}{}
		r.writes++
	}
	return nil
}

type idempotentViewRepository struct {
	events map[string]struct{}
	writes int
}

func (r *idempotentViewRepository) ApplyBehaviorEvent(
	_ context.Context,
	event *applicationexposure.ViewEventRecordedEvent,
) (bool, error) {
	if r.events == nil {
		r.events = map[string]struct{}{}
	}
	if _, exists := r.events[event.EventID]; exists {
		return false, nil
	}
	r.events[event.EventID] = struct{}{}
	r.writes++
	return true, nil
}

func TestStableIDsAbsorbKafkaRedelivery(t *testing.T) {
	actionRepo := &idempotentActionRepository{}
	actionWorker := applicationinteraction.NewActionWorker(actionRepo, nil)
	action := actionFixture()
	if err := actionWorker.HandleActionChanged(context.Background(), action); err != nil {
		t.Fatal(err)
	}
	actionPayload := actionPayload(action)
	outcome, err := NewActionHandler(actionWorker).Handle(
		context.Background(),
		applicationeventstream.Event{Payload: &actionPayload},
	)
	if err != nil || outcome != applicationeventstream.OutcomeDurableSuccess || actionRepo.writes != 1 {
		t.Fatalf("action outcome=%s err=%v writes=%d", outcome, err, actionRepo.writes)
	}

	viewRepo := &idempotentViewRepository{}
	viewWorker := applicationrecommendation.NewBehaviorEventWorker(viewRepo, nil)
	view := viewFixture()
	if err := viewWorker.Handle(context.Background(), view); err != nil {
		t.Fatal(err)
	}
	viewPayload := viewPayload(view)
	outcome, err = NewViewHandler(viewWorker).Handle(
		context.Background(),
		applicationeventstream.Event{Payload: &viewPayload},
	)
	if err != nil || outcome != applicationeventstream.OutcomeDurableSuccess || viewRepo.writes != 1 {
		t.Fatalf("view outcome=%s err=%v writes=%d", outcome, err, viewRepo.writes)
	}
}

type terminalBehaviorError struct{}

func (terminalBehaviorError) Error() string  { return "terminal" }
func (terminalBehaviorError) Terminal() bool { return true }

func terminalActionError() error {
	worker := applicationinteraction.NewActionWorker(nil, nil)
	return worker.HandleActionChanged(context.Background(), nil)
}

func actionFixture() *applicationinteraction.ActionChangedEvent {
	now := time.Date(2026, 8, 9, 2, 0, 0, 0, time.UTC)
	return &applicationinteraction.ActionChangedEvent{
		EventID: "action-event-1", UserID: 7, VideoID: 11, ActionType: "LIKE",
		Active: true, IdempotencyKey: "like-1", Version: 3, OccurredAt: now,
	}
}

func viewFixture() *applicationexposure.ViewEventRecordedEvent {
	now := time.Date(2026, 8, 9, 2, 0, 0, 0, time.UTC)
	return &applicationexposure.ViewEventRecordedEvent{
		EventID: "view-event-1", ViewEventID: 12, UserID: 7, VideoID: 11,
		Scene: "recommend", EventType: "play", RecordedAt: now, OccurredAt: now,
	}
}
