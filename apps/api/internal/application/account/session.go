package applicationaccount

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	domainaccount "github.com/shiyudesu/frux/internal/domain/account"

	"golang.org/x/crypto/bcrypt"
)

var dummyPasswordHash = []byte("$2b$10$4awlyb.eTaP9IAqTyDByje8A/QGYJCP8kYWByewITZIuU8bUZ1H3.")

type SessionTokenGenerator interface {
	NewID() (string, error)
	NewSecret() (string, error)
	HashSecret(secret string) string
}

type CryptoSessionTokenGenerator struct{}

func (CryptoSessionTokenGenerator) NewID() (string, error) {
	return randomHex(16)
}

func (CryptoSessionTokenGenerator) NewSecret() (string, error) {
	return randomHex(32)
}

func (CryptoSessionTokenGenerator) HashSecret(secret string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(secret)))
	return hex.EncodeToString(sum[:])
}

func randomHex(size int) (string, error) {
	buffer := make([]byte, size)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(buffer), nil
}

func consumeDummyPassword(password string) {
	_ = bcrypt.CompareHashAndPassword(dummyPasswordHash, []byte(domainaccount.NormalizePassword(password)))
}

func (s *Service) createLoginSession(
	ctx context.Context,
	user *domainaccount.User,
) (*LoginResult, error) {
	if s.sessions == nil || s.tokens == nil || s.signer == nil {
		return nil, ErrCreateRefreshSessionFailed
	}
	session, credential, err := s.newRefreshSession(user.ID, user.AuthVersion)
	if err != nil {
		return nil, ErrCreateRefreshSessionFailed
	}
	if err := s.sessions.CreateRefreshSession(ctx, session); err != nil {
		return nil, ErrCreateRefreshSessionFailed
	}
	result, err := s.loginResult(user.ID, session.ID, user.AuthVersion, credential, session.ExpiresAt)
	if err != nil {
		_ = s.sessions.RevokeRefreshSession(
			context.WithoutCancel(ctx), session.ID, session.SecretHash,
			domainaccount.RefreshRevocationLogout, s.now(),
		)
		return nil, err
	}
	return result, nil
}

func (s *Service) Refresh(
	ctx context.Context,
	rawCredential string,
) (*LoginResult, error) {
	if s.sessions == nil || s.tokens == nil || s.signer == nil {
		return nil, ErrRotateRefreshSessionFailed
	}
	sessionID, secret, ok := ParseRefreshCredential(rawCredential)
	if !ok {
		return nil, domainaccount.ErrInvalidRefreshSession
	}
	newSecret, err := s.tokens.NewSecret()
	if err != nil {
		return nil, ErrRotateRefreshSessionFailed
	}
	now := s.now()
	rotated, err := s.sessions.RotateRefreshSession(ctx, domainaccount.RotateRefreshSessionInput{
		SessionID:     sessionID,
		SecretHash:    s.tokens.HashSecret(secret),
		NewSecretHash: s.tokens.HashSecret(newSecret),
		RotatedAt:     now,
		PreviousGrace: s.previousGrace,
	})
	if err != nil {
		if errors.Is(err, domainaccount.ErrInvalidRefreshSession) ||
			errors.Is(err, domainaccount.ErrRefreshSessionExpired) ||
			errors.Is(err, domainaccount.ErrRefreshSessionRevoked) {
			return nil, err
		}
		if errors.Is(err, domainaccount.ErrRefreshSessionSuperseded) {
			return nil, domainaccount.ErrRefreshSessionSuperseded
		}
		return nil, ErrRotateRefreshSessionFailed
	}
	if rotated == nil || rotated.Session == nil {
		return nil, ErrRotateRefreshSessionFailed
	}
	if rotated.Superseded {
		return nil, domainaccount.ErrRefreshSessionSuperseded
	}
	if rotated.ReplayFound {
		return nil, domainaccount.ErrRefreshSessionReplayed
	}
	if rotated.Account == nil {
		return nil, ErrRotateRefreshSessionFailed
	}
	credential := BuildRefreshCredential(rotated.Session.ID, newSecret)
	return s.loginResult(
		rotated.Account.ID,
		rotated.Session.ID,
		rotated.Account.AuthVersion,
		credential,
		rotated.Session.ExpiresAt,
	)
}

func (s *Service) Logout(ctx context.Context, rawCredential string) error {
	if s.sessions == nil || s.tokens == nil {
		return nil
	}
	sessionID, secret, ok := ParseRefreshCredential(rawCredential)
	if !ok {
		return nil
	}
	if err := s.sessions.RevokeRefreshSession(
		ctx, sessionID, s.tokens.HashSecret(secret),
		domainaccount.RefreshRevocationLogout, s.now(),
	); err != nil {
		return ErrRevokeRefreshSessionFailed
	}
	return nil
}

