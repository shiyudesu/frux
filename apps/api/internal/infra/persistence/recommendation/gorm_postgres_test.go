package infrarecommendation

import (
	applicationexposure "GCFeed/internal/application/exposure"
	"context"
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

func TestApplyBehaviorEventDeduplicatesLegacyQueuedMessageByViewEventID(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("GCFEED_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("GCFEED_POSTGRES_TEST_DSN is not set; skipping real PostgreSQL integration test")
	}
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	schema := fmt.Sprintf("gcfeed_recommendation_behavior_test_%d", time.Now().UnixNano())
	if _, err := admin.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec("DROP SCHEMA " + schema + " CASCADE"); err != nil {
			t.Errorf("drop schema: %v", err)
		}
		_ = admin.Close()
	})

	sqlDB, err := sql.Open("pgx", recommendationPostgresDSNWithSchema(dsn, schema))
	if err != nil {
		t.Fatalf("open schema PostgreSQL: %v", err)
	}
	defer sqlDB.Close()
	db, err := gorm.Open(gormpostgres.New(gormpostgres.Config{Conn: sqlDB}), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatalf("open GORM: %v", err)
	}
	if err := db.AutoMigrate(&BehaviorEventModel{}); err != nil {
		t.Fatalf("migrate behavior events: %v", err)
	}

	occurredAt := time.Now().UTC().Truncate(time.Microsecond)
	if err := db.Create(&BehaviorEventModel{
		EventID: "legacy-55", ViewEventID: 55, UserID: 9, VideoID: 1001,
		EventType: "play", OccurredAt: occurredAt,
	}).Error; err != nil {
		t.Fatalf("create backfilled behavior event: %v", err)
	}

	repo := New(db)
	applied, err := repo.ApplyBehaviorEvent(context.Background(), &applicationexposure.ViewEventRecordedEvent{
		EventID: "old-random-message-id", ViewEventID: 55, UserID: 9, VideoID: 1001,
		EventType: "play", OccurredAt: occurredAt,
	})
	if err != nil || applied {
		t.Fatalf("legacy queued duplicate: applied=%v err=%v", applied, err)
	}
	var count int64
	if err := db.Model(&BehaviorEventModel{}).Count(&count).Error; err != nil {
		t.Fatalf("count behavior events: %v", err)
	}
	if count != 1 {
		t.Fatalf("legacy queued duplicate inserted another row: count=%d", count)
	}
}

func recommendationPostgresDSNWithSchema(dsn, schema string) string {
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
