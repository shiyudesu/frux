package applicationgovernance

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	domaingovernance "github.com/shiyudesu/frux/internal/domain/governance"
)

type runtimeSource struct {
	mu        sync.Mutex
	revisions []*domaingovernance.Revision
	err       error
}

func (s *runtimeSource) ListActive(context.Context) ([]*domaingovernance.Revision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return nil, s.err
	}
	return append([]*domaingovernance.Revision(nil), s.revisions...), nil
}

func TestRuntimeFreshMissingExpiredFailureAndStaleness(t *testing.T) {
	registry := domaingovernance.DefaultRegistry()
	now := time.Date(2026, 8, 6, 9, 0, 0, 0, time.UTC)
	clock := now
	source := &runtimeSource{}
	runtime := NewRuntime(registry, domaingovernance.ProcessAPI, source,
		WithRuntimeClock(func() time.Time { return clock }))

	if runtime.Bool(domaingovernance.FeedPreloadEnabled) {
		t.Fatal("unloaded runtime must use failure default")
	}
	if err := runtime.Refresh(context.Background()); err != nil {
		t.Fatalf("refresh missing snapshot: %v", err)
	}
	if !runtime.Bool(domaingovernance.FeedPreloadEnabled) {
		t.Fatal("missing active revision must use registered default")
	}

	expiry := now.Add(time.Minute)
	revision, err := domaingovernance.NewRevision(registry, domaingovernance.RevisionInput{
		Key: domaingovernance.FeedPreloadEnabled, Revision: 1,
		Value: domaingovernance.BooleanValue(false), Reason: "incident",
		ExpiresAt: &expiry, ActorID: 7, CreatedAt: now,
	}, now)
	if err != nil {
		t.Fatalf("new revision: %v", err)
	}
	source.revisions = []*domaingovernance.Revision{revision}
	if err := runtime.Refresh(context.Background()); err != nil {
		t.Fatalf("refresh active snapshot: %v", err)
	}
	if runtime.Bool(domaingovernance.FeedPreloadEnabled) {
		t.Fatal("fresh active false value was not applied")
	}

	source.err = errors.New("database unavailable")
	clock = now.Add(30 * time.Second)
	if err := runtime.Refresh(context.Background()); err == nil {
		t.Fatal("expected polling failure")
	}
	if runtime.Bool(domaingovernance.FeedPreloadEnabled) {
		t.Fatal("last-known-good value was not retained")
	}

	clock = expiry
	if !runtime.Bool(domaingovernance.FeedPreloadEnabled) {
		t.Fatal("expired revision must use normal default")
	}
	clock = now.Add(3 * time.Minute)
	if runtime.Bool(domaingovernance.FeedPreloadEnabled) {
		t.Fatal("over-stale snapshot must use failure default")
	}
}

func TestRuntimeRejectsInvalidSnapshotAndUnsupportedProcess(t *testing.T) {
	registry, err := domaingovernance.NewRegistry([]domaingovernance.Definition{{
		Key: "api.only", Owner: "test", Description: "api only",
		ValueType:      domaingovernance.ValueTypeBoolean,
		Default:        domaingovernance.BooleanValue(true),
		FailureDefault: domaingovernance.BooleanValue(false),
		Processes:      []domaingovernance.Process{domaingovernance.ProcessAPI},
		MaxStaleness:   time.Minute,
	}})
	if err != nil {
		t.Fatalf("new registry: %v", err)
	}
	source := &runtimeSource{}
	worker := NewRuntime(registry, domaingovernance.ProcessWorker, source)
	if err := worker.Refresh(context.Background()); err != nil {
		t.Fatalf("refresh worker: %v", err)
	}
	if worker.Bool("api.only") {
		t.Fatal("unsupported process must use failure default")
	}
	if worker.Bool("unknown") {
		t.Fatal("unknown control must fail closed")
	}
}

func TestRuntimeRetainsLastKnownGoodOnInvalidSnapshotAndStopsCleanly(t *testing.T) {
	registry := domaingovernance.DefaultRegistry()
	now := time.Date(2026, 8, 6, 9, 0, 0, 0, time.UTC)
	valid, err := domaingovernance.NewRevision(registry, domaingovernance.RevisionInput{
		Key: domaingovernance.FeedPreloadEnabled, Revision: 1,
		Value: domaingovernance.BooleanValue(false), Reason: "valid",
		ActorID: 1, CreatedAt: now,
	}, now)
	if err != nil {
		t.Fatalf("new valid revision: %v", err)
	}
	source := &runtimeSource{revisions: []*domaingovernance.Revision{valid}}
	runtime := NewRuntime(registry, domaingovernance.ProcessAPI, source,
		WithRuntimeClock(func() time.Time { return now }))
	if err := runtime.Refresh(context.Background()); err != nil {
		t.Fatalf("refresh valid snapshot: %v", err)
	}

	otherRegistry, err := domaingovernance.NewRegistry([]domaingovernance.Definition{{
		Key: "other.control", Owner: "test", Description: "other",
		ValueType:      domaingovernance.ValueTypeBoolean,
		Default:        domaingovernance.BooleanValue(true),
		FailureDefault: domaingovernance.BooleanValue(false),
		Processes:      []domaingovernance.Process{domaingovernance.ProcessAPI},
		MaxStaleness:   time.Minute,
	}})
	if err != nil {
		t.Fatalf("new other registry: %v", err)
	}
	unknown, err := domaingovernance.NewRevision(otherRegistry, domaingovernance.RevisionInput{
		Key: "other.control", Revision: 1, Value: domaingovernance.BooleanValue(true),
		Reason: "corrupt row", ActorID: 1, CreatedAt: now,
	}, now)
	if err != nil {
		t.Fatalf("new unknown revision: %v", err)
	}
	source.revisions = []*domaingovernance.Revision{unknown}
	if err := runtime.Refresh(context.Background()); !errors.Is(err, ErrInvalidSnapshot) {
		t.Fatalf("invalid snapshot error = %v", err)
	}
	if runtime.Bool(domaingovernance.FeedPreloadEnabled) {
		t.Fatal("invalid refresh replaced last-known-good snapshot")
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runtime.Run(ctx, 5*time.Millisecond, time.Millisecond) }()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("poller shutdown: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("poller did not stop after cancellation")
	}
}
