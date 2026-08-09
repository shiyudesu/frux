package applicationeventstream

import (
	"context"
	"errors"
	"testing"
	"time"
)

type parityCheckerFunc func(context.Context, Event) (ParityResult, error)

func (f parityCheckerFunc) Compare(ctx context.Context, event Event) (ParityResult, error) {
	return f(ctx, event)
}

type shadowObserverStub struct {
	results []string
}

func (o *shadowObserverStub) ObserveShadow(result string) {
	o.results = append(o.results, result)
}

func TestShadowHandlerNeverUsesActiveGroupOrMutatingOutcome(t *testing.T) {
	if _, err := NewShadowHandler("frux.active.v1", time.Hour, nil, nil); err == nil {
		t.Fatal("active group accepted as shadow")
	}
	handler, err := NewShadowHandler("frux.probe.shadow.v1", time.Hour, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	handler.now = func() time.Time { return now }
	outcome, err := handler.Handle(context.Background(), Event{
		Metadata: RecordMetadata{Group: "frux.probe.shadow.v1", Key: []byte("probe:one")},
		EventID:  "event-one", EventType: "probe", ProducedAt: now.Add(-time.Minute),
	})
	if err != nil || outcome != OutcomeDurableSuccess {
		t.Fatalf("outcome=%s error=%v", outcome, err)
	}
	outcome, err = handler.Handle(context.Background(), Event{
		Metadata: RecordMetadata{Group: "frux.probe.active.v1", Key: []byte("probe:one")},
		EventID:  "event-one", EventType: "probe", ProducedAt: now,
	})
	if err != nil || outcome != OutcomeTerminal {
		t.Fatalf("active group outcome=%s error=%v", outcome, err)
	}
}

func TestShadowHandlerAgeAndParity(t *testing.T) {
	now := time.Now().UTC()
	handler, err := NewShadowHandler(
		"frux.probe.shadow.v1", time.Hour,
		parityCheckerFunc(func(context.Context, Event) (ParityResult, error) {
			return ParityMatch, nil
		}),
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}

	handler.now = func() time.Time { return now }
	event := Event{
		Metadata: RecordMetadata{Group: "frux.probe.shadow.v1", Key: []byte("probe:one")},
		EventID:  "event-one", EventType: "probe", ProducedAt: now.Add(-2 * time.Hour),
	}
	if outcome, _ := handler.Handle(context.Background(), event); outcome != OutcomeTerminal {
		t.Fatalf("expired outcome = %s", outcome)
	}
	event.ProducedAt = now
	handler.parity = parityCheckerFunc(func(context.Context, Event) (ParityResult, error) {
		return "", errors.New("database unavailable")
	})
	if outcome, err := handler.Handle(context.Background(), event); outcome != OutcomeRetryable || err == nil {
		t.Fatalf("parity outcome=%s error=%v", outcome, err)
	}
}

func TestShadowHandlerRetriesPendingParityThenMatches(t *testing.T) {
	now := time.Now().UTC()
	calls := 0
	observer := &shadowObserverStub{}
	handler, err := NewShadowHandler(
		"frux.probe.shadow.v1",
		time.Hour,
		parityCheckerFunc(func(context.Context, Event) (ParityResult, error) {
			calls++
			if calls < 3 {
				return ParityPending, nil
			}
			return ParityMatch, nil
		}),
		observer,
	)
	if err != nil {
		t.Fatal(err)
	}
	handler.now = func() time.Time { return now }
	handler.retryDelay = 10 * time.Millisecond
	event := Event{
		Metadata: RecordMetadata{Group: "frux.probe.shadow.v1", Key: []byte("probe:one")},
		EventID:  "event-pending", EventType: "probe", ProducedAt: now,
	}
	for attempt := 0; attempt < 2; attempt++ {
		outcome, err := handler.Handle(context.Background(), event)
		var pending PendingParityError
		if outcome != OutcomeRetryable || !errors.As(err, &pending) ||
			pending.RetryAfter() != 10*time.Millisecond {
			t.Fatalf("attempt=%d outcome=%s error=%v", attempt, outcome, err)
		}
	}
	outcome, err := handler.Handle(context.Background(), event)
	if err != nil || outcome != OutcomeDurableSuccess ||
		len(observer.results) != 3 ||
		observer.results[2] != "parity_match" {
		t.Fatalf("outcome=%s error=%v results=%v", outcome, err, observer.results)
	}
}

func TestShadowHandlerBoundsMissingFactRetriesWithoutMismatch(t *testing.T) {
	now := time.Now().UTC()
	observer := &shadowObserverStub{}
	handler, err := NewShadowHandler(
		"frux.probe.shadow.v1",
		time.Hour,
		parityCheckerFunc(func(context.Context, Event) (ParityResult, error) {
			return ParityPending, nil
		}),
		observer,
	)
	if err != nil {
		t.Fatal(err)
	}
	handler.now = func() time.Time { return now }
	handler.maxRetries = 2
	event := Event{
		Metadata: RecordMetadata{Group: "frux.probe.shadow.v1", Key: []byte("probe:one")},
		EventID:  "event-never-arrives", EventType: "probe", ProducedAt: now,
	}
	for attempt := 0; attempt < 2; attempt++ {
		if outcome, _ := handler.Handle(context.Background(), event); outcome != OutcomeRetryable {
			t.Fatalf("attempt=%d outcome=%s", attempt, outcome)
		}
	}
	if outcome, err := handler.Handle(context.Background(), event); err != nil || outcome != OutcomeDurableSuccess {
		t.Fatalf("final outcome=%s error=%v", outcome, err)
	}
	for _, result := range observer.results {
		if result == "parity_mismatch" {
			t.Fatalf("missing fact was recorded as mismatch: %v", observer.results)
		}
	}
	if observer.results[len(observer.results)-1] != "parity_pending_exhausted" {
		t.Fatalf("results=%v", observer.results)
	}
}
