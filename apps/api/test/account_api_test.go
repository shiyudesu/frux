package test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	applicationaccount "github.com/shiyudesu/frux/internal/application/account"
	domainaccount "github.com/shiyudesu/frux/internal/domain/account"
	infrajwt "github.com/shiyudesu/frux/internal/infra/jwt"
	interfaceshttpaccount "github.com/shiyudesu/frux/internal/interfaces/http/account"
	interfaceshttpapierror "github.com/shiyudesu/frux/internal/interfaces/http/apierror"
	interfaceshttpmiddleware "github.com/shiyudesu/frux/internal/interfaces/http/middleware"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
)

type accountProfileResponse struct {
	ID                int64  `json:"id"`
	Account           string `json:"account"`
	Nickname          string `json:"nickname"`
	AvatarURL         string `json:"avatar_url"`
	Bio               string `json:"bio"`
	Status            int    `json:"status"`
	Role              string `json:"role"`
	FollowingCount    int    `json:"following_count"`
	FollowerCount     int    `json:"follower_count"`
	WorkCount         int    `json:"work_count"`
	Gender            int    `json:"gender"`
	ReceivedLikeCount int    `json:"received_like_count"`
	PublicWorkCount   int    `json:"public_work_count"`
	ProfileSettings   *struct {
		LikedVisibility    string `json:"liked_visibility"`
		FavoriteVisibility string `json:"favorite_visibility"`
	} `json:"profile_settings"`
}

type accountTokenResponse struct {
	AccessToken      string `json:"access_token"`
	TokenType        string `json:"token_type"`
	ExpiresInSeconds int64  `json:"expires_in_seconds"`
}

type publicAccountProfileResponse struct {
	ID             int64  `json:"id"`
	Nickname       string `json:"nickname"`
	AvatarURL      string `json:"avatar_url"`
	Bio            string `json:"bio"`
	FollowingCount int    `json:"following_count"`
	FollowerCount  int    `json:"follower_count"`
	WorkCount      int    `json:"work_count"`
}

// memoryAccountRepo 是账号测试用的内存仓储，模拟真实 Repository 的唯一账号索引。
type memoryAccountRepo struct {
	mu               sync.Mutex
	nextID           int64
	byID             map[int64]*domainaccount.User
	byAccount        map[string]int64
	settings         map[int64]*domainaccount.ProfileSetting
	sessions         map[string]*domainaccount.RefreshSession
	failAtomicUpdate bool
	atomicReady      *sync.WaitGroup
	atomicRelease    <-chan struct{}
	passwordReady    *sync.WaitGroup
	passwordRelease  <-chan struct{}
}

func newMemoryAccountRepo() *memoryAccountRepo {
	return &memoryAccountRepo{
		nextID:    1,
		byID:      map[int64]*domainaccount.User{},
		byAccount: map[string]int64{},
		settings:  map[int64]*domainaccount.ProfileSetting{},
		sessions:  map[string]*domainaccount.RefreshSession{},
	}
}

func (r *memoryAccountRepo) CreateRefreshSession(
	_ context.Context,
	session *domainaccount.RefreshSession,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessions[session.ID] = cloneRefreshSession(session)
	return nil
}

func (r *memoryAccountRepo) RotateRefreshSession(
	_ context.Context,
	input domainaccount.RotateRefreshSessionInput,
) (*domainaccount.RotateRefreshSessionResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	session := r.sessions[input.SessionID]
	if session == nil {
		return nil, domainaccount.ErrInvalidRefreshSession
	}
	if session.RevokedAt != nil {
		return nil, domainaccount.ErrRefreshSessionRevoked
	}
	if !input.RotatedAt.Before(session.ExpiresAt) {
		revokedAt := input.RotatedAt
		session.RevokedAt = &revokedAt
		session.RevocationReason = domainaccount.RefreshRevocationExpired
		return nil, domainaccount.ErrRefreshSessionExpired
	}
	user := r.byID[session.UserID]
	if user == nil || user.Status != domainaccount.StatusNormal ||
		user.AuthVersion != session.AuthVersion {
		return nil, domainaccount.ErrRefreshSessionRevoked
	}
	if session.MatchesCurrent(input.SecretHash) {
		previousValidTo := input.RotatedAt.Add(input.PreviousGrace)
		session.PreviousSecretHash = session.SecretHash
		session.PreviousSecretValidTo = &previousValidTo
		session.SecretHash = input.NewSecretHash
		session.LastUsedAt = input.RotatedAt
		return &domainaccount.RotateRefreshSessionResult{
			Session: cloneRefreshSession(session),
			Account: cloneUser(user),
		}, nil
	}
	if session.MatchesPreviousWithinGrace(input.SecretHash, input.RotatedAt) {
		return &domainaccount.RotateRefreshSessionResult{
			Session: cloneRefreshSession(session), Superseded: true,
		}, nil
	}
	if session.MatchesPrevious(input.SecretHash) {
		for _, candidate := range r.sessions {
			if candidate.FamilyID == session.FamilyID && candidate.RevokedAt == nil {
				revokedAt := input.RotatedAt
				candidate.RevokedAt = &revokedAt
				candidate.RevocationReason = domainaccount.RefreshRevocationReplay
			}
		}
		return &domainaccount.RotateRefreshSessionResult{
			Session: cloneRefreshSession(session), ReplayFound: true,
		}, nil
	}
	for _, candidate := range r.sessions {
		if candidate.FamilyID == session.FamilyID && candidate.RevokedAt == nil {
			revokedAt := input.RotatedAt
			candidate.RevokedAt = &revokedAt
			candidate.RevocationReason = domainaccount.RefreshRevocationReplay
		}
	}
	return &domainaccount.RotateRefreshSessionResult{
		Session: cloneRefreshSession(session), ReplayFound: true,
	}, nil
}

func (r *memoryAccountRepo) RevokeRefreshSession(
	_ context.Context,
	sessionID, secretHash, reason string,
	revokedAt time.Time,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	session := r.sessions[sessionID]
	if session == nil || session.RevokedAt != nil {
		return nil
	}
	if !session.MatchesCurrent(secretHash) && !session.MatchesPrevious(secretHash) {
		return nil
	}
	session.RevokedAt = &revokedAt
	session.RevocationReason = reason
	return nil
}

func (r *memoryAccountRepo) ReplacePasswordAndSessions(
	_ context.Context,
	input domainaccount.ReplacePasswordAndSessionsInput,
) error {
	if r.passwordReady != nil {
		r.passwordReady.Done()
		<-r.passwordRelease
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	user := r.byID[input.Change.UserID]
	if user == nil || user.Password != input.Change.ExpectedPassword ||
		user.AuthVersion != input.Change.CurrentAuthVersion {
		return domainaccount.ErrCredentialChanged
	}

	if r.failAtomicUpdate {
		return errors.New("forced atomic update failure")
	}
	user.Password = input.Change.NewPassword
	user.AuthVersion = input.Change.NextAuthVersion
	for _, session := range r.sessions {
		if session.UserID == user.ID && session.RevokedAt == nil {
			revokedAt := input.ChangedAt
			session.RevokedAt = &revokedAt
			session.RevocationReason = domainaccount.RefreshRevocationPasswordChange
			session.ReplacedBySessionID = input.ReplacementSession.ID
		}
	}
	r.sessions[input.ReplacementSession.ID] = cloneRefreshSession(input.ReplacementSession)
	return nil
}

func (r *memoryAccountRepo) DeleteExpiredRefreshSessions(
	_ context.Context,
	now, revokedBefore time.Time,
	limit int,
) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var deleted int64
	for id, session := range r.sessions {
		if int(deleted) >= limit {
			break
		}
		if !session.ExpiresAt.After(now) ||
			(session.RevokedAt != nil && !session.RevokedAt.After(revokedBefore)) {
			delete(r.sessions, id)
			deleted++
		}
	}
	return deleted, nil
}

