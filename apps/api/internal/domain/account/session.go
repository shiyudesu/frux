package domainaccount

import (
	"crypto/subtle"
	"strings"
	"time"
)

const (
	RefreshRevocationLogout         = "logout"
	RefreshRevocationPasswordChange = "password_change"
	RefreshRevocationReplay         = "replay"
	RefreshRevocationExpired        = "expired"
	RefreshRevocationAdminFreeze    = "admin_freeze"
	RefreshRevocationAdminSignOut   = "admin_sign_out"
)

type RefreshSession struct {
	ID                    string
	FamilyID              string
	UserID                int64
	SecretHash            string
	PreviousSecretHash    string
	PreviousSecretValidTo *time.Time
	AuthVersion           int64
	ExpiresAt             time.Time
	LastUsedAt            time.Time
	RevokedAt             *time.Time
	RevocationReason      string
	ReplacedBySessionID   string
}

type RotateRefreshSessionInput struct {
	SessionID     string
	SecretHash    string
	NewSecretHash string
	RotatedAt     time.Time
	PreviousGrace time.Duration
}

type RotateRefreshSessionResult struct {
	Session     *RefreshSession
	Account     *User
	Superseded  bool
	ReplayFound bool
}

type ReplacePasswordAndSessionsInput struct {
	Change             PasswordChange
	ReplacementSession *RefreshSession
	ChangedAt          time.Time
}

func NewRefreshSession(id, familyID string, userID int64, secretHash string, authVersion int64, now, expiresAt time.Time) (*RefreshSession, error) {
	id = strings.TrimSpace(id)
	familyID = strings.TrimSpace(familyID)
	secretHash = strings.TrimSpace(secretHash)
	if id == "" {
		return nil, ErrInvalidRefreshSessionID
	}
	if familyID == "" {
		return nil, ErrInvalidRefreshFamilyID
	}
	if userID <= 0 {
		return nil, ErrInvalidUserID
	}
	if secretHash == "" {
		return nil, ErrInvalidRefreshSecretHash
	}
	if authVersion <= 0 {
		return nil, ErrInvalidAuthVersion
	}
	if now.IsZero() || !expiresAt.After(now) {
		return nil, ErrRefreshSessionExpired
	}
	return &RefreshSession{
		ID:          id,
		FamilyID:    familyID,
		UserID:      userID,
		SecretHash:  secretHash,
		AuthVersion: authVersion,
		ExpiresAt:   expiresAt,
		LastUsedAt:  now,
	}, nil
}

func RestoreRefreshSession(
	id, familyID string,
	userID int64,
	secretHash, previousSecretHash string,
	previousSecretValidTo *time.Time,
	authVersion int64,
	expiresAt, lastUsedAt time.Time,
	revokedAt *time.Time,
	revocationReason, replacedBySessionID string,
) *RefreshSession {
	return &RefreshSession{
		ID:                    strings.TrimSpace(id),
		FamilyID:              strings.TrimSpace(familyID),
		UserID:                userID,
		SecretHash:            strings.TrimSpace(secretHash),
		PreviousSecretHash:    strings.TrimSpace(previousSecretHash),
		PreviousSecretValidTo: previousSecretValidTo,
		AuthVersion:           authVersion,
		ExpiresAt:             expiresAt,
		LastUsedAt:            lastUsedAt,
		RevokedAt:             revokedAt,
		RevocationReason:      strings.TrimSpace(revocationReason),
		ReplacedBySessionID:   strings.TrimSpace(replacedBySessionID),
	}
}

func (s *RefreshSession) Active(now time.Time) bool {
	return s != nil && s.ID != "" && s.UserID > 0 && s.AuthVersion > 0 &&
		s.RevokedAt == nil && now.Before(s.ExpiresAt)
}

func (s *RefreshSession) MatchesCurrent(hash string) bool {
	return secureHashEqual(s.SecretHash, hash)
}

func (s *RefreshSession) MatchesPreviousWithinGrace(hash string, now time.Time) bool {
	return s != nil && s.PreviousSecretValidTo != nil && !now.After(*s.PreviousSecretValidTo) &&
		secureHashEqual(s.PreviousSecretHash, hash)
}

func (s *RefreshSession) MatchesPrevious(hash string) bool {
	return secureHashEqual(s.PreviousSecretHash, hash)
}

func secureHashEqual(left, right string) bool {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	if left == "" || right == "" || len(left) != len(right) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}
