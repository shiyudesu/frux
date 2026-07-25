package applicationinteraction

import (
	domaininteraction "GCFeed/internal/domain/interaction"
	"context"
	"errors"
	"testing"
	"time"
)

type acceptedActionEventRepositoryStub struct {
	err    error
	events []*domaininteraction.AcceptedActionEvent
}

func (r *acceptedActionEventRepositoryStub) PersistAcceptedActionEvent(_ context.Context, event *domaininteraction.AcceptedActionEvent) error {
	r.events = append(r.events, event)
	return r.err
}

func TestActionWorkerClassifiesMissingVideoAsTerminal(t *testing.T) {
	repo := &acceptedActionEventRepositoryStub{err: domaininteraction.ErrVideoNotFound}
	worker := NewActionWorker(repo, nil)

	err := worker.HandleActionChanged(context.Background(), validActionChangedEvent())
	if !IsTerminalActionEventError(err) {
		t.Fatalf("missing video should be terminal, got %v", err)
	}
	if len(repo.events) != 1 {
		t.Fatalf("expected one persistence attempt, got %d", len(repo.events))
	}
}

func TestActionWorkerClassifiesInvalidEventAsTerminal(t *testing.T) {
	repo := &acceptedActionEventRepositoryStub{}
	worker := NewActionWorker(repo, nil)
	event := validActionChangedEvent()
	event.EventID = ""

	err := worker.HandleActionChanged(context.Background(), event)
	if !IsTerminalActionEventError(err) {
		t.Fatalf("invalid event should be terminal, got %v", err)
	}
	if len(repo.events) != 0 {
		t.Fatalf("invalid event reached persistence: %+v", repo.events)
	}
}

func TestActionWorkerLeavesTransientPersistenceFailureRetryable(t *testing.T) {
	transient := errors.New("database unavailable")
	repo := &acceptedActionEventRepositoryStub{err: transient}
	worker := NewActionWorker(repo, nil)

	err := worker.HandleActionChanged(context.Background(), validActionChangedEvent())
	if !errors.Is(err, transient) {
		t.Fatalf("expected transient error, got %v", err)
	}
	if IsTerminalActionEventError(err) {
		t.Fatalf("transient error was classified terminal: %v", err)
	}
}

func TestActionWorkerAcknowledgesRepositoryNoOp(t *testing.T) {
	repo := &acceptedActionEventRepositoryStub{}
	worker := NewActionWorker(repo, nil)

	if err := worker.HandleActionChanged(context.Background(), validActionChangedEvent()); err != nil {
		t.Fatalf("duplicate or stale repository no-op should be acknowledged: %v", err)
	}
}

func TestActionWorkerClassifiesEventConflictAsTerminal(t *testing.T) {
	repo := &acceptedActionEventRepositoryStub{err: domaininteraction.ErrActionEventConflict}
	worker := NewActionWorker(repo, nil)

	err := worker.HandleActionChanged(context.Background(), validActionChangedEvent())
	if !IsTerminalActionEventError(err) {
		t.Fatalf("event conflict should be terminal, got %v", err)
	}
}

func validActionChangedEvent() *ActionChangedEvent {
	return &ActionChangedEvent{
		EventID:        "event-1",
		UserID:         7,
		VideoID:        11,
		ActionType:     domaininteraction.ActionTypeLike,
		Active:         true,
		IdempotencyKey: "request-1",
		Version:        1,
		OccurredAt:     time.Now().UTC(),
	}
}
