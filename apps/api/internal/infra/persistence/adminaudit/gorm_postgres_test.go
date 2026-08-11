package infraadminaudit

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	domainaccount "github.com/shiyudesu/frux/internal/domain/account"
	domainadminaudit "github.com/shiyudesu/frux/internal/domain/adminaudit"

	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"

	_ "github.com/jackc/pgx/v5/stdlib"
)

type protectedMutationModel struct {
	ID    int64  `gorm:"column:id;primaryKey;autoIncrement"`
	Value string `gorm:"column:value;size:64;not null"`
}

func (protectedMutationModel) TableName() string {
	return "protected_mutation_test"
}

type auditWriteObserver struct {
	observations []string
}

func (o *auditWriteObserver) RecordAdminAuditWrite(outcome, result string) {
	o.observations = append(o.observations, outcome+":"+result)
}

func TestRepositoryAppendListAndCursorPostgres(t *testing.T) {
	db := newAdminAuditPostgresDB(t)
	if err := db.AutoMigrate(&EventModel{}); err != nil {
		t.Fatalf("migrate audit event: %v", err)
	}
	observer := &auditWriteObserver{}
	repository := New(db, WithWriteObserver(observer))
	base := time.Date(2026, 8, 5, 8, 0, 0, 0, time.UTC)
	facts := []*domainadminaudit.Fact{
		mustAuditFact(t, 1, domainadminaudit.ActionContentEnforce, domainadminaudit.OutcomeSuccess, "video-1", base),
		mustAuditFact(t, 2, domainadminaudit.ActionAuditQuery, domainadminaudit.OutcomeDenied, "events", base.Add(time.Minute)),
		mustAuditFact(t, 1, domainadminaudit.ActionContentEnforce, domainadminaudit.OutcomeSuccess, "video-2", base.Add(time.Minute)),
		mustAuditFact(t, 1, domainadminaudit.ActionContentRestore, domainadminaudit.OutcomeSuccess, "video-3", base.Add(2*time.Minute)),
	}
	for _, fact := range facts {
		if err := repository.Append(context.Background(), fact); err != nil {
			t.Fatalf("append audit fact: %v", err)
		}
	}
	if len(observer.observations) != len(facts) {
		t.Fatalf("write observations = %#v", observer.observations)
	}
	for _, observation := range observer.observations {
		if !strings.HasSuffix(observation, ":"+WriteResultCommitted) {
			t.Fatalf("standalone append was not observed after commit: %#v", observer.observations)
		}
	}

	first, err := repository.List(context.Background(), domainadminaudit.Query{
		From: base.Add(-time.Second), To: base.Add(3 * time.Minute), Limit: 2,
	})
	if err != nil {
		t.Fatalf("list first page: %v", err)
	}
	if len(first) != 2 || first[0].TargetID() != "video-3" || first[1].TargetID() != "video-2" {
		t.Fatalf("unexpected first page: %#v", auditTargetIDs(first))
	}
	second, err := repository.List(context.Background(), domainadminaudit.Query{
		From: base.Add(-time.Second), To: base.Add(3 * time.Minute), Limit: 2,
		Cursor: &domainadminaudit.Cursor{CreatedAt: first[1].CreatedAt(), EventID: first[1].ID()},
	})
	if err != nil {
		t.Fatalf("list second page: %v", err)
	}
	if len(second) != 2 || second[0].TargetID() != "events" || second[1].TargetID() != "video-1" {
		t.Fatalf("unexpected second page: %#v", auditTargetIDs(second))
	}

	filtered, err := repository.List(context.Background(), domainadminaudit.Query{
		ActorID: 1, Action: domainadminaudit.ActionContentEnforce,
		TargetType: domainadminaudit.TargetVideo, Outcome: domainadminaudit.OutcomeSuccess,
		From: base.Add(-time.Second), To: base.Add(3 * time.Minute), Limit: 10,
	})
	if err != nil {
		t.Fatalf("list filtered events: %v", err)
	}
	if len(filtered) != 2 || filtered[0].TargetID() != "video-2" || filtered[1].TargetID() != "video-1" {
		t.Fatalf("unexpected filtered page: %#v", auditTargetIDs(filtered))
	}
}

func TestRepositoryListsPreRetirementDeadLetterAuditPostgres(t *testing.T) {
	db := newAdminAuditPostgresDB(t)
	if err := db.AutoMigrate(&EventModel{}); err != nil {
		t.Fatalf("migrate audit event: %v", err)
	}
	createdAt := time.Date(2026, 8, 10, 8, 0, 0, 0, time.UTC)
	model := EventModel{
		ActorID:    9,
		Permission: string(domainaccount.PermissionGovernanceExecute),
		Action:     string(domainadminaudit.ActionDeadLetterReplay),
		TargetType: string(domainadminaudit.TargetDeadLetterMessage),
		TargetID:   "legacy-action-1",
		Outcome:    string(domainadminaudit.OutcomeSuccess),
		RequestID:  domainadminaudit.NewRequestID(),
		DetailJSON: `{"http_method":"POST","original_event_id":"legacy-action-1","queue":"frux.interaction.action_changed.dlq.q2","reason_code":"operator_retry","replay_id":"replay-0123456789abcdef0123456789abcdef","route":"/api/admin/dead-letter-messages/:messageId/replay"}`,
		CreatedAt:  createdAt,
	}
	if err := db.Create(&model).Error; err != nil {
		t.Fatalf("insert pre-retirement audit fact: %v", err)
	}
	facts, err := New(db).List(context.Background(), domainadminaudit.Query{
		Action: domainadminaudit.ActionDeadLetterReplay,
		From:   createdAt.Add(-time.Second),
		To:     createdAt.Add(time.Second),
		Limit:  10,
	})
	if err != nil {
		t.Fatalf("list pre-retirement audit fact: %v", err)
	}
	if len(facts) != 1 ||
		facts[0].Action() != domainadminaudit.ActionDeadLetterReplay ||
		facts[0].Detail()["queue"] != "frux.interaction.action_changed.dlq.q2" {
		t.Fatalf("pre-retirement audit facts = %#v", facts)
	}
}