// Save 模拟 account 表插入逻辑，并在账号重复时返回领域错误。
func (r *memoryAccountRepo) Save(ctx context.Context, user *domainaccount.User) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.byAccount[user.Account]; exists {
		return domainaccount.ErrAccountAlreadyExists
	}

	user.ID = r.nextID
	r.nextID++
	r.byID[user.ID] = cloneUser(user)
	r.byAccount[user.Account] = user.ID
	r.settings[user.ID], _ = domainaccount.NewDefaultProfileSetting(user.ID)
	return nil
}

// FindByAccount 模拟登录时按账号查询用户。
func (r *memoryAccountRepo) FindByAccount(ctx context.Context, account string) (*domainaccount.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	id, exists := r.byAccount[account]
	if !exists {
		return nil, domainaccount.ErrUserNotFound
	}
	return cloneUser(r.byID[id]), nil
}

// FindByID 模拟根据 token 中的用户 ID 查询个人资料。
func (r *memoryAccountRepo) FindByID(ctx context.Context, id int64) (*domainaccount.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	user, exists := r.byID[id]
	if !exists {
		return nil, domainaccount.ErrUserNotFound
	}
	return cloneUser(user), nil
}

func (r *memoryAccountRepo) FindAdminPrincipalByID(
	_ context.Context,
	id int64,
) (*domainaccount.AdminPrincipal, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	user := r.byID[id]
	if user == nil {
		return nil, domainaccount.ErrUserNotFound
	}
	return domainaccount.RestoreAdminPrincipalWithAuthVersion(
		user.ID, user.Status, user.Role, user.AuthVersion,
	), nil
}

// UpdateProfile 只更新资料字段，与真实仓储保持同样的行为边界。
func (r *memoryAccountRepo) UpdateProfile(_ context.Context, update domainaccount.ProfileUpdate) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	stored, exists := r.byID[update.UserID]
	if !exists {
		return domainaccount.ErrUserNotFound
	}
	applyProfileUpdate(stored, update)
	return nil
}

func (r *memoryAccountRepo) UpdateProfileAndSetting(_ context.Context, profile *domainaccount.ProfileUpdate, setting *domainaccount.ProfileSettingUpdate) error {
	if r.atomicReady != nil {
		r.atomicReady.Done()
		<-r.atomicRelease
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.failAtomicUpdate {
		return errors.New("forced atomic update failure")
	}
	if profile != nil {
		if _, exists := r.byID[profile.UserID]; !exists {
			return domainaccount.ErrUserNotFound
		}
	}
	if setting != nil {
		if _, exists := r.byID[setting.UserID]; !exists {
			return domainaccount.ErrUserNotFound
		}
	}
	if profile != nil {
		applyProfileUpdate(r.byID[profile.UserID], *profile)
	}
	if setting != nil {
		stored := r.settings[setting.UserID]
		if stored == nil {
			stored, _ = domainaccount.NewDefaultProfileSetting(setting.UserID)
			r.settings[setting.UserID] = stored
		}
		applyProfileSettingUpdate(stored, *setting)
	}
	return nil
}

func (r *memoryAccountRepo) GetProfileSetting(ctx context.Context, userID int64) (*domainaccount.ProfileSetting, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	setting, exists := r.settings[userID]
	if !exists {
		return domainaccount.NewDefaultProfileSetting(userID)
	}
	cloned := *setting
	return &cloned, nil
}

func (r *memoryAccountRepo) UpdateProfileSetting(_ context.Context, update domainaccount.ProfileSettingUpdate) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.byID[update.UserID]; !exists {
		return domainaccount.ErrUserNotFound
	}
	stored := r.settings[update.UserID]
	if stored == nil {
		stored, _ = domainaccount.NewDefaultProfileSetting(update.UserID)
		r.settings[update.UserID] = stored
	}
	applyProfileSettingUpdate(stored, update)
	return nil
}

func applyProfileUpdate(user *domainaccount.User, update domainaccount.ProfileUpdate) {
	if update.Nickname != nil {
		user.Nickname = *update.Nickname
	}
	if update.AvatarURL != nil {
		user.AvatarURL = *update.AvatarURL
	}
	if update.Bio != nil {
		user.Bio = *update.Bio
	}
	if update.Gender != nil {
		user.Gender = *update.Gender
	}
}

func applyProfileSettingUpdate(setting *domainaccount.ProfileSetting, update domainaccount.ProfileSettingUpdate) {
	if update.LikedVisibility != nil {
		setting.LikedVisibility = *update.LikedVisibility
	}
	if update.FavoriteVisibility != nil {
		setting.FavoriteVisibility = *update.FavoriteVisibility
	}
}

func (r *memoryAccountRepo) SetStatsForTest(userID int64, followingCount int, followerCount int, workCount int) {
	r.mu.Lock()
	defer r.mu.Unlock()

	stored, exists := r.byID[userID]
	if !exists {
		return
	}
	stored.FollowingCount = followingCount
	stored.FollowerCount = followerCount
	stored.WorkCount = workCount
}

// cloneUser 返回副本，避免测试代码直接修改仓储中的内部对象。
func cloneUser(user *domainaccount.User) *domainaccount.User {
	cloned := *user
	return &cloned
}

func cloneRefreshSession(session *domainaccount.RefreshSession) *domainaccount.RefreshSession {
	if session == nil {
		return nil
	}
	cloned := *session
	if session.PreviousSecretValidTo != nil {
		value := *session.PreviousSecretValidTo
		cloned.PreviousSecretValidTo = &value
	}
	if session.RevokedAt != nil {
		value := *session.RevokedAt
		cloned.RevokedAt = &value
	}
	return &cloned
}

