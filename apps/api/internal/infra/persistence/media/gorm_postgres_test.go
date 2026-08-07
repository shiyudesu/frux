package inframedia

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	domainmedia "github.com/shiyudesu/frux/internal/domain/media"

	_ "github.com/jackc/pgx/v5/stdlib"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestCleanupTaskPostgreSQLFencingAndDeadline(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("FRUX_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("FRUX_POSTGRES_TEST_DSN is not set")
	}
	db := openMediaPostgres(t, dsn)
	repository := New(db)
	now := time.Now().UTC().Truncate(time.Microsecond)
	task, err := domainmedia.NewCleanupTask(
		0, domainmedia.StorageBackendS3, "moderation/1/frame.jpg",
		now.Add(time.Hour), 3,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.CreateCleanupTasks(context.Background(), []*domainmedia.CleanupTask{task}); err != nil {
		t.Fatal(err)
	}
	earlier, err := domainmedia.NewCleanupTask(
		0, domainmedia.StorageBackendS3, task.ObjectKey, now.Add(time.Minute), 3,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.CreateCleanupTasks(context.Background(), []*domainmedia.CleanupTask{earlier}); err != nil {
		t.Fatal(err)
	}
	var stored CleanupTaskModel
	if err := db.Where("object_key = ?", task.ObjectKey).Take(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if !stored.NotBefore.Equal(earlier.NotBefore) {
		t.Fatalf("not_before = %v, want %v", stored.NotBefore, earlier.NotBefore)
	}
	leased, err := repository.LeaseCleanupTasks(
		context.Background(), "cleanup-owner", now.Add(2*time.Minute),
		now.Add(7*time.Minute), 1,
	)
	if err != nil || len(leased) != 1 {
		t.Fatalf("leased = %#v err=%v", leased, err)
	}
	if err := repository.RenewCleanupTaskLease(
		context.Background(), leased[0].ID, "cleanup-owner", 5*time.Minute,
	); err != nil {
		t.Fatal(err)
	}
	leased[0].State = domainmedia.CleanupStateCompleted
	finishedAt := now.Add(3 * time.Minute)
	leased[0].CompletedAt = &finishedAt
	if err := repository.UpdateCleanupTaskOwned(
		context.Background(), leased[0], "stale-owner",
	); err == nil {
		t.Fatal("stale cleanup owner updated task")
	}
	if err := repository.UpdateCleanupTaskOwned(
		context.Background(), leased[0], "cleanup-owner",
	); err != nil {
		t.Fatal(err)
	}
}

func TestUploadSessionPostgreSQLIdempotentReplayIgnoresNewGeneratedID(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("FRUX_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("FRUX_POSTGRES_TEST_DSN is not set")
	}
	db := openMediaPostgres(t, dsn)
	repository := New(db)
	now := time.Now().UTC().Truncate(time.Microsecond)
	first, err := domainmedia.NewUploadSession(
		"session-one", 7, domainmedia.AssetKindVideo, domainmedia.StorageBackendS3,
		"uploads/7/session-one/video/source.mp4", "video/mp4", 1024,
		strings.Repeat("a", 64), "same-key", "same-fingerprint",
		now.Add(time.Hour), now,
	)
	if err != nil {
		t.Fatal(err)
	}
	stored, created, err := repository.CreateUploadSession(context.Background(), first)
	if err != nil || !created {
		t.Fatalf("first session = %#v created=%v err=%v", stored, created, err)
	}
	replay := *first
	replay.ID = "session-two"
	replay.ObjectKey = "uploads/7/session-two/video/source.mp4"
	stored, created, err = repository.CreateUploadSession(context.Background(), &replay)
	if err != nil || created || stored.ID != first.ID {
		t.Fatalf("replay session = %#v created=%v err=%v", stored, created, err)
	}
	replay.RequestFingerprint = "different"
	if _, _, err := repository.CreateUploadSession(
		context.Background(), &replay,
	); !errors.Is(err, domainmedia.ErrUploadSessionConflict) {
		t.Fatalf("conflicting replay error = %v", err)
	}
}

func openMediaPostgres(t *testing.T, dsn string) *gorm.DB {
	t.Helper()
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("frux_media_%d", time.Now().UnixNano())
	if _, err := admin.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec("DROP SCHEMA " + schema + " CASCADE")
		_ = admin.Close()
	})
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	sqlDB, err := sql.Open("pgx", parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(
		gormpostgres.New(gormpostgres.Config{Conn: sqlDB}),
		&gorm.Config{TranslateError: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&CleanupTaskModel{}, &UploadSessionModel{}); err != nil {
		t.Fatal(err)
	}
	return db
}
