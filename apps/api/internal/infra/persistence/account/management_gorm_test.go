package infraaccount

import (
	"fmt"
	"strings"
	"testing"
	"time"

	domainaccount "github.com/shiyudesu/frux/internal/domain/account"

	"gorm.io/gorm"
)

func TestBuildManagedAccountListQueryIsPrivateStableAndRoleBound(t *testing.T) {
	db := newUserSearchDryRunDB(t)
	cursor := &domainaccount.ManagedAccountCursor{
		CreatedAt: time.Date(2026, 8, 14, 7, 0, 0, 0, time.UTC),
		UserID:    42,
	}
	const search = `alice%_\part`
	statement := buildManagedAccountListQuery(db, domainaccount.ManagedAccountQuery{
		UserID: 7, Search: search, Status: domainaccount.StatusFrozen,
		Cursor: cursor, Limit: 21,
	}).Session(&gorm.Session{DryRun: true}).Scan(&[]managedAccountModel{}).Statement
	sql := statement.SQL.String()
	for _, fragment := range []string{
		"a.role =", "a.status =", "a.id =", "a.account ILIKE", "a.nickname ILIKE",
		"a.created_at <", "ORDER BY a.created_at DESC,a.id DESC", "LIMIT",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("managed account SQL missing %q:\n%s", fragment, sql)
		}
	}
	for _, forbidden := range []string{
		"a.password", "secret_hash", "previous_secret_hash", "account_refresh_session.id",
	} {
		if strings.Contains(sql, forbidden) {
			t.Fatalf("managed account SQL projected credential field %q:\n%s", forbidden, sql)
		}
	}
	if strings.Contains(sql, search) {
		t.Fatalf("managed account search interpolated input:\n%s", sql)
	}
	if !containsManagedAccountVar(statement.Vars, `%alice\%\_\\part%`) {
		t.Fatalf("escaped search variable missing: %#v", statement.Vars)
	}
	if !containsManagedAccountVar(statement.Vars, domainaccount.RoleUser) {
		t.Fatalf("ordinary role filter missing: %#v", statement.Vars)
	}
}

func containsManagedAccountVar(values []any, want any) bool {
	for _, value := range values {
		if fmt.Sprint(value) == fmt.Sprint(want) {
			return true
		}
	}
	return false
}
