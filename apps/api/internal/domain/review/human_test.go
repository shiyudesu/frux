package domainreview

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestHumanLeaseOwnershipExpiryRenewalAndRelease(t *testing.T) {
	now := time.Date(2026, 8, 6, 4, 0, 0, 0, time.UTC)
	reviewCase := RestoreHumanCase(1, 2, 3, CaseStatusPendingHuman, 1, 50, 1, 0, "", nil, now, now, nil)
	tokenHash := strings.Repeat("a", 64)
	if err := reviewCase.Claim(7, tokenHash, 1, now, 10*time.Minute); err != nil {
		t.Fatal(err)
	}
	if reviewCase.Version != 2 || reviewCase.AssignedReviewerID != 7 {
		t.Fatalf("claimed case = %#v", reviewCase)
	}
	if err := reviewCase.Renew(8, tokenHash, 2, now.Add(time.Minute), 10*time.Minute); !errors.Is(err, ErrReviewLeaseNotOwned) {
		t.Fatalf("foreign renewal error = %v", err)
	}
	if err := reviewCase.Renew(7, tokenHash, 1, now.Add(time.Minute), 10*time.Minute); !errors.Is(err, ErrReviewCaseVersion) {
		t.Fatalf("stale renewal error = %v", err)
	}
	if err := reviewCase.Renew(7, tokenHash, 2, now.Add(10*time.Minute), 10*time.Minute); !errors.Is(err, ErrReviewLeaseExpired) {
		t.Fatalf("boundary expiry error = %v", err)
	}
	if !reviewCase.Expire(now.Add(10*time.Minute)) || reviewCase.AssignedReviewerID != 0 || reviewCase.Version != 3 {
		t.Fatalf("expired case = %#v", reviewCase)
	}
	if err := reviewCase.Claim(7, tokenHash, 3, now.Add(11*time.Minute), 10*time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := reviewCase.Release(7, tokenHash, 4, now.Add(12*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if reviewCase.AssignedReviewerID != 0 || reviewCase.Version != 5 {
		t.Fatalf("released case = %#v", reviewCase)
	}
}

func TestHumanDecisionValidationAndPayloadBinding(t *testing.T) {
	now := time.Date(2026, 8, 6, 4, 0, 0, 0, time.UTC)
	input := HumanDecisionInput{
		CaseID: 1, ReviewerID: 7, Outcome: OutcomeReject, ReasonCode: ReasonRejectSpam,
		Note: "  repeated promotion  ", ReviewVersion: 2, ExpectedCaseVersion: 4,
		IdempotencyKey: "decision-1", DecidedAt: now,
	}
	first, err := NewHumanDecision(input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewHumanDecision(input)
	if err != nil {
		t.Fatal(err)
	}
	if first.Note != "repeated promotion" || first.PayloadHash != second.PayloadHash ||
		first.IdempotencyKeyHash != second.IdempotencyKeyHash {
		t.Fatalf("normalized decisions differ: %#v %#v", first, second)
	}
	changed := input
	changed.ReasonCode = ReasonRejectHate
	third, err := NewHumanDecision(changed)
	if err != nil {
		t.Fatal(err)
	}
	if third.PayloadHash == first.PayloadHash {
		t.Fatal("different normalized payload reused hash")
	}
	invalid := input
	invalid.Outcome = OutcomeApprove
	if _, err := NewHumanDecision(invalid); !errors.Is(err, ErrInvalidReasonCode) {
		t.Fatalf("outcome/reason mismatch error = %v", err)
	}
	invalid = input
	invalid.ReasonCode = ReasonRejectOther
	invalid.Note = ""
	if _, err := NewHumanDecision(invalid); !errors.Is(err, ErrReviewNoteRequired) {
		t.Fatalf("required note error = %v", err)
	}
	invalid = input
	invalid.Note = strings.Repeat("界", MaxHumanNoteLength+1)
	if _, err := NewHumanDecision(invalid); !errors.Is(err, ErrReviewNoteTooLong) {
		t.Fatalf("long note error = %v", err)
	}
}

func TestDecisionValidatesReviewAndCaseVersions(t *testing.T) {
	now := time.Now().UTC()
	reviewCase := RestoreHumanCase(
		1, 2, 3, CaseStatusPendingHuman, 1, 10, 4, 7, strings.Repeat("b", 64),
		timePointer(now.Add(time.Minute)), now, now, nil,
	)
	if err := reviewCase.ValidateDecision(7, strings.Repeat("b", 64), 4, 2, now); !errors.Is(err, ErrReviewSubjectStale) {
		t.Fatalf("review version error = %v", err)
	}
	if err := reviewCase.ValidateDecision(7, strings.Repeat("b", 64), 3, 3, now); !errors.Is(err, ErrReviewCaseVersion) {
		t.Fatalf("case version error = %v", err)
	}
	if err := reviewCase.ValidateDecision(7, strings.Repeat("b", 64), 4, 3, now); err != nil {
		t.Fatalf("valid decision lease error = %v", err)
	}
}

func TestHumanRetirementStatusesAndEventsAreValid(t *testing.T) {
	for _, status := range []string{CaseStatusCancelled, CaseStatusSuperseded} {
		if !ValidCaseStatus(status) {
			t.Fatalf("invalid retirement status %q", status)
		}
	}
	for _, event := range []string{AssignmentEventCancelled, AssignmentEventSuperseded} {
		if !ValidAssignmentEvent(event) {
			t.Fatalf("invalid retirement event %q", event)
		}
	}
}

func timePointer(value time.Time) *time.Time { return &value }
