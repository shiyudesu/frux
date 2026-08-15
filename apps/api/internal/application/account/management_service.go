package applicationaccount

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	applicationadminaudit "github.com/shiyudesu/frux/internal/application/adminaudit"
	domainaccount "github.com/shiyudesu/frux/internal/domain/account"
	domainadminaudit "github.com/shiyudesu/frux/internal/domain/adminaudit"
)

const (
	managedAccountCursorVersion = 1
	defaultManagedAccountLimit  = 20
)

var ErrLoadManagedAccountsFailed = errors.New("failed to load managed accounts")
var ErrManageAccountFailed = errors.New("failed to manage account")

type AccountManagementRepository interface {
	domainaccount.ManagedAccountReader
	CommitManagedAccountOperation(
		ctx context.Context,
		command domainaccount.AccountManagementCommand,
		buildAudit func(domainaccount.AccountManagementAuditInput) (*domainadminaudit.Fact, error),
	) (*domainaccount.AccountManagementResult, error)
}

type ManagementService struct {
	repository   AccountManagementRepository
	cursorSecret []byte
	now          func() time.Time
}

type ManagementOption func(*ManagementService)

type ManagedAccountListRequest struct {
	UserID int64
	Search string
	Status int
	Cursor string
	Limit  int
}

type ManagedAccountPage struct {
	Items      []*domainaccount.ManagedAccount
	NextCursor string
	HasMore    bool
}

type ManageAccountRequest struct {
	ActorID         int64
	UserID          int64
	ExpectedVersion int64
	ReasonCode      string
	IdempotencyKey  string
}

type managedAccountCursorEnvelope struct {
	Version    int    `json:"v"`
	FilterHash string `json:"f"`
	CreatedAt  string `json:"t"`
	UserID     int64  `json:"id"`
}

func NewManagement(
	repository AccountManagementRepository,
	cursorSecret string,
	options ...ManagementOption,
) *ManagementService {
	service := &ManagementService{
		repository: repository, cursorSecret: []byte(strings.TrimSpace(cursorSecret)),
		now: func() time.Time { return time.Now().UTC() },
	}
	for _, option := range options {
		if option != nil {
			option(service)
		}
	}
	return service
}

func WithManagementClock(now func() time.Time) ManagementOption {
	return func(service *ManagementService) {
		if now != nil {
			service.now = now
		}
	}
}

func (s *ManagementService) List(
	ctx context.Context,
	request ManagedAccountListRequest,
) (*ManagedAccountPage, error) {
	if s == nil || s.repository == nil || len(s.cursorSecret) == 0 {
		return nil, ErrLoadManagedAccountsFailed
	}
	query, filterHash, err := normalizeManagedAccountListRequest(request)
	if err != nil {
		return nil, err
	}
	query.Cursor, err = s.decodeCursor(request.Cursor, filterHash)
	if err != nil {
		return nil, err
	}
	items, err := s.repository.ListManagedAccounts(ctx, query)
	if err != nil {
		if errors.Is(err, domainaccount.ErrInvalidManagedAccountCursor) {
			return nil, err
		}
		return nil, ErrLoadManagedAccountsFailed
	}
	limit := query.Limit - 1
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	nextCursor := ""
	if hasMore && len(items) > 0 {
		last := items[len(items)-1]
		nextCursor = s.encodeCursor(filterHash, &domainaccount.ManagedAccountCursor{
			CreatedAt: last.CreatedAt, UserID: last.ID,
		})
	}
	return &ManagedAccountPage{Items: items, NextCursor: nextCursor, HasMore: hasMore}, nil
}

