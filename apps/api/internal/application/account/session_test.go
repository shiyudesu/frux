package applicationaccount

import (
	"context"
	"errors"
	"testing"
	"time"

	domainaccount "github.com/shiyudesu/frux/internal/domain/account"
)

type accountSessionRepositoryStub struct {
	user          *domainaccount.User
	session       *domainaccount.RefreshSession
	rotateResult  *domainaccount.RotateRefreshSessionResult
	rotateErr     error
	replaceInput  *domainaccount.ReplacePasswordAndSessionsInput
	revokeCalls   int
	cleanupCount  int64
	cleanupCounts []int64
	cleanupCalls  int
	cleanupErr    error
}

func (r *accountSessionRepositoryStub) Save(context.Context, *domainaccount.User) error {
	return nil
}

func (r *accountSessionRepositoryStub) FindByAccount(context.Context, string) (*domainaccount.User, error) {
	if r.user == nil {
		return nil, domainaccount.ErrUserNotFound
	}
	copy := *r.user
	return &copy, nil
}

func (r *accountSessionRepositoryStub) FindByID(context.Context, int64) (*domainaccount.User, error) {
	if r.user == nil {
		return nil, domainaccount.ErrUserNotFound
	}
	copy := *r.user
	return &copy, nil
}

func (*accountSessionRepositoryStub) UpdateProfile(context.Context, domainaccount.ProfileUpdate) error {
	return nil
}

func (*accountSessionRepositoryStub) UpdateProfileAndSetting(
	context.Context,
	*domainaccount.ProfileUpdate,
	*domainaccount.ProfileSettingUpdate,
) error {
	return nil
}

func (r *accountSessionRepositoryStub) CreateRefreshSession(
	_ context.Context,
	session *domainaccount.RefreshSession,
) error {
	copy := *session
	r.session = &copy
	return nil
}

func (r *accountSessionRepositoryStub) RotateRefreshSession(
	context.Context,
	domainaccount.RotateRefreshSessionInput,
) (*domainaccount.RotateRefreshSessionResult, error) {
	return r.rotateResult, r.rotateErr
}

func (r *accountSessionRepositoryStub) RevokeRefreshSession(
	context.Context, string, string, string, time.Time,
) error {
	r.revokeCalls++
	return nil
}

func (r *accountSessionRepositoryStub) ReplacePasswordAndSessions(
	_ context.Context,
	input domainaccount.ReplacePasswordAndSessionsInput,
) error {
	copy := input
	r.replaceInput = &copy
	return nil
}

func (r *accountSessionRepositoryStub) DeleteExpiredRefreshSessions(
	context.Context, time.Time, time.Time, int,
) (int64, error) {
	r.cleanupCalls++
	if len(r.cleanupCounts) > 0 {
		value := r.cleanupCounts[0]
		r.cleanupCounts = r.cleanupCounts[1:]
		return value, r.cleanupErr
	}
	return r.cleanupCount, r.cleanupErr
}

type accountTokenSignerStub struct {
	err error
}

func (s accountTokenSignerStub) SignConsumerAccessToken(
	_ int64, sessionID string, version int64,
) (string, error) {
	if s.err != nil {
		return "", s.err
	}
	return sessionID + "-v" + string(rune('0'+version)), nil
}

func (accountTokenSignerStub) AccessTTL() time.Duration {
	return 5 * time.Minute
}

type deterministicSessionTokens struct {
	ids     []string
	secrets []string
}

func (g *deterministicSessionTokens) NewID() (string, error) {
	value := g.ids[0]
	g.ids = g.ids[1:]
	return value, nil
}

func (g *deterministicSessionTokens) NewSecret() (string, error) {
	value := g.secrets[0]
	g.secrets = g.secrets[1:]
	return value, nil
}

func (*deterministicSessionTokens) HashSecret(secret string) string {
	return "hash:" + secret
}