// TestAccountAPIFlow 覆盖注册、重复注册、登录、读取资料、更新资料和登出完整流程。
func TestAccountAPIFlow(t *testing.T) {
	router := newAccountRouter(t)

	registerResponse := performJSONRequest(
		router,
		http.MethodPost,
		"/api/users",
		`{"account":" Alice ","password":"CaseSensitivePassword","nickname":"Alice Nickname"}`,
		"",
	)
	requireStatus(t, registerResponse, http.StatusCreated)

	var created accountProfileResponse
	decodeJSON(t, registerResponse, &created)
	if created.ID == 0 {
		t.Fatalf("expected created user id")
	}
	if created.Account != "alice" || created.Nickname != "Alice Nickname" || created.Status != domainaccount.StatusNormal || created.Role != domainaccount.RoleUser {
		t.Fatalf("unexpected register response: %+v", created)
	}
	if !strings.Contains(registerResponse.Body.String(), `"account":"alice"`) {
		t.Fatalf("registration response omitted owner account: %s", registerResponse.Body.String())
	}

	duplicateResponse := performJSONRequest(
		router,
		http.MethodPost,
		"/api/users",
		`{"account":"ALICE","password":"CaseSensitivePassword","nickname":"duplicate"}`,
		"",
	)
	assertAPIError(t, duplicateResponse, http.StatusConflict, interfaceshttpapierror.CodeAccountAlreadyExists, "account already exists")

	loginResponse := performJSONRequest(
		router,
		http.MethodPost,
		"/api/sessions",
		`{"account":" aLiCe ","password":"CaseSensitivePassword"}`,
		"",
	)
	requireStatus(t, loginResponse, http.StatusOK)

	var token accountTokenResponse
	decodeJSON(t, loginResponse, &token)
	if token.AccessToken == "" || token.TokenType != "Bearer" || token.ExpiresInSeconds != 900 {
		t.Fatalf("unexpected login response: %+v", token)
	}
	assertOnlyAssetTokenCookieSet(t, loginResponse)

	meResponse := performJSONRequest(router, http.MethodGet, "/api/users/me", "", token.AccessToken)
	requireStatus(t, meResponse, http.StatusOK)
	assertNoAssetCookiesSet(t, meResponse)

	var profile accountProfileResponse
	decodeJSON(t, meResponse, &profile)
	if profile.ID != created.ID || profile.Account != "alice" || profile.Nickname != "Alice Nickname" {
		t.Fatalf("unexpected profile response: %+v", profile)
	}
	if !strings.Contains(meResponse.Body.String(), `"account":"alice"`) {
		t.Fatalf("authenticated profile omitted owner account: %s", meResponse.Body.String())
	}

	updateResponse := performJSONRequest(
		router,
		http.MethodPatch,
		"/api/users/me",
		`{"nickname":"tester-updated","avatar_url":"https://example.com/avatar.png","bio":"hello feed"}`,
		token.AccessToken,
	)
	requireStatus(t, updateResponse, http.StatusOK)
	assertNoAssetCookiesSet(t, updateResponse)

	var updated accountProfileResponse
	decodeJSON(t, updateResponse, &updated)
	if updated.Nickname != "tester-updated" || updated.AvatarURL != "https://example.com/avatar.png" || updated.Bio != "hello feed" {
		t.Fatalf("unexpected updated profile response: %+v", updated)
	}

	logoutResponse := performJSONRequest(router, http.MethodDelete, "/api/sessions/current", "", token.AccessToken)
	requireStatus(t, logoutResponse, http.StatusNoContent)
	assertNoAssetCookiesSet(t, logoutResponse)

	logoutWithoutToken := performJSONRequest(router, http.MethodDelete, "/api/sessions/current", "", "")
	requireStatus(t, logoutWithoutToken, http.StatusNoContent)
	assertNoAssetCookiesSet(t, logoutWithoutToken)

	logoutWithExpiredToken := performJSONRequest(router, http.MethodDelete, "/api/sessions/current", "", "expired-token")
	requireStatus(t, logoutWithExpiredToken, http.StatusNoContent)
	assertNoAssetCookiesSet(t, logoutWithExpiredToken)
}

func TestStaleLogoutResponseCannotClearNewerLoginAssetCookie(t *testing.T) {
	router := server.New()
	jwtManager, err := infrajwt.NewManager("late-response-secret", "15m")
	if err != nil {
		t.Fatalf("new jwt manager: %v", err)
	}
	handler := interfaceshttpaccount.New(applicationaccount.New(newMemoryAccountRepo(), jwtManager))
	entered := make(chan struct{})
	release := make(chan struct{})
	router.DELETE("/api/sessions/current", func(ctx context.Context, c *app.RequestContext) {
		close(entered)
		<-release
		handler.Logout(ctx, c)
	})
	router.POST("/api/sessions", func(_ context.Context, c *app.RequestContext) {
		interfaceshttpmiddleware.SetAssetTokenCookie(c, "newer-login-token", time.Now().Add(15*time.Minute))
		c.Status(http.StatusOK)
	})

	token, err := jwtManager.SignAccessToken(42, domainaccount.RoleUser)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	logoutDone := make(chan *ut.ResponseRecorder, 1)
	go func() {
		logoutDone <- performJSONRequest(router, http.MethodDelete, "/api/sessions/current", "", token)
	}()
	<-entered

	newerLogin := performJSONRequest(router, http.MethodPost, "/api/sessions", "", "")
	requireStatus(t, newerLogin, http.StatusOK)
	assertOnlyAssetTokenCookieSet(t, newerLogin)
	close(release)

	staleLogout := <-logoutDone
	requireStatus(t, staleLogout, http.StatusNoContent)
	assertNoAssetCookiesSet(t, staleLogout)
}

func assertOnlyAssetTokenCookieSet(t *testing.T, response *ut.ResponseRecorder) {
	t.Helper()
	cookies := response.Header().GetAll("Set-Cookie")
	if len(cookies) != 1 || !strings.Contains(cookies[0], interfaceshttpmiddleware.AssetTokenCookieName+"=") {
		t.Fatalf("login did not set exactly the HttpOnly asset token cookie: %v", cookies)
	}
	if !strings.Contains(strings.ToLower(cookies[0]), "httponly") {
		t.Fatalf("asset token cookie is not HttpOnly: %v", cookies)
	}
	if strings.Contains(cookies[0], interfaceshttpmiddleware.AssetActiveCookieName+"=") {
		t.Fatalf("server must not activate the client-controlled asset marker: %v", cookies)
	}
}

func assertNoAssetCookiesSet(t *testing.T, response *ut.ResponseRecorder) {
	t.Helper()
	for _, cookie := range response.Header().GetAll("Set-Cookie") {
		if strings.Contains(cookie, interfaceshttpmiddleware.AssetTokenCookieName+"=") ||
			strings.Contains(cookie, interfaceshttpmiddleware.AssetActiveCookieName+"=") {
			t.Fatalf("authenticated response unexpectedly refreshed asset cookies: %v", response.Header().GetAll("Set-Cookie"))
		}
	}
}

// TestAccountAPIValidation 覆盖账号接口的常见参数错误和未登录访问。
func TestAccountAPIValidation(t *testing.T) {
	router := newAccountRouter(t)

	registerResponse := performJSONRequest(
		router,
		http.MethodPost,
		"/api/users",
		`{"account":"test","password":"","nickname":"tester"}`,
		"",
	)
	assertAPIError(t, registerResponse, http.StatusBadRequest, interfaceshttpapierror.CodeAccountPasswordInvalid, domainaccount.ErrEmptyPassword.Error())

	shortPassword := performJSONRequest(
		router,
		http.MethodPost,
		"/api/users",
		`{"account":"short","password":"1234567","nickname":"short"}`,
		"",
	)
	assertAPIError(
		t, shortPassword, http.StatusBadRequest,
		interfaceshttpapierror.CodeAccountPasswordInvalid,
		domainaccount.ErrPasswordTooShort.Error(),
	)

	loginResponse := performJSONRequest(
		router,
		http.MethodPost,
		"/api/sessions",
		`{"account":"","password":"12345678"}`,
		"",
	)
	assertAPIError(t, loginResponse, http.StatusBadRequest, interfaceshttpapierror.CodeAccountValidationFailed, domainaccount.ErrEmptyAccount.Error())

	unauthorizedMeResponse := performJSONRequest(router, http.MethodGet, "/api/users/me", "", "")
	assertAPIError(t, unauthorizedMeResponse, http.StatusUnauthorized, interfaceshttpapierror.CodeInvalidAccessToken, "invalid access token")
	invalidTokenResponse := performJSONRequest(router, http.MethodGet, "/api/users/me", "", "not-a-token")
	assertAPIError(t, invalidTokenResponse, http.StatusUnauthorized, interfaceshttpapierror.CodeInvalidAccessToken, "invalid access token")

	token := registerAndLogin(t, router)

	emptyPatchResponse := performJSONRequest(router, http.MethodPatch, "/api/users/me", `{}`, token)
	assertAPIError(t, emptyPatchResponse, http.StatusBadRequest, interfaceshttpapierror.CodeAccountValidationFailed, domainaccount.ErrEmptyProfileUpdate.Error())

	emptyNicknameResponse := performJSONRequest(
		router,
		http.MethodPatch,
		"/api/users/me",
		`{"nickname":"   "}`,
		token,
	)
	assertAPIError(t, emptyNicknameResponse, http.StatusBadRequest, interfaceshttpapierror.CodeAccountValidationFailed, domainaccount.ErrEmptyNickname.Error())
}

