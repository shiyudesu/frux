package applicationvideo

import (
	"context"
	"errors"
	"testing"
	"time"
)

type publicationStoreStub struct {
	items      []*PublicationOutboxItem
	ensured    int
	dispatched int
	failed     int
}

func (s *publicationStoreStub) EnsurePublicationEvent(
	context.Context, *PublishedEvent, time.Time,
) error {
	s.ensured++
	return nil
}
func (s *publicationStoreStub) ClaimPublicationEvents(
	context.Context, string, int, time.Time, time.Time,
) ([]*PublicationOutboxItem, error) {
	items := s.items
	s.items = nil
	return items, nil
}
func (s *publicationStoreStub) MarkPublicationEventDispatched(
	context.Context, string, string, time.Time,
) error {
	s.dispatched++
	return nil
}
func (s *publicationStoreStub) MarkPublicationEventFailed(
	context.Context, string, string, time.Time, string,
) error {
	s.failed++
	return nil
}
func (*publicationStoreStub) PublicationOutboxStats(
	context.Context, time.Time,
) (PublicationOutboxStats, error) {
	return PublicationOutboxStats{}, nil
}
func (*publicationStoreStub) ReconcilePublicationEvents(
	context.Context, int, time.Time,
) (int, error) {
	return 0, nil
}

type publicationPublisherStub struct {
	err   error
	calls int
}

func (p *publicationPublisherStub) PublishVideoPublished(
	context.Context, *PublishedEvent,
) error {
	p.calls++
	return p.err
}

func TestDurablePublicationBoundaryAndDispatcher(t *testing.T) {
	now := time.Now().UTC()
	event := &PublishedEvent{
		EventID: "video-published:1:1", VideoID: 1, AuthorID: 2,
		PublishedAt: now, OccurredAt: now,
	}
	store := &publicationStoreStub{items: []*PublicationOutboxItem{{
		Event: event, Attempts: 1,
	}}}
	durable := NewDurablePublicationPublisher(store)
	if err := durable.PublishVideoPublished(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if store.ensured != 1 {
		t.Fatalf("ensured = %d", store.ensured)
	}
	publisher := &publicationPublisherStub{}
	dispatcher := NewPublicationOutboxDispatcher(store, publisher, nil)
	dispatcher.now = func() time.Time { return now }
	if processed, err := dispatcher.RunOnce(context.Background()); err != nil || processed != 1 {
		t.Fatalf("processed=%d err=%v", processed, err)
	}
	if publisher.calls != 1 || store.dispatched != 1 {
		t.Fatalf("calls=%d dispatched=%d", publisher.calls, store.dispatched)
	}

	store.items = []*PublicationOutboxItem{{Event: event, Attempts: 2}}
	publisher.err = errors.New("kafka unavailable")
	if _, err := dispatcher.RunOnce(context.Background()); err == nil {
		t.Fatal("transport failure was hidden")
	}
	if store.failed != 1 {
		t.Fatalf("failed marks = %d", store.failed)
	}
	store.items = []*PublicationOutboxItem{{Event: event, Attempts: 3}}
	publisher.err = nil
	if processed, err := dispatcher.RunOnce(context.Background()); err != nil || processed != 1 {
		t.Fatalf("recovery processed=%d err=%v", processed, err)
	}
	if store.dispatched != 2 {
		t.Fatalf("recovery dispatched = %d", store.dispatched)
	}
}
