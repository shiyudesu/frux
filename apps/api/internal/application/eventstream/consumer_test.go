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
