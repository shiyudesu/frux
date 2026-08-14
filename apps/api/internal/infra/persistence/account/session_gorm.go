package infraaccount

import (
	"context"
	"errors"
	"strings"
	"time"

	domainaccount "github.com/shiyudesu/frux/internal/domain/account"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (r *Repository) CreateRefreshSession(ctx context.Context, session *domainaccount.RefreshSession) error {
	if session == nil {
		return domainaccount.ErrInvalidRefreshSession
	}
	return r.db.WithContext(ctx).Create(refreshSessionModelFromDomain(session)).Error
}

func (r *Repository) RotateRefreshSession(
	ctx context.Context,
	input domainaccount.RotateRefreshSessionInput,
) (*domainaccount.RotateRefreshSessionResult, error) {
	input.SessionID = strings.TrimSpace(input.SessionID)
	input.SecretHash = strings.TrimSpace(input.SecretHash)
	input.NewSecretHash = strings.TrimSpace(input.NewSecretHash)
	if input.SessionID == "" {
		return nil, domainaccount.ErrInvalidRefreshSessionID
	}
	if input.SecretHash == "" || input.NewSecretHash == "" {
		return nil, domainaccount.ErrInvalidRefreshSecretHash
	}
	if input.RotatedAt.IsZero() || input.PreviousGrace <= 0 {
		return nil, domainaccount.ErrInvalidRefreshSession
	}

	var result *domainaccount.RotateRefreshSessionResult
	var outcomeErr error
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var identity struct {
			UserID int64
		}
		err := tx.Model(&RefreshSessionModel{}).
			Select("user_id").
			Where("id = ?", input.SessionID).
			Take(&identity).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			outcomeErr = domainaccount.ErrInvalidRefreshSession
			return nil
		}
		if err != nil {
			return err
		}

		var account UserModel
		err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", identity.UserID).
			Take(&account).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			outcomeErr = domainaccount.ErrInvalidRefreshSession
			return nil
		}
		if err != nil {
			return err
		}

		var model RefreshSessionModel
		err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", input.SessionID).
			Take(&model).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			outcomeErr = domainaccount.ErrInvalidRefreshSession
			return nil
		}
		if err != nil {
			return err
		}
		session := restoreRefreshSession(model)
		if session.RevokedAt != nil {
			outcomeErr = domainaccount.ErrRefreshSessionRevoked
			return nil
		}
		if !input.RotatedAt.Before(session.ExpiresAt) {
			if err := revokeRefreshModel(
				tx, &model, domainaccount.RefreshRevocationExpired, input.RotatedAt, "",
			); err != nil {
				return err
			}
			outcomeErr = domainaccount.ErrRefreshSessionExpired
			return nil
		}
		if account.ID != session.UserID {
			outcomeErr = domainaccount.ErrInvalidRefreshSession
			return nil
		}
		if account.Status != domainaccount.StatusNormal ||
			account.AuthVersion != session.AuthVersion {
			if err := revokeRefreshModel(
				tx, &model, domainaccount.RefreshRevocationPasswordChange,
				input.RotatedAt, "",
			); err != nil {
				return err
			}
			outcomeErr = domainaccount.ErrRefreshSessionRevoked
			return nil
		}
		if session.MatchesCurrent(input.SecretHash) {
			previousValidTo := input.RotatedAt.Add(input.PreviousGrace)
			updates := map[string]any{
				"previous_secret_hash":     session.SecretHash,
				"previous_secret_valid_to": previousValidTo,
				"secret_hash":              input.NewSecretHash,
				"last_used_at":             input.RotatedAt,
			}
			update := tx.Model(&RefreshSessionModel{}).
				Where(
					"id = ? AND secret_hash = ? AND revoked_at IS NULL",
					model.ID, session.SecretHash,
				).
				Updates(updates)
			if update.Error != nil {
				return update.Error
			}
			if update.RowsAffected != 1 {
				outcomeErr = domainaccount.ErrRefreshSessionSuperseded
				return nil
			}
			model.PreviousSecretHash = session.SecretHash
			model.PreviousSecretValidTo = &previousValidTo
			model.SecretHash = input.NewSecretHash
			model.LastUsedAt = input.RotatedAt
			result = &domainaccount.RotateRefreshSessionResult{
				Session: restoreRefreshSession(model),
				Account: domainaccount.RestoreUserWithDashboardAuthVersion(
					account.ID, account.Account, account.Password, account.Nickname,
					account.AvatarURL, account.Bio, account.Gender, account.Status,
					account.Role, account.AuthVersion, 0, 0, 0, 0, 0,
				),
			}
			return nil
		}
		if session.MatchesPreviousWithinGrace(input.SecretHash, input.RotatedAt) {
			result = &domainaccount.RotateRefreshSessionResult{
				Session: session, Superseded: true,
			}
			return nil
		}
		if session.MatchesPrevious(input.SecretHash) {
			if err := tx.Model(&RefreshSessionModel{}).
				Where("family_id = ? AND revoked_at IS NULL", session.FamilyID).
				Updates(map[string]any{
					"revoked_at":        input.RotatedAt,
					"revocation_reason": domainaccount.RefreshRevocationReplay,
				}).Error; err != nil {
				return err
			}
			result = &domainaccount.RotateRefreshSessionResult{
				Session: session, ReplayFound: true,
			}
			return nil
		}
		if err := tx.Model(&RefreshSessionModel{}).
			Where("family_id = ? AND revoked_at IS NULL", session.FamilyID).
			Updates(map[string]any{
				"revoked_at":        input.RotatedAt,
				"revocation_reason": domainaccount.RefreshRevocationReplay,
			}).Error; err != nil {
			return err
		}
		result = &domainaccount.RotateRefreshSessionResult{
			Session: session, ReplayFound: true,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if outcomeErr != nil {
		return nil, outcomeErr
	}
	return result, nil
}

