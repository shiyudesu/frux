package domaingovernance

import (
	"errors"
	"testing"
	"time"
)

func TestRegistryRejectsUnknownKeysAndEnforcesProcessScope(t *testing.T) {
	registry := DefaultRegistry()
	definition, err := registry.Require(FeedPreloadEnabled)
	if err != nil {
		t.Fatalf("require registered control: %v", err)
	}
	if !definition.Supports(ProcessAPI) || !definition.Supports(ProcessWorker) {
		t.Fatalf("unexpected process scope: %#v", definition.Processes)
	}
	if _, err := registry.Require("runtime.custom"); !errors.Is(err, ErrUnknownControl) {
		t.Fatalf("unknown key error = %v", err)
	}

	apiOnly, err := NewRegistry([]Definition{{
		Key: "api.only", Owner: "test", Description: "test control",
		ValueType: ValueTypeBoolean, Default: BooleanValue(true),
		FailureDefault: BooleanValue(false), Processes: []Process{ProcessAPI},
		MaxStaleness: time.Minute,
	}})
	if err != nil {
		t.Fatalf("new registry: %v", err)
	}
	scoped, _ := apiOnly.Require("api.only")
	if scoped.Supports(ProcessWorker) {
		t.Fatal("worker unexpectedly supports API-only control")
	}
}

func TestRevisionValidationExpiryAndRollback(t *testing.T) {
	registry := DefaultRegistry()
	now := time.Date(2026, 8, 6, 8, 0, 0, 0, time.UTC)
	expiry := now.Add(time.Minute)
	revision, err := NewRevision(registry, RevisionInput{
		Key: FeedPreloadEnabled, Revision: 2, Value: BooleanValue(false),
		Reason: "incident mitigation", ExpiresAt: &expiry, ActorID: 9,
		CreatedAt: now, RollbackFromRevision: 1,
	}, now)
	if err != nil {
		t.Fatalf("new rollback revision: %v", err)
	}
	if revision.Number() != 2 || revision.RollbackFromRevision() != 1 ||
		revision.Expired(now) || !revision.Expired(expiry) {
		t.Fatalf("unexpected revision: %+v", revision)
	}

	tests := []struct {
		name  string
		input RevisionInput
		err   error
	}{
		{
			name: "unknown key",
			input: RevisionInput{Key: "unknown", Revision: 1, Value: BooleanValue(true),
				Reason: "test", ActorID: 1, CreatedAt: now},
			err: ErrUnknownControl,
		},
		{
			name: "invalid revision",
			input: RevisionInput{Key: FeedPreloadEnabled, Revision: 0, Value: BooleanValue(true),
				Reason: "test", ActorID: 1, CreatedAt: now},
			err: ErrInvalidRevision,
		},
		{
			name: "invalid rollback target",
			input: RevisionInput{Key: FeedPreloadEnabled, Revision: 2, Value: BooleanValue(true),
				Reason: "test", ActorID: 1, CreatedAt: now, RollbackFromRevision: 2},
			err: ErrInvalidRevision,
		},
		{
			name: "expired at creation",
			input: RevisionInput{Key: FeedPreloadEnabled, Revision: 1, Value: BooleanValue(true),
				Reason: "test", ActorID: 1, CreatedAt: now, ExpiresAt: &now},
			err: ErrInvalidExpiry,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NewRevision(registry, tt.input, now); !errors.Is(err, tt.err) {
				t.Fatalf("NewRevision() error = %v, want %v", err, tt.err)
			}
		})
	}
}
