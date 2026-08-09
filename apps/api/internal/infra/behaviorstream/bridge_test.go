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

type rabbitPublisherStub struct {
	actionCalls int
	viewCalls   int
	actionErr   error
	viewErr     error
}

func (p *rabbitPublisherStub) PublishActionChanged(
	context.Context,
	*applicationinteraction.ActionChangedEvent,
) error {
	p.actionCalls++
	return p.actionErr
}

func (p *rabbitPublisherStub) PublishViewEventRecorded(
	context.Context,
	*applicationexposure.ViewEventRecordedEvent,
) error {
	p.viewCalls++
	return p.viewErr
}

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

func TestActionPublisherSupportsAllMigrationModes(t *testing.T) {
	event := actionFixture()
	tests := []struct {
		name        string
		mode        infrakafka.ProducerMode
		rabbitCalls int
		kafkaCalls  int
	}{
		{name: "rabbit", mode: infrakafka.ProducerModeRabbit, rabbitCalls: 1},
		{name: "rabbit mirror", mode: infrakafka.ProducerModeRabbitWithKafkaMirror, rabbitCalls: 1, kafkaCalls: 1},
		{name: "kafka mirror", mode: infrakafka.ProducerModeKafkaWithRabbitMirror, rabbitCalls: 1, kafkaCalls: 1},
		{name: "kafka", mode: infrakafka.ProducerModeKafka, kafkaCalls: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rabbit := &rabbitPublisherStub{}
			kafka := &kafkaPublisherStub{}
			publisher, err := NewActionPublisher(test.mode, rabbit, kafka, nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := publisher.PublishActionChanged(context.Background(), event); err != nil {
				t.Fatal(err)
			}
			if rabbit.actionCalls != test.rabbitCalls || kafka.calls != test.kafkaCalls {
				t.Fatalf("rabbit=%d kafka=%d", rabbit.actionCalls, kafka.calls)
			}
			if kafka.calls > 0 && (kafka.topic != infrakafka.TopicActionChanged ||
				string(kafka.key) != "action:7:11:LIKE" ||
				kafka.metadata.EventID != event.EventID ||
				kafka.payload.(infrakafka.ActionChangedPayload).Version != event.Version) {
				t.Fatalf("Kafka record = %+v %q %#v", kafka.metadata, kafka.key, kafka.payload)
			}
		})
	}
}

