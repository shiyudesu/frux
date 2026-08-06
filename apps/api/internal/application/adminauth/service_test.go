package applicationadminauth

import (
	"context"
	"errors"
	"testing"
	"time"

	domainaccount "github.com/shiyudesu/frux/internal/domain/account"
)

type adminAuthRepositoryStub struct {
	users map[string]*domainaccount.User
	err   error
}

func (r adminAuthRepositoryStub) FindByAccount(_ context.Context, account string) (*domainaccount.User, error) {
	if r.err != nil {
		return nil, r.err
	}
	user := r.users[account]
	if user == nil {
		return nil, domainaccount.ErrUserNotFound
	}
	return user, nil
}

type adminAuthSignerStub struct {
	token string
	err   error
}

func (s adminAuthSignerStub) SignAdminAccessToken(int64, string) (string, error) {
	return s.token, s.err
}

func (adminAuthSignerStub) AdminAccessTTL() time.Duration {
	return 30 * time.Minute
}

func TestAdminAuthenticationEligibilityAndGenericFailure(t *testing.T) {
	reviewer := newAdminAuthUser(t, 1, "reviewer", domainaccount.StatusNormal, domainaccount.RoleReviewer)
	user := newAdminAuthUser(t, 2, "user", domainaccount.StatusNormal, domainaccount.RoleUser)
	disabled := newAdminAuthUser(t, 3, "disabled", 2, domainaccount.RoleAdmin)
	service := New(adminAuthRepositoryStub{users: map[string]*domainaccount.User{
		reviewer.Account: reviewer, user.Account: user, disabled.Account: disabled,
	}}, adminAuthSignerStub{token: "admin-token"})

	result, err := service.Login(context.Background(), " REVIEWER ", "Password123!")
	if err != nil || result.AccessToken != "admin-token" ||
		result.Principal.UserID != 1 || len(result.Principal.Permissions()) == 0 {
		t.Fatalf("valid login = %#v err=%v", result, err)
	}
	for _, input := range []struct {
		account  string
		password string
	}{
		{"missing", "Password123!"},
		{"reviewer", "wrong"},
		{"user", "Password123!"},
		{"disabled", "Password123!"},
		{"", "Password123!"},
	} {
		if _, err := service.Login(context.Background(), input.account, input.password); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("login %q error = %v", input.account, err)
		}
	}
}

func TestAdminAuthenticationSurfacesInfrastructureFailures(t *testing.T) {
	service := New(adminAuthRepositoryStub{err: errors.New("database")}, adminAuthSignerStub{})
	if _, err := service.Login(context.Background(), "reviewer", "Password123!"); !errors.Is(err, ErrLoadAccountFailed) {
		t.Fatalf("load failure = %v", err)
	}
	reviewer := newAdminAuthUser(t, 1, "reviewer", domainaccount.StatusNormal, domainaccount.RoleReviewer)
	service = New(
		adminAuthRepositoryStub{users: map[string]*domainaccount.User{reviewer.Account: reviewer}},
		adminAuthSignerStub{err: errors.New("sign")},
	)
	if _, err := service.Login(context.Background(), "reviewer", "Password123!"); !errors.Is(err, ErrSignTokenFailed) {
		t.Fatalf("sign failure = %v", err)
	}
}

func newAdminAuthUser(t *testing.T, id int64, account string, status int, role string) *domainaccount.User {
	t.Helper()
	user, err := domainaccount.New(account, "Password123!", account)
	if err != nil {
		t.Fatal(err)
	}
	user.ID = id
	user.Status = status
	user.Role = role
	return user
}
