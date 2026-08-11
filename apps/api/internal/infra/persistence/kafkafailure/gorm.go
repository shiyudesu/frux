package infrakafkafailure

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"

	applicationkafkafailure "github.com/shiyudesu/frux/internal/application/kafkafailure"
	domainadminaudit "github.com/shiyudesu/frux/internal/domain/adminaudit"
	domainkafkafailure "github.com/shiyudesu/frux/internal/domain/kafkafailure"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type AuditWriter interface {
	AppendInTransaction(ctx context.Context, tx *gorm.DB, fact *domainadminaudit.Fact) error
	RecordCommittedWrite(fact *domainadminaudit.Fact)
}

type Repository struct {
	db          *gorm.DB
	auditWriter AuditWriter
}

func New(db *gorm.DB, auditWriter AuditWriter) *Repository {
	return &Repository{db: db, auditWriter: auditWriter}
}

func (r *Repository) Execute(
	ctx context.Context,
	command domainkafkafailure.ReplayCommand,
	operation func() applicationkafkafailure.ReplayCompletion,
	reconcile ...applicationkafkafailure.ReplayReconciliation,
) (*domainkafkafailure.ReplayResult, error) {
	if r == nil || r.db == nil || r.auditWriter == nil || operation == nil ||
		command.ActorID <= 0 || command.IdempotencyFingerprint == "" ||
		command.RequestFingerprint == "" || command.ReplayID == "" {
		return nil, domainkafkafailure.ErrReplayPersistence
	}
	session, release, err := r.lockReplaySession(ctx, command)
	if err != nil {
		return nil, err
	}
	defer release()

	pending, existing, err := r.claimPending(ctx, session, command)
	if err != nil {
		return nil, err
	}
	if existing {
		if pending.Status == domainkafkafailure.StatusPending {
			if len(reconcile) == 0 || reconcile[0] == nil {
				return pending, domainkafkafailure.ErrReplayPending
			}
			pendingCommand := commandForPending(command, pending)
			completion, reconcileErr := reconcile[0](pendingCommand)
			if reconcileErr != nil {
				return pending, reconcileErr
			}
			if err := validateCompletion(pendingCommand, completion); err != nil {
				return pending, err
			}
			result, err := r.finalize(
				ctx, session, pendingCommand, completion,
			)
			if err != nil {
				return pending, err
			}
			result.Duplicate = true
			result.Reconciled = true
			r.auditWriter.RecordCommittedWrite(completion.AuditFact)
			return result, nil
		}
		return pending, nil
	}

	completion := operation()
	if completion.Status == domainkafkafailure.StatusPending {
		return pending, domainkafkafailure.ErrReplayPending
	}
	if err := validateCompletion(command, completion); err != nil {
		return pending, err
	}
	result, err := r.finalize(ctx, session, command, completion)
	if err != nil {
		return pending, err
	}
	r.auditWriter.RecordCommittedWrite(completion.AuditFact)
	return result, nil
}

func commandForPending(
	command domainkafkafailure.ReplayCommand,
	pending *domainkafkafailure.ReplayResult,
) domainkafkafailure.ReplayCommand {
	if pending == nil {
		return command
	}
	command.Coordinate = pending.Coordinate
	command.ActorID = pending.ActorID
	command.Reason = pending.Reason
	command.ReplayID = pending.ReplayID
	command.RequestedAt = pending.RequestedAt
	return command
}

func (r *Repository) lockReplaySession(
	ctx context.Context,
	command domainkafkafailure.ReplayCommand,
) (*gorm.DB, func(), error) {
	sqlDB, err := r.db.DB()
	if err != nil {
		return nil, nil, err
	}
	connection, err := sqlDB.Conn(ctx)
	if err != nil {
		return nil, nil, err
	}
	idempotencyLock := fmt.Sprintf(
		"%d|%s", command.ActorID, command.IdempotencyFingerprint,
	)
	coordinateLock := command.Coordinate.String()
	if _, err := connection.ExecContext(
		ctx,
		"SELECT pg_advisory_lock(hashtextextended($1, 0))",
		idempotencyLock,
	); err != nil {
		_ = connection.Close()
		return nil, nil, err
	}
	if _, err := connection.ExecContext(
		ctx,
		"SELECT pg_advisory_lock(hashtextextended($1, 1))",
		coordinateLock,
	); err != nil {
		unlockReplaySession(connection, idempotencyLock, coordinateLock, false)
		return nil, nil, err
	}
	session := r.db.Session(&gorm.Session{NewDB: true, Initialized: true})
	session.Statement.ConnPool = connection
	return session, func() {
		unlockReplaySession(connection, idempotencyLock, coordinateLock, true)
	}, nil
}

