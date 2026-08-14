package domainaccount

import (
	"errors"
	"testing"
	"time"
)

func TestRefreshSessionMatchingAndExpiry(t *testing.T) {
	now := time.Date(2026, 8, 13, 4, 0, 0, 0, time.UTC)
	session, err := NewRefreshSession("session", "family", 7, "current", 2, now, now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	previousUntil := now.Add(10 * time.Second)
	session.PreviousSecretHash = "previous"
	session.PreviousSecretValidTo = &previousUntil
	if !session.Active(now) || !session.MatchesCurrent("current") {
		t.Fatalf("session should be active: %+v", session)
	}
	if !session.MatchesPreviousWithinGrace("previous", now.Add(5*time.Second)) {
		t.Fatal("previous secret should match during grace")
	}
	if session.MatchesPreviousWithinGrace("previous", now.Add(11*time.Second)) {
		t.Fatal("previous secret should not match after grace")
	}
	if !session.MatchesPrevious("previous") {
		t.Fatal("previous replay evidence should remain comparable")
	}
}

func TestNewRefreshSessionValidation(t *testing.T) {
	now := time.Now()
	if _, err := NewRefreshSession("", "family", 1, "hash", 1, now, now.Add(time.Hour)); !errors.Is(err, ErrInvalidRefreshSessionID) {
		t.Fatalf("session id error = %v", err)
	}
	if _, err := NewRefreshSession("session", "family", 1, "hash", 1, now, now); !errors.Is(err, ErrRefreshSessionExpired) {
		t.Fatalf("expiry error = %v", err)
	}
}
