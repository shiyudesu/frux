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

func TestDualPublishRequiresBothTransportAcknowledgements(t *testing.T) {
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
	if err := publisher.PublishActionChanged(context.Background(), event); !errors.Is(err, kafka.err) {
		t.Fatalf("Kafka mirror failure = %v", err)
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

	rabbit.actionErr = errors.New("Rabbit mirror unavailable")
	kafka.err = nil
	if err := publisher.PublishActionChanged(context.Background(), event); !errors.Is(err, rabbit.actionErr) {
		t.Fatalf("Rabbit mirror failure = %v", err)
	}
}

func TestDualPublishFailureExposesTransportAcknowledgements(t *testing.T) {
	failure := errors.New("transport unavailable")
	tests := []struct {
		name      string
		rabbitErr error
		kafkaErr  error
		rabbitAck bool
		kafkaAck  bool
		anyAck    bool
	}{
		{
			name:     "primary only acknowledged",
			kafkaErr: failure, rabbitAck: true, anyAck: true,
		},
		{
			name:      "mirror only acknowledged",
			rabbitErr: failure, kafkaAck: true, anyAck: true,
		},
		{
			name:      "neither acknowledged",
			rabbitErr: failure, kafkaErr: failure,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			publisher, err := NewActionPublisher(
				infrakafka.ProducerModeRabbitWithKafkaMirror,
				&rabbitPublisherStub{actionErr: test.rabbitErr},
				&kafkaPublisherStub{err: test.kafkaErr},
				nil,
			)
			if err != nil {
				t.Fatal(err)
			}
			err = publisher.PublishActionChanged(context.Background(), actionFixture())
			var publicationErr applicationeventstream.PublicationAcknowledgementError
			if !errors.As(err, &publicationErr) {
				t.Fatalf("error %T does not expose acknowledgements: %v", err, err)
			}
			if publicationErr.TransportAcknowledged("rabbit") != test.rabbitAck ||
				publicationErr.TransportAcknowledged("kafka") != test.kafkaAck ||
				publicationErr.AnyTransportAcknowledged() != test.anyAck {
				t.Fatalf(
					"rabbit=%t kafka=%t any=%t",
					publicationErr.TransportAcknowledged("rabbit"),
					publicationErr.TransportAcknowledged("kafka"),
					publicationErr.AnyTransportAcknowledged(),
				)
			}
		})
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

func TestDualViewPublishFailureExposesTransportAcknowledgements(t *testing.T) {
	failure := errors.New("transport unavailable")
	for _, test := range []struct {
		name      string
		rabbitErr error
		kafkaErr  error
		rabbitAck bool
		kafkaAck  bool
	}{
		{name: "primary only", kafkaErr: failure, rabbitAck: true},
		{name: "mirror only", rabbitErr: failure, kafkaAck: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			publisher, err := NewViewPublisher(
				infrakafka.ProducerModeRabbitWithKafkaMirror,
				&rabbitPublisherStub{viewErr: test.rabbitErr},
				&kafkaPublisherStub{err: test.kafkaErr},
				nil,
			)
			if err != nil {
				t.Fatal(err)
			}
			err = publisher.PublishViewEventRecorded(context.Background(), viewFixture())
			var publicationErr applicationeventstream.PublicationAcknowledgementError
			if !errors.As(err, &publicationErr) {
				t.Fatalf("error %T does not expose acknowledgements: %v", err, err)
			}
			if publicationErr.TransportAcknowledged("rabbit") != test.rabbitAck ||
				publicationErr.TransportAcknowledged("kafka") != test.kafkaAck {
				t.Fatalf(
					"rabbit=%t kafka=%t",
					publicationErr.TransportAcknowledged("rabbit"),
					publicationErr.TransportAcknowledged("kafka"),
				)
			}
		})
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

type publicationObservation struct {
	stream    string
	role      string
	transport string
	result    string
}

type possiblyAcknowledgedBridgeError struct{}

func (possiblyAcknowledgedBridgeError) Error() string             { return "uncertain" }
func (possiblyAcknowledgedBridgeError) MayHaveAcknowledged() bool { return true }

func TestPublicationResultRecognizesTransportUncertainty(t *testing.T) {
	if result := publicationResult(possiblyAcknowledgedBridgeError{}); result != "uncertain" {
		t.Fatalf("result = %q", result)
	}

}

func TestDualPublicationStartsMirrorBeforePrimaryDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	mirrorStarted := make(chan struct{})
	primaryErr, mirrorErr := publishConcurrently(
		ctx,
		func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		},
		func(context.Context) error {
			close(mirrorStarted)
			return nil
		},
	)
	select {
	case <-mirrorStarted:
	default:
		t.Fatal("mirror did not start concurrently")
	}
	if !errors.Is(primaryErr, context.DeadlineExceeded) || mirrorErr != nil {
		t.Fatalf("primary=%v mirror=%v", primaryErr, mirrorErr)
	}
}

type publicationObserverStub struct {
	observations []publicationObservation
}

func (o *publicationObserverStub) ObserveBehaviorPublication(stream, role, transport, result string) {
	o.observations = append(o.observations, publicationObservation{
		stream: stream, role: role, transport: transport, result: result,
	})
}

func TestDualPublishObservesCombinedOutcome(t *testing.T) {
	observer := &publicationObserverStub{}
	rabbit := &rabbitPublisherStub{}
	kafka := &kafkaPublisherStub{err: infrakafka.ErrProduceUncertain}
	publisher, err := NewViewPublisher(
		infrakafka.ProducerModeRabbitWithKafkaMirror,
		rabbit,
		kafka,
		observer,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := publisher.PublishViewEventRecorded(
		context.Background(),
		viewFixture(),
	); !errors.Is(err, infrakafka.ErrProduceUncertain) {
		t.Fatalf("error=%v", err)
	}
	last := observer.observations[len(observer.observations)-1]
	if last.stream != StreamView || last.role != "combined" ||
		last.transport != "dual" || last.result != "uncertain" {
		t.Fatalf("combined observation=%+v", last)
	}
}

type dualViewOutboxStore struct {
	pending    bool
	leased     bool
	dispatched int
	failed     int
	event      *applicationexposure.ViewEventRecordedEvent
}

func (s *dualViewOutboxStore) ClaimViewEventOutbox(
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

func (s *dualViewOutboxStore) MarkViewEventOutboxDispatched(
	context.Context,
	int64,
	time.Time,
) error {
	s.pending = false
	s.leased = false
	s.dispatched++
	return nil
}

func (s *dualViewOutboxStore) MarkViewEventOutboxFailed(
	context.Context,
	int64,
	time.Time,
	string,
) error {
	s.leased = false
	s.failed++
	return nil
}

func (s *dualViewOutboxStore) ViewEventOutboxStats(
	context.Context,
	time.Time,
) (applicationexposure.OutboxStats, error) {
	if s.pending {
		return applicationexposure.OutboxStats{Pending: 1}, nil
	}
	return applicationexposure.OutboxStats{}, nil
}

func TestDualViewPublishFailureLeavesOutboxPending(t *testing.T) {
	kafkaErr := errors.New("Kafka unavailable")
	publisher, err := NewViewPublisher(
		infrakafka.ProducerModeRabbitWithKafkaMirror,
		&rabbitPublisherStub{},
		&kafkaPublisherStub{err: kafkaErr},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	store := &dualViewOutboxStore{pending: true, event: viewFixture()}
	dispatched, err := applicationexposure.NewOutboxDispatcher(store, publisher).
		DispatchOnce(context.Background())
	if !errors.Is(err, kafkaErr) || dispatched != 0 || !store.pending ||
		store.dispatched != 0 || store.failed != 1 {
		t.Fatalf("dispatched=%d err=%v store=%+v", dispatched, err, store)
	}
}

func TestDualViewPartialAcknowledgementsLeaveOutboxPending(t *testing.T) {
	failure := errors.New("transport unavailable")
	for _, test := range []struct {
		name      string
		rabbitErr error
		kafkaErr  error
	}{
		{name: "primary only", kafkaErr: failure},
		{name: "mirror only", rabbitErr: failure},
		{name: "neither", rabbitErr: failure, kafkaErr: failure},
	} {
		t.Run(test.name, func(t *testing.T) {
			publisher, err := NewViewPublisher(
				infrakafka.ProducerModeRabbitWithKafkaMirror,
				&rabbitPublisherStub{viewErr: test.rabbitErr},
				&kafkaPublisherStub{err: test.kafkaErr},
				nil,
			)
			if err != nil {
				t.Fatal(err)
			}
			store := &dualViewOutboxStore{pending: true, event: viewFixture()}
			dispatched, err := applicationexposure.NewOutboxDispatcher(store, publisher).
				DispatchOnce(context.Background())
			if err == nil || dispatched != 0 || !store.pending ||
				store.dispatched != 0 || store.failed != 1 {
				t.Fatalf("dispatched=%d err=%v store=%+v", dispatched, err, store)
			}
		})
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
