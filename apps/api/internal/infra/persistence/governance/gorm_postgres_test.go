package infragovernance

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	applicationgovernance "github.com/shiyudesu/frux/internal/application/governance"
	domaingovernance "github.com/shiyudesu/frux/internal/domain/governance"
	infraadminaudit "github.com/shiyudesu/frux/internal/infra/persistence/adminaudit"

	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestRepositoryConcurrentRevisionAndAuditRollbackPostgres(t *testing.T) {
	db := newGovernancePostgresDB(t)
	if err := db.AutoMigrate(
		&RevisionModel{}, &ActiveModel{}, &infraadminaudit.EventModel{},
	); err != nil {
		t.Fatalf("migrate governance models: %v", err)
	}
	registry := domaingovernance.DefaultRegistry()
	auditRepo := infraadminaudit.New(db)
	repository := New(db, registry, auditRepo)
	now := time.Date(2026, 8, 6, 13, 0, 0, 0, time.UTC)
	service := applicationgovernance.New(
		registry, repository,
		applicationgovernance.WithClock(func() time.Time { return now }),
	)

	start := make(chan struct{})
	results := make(chan error, 2)
	var wait sync.WaitGroup
	for actorID := int64(1); actorID <= 2; actorID++ {
		wait.Add(1)
		go func(actor int64) {
			defer wait.Done()
			<-start
			_, err := service.Update(context.Background(), applicationgovernance.UpdateRequest{
				Key: domaingovernance.FeedPreloadEnabled, ActorID: actor,
				ExpectedRevision: 0, Value: domaingovernance.BooleanValue(false),
				Reason: "concurrent operator update",
			})
			results <- err
		}(actorID)
	}
	close(start)
	wait.Wait()
	close(results)
	successes, conflicts := 0, 0
	for err := range results {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, domaingovernance.ErrRevisionConflict):
			conflicts++
		default:
			t.Fatalf("unexpected concurrent error: %v", err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("successes=%d conflicts=%d", successes, conflicts)
	}

	second, err := service.Update(context.Background(), applicationgovernance.UpdateRequest{
		Key: domaingovernance.FeedPreloadEnabled, ActorID: 3,
		ExpectedRevision: 1, Value: domaingovernance.BooleanValue(true),
		Reason: "restore preloading",
	})
	if err != nil {
		t.Fatalf("second update: %v", err)
	}
	rollback, err := service.Rollback(context.Background(), applicationgovernance.RollbackRequest{
		Key: domaingovernance.FeedPreloadEnabled, ActorID: 4,
		ExpectedRevision: second.Number(), TargetRevision: 1,
		Reason: "rollback preloading regression",
	})
	if err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if rollback.Number() != 3 || rollback.RollbackFromRevision() != 1 {
		t.Fatalf("unexpected rollback revision: %+v", rollback)
	}

	active, err := repository.ListActive(context.Background())
	if err != nil {
		t.Fatalf("list active: %v", err)
	}
	history, err := repository.ListRevisions(
		context.Background(), domaingovernance.FeedPreloadEnabled, 10,
	)
	if err != nil {
		t.Fatalf("list revisions: %v", err)
	}
	if len(active) != 1 || active[0].Number() != 3 || len(history) != 3 {
		t.Fatalf("active=%#v history=%#v", active, history)
	}
	var auditCount int64
	if err := db.Model(&infraadminaudit.EventModel{}).
		Where("action = ?", "governance.execute").
		Count(&auditCount).Error; err != nil {
		t.Fatalf("count governance audits: %v", err)
	}
	if auditCount != 3 {
		t.Fatalf("governance audit count=%d", auditCount)
	}

	if err := db.Exec(`
		CREATE FUNCTION reject_governance_audit() RETURNS trigger AS $$
		BEGIN
			IF NEW.action = 'governance.execute' THEN
				RAISE EXCEPTION 'forced governance audit failure';
			END IF;
			RETURN NEW;
		END;
		$$ LANGUAGE plpgsql
	`).Error; err != nil {
		t.Fatalf("create audit rejection function: %v", err)
	}
	if err := db.Exec(`
		CREATE TRIGGER reject_governance_audit
		BEFORE INSERT ON admin_audit_event
		FOR EACH ROW EXECUTE FUNCTION reject_governance_audit()
	`).Error; err != nil {
		t.Fatalf("create audit rejection trigger: %v", err)
	}
	if _, err := service.Update(context.Background(), applicationgovernance.UpdateRequest{
		Key: domaingovernance.FeedPreloadEnabled, ActorID: 5,
		ExpectedRevision: 3, Value: domaingovernance.BooleanValue(true),
		Reason: "must fail atomically",
	}); !errors.Is(err, applicationgovernance.ErrUpdateControlFailed) {
		t.Fatalf("audit failure error=%v", err)
	}
	active, err = repository.ListActive(context.Background())
	if err != nil {
		t.Fatalf("list active after audit failure: %v", err)
	}
	if len(active) != 1 || active[0].Number() != 3 {
		t.Fatalf("audit failure changed active revision: %#v", active)
	}
	if _, err := repository.FindRevision(
		context.Background(), domaingovernance.FeedPreloadEnabled, 4,
	); !errors.Is(err, domaingovernance.ErrRevisionNotFound) {
		t.Fatalf("revision 4 survived audit rollback: %v", err)
	}
}

func newGovernancePostgresDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("FRUX_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("FRUX_POSTGRES_TEST_DSN is not set; skipping real PostgreSQL integration test")
	}
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	schema := fmt.Sprintf("frux_governance_test_%d", time.Now().UnixNano())
	if _, err := admin.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec("DROP SCHEMA " + schema + " CASCADE"); err != nil {
			t.Errorf("drop schema: %v", err)
		}
		_ = admin.Close()
	})
	sqlDB, err := sql.Open("pgx", governancePostgresDSNWithSchema(dsn, schema))
	if err != nil {
		t.Fatalf("open schema PostgreSQL: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(
		gormpostgres.New(gormpostgres.Config{Conn: sqlDB}),
		&gorm.Config{TranslateError: true},
	)
	if err != nil {
		t.Fatalf("open GORM: %v", err)
	}
	return db
}

func governancePostgresDSNWithSchema(dsn, schema string) string {
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