func TestAccountLoginInvalidCredentialsAreIndistinguishable(t *testing.T) {
	router := newAccountRouter(t)

	registerResponse := performJSONRequest(
		router,
		http.MethodPost,
		"/api/users",
		`{"account":"login-user","password":"12345678","nickname":"login tester"}`,
		"",
	)
	requireStatus(t, registerResponse, http.StatusCreated)

	unknownAccount := performJSONRequest(
		router,
		http.MethodPost,
		"/api/sessions",
		`{"account":"missing-user","password":"12345678"}`,
		"",
	)
	wrongPassword := performJSONRequest(
		router,
		http.MethodPost,
		"/api/sessions",
		`{"account":"login-user","password":"wrong-password"}`,
		"",
	)

	assertAPIError(t, unknownAccount, http.StatusUnauthorized, interfaceshttpapierror.CodeAuthInvalidCredentials, "invalid credentials")
	assertAPIError(t, wrongPassword, http.StatusUnauthorized, interfaceshttpapierror.CodeAuthInvalidCredentials, "invalid credentials")
	if unknownAccount.Body.String() != wrongPassword.Body.String() {
		t.Fatalf("credential failures should be indistinguishable: unknown=%s wrong=%s", unknownAccount.Body.String(), wrongPassword.Body.String())
	}
	unknownEmpty := performJSONRequest(
		router, http.MethodPost, "/api/sessions",
		`{"account":"missing-user","password":"   "}`,
		"",
	)
	existingEmpty := performJSONRequest(
		router, http.MethodPost, "/api/sessions",
		`{"account":"login-user","password":"   "}`,
		"",
	)
	if unknownEmpty.Body.String() != existingEmpty.Body.String() {
		t.Fatalf(
			"empty credential failures should be indistinguishable: unknown=%s existing=%s",
			unknownEmpty.Body.String(), existingEmpty.Body.String(),
		)
	}
}

func TestFrozenAccountRequiresPasswordProofAndCannotCreateSession(t *testing.T) {
	router, repo := newAccountRouterWithRepo(t)
	register := performJSONRequest(
		router, http.MethodPost, "/api/users",
		`{"account":"inactive","password":"Password123!","nickname":"Inactive"}`,
		"",
	)
	requireStatus(t, register, http.StatusCreated)
	repo.mu.Lock()
	repo.byID[1].Status = domainaccount.StatusFrozen
	sessionCount := len(repo.sessions)
	repo.mu.Unlock()
	wrongPassword := performJSONRequest(
		router, http.MethodPost, "/api/sessions",
		`{"account":"inactive","password":"wrong-password"}`,
		"",
	)
	assertAPIError(
		t, wrongPassword, http.StatusUnauthorized,
		interfaceshttpapierror.CodeAuthInvalidCredentials,
		"invalid credentials",
	)
	login := performJSONRequest(
		router, http.MethodPost, "/api/sessions",
		`{"account":"inactive","password":"Password123!"}`,
		"",
	)
	assertAPIError(
		t, login, http.StatusLocked,
		interfaceshttpapierror.CodeAuthAccountFrozen,
		"account frozen",
	)
	if strings.TrimSpace(strings.Join(login.Header().GetAll("Set-Cookie"), "")) != "" {
		t.Fatalf("frozen login set cookies: %v", login.Header().GetAll("Set-Cookie"))
	}
	repo.mu.Lock()
	if len(repo.sessions) != sessionCount {
		t.Fatalf("frozen login created session: before=%d after=%d", sessionCount, len(repo.sessions))
	}
	repo.byID[1].Status = domainaccount.StatusCancelled
	repo.mu.Unlock()
	cancelled := performJSONRequest(
		router, http.MethodPost, "/api/sessions",
		`{"account":"inactive","password":"Password123!"}`,
		"",
	)
	assertAPIError(
		t, cancelled, http.StatusUnauthorized,
		interfaceshttpapierror.CodeAuthInvalidCredentials,
		"invalid credentials",
	)
}

func TestLoginRejectsCrossOriginAndNonJSONRequests(t *testing.T) {
	router := newAccountRouter(t)
	register := performJSONRequest(
		router, http.MethodPost, "/api/users",
		`{"account":"csrf-user","password":"Password123!","nickname":"CSRF"}`,
		"",
	)
	requireStatus(t, register, http.StatusCreated)
	crossOrigin := performJSONRequestWithHeaders(
		router, http.MethodPost, "/api/sessions",
		`{"account":"csrf-user","password":"Password123!"}`,
		ut.Header{Key: "Origin", Value: "https://attacker.example"},
		ut.Header{Key: "Host", Value: "frux.example"},
	)
	assertAPIError(
		t, crossOrigin, http.StatusBadRequest,
		interfaceshttpapierror.CodeInvalidRequest, "invalid request",
	)
	body := `{"account":"csrf-user","password":"Password123!"}`
	plainText := ut.PerformRequest(
		router.Engine,
		http.MethodPost,
		"/api/sessions",
		&ut.Body{Body: bytes.NewBufferString(body), Len: len(body)},
		ut.Header{Key: "Content-Type", Value: "text/plain"},
	)
	assertAPIError(
		t, plainText, http.StatusBadRequest,
		interfaceshttpapierror.CodeInvalidRequest, "invalid request",
	)
}

