package domainaccount

import (
	"errors"
	"testing"
	"time"
)

func TestAccountLifecycleNotificationIsClosedAndStable(t *testing.T) {
	now := time.Date(2026, 8, 15, 3, 0, 0, 0, time.UTC)
	notification, err := NewAccountLifecycleNotification(
		42, AccountOperationFreeze, AccountReasonAbuse, 7, now,
	)
	if err != nil {
		t.Fatal(err)
	}
	if notification.EventID != "account-freeze:42:7" ||
		notification.RecipientID != 42 ||
		notification.ReasonCode != AccountReasonAbuse {
		t.Fatalf("notification = %+v", notification)
	}
	tests := []struct {
		name      string
		userID    int64
		operation AccountManagementOperation
		reason    string
		version   int64
		at        time.Time
	}{
		{name: "recipient", operation: AccountOperationFreeze, reason: AccountReasonAbuse, version: 1, at: now},
		{name: "operation", userID: 1, operation: AccountOperationRevokeSessions, reason: AccountReasonSecurityResponse, version: 1, at: now},
		{name: "reason", userID: 1, operation: AccountOperationFreeze, reason: AccountReasonAppealApproved, version: 1, at: now},
		{name: "version", userID: 1, operation: AccountOperationFreeze, reason: AccountReasonAbuse, at: now},
		{name: "time", userID: 1, operation: AccountOperationFreeze, reason: AccountReasonAbuse, version: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NewAccountLifecycleNotification(
				tt.userID, tt.operation, tt.reason, tt.version, tt.at,
			); !errors.Is(err, ErrInvalidAccountNotification) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestRestoreAccountNotificationOutboxItemValidatesIdentity(t *testing.T) {
	now := time.Now().UTC()
	notification, err := NewAccountLifecycleNotification(
		9, AccountOperationUnfreeze, AccountReasonAppealApproved, 3, now,
	)
	if err != nil {
		t.Fatal(err)
	}
	item, err := RestoreAccountNotificationOutboxItem(
		*notification, AccountNotificationPending, 1, now, "worker", nil,
		"", nil, now, now,
	)
	if err != nil || item.EventID != notification.EventID {
		t.Fatalf("item=%+v err=%v", item, err)
	}
	changed := *notification
	changed.EventID = "forged"
	if _, err := RestoreAccountNotificationOutboxItem(
		changed, AccountNotificationPending, 0, now, "", nil, "", nil, now, now,
	); !errors.Is(err, ErrInvalidAccountNotification) {
		t.Fatalf("forged event error = %v", err)
	}
}