func TestLoginCreatesHashedRefreshSessionAndRejectsInactiveAccount(t *testing.T) {
	user, err := domainaccount.New("alice", "Password123!", "Alice")
	if err != nil {
		t.Fatal(err)
	}
	user.ID = 7
	repo := &accountSessionRepositoryStub{user: user}
	now := time.Date(2026, 8, 13, 4, 0, 0, 0, time.UTC)
	service := New(
		repo,
		accountTokenSignerStub{},
		WithRefreshSessionRepository(repo),
		WithSessionTokenGenerator(&deterministicSessionTokens{
			ids: []string{"session", "family"}, secrets: []string{"secret"},
		}),
		WithClock(func() time.Time { return now }),
	)
	result, err := service.Login(context.Background(), "ALICE", "Password123!")
	if err != nil {
		t.Fatal(err)
	}
	if result.RefreshCredential != "session.secret" ||
		repo.session.SecretHash != "hash:secret" ||
		repo.session.SecretHash == result.RefreshCredential {
		t.Fatalf("result=%+v session=%+v", result, repo.session)
	}

	repo.user.Status = 2
	if _, err := service.Login(context.Background(), "alice", "Password123!"); !errors.Is(err, domainaccount.ErrInvalidCredentials) {
		t.Fatalf("inactive login error = %v", err)
	}
	repo.user = nil
	if _, err := service.Login(context.Background(), "missing", "Password123!"); !errors.Is(err, domainaccount.ErrInvalidCredentials) {
		t.Fatalf("missing login error = %v", err)
	}
}

func TestRefreshClassifiesSupersededAndReplay(t *testing.T) {
	repo := &accountSessionRepositoryStub{
		rotateResult: &domainaccount.RotateRefreshSessionResult{
			Session:    &domainaccount.RefreshSession{ID: "session"},
			Superseded: true,
		},
	}
	service := New(
		repo,
		accountTokenSignerStub{},
		WithRefreshSessionRepository(repo),
		WithSessionTokenGenerator(&deterministicSessionTokens{
			secrets: []string{"next"},
		}),
	)
	if _, err := service.Refresh(context.Background(), "session.secret"); !errors.Is(err, domainaccount.ErrRefreshSessionSuperseded) {
		t.Fatalf("superseded error = %v", err)
	}
	repo.rotateResult = &domainaccount.RotateRefreshSessionResult{
		Session:     &domainaccount.RefreshSession{ID: "session"},
		ReplayFound: true,
	}
	service.tokens = &deterministicSessionTokens{secrets: []string{"next"}}
	if _, err := service.Refresh(context.Background(), "session.secret"); !errors.Is(err, domainaccount.ErrRefreshSessionReplayed) {
		t.Fatalf("replay error = %v", err)
	}
}