func TestAccountRefreshLogoutAndPasswordChangeFlow(t *testing.T) {
	router := newAccountRouter(t)
	register := performJSONRequest(
		router, http.MethodPost, "/api/users",
		`{"account":"session-user","password":"Password123!","nickname":"Session User"}`,
		"",
	)
	requireStatus(t, register, http.StatusCreated)

	login := performJSONRequest(
		router, http.MethodPost, "/api/sessions",
		`{"account":"session-user","password":"Password123!"}`,
		"",
	)
	requireStatus(t, login, http.StatusOK)
	var first accountTokenResponse
	decodeJSON(t, login, &first)
	firstRefresh := responseCookieValue(t, login, interfaceshttpmiddleware.RefreshTokenCookieName)
	if firstRefresh == "" {
		t.Fatalf("login did not set refresh cookie: %v", login.Header().GetAll("Set-Cookie"))
	}
	setCookie := strings.Join(login.Header().GetAll("Set-Cookie"), "\n")
	for _, expected := range []string{
		"frux_refresh_token=", "path=/api/sessions", "HttpOnly", "SameSite=Strict",
		"frux_asset_token=", "path=/uploads",
	} {
		if !strings.Contains(setCookie, expected) {
			t.Fatalf("login cookie header missing %q: %s", expected, setCookie)
		}
	}

	crossOrigin := performJSONRequestWithHeaders(
		router, http.MethodPost, "/api/sessions/current/refresh", "",
		ut.Header{Key: "Origin", Value: "https://attacker.example"},
		ut.Header{Key: "Host", Value: "frux.example"},
		ut.Header{
			Key:   "Cookie",
			Value: interfaceshttpmiddleware.RefreshTokenCookieName + "=" + firstRefresh,
		},
	)
	assertAPIError(
		t, crossOrigin, http.StatusBadRequest,
		interfaceshttpapierror.CodeInvalidRequest,
		"invalid request",
	)

	refresh := performJSONRequestWithHeaders(
		router, http.MethodPost, "/api/sessions/current/refresh", "",
		ut.Header{Key: "Origin", Value: "http://localhost:5173"},
		ut.Header{Key: "Host", Value: "127.0.0.1:8080"},
		ut.Header{Key: "X-Forwarded-Host", Value: "localhost:5173"},
		ut.Header{
			Key:   "Cookie",
			Value: interfaceshttpmiddleware.RefreshTokenCookieName + "=" + firstRefresh,
		},
	)
	requireStatus(t, refresh, http.StatusOK)
	var refreshed accountTokenResponse
	decodeJSON(t, refresh, &refreshed)
	secondRefresh := responseCookieValue(
		t, refresh, interfaceshttpmiddleware.RefreshTokenCookieName,
	)
	if refreshed.AccessToken == "" || secondRefresh == "" || secondRefresh == firstRefresh {
		t.Fatalf("refresh did not rotate credentials: token=%t cookie=%q", refreshed.AccessToken != "", secondRefresh)
	}

	superseded := performJSONRequestWithHeaders(
		router, http.MethodPost, "/api/sessions/current/refresh", "",
		ut.Header{
			Key:   "Cookie",
			Value: interfaceshttpmiddleware.RefreshTokenCookieName + "=" + firstRefresh,
		},
	)
	assertAPIError(
		t, superseded, http.StatusConflict,
		interfaceshttpapierror.CodeAuthRefreshSuperseded,
		"refresh session superseded",
	)

	wrongCurrent := performJSONRequest(
		router, http.MethodPut, "/api/users/me/password",
		`{"current_password":"wrong","new_password":"Replacement123!"}`,
		refreshed.AccessToken,
	)
	assertAPIError(
		t, wrongCurrent, http.StatusBadRequest,
		interfaceshttpapierror.CodeAccountCurrentPasswordIncorrect,
		"current password is incorrect",
	)

	changed := performJSONRequest(
		router, http.MethodPut, "/api/users/me/password",
		`{"current_password":"Password123!","new_password":"Replacement123!"}`,
		refreshed.AccessToken,
	)
	requireStatus(t, changed, http.StatusOK)
	var replacement accountTokenResponse
	decodeJSON(t, changed, &replacement)
	replacementRefresh := responseCookieValue(
		t, changed, interfaceshttpmiddleware.RefreshTokenCookieName,
	)
	if replacement.AccessToken == "" || replacementRefresh == "" {
		t.Fatal("password change did not return replacement session")
	}

	oldAccess := performJSONRequest(
		router, http.MethodGet, "/api/users/me", "", refreshed.AccessToken,
	)
	requireStatus(t, oldAccess, http.StatusOK)

	oldRefresh := performJSONRequestWithHeaders(
		router, http.MethodPost, "/api/sessions/current/refresh", "",
		ut.Header{
			Key:   "Cookie",
			Value: interfaceshttpmiddleware.RefreshTokenCookieName + "=" + secondRefresh,
		},
	)
	assertAPIError(
		t, oldRefresh, http.StatusUnauthorized,
		interfaceshttpapierror.CodeAuthRefreshInvalid,
		"refresh session invalid",
	)

	oldLogin := performJSONRequest(
		router, http.MethodPost, "/api/sessions",
		`{"account":"session-user","password":"Password123!"}`,
		"",
	)
	assertAPIError(
		t, oldLogin, http.StatusUnauthorized,
		interfaceshttpapierror.CodeAuthInvalidCredentials,
		"invalid credentials",
	)
	newLogin := performJSONRequest(
		router, http.MethodPost, "/api/sessions",
		`{"account":"session-user","password":"Replacement123!"}`,
		"",
	)
	requireStatus(t, newLogin, http.StatusOK)

	logout := performJSONRequestWithHeaders(
		router, http.MethodDelete, "/api/sessions/current", "",
		ut.Header{
			Key:   "Cookie",
			Value: interfaceshttpmiddleware.RefreshTokenCookieName + "=" + replacementRefresh,
		},
	)
	requireStatus(t, logout, http.StatusNoContent)
	if strings.Contains(
		strings.Join(logout.Header().GetAll("Set-Cookie"), "\n"),
		interfaceshttpmiddleware.RefreshTokenCookieName+"=",
	) {
		t.Fatalf("logout response cleared or replaced refresh cookie: %v", logout.Header().GetAll("Set-Cookie"))
	}
	repeatedLogout := performJSONRequestWithHeaders(
		router, http.MethodDelete, "/api/sessions/current", "",
		ut.Header{
			Key:   "Cookie",
			Value: interfaceshttpmiddleware.RefreshTokenCookieName + "=" + replacementRefresh,
		},
	)
	requireStatus(t, repeatedLogout, http.StatusNoContent)
}

func TestRefreshReplayRevokesTokenFamily(t *testing.T) {
	router, repo := newAccountRouterWithRepo(t)
	register := performJSONRequest(
		router, http.MethodPost, "/api/users",
		`{"account":"replay-user","password":"Password123!","nickname":"Replay"}`,
		"",
	)
	requireStatus(t, register, http.StatusCreated)
	login := performJSONRequest(
		router, http.MethodPost, "/api/sessions",
		`{"account":"replay-user","password":"Password123!"}`,
		"",
	)
	requireStatus(t, login, http.StatusOK)
	firstRefresh := responseCookieValue(
		t, login, interfaceshttpmiddleware.RefreshTokenCookieName,
	)
	refresh := performJSONRequestWithHeaders(
		router, http.MethodPost, "/api/sessions/current/refresh", "",
		ut.Header{
			Key:   "Cookie",
			Value: interfaceshttpmiddleware.RefreshTokenCookieName + "=" + firstRefresh,
		},
	)
	requireStatus(t, refresh, http.StatusOK)
	secondRefresh := responseCookieValue(
		t, refresh, interfaceshttpmiddleware.RefreshTokenCookieName,
	)
	sessionID, _, ok := applicationaccount.ParseRefreshCredential(firstRefresh)
	if !ok {
		t.Fatal("invalid test refresh credential")
	}
	expiredGrace := time.Now().Add(-time.Second)
	repo.mu.Lock()
	repo.sessions[sessionID].PreviousSecretValidTo = &expiredGrace
	repo.mu.Unlock()

	replay := performJSONRequestWithHeaders(
		router, http.MethodPost, "/api/sessions/current/refresh", "",
		ut.Header{
			Key:   "Cookie",
			Value: interfaceshttpmiddleware.RefreshTokenCookieName + "=" + firstRefresh,
		},
	)
	assertAPIError(
		t, replay, http.StatusUnauthorized,
		interfaceshttpapierror.CodeAuthRefreshReplayed,
		"refresh session replayed",
	)
	familyRevoked := performJSONRequestWithHeaders(
		router, http.MethodPost, "/api/sessions/current/refresh", "",
		ut.Header{
			Key:   "Cookie",
			Value: interfaceshttpmiddleware.RefreshTokenCookieName + "=" + secondRefresh,
		},
	)
	assertAPIError(
		t, familyRevoked, http.StatusUnauthorized,
		interfaceshttpapierror.CodeAuthRefreshInvalid,
		"refresh session invalid",
	)
}