func unlockReplaySession(
	connection *sql.Conn,
	idempotencyLock string,
	coordinateLock string,
	unlockCoordinate bool,
) {
	if connection == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if unlockCoordinate {
		_, _ = connection.ExecContext(
			ctx,
			"SELECT pg_advisory_unlock(hashtextextended($1, 1))",
			coordinateLock,
		)
	}
	_, _ = connection.ExecContext(
		ctx,
		"SELECT pg_advisory_unlock(hashtextextended($1, 0))",
		idempotencyLock,
	)
	_ = connection.Close()
}

func (r *Repository) claimPending(
	ctx context.Context,
	session *gorm.DB,
	command domainkafkafailure.ReplayCommand,
) (*domainkafkafailure.ReplayResult, bool, error) {
	var result *domainkafkafailure.ReplayResult
	existing := false
	err := session.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var stored ReplayAttemptModel
		findErr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where(
				"actor_id = ? AND idempotency_key_fingerprint = ?",
				command.ActorID, command.IdempotencyFingerprint,
			).
			Take(&stored).Error
		if findErr == nil {
			if stored.RequestFingerprint != command.RequestFingerprint {
				return domainkafkafailure.ErrIdempotencyConflict
			}
			restored, err := restoreResult(stored)
			if err != nil {
				return err
			}
			restored.Duplicate = true
			result = restored
			existing = true
			return nil
		}
		if !errors.Is(findErr, gorm.ErrRecordNotFound) {
			return findErr
		}

		findErr = tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where(
				"dlq_topic = ? AND dlq_partition = ? AND dlq_offset = ? AND status = ?",
				command.Coordinate.Topic,
				command.Coordinate.Partition,
				command.Coordinate.Offset,
				string(domainkafkafailure.StatusPending),
			).
			Take(&stored).Error
		if findErr == nil {
			return domainkafkafailure.ErrReplayPending
		}
		if !errors.Is(findErr, gorm.ErrRecordNotFound) {
			return findErr
		}

		pending := ReplayAttemptModel{
			IdempotencyKeyFingerprint: command.IdempotencyFingerprint,
			RequestFingerprint:        command.RequestFingerprint,
			DLQTopic:                  command.Coordinate.Topic,
			DLQPartition:              command.Coordinate.Partition,
			DLQOffset:                 command.Coordinate.Offset,
			ActorID:                   command.ActorID,
			ReplayID:                  command.ReplayID,
			Reason:                    string(command.Reason),
			Status:                    string(domainkafkafailure.StatusPending),
			RequestedAt:               command.RequestedAt,
		}
		if err := tx.Create(&pending).Error; err != nil {
			return err
		}
		result = pendingResult(command)
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return result, existing, nil
}

func (r *Repository) finalize(
	ctx context.Context,
	session *gorm.DB,
	command domainkafkafailure.ReplayCommand,
	completion applicationkafkafailure.ReplayCompletion,
) (*domainkafkafailure.ReplayResult, error) {
	completedAt := completion.CompletedAt.UTC()
	err := session.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		updates := map[string]any{
			"source_topic":      completion.SourceTopic,
			"source_partition":  completion.SourcePartition,
			"source_offset":     completion.SourceOffset,
			"consumer_group":    completion.ConsumerGroup,
			"original_event_id": completion.EventID,
			"status":            string(completion.Status),
			"failure_code":      string(completion.FailureCode),
			"completed_at":      completedAt,
			"updated_at":        completedAt,
		}
		updated := tx.Model(&ReplayAttemptModel{}).
			Where(
				"actor_id = ? AND idempotency_key_fingerprint = ? AND replay_id = ? AND status = ?",
				command.ActorID,
				command.IdempotencyFingerprint,
				command.ReplayID,
				string(domainkafkafailure.StatusPending),
			).
			Updates(updates)
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return domainkafkafailure.ErrReplayPersistence
		}
		return r.auditWriter.AppendInTransaction(ctx, tx, completion.AuditFact)
	})
	if err != nil {
		return nil, err
	}
	return &domainkafkafailure.ReplayResult{
		Coordinate:      command.Coordinate,
		SourceTopic:     completion.SourceTopic,
		SourcePartition: completion.SourcePartition,
		SourceOffset:    completion.SourceOffset,
		ConsumerGroup:   completion.ConsumerGroup,
		ActorID:         command.ActorID,
		ReplayID:        command.ReplayID,
		Reason:          command.Reason,
		Status:          completion.Status,
		FailureCode:     completion.FailureCode,
		RequestedAt:     command.RequestedAt,
		CompletedAt:     completedAt,
	}, nil
}

func pendingResult(
	command domainkafkafailure.ReplayCommand,
) *domainkafkafailure.ReplayResult {
	return &domainkafkafailure.ReplayResult{
		Coordinate:  command.Coordinate,
		ActorID:     command.ActorID,
		ReplayID:    command.ReplayID,
		Reason:      command.Reason,
		Status:      domainkafkafailure.StatusPending,
		RequestedAt: command.RequestedAt,
	}
}

