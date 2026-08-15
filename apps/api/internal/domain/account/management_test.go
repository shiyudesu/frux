package domainaccount

import (
	"errors"
	"testing"
	"time"
)

func TestAccountStatusValidation(t *testing.T) {
	for _, status := range []int{StatusNormal, StatusFrozen, StatusCancelled} {
		if !ValidAccountStatus(status) {
			t.Fatalf("status %d should be valid", status)
		}
	}
	if ValidAccountStatus(0) || ValidAccountStatus(4) {
		t.Fatal("unknown account status accepted")
	}
}

func TestNormalizeAccountManagementCommandAndFingerprint(t *testing.T) {
	now := time.Date(2026, 8, 14, 8, 0, 0, 0, time.FixedZone("offset", 8*60*60))
	command, err := NormalizeAccountManagementCommand(AccountManagementCommand{
		ActorID: 7, UserID: 42, ExpectedVersion: 3,
		Operation: AccountOperationFreeze, ReasonCode: " abuse ",
		IdempotencyKey: " retry-1 ", OccurredAt: now,
	})
	if err != nil {
		t.Fatalf("normalize command: %v", err)
	}
	if command.ReasonCode != AccountReasonAbuse || command.IdempotencyKey != "retry-1" ||
		command.OccurredAt.Location() != time.UTC {
		t.Fatalf("unexpected normalized command: %+v", command)
	}
	if command.Fingerprint() == "" || command.Fingerprint() != command.Fingerprint() {
		t.Fatal("fingerprint must be stable")
	}
	changed := command
	changed.ExpectedVersion++
	if changed.Fingerprint() == command.Fingerprint() {
		t.Fatal("fingerprint must bind expected version")
	}
}

func TestAccountManagementCommandValidation(t *testing.T) {
	base := AccountManagementCommand{
		ActorID: 1, UserID: 2, ExpectedVersion: 1,
		Operation: AccountOperationFreeze, ReasonCode: AccountReasonSecurityRisk,
		IdempotencyKey: "key", OccurredAt: time.Now().UTC(),
	}
	tests := []struct {
		name   string
		change func(*AccountManagementCommand)
		want   error
	}{
		{name: "actor", change: func(command *AccountManagementCommand) { command.ActorID = 0 }, want: ErrInvalidUserID},
		{name: "user", change: func(command *AccountManagementCommand) { command.UserID = 0 }, want: ErrInvalidUserID},
		{name: "version", change: func(command *AccountManagementCommand) { command.ExpectedVersion = 0 }, want: ErrInvalidAuthVersion},
		{name: "operation", change: func(command *AccountManagementCommand) { command.Operation = "delete" }, want: ErrInvalidAccountManagementOperation},
		{name: "reason", change: func(command *AccountManagementCommand) { command.ReasonCode = AccountReasonAppealApproved }, want: ErrInvalidAccountManagementReason},
		{name: "key missing", change: func(command *AccountManagementCommand) { command.IdempotencyKey = "" }, want: ErrAccountManagementIdempotencyKeyRequired},
		{name: "key long", change: func(command *AccountManagementCommand) {
			command.IdempotencyKey = string(make([]byte, MaxManagementIdempotencyLength+1))
		}, want: ErrAccountManagementIdempotencyKeyTooLong},
		{name: "time", change: func(command *AccountManagementCommand) { command.OccurredAt = time.Time{} }, want: ErrInvalidAccountManagementResult},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			command := base
			tt.change(&command)
			if _, err := NormalizeAccountManagementCommand(command); !errors.Is(err, tt.want) {
				t.Fatalf("error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestAccountManagementTransitions(t *testing.T) {
	tests := []struct {
		operation AccountManagementOperation
		status    int
		want      int
		revoke    bool
		err       error
	}{
		{AccountOperationFreeze, StatusNormal, StatusFrozen, true, nil},
		{AccountOperationUnfreeze, StatusFrozen, StatusNormal, false, nil},
		{AccountOperationRevokeSessions, StatusNormal, StatusNormal, true, nil},
		{AccountOperationRevokeSessions, StatusFrozen, StatusFrozen, true, nil},
		{AccountOperationFreeze, StatusFrozen, 0, false, ErrInvalidAccountManagementTransition},
		{AccountOperationUnfreeze, StatusNormal, 0, false, ErrInvalidAccountManagementTransition},
		{AccountOperationRevokeSessions, StatusCancelled, 0, false, ErrInvalidAccountManagementTransition},
	}
	for _, tt := range tests {
		command := AccountManagementCommand{Operation: tt.operation}
		status, revoke, err := command.Transition(tt.status)
		if !errors.Is(err, tt.err) || status != tt.want || revoke != tt.revoke {
			t.Fatalf("%s from %d = (%d,%v,%v), want (%d,%v,%v)",
				tt.operation, tt.status, status, revoke, err, tt.want, tt.revoke, tt.err)
		}
	}
}

func TestRestoreManagedAccountAndResult(t *testing.T) {
	now := time.Now().UTC()
	account := RestoreManagedAccount(
		9, " ALICE ", " Alice ", " avatar ", " bio ", GenderOther, StatusFrozen, 4,
		-1, 2, 3, 4, 5, 6, now, now,
	)
	if account.Account != "alice" || account.Nickname != "Alice" ||
		account.FollowingCount != 0 || account.ActiveSessionCount != 6 {
		t.Fatalf("unexpected managed account: %+v", account)
	}
	result, err := RestoreAccountManagementResult(
		9, AccountOperationFreeze, StatusFrozen, 5, 2, now,
	)
	if err != nil {
		t.Fatalf("restore result: %v", err)
	}
	if result.UserID != 9 || result.Version != 5 || result.RevokedSessionCount != 2 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if _, err := RestoreAccountManagementResult(
		9, AccountOperationFreeze, StatusFrozen, 0, 0, now,
	); !errors.Is(err, ErrInvalidAccountManagementResult) {
		t.Fatalf("invalid result error = %v", err)
	}
}