func TestChangePasswordReplacesSessionsAndCleanupSurfacesFailure(t *testing.T) {
	user, err := domainaccount.New("alice", "Password123!", "Alice")
	if err != nil {
		t.Fatal(err)
	}

	user.ID = 7
	repo := &accountSessionRepositoryStub{user: user, cleanupCount: 3}
	service := New(
		repo,
		accountTokenSignerStub{},
		WithRefreshSessionRepository(repo),
		WithSessionTokenGenerator(&deterministicSessionTokens{
			ids: []string{"replacement", "family"}, secrets: []string{"secret"},
		}),
	)
	result, err := service.ChangePassword(
		context.Background(), user.ID, "Password123!", "Replacement123!",
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.RefreshCredential != "replacement.secret" ||
		repo.replaceInput == nil ||
		repo.replaceInput.Change.NextAuthVersion != 2 {
		t.Fatalf("result=%+v input=%+v", result, repo.replaceInput)
	}
	deleted, err := service.CleanupRefreshSessions(
		context.Background(), 24*time.Hour, 100,
	)
	if err != nil || deleted != 3 {
		t.Fatalf("cleanup deleted=%d err=%v", deleted, err)
	}
	repo.cleanupErr = errors.New("database")
	if _, err := service.CleanupRefreshSessions(
		context.Background(), 24*time.Hour, 100,
	); !errors.Is(err, ErrCleanupRefreshSessionsFailed) {
		t.Fatalf("cleanup failure = %v", err)
	}
}

func TestRefreshSessionCleanupWorkerReportsErrors(t *testing.T) {
	repo := &accountSessionRepositoryStub{cleanupErr: errors.New("database")}
	service := New(
		repo,
		accountTokenSignerStub{},
		WithRefreshSessionRepository(repo),
	)
	reported := make(chan error, 1)
	worker := NewRefreshSessionCleanupWorker(
		service,
		WithRefreshSessionCleanupErrorHandler(func(err error) {
			reported <- err
		}),
	)
	worker.cleanup(context.Background())
	select {
	case err := <-reported:
		if !errors.Is(err, ErrCleanupRefreshSessionsFailed) {
			t.Fatalf("reported error = %v", err)
		}

	default:
		t.Fatal("cleanup error was not reported")
	}
	if err := NewRefreshSessionCleanupWorker(nil).Start(context.Background()); !errors.Is(err, ErrInvalidRefreshSessionCleanup) {
		t.Fatalf("invalid worker error = %v", err)
	}
}

func TestRefreshSessionCleanupWorkerDrainsBoundedBatches(t *testing.T) {
	repo := &accountSessionRepositoryStub{
		cleanupCounts: []int64{100, 100, 25},
	}
	service := New(
		repo,
		accountTokenSignerStub{},
		WithRefreshSessionRepository(repo),
	)
	worker := NewRefreshSessionCleanupWorker(
		service,
		WithRefreshSessionCleanupSchedule(time.Hour, 24*time.Hour, 100),
	)
	worker.cleanup(context.Background())
	if repo.cleanupCalls != 3 {
		t.Fatalf("cleanup calls = %d", repo.cleanupCalls)
	}

	repo.cleanupCalls = 0
	repo.cleanupCounts = make([]int64, maxRefreshSessionCleanupBatches+2)
	for index := range repo.cleanupCounts {
		repo.cleanupCounts[index] = 100
	}
	worker.cleanup(context.Background())
	if repo.cleanupCalls != maxRefreshSessionCleanupBatches {
		t.Fatalf("bounded cleanup calls = %d", repo.cleanupCalls)
	}
}

func TestLoginSigningFailureRevokesCreatedSession(t *testing.T) {
	user, err := domainaccount.New("alice", "Password123!", "Alice")
	if err != nil {
		t.Fatal(err)
	}

	user.ID = 7
	repo := &accountSessionRepositoryStub{user: user}
	service := New(
		repo,
		accountTokenSignerStub{err: errors.New("sign")},
		WithRefreshSessionRepository(repo),
		WithSessionTokenGenerator(&deterministicSessionTokens{
			ids: []string{"session", "family"}, secrets: []string{"secret"},
		}),
	)
	if _, err := service.Login(
		context.Background(), "alice", "Password123!",
	); !errors.Is(err, ErrSignAccessTokenFailed) {
		t.Fatalf("login signing error = %v", err)
	}
	if repo.revokeCalls != 1 {
		t.Fatalf("revoke calls = %d", repo.revokeCalls)
	}
}

func TestPasswordSigningFailureDoesNotCommitCredentialChange(t *testing.T) {
	user, err := domainaccount.New("alice", "Password123!", "Alice")
	if err != nil {
		t.Fatal(err)
	}
	user.ID = 7
	repo := &accountSessionRepositoryStub{user: user}
	service := New(
		repo,
		accountTokenSignerStub{err: errors.New("sign")},
		WithRefreshSessionRepository(repo),
		WithSessionTokenGenerator(&deterministicSessionTokens{
			ids: []string{"session", "family"}, secrets: []string{"secret"},
		}),
	)
	if _, err := service.ChangePassword(
		context.Background(), user.ID, "Password123!", "Replacement123!",
	); !errors.Is(err, ErrSignAccessTokenFailed) {
		t.Fatalf("password signing error = %v", err)
	}
	if repo.replaceInput != nil {
		t.Fatalf("credential change committed after signing failure: %+v", repo.replaceInput)
	}
}
