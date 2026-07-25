package domainexposure

import "testing"

func TestViewEventProjectionOwnership(t *testing.T) {
	exposed, err := NewViewEvent(1, 2, "timeline", "", EventTypeExposed, 0, false)
	if err != nil {
		t.Fatalf("new exposed event: %v", err)
	}
	if exposed.CountsAsHistory() {
		t.Fatal("exposed-only event entered watch history")
	}
	for _, eventType := range []string{EventTypePlay, EventTypeComplete, EventTypeSkip} {
		event, err := NewViewEvent(1, 2, "timeline", "", eventType, 100, false)
		if err != nil {
			t.Fatalf("new %s event: %v", eventType, err)
		}
		if !event.CountsAsHistory() {
			t.Fatalf("%s event did not enter watch history", eventType)
		}
	}
}