func (s *Service) ChangePassword(
	ctx context.Context,
	userID int64,
	currentPassword, newPassword string,
) (*LoginResult, error) {
	if userID <= 0 {
		return nil, domainaccount.ErrInvalidUserID
	}
	if s.sessions == nil || s.tokens == nil || s.signer == nil {
		return nil, ErrChangePasswordFailed
	}
	user, err := s.repo.FindByID(ctx, userID)
	if err != nil {
		if errors.Is(err, domainaccount.ErrUserNotFound) {
			return nil, domainaccount.ErrUserNotFound
		}
		return nil, ErrLoadAccountFailed
	}
	if user.Status != domainaccount.StatusNormal {
		return nil, domainaccount.ErrInvalidCredentials
	}
	change, err := user.PreparePasswordChange(currentPassword, newPassword)
	if err != nil {
		return nil, err
	}
	replacement, credential, err := s.newRefreshSession(userID, change.NextAuthVersion)
	if err != nil {
		return nil, ErrChangePasswordFailed
	}
	result, err := s.loginResult(
		userID, replacement.ID, change.NextAuthVersion, credential,
		replacement.ExpiresAt,
	)
	if err != nil {
		return nil, err
	}
	changedAt := s.now()
	if err := s.sessions.ReplacePasswordAndSessions(
		ctx,
		domainaccount.ReplacePasswordAndSessionsInput{
			Change:             *change,
			ReplacementSession: replacement,
			ChangedAt:          changedAt,
		},
	); err != nil {
		if errors.Is(err, domainaccount.ErrCredentialChanged) {
			return nil, domainaccount.ErrCredentialChanged
		}
		return nil, ErrChangePasswordFailed
	}
	return result, nil
}

func (s *Service) CleanupRefreshSessions(
	ctx context.Context,
	revokedRetention time.Duration,
	limit int,
) (int64, error) {
	if s.sessions == nil {
		return 0, nil
	}
	now := s.now()
	deleted, err := s.sessions.DeleteExpiredRefreshSessions(
		ctx, now, now.Add(-revokedRetention), limit,
	)
	if err != nil {
		return 0, ErrCleanupRefreshSessionsFailed
	}
	return deleted, nil
}

func (s *Service) newRefreshSession(
	userID, authVersion int64,
) (*domainaccount.RefreshSession, string, error) {
	sessionID, err := s.tokens.NewID()
	if err != nil {
		return nil, "", err
	}
	familyID, err := s.tokens.NewID()
	if err != nil {
		return nil, "", err
	}
	secret, err := s.tokens.NewSecret()
	if err != nil {
		return nil, "", err
	}
	now := s.now()
	session, err := domainaccount.NewRefreshSession(
		sessionID, familyID, userID, s.tokens.HashSecret(secret), authVersion,
		now, now.Add(s.refreshTTL),
	)
	if err != nil {
		return nil, "", err
	}
	return session, BuildRefreshCredential(sessionID, secret), nil
}

func (s *Service) loginResult(
	userID int64,
	sessionID string,
	authVersion int64,
	refreshCredential string,
	refreshExpiresAt time.Time,
) (*LoginResult, error) {
	accessToken, err := s.signer.SignConsumerAccessToken(
		userID, sessionID, authVersion,
	)
	if err != nil {
		return nil, ErrSignAccessTokenFailed
	}
	return &LoginResult{
		AccessToken:       accessToken,
		TokenType:         "Bearer",
		ExpiresInSeconds:  int64(s.signer.AccessTTL().Seconds()),
		RefreshCredential: refreshCredential,
		RefreshExpiresAt:  refreshExpiresAt,
	}, nil
}

func BuildRefreshCredential(sessionID, secret string) string {
	sessionID = strings.TrimSpace(sessionID)
	secret = strings.TrimSpace(secret)
	if sessionID == "" || secret == "" {
		return ""
	}
	return sessionID + "." + secret
}

func ParseRefreshCredential(raw string) (string, string, bool) {
	parts := strings.Split(strings.TrimSpace(raw), ".")
	if len(parts) != 2 {
		return "", "", false
	}
	sessionID := strings.TrimSpace(parts[0])
	secret := strings.TrimSpace(parts[1])
	if sessionID == "" || secret == "" {
		return "", "", false
	}
	return sessionID, secret, true
}