func TestAppendInTransactionFailureRollsBackProtectedMutation(t *testing.T) {
	db := newAdminAuditPostgresDB(t)
	if err := db.AutoMigrate(&EventModel{}, &protectedMutationModel{}); err != nil {
		t.Fatalf("migrate transaction fixtures: %v", err)
	}
	if err := db.Exec(`
		CREATE FUNCTION reject_admin_audit_event() RETURNS trigger AS $$
		BEGIN
			RAISE EXCEPTION 'forced audit failure';
		END;
		$$ LANGUAGE plpgsql
	`).Error; err != nil {
		t.Fatalf("create audit rejection function: %v", err)
	}
	if err := db.Exec(`
		CREATE TRIGGER reject_admin_audit_event
		BEFORE INSERT ON admin_audit_event
		FOR EACH ROW EXECUTE FUNCTION reject_admin_audit_event()
	`).Error; err != nil {
		t.Fatalf("create audit rejection trigger: %v", err)
	}

	observer := &auditWriteObserver{}
	repository := New(db, WithWriteObserver(observer))
	fact := mustAuditFact(
		t, 9, domainadminaudit.ActionContentEnforce, domainadminaudit.OutcomeSuccess,
		"video-9", time.Now().UTC(),
	)
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&protectedMutationModel{Value: "must rollback"}).Error; err != nil {
			return err
		}
		return repository.AppendInTransaction(context.Background(), tx, fact)
	})
	if err == nil {
		t.Fatal("expected forced audit insertion failure")
	}
	var mutationCount int64
	if err := db.Model(&protectedMutationModel{}).Count(&mutationCount).Error; err != nil {
		t.Fatalf("count protected mutations: %v", err)
	}
	if mutationCount != 0 {
		t.Fatalf("protected mutation committed without audit fact: %d", mutationCount)
	}
	if len(observer.observations) != 1 ||
		observer.observations[0] != string(domainadminaudit.OutcomeSuccess)+":"+WriteResultFailed {
		t.Fatalf("unexpected write observations: %#v", observer.observations)
	}
}

func newAdminAuditPostgresDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("FRUX_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("FRUX_POSTGRES_TEST_DSN is not set; skipping real PostgreSQL integration test")
	}
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	schema := fmt.Sprintf("frux_admin_audit_test_%d", time.Now().UnixNano())
	if _, err := admin.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec("DROP SCHEMA " + schema + " CASCADE"); err != nil {
			t.Errorf("drop schema: %v", err)
		}
		_ = admin.Close()
	})

	sqlDB, err := sql.Open("pgx", adminAuditPostgresDSNWithSchema(dsn, schema))
	if err != nil {
		t.Fatalf("open schema PostgreSQL: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(gormpostgres.New(gormpostgres.Config{Conn: sqlDB}), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatalf("open GORM: %v", err)
	}
	return db
}

func adminAuditPostgresDSNWithSchema(dsn, schema string) string {
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

func mustAuditFact(
	t *testing.T,
	actorID int64,
	action domainadminaudit.Action,
	outcome domainadminaudit.Outcome,
	targetID string,
	createdAt time.Time,
) *domainadminaudit.Fact {
	t.Helper()
	permission := domainaccount.PermissionContentEnforce
	targetType := domainadminaudit.TargetVideo
	detail := map[string]string{
		"http_method": "POST", "route": "/api/admin/videos/:videoId/enforcement",
		"reason_code": "policy_violation", "previous_status": "published", "new_status": "offline",
	}
	if action == domainadminaudit.ActionAuditQuery {
		permission = domainaccount.PermissionAuditRead
		targetType = domainadminaudit.TargetAuditTrail
		detail = map[string]string{
			"http_method": "GET", "reason_code": "permission_denied", "route": "/api/admin/audit-events",
		}
	}
	if action == domainadminaudit.ActionContentRestore {
		detail = map[string]string{
			"http_method": "POST", "route": "/api/admin/videos/:videoId/restoration",
			"reason_code": "compliance_restored", "previous_status": "offline", "new_status": "published",
		}
	}
	fact, err := domainadminaudit.NewFact(domainadminaudit.FactInput{
		ActorID: actorID, Permission: permission, Action: action,
		TargetType: targetType, TargetID: targetID, Outcome: outcome,
		RequestID: domainadminaudit.NewRequestID(),
		Detail:    detail, CreatedAt: createdAt,
	})
	if err != nil {
		t.Fatalf("new audit fact: %v", err)
	}
	return fact
}

func auditTargetIDs(facts []*domainadminaudit.Fact) []string {
	targetIDs := make([]string, 0, len(facts))
	for _, fact := range facts {
		targetIDs = append(targetIDs, fact.TargetID())
	}
	return targetIDs
}
