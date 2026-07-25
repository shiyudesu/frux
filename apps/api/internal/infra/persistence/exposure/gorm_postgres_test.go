package infraexposure

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestViewHistoryUpsertRejectsOutOfOrderEvents(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("GCFEED_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("GCFEED_POSTGRES_TEST_DSN is not set; skipping real PostgreSQL integration test")
	}
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	schema := fmt.Sprintf("gcfeed_exposure_test_%d", time.Now().UnixNano())
	if _, err := admin.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec("DROP SCHEMA " + schema + " CASCADE"); err != nil {
			t.Errorf("drop schema: %v", err)
		}
		if err := admin.Close(); err != nil {
			t.Errorf("close PostgreSQL: %v", err)
		}
	})

	sqlDB, err := sql.Open("pgx", exposurePostgresDSNWithSchema(dsn, schema))
	if err != nil {
		t.Fatalf("open schema PostgreSQL: %v", err)
	}
	defer sqlDB.Close()
	db, err := gorm.Open(gormpostgres.New(gormpostgres.Config{Conn: sqlDB}), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatalf("open GORM: %v", err)
	}
	if err := db.AutoMigrate(&ViewHistoryModel{}); err != nil {
		t.Fatalf("migrate view history: %v", err)
	}

	base := time.Now().UTC().Truncate(time.Microsecond)
	newer := ViewEventModel{
		ID: 20, UserID: 7, VideoID: 9, Scene: "profile",
		EventType: "complete", WatchMs: 2000, Completed: true, CreatedAt: base.Add(2 * time.Second),
	}
	older := ViewEventModel{
		ID: 10, UserID: 7, VideoID: 9, Scene: "timeline",
		EventType: "play", WatchMs: 500, CreatedAt: base.Add(time.Second),
	}
	if err := upsertViewHistory(db, newer); err != nil {
		t.Fatalf("upsert newer event: %v", err)
	}
	if err := upsertViewHistory(db, older); err != nil {
		t.Fatalf("upsert older event: %v", err)
	}

	var history ViewHistoryModel
	if err := db.Where("user_id = ? AND video_id = ?", 7, 9).Take(&history).Error; err != nil {
		t.Fatalf("load history: %v", err)
	}
	if history.LastEventID != newer.ID || history.LastEventType != newer.EventType || !history.Completed || history.LastWatchMs != newer.WatchMs {
		t.Fatalf("older event regressed projection: %+v", history)
	}
	if !history.FirstWatchedAt.Equal(older.CreatedAt) {
		t.Fatalf("first watched time was not preserved: got=%s want=%s", history.FirstWatchedAt, older.CreatedAt)
	}

	tieWinner := newer
	tieWinner.ID = 21
	tieWinner.EventType = "skip"
	tieWinner.Completed = false
	tieWinner.WatchMs = 1700
	if err := upsertViewHistory(db, tieWinner); err != nil {
		t.Fatalf("upsert tie winner: %v", err)
	}
	tieLoser := newer
	tieLoser.ID = 19
	tieLoser.EventType = "play"
	tieLoser.Completed = false
	tieLoser.WatchMs = 100
	if err := upsertViewHistory(db, tieLoser); err != nil {
		t.Fatalf("upsert tie loser: %v", err)
	}
	if err := db.Where("user_id = ? AND video_id = ?", 7, 9).Take(&history).Error; err != nil {
		t.Fatalf("reload history: %v", err)
	}
	if history.LastEventID != tieWinner.ID || history.LastEventType != tieWinner.EventType || history.LastWatchMs != tieWinner.WatchMs {
		t.Fatalf("event ID tie-breaker regressed projection: %+v", history)
	}
}

func exposurePostgresDSNWithSchema(dsn, schema string) string {
	if strings.Contains(dsn, "://") {
		parsed, err := url.Parse(dsn)
		if err == nil {
			query := parsed.Query()
			query.Set("search_path", schema)
			query.Set("TimeZone", "UTC")
			parsed.RawQuery = query.Encode()
			return parsed.String()
		}
	}
	return strings.TrimSpace(dsn) + " search_path=" + schema + " TimeZone=UTC"
}
