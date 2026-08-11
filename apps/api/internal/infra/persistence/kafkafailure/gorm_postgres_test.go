package infrakafkafailure

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	applicationkafkafailure "github.com/shiyudesu/frux/internal/application/kafkafailure"
	domainaccount "github.com/shiyudesu/frux/internal/domain/account"
	domainadminaudit "github.com/shiyudesu/frux/internal/domain/adminaudit"
	domainkafkafailure "github.com/shiyudesu/frux/internal/domain/kafkafailure"
	infraadminaudit "github.com/shiyudesu/frux/internal/infra/persistence/adminaudit"

	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestRepositorySerializesReplayAndPersistsIdempotentAuditPostgres(t *testing.T) {
	db := newKafkaFailurePostgresDB(t)
	if err := db.AutoMigrate(
		&ReplayAttemptModel{}, &infraadminaudit.EventModel{},
	); err != nil {
		t.Fatalf("migrate replay fixtures: %v", err)
	}

	repository := New(db, infraadminaudit.New(db))
	now := time.Date(2026, 8, 10, 16, 0, 0, 0, time.UTC)
	command := mustReplayCommand(
		t, "first-key", "operator_retry",
		"replay-0123456789abcdef0123456789abcdef", now,
	)
	var operations atomic.Int64
	operation := func() applicationkafkafailure.ReplayCompletion {
		operations.Add(1)
		time.Sleep(50 * time.Millisecond)
		return successfulCompletion(t, command, now.Add(time.Second))
	}

	start := make(chan struct{})
	results := make(chan *domainkafkafailure.ReplayResult, 2)
	errs := make(chan error, 2)
	var wait sync.WaitGroup
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			result, err := repository.Execute(context.Background(), command, operation)
			results <- result
			errs <- err
		}()
	}
	close(start)
	wait.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent replay: %v", err)
		}
	}
	duplicates := 0
	for result := range results {
		if result == nil || result.Status != domainkafkafailure.StatusSucceeded {
			t.Fatalf("unexpected concurrent result: %+v", result)
		}
		if result.Duplicate {
			duplicates++
		}
	}
	if operations.Load() != 1 || duplicates != 1 {
		t.Fatalf("operations=%d duplicates=%d", operations.Load(), duplicates)
	}

	conflict := command
	conflict.Reason = domainkafkafailure.ReasonPostFixReplay
	conflict.RequestFingerprint = domainkafkafailure.ReplayRequestFingerprint(
		conflict.Coordinate, conflict.Reason,
	)
	if _, err := repository.Execute(
		context.Background(), conflict, operation,
	); !errors.Is(err, domainkafkafailure.ErrIdempotencyConflict) {
		t.Fatalf("conflict error=%v", err)
	}

	later := mustReplayCommand(
		t, "later-key", "post_fix_replay",
		"replay-fedcba9876543210fedcba9876543210", now.Add(2*time.Second),
	)
	if _, err := repository.Execute(
		context.Background(), later,
		func() applicationkafkafailure.ReplayCompletion {
			operations.Add(1)
			return successfulCompletion(t, later, now.Add(3*time.Second))
		},
	); err != nil {
		t.Fatalf("later intentional replay: %v", err)
	}
	if operations.Load() != 2 {
		t.Fatalf("later replay operations=%d", operations.Load())
	}

	var attempts []ReplayAttemptModel
	if err := db.Order("id").Find(&attempts).Error; err != nil {
		t.Fatalf("list attempts: %v", err)
	}
	if len(attempts) != 2 ||
		attempts[0].IdempotencyKeyFingerprint == "first-key" ||
		attempts[0].Status != string(domainkafkafailure.StatusSucceeded) {
		t.Fatalf("attempts=%+v", attempts)
	}
	var audits int64
	if err := db.Model(&infraadminaudit.EventModel{}).
		Where("action = ?", string(domainadminaudit.ActionKafkaDeadLetterReplay)).
		Count(&audits).Error; err != nil {
		t.Fatalf("count audits: %v", err)
	}
	if audits != 2 {
		t.Fatalf("audit count=%d", audits)
	}
}

