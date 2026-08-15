package domainaccount

import (
	"fmt"
	"strings"
	"time"
)

const (
	AccountNotificationPending   = "pending"
	AccountNotificationDelivered = "delivered"
	AccountNotificationTerminal  = "terminal"
)

type AccountLifecycleNotification struct {
	EventID     string
	RecipientID int64
	Operation   AccountManagementOperation
	ReasonCode  string
	AuthVersion int64
	OccurredAt  time.Time
}

type AccountNotificationOutboxItem struct {
	AccountLifecycleNotification
	State       string
	Attempts    int
	AvailableAt time.Time
	LeaseOwner  string
	LeaseUntil  *time.Time
	LastError   string
	DeliveredAt *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func NewAccountLifecycleNotification(
	recipientID int64,
	operation AccountManagementOperation,
	reasonCode string,
	authVersion int64,
	occurredAt time.Time,
) (*AccountLifecycleNotification, error) {
	reasonCode = strings.TrimSpace(reasonCode)
	occurredAt = occurredAt.UTC()
	if recipientID <= 0 || authVersion <= 0 || occurredAt.IsZero() {
		return nil, ErrInvalidAccountNotification
	}
	if operation != AccountOperationFreeze && operation != AccountOperationUnfreeze {
		return nil, ErrInvalidAccountNotification
	}
	if !ValidAccountManagementReason(operation, reasonCode) {
		return nil, ErrInvalidAccountNotification
	}
	return &AccountLifecycleNotification{
		EventID: AccountLifecycleNotificationEventID(
			recipientID, operation, authVersion,
		),
		RecipientID: recipientID,
		Operation:   operation,
		ReasonCode:  reasonCode,
		AuthVersion: authVersion,
		OccurredAt:  occurredAt,
	}, nil
}

func RestoreAccountNotificationOutboxItem(
	notification AccountLifecycleNotification,
	state string,
	attempts int,
	availableAt time.Time,
	leaseOwner string,
	leaseUntil *time.Time,
	lastError string,
	deliveredAt *time.Time,
	createdAt time.Time,
	updatedAt time.Time,
) (*AccountNotificationOutboxItem, error) {
	validated, err := NewAccountLifecycleNotification(
		notification.RecipientID,
		notification.Operation,
		notification.ReasonCode,
		notification.AuthVersion,
		notification.OccurredAt,
	)
	if err != nil || validated.EventID != strings.TrimSpace(notification.EventID) ||
		!ValidAccountNotificationState(state) || attempts < 0 ||
		availableAt.IsZero() || createdAt.IsZero() || updatedAt.IsZero() {
		return nil, ErrInvalidAccountNotification
	}
	return &AccountNotificationOutboxItem{
		AccountLifecycleNotification: *validated,
		State:                        state,
		Attempts:                     attempts,
		AvailableAt:                  availableAt.UTC(),
		LeaseOwner:                   strings.TrimSpace(leaseOwner),
		LeaseUntil:                   leaseUntil,
		LastError:                    strings.TrimSpace(lastError),
		DeliveredAt:                  deliveredAt,
		CreatedAt:                    createdAt.UTC(),
		UpdatedAt:                    updatedAt.UTC(),
	}, nil
}

func AccountLifecycleNotificationEventID(
	userID int64,
	operation AccountManagementOperation,
	authVersion int64,
) string {
	return fmt.Sprintf("account-%s:%d:%d", operation, userID, authVersion)
}

func ValidAccountNotificationState(state string) bool {
	switch strings.TrimSpace(state) {
	case AccountNotificationPending, AccountNotificationDelivered, AccountNotificationTerminal:
		return true
	default:
		return false
	}
}
