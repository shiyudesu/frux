package infravideo

import (
	"fmt"
	domainsearch "github.com/shiyudesu/frux/internal/domain/search"
	"strings"
	"testing"
	"time"

	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestBuildVideoSearchQueryIsParameterizedAndStable(t *testing.T) {
	db := newVideoSearchDryRunDB(t)
	cursor := &domainsearch.VideoCursor{
		Relevance:   domainsearch.VideoRelevanceTitleContains,
		PublishedAt: time.Date(2026, 8, 4, 4, 0, 0, 0, time.UTC),
		VideoID:     19,
	}
	const query = `100%_off\sale`
	statement := buildVideoSearchQuery(db, query, cursor, 11).
		Session(&gorm.Session{DryRun: true}).
		Scan(&[]videoSearchModel{}).
		Statement
	sql := statement.SQL.String()
	for _, fragment := range []string{
		"v.status =", "v.visibility =", "v.media_status IN", "v.published_at IS NOT NULL",
		"v.title ILIKE", "v.description ILIKE", "relevance >",
		"ORDER BY relevance ASC,published_at DESC,id DESC", "LIMIT",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("video search SQL missing %q:\n%s", fragment, sql)
		}
	}
	if strings.Contains(sql, query) {
		t.Fatalf("video search interpolated query into SQL:\n%s", sql)
	}
	if !containsSearchVar(statement.Vars, `100\%\_off\\sale%`) ||
		!containsSearchVar(statement.Vars, `%100\%\_off\\sale%`) {
		t.Fatalf("video search variables do not contain escaped prefix/contains patterns: %#v", statement.Vars)
	}
}

func newVideoSearchDryRunDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(gormpostgres.New(gormpostgres.Config{
		DSN: "host=localhost user=frux password=frux dbname=frux sslmode=disable",
	}), &gorm.Config{DryRun: true, DisableAutomaticPing: true})
	if err != nil {
		t.Fatalf("open dry-run PostgreSQL GORM: %v", err)
	}
	return db
}

func containsSearchVar(values []any, want string) bool {
	for _, value := range values {
		if fmt.Sprint(value) == want {
			return true
		}
	}
	return false
}
