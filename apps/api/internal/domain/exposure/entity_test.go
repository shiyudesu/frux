package domainexposure

import (
	"errors"
	"testing"
	"time"
)

func TestViewEventProjectionOwnership(t *testing.T) {
	exposed, err := NewViewEvent(NewViewEventInput{UserID: 1, VideoID: 2, Scene: "timeline", EventType: EventTypeExposed})
	if err != nil {
		t.Fatalf("new exposed event: %v", err)
	}
	if exposed.CountsAsHistory() {
		t.Fatal("exposed-only event entered watch history")
	}
	for _, eventType := range []string{EventTypePlay, EventTypeComplete, EventTypeSkip} {
		event, err := NewViewEvent(NewViewEventInput{UserID: 1, VideoID: 2, Scene: "timeline", EventType: eventType, WatchMs: 100})
		if err != nil {
			t.Fatalf("new %s event: %v", eventType, err)
		}
		if !event.CountsAsHistory() {
			t.Fatalf("%s event did not enter watch history", eventType)
		}
	}
}

func TestViewEventEnvelopeValidation(t *testing.T) {
	occurredAt := time.Date(2026, 7, 25, 10, 0, 0, 123456789, time.UTC)
	duration := 60_000
	event, err := NewViewEvent(NewViewEventInput{
		UserID: 1, VideoID: 2, Scene: " TIMELINE ", RequestID: " req ",
		EventType: EventTypeProgress, EventID: " event-1 ", PlaybackSessionID: " session-1 ",
		Sequence: 3, OccurredAt: occurredAt, PositionMs: 20_000, WatchMs: 18_000, DurationMs: &duration,
	})
	if err != nil {
		t.Fatalf("new progress event: %v", err)
	}
	if event.EventID != "event-1" || event.PlaybackSessionID != "session-1" || event.Scene != "timeline" {
		t.Fatalf("event was not normalized: %+v", event)
	}
	if event.OccurredAt.Nanosecond() != 123456000 {
		t.Fatalf("occurred_at was not normalized to PostgreSQL precision: %s", event.OccurredAt)
	}

	_, err = NewViewEvent(NewViewEventInput{
		UserID: 1, VideoID: 2, Scene: "timeline", EventType: EventTypeProgress,
	})
	if !errors.Is(err, ErrEmptyEventID) {
		t.Fatalf("expected progress envelope error, got %v", err)
	}
}