func TestRepositorySerializesDifferentKeysForOneCoordinatePostgres(t *testing.T) {
	db := newKafkaFailurePostgresDB(t)
	if err := db.AutoMigrate(
		&ReplayAttemptModel{}, &infraadminaudit.EventModel{},
	); err != nil {
		t.Fatal(err)
	}
	repository := New(db, infraadminaudit.New(db))
	now := time.Date(2026, 8, 10, 16, 30, 0, 0, time.UTC)
	commands := []domainkafkafailure.ReplayCommand{
		mustReplayCommand(
			t, "coordinate-first", "operator_retry",
			"replay-0123456789abcdef0123456789abcdef", now,
		),
		mustReplayCommand(
			t, "coordinate-second", "post_fix_replay",
			"replay-fedcba9876543210fedcba9876543210", now.Add(time.Second),
		),
	}
	var active atomic.Int64
	var maxActive atomic.Int64
	start := make(chan struct{})
	errs := make(chan error, len(commands))
	var wait sync.WaitGroup
	for _, command := range commands {
		command := command
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			_, err := repository.Execute(
				context.Background(),
				command,
				func() applicationkafkafailure.ReplayCompletion {
					current := active.Add(1)
					for {
						seen := maxActive.Load()
						if current <= seen || maxActive.CompareAndSwap(seen, current) {
							break
						}
					}
					time.Sleep(50 * time.Millisecond)
					active.Add(-1)
					return successfulCompletion(
						t, command, command.RequestedAt.Add(2*time.Second),
					)
				},
			)
			errs <- err
		}()
	}
	close(start)
	wait.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("coordinate replay: %v", err)
		}
	}
	if maxActive.Load() != 1 {
		t.Fatalf("concurrent coordinate operations=%d", maxActive.Load())
	}
}

func TestRepositoryCommitsPendingBeforeOperationPostgres(t *testing.T) {
	db := newKafkaFailurePostgresDB(t)
	if err := db.AutoMigrate(
		&ReplayAttemptModel{}, &infraadminaudit.EventModel{},
	); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`
		CREATE FUNCTION reject_kafka_replay_pending() RETURNS trigger AS $$
		BEGIN
			RAISE EXCEPTION 'forced pending commit failure';
		END;
		$$ LANGUAGE plpgsql
	`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`
		CREATE TRIGGER reject_kafka_replay_pending
		BEFORE INSERT ON kafka_failure_replay_attempt
		FOR EACH ROW EXECUTE FUNCTION reject_kafka_replay_pending()
	`).Error; err != nil {
		t.Fatal(err)
	}
	repository := New(db, infraadminaudit.New(db))
	command := mustReplayCommand(
		t, "pending-failure", "operator_retry",
		"replay-0123456789abcdef0123456789abcdef",
		time.Date(2026, 8, 10, 17, 0, 0, 0, time.UTC),
	)
	var operations atomic.Int64
	result, err := repository.Execute(
		context.Background(),
		command,
		func() applicationkafkafailure.ReplayCompletion {
			operations.Add(1)
			return successfulCompletion(t, command, command.RequestedAt.Add(time.Second))
		},
	)
	if err == nil || result != nil || operations.Load() != 0 {
		t.Fatalf("result=%+v err=%v operations=%d", result, err, operations.Load())
	}
}

