package applicationaccount

import (
	domainaccount "GCFeed/internal/domain/account"
	"context"
	"errors"
	"time"
)

var ErrLoadAccountFailed = errors.New("failed to load account")
var ErrSaveAccountFailed = errors.New("failed to save account")
var ErrUpdateAccountFailed = errors.New("failed to update account")
var ErrSignAccessTokenFailed = errors.New("failed to sign access token")

// TokenSigner 是应用层依赖的最小 JWT 能力，账号服务只关心“签发 token”和“过期时间”。
type TokenSigner interface {
	SignAccessToken(userID int64, role string) (string, error)
	AccessTTL() time.Duration
}

// Service 编排账号用例：注册、登录、读取资料、更新资料。
type Service struct {
	repo     domainaccount.Repository
	signer   TokenSigner
	settings domainaccount.ProfileSettingRepository
}

// LoginResult 是登录成功后返回给 HTTP 层的 token 数据。
type LoginResult struct {
	AccessToken      string
	TokenType        string
	ExpiresInSeconds int64
}

// Profile 是应用层对外暴露的用户资料视图，屏蔽密码等敏感字段。
type Profile struct {
	ID                int64
	Account           string
	Nickname          string
	AvatarURL         string
	Bio               string
	Status            int
	Role              string
	FollowingCount    int
	FollowerCount     int
	WorkCount         int
	Gender            int
	PublicWorkCount   int
	PrivateWorkCount  int
	ReceivedLikeCount int
	CollectionCount   int
	ProfileSettings   *ProfileSetting
}

type ProfileSetting struct {
	LikedVisibility    string
	FavoriteVisibility string
}

type Option func(*Service)

func WithProfileSettingRepository(repo domainaccount.ProfileSettingRepository) Option {
	return func(service *Service) { service.settings = repo }
}

func New(repo domainaccount.Repository, signer TokenSigner, options ...Option) *Service {
	service := &Service{
		repo:   repo,
		signer: signer,
	}
	for _, option := range options {
		option(service)
	}
	return service
}

// Register 创建新用户：领域层负责校验和加密密码，仓储层负责持久化。
func (s *Service) Register(ctx context.Context, account, password, nickname string) (*Profile, error) {
	user, err := domainaccount.New(account, password, nickname)
	if err != nil {
		return nil, err
	}

	err = s.repo.Save(ctx, user)
	if err != nil {
		if errors.Is(err, domainaccount.ErrAccountAlreadyExists) {
			return nil, domainaccount.ErrAccountAlreadyExists
		}
		return nil, ErrSaveAccountFailed
	}
	profile := profileFromUser(user)
	if s.settings != nil {
		setting, err := s.settings.GetProfileSetting(ctx, user.ID)
		if err != nil {
			return nil, ErrLoadAccountFailed
		}
		profile.ProfileSettings = profileSettingFromDomain(setting)
	}
	return profile, nil
}

// Login 完成账号密码登录，认证通过后签发访问 token。
func (s *Service) Login(ctx context.Context, account, password string) (*LoginResult, error) {
	account = domainaccount.NormalizeAccount(account)
	if account == "" {
		return nil, domainaccount.ErrEmptyAccount
	}

	user, err := s.repo.FindByAccount(ctx, account)
	if err != nil {
		if errors.Is(err, domainaccount.ErrUserNotFound) {
			return nil, domainaccount.ErrInvalidCredentials
		}
		return nil, ErrLoadAccountFailed
	}
	if err := user.Authenticate(password); err != nil {
		return nil, err
	}

	// token 内写入用户 ID 和角色，后续鉴权中间件会解析并放入请求上下文。
	accessToken, err := s.signer.SignAccessToken(user.ID, user.Role)
	if err != nil {
		return nil, ErrSignAccessTokenFailed
	}

	return &LoginResult{
		AccessToken:      accessToken,
		TokenType:        "Bearer",
		ExpiresInSeconds: int64(s.signer.AccessTTL().Seconds()),
	}, nil
}