func (s *ManagementService) Get(
	ctx context.Context,
	userID int64,
) (*domainaccount.ManagedAccount, error) {
	if userID <= 0 {
		return nil, domainaccount.ErrInvalidUserID
	}
	if s == nil || s.repository == nil {
		return nil, ErrLoadManagedAccountsFailed
	}
	account, err := s.repository.GetManagedAccount(ctx, userID)
	if err != nil {
		if errors.Is(err, domainaccount.ErrManagedAccountNotFound) {
			return nil, err
		}
		return nil, ErrLoadManagedAccountsFailed
	}
	return account, nil
}

func (s *ManagementService) Freeze(
	ctx context.Context,
	request ManageAccountRequest,
) (*domainaccount.AccountManagementResult, error) {
	return s.manage(ctx, request, domainaccount.AccountOperationFreeze)
}

func (s *ManagementService) Unfreeze(
	ctx context.Context,
	request ManageAccountRequest,
) (*domainaccount.AccountManagementResult, error) {
	return s.manage(ctx, request, domainaccount.AccountOperationUnfreeze)
}

func (s *ManagementService) RevokeSessions(
	ctx context.Context,
	request ManageAccountRequest,
) (*domainaccount.AccountManagementResult, error) {
	return s.manage(ctx, request, domainaccount.AccountOperationRevokeSessions)
}

func (s *ManagementService) manage(
	ctx context.Context,
	request ManageAccountRequest,
	operation domainaccount.AccountManagementOperation,
) (*domainaccount.AccountManagementResult, error) {
	if s == nil || s.repository == nil || s.now == nil {
		return nil, ErrManageAccountFailed
	}
	command, err := domainaccount.NormalizeAccountManagementCommand(
		domainaccount.AccountManagementCommand{
			ActorID: request.ActorID, UserID: request.UserID,
			ExpectedVersion: request.ExpectedVersion, Operation: operation,
			ReasonCode: request.ReasonCode, IdempotencyKey: request.IdempotencyKey,
			OccurredAt: s.now(),
		},
	)
	if err != nil {
		return nil, err
	}
	action, route := accountAuditAction(operation)
	result, err := s.repository.CommitManagedAccountOperation(
		ctx,
		command,
		func(input domainaccount.AccountManagementAuditInput) (*domainadminaudit.Fact, error) {
			return applicationadminaudit.BuildSuccessFact(applicationadminaudit.BuildInput{
				ActorID: command.ActorID, Permission: domainaccount.PermissionAccountManage,
				Action: action, TargetType: domainadminaudit.TargetUserAccount,
				TargetID:       strconv.FormatInt(command.UserID, 10),
				RequestID:      domainadminaudit.NewRequestID(),
				IdempotencyKey: command.IdempotencyKey,
				Detail: map[string]string{
					"http_method": "POST", "route": route,
					"reason_code":           command.ReasonCode,
					"previous_status":       accountStatusName(input.PreviousStatus),
					"new_status":            accountStatusName(input.NewStatus),
					"previous_version":      strconv.FormatInt(input.PreviousVersion, 10),
					"new_version":           strconv.FormatInt(input.NewVersion, 10),
					"revoked_session_count": strconv.FormatInt(input.RevokedSessionCount, 10),
				},
			}, command.OccurredAt)
		},
	)
	if err != nil {
		switch {
		case errors.Is(err, domainaccount.ErrManagedAccountNotFound),
			errors.Is(err, domainaccount.ErrInvalidAccountManagementTransition),
			errors.Is(err, domainaccount.ErrAccountManagementVersionConflict),
			errors.Is(err, domainaccount.ErrAccountManagementIdempotencyConflict):
			return nil, err
		default:
			return nil, ErrManageAccountFailed
		}
	}
	return result, nil
}

