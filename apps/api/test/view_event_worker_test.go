package test

import (
	applicationexposure "GCFeed/internal/application/exposure"
	applicationrecommendation "GCFeed/internal/application/recommendation"
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type memoryOutboxStore struct {
	mu          sync.Mutex
	item        applicationexposure.OutboxItem
	availableAt time.Time
	leasedUntil time.Time
	dispatched  bool
	failures    int
}

func (s *memoryOutboxStore) ClaimViewEventOutbox(_ context.Context, _ int, now, leasedUntil time.Time) ([]applicationexposure.OutboxItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dispatched || now.Before(s.availableAt) || now.Before(s.leasedUntil) {
		return []applicationexposure.OutboxItem{}, nil
	}
	s.item.Attempts++
	s.leasedUntil = leasedUntil
	return []applicationexposure.OutboxItem{s.item}, nil
}

func (s *memoryOutboxStore) MarkViewEventOutboxDispatched(_ context.Context, _ int64, _ time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dispatched = true
	s.leasedUntil = time.Time{}
	return nil
}

func (s *memoryOutboxStore) MarkViewEventOutboxFailed(_ context.Context, _ int64, availableAt time.Time, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failures++
	s.availableAt = availableAt
	s.leasedUntil = time.Time{}
	return nil
}

func (s *memoryOutboxStore) ViewEventOutboxStats(_ context.Context, _ time.Time) (applicationexposure.OutboxStats, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dispatched {
		return applicationexposure.OutboxStats{}, nil
	}
	return applicationexposure.OutboxStats{Pending: 1, OldestPending: s.item.Event.RecordedAt}, nil
}

type flakyViewEventPublisher struct {
	mu        sync.Mutex
	failures  int
	published []string
}

func (p *flakyViewEventPublisher) PublishViewEventRecorded(_ context.Context, event *applicationexposure.ViewEventRecordedEvent) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.failures > 0 {
		p.failures--
		return errors.New("rabbitmq unavailable")
	}
	p.published = append(p.published, event.EventID)
	return nil
}

func TestViewEventOutboxDispatcherRetriesAfterPublishFailure(t *testing.T) {
	now := time.Date(2026, 7, 25, 10, 0, 0, 0, time.UTC)
	store := &memoryOutboxStore{
		item: applicationexposure.OutboxItem{
			ID: 1,
			Event: &applicationexposure.ViewEventRecordedEvent{
				EventID: "event-1", ViewEventID: 11, UserID: 42, VideoID: 1001,
				EventType: "progress", RecordedAt: now, OccurredAt: now,
			},
		},
		availableAt: now,
	}
	publisher := &flakyViewEventPublisher{failures: 1}
	current := now
	dispatcher := applicationexposure.NewOutboxDispatcher(
		store,
		publisher,
		applicationexposure.WithOutboxNow(func() time.Time { return current }),
	)

	if dispatched, err := dispatcher.DispatchOnce(context.Background()); err == nil || dispatched != 0 {
		t.Fatalf("expected first publish failure, dispatched=%d err=%v", dispatched, err)
	}
	if store.failures != 1 || store.dispatched {
		t.Fatalf("failed row was not released for retry: %+v", store)
	}

	current = current.Add(2 * time.Second)
	if dispatched, err := dispatcher.DispatchOnce(context.Background()); err != nil || dispatched != 1 {
		t.Fatalf("retry dispatch failed: dispatched=%d err=%v", dispatched, err)
	}
	if len(publisher.published) != 1 || publisher.published[0] != "event-1" {
		t.Fatalf("unexpected published events: %+v", publisher.published)
	}
}

type memoryBehaviorEventRepo struct {
	mu      sync.Mutex
	applied map[string]struct{}
}

func (r *memoryBehaviorEventRepo) ApplyBehaviorEvent(_ context.Context, event *applicationexposure.ViewEventRecordedEvent) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.applied[event.EventID]; exists {
		return false, nil
	}
	r.applied[event.EventID] = struct{}{}
	return true, nil
}

func TestRecommendationWorkerDeduplicatesViewEventDelivery(t *testing.T) {
	repo := &memoryBehaviorEventRepo{applied: map[string]struct{}{}}
	worker := applicationrecommendation.NewBehaviorEventWorker(repo, nil)
	event := &applicationexposure.ViewEventRecordedEvent{
		EventID: "event-duplicate", ViewEventID: 12, UserID: 42, VideoID: 1001,
		EventType: "progress", Sequence: 2, PositionMs: 20_000, WatchMs: 18_000,
		OccurredAt: time.Now().UTC(),
	}
	if err := worker.Handle(context.Background(), event); err != nil {
		t.Fatalf("handle first delivery: %v", err)
	}
	if err := worker.Handle(context.Background(), event); err != nil {
		t.Fatalf("handle duplicate delivery: %v", err)
	}
	if len(repo.applied) != 1 {
		t.Fatalf("duplicate delivery was applied more than once: %+v", repo.applied)
	}
}
