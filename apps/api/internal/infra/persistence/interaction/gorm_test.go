package infrainteraction

import (
	domaininteraction "GCFeed/internal/domain/interaction"
	"testing"
	"time"
)

func TestSameAcceptedActionEventRejectsRecommendationRequestReattribution(t *testing.T) {
	occurredAt := time.Date(2026, 7, 27, 3, 0, 0, 0, time.UTC)
	original, err := domaininteraction.NewAcceptedActionEventWithRecommendation(
		"event-1", 7, 11, domaininteraction.ActionTypeLike, true, "key-1", "request-original", 1, occurredAt,
	)
	if err != nil {
		t.Fatal(err)
	}
	reattributed, err := domaininteraction.NewAcceptedActionEventWithRecommendation(
		"event-1", 7, 11, domaininteraction.ActionTypeLike, true, "key-1", "request-other", 1, occurredAt,
	)
	if err != nil {
		t.Fatal(err)
	}
	if sameAcceptedActionEvent(actionEventModelFromDomain(original), actionEventModelFromDomain(reattributed)) {
		t.Fatal("same event ID with a different recommendation request ID was accepted")
	}
}

func TestNewerSameStateAcceptedEventAdvancesMaterializedOrder(t *testing.T) {
	base := time.Date(2026, 7, 27, 3, 30, 0, 0, time.UTC)
	current := ActionModel{
		Status:                  domaininteraction.ActionStatusActive,
		RecommendationRequestID: "request-v1",
		LatestEventVersion:      1,
	}
	eventID := "active-v1"
	current.LatestEventOccurredAt = &base
	current.LatestEventID = &eventID

	newest, err := domaininteraction.NewAcceptedActionEventWithRecommendation(
		"active-v3", 7, 11, domaininteraction.ActionTypeLike, true, "key-v3", "request-v3", 3, base.Add(2*time.Second),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !acceptedActionEventTransitionsState(current, true, newest) {
		t.Fatal("a newer event with unchanged active state must advance materialized order")
	}
	current.RecommendationRequestID = newest.RecommendationRequestID
	setLatestActionOrder(&current, newest)

	stale, err := domaininteraction.NewAcceptedActionEventWithRecommendation(
		"inactive-v2", 7, 11, domaininteraction.ActionTypeLike, false, "key-v2", "request-v2", 2, base.Add(time.Second),
	)
	if err != nil {
		t.Fatal(err)
	}
	if acceptedActionEventTransitionsState(current, true, stale) {
		t.Fatal("older inactive event must not reverse newer active state")
	}
	if current.Status != domaininteraction.ActionStatusActive || current.RecommendationRequestID != "request-v3" ||
		current.LatestEventVersion != 3 || current.LatestEventID == nil || *current.LatestEventID != "active-v3" {
		t.Fatalf("newest same-state metadata was not retained: %+v", current)
	}
}
