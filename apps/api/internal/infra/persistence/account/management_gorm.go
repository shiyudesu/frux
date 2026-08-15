package infraaccount

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	domainaccount "github.com/shiyudesu/frux/internal/domain/account"
	domainadminaudit "github.com/shiyudesu/frux/internal/domain/adminaudit"
	domainsearch "github.com/shiyudesu/frux/internal/domain/search"
	infrapersistence "github.com/shiyudesu/frux/internal/infra/persistence"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type managedAccountModel struct {
	ID                 int64
	Account            string
	Nickname           string
	AvatarURL          string
	Bio                string
	Gender             int
	Status             int
	AuthVersion        int64
	FollowingCount     int
	FollowerCount      int
	PublicWorkCount    int
	PrivateWorkCount   int
	ReceivedLikeCount  int
	ActiveSessionCount int
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type managementResultModel struct {
	UserID              int64     `json:"user_id"`
	Operation           string    `json:"operation"`
	Status              int       `json:"status"`
	Version             int64     `json:"version"`
	RevokedSessionCount int64     `json:"revoked_session_count"`
	OccurredAt          time.Time `json:"occurred_at"`
}

func EnsureManagedAccountIndexes(db *gorm.DB) error {
	return db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_account_role_status_created_id
		ON account (role, status, created_at DESC, id DESC)
	`).Error
}

func (r *Repository) ListManagedAccounts(
	ctx context.Context,
	filter domainaccount.ManagedAccountQuery,
) ([]*domainaccount.ManagedAccount, error) {
	var models []managedAccountModel
	query := buildManagedAccountListQuery(r.db.WithContext(ctx), filter)
	if err := query.Scan(&models).Error; err != nil {
		return nil, err
	}
	result := make([]*domainaccount.ManagedAccount, 0, len(models))
	for _, model := range models {
		result = append(result, restoreManagedAccount(model))
	}
	return result, nil
}

func buildManagedAccountListQuery(
	db *gorm.DB,
	filter domainaccount.ManagedAccountQuery,
) *gorm.DB {
	query := managedAccountBaseQuery(db).Where("a.role = ?", domainaccount.RoleUser)
	if filter.UserID > 0 {
		query = query.Where("a.id = ?", filter.UserID)
	}
	if filter.Status != 0 {
		query = query.Where("a.status = ?", filter.Status)
	}
	if filter.Search != "" {
		pattern := "%" + domainsearch.EscapeLikeLiteral(filter.Search) + "%"
		query = query.Where(
			"(a.account ILIKE ? ESCAPE '\\' OR a.nickname ILIKE ? ESCAPE '\\')",
			pattern, pattern,
		)
	}
	if filter.Cursor != nil {
		query = query.Where(
			"(a.created_at < ? OR (a.created_at = ? AND a.id < ?))",
			filter.Cursor.CreatedAt.UTC(), filter.Cursor.CreatedAt.UTC(), filter.Cursor.UserID,
		)
	}
	return query.Order("a.created_at DESC").Order("a.id DESC").Limit(filter.Limit)
}

func (r *Repository) GetManagedAccount(
	ctx context.Context,
	userID int64,
) (*domainaccount.ManagedAccount, error) {
	if userID <= 0 {
		return nil, domainaccount.ErrInvalidUserID
	}
	var model managedAccountModel
	err := managedAccountBaseQuery(r.db.WithContext(ctx)).
		Where("a.id = ? AND a.role = ?", userID, domainaccount.RoleUser).
		Take(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, domainaccount.ErrManagedAccountNotFound
	}
	if err != nil {
		return nil, err
	}
	return restoreManagedAccount(model), nil
}

func (r *Repository) CommitManagedAccountOperation(
	ctx context.Context,
	raw domainaccount.AccountManagementCommand,
	buildAudit func(domainaccount.AccountManagementAuditInput) (*domainadminaudit.Fact, error),
) (*domainaccount.AccountManagementResult, error) {
	command, err := domainaccount.NormalizeAccountManagementCommand(raw)
	if err != nil {
		return nil, err
	}
	if r.auditWriter == nil || buildAudit == nil {
		return nil, domainadminaudit.ErrAuditWriteFailed
	}
	fingerprint := command.Fingerprint()
	var result *domainaccount.AccountManagementResult
	var committedFact *domainadminaudit.Fact
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		existing, found, err := findManagementOperation(
			tx, command.ActorID, command.IdempotencyKey,
		)
		if err != nil {
			return err
		}
		if found {
			if existing.Fingerprint != fingerprint {
				return domainaccount.ErrAccountManagementIdempotencyConflict
			}
			result, err = managementResultFromModel(*existing)
			if err != nil {
				return err
			}
			result.Replayed = true
			return nil
		}

		var current struct {
			ID          int64
			Status      int
			Role        string
			AuthVersion int64
		}
		err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Table("account").
			Select("id, status, role, auth_version").
			Where("id = ? AND role = ?", command.UserID, domainaccount.RoleUser).
			Take(&current).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return domainaccount.ErrManagedAccountNotFound
		}
		if err != nil {
			return err
		}
		if current.AuthVersion != command.ExpectedVersion {
			return domainaccount.ErrAccountManagementVersionConflict
		}
		nextStatus, revokeSessions, err := command.Transition(current.Status)
		if err != nil {
			return err
		}
		nextVersion := current.AuthVersion + 1
		update := tx.Model(&UserModel{}).
			Where(
				"id = ? AND role = ? AND auth_version = ?",
				command.UserID, domainaccount.RoleUser, current.AuthVersion,
			).
			Updates(map[string]any{
				"status": nextStatus, "auth_version": nextVersion,
				"updated_at": command.OccurredAt,
			})
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected != 1 {
			return domainaccount.ErrAccountManagementVersionConflict
		}

		var revokedSessionCount int64
		if revokeSessions {
			reason := domainaccount.RefreshRevocationAdminFreeze
			if command.Operation == domainaccount.AccountOperationRevokeSessions {
				reason = domainaccount.RefreshRevocationAdminSignOut
			}
			revoke := tx.Model(&RefreshSessionModel{}).
				Where("user_id = ? AND revoked_at IS NULL", command.UserID).
				Updates(map[string]any{
					"revoked_at": command.OccurredAt, "revocation_reason": reason,
				})
			if revoke.Error != nil {
				return revoke.Error
			}
			revokedSessionCount = revoke.RowsAffected
		}

		auditInput := domainaccount.AccountManagementAuditInput{
			PreviousStatus: current.Status, NewStatus: nextStatus,
			PreviousVersion: current.AuthVersion, NewVersion: nextVersion,
			RevokedSessionCount: revokedSessionCount,
		}
		committedFact, err = buildAudit(auditInput)
		if err != nil {
			return err
		}
		if err := r.auditWriter.AppendInTransaction(ctx, tx, committedFact); err != nil {
			return err
		}
		if command.Operation == domainaccount.AccountOperationFreeze ||
			command.Operation == domainaccount.AccountOperationUnfreeze {
			if err := appendAccountLifecycleNotification(
				tx, command.UserID, command.Operation, command.ReasonCode,
				nextVersion, command.OccurredAt,
			); err != nil {
				return err
			}
		}
		result, err = domainaccount.RestoreAccountManagementResult(
			command.UserID, command.Operation, nextStatus, nextVersion,
			revokedSessionCount, command.OccurredAt,
		)
		if err != nil {
			return err
		}
		encoded, err := json.Marshal(managementResultToModel(result))
		if err != nil {
			return domainaccount.ErrInvalidAccountManagementResult
		}
		operation := ManagementOperationModel{
			ActorID: command.ActorID, IdempotencyKey: command.IdempotencyKey,
			Fingerprint: fingerprint, UserID: command.UserID,
			Operation: string(command.Operation), ResultJSON: string(encoded),
			CreatedAt: command.OccurredAt,
		}
		if err := tx.Create(&operation).Error; err != nil {
			if infrapersistence.IsDuplicatedKeyError(err) {
				return domainaccount.ErrAccountManagementIdempotencyConflict
			}
			return err
		}
		return nil
	})
	if err != nil {
		existing, loadErr := r.loadMatchingManagementOperation(
			ctx, command.ActorID, command.IdempotencyKey, fingerprint,
		)
		if loadErr == nil && existing != nil {
			existing.Replayed = true
			return existing, nil
		}
		return nil, err
	}
	if committedFact != nil {
		r.auditWriter.RecordCommittedWrite(committedFact)
	}
	return result, nil
}

func managedAccountBaseQuery(db *gorm.DB) *gorm.DB {
	return db.Table("account AS a").
		Select(managedAccountSelect()).
		Joins("LEFT JOIN user_relation_stat AS rs ON rs.user_id = a.id").
		Joins("LEFT JOIN user_content_stat AS cs ON cs.user_id = a.id").
		Joins("LEFT JOIN (SELECT user_id, COUNT(*) AS following_count FROM user_follow WHERE status = 1 GROUP BY user_id) AS active_following ON active_following.user_id = a.id").
		Joins("LEFT JOIN (SELECT target_user_id, COUNT(*) AS follower_count FROM user_follow WHERE status = 1 GROUP BY target_user_id) AS active_followers ON active_followers.target_user_id = a.id").
		Joins("LEFT JOIN (SELECT user_id, COUNT(*) AS active_session_count FROM account_refresh_session WHERE revoked_at IS NULL AND expires_at > NOW() GROUP BY user_id) AS active_sessions ON active_sessions.user_id = a.id")
}

func managedAccountSelect() string {
	return strings.Join([]string{
		"a.id", "a.account", "a.nickname", "a.avatar_url", "a.bio", "a.gender",
		"a.status", "a.auth_version", "a.created_at", "a.updated_at",
		"COALESCE(active_following.following_count, rs.following_count, 0) AS following_count",
		"COALESCE(active_followers.follower_count, rs.follower_count, 0) AS follower_count",
		"COALESCE(cs.public_work_count, 0) AS public_work_count",
		"COALESCE(cs.private_work_count, 0) AS private_work_count",
		"COALESCE(cs.received_like_count, 0) AS received_like_count",
		"COALESCE(active_sessions.active_session_count, 0) AS active_session_count",
	}, ", ")
}

func restoreManagedAccount(model managedAccountModel) *domainaccount.ManagedAccount {
	return domainaccount.RestoreManagedAccount(
		model.ID, model.Account, model.Nickname, model.AvatarURL, model.Bio,
		model.Gender, model.Status, model.AuthVersion,
		model.FollowingCount, model.FollowerCount, model.PublicWorkCount,
		model.PrivateWorkCount, model.ReceivedLikeCount, model.ActiveSessionCount,
		model.CreatedAt, model.UpdatedAt,
	)
}

func findManagementOperation(
	tx *gorm.DB,
	actorID int64,
	idempotencyKey string,
) (*ManagementOperationModel, bool, error) {
	var operation ManagementOperationModel
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("actor_id = ? AND idempotency_key = ?", actorID, idempotencyKey).
		Take(&operation).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return &operation, true, nil
}

func (r *Repository) loadMatchingManagementOperation(
	ctx context.Context,
	actorID int64,
	idempotencyKey, fingerprint string,
) (*domainaccount.AccountManagementResult, error) {
	var operation ManagementOperationModel
	err := r.db.WithContext(ctx).
		Where("actor_id = ? AND idempotency_key = ?", actorID, idempotencyKey).
		Take(&operation).Error
	if err != nil {
		return nil, err
	}
	if operation.Fingerprint != fingerprint {
		return nil, domainaccount.ErrAccountManagementIdempotencyConflict
	}
	return managementResultFromModel(operation)
}

func managementResultToModel(result *domainaccount.AccountManagementResult) managementResultModel {
	return managementResultModel{
		UserID: result.UserID, Operation: string(result.Operation),
		Status: result.Status, Version: result.Version,
		RevokedSessionCount: result.RevokedSessionCount, OccurredAt: result.OccurredAt,
	}
}

func managementResultFromModel(
	operation ManagementOperationModel,
) (*domainaccount.AccountManagementResult, error) {
	var stored managementResultModel
	if err := json.Unmarshal([]byte(operation.ResultJSON), &stored); err != nil {
		return nil, domainaccount.ErrInvalidAccountManagementResult
	}
	if stored.UserID != operation.UserID || stored.Operation != operation.Operation {
		return nil, domainaccount.ErrInvalidAccountManagementResult
	}
	return domainaccount.RestoreAccountManagementResult(
		stored.UserID, domainaccount.AccountManagementOperation(stored.Operation),
		stored.Status, stored.Version, stored.RevokedSessionCount, stored.OccurredAt,
	)
}

var _ domainaccount.ManagedAccountReader = (*Repository)(nil)