func validateCompletion(
	command domainkafkafailure.ReplayCommand,
	completion applicationkafkafailure.ReplayCompletion,
) error {
	if completion.AuditFact == nil ||
		completion.CompletedAt.IsZero() ||
		completion.SourceTopic == "" ||
		completion.ConsumerGroup == "" ||
		completion.EventID == "" ||
		completion.SourcePartition < 0 ||
		completion.SourceOffset < 0 ||
		!domainkafkafailure.ValidStatus(completion.Status) ||
		completion.Status == domainkafkafailure.StatusPending ||
		!domainkafkafailure.ValidFailureCode(completion.FailureCode) {
		return domainkafkafailure.ErrReplayPersistence
	}
	if completion.Status == domainkafkafailure.StatusSucceeded &&
		completion.FailureCode != domainkafkafailure.FailureNone {
		return domainkafkafailure.ErrReplayPersistence
	}
	if completion.Status == domainkafkafailure.StatusFailed &&
		completion.FailureCode == domainkafkafailure.FailureNone {
		return domainkafkafailure.ErrReplayPersistence
	}
	fact := completion.AuditFact
	expectedOutcome := domainadminaudit.OutcomeSuccess
	if completion.Status == domainkafkafailure.StatusFailed {
		expectedOutcome = domainadminaudit.OutcomeFailure
	}
	detail := fact.Detail()
	if fact.ActorID() != command.ActorID ||
		fact.Action() != domainadminaudit.ActionKafkaDeadLetterReplay ||
		fact.TargetType() != domainadminaudit.TargetKafkaDeadLetterRecord ||
		fact.Outcome() != expectedOutcome ||
		fact.IdempotencyKeyHash() != command.IdempotencyFingerprint ||
		detail["topic"] != command.Coordinate.Topic ||
		detail["partition"] != strconv.FormatInt(int64(command.Coordinate.Partition), 10) ||
		detail["offset"] != strconv.FormatInt(command.Coordinate.Offset, 10) ||
		detail["reason_code"] != string(command.Reason) ||
		detail["replay_id"] != command.ReplayID ||
		detail["source_topic"] != completion.SourceTopic ||
		detail["source_partition"] != strconv.FormatInt(int64(completion.SourcePartition), 10) ||
		detail["source_offset"] != strconv.FormatInt(completion.SourceOffset, 10) ||
		detail["consumer_group"] != completion.ConsumerGroup {
		return domainkafkafailure.ErrReplayPersistence
	}
	if completion.Status == domainkafkafailure.StatusFailed &&
		detail["failure_code"] != string(completion.FailureCode) {
		return domainkafkafailure.ErrReplayPersistence
	}
	return nil
}

func restoreResult(model ReplayAttemptModel) (*domainkafkafailure.ReplayResult, error) {
	coordinate, err := domainkafkafailure.NormalizeCoordinate(
		model.DLQTopic, model.DLQPartition, model.DLQOffset,
	)
	if err != nil {
		return nil, domainkafkafailure.ErrReplayPersistence
	}
	reason, err := domainkafkafailure.NormalizeReason(model.Reason)
	if err != nil {
		return nil, domainkafkafailure.ErrReplayPersistence
	}
	status := domainkafkafailure.ReplayStatus(model.Status)
	failureCode := domainkafkafailure.FailureCode(model.FailureCode)
	if !domainkafkafailure.ValidStatus(status) ||
		!domainkafkafailure.ValidFailureCode(failureCode) {
		return nil, domainkafkafailure.ErrReplayPersistence
	}
	if status == domainkafkafailure.StatusPending {
		if failureCode != domainkafkafailure.FailureNone || model.CompletedAt != nil {
			return nil, domainkafkafailure.ErrReplayPersistence
		}
		return &domainkafkafailure.ReplayResult{
			Coordinate:  coordinate,
			ActorID:     model.ActorID,
			ReplayID:    model.ReplayID,
			Reason:      reason,
			Status:      status,
			RequestedAt: model.RequestedAt.UTC(),
		}, nil
	}
	if model.CompletedAt == nil ||
		(status == domainkafkafailure.StatusSucceeded &&
			failureCode != domainkafkafailure.FailureNone) ||
		(status == domainkafkafailure.StatusFailed &&
			failureCode == domainkafkafailure.FailureNone) {
		return nil, domainkafkafailure.ErrReplayPersistence
	}
	return &domainkafkafailure.ReplayResult{
		Coordinate:      coordinate,
		SourceTopic:     model.SourceTopic,
		SourcePartition: model.SourcePartition,
		SourceOffset:    model.SourceOffset,
		ConsumerGroup:   model.ConsumerGroup,
		ActorID:         model.ActorID,
		ReplayID:        model.ReplayID,
		Reason:          reason,
		Status:          status,
		FailureCode:     failureCode,
		RequestedAt:     model.RequestedAt.UTC(),
		CompletedAt:     model.CompletedAt.UTC(),
	}, nil
}

var _ applicationkafkafailure.Ledger = (*Repository)(nil)
