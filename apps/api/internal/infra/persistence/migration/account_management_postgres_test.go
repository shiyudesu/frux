package migration

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	applicationadminaudit "github.com/shiyudesu/frux/internal/application/adminaudit"
	domainaccount "github.com/shiyudesu/frux/internal/domain/account"
	domainadminaudit "github.com/shiyudesu/frux/internal/domain/adminaudit"
	infraaccount "github.com/shiyudesu/frux/internal/infra/persistence/account"
	infraadminaudit "github.com/shiyudesu/frux/internal/infra/persistence/adminaudit"

	"gorm.io/gorm"
)

type failingAccountAuditWriter struct{}

func (failingAccountAuditWriter) AppendInTransaction(
	context.Context,
	*gorm.DB,
	*domainadminaudit.Fact,
) error {
	return errors.New("audit unavailable")
}

func (failingAccountAuditWriter) RecordCommittedWrite(*domainadminaudit.Fact) {}

func TestManagedAccountPersistenceAndTransactions(t *testing.T) {
	fixture := newPostgresFixture(t)
	db := fixture.openGORM(t)
	if err := AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	auditRepo := infraadminaudit.New(db)
	repo := infraaccount.New(db, infraaccount.WithAdminAuditWriter(auditRepo))
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)

	userOne := createManagedTestUser(t, db, repo, "managed-one", now.Add(-3*time.Minute))
	userTwo := createManagedTestUser(t, db, repo, "managed-two", now.Add(-2*time.Minute))
	userThree := createManagedTestUser(t, db, repo, "managed-three", now.Add(-time.Minute))
	privileged := createManagedTestUser(t, db, repo, "managed-admin", now)
	if err := db.Model(&infraaccount.UserModel{}).Where("id = ?", privileged.ID).
		Update("role", domainaccount.RoleAdmin).Error; err != nil {
		t.Fatal(err)
	}
	createManagedTestSession(t, db, userOne.ID, userOne.AuthVersion, "managed-one-a", now)
	createManagedTestSession(t, db, userOne.ID, userOne.AuthVersion, "managed-one-b", now)

	items, err := repo.ListManagedAccounts(ctx, domainaccount.ManagedAccountQuery{
		Search: "managed", Limit: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].ID != userThree.ID || items[1].ID != userTwo.ID {
		t.Fatalf("unexpected first page: %+v", items)
	}
	next, err := repo.ListManagedAccounts(ctx, domainaccount.ManagedAccountQuery{
		Search: "managed", Limit: 2,
		Cursor: &domainaccount.ManagedAccountCursor{
			CreatedAt: items[1].CreatedAt, UserID: items[1].ID,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(next) != 1 || next[0].ID != userOne.ID {
		t.Fatalf("unexpected next page: %+v", next)
	}
	if _, err := repo.GetManagedAccount(ctx, privileged.ID); !errors.Is(err, domainaccount.ErrManagedAccountNotFound) {
		t.Fatalf("privileged detail error = %v", err)
	}
	detail, err := repo.GetManagedAccount(ctx, userOne.ID)
	if err != nil || detail.ActiveSessionCount != 2 {
		t.Fatalf("detail=%+v err=%v", detail, err)
	}

	freeze := managedTestCommand(
		7, userOne.ID, 1, domainaccount.AccountOperationFreeze,
		domainaccount.AccountReasonAbuse, "freeze-key", now,
	)
	frozen, err := repo.CommitManagedAccountOperation(ctx, freeze, managedTestAuditBuilder(freeze))
	if err != nil {
		t.Fatal(err)
	}
	if frozen.Status != domainaccount.StatusFrozen || frozen.Version != 2 ||
		frozen.RevokedSessionCount != 2 || frozen.Replayed {
		t.Fatalf("freeze result: %+v", frozen)
	}
	assertAccountNotificationCount(t, db, userOne.ID, 1)
	var freezeNotification infraaccount.NotificationOutboxModel
	if err := db.Where(
		"event_id = ?",
		domainaccount.AccountLifecycleNotificationEventID(
			userOne.ID, domainaccount.AccountOperationFreeze, 2,
		),
	).Take(&freezeNotification).Error; err != nil {
		t.Fatal(err)
	}
	if freezeNotification.ReasonCode != domainaccount.AccountReasonAbuse ||
		freezeNotification.State != domainaccount.AccountNotificationPending {
		t.Fatalf("freeze notification = %+v", freezeNotification)
	}
	if _, err := repo.RotateRefreshSession(ctx, domainaccount.RotateRefreshSessionInput{
		SessionID: "managed-one-a", SecretHash: strings.Repeat("a", 64),
		NewSecretHash: strings.Repeat("b", 64), RotatedAt: now.Add(500 * time.Millisecond),
		PreviousGrace: 10 * time.Second,
	}); !errors.Is(err, domainaccount.ErrRefreshSessionRevoked) {
		t.Fatalf("frozen refresh error = %v", err)
	}
	replayed, err := repo.CommitManagedAccountOperation(ctx, freeze, managedTestAuditBuilder(freeze))
	if err != nil || !replayed.Replayed {
		t.Fatalf("replay=%+v err=%v", replayed, err)
	}
	assertAccountNotificationCount(t, db, userOne.ID, 1)
	conflicting := freeze
	conflicting.ReasonCode = domainaccount.AccountReasonSecurityRisk
	if _, err := repo.CommitManagedAccountOperation(
		ctx, conflicting, managedTestAuditBuilder(conflicting),
	); !errors.Is(err, domainaccount.ErrAccountManagementIdempotencyConflict) {
		t.Fatalf("idempotency conflict error = %v", err)
	}

	unfreeze := managedTestCommand(
		7, userOne.ID, 2, domainaccount.AccountOperationUnfreeze,
		domainaccount.AccountReasonAppealApproved, "unfreeze-key", now.Add(time.Second),
	)
	unfrozen, err := repo.CommitManagedAccountOperation(ctx, unfreeze, managedTestAuditBuilder(unfreeze))
	if err != nil || unfrozen.Status != domainaccount.StatusNormal ||
		unfrozen.Version != 3 || unfrozen.RevokedSessionCount != 0 {
		t.Fatalf("unfreeze=%+v err=%v", unfrozen, err)
	}
	assertAccountNotificationCount(t, db, userOne.ID, 2)
	if _, err := repo.RotateRefreshSession(ctx, domainaccount.RotateRefreshSessionInput{
		SessionID: "managed-one-a", SecretHash: strings.Repeat("a", 64),
		NewSecretHash: strings.Repeat("c", 64), RotatedAt: now.Add(1500 * time.Millisecond),
		PreviousGrace: 10 * time.Second,
	}); !errors.Is(err, domainaccount.ErrRefreshSessionRevoked) {
		t.Fatalf("unfrozen old refresh error = %v", err)
	}
	createManagedTestSession(t, db, userOne.ID, 3, "managed-one-c", now.Add(2*time.Second))
	signOut := managedTestCommand(
		7, userOne.ID, 3, domainaccount.AccountOperationRevokeSessions,
		domainaccount.AccountReasonSecurityResponse, "signout-key", now.Add(3*time.Second),
	)
	signedOut, err := repo.CommitManagedAccountOperation(ctx, signOut, managedTestAuditBuilder(signOut))
	if err != nil || signedOut.Status != domainaccount.StatusNormal ||
		signedOut.Version != 4 || signedOut.RevokedSessionCount != 1 {
		t.Fatalf("signout=%+v err=%v", signedOut, err)
	}
	assertAccountNotificationCount(t, db, userOne.ID, 2)
	if _, err := repo.RotateRefreshSession(ctx, domainaccount.RotateRefreshSessionInput{
		SessionID: "managed-one-c", SecretHash: strings.Repeat("a", 64),
		NewSecretHash: strings.Repeat("d", 64), RotatedAt: now.Add(4 * time.Second),
		PreviousGrace: 10 * time.Second,
	}); !errors.Is(err, domainaccount.ErrRefreshSessionRevoked) {
		t.Fatalf("forced-out refresh error = %v", err)
	}

	if err := db.Model(&infraaccount.UserModel{}).Where("id = ?", userTwo.ID).
		Update("role", domainaccount.RoleOperator).Error; err != nil {
		t.Fatal(err)
	}
	roleChanged := managedTestCommand(
		7, userTwo.ID, 1, domainaccount.AccountOperationFreeze,
		domainaccount.AccountReasonAbuse, "role-key", now,
	)
	if _, err := repo.CommitManagedAccountOperation(
		ctx, roleChanged, managedTestAuditBuilder(roleChanged),
	); !errors.Is(err, domainaccount.ErrManagedAccountNotFound) {
		t.Fatalf("role-changed error = %v", err)
	}
	if err := db.Model(&infraaccount.UserModel{}).Where("id = ?", userThree.ID).
		Update("status", domainaccount.StatusCancelled).Error; err != nil {
		t.Fatal(err)
	}
	cancelled := managedTestCommand(
		7, userThree.ID, 1, domainaccount.AccountOperationFreeze,
		domainaccount.AccountReasonAbuse, "cancelled-key", now,
	)
	if _, err := repo.CommitManagedAccountOperation(
		ctx, cancelled, managedTestAuditBuilder(cancelled),
	); !errors.Is(err, domainaccount.ErrInvalidAccountManagementTransition) {
		t.Fatalf("cancelled error = %v", err)
	}

	var auditCount int64
	if err := db.Model(&infraadminaudit.EventModel{}).
		Where("target_type = ? AND target_id = ?", domainadminaudit.TargetUserAccount, strconv.FormatInt(userOne.ID, 10)).
		Count(&auditCount).Error; err != nil {
		t.Fatal(err)
	}
	if auditCount != 3 {
		t.Fatalf("audit count = %d", auditCount)
	}
}

func TestManagedAccountConcurrentReplayAndAuditRollback(t *testing.T) {
	fixture := newPostgresFixture(t)
	db := fixture.openGORM(t)
	if err := AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	auditRepo := infraadminaudit.New(db)
	repo := infraaccount.New(db, infraaccount.WithAdminAuditWriter(auditRepo))
	target := createManagedTestUser(t, db, repo, "managed-race", now)
	createManagedTestSession(t, db, target.ID, 1, "race-session", now)
	command := managedTestCommand(
		10, target.ID, 1, domainaccount.AccountOperationFreeze,
		domainaccount.AccountReasonSecurityRisk, "race-key", now.Add(time.Second),
	)

	results := make([]*domainaccount.AccountManagementResult, 2)
	errs := make([]error, 2)
	var wait sync.WaitGroup
	wait.Add(2)
	for index := range results {
		go func(index int) {
			defer wait.Done()
			results[index], errs[index] = repo.CommitManagedAccountOperation(
				ctx, command, managedTestAuditBuilder(command),
			)
		}(index)
	}
	wait.Wait()
	if errs[0] != nil || errs[1] != nil {
		t.Fatalf("concurrent errors: %v %v", errs[0], errs[1])
	}
	if results[0].Replayed == results[1].Replayed {
		t.Fatalf("expected one original and one replay: %+v %+v", results[0], results[1])
	}
	var auditCount int64
	if err := db.Model(&infraadminaudit.EventModel{}).
		Where("target_type = ? AND target_id = ?", domainadminaudit.TargetUserAccount, strconv.FormatInt(target.ID, 10)).
		Count(&auditCount).Error; err != nil {
		t.Fatal(err)
	}
	if auditCount != 1 {
		t.Fatalf("concurrent audit count = %d", auditCount)
	}
	assertAccountNotificationCount(t, db, target.ID, 1)

	rollbackTarget := createManagedTestUser(t, db, repo, "managed-rollback", now)
	createManagedTestSession(t, db, rollbackTarget.ID, 1, "rollback-session", now)
	failingRepo := infraaccount.New(db, infraaccount.WithAdminAuditWriter(failingAccountAuditWriter{}))
	rollbackCommand := managedTestCommand(
		10, rollbackTarget.ID, 1, domainaccount.AccountOperationFreeze,
		domainaccount.AccountReasonAbuse, "rollback-key", now.Add(2*time.Second),
	)
	if _, err := failingRepo.CommitManagedAccountOperation(
		ctx, rollbackCommand, managedTestAuditBuilder(rollbackCommand),
	); err == nil {
		t.Fatal("expected audit failure")
	}
	var account infraaccount.UserModel
	if err := db.Where("id = ?", rollbackTarget.ID).Take(&account).Error; err != nil {
		t.Fatal(err)
	}
	if account.Status != domainaccount.StatusNormal || account.AuthVersion != 1 {
		t.Fatalf("rollback account changed: %+v", account)
	}
	var session infraaccount.RefreshSessionModel
	if err := db.Where("id = ?", "rollback-session").Take(&session).Error; err != nil {
		t.Fatal(err)
	}
	if session.RevokedAt != nil {
		t.Fatalf("rollback session revoked: %+v", session)
	}
	var operationCount int64
	if err := db.Model(&infraaccount.ManagementOperationModel{}).
		Where("actor_id = ? AND idempotency_key = ?", 10, "rollback-key").
		Count(&operationCount).Error; err != nil {
		t.Fatal(err)
	}
	if operationCount != 0 {
		t.Fatalf("rollback operation count = %d", operationCount)
	}
	assertAccountNotificationCount(t, db, rollbackTarget.ID, 0)
}

func TestAccountNotificationOutboxLeaseLifecycle(t *testing.T) {
	fixture := newPostgresFixture(t)
	db := fixture.openGORM(t)
	if err := AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	auditRepo := infraadminaudit.New(db)
	repo := infraaccount.New(db, infraaccount.WithAdminAuditWriter(auditRepo))
	first := createManagedTestUser(t, db, repo, "notify-first", now)
	second := createManagedTestUser(t, db, repo, "notify-second", now.Add(time.Second))
	for index, user := range []*domainaccount.User{first, second} {
		command := managedTestCommand(
			12, user.ID, 1, domainaccount.AccountOperationFreeze,
			domainaccount.AccountReasonAbuse,
			"notify-"+strconv.Itoa(index),
			now.Add(time.Duration(index)*time.Second),
		)
		if _, err := repo.CommitManagedAccountOperation(
			ctx, command, managedTestAuditBuilder(command),
		); err != nil {
			t.Fatal(err)
		}
	}

	items, err := repo.ClaimAccountNotifications(
		ctx, "worker-one", 1, now.Add(2*time.Second), now.Add(32*time.Second),
	)
	if err != nil || len(items) != 1 || items[0].RecipientID != first.ID ||
		items[0].Attempts != 1 {
		t.Fatalf("first claim=%+v err=%v", items, err)
	}
	if err := repo.MarkAccountNotificationDelivered(
		ctx, items[0].EventID, "wrong-owner", now.Add(3*time.Second),
	); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("wrong owner delivered error = %v", err)
	}
	if err := repo.MarkAccountNotificationFailed(
		ctx, items[0].EventID, "worker-one", now.Add(4*time.Second),
		"temporary", false,
	); err != nil {
		t.Fatal(err)
	}
	if claimed, err := repo.ClaimAccountNotifications(
		ctx, "worker-two", 1, now.Add(3*time.Second), now.Add(33*time.Second),
	); err != nil || len(claimed) != 1 || claimed[0].RecipientID != second.ID {
		t.Fatalf("second claim=%+v err=%v", claimed, err)
	}
	retry, err := repo.ClaimAccountNotifications(
		ctx, "worker-three", 1, now.Add(5*time.Second), now.Add(35*time.Second),
	)
	if err != nil || len(retry) != 1 || retry[0].RecipientID != first.ID ||
		retry[0].Attempts != 2 {
		t.Fatalf("retry claim=%+v err=%v", retry, err)
	}
	if err := repo.MarkAccountNotificationFailed(
		ctx, retry[0].EventID, "worker-three", now.Add(6*time.Second),
		"invalid", true,
	); err != nil {
		t.Fatal(err)
	}
	var terminal infraaccount.NotificationOutboxModel
	if err := db.Where("event_id = ?", retry[0].EventID).Take(&terminal).Error; err != nil {
		t.Fatal(err)
	}
	if terminal.State != domainaccount.AccountNotificationTerminal ||
		terminal.LeaseOwner != "" || terminal.LeaseUntil != nil {
		t.Fatalf("terminal notification = %+v", terminal)
	}
}

func createManagedTestUser(
	t *testing.T,
	db *gorm.DB,
	repo *infraaccount.Repository,
	account string,
	createdAt time.Time,
) *domainaccount.User {
	t.Helper()
	user, err := domainaccount.New(account, "Password123!", account)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&infraaccount.UserModel{}).Where("id = ?", user.ID).
		Updates(map[string]any{"created_at": createdAt, "updated_at": createdAt}).Error; err != nil {
		t.Fatal(err)
	}
	return user
}

func createManagedTestSession(
	t *testing.T,
	db *gorm.DB,
	userID, authVersion int64,
	id string,
	now time.Time,
) {
	t.Helper()
	model := infraaccount.RefreshSessionModel{
		ID: id, FamilyID: id + "-family", UserID: userID,
		SecretHash: strings.Repeat("a", 64), AuthVersion: authVersion,
		ExpiresAt: now.Add(time.Hour), LastUsedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&model).Error; err != nil {
		t.Fatal(err)
	}
}

func managedTestCommand(
	actorID, userID, version int64,
	operation domainaccount.AccountManagementOperation,
	reason, key string,
	at time.Time,
) domainaccount.AccountManagementCommand {
	return domainaccount.AccountManagementCommand{
		ActorID: actorID, UserID: userID, ExpectedVersion: version,
		Operation: operation, ReasonCode: reason, IdempotencyKey: key, OccurredAt: at,
	}
}

func managedTestAuditBuilder(
	command domainaccount.AccountManagementCommand,
) func(domainaccount.AccountManagementAuditInput) (*domainadminaudit.Fact, error) {
	action := domainadminaudit.ActionAccountFreeze
	route := "/api/admin/accounts/:userId/freeze"
	if command.Operation == domainaccount.AccountOperationUnfreeze {
		action = domainadminaudit.ActionAccountUnfreeze
		route = "/api/admin/accounts/:userId/unfreeze"
	} else if command.Operation == domainaccount.AccountOperationRevokeSessions {
		action = domainadminaudit.ActionAccountSessionsRevoke
		route = "/api/admin/accounts/:userId/sessions/revoke"
	}
	return func(input domainaccount.AccountManagementAuditInput) (*domainadminaudit.Fact, error) {
		return applicationadminaudit.BuildSuccessFact(applicationadminaudit.BuildInput{
			ActorID: command.ActorID, Permission: domainaccount.PermissionAccountManage,
			Action: action, TargetType: domainadminaudit.TargetUserAccount,
			TargetID:  strconv.FormatInt(command.UserID, 10),
			RequestID: domainadminaudit.NewRequestID(), IdempotencyKey: command.IdempotencyKey,
			Detail: map[string]string{
				"http_method": "POST", "route": route, "reason_code": command.ReasonCode,
				"previous_status":       managedTestStatus(input.PreviousStatus),
				"new_status":            managedTestStatus(input.NewStatus),
				"previous_version":      strconv.FormatInt(input.PreviousVersion, 10),
				"new_version":           strconv.FormatInt(input.NewVersion, 10),
				"revoked_session_count": strconv.FormatInt(input.RevokedSessionCount, 10),
			},
		}, command.OccurredAt)
	}
}

func managedTestStatus(status int) string {
	if status == domainaccount.StatusNormal {
		return "normal"
	}
	if status == domainaccount.StatusFrozen {
		return "frozen"
	}
	return "cancelled"
}

func assertAccountNotificationCount(
	t *testing.T,
	db *gorm.DB,
	userID int64,
	want int64,
) {
	t.Helper()
	var count int64
	if err := db.Model(&infraaccount.NotificationOutboxModel{}).
		Where("recipient_id = ?", userID).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != want {
		t.Fatalf("account notification count = %d, want %d", count, want)
	}
}
