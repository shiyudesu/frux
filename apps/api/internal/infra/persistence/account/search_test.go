package infraaccount

import (
	domainsearch "github.com/shiyudesu/frux/internal/domain/search"
	"fmt"
	"strings"
	"testing"
	"time"

	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestBuildUserSearchQueryIsParameterizedActiveAndStable(t *testing.T) {
	db := newUserSearchDryRunDB(t)
	cursor := &domainsearch.UserCursor{
		Relevance: domainsearch.UserRelevanceNicknamePrefix,
		UpdatedAt: time.Date(2026, 8, 4, 4, 30, 0, 0, time.UTC),
		UserID:    23,
	}
	const query = `name%_\part`
	statement := buildUserSearchQuery(db, query, cursor, 7).
		Session(&gorm.Session{DryRun: true}).
		Scan(&[]userSearchModel{}).
		Statement
	sql := statement.SQL.String()
	for _, fragment := range []string{
		"a.status =", "a.account ILIKE", "a.nickname ILIKE", "relevance >",
		"ORDER BY relevance ASC,updated_at DESC,id DESC", "LIMIT",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("user search SQL missing %q:\n%s", fragment, sql)
		}
	}
	if strings.Contains(sql, query) {
		t.Fatalf("user search interpolated query into SQL:\n%s", sql)
	}
	if strings.Contains(sql, "password") || strings.Contains(sql, "role") {
		t.Fatalf("user search projected private account fields:\n%s", sql)
	}
	if !containsUserSearchVar(statement.Vars, `name\%\_\\part%`) ||
		!containsUserSearchVar(statement.Vars, `%name\%\_\\part%`) {
		t.Fatalf("user search variables do not contain escaped prefix/contains patterns: %#v", statement.Vars)
	}
}

func newUserSearchDryRunDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(gormpostgres.New(gormpostgres.Config{
		DSN: "host=localhost user=frux password=frux dbname=frux sslmode=disable",
	}), &gorm.Config{DryRun: true, DisableAutomaticPing: true})
	if err != nil {
		t.Fatalf("open dry-run PostgreSQL GORM: %v", err)
	}
	return db
}

func containsUserSearchVar(values []any, want string) bool {
	for _, value := range values {
		if fmt.Sprint(value) == want {
			return true
		}
	}
	return false
}