func TestRepositoryKeepsAcknowledgedReplayPendingAfterFinalizationFailurePostgres(t *testing.T) {
	db := newKafkaFailurePostgresDB(t)
	if err := db.AutoMigrate(
		&ReplayAttemptModel{}, &infraadminaudit.EventModel{},
	); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`
		CREATE FUNCTION reject_kafka_replay_finalization() RETURNS trigger AS $$
		BEGIN
			IF NEW.status <> 'pending' THEN
				RAISE EXCEPTION 'forced replay finalization failure';
			END IF;
			RETURN NEW;
		END;
		$$ LANGUAGE plpgsql
	`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`
		CREATE TRIGGER reject_kafka_replay_finalization
		BEFORE UPDATE ON kafka_failure_replay_attempt
		FOR EACH ROW EXECUTE FUNCTION reject_kafka_replay_finalization()
	`).Error; err != nil {
		t.Fatal(err)
	}
	repository := New(db, infraadminaudit.New(db))
	command := mustReplayCommand(
		t, "unknown-outcome", "operator_retry",
		"replay-0123456789abcdef0123456789abcdef",
		time.Date(2026, 8, 10, 18, 0, 0, 0, time.UTC),
	)
	var operations atomic.Int64
	operation := func() applicationkafkafailure.ReplayCompletion {
		operations.Add(1)
		return successfulCompletion(t, command, command.RequestedAt.Add(time.Second))
	}
	result, err := repository.Execute(context.Background(), command, operation)
	if err == nil || result == nil ||
		result.Status != domainkafkafailure.StatusPending ||
		operations.Load() != 1 {
		t.Fatalf("result=%+v err=%v operations=%d", result, err, operations.Load())
	}
	var stored ReplayAttemptModel
	if err := db.Take(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != string(domainkafkafailure.StatusPending) ||
		stored.CompletedAt != nil {
		t.Fatalf("stored=%+v", stored)
	}

	repeated, err := repository.Execute(context.Background(), command, operation)
	if !errors.Is(err, domainkafkafailure.ErrReplayPending) ||
		repeated == nil || !repeated.Duplicate ||
		repeated.Status != domainkafkafailure.StatusPending ||
		repeated.ReplayID != command.ReplayID ||
		operations.Load() != 1 {
		t.Fatalf(
			"repeated=%+v err=%v operations=%d",
			repeated, err, operations.Load(),
		)
	}
	other := mustReplayCommand(
		t, "unknown-outcome-new-key", "post_fix_replay",
		"replay-fedcba9876543210fedcba9876543210",
		command.RequestedAt.Add(2*time.Second),
	)
	if result, err := repository.Execute(
		context.Background(), other, operation,
	); !errors.Is(err, domainkafkafailure.ErrReplayPending) ||
		result != nil || operations.Load() != 1 {
		t.Fatalf(
			"new-key result=%+v err=%v operations=%d",
			result, err, operations.Load(),
		)
	}
}

