package applicationexposure

import (
	"context"
	"errors"
	"testing"
	"time"
)

type outboxStoreStub struct {
	item       OutboxItem
	pending    bool
	leased     bool
	dispatched int
	failed     int
}

func (s *outboxStoreStub) ClaimViewEventOutbox(
	context.Context,
	int,
	time.Time,
	time.Time,
) ([]OutboxItem, error) {
	if !s.pending || s.leased {
		return nil, nil
	}
	s.leased = true
	s.item.Attempts++
	return []OutboxItem{s.item}, nil
}

func (s *outboxStoreStub) MarkViewEventOutboxDispatched(
	context.Context,
	int64,
	time.Time,
) error {
	s.pending = false
	s.leased = false
	s.dispatched++
	return nil
}

func (s *outboxStoreStub) MarkViewEventOutboxFailed(
	context.Context,
	int64,
	time.Time,
	string,
) error {
	s.leased = false
	s.failed++
	return nil
}

func (s *outboxStoreStub) ViewEventOutboxStats(
	context.Context,
	time.Time,
) (OutboxStats, error) {
	if s.pending {
		return OutboxStats{Pending: 1}, nil
	}
	return OutboxStats{}, nil
}

type viewPublisherStub struct {
	errors []error
	calls  int
}

func (p *viewPublisherStub) PublishViewEventRecorded(
	context.Context,
	*ViewEventRecordedEvent,
) error {
	p.calls++
	if len(p.errors) == 0 {
		return nil
	}
	err := p.errors[0]
	p.errors = p.errors[1:]
	return err
}

func TestOutboxMarksDispatchedOnlyAfterPrimaryAcknowledgement(t *testing.T) {
	store := outboxFixture()
	publisher := &viewPublisherStub{}
	dispatcher := NewOutboxDispatcher(store, publisher)
	count, err := dispatcher.DispatchOnce(context.Background())
	if err != nil || count != 1 || store.dispatched != 1 || store.failed != 0 {
		t.Fatalf("count=%d err=%v store=%#v", count, err, store)
	}
	if count, err := dispatcher.DispatchOnce(context.Background()); err != nil || count != 0 {
		t.Fatalf("duplicate dispatch count=%d err=%v", count, err)
	}
}

func TestOutboxKafkaOutageRetainsPendingRowForRestartRecovery(t *testing.T) {
	store := outboxFixture()
	publisher := &viewPublisherStub{errors: []error{errors.New("Kafka unavailable"), nil}}
	dispatcher := NewOutboxDispatcher(store, publisher)
	count, err := dispatcher.DispatchOnce(context.Background())
	if err == nil || count != 0 || store.failed != 1 || !store.pending {
		t.Fatalf("count=%d err=%v store=%#v", count, err, store)
	}
	restarted := NewOutboxDispatcher(store, publisher)
	count, err = restarted.DispatchOnce(context.Background())
	if err != nil || count != 1 || store.dispatched != 1 || store.pending {
		t.Fatalf("count=%d err=%v store=%#v", count, err, store)
	}
}

func TestOutboxDuplicateClaimAfterAcknowledgementIsIdempotent(t *testing.T) {
	store := outboxFixture()
	publisher := &viewPublisherStub{}
	first := NewOutboxDispatcher(store, publisher)
	if count, err := first.DispatchOnce(context.Background()); err != nil || count != 1 {
		t.Fatalf("first count=%d err=%v", count, err)
	}
	restarted := NewOutboxDispatcher(store, publisher)
	if count, err := restarted.DispatchOnce(context.Background()); err != nil || count != 0 {
		t.Fatalf("restart count=%d err=%v", count, err)
	}
	if publisher.calls != 1 {
		t.Fatalf("publish calls = %d", publisher.calls)
	}
}

func outboxFixture() *outboxStoreStub {
	now := time.Now().UTC()
	return &outboxStoreStub{
		pending: true,
		item: OutboxItem{
			ID: 1,
			Event: &ViewEventRecordedEvent{
				EventID: "view-event-1", ViewEventID: 1, UserID: 7, VideoID: 11,
				EventType: "play", RecordedAt: now, OccurredAt: now,
			},
		},
	}
}
