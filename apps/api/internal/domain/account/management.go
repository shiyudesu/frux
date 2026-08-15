package domainaccount

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"time"
)

const (
	MaxManagedAccountQueryLength   = 128
	MaxManagedAccountPageSize      = 100
	MaxManagementIdempotencyLength = 128
)

type AccountManagementOperation string

const (
	AccountOperationFreeze         AccountManagementOperation = "freeze"
	AccountOperationUnfreeze       AccountManagementOperation = "unfreeze"
	AccountOperationRevokeSessions AccountManagementOperation = "revoke_sessions"
)

const (
	AccountReasonPolicyViolation  = "policy_violation"
	AccountReasonAbuse            = "abuse"
	AccountReasonSecurityRisk     = "security_risk"
	AccountReasonAppealApproved   = "appeal_approved"
	AccountReasonIssueResolved    = "issue_resolved"
	AccountReasonManualCorrection = "manual_correction"
	AccountReasonSecurityResponse = "security_response"
	AccountReasonUserRequest      = "user_request"
	AccountReasonOperatorRequest  = "operator_request"
)

var accountManagementReasons = map[AccountManagementOperation]map[string]struct{}{
	AccountOperationFreeze: {
		AccountReasonPolicyViolation: {},
		AccountReasonAbuse:           {},
		AccountReasonSecurityRisk:    {},
	},
	AccountOperationUnfreeze: {
		AccountReasonAppealApproved:   {},
		AccountReasonIssueResolved:    {},
		AccountReasonManualCorrection: {},
	},
	AccountOperationRevokeSessions: {
		AccountReasonSecurityResponse: {},
		AccountReasonUserRequest:      {},
		AccountReasonOperatorRequest:  {},
	},
}

type ManagedAccountCursor struct {
	CreatedAt time.Time
	UserID    int64
}

type ManagedAccountQuery struct {
	UserID int64
	Search string
	Status int
	Cursor *ManagedAccountCursor
	Limit  int
}

type ManagedAccount struct {
	ID                 int64
	Account            string
	Nickname           string
	AvatarURL          string
	Bio                string
	Gender             int
	Status             int
	Version            int64
	FollowingCount     int
	FollowerCount      int
	PublicWorkCount    int
	PrivateWorkCount   int
	ReceivedLikeCount  int
	ActiveSessionCount int
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type AccountManagementCommand struct {
	ActorID         int64
	UserID          int64
	ExpectedVersion int64
	Operation       AccountManagementOperation
	ReasonCode      string
	IdempotencyKey  string
	OccurredAt      time.Time
}

type AccountManagementResult struct {
	UserID              int64
	Operation           AccountManagementOperation
	Status              int
	Version             int64
	RevokedSessionCount int64
	OccurredAt          time.Time
	Replayed            bool
}

type AccountManagementAuditInput struct {
	PreviousStatus      int
	NewStatus           int
	PreviousVersion     int64
	NewVersion          int64
	RevokedSessionCount int64
}

func ValidAccountManagementOperation(operation AccountManagementOperation) bool {
	_, ok := accountManagementReasons[operation]
	return ok
}

func ValidAccountManagementReason(operation AccountManagementOperation, reason string) bool {
	reason = strings.TrimSpace(reason)
	reasons, ok := accountManagementReasons[operation]
	if !ok {
		return false
	}
	_, ok = reasons[reason]
	return ok
}

func NormalizeAccountManagementCommand(command AccountManagementCommand) (AccountManagementCommand, error) {
	command.ReasonCode = strings.TrimSpace(command.ReasonCode)
	command.IdempotencyKey = strings.TrimSpace(command.IdempotencyKey)
	command.OccurredAt = command.OccurredAt.UTC()
	if command.ActorID <= 0 || command.UserID <= 0 {
		return AccountManagementCommand{}, ErrInvalidUserID
	}
	if command.ExpectedVersion <= 0 {
		return AccountManagementCommand{}, ErrInvalidAuthVersion
	}
	if !ValidAccountManagementOperation(command.Operation) {
		return AccountManagementCommand{}, ErrInvalidAccountManagementOperation
	}
	if !ValidAccountManagementReason(command.Operation, command.ReasonCode) {
		return AccountManagementCommand{}, ErrInvalidAccountManagementReason
	}
	if command.IdempotencyKey == "" {
		return AccountManagementCommand{}, ErrAccountManagementIdempotencyKeyRequired
	}
	if len(command.IdempotencyKey) > MaxManagementIdempotencyLength {
		return AccountManagementCommand{}, ErrAccountManagementIdempotencyKeyTooLong
	}
	if command.OccurredAt.IsZero() {
		return AccountManagementCommand{}, ErrInvalidAccountManagementResult
	}
	return command, nil
}

func (command AccountManagementCommand) Fingerprint() string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		strconv.FormatInt(command.UserID, 10),
		string(command.Operation),
		strconv.FormatInt(command.ExpectedVersion, 10),
		command.ReasonCode,
	}, "\x00")))
	return hex.EncodeToString(sum[:])
}

func (command AccountManagementCommand) Transition(currentStatus int) (int, bool, error) {
	if !ValidAccountStatus(currentStatus) {
		return 0, false, ErrInvalidAccountManagementTransition
	}
	switch command.Operation {
	case AccountOperationFreeze:
		if currentStatus != StatusNormal {
			return 0, false, ErrInvalidAccountManagementTransition
		}
		return StatusFrozen, true, nil
	case AccountOperationUnfreeze:
		if currentStatus != StatusFrozen {
			return 0, false, ErrInvalidAccountManagementTransition
		}
		return StatusNormal, false, nil
	case AccountOperationRevokeSessions:
		if currentStatus != StatusNormal && currentStatus != StatusFrozen {
			return 0, false, ErrInvalidAccountManagementTransition
		}
		return currentStatus, true, nil
	default:
		return 0, false, ErrInvalidAccountManagementOperation
	}
}

func RestoreManagedAccount(
	id int64,
	account, nickname, avatarURL, bio string,
	gender, status int,
	version int64,
	followingCount, followerCount, publicWorkCount, privateWorkCount,
	receivedLikeCount, activeSessionCount int,
	createdAt, updatedAt time.Time,
) *ManagedAccount {
	return &ManagedAccount{
		ID: id, Account: NormalizeAccount(account), Nickname: strings.TrimSpace(nickname),
		AvatarURL: strings.TrimSpace(avatarURL), Bio: strings.TrimSpace(bio),
		Gender: gender, Status: status, Version: version,
		FollowingCount: clampCount(followingCount), FollowerCount: clampCount(followerCount),
		PublicWorkCount: clampCount(publicWorkCount), PrivateWorkCount: clampCount(privateWorkCount),
		ReceivedLikeCount: clampCount(receivedLikeCount), ActiveSessionCount: clampCount(activeSessionCount),
		CreatedAt: createdAt.UTC(), UpdatedAt: updatedAt.UTC(),
	}
}

func RestoreAccountManagementResult(
	userID int64,
	operation AccountManagementOperation,
	status int,
	version, revokedSessionCount int64,
	occurredAt time.Time,
) (*AccountManagementResult, error) {
	if userID <= 0 || !ValidAccountManagementOperation(operation) ||
		!ValidAccountStatus(status) || version <= 0 || revokedSessionCount < 0 ||
		occurredAt.IsZero() {
		return nil, ErrInvalidAccountManagementResult
	}
	return &AccountManagementResult{
		UserID: userID, Operation: operation, Status: status, Version: version,
		RevokedSessionCount: revokedSessionCount, OccurredAt: occurredAt.UTC(),
	}, nil
}