// GetProfile 根据登录态中的用户 ID 读取当前用户资料。
func (s *Service) GetProfile(ctx context.Context, userID int64) (*Profile, error) {
	if userID <= 0 {
		return nil, domainaccount.ErrInvalidUserID
	}

	user, err := s.repo.FindByID(ctx, userID)
	if err != nil {
		if errors.Is(err, domainaccount.ErrUserNotFound) {
			return nil, domainaccount.ErrUserNotFound
		}
		return nil, ErrLoadAccountFailed
	}

	profile := profileFromUser(user)
	if s.settings != nil {
		setting, err := s.settings.GetProfileSetting(ctx, userID)
		if err != nil {
			return nil, ErrLoadAccountFailed
		}
		profile.ProfileSettings = profileSettingFromDomain(setting)
	}
	return profile, nil
}

// GetPublicProfile 根据用户 ID 读取公开资料，用于访问他人主页。
func (s *Service) GetPublicProfile(ctx context.Context, userID int64) (*Profile, error) {
	if userID <= 0 {
		return nil, domainaccount.ErrInvalidUserID
	}

	user, err := s.repo.FindByID(ctx, userID)
	if err != nil {
		if errors.Is(err, domainaccount.ErrUserNotFound) {
			return nil, domainaccount.ErrUserNotFound
		}
		return nil, ErrLoadAccountFailed
	}

	profile := profileFromUser(user)
	if s.settings != nil {
		setting, err := s.settings.GetProfileSetting(ctx, userID)
		if err != nil {
			return nil, ErrLoadAccountFailed
		}
		profile.ProfileSettings = profileSettingFromDomain(setting)
	}
	return profile, nil
}

// UpdateProfile 支持部分更新，nil 表示该字段没有出现在请求体中。
func (s *Service) UpdateProfile(ctx context.Context, userID int64, nickname, avatarURL, bio *string, gender ...*int) (*Profile, error) {
	if userID <= 0 {
		return nil, domainaccount.ErrInvalidUserID
	}

	user, err := s.repo.FindByID(ctx, userID)
	if err != nil {
		if errors.Is(err, domainaccount.ErrUserNotFound) {
			return nil, domainaccount.ErrUserNotFound
		}
		return nil, ErrLoadAccountFailed
	}
	var genderValue *int
	if len(gender) > 0 {
		genderValue = gender[0]
	}
	if err := user.UpdateProfileWithGender(nickname, avatarURL, bio, genderValue); err != nil {
		return nil, err
	}
	update := profileUpdateFromUser(user, nickname, avatarURL, bio, genderValue)
	if err := s.repo.UpdateProfile(ctx, update); err != nil {
		if errors.Is(err, domainaccount.ErrUserNotFound) {
			return nil, domainaccount.ErrUserNotFound
		}
		return nil, ErrUpdateAccountFailed
	}

	return s.GetProfile(ctx, userID)
}

func (s *Service) UpdateProfileAndSetting(
	ctx context.Context,
	userID int64,
	nickname, avatarURL, bio *string,
	gender *int,
	likedVisibility, favoriteVisibility *string,
) (*Profile, error) {
	if userID <= 0 {
		return nil, domainaccount.ErrInvalidUserID
	}
	hasProfileUpdate := nickname != nil || avatarURL != nil || bio != nil || gender != nil
	hasSettingUpdate := likedVisibility != nil || favoriteVisibility != nil
	if !hasProfileUpdate && !hasSettingUpdate {
		return nil, domainaccount.ErrEmptyProfileUpdate
	}

	user, err := s.repo.FindByID(ctx, userID)
	if err != nil {
		if errors.Is(err, domainaccount.ErrUserNotFound) {
			return nil, domainaccount.ErrUserNotFound
		}
		return nil, ErrLoadAccountFailed
	}
	var profileUpdate *domainaccount.ProfileUpdate
	if hasProfileUpdate {
		if err := user.UpdateProfileWithGender(nickname, avatarURL, bio, gender); err != nil {
			return nil, err
		}
		update := profileUpdateFromUser(user, nickname, avatarURL, bio, gender)
		profileUpdate = &update
	}

	var setting *domainaccount.ProfileSetting
	if s.settings != nil {
		setting, err = s.settings.GetProfileSetting(ctx, userID)
		if err != nil {
			return nil, ErrLoadAccountFailed
		}
	} else {
		setting, _ = domainaccount.NewDefaultProfileSetting(userID)
	}
	var settingUpdate *domainaccount.ProfileSettingUpdate
	if hasSettingUpdate {
		if s.settings == nil {
			return nil, ErrUpdateAccountFailed
		}
		if err := setting.Update(likedVisibility, favoriteVisibility); err != nil {
			return nil, err
		}
		update := profileSettingUpdateFromSetting(setting, likedVisibility, favoriteVisibility)
		settingUpdate = &update
	}

	if err := s.repo.UpdateProfileAndSetting(ctx, profileUpdate, settingUpdate); err != nil {
		if errors.Is(err, domainaccount.ErrUserNotFound) {
			return nil, domainaccount.ErrUserNotFound
		}
		return nil, ErrUpdateAccountFailed
	}
	return s.GetProfile(ctx, userID)
}