func normalizeManagedAccountListRequest(
	request ManagedAccountListRequest,
) (domainaccount.ManagedAccountQuery, string, error) {
	if request.UserID < 0 {
		return domainaccount.ManagedAccountQuery{}, "", domainaccount.ErrInvalidManagedAccountQuery
	}
	search := strings.TrimSpace(request.Search)
	if len([]rune(search)) > domainaccount.MaxManagedAccountQueryLength {
		return domainaccount.ManagedAccountQuery{}, "", domainaccount.ErrManagedAccountQueryTooLong
	}
	if request.Status != 0 && !domainaccount.ValidAccountStatus(request.Status) {
		return domainaccount.ManagedAccountQuery{}, "", domainaccount.ErrInvalidManagedAccountStatus
	}
	limit := request.Limit
	if limit == 0 {
		limit = defaultManagedAccountLimit
	}
	if limit < 1 || limit > domainaccount.MaxManagedAccountPageSize {
		return domainaccount.ManagedAccountQuery{}, "", domainaccount.ErrInvalidManagedAccountLimit
	}
	query := domainaccount.ManagedAccountQuery{
		UserID: request.UserID, Search: search, Status: request.Status, Limit: limit + 1,
	}
	payload, _ := json.Marshal(struct {
		UserID int64  `json:"user_id"`
		Search string `json:"search"`
		Status int    `json:"status"`
	}{UserID: query.UserID, Search: query.Search, Status: query.Status})
	sum := sha256.Sum256(payload)
	return query, base64.RawURLEncoding.EncodeToString(sum[:]), nil
}

func (s *ManagementService) encodeCursor(
	filterHash string,
	cursor *domainaccount.ManagedAccountCursor,
) string {
	payload, err := json.Marshal(managedAccountCursorEnvelope{
		Version: managedAccountCursorVersion, FilterHash: filterHash,
		CreatedAt: cursor.CreatedAt.UTC().Format(time.RFC3339Nano), UserID: cursor.UserID,
	})
	if err != nil {
		return ""
	}
	mac := hmac.New(sha256.New, s.cursorSecret)
	_, _ = mac.Write(payload)
	return base64.RawURLEncoding.EncodeToString(payload) + "." +
		base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (s *ManagementService) decodeCursor(
	raw, filterHash string,
) (*domainaccount.ManagedAccountCursor, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ".")
	if len(parts) != 2 {
		return nil, domainaccount.ErrInvalidManagedAccountCursor
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, domainaccount.ErrInvalidManagedAccountCursor
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, domainaccount.ErrInvalidManagedAccountCursor
	}
	mac := hmac.New(sha256.New, s.cursorSecret)
	_, _ = mac.Write(payload)
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return nil, domainaccount.ErrInvalidManagedAccountCursor
	}
	var envelope managedAccountCursorEnvelope
	if err := json.Unmarshal(payload, &envelope); err != nil ||
		envelope.Version != managedAccountCursorVersion ||
		envelope.FilterHash != filterHash || envelope.UserID <= 0 {
		return nil, domainaccount.ErrInvalidManagedAccountCursor
	}
	createdAt, err := time.Parse(time.RFC3339Nano, envelope.CreatedAt)
	if err != nil || createdAt.IsZero() {
		return nil, domainaccount.ErrInvalidManagedAccountCursor
	}
	return &domainaccount.ManagedAccountCursor{
		CreatedAt: createdAt.UTC(), UserID: envelope.UserID,
	}, nil
}

func accountAuditAction(
	operation domainaccount.AccountManagementOperation,
) (domainadminaudit.Action, string) {
	switch operation {
	case domainaccount.AccountOperationFreeze:
		return domainadminaudit.ActionAccountFreeze, "/api/admin/accounts/:userId/freeze"
	case domainaccount.AccountOperationUnfreeze:
		return domainadminaudit.ActionAccountUnfreeze, "/api/admin/accounts/:userId/unfreeze"
	default:
		return domainadminaudit.ActionAccountSessionsRevoke, "/api/admin/accounts/:userId/sessions/revoke"
	}
}

func accountStatusName(status int) string {
	switch status {
	case domainaccount.StatusNormal:
		return "normal"
	case domainaccount.StatusFrozen:
		return "frozen"
	default:
		return "cancelled"
	}
}