func (r *Repository) RevokeRefreshSession(
	ctx context.Context,
	sessionID, secretHash, reason string,
	revokedAt time.Time,
) error {
	sessionID = strings.TrimSpace(sessionID)
	secretHash = strings.TrimSpace(secretHash)
	if sessionID == "" || secretHash == "" || revokedAt.IsZero() {
		return nil
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var model RefreshSessionModel
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", sessionID).
			Take(&model).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		session := restoreRefreshSession(model)
		if session.RevokedAt != nil ||
			(!session.MatchesCurrent(secretHash) && !session.MatchesPrevious(secretHash)) {
			return nil
		}
		return revokeRefreshModel(tx, &model, reason, revokedAt, "")
	})
}

func (r *Repository) ReplacePasswordAndSessions(
	ctx context.Context,
	input domainaccount.ReplacePasswordAndSessionsInput,
) error {
	if input.ReplacementSession == nil || input.ChangedAt.IsZero() {
		return domainaccount.ErrInvalidRefreshSession
	}
	change := input.Change
	if change.UserID <= 0 || change.ExpectedPassword == "" ||
		change.NewPassword == "" || change.CurrentAuthVersion <= 0 ||
		change.NextAuthVersion != change.CurrentAuthVersion+1 {
		return domainaccount.ErrCredentialChanged
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		update := tx.Model(&UserModel{}).
			Where(
				"id = ? AND password = ? AND auth_version = ?",
				change.UserID, change.ExpectedPassword, change.CurrentAuthVersion,
			).
			Updates(map[string]any{
				"password":     change.NewPassword,
				"auth_version": change.NextAuthVersion,
			})
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected == 0 {
			return domainaccount.ErrCredentialChanged
		}
		if err := tx.Model(&RefreshSessionModel{}).
			Where("user_id = ? AND revoked_at IS NULL", change.UserID).
			Updates(map[string]any{
				"revoked_at":             input.ChangedAt,
				"revocation_reason":      domainaccount.RefreshRevocationPasswordChange,
				"replaced_by_session_id": input.ReplacementSession.ID,
			}).Error; err != nil {
			return err
		}
		return tx.Create(refreshSessionModelFromDomain(input.ReplacementSession)).Error
	})
}

func (r *Repository) DeleteExpiredRefreshSessions(
	ctx context.Context,
	now, revokedBefore time.Time,
	limit int,
) (int64, error) {
	if now.IsZero() || revokedBefore.IsZero() || limit <= 0 {
		return 0, nil
	}
	result := r.db.WithContext(ctx).Exec(`
		DELETE FROM account_refresh_session
		WHERE id IN (
			SELECT id
			FROM account_refresh_session
			WHERE expires_at <= ? OR (revoked_at IS NOT NULL AND revoked_at <= ?)
			ORDER BY expires_at ASC, id ASC
			LIMIT ?
		)
	`, now, revokedBefore, limit)
	return result.RowsAffected, result.Error
}

func refreshSessionModelFromDomain(
	session *domainaccount.RefreshSession,
) *RefreshSessionModel {
	return &RefreshSessionModel{
		ID:                    session.ID,
		FamilyID:              session.FamilyID,
		UserID:                session.UserID,
		SecretHash:            session.SecretHash,
		PreviousSecretHash:    session.PreviousSecretHash,
		PreviousSecretValidTo: session.PreviousSecretValidTo,
		AuthVersion:           session.AuthVersion,
		ExpiresAt:             session.ExpiresAt,
		LastUsedAt:            session.LastUsedAt,
		RevokedAt:             session.RevokedAt,
		RevocationReason:      session.RevocationReason,
		ReplacedBySessionID:   session.ReplacedBySessionID,
	}
}

func restoreRefreshSession(model RefreshSessionModel) *domainaccount.RefreshSession {
	return domainaccount.RestoreRefreshSession(
		model.ID, model.FamilyID, model.UserID, model.SecretHash,
		model.PreviousSecretHash, model.PreviousSecretValidTo, model.AuthVersion,
		model.ExpiresAt, model.LastUsedAt, model.RevokedAt, model.RevocationReason,
		model.ReplacedBySessionID,
	)
}

func revokeRefreshModel(
	tx *gorm.DB,
	model *RefreshSessionModel,
	reason string,
	revokedAt time.Time,
	replacedBy string,
) error {
	if model.RevokedAt != nil {
		return nil
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = domainaccount.RefreshRevocationLogout
	}
	return tx.Model(&RefreshSessionModel{}).
		Where("id = ? AND revoked_at IS NULL", model.ID).
		Updates(map[string]any{
			"revoked_at":             revokedAt,
			"revocation_reason":      reason,
			"replaced_by_session_id": replacedBy,
		}).Error
}