func TestOlderRefreshReplayRevokesAfterMultipleRotations(t *testing.T) {
	router := newAccountRouter(t)
	register := performJSONRequest(
		router, http.MethodPost, "/api/users",
		`{"account":"old-replay","password":"Password123!","nickname":"Replay"}`,
		"",
	)
	requireStatus(t, register, http.StatusCreated)
	login := performJSONRequest(
		router, http.MethodPost, "/api/sessions",
		`{"account":"old-replay","password":"Password123!"}`,
		"",
	)
	requireStatus(t, login, http.StatusOK)
	first := responseCookieValue(
		t, login, interfaceshttpmiddleware.RefreshTokenCookieName,
	)
	secondResponse := performJSONRequestWithHeaders(
		router, http.MethodPost, "/api/sessions/current/refresh", "",
		ut.Header{
			Key:   "Cookie",
			Value: interfaceshttpmiddleware.RefreshTokenCookieName + "=" + first,
		},
	)
	requireStatus(t, secondResponse, http.StatusOK)
	second := responseCookieValue(
		t, secondResponse, interfaceshttpmiddleware.RefreshTokenCookieName,
	)
	thirdResponse := performJSONRequestWithHeaders(
		router, http.MethodPost, "/api/sessions/current/refresh", "",
		ut.Header{
			Key:   "Cookie",
			Value: interfaceshttpmiddleware.RefreshTokenCookieName + "=" + second,
		},
	)
	requireStatus(t, thirdResponse, http.StatusOK)
	third := responseCookieValue(
		t, thirdResponse, interfaceshttpmiddleware.RefreshTokenCookieName,
	)
	replayed := performJSONRequestWithHeaders(
		router, http.MethodPost, "/api/sessions/current/refresh", "",
		ut.Header{
			Key:   "Cookie",
			Value: interfaceshttpmiddleware.RefreshTokenCookieName + "=" + first,
		},
	)
	assertAPIError(
		t, replayed, http.StatusUnauthorized,
		interfaceshttpapierror.CodeAuthRefreshReplayed,
		"refresh session replayed",
	)
	revoked := performJSONRequestWithHeaders(
		router, http.MethodPost, "/api/sessions/current/refresh", "",
		ut.Header{
			Key:   "Cookie",
			Value: interfaceshttpmiddleware.RefreshTokenCookieName + "=" + third,
		},
	)
	assertAPIError(
		t, revoked, http.StatusUnauthorized,
		interfaceshttpapierror.CodeAuthRefreshInvalid,
		"refresh session invalid",
	)
}

func TestRefreshRejectsMissingMalformedAndExpiredCredentials(t *testing.T) {
	router, repo := newAccountRouterWithRepo(t)
	for _, cookie := range []string{"", "malformed"} {
		headers := []ut.Header{}
		if cookie != "" {
			headers = append(headers, ut.Header{
				Key:   "Cookie",
				Value: interfaceshttpmiddleware.RefreshTokenCookieName + "=" + cookie,
			})
		}
		response := performJSONRequestWithHeaders(
			router, http.MethodPost, "/api/sessions/current/refresh", "", headers...,
		)
		assertAPIError(
			t, response, http.StatusUnauthorized,
			interfaceshttpapierror.CodeAuthRefreshInvalid,
			"refresh session invalid",
		)
	}
	register := performJSONRequest(
		router, http.MethodPost, "/api/users",
		`{"account":"expired-user","password":"Password123!","nickname":"Expired"}`,
		"",
	)
	requireStatus(t, register, http.StatusCreated)
	login := performJSONRequest(
		router, http.MethodPost, "/api/sessions",
		`{"account":"expired-user","password":"Password123!"}`,
		"",
	)
	requireStatus(t, login, http.StatusOK)
	credential := responseCookieValue(
		t, login, interfaceshttpmiddleware.RefreshTokenCookieName,
	)
	sessionID, _, ok := applicationaccount.ParseRefreshCredential(credential)
	if !ok {
		t.Fatal("invalid test refresh credential")
	}
	repo.mu.Lock()
	repo.sessions[sessionID].ExpiresAt = time.Now().Add(-time.Second)
	repo.mu.Unlock()
	expired := performJSONRequestWithHeaders(
		router, http.MethodPost, "/api/sessions/current/refresh", "",
		ut.Header{
			Key:   "Cookie",
			Value: interfaceshttpmiddleware.RefreshTokenCookieName + "=" + credential,
		},
	)
	assertAPIError(
		t, expired, http.StatusUnauthorized,
		interfaceshttpapierror.CodeAuthRefreshInvalid,
		"refresh session invalid",
	)
}

func TestConcurrentPasswordChangesCommitOnce(t *testing.T) {
	router, repo := newAccountRouterWithRepo(t)
	token := registerAndLogin(t, router)
	ready := &sync.WaitGroup{}
	ready.Add(2)
	release := make(chan struct{})
	repo.passwordReady = ready
	repo.passwordRelease = release

	responses := make(chan *ut.ResponseRecorder, 2)
	for _, replacement := range []string{"ReplacementOne!", "ReplacementTwo!"} {
		replacement := replacement
		go func() {
			responses <- performJSONRequest(
				router, http.MethodPut, "/api/users/me/password",
				fmt.Sprintf(
					`{"current_password":"12345678","new_password":%q}`,
					replacement,
				),
				token,
			)
		}()
	}
	ready.Wait()
	close(release)
	statuses := map[int]int{}
	for range 2 {
		statuses[(<-responses).Code]++
	}
	repo.passwordReady = nil
	repo.passwordRelease = nil
	if statuses[http.StatusOK] != 1 || statuses[http.StatusConflict] != 1 {
		t.Fatalf("password change statuses = %+v", statuses)
	}
}

func TestPasswordChangeFailurePreservesCredentialAndSessions(t *testing.T) {
	router, repo := newAccountRouterWithRepo(t)
	token := registerAndLogin(t, router)
	repo.failAtomicUpdate = true
	failed := performJSONRequest(
		router, http.MethodPut, "/api/users/me/password",
		`{"current_password":"12345678","new_password":"Replacement123!"}`,
		token,
	)
	assertAPIError(
		t, failed, http.StatusServiceUnavailable,
		interfaceshttpapierror.CodeAuthenticationUnavailable,
		"authentication unavailable",
	)
	repo.failAtomicUpdate = false
	oldLogin := performJSONRequest(
		router, http.MethodPost, "/api/sessions",
		`{"account":"login-user","password":"12345678"}`,
		"",
	)
	requireStatus(t, oldLogin, http.StatusOK)
}