func TestRepositoryKeepsUncertainReplayClaimPendingPostgres(t *testing.T) {
	db := newKafkaFailurePostgresDB(t)
	if err := db.AutoMigrate(
		&ReplayAttemptModel{}, &infraadminaudit.EventModel{},
	); err != nil {
		t.Fatal(err)
	}
	repository := New(db, infraadminaudit.New(db))
	command := mustReplayCommand(
		t, "uncertain-publish", "operator_retry",
		"replay-0123456789abcdef0123456789abcdef",
		time.Date(2026, 8, 10, 18, 30, 0, 0, time.UTC),
	)
	var operations atomic.Int64
	result, err := repository.Execute(
		context.Background(),
		command,
		func() applicationkafkafailure.ReplayCompletion {
			operations.Add(1)
			return applicationkafkafailure.ReplayCompletion{
				Status: domainkafkafailure.StatusPending,
			}
		},
	)
	if !errors.Is(err, domainkafkafailure.ErrReplayPending) ||
		result == nil || result.Status != domainkafkafailure.StatusPending ||
		result.ReplayID != command.ReplayID || operations.Load() != 1 {
		t.Fatalf("result=%+v err=%v operations=%d", result, err, operations.Load())
	}

	var stored ReplayAttemptModel
	if err := db.Take(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != string(domainkafkafailure.StatusPending) ||
		stored.ReplayID != command.ReplayID ||
		stored.FailureCode != "" || stored.CompletedAt != nil {
		t.Fatalf("stored=%+v", stored)
	}
	var audits int64
	if err := db.Model(&infraadminaudit.EventModel{}).Count(&audits).Error; err != nil {
		t.Fatal(err)
	}
	if audits != 0 {
		t.Fatalf("audit count=%d", audits)
	}

	reconciled, err := repository.Execute(
		context.Background(),
		command,
		func() applicationkafkafailure.ReplayCompletion {
			operations.Add(1)
			return applicationkafkafailure.ReplayCompletion{}
		},
		func(pendingCommand domainkafkafailure.ReplayCommand) (
			applicationkafkafailure.ReplayCompletion,
			error,
		) {
			return successfulCompletion(
				t, pendingCommand, command.RequestedAt.Add(time.Second),
			), nil
		},
	)
	if err != nil || reconciled == nil ||
		reconciled.Status != domainkafkafailure.StatusSucceeded ||
		!reconciled.Duplicate || reconciled.ReplayID != command.ReplayID ||
		operations.Load() != 1 {
		t.Fatalf(
			"reconciled=%+v err=%v operations=%d",
			reconciled, err, operations.Load(),
		)
	}
}

func TestRepositoryAuditFailureRollsFinalizationBackToPendingPostgres(t *testing.T) {
	db := newKafkaFailurePostgresDB(t)
	if err := db.AutoMigrate(
		&ReplayAttemptModel{}, &infraadminaudit.EventModel{},
	); err != nil {
		t.Fatal(err)
	}

	repository := New(db, failingKafkaReplayAuditWriter{})
	command := mustReplayCommand(
		t, "audit-failure", "operator_retry",
		"replay-0123456789abcdef0123456789abcdef",
		time.Date(2026, 8, 10, 19, 0, 0, 0, time.UTC),
	)
	result, err := repository.Execute(
		context.Background(),
		command,
		func() applicationkafkafailure.ReplayCompletion {
			return successfulCompletion(t, command, command.RequestedAt.Add(time.Second))
		},
	)
	if err == nil || result == nil ||
		result.Status != domainkafkafailure.StatusPending {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	var stored ReplayAttemptModel
	if err := db.Take(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != string(domainkafkafailure.StatusPending) ||
		stored.CompletedAt != nil {
		t.Fatalf("stored=%+v", stored)
	}
	var audits int64
	if err := db.Model(&infraadminaudit.EventModel{}).Count(&audits).Error; err != nil {
		t.Fatal(err)
	}
	if audits != 0 {
		t.Fatalf("audit count=%d", audits)
	}
}

func TestRepositoryReconcilesPendingReplayAndWritesAuditPostgres(t *testing.T) {
	db := newKafkaFailurePostgresDB(t)
	if err := db.AutoMigrate(
		&ReplayAttemptModel{}, &infraadminaudit.EventModel{},
	); err != nil {
		t.Fatal(err)
	}
	auditWriter := &switchableKafkaReplayAuditWriter{
		delegate: infraadminaudit.New(db),
		fail:     true,
	}
	repository := New(db, auditWriter)
	command := mustReplayCommand(
		t, "reconcile-audit", "operator_retry",
		"replay-0123456789abcdef0123456789abcdef",
		time.Date(2026, 8, 10, 20, 0, 0, 0, time.UTC),
	)
	var operations atomic.Int64
	result, err := repository.Execute(
		context.Background(),
		command,
		func() applicationkafkafailure.ReplayCompletion {
			operations.Add(1)
			return successfulCompletion(t, command, command.RequestedAt.Add(time.Second))
		},
	)
	if err == nil || result == nil ||
		result.Status != domainkafkafailure.StatusPending ||
		operations.Load() != 1 {
		t.Fatalf("result=%+v err=%v operations=%d", result, err, operations.Load())
	}

	auditWriter.fail = false
	var reconciliations atomic.Int64
	result, err = repository.Execute(
		context.Background(),
		command,
		func() applicationkafkafailure.ReplayCompletion {
			operations.Add(1)
			return applicationkafkafailure.ReplayCompletion{}
		},
		func(pendingCommand domainkafkafailure.ReplayCommand) (
			applicationkafkafailure.ReplayCompletion,
			error,
		) {
			reconciliations.Add(1)
			if pendingCommand.ReplayID != command.ReplayID {
				t.Fatalf("pending command=%+v", pendingCommand)
			}
			return successfulCompletion(
				t, pendingCommand, command.RequestedAt.Add(2*time.Second),
			), nil
		},
	)
	if err != nil || result == nil ||
		result.Status != domainkafkafailure.StatusSucceeded ||
		!result.Duplicate || operations.Load() != 1 ||
		reconciliations.Load() != 1 {
		t.Fatalf(
			"result=%+v err=%v operations=%d reconciliations=%d",
			result, err, operations.Load(), reconciliations.Load(),
		)
	}
	var audits int64
	if err := db.Model(&infraadminaudit.EventModel{}).
		Where("action = ?", string(domainadminaudit.ActionKafkaDeadLetterReplay)).
		Count(&audits).Error; err != nil {
		t.Fatal(err)
	}
	if audits != 1 {
		t.Fatalf("audit count=%d", audits)
	}
}

type failingKafkaReplayAuditWriter struct{}

func (failingKafkaReplayAuditWriter) AppendInTransaction(
	context.Context,
	*gorm.DB,
	*domainadminaudit.Fact,
) error {
	return errors.New("forced audit failure")
}

func (failingKafkaReplayAuditWriter) RecordCommittedWrite(*domainadminaudit.Fact) {}

type switchableKafkaReplayAuditWriter struct {
	delegate AuditWriter
	fail     bool
}

func (w *switchableKafkaReplayAuditWriter) AppendInTransaction(
	ctx context.Context,
	tx *gorm.DB,
	fact *domainadminaudit.Fact,
) error {
	if w.fail {
		return errors.New("forced audit failure")
	}
	return w.delegate.AppendInTransaction(ctx, tx, fact)
}

func (w *switchableKafkaReplayAuditWriter) RecordCommittedWrite(
	fact *domainadminaudit.Fact,
) {
	w.delegate.RecordCommittedWrite(fact)
}

func mustReplayCommand(
	t *testing.T,
	key, reason, replayID string,
	now time.Time,
) domainkafkafailure.ReplayCommand {
	t.Helper()
	command, err := domainkafkafailure.NewReplayCommand(
		"frux.feed.video-published.dlq.v1", 2, 41, 9,
		reason, key, replayID, now,
	)
	if err != nil {
		t.Fatalf("new replay command: %v", err)
	}
	return command
}

func successfulCompletion(
	t *testing.T,
	command domainkafkafailure.ReplayCommand,
	completedAt time.Time,
) applicationkafkafailure.ReplayCompletion {
	t.Helper()
	detail := map[string]string{
		"http_method": "POST",
		"route":       "/api/admin/kafka-dead-letters/:topic/records/:partition/:offset/replay",
		"reason_code": string(command.Reason),
		"topic":       command.Coordinate.Topic,
		"partition":   "2", "offset": "41",
		"source_topic":     "frux.video.published.v1",
		"source_partition": "1", "source_offset": "29",
		"consumer_group":    "feed_video_published_active",
		"original_event_id": "event-video-42",
		"replay_id":         command.ReplayID,
	}
	fact, err := domainadminaudit.NewFact(domainadminaudit.FactInput{
		ActorID:            command.ActorID,
		Permission:         domainaccount.PermissionGovernanceExecute,
		Action:             domainadminaudit.ActionKafkaDeadLetterReplay,
		TargetType:         domainadminaudit.TargetKafkaDeadLetterRecord,
		TargetID:           "coordinate",
		Outcome:            domainadminaudit.OutcomeSuccess,
		RequestID:          domainadminaudit.NewRequestID(),
		IdempotencyKeyHash: command.IdempotencyFingerprint,
		Detail:             detail,
		CreatedAt:          completedAt,
	})
	if err != nil {
		t.Fatalf("new audit fact: %v", err)
	}
	return applicationkafkafailure.ReplayCompletion{
		SourceTopic:     "frux.video.published.v1",
		SourcePartition: 1,
		SourceOffset:    29,
		ConsumerGroup:   "feed_video_published_active",
		EventID:         "event-video-42",
		Status:          domainkafkafailure.StatusSucceeded,
		CompletedAt:     completedAt,
		AuditFact:       fact,
	}
}

func newKafkaFailurePostgresDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("FRUX_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("FRUX_POSTGRES_TEST_DSN is not set; skipping real PostgreSQL integration test")
	}
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	schema := fmt.Sprintf("frux_kafka_failure_test_%d", time.Now().UnixNano())
	if _, err := admin.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec("DROP SCHEMA " + schema + " CASCADE"); err != nil {
			t.Errorf("drop schema: %v", err)
		}
		_ = admin.Close()
	})
	sqlDB, err := sql.Open("pgx", kafkaFailurePostgresDSNWithSchema(dsn, schema))
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

func kafkaFailurePostgresDSNWithSchema(dsn, schema string) string {
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