func TestPrimaryFailureReturnsButMirrorFailureDoesNot(t *testing.T) {
	event := actionFixture()
	rabbit := &rabbitPublisherStub{}
	kafka := &kafkaPublisherStub{err: errors.New("mirror unavailable")}
	publisher, err := NewActionPublisher(
		infrakafka.ProducerModeRabbitWithKafkaMirror,
		rabbit,
		kafka,
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := publisher.PublishActionChanged(context.Background(), event); err != nil {
		t.Fatalf("mirror failure failed primary request: %v", err)
	}

	kafka.err = infrakafka.ErrProduceUncertain
	publisher, err = NewActionPublisher(
		infrakafka.ProducerModeKafkaWithRabbitMirror,
		rabbit,
		kafka,
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := publisher.PublishActionChanged(context.Background(), event); !errors.Is(err, infrakafka.ErrProduceUncertain) {
		t.Fatalf("primary uncertainty = %v", err)
	}
}

func TestViewPublisherUsesUserKeyAndPrimaryAcknowledgement(t *testing.T) {
	rabbit := &rabbitPublisherStub{}
	kafka := &kafkaPublisherStub{}
	publisher, err := NewViewPublisher(
		infrakafka.ProducerModeKafkaWithRabbitMirror,
		rabbit,
		kafka,
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	event := viewFixture()
	if err := publisher.PublishViewEventRecorded(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if kafka.topic != infrakafka.TopicViewEventRecorded ||
		string(kafka.key) != "user:7" ||
		rabbit.viewCalls != 1 {
		t.Fatalf("topic=%s key=%q rabbit=%d", kafka.topic, kafka.key, rabbit.viewCalls)
	}
}

func TestRabbitActiveBehaviorPathsRequireRabbitAcknowledgement(t *testing.T) {
	rabbitErr := errors.New("Rabbit publisher confirm failed")
	t.Run("action", func(t *testing.T) {
		rabbit := &rabbitPublisherStub{actionErr: rabbitErr}
		kafka := &kafkaPublisherStub{}
		publisher, err := NewActionPublisher(
			infrakafka.ProducerModeRabbitWithKafkaMirror,
			rabbit,
			kafka,
			nil,
		)
		if err != nil {
			t.Fatal(err)
		}
		if err := publisher.PublishActionChanged(
			context.Background(),
			actionFixture(),
		); !errors.Is(err, rabbitErr) {
			t.Fatalf("error=%v", err)
		}
		if kafka.calls != 1 {
			t.Fatalf("Kafka mirror calls=%d", kafka.calls)
		}
	})
	t.Run("view", func(t *testing.T) {
		rabbit := &rabbitPublisherStub{viewErr: rabbitErr}
		kafka := &kafkaPublisherStub{}
		publisher, err := NewViewPublisher(
			infrakafka.ProducerModeRabbitWithKafkaMirror,
			rabbit,
			kafka,
			nil,
		)
		if err != nil {
			t.Fatal(err)
		}
		if err := publisher.PublishViewEventRecorded(
			context.Background(),
			viewFixture(),
		); !errors.Is(err, rabbitErr) {
			t.Fatalf("error=%v", err)
		}
		if kafka.calls != 1 {
			t.Fatalf("Kafka mirror calls=%d", kafka.calls)
		}
	})
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

func TestActiveHandlersExposeOnlyDurableOrTerminalOutcomes(t *testing.T) {
	actionEvent := applicationeventstream.Event{Payload: &infrakafka.ActionChangedPayload{}}
	outcome, err := NewActionHandler(actionWorkerStub{}).Handle(context.Background(), actionEvent)
	if err != nil || outcome != applicationeventstream.OutcomeDurableSuccess {
		t.Fatalf("action outcome=%s err=%v", outcome, err)
	}
	outcome, err = NewActionHandler(actionWorkerStub{
		err: fmtTerminalAction(),
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

type cutoverActionRepository struct {
	events map[string]struct{}
	writes int
}

func (r *cutoverActionRepository) PersistAcceptedActionEvent(
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

type cutoverViewRepository struct {
	events map[string]struct{}
	writes int
}

func (r *cutoverViewRepository) ApplyBehaviorEvent(
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

func TestStableIDsAbsorbDeliveriesAroundCutoverBoundary(t *testing.T) {
	actionRepo := &cutoverActionRepository{}
	actionWorker := applicationinteraction.NewActionWorker(actionRepo, nil)
	action := actionFixture()
	if err := actionWorker.HandleActionChanged(context.Background(), action); err != nil {
		t.Fatal(err)
	}

	outcome, err := NewActionHandler(actionWorker).Handle(context.Background(), applicationeventstream.Event{
		Payload: func() *infrakafka.ActionChangedPayload {
			payload := actionPayload(action)
			return &payload
		}(),
	})
	if err != nil || outcome != applicationeventstream.OutcomeDurableSuccess || actionRepo.writes != 1 {
		t.Fatalf("action outcome=%s err=%v writes=%d", outcome, err, actionRepo.writes)
	}

	viewRepo := &cutoverViewRepository{}
	viewWorker := applicationrecommendation.NewBehaviorEventWorker(viewRepo, nil)
	view := viewFixture()
	if err := viewWorker.Handle(context.Background(), view); err != nil {
		t.Fatal(err)
	}
	outcome, err = NewViewHandler(viewWorker).Handle(context.Background(), applicationeventstream.Event{
		Payload: func() *infrakafka.ViewEventRecordedPayload {
			payload := viewPayload(view)
			return &payload
		}(),
	})
	if err != nil || outcome != applicationeventstream.OutcomeDurableSuccess || viewRepo.writes != 1 {
		t.Fatalf("view outcome=%s err=%v writes=%d", outcome, err, viewRepo.writes)
	}
}

type actionParityReaderStub struct {
	found bool
	match bool
}

func (r actionParityReaderStub) CompareAcceptedActionEvent(
	context.Context,
	*applicationinteraction.ActionChangedEvent,
) (bool, bool, error) {
	return r.found, r.match, nil
}

type viewParityReaderStub struct {
	found bool
	match bool
}

func (r viewParityReaderStub) CompareBehaviorEvent(
	context.Context,
	*applicationexposure.ViewEventRecordedEvent,
) (bool, bool, error) {
	return r.found, r.match, nil
}

func TestBehaviorParityDistinguishesPendingFactsFromConflicts(t *testing.T) {
	actionPayload := actionPayload(actionFixture())
	actionEvent := applicationeventstream.Event{Payload: &actionPayload}
	result, err := (ActionParityChecker{
		Reader: actionParityReaderStub{},
	}).Compare(context.Background(), actionEvent)
	if err != nil || result != applicationeventstream.ParityPending {
		t.Fatalf("missing action result=%s error=%v", result, err)
	}
	result, err = (ActionParityChecker{
		Reader: actionParityReaderStub{found: true},
	}).Compare(context.Background(), actionEvent)
	if err != nil || result != applicationeventstream.ParityMismatch {
		t.Fatalf("conflicting action result=%s error=%v", result, err)
	}

	viewPayload := viewPayload(viewFixture())
	viewEvent := applicationeventstream.Event{Payload: &viewPayload}
	result, err = (ViewParityChecker{
		Reader: viewParityReaderStub{},
	}).Compare(context.Background(), viewEvent)
	if err != nil || result != applicationeventstream.ParityPending {
		t.Fatalf("missing view result=%s error=%v", result, err)
	}
	result, err = (ViewParityChecker{
		Reader: viewParityReaderStub{found: true},
	}).Compare(context.Background(), viewEvent)
	if err != nil || result != applicationeventstream.ParityMismatch {
		t.Fatalf("conflicting view result=%s error=%v", result, err)
	}
}

type terminalBehaviorError struct{}

func (terminalBehaviorError) Error() string  { return "terminal" }
func (terminalBehaviorError) Terminal() bool { return true }

func fmtTerminalAction() error {
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
