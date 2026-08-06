package applicationadminauth

import (
	"context"
	"errors"
	"time"

	domainaccount "github.com/shiyudesu/frux/internal/domain/account"

	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidCredentials = errors.New("invalid admin credentials")
	ErrLoadAccountFailed  = errors.New("failed to load admin account")
	ErrSignTokenFailed    = errors.New("failed to sign admin access token")
)

var dummyPasswordHash = []byte("$2b$10$4awlyb.eTaP9IAqTyDByje8A/QGYJCP8kYWByewITZIuU8bUZ1H3.")

type Repository interface {
	FindByAccount(ctx context.Context, account string) (*domainaccount.User, error)
}

type TokenSigner interface {
	SignAdminAccessToken(userID int64, role string) (string, error)
	AdminAccessTTL() time.Duration
}

type Service struct {
	repository Repository
	signer     TokenSigner
}

type LoginResult struct {
	AccessToken      string
	TokenType        string
	ExpiresInSeconds int64
	Principal        *domainaccount.AdminPrincipal
}

func New(repository Repository, signer TokenSigner) *Service {
	return &Service{repository: repository, signer: signer}
}

func (s *Service) Login(ctx context.Context, account, password string) (*LoginResult, error) {
	if s == nil || s.repository == nil || s.signer == nil {
		return nil, ErrLoadAccountFailed
	}
	account = domainaccount.NormalizeAccount(account)
	if account == "" {
		consumeDummyPassword(password)
		return nil, ErrInvalidCredentials
	}
	user, err := s.repository.FindByAccount(ctx, account)
	if err != nil {
		if errors.Is(err, domainaccount.ErrUserNotFound) {
			consumeDummyPassword(password)
			return nil, ErrInvalidCredentials
		}
		return nil, ErrLoadAccountFailed
	}
	if err := user.Authenticate(password); err != nil {
		return nil, ErrInvalidCredentials
	}
	principal := domainaccount.RestoreAdminPrincipal(user.ID, user.Status, user.Role)
	if !principal.Active() || len(principal.Permissions()) == 0 {
		return nil, ErrInvalidCredentials
	}
	token, err := s.signer.SignAdminAccessToken(user.ID, user.Role)
	if err != nil {
		return nil, ErrSignTokenFailed
	}
	return &LoginResult{
		AccessToken: token, TokenType: "Bearer",
		ExpiresInSeconds: int64(s.signer.AdminAccessTTL().Seconds()),
		Principal:        principal,
	}, nil
}

func consumeDummyPassword(password string) {
	_ = bcrypt.CompareHashAndPassword(dummyPasswordHash, []byte(password))
}