func (s *Service) GetProfileSetting(ctx context.Context, userID int64) (*ProfileSetting, error) {
	if userID <= 0 {
		return nil, domainaccount.ErrInvalidUserID
	}
	if s.settings == nil {
		setting, _ := domainaccount.NewDefaultProfileSetting(userID)
		return profileSettingFromDomain(setting), nil
	}
	setting, err := s.settings.GetProfileSetting(ctx, userID)
	if err != nil {
		return nil, ErrLoadAccountFailed
	}
	return profileSettingFromDomain(setting), nil
}

func (s *Service) UpdateProfileSetting(ctx context.Context, userID int64, likedVisibility, favoriteVisibility *string) (*ProfileSetting, error) {
	if userID <= 0 {
		return nil, domainaccount.ErrInvalidUserID
	}
	if s.settings == nil {
		return nil, ErrUpdateAccountFailed
	}
	setting, err := s.settings.GetProfileSetting(ctx, userID)
	if err != nil {
		return nil, ErrLoadAccountFailed
	}
	if err := setting.Update(likedVisibility, favoriteVisibility); err != nil {
		return nil, err
	}
	update := profileSettingUpdateFromSetting(setting, likedVisibility, favoriteVisibility)
	if err := s.settings.UpdateProfileSetting(ctx, update); err != nil {
		return nil, ErrUpdateAccountFailed
	}
	return s.GetProfileSetting(ctx, userID)
}

func profileUpdateFromUser(
	user *domainaccount.User,
	nickname, avatarURL, bio *string,
	gender *int,
) domainaccount.ProfileUpdate {
	update := domainaccount.ProfileUpdate{UserID: user.ID}
	if nickname != nil {
		value := user.Nickname
		update.Nickname = &value
	}
	if avatarURL != nil {
		value := user.AvatarURL
		update.AvatarURL = &value
	}
	if bio != nil {
		value := user.Bio
		update.Bio = &value
	}
	if gender != nil {
		value := user.Gender
		update.Gender = &value
	}
	return update
}

func profileSettingUpdateFromSetting(
	setting *domainaccount.ProfileSetting,
	likedVisibility, favoriteVisibility *string,
) domainaccount.ProfileSettingUpdate {
	update := domainaccount.ProfileSettingUpdate{UserID: setting.UserID}
	if likedVisibility != nil {
		value := setting.LikedVisibility
		update.LikedVisibility = &value
	}
	if favoriteVisibility != nil {
		value := setting.FavoriteVisibility
		update.FavoriteVisibility = &value
	}
	return update
}

// profileFromUser 把领域用户转换成安全的资料对象，避免向外暴露密码哈希。
func profileFromUser(user *domainaccount.User) *Profile {
	return &Profile{
		ID:                user.ID,
		Account:           user.Account,
		Nickname:          user.Nickname,
		AvatarURL:         user.AvatarURL,
		Bio:               user.Bio,
		Status:            user.Status,
		Role:              user.Role,
		FollowingCount:    user.FollowingCount,
		FollowerCount:     user.FollowerCount,
		WorkCount:         user.WorkCount,
		Gender:            user.Gender,
		PublicWorkCount:   user.PublicWorkCount,
		PrivateWorkCount:  user.PrivateWorkCount,
		ReceivedLikeCount: user.ReceivedLikeCount,
		CollectionCount:   user.CollectionCount,
	}
}

func profileSettingFromDomain(setting *domainaccount.ProfileSetting) *ProfileSetting {
	if setting == nil {
		return nil
	}
	return &ProfileSetting{LikedVisibility: setting.LikedVisibility, FavoriteVisibility: setting.FavoriteVisibility}
}