func TestPasswordChangeImmediatelyInvalidatesAdminCredential(t *testing.T) {
	router, repo := newAccountRouterWithRepo(t)
	token := registerAndLogin(t, router)
	repo.mu.Lock()
	repo.byID[1].Role = domainaccount.RoleReviewer
	repo.mu.Unlock()
	manager, err := infrajwt.NewManager("test-secret", "15m", "30m")
	if err != nil {
		t.Fatal(err)
	}
	adminToken, err := manager.SignAdminAccessTokenVersion(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	router.GET(
		"/api/admin/session-check",
		interfaceshttpmiddleware.NewAdminJWTAuth(manager),
		interfaceshttpmiddleware.NewRequireAdminPermission(
			repo, domainaccount.PermissionReviewRead,
		),
		func(_ context.Context, c *app.RequestContext) {
			c.Status(http.StatusNoContent)
		},
	)
	before := performJSONRequestWithHeaders(
		router, http.MethodGet, "/api/admin/session-check", "",
		ut.Header{Key: "Authorization", Value: "Bearer " + adminToken},
	)
	requireStatus(t, before, http.StatusNoContent)
	changed := performJSONRequest(
		router, http.MethodPut, "/api/users/me/password",
		`{"current_password":"12345678","new_password":"Replacement123!"}`,
		token,
	)
	requireStatus(t, changed, http.StatusOK)
	after := performJSONRequestWithHeaders(
		router, http.MethodGet, "/api/admin/session-check", "",
		ut.Header{Key: "Authorization", Value: "Bearer " + adminToken},
	)
	assertAPIError(
		t, after, http.StatusUnauthorized,
		interfaceshttpapierror.CodeAdminAuthInvalidAccessToken,
		"invalid admin access token",
	)
}

// TestPublicAccountProfile 覆盖公开用户主页资料中的关注数、粉丝数和作品数。
func TestPublicAccountProfile(t *testing.T) {
	router, repo := newAccountRouterWithRepo(t)

	registerResponse := performJSONRequest(
		router,
		http.MethodPost,
		"/api/users",
		`{"account":"creator","password":"12345678","nickname":"creator name"}`,
		"",
	)
	requireStatus(t, registerResponse, http.StatusCreated)

	var created accountProfileResponse
	decodeJSON(t, registerResponse, &created)
	repo.SetStatsForTest(created.ID, 7, 11, 3)

	response := performJSONRequest(router, http.MethodGet, "/api/users/1", "", "")
	requireStatus(t, response, http.StatusOK)

	var profile publicAccountProfileResponse
	decodeJSON(t, response, &profile)
	if profile.ID != created.ID || profile.Nickname != "creator name" || profile.FollowingCount != 7 || profile.FollowerCount != 11 || profile.WorkCount != 3 {
		t.Fatalf("unexpected public profile response: %+v", profile)
	}
	if strings.Contains(response.Body.String(), `"account"`) {
		t.Fatalf("public profile leaked account field: %s", response.Body.String())
	}
}

func TestProfileGenderAndSettings(t *testing.T) {
	router := newAccountRouter(t)
	token := registerAndLogin(t, router)

	meResponse := performJSONRequest(router, http.MethodGet, "/api/users/me", "", token)
	requireStatus(t, meResponse, http.StatusOK)
	var me accountProfileResponse
	decodeJSON(t, meResponse, &me)
	if me.ProfileSettings == nil || me.ProfileSettings.LikedVisibility != domainaccount.ProfileVisibilityPrivate || me.ProfileSettings.FavoriteVisibility != domainaccount.ProfileVisibilityPrivate {
		t.Fatalf("unexpected default settings: %+v", me.ProfileSettings)
	}

	updateResponse := performJSONRequest(router, http.MethodPatch, "/api/users/me", `{"gender":2}`, token)
	requireStatus(t, updateResponse, http.StatusOK)
	var updated accountProfileResponse
	decodeJSON(t, updateResponse, &updated)
	if updated.Gender != domainaccount.GenderFemale {
		t.Fatalf("unexpected gender: %d", updated.Gender)
	}

	invalidGender := performJSONRequest(router, http.MethodPatch, "/api/users/me", `{"gender":9}`, token)
	requireStatus(t, invalidGender, http.StatusBadRequest)

	settingsResponse := performJSONRequest(router, http.MethodPatch, "/api/users/me/profile-settings", `{"liked_visibility":"public"}`, token)
	requireStatus(t, settingsResponse, http.StatusOK)
	var settings map[string]string
	decodeJSON(t, settingsResponse, &settings)
	if settings["liked_visibility"] != domainaccount.ProfileVisibilityPublic || settings["favorite_visibility"] != domainaccount.ProfileVisibilityPrivate {
		t.Fatalf("unexpected updated settings: %+v", settings)
	}
	favoriteResponse := performJSONRequest(router, http.MethodPatch, "/api/users/me/profile-settings", `{"favorite_visibility":"public"}`, token)
	requireStatus(t, favoriteResponse, http.StatusOK)
	likedResponse := performJSONRequest(router, http.MethodPatch, "/api/users/me/profile-settings", `{"liked_visibility":"private"}`, token)
	requireStatus(t, likedResponse, http.StatusOK)
	decodeJSON(t, likedResponse, &settings)
	if settings["liked_visibility"] != domainaccount.ProfileVisibilityPrivate || settings["favorite_visibility"] != domainaccount.ProfileVisibilityPublic {
		t.Fatalf("partial settings update overwrote an unrelated field: %+v", settings)
	}

	invalidSetting := performJSONRequest(router, http.MethodPatch, "/api/users/me/profile-settings", `{"favorite_visibility":"friends"}`, token)
	requireStatus(t, invalidSetting, http.StatusBadRequest)

	atomicUpdate := performJSONRequest(
		router,
		http.MethodPatch,
		"/api/users/me",
		`{"nickname":"atomic nickname","bio":"atomic bio","gender":3,"profile_settings":{"liked_visibility":"private","favorite_visibility":"private"}}`,
		token,
	)
	requireStatus(t, atomicUpdate, http.StatusOK)
	var atomicProfile accountProfileResponse
	decodeJSON(t, atomicUpdate, &atomicProfile)
	if atomicProfile.Nickname != "atomic nickname" || atomicProfile.Bio != "atomic bio" || atomicProfile.Gender != domainaccount.GenderOther {
		t.Fatalf("unexpected atomic profile response: %+v", atomicProfile)
	}
	if atomicProfile.ProfileSettings == nil || atomicProfile.ProfileSettings.LikedVisibility != domainaccount.ProfileVisibilityPrivate {
		t.Fatalf("unexpected atomic settings response: %+v", atomicProfile.ProfileSettings)
	}

	publicResponse := performJSONRequest(router, http.MethodGet, "/api/users/1", "", "")
	requireStatus(t, publicResponse, http.StatusOK)
	var publicPayload map[string]json.RawMessage
	decodeJSON(t, publicResponse, &publicPayload)
	if _, exists := publicPayload["profile_settings"]; exists {
		t.Fatalf("public response exposed profile settings: %s", publicResponse.Body.String())
	}
}

func TestAtomicProfileUpdateRollsBackOnFailure(t *testing.T) {
	router, repo := newAccountRouterWithRepo(t)
	token := registerAndLogin(t, router)
	repo.failAtomicUpdate = true

	response := performJSONRequest(
		router,
		http.MethodPatch,
		"/api/users/me",
		`{"nickname":"must not persist","profile_settings":{"liked_visibility":"public"}}`,
		token,
	)
	assertAPIError(t, response, http.StatusInternalServerError, interfaceshttpapierror.CodeInternal, "internal server error")

	repo.failAtomicUpdate = false
	profileResponse := performJSONRequest(router, http.MethodGet, "/api/users/me", "", token)
	requireStatus(t, profileResponse, http.StatusOK)
	var profile accountProfileResponse
	decodeJSON(t, profileResponse, &profile)
	if profile.Nickname != "login tester" {
		t.Fatalf("profile changed after failed atomic update: %+v", profile)
	}
	if profile.ProfileSettings == nil || profile.ProfileSettings.LikedVisibility != domainaccount.ProfileVisibilityPrivate {
		t.Fatalf("settings changed after failed atomic update: %+v", profile.ProfileSettings)
	}
}

func TestConcurrentPartialProfileUpdatesPreserveUnrelatedFields(t *testing.T) {
	router, repo := newAccountRouterWithRepo(t)
	token := registerAndLogin(t, router)
	var ready sync.WaitGroup
	ready.Add(2)
	release := make(chan struct{})
	repo.atomicReady = &ready
	repo.atomicRelease = release

	responses := make(chan *ut.ResponseRecorder, 2)
	go func() {
		responses <- performJSONRequest(
			router,
			http.MethodPatch,
			"/api/users/me",
			`{"nickname":"concurrent nickname","profile_settings":{"liked_visibility":"public"}}`,
			token,
		)
	}()
	go func() {
		responses <- performJSONRequest(
			router,
			http.MethodPatch,
			"/api/users/me",
			`{"bio":"concurrent bio","profile_settings":{"favorite_visibility":"public"}}`,
			token,
		)
	}()
	ready.Wait()
	close(release)
	for range 2 {
		requireStatus(t, <-responses, http.StatusOK)
	}
	repo.atomicReady = nil
	repo.atomicRelease = nil

	response := performJSONRequest(router, http.MethodGet, "/api/users/me", "", token)
	requireStatus(t, response, http.StatusOK)
	var profile accountProfileResponse
	decodeJSON(t, response, &profile)
	if profile.Nickname != "concurrent nickname" || profile.Bio != "concurrent bio" {
		t.Fatalf("concurrent partial profile update lost a field: %+v", profile)
	}
	if profile.ProfileSettings == nil ||
		profile.ProfileSettings.LikedVisibility != domainaccount.ProfileVisibilityPublic ||
		profile.ProfileSettings.FavoriteVisibility != domainaccount.ProfileVisibilityPublic {
		t.Fatalf("concurrent partial settings update lost a field: %+v", profile.ProfileSettings)
	}
}

// registerAndLogin 为需要登录态的测试准备可用 access token。
func registerAndLogin(t *testing.T, router *server.Hertz) string {
	t.Helper()

	registerResponse := performJSONRequest(
		router,
		http.MethodPost,
		"/api/users",
		`{"account":"login-user","password":"12345678","nickname":"login tester"}`,
		"",
	)
	requireStatus(t, registerResponse, http.StatusCreated)

	loginResponse := performJSONRequest(
		router,
		http.MethodPost,
		"/api/sessions",
		`{"account":"login-user","password":"12345678"}`,
		"",
	)
	requireStatus(t, loginResponse, http.StatusOK)

	var token accountTokenResponse
	decodeJSON(t, loginResponse, &token)
	if token.AccessToken == "" {
		t.Fatalf("expected access token")
	}
	return token.AccessToken
}

// newAccountRouter 只装配账号相关路由，使测试聚焦账号模块。
func newAccountRouter(t *testing.T) *server.Hertz {
	router, _ := newAccountRouterWithRepo(t)
	return router
}

func newAccountRouterWithRepo(t *testing.T) (*server.Hertz, *memoryAccountRepo) {
	t.Helper()

	router := server.New()

	jwtManager, err := infrajwt.NewManager("test-secret", "15m")
	if err != nil {
		t.Fatalf("new jwt manager: %v", err)
	}
	repo := newMemoryAccountRepo()
	service := applicationaccount.New(repo, jwtManager, applicationaccount.WithProfileSettingRepository(repo))
	handler := interfaceshttpaccount.New(service)
	authMiddleware := interfaceshttpmiddleware.NewJWTAuth(jwtManager)

	api := router.Group("/api")
	// 测试路由保持和正式 RESTful 路由一致，便于测试覆盖真实接口路径。
	sessions := api.Group("/sessions")
	sessions.POST("", handler.Login)
	sessions.POST("/current/refresh", handler.Refresh)
	sessions.DELETE("/current", handler.Logout)

	users := api.Group("/users")
	users.POST("", handler.Register)
	users.GET("/me", authMiddleware, handler.Me)
	users.PATCH("/me", authMiddleware, handler.UpdateMe)
	users.PUT("/me/password", authMiddleware, handler.ChangePassword)
	users.GET("/me/profile-settings", authMiddleware, handler.GetProfileSettings)
	users.PATCH("/me/profile-settings", authMiddleware, handler.UpdateProfileSettings)
	users.GET("/:userId", handler.Get)

	return router, repo
}

// performJSONRequest 构造 JSON 请求，并在需要时附加 Bearer token。
func performJSONRequest(router *server.Hertz, method, path, body, accessToken string) *ut.ResponseRecorder {
	headers := make([]ut.Header, 0, 1)
	if accessToken != "" {
		headers = append(headers, ut.Header{Key: "Authorization", Value: "Bearer " + accessToken})
	}
	return performJSONRequestWithHeaders(router, method, path, body, headers...)
}

func performJSONRequestWithHeaders(router *server.Hertz, method, path, body string, headers ...ut.Header) *ut.ResponseRecorder {
	var requestBody *ut.Body
	if body != "" {
		requestBody = &ut.Body{Body: bytes.NewBufferString(body), Len: len(body)}
		headers = append(headers, ut.Header{Key: "Content-Type", Value: "application/json"})
	}
	return ut.PerformRequest(router.Engine, method, path, requestBody, headers...)
}

// decodeJSON 解码响应体，失败时输出原始响应内容便于定位问题。
func decodeJSON(t *testing.T, resp *ut.ResponseRecorder, target any) {
	t.Helper()

	if err := json.Unmarshal(resp.Body.Bytes(), target); err != nil {
		t.Fatalf("decode response body %q: %v", resp.Body.String(), err)
	}
}

// requireStatus 统一断言 HTTP 状态码，失败时把响应体一并打印出来。
func requireStatus(t *testing.T, resp *ut.ResponseRecorder, expected int) {
	t.Helper()

	if resp.Code != expected {
		t.Fatalf("expected status %d, got %d body=%s", expected, resp.Code, resp.Body.String())
	}
}

func responseCookieValue(
	t *testing.T,
	resp *ut.ResponseRecorder,
	name string,
) string {
	t.Helper()
	marker := name + "="
	for _, value := range resp.Header().GetAll("Set-Cookie") {
		index := strings.Index(value, marker)
		if index < 0 {
			continue
		}
		raw := value[index+len(marker):]
		if end := strings.IndexByte(raw, ';'); end >= 0 {
			raw = raw[:end]
		}
		return raw
	}
	return ""
}
