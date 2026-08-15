package applicationaccount

import (
	"context"
	"errors"
	"testing"
	"time"

	domainaccount "github.com/shiyudesu/frux/internal/domain/account"
	domainadminaudit "github.com/shiyudesu/frux/internal/domain/adminaudit"
)

type managementRepositoryStub struct {
	items        []*domainaccount.ManagedAccount
	detail       *domainaccount.ManagedAccount
	listErr      error
	detailErr    error
	commitErr    error
	lastQuery    domainaccount.ManagedAccountQuery
	lastCommand  domainaccount.AccountManagementCommand
	lastAudit    *domainadminaudit.Fact
	auditInput   domainaccount.AccountManagementAuditInput
	commitResult *domainaccount.AccountManagementResult
}

func (r *managementRepositoryStub) ListManagedAccounts(
	_ context.Context,
	query domainaccount.ManagedAccountQuery,
) ([]*domainaccount.ManagedAccount, error) {
	r.lastQuery = query
	if r.listErr != nil {
		return nil, r.listErr
	}
	items := r.items
	if len(items) > query.Limit {
		items = items[:query.Limit]
	}
	return items, nil
}

func (r *managementRepositoryStub) GetManagedAccount(
	_ context.Context,
	_ int64,
) (*domainaccount.ManagedAccount, error) {
	if r.detailErr != nil {
		return nil, r.detailErr
	}
	return r.detail, nil
}

func (r *managementRepositoryStub) CommitManagedAccountOperation(
	_ context.Context,
	command domainaccount.AccountManagementCommand,
	buildAudit func(domainaccount.AccountManagementAuditInput) (*domainadminaudit.Fact, error),
) (*domainaccount.AccountManagementResult, error) {
	r.lastCommand = command
	if r.commitErr != nil {
		return nil, r.commitErr
	}
	fact, err := buildAudit(r.auditInput)
	if err != nil {
		return nil, err
	}
	r.lastAudit = fact
	return r.commitResult, nil
}

func TestManagedAccountListCursorBindsFilters(t *testing.T) {
	now := time.Now().UTC()
	repository := &managementRepositoryStub{items: []*domainaccount.ManagedAccount{
		{ID: 3, Status: domainaccount.StatusNormal, CreatedAt: now},
		{ID: 2, Status: domainaccount.StatusFrozen, CreatedAt: now.Add(-time.Second)},
	}}
	service := NewManagement(repository, "cursor-secret")
	page, err := service.List(context.Background(), ManagedAccountListRequest{
		Search: "alice", Status: domainaccount.StatusNormal, Limit: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !page.HasMore || page.NextCursor == "" || len(page.Items) != 1 ||
		repository.lastQuery.Limit != 2 {
		t.Fatalf("page=%+v query=%+v", page, repository.lastQuery)
	}
	_, err = service.List(context.Background(), ManagedAccountListRequest{
		Search: "bob", Status: domainaccount.StatusNormal,
		Limit: 1, Cursor: page.NextCursor,
	})
	if !errors.Is(err, domainaccount.ErrInvalidManagedAccountCursor) {
		t.Fatalf("changed filter error = %v", err)
	}
}

func TestManagedAccountListValidationAndErrors(t *testing.T) {
	service := NewManagement(&managementRepositoryStub{}, "secret")
	if _, err := service.List(context.Background(), ManagedAccountListRequest{
		Status: 99,
	}); !errors.Is(err, domainaccount.ErrInvalidManagedAccountStatus) {
		t.Fatalf("status error = %v", err)
	}
	if _, err := service.List(context.Background(), ManagedAccountListRequest{
		Limit: domainaccount.MaxManagedAccountPageSize + 1,
	}); !errors.Is(err, domainaccount.ErrInvalidManagedAccountLimit) {
		t.Fatalf("limit error = %v", err)
	}
	repository := &managementRepositoryStub{listErr: errors.New("database unavailable")}
	if _, err := NewManagement(repository, "secret").List(
		context.Background(), ManagedAccountListRequest{},
	); !errors.Is(err, ErrLoadManagedAccountsFailed) {
		t.Fatalf("repository error = %v", err)
	}
}

func TestManagedAccountMutationBuildsExactAuditFact(t *testing.T) {
	now := time.Date(2026, 8, 14, 8, 0, 0, 0, time.UTC)
	result, err := domainaccount.RestoreAccountManagementResult(
		42, domainaccount.AccountOperationFreeze, domainaccount.StatusFrozen, 3, 2, now,
	)
	if err != nil {
		t.Fatal(err)
	}
	repository := &managementRepositoryStub{
		auditInput: domainaccount.AccountManagementAuditInput{
			PreviousStatus: domainaccount.StatusNormal, NewStatus: domainaccount.StatusFrozen,
			PreviousVersion: 2, NewVersion: 3, RevokedSessionCount: 2,
		},
		commitResult: result,
	}
	service := NewManagement(
		repository, "secret", WithManagementClock(func() time.Time { return now }),
	)
	got, err := service.Freeze(context.Background(), ManageAccountRequest{
		ActorID: 7, UserID: 42, ExpectedVersion: 2,
		ReasonCode: domainaccount.AccountReasonAbuse, IdempotencyKey: "retry-key",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != result || repository.lastCommand.Operation != domainaccount.AccountOperationFreeze {
		t.Fatalf("result=%+v command=%+v", got, repository.lastCommand)
	}
	fact := repository.lastAudit
	if fact == nil || fact.Action() != domainadminaudit.ActionAccountFreeze ||
		fact.Permission() != domainaccount.PermissionAccountManage ||
		fact.TargetType() != domainadminaudit.TargetUserAccount ||
		fact.TargetID() != "42" || fact.IdempotencyKeyHash() == "" {
		t.Fatalf("unexpected fact: %+v", fact)
	}
	detail := fact.Detail()
	if detail["previous_status"] != "normal" || detail["new_status"] != "frozen" ||
		detail["previous_version"] != "2" || detail["new_version"] != "3" ||
		detail["revoked_session_count"] != "2" {
		t.Fatalf("unexpected detail: %#v", detail)
	}
}

func TestManagedAccountMutationPreservesDomainConflicts(t *testing.T) {
	for _, expected := range []error{
		domainaccount.ErrManagedAccountNotFound,
		domainaccount.ErrInvalidAccountManagementTransition,
		domainaccount.ErrAccountManagementVersionConflict,
		domainaccount.ErrAccountManagementIdempotencyConflict,
	} {
		repository := &managementRepositoryStub{commitErr: expected}
		service := NewManagement(repository, "secret")
		_, err := service.RevokeSessions(context.Background(), ManageAccountRequest{
			ActorID: 1, UserID: 2, ExpectedVersion: 3,
			ReasonCode:     domainaccount.AccountReasonSecurityResponse,
			IdempotencyKey: "key",
		})
		if !errors.Is(err, expected) {
			t.Fatalf("error = %v, want %v", err, expected)
		}
	}
}
