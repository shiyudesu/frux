package domainaccount

import (
	"strings"

	"golang.org/x/crypto/bcrypt"
)

const (
	RoleUser     = "user"
	RoleAdmin    = "admin"
	StatusNormal = 1

	GenderUnspecified = 0
	GenderMale        = 1
	GenderFemale      = 2
	GenderOther       = 3

	ProfileVisibilityPrivate = "private"
	ProfileVisibilityPublic  = "public"
)

// User 是账号聚合根，保存登录凭证、展示资料和权限角色。
type User struct {
	ID        int64
	Account   string
	Password  string
	Nickname  string
	AvatarURL string
	Bio       string
	Gender    int
	Status    int
	Role      string
	// FollowingCount 和 FollowerCount 来自关系模块统计表，用于个人页展示。
	FollowingCount    int
	FollowerCount     int
	WorkCount         int
	PublicWorkCount   int
	PrivateWorkCount  int
	ReceivedLikeCount int
	CollectionCount   int
}

type ProfileSetting struct {
	UserID             int64
	LikedVisibility    string
	FavoriteVisibility string
}

type AuthorDisplay struct {
	UserID    int64
	Nickname  string
	AvatarURL string
}

// NormalizeAccount returns the canonical identity used for storage and lookup.
func NormalizeAccount(account string) string {
	return strings.ToLower(strings.TrimSpace(account))
}

// New 创建新用户，负责输入清洗、必填校验和密码哈希。
func New(account, password, nickname string) (*User, error) {
	account = NormalizeAccount(account)
	password = strings.TrimSpace(password)
	nickname = strings.TrimSpace(nickname)

	if account == "" {
		return nil, ErrEmptyAccount
	}
	if password == "" {
		return nil, ErrEmptyPassword
	}
	if nickname == "" {
		return nil, ErrEmptyNickname
	}

	// 密码只保存 bcrypt 哈希，数据库中不会保存明文密码。
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, ErrHashPasswordFailed
	}

	return &User{
		Account:  account,
		Password: string(hashedPassword),
		Nickname: nickname,
		Status:   StatusNormal,
		Role:     RoleUser,
	}, nil
}

// RestoreUser 从数据库记录恢复领域对象，读取路径无需再次执行注册校验。
func RestoreUser(id int64, account, password, nickname, avatarURL, bio string, status int, role string) *User {
	return RestoreUserWithStats(id, account, password, nickname, avatarURL, bio, status, role, 0, 0, 0)
}

// RestoreUserWithStats 从数据库记录恢复领域对象，并带上关系统计。
func RestoreUserWithStats(id int64, account, password, nickname, avatarURL, bio string, status int, role string, followingCount int, followerCount int, workCount int) *User {
	return RestoreUserWithDashboard(id, account, password, nickname, avatarURL, bio, GenderUnspecified, status, role, followingCount, followerCount, workCount, 0, 0, 0)
}

func RestoreUserWithDashboard(id int64, account, password, nickname, avatarURL, bio string, gender int, status int, role string, followingCount int, followerCount int, publicWorkCount int, privateWorkCount int, receivedLikeCount int, collectionCount int) *User {
	account = NormalizeAccount(account)
	password = strings.TrimSpace(password)
	nickname = strings.TrimSpace(nickname)
	avatarURL = strings.TrimSpace(avatarURL)
	bio = strings.TrimSpace(bio)
	role = strings.TrimSpace(role)
	if status == 0 {
		status = StatusNormal
	}
	if role == "" {
		// 老数据或测试数据没有角色时，按普通用户处理。
		role = RoleUser
	}
	if !ValidGender(gender) {
		gender = GenderUnspecified
	}

	return &User{
		ID:                id,
		Account:           account,
		Password:          password,
		Nickname:          nickname,
		AvatarURL:         avatarURL,
		Bio:               bio,
		Gender:            gender,
		Status:            status,
		Role:              role,
		FollowingCount:    clampCount(followingCount),
		FollowerCount:     clampCount(followerCount),
		WorkCount:         clampCount(publicWorkCount),
		PublicWorkCount:   clampCount(publicWorkCount),
		PrivateWorkCount:  clampCount(privateWorkCount),
		ReceivedLikeCount: clampCount(receivedLikeCount),
		CollectionCount:   clampCount(collectionCount),
	}
}

func RestoreAuthorDisplay(userID int64, nickname, avatarURL string) *AuthorDisplay {
	return &AuthorDisplay{
		UserID:    userID,
		Nickname:  strings.TrimSpace(nickname),
		AvatarURL: strings.TrimSpace(avatarURL),
	}
}

// Authenticate 校验用户输入密码是否匹配已保存的 bcrypt 哈希。
func (u *User) Authenticate(password string) error {
	password = strings.TrimSpace(password)
	if password == "" {
		return ErrEmptyPassword
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.Password), []byte(password)); err != nil {
		return ErrInvalidCredentials
	}
	return nil
}

// UpdateProfile 执行资料部分更新，指针为 nil 表示该字段保持原值。
func (u *User) UpdateProfile(nickname, avatarURL, bio *string) error {
	return u.UpdateProfileWithGender(nickname, avatarURL, bio, nil)
}

func (u *User) UpdateProfileWithGender(nickname, avatarURL, bio *string, gender *int) error {
	if nickname == nil && avatarURL == nil && bio == nil && gender == nil {
		return ErrEmptyProfileUpdate
	}

	if nickname != nil {
		value := strings.TrimSpace(*nickname)
		if value == "" {
			return ErrEmptyNickname
		}
		u.Nickname = value
	}
	if avatarURL != nil {
		u.AvatarURL = strings.TrimSpace(*avatarURL)
	}
	if bio != nil {
		u.Bio = strings.TrimSpace(*bio)
	}
	if gender != nil {
		if !ValidGender(*gender) {
			return ErrInvalidGender
		}
		u.Gender = *gender
	}

	return nil
}

func NewDefaultProfileSetting(userID int64) (*ProfileSetting, error) {
	if userID <= 0 {
		return nil, ErrInvalidUserID
	}
	return &ProfileSetting{
		UserID:             userID,
		LikedVisibility:    ProfileVisibilityPrivate,
		FavoriteVisibility: ProfileVisibilityPrivate,
	}, nil
}

func RestoreProfileSetting(userID int64, likedVisibility, favoriteVisibility string) *ProfileSetting {
	setting, _ := NewDefaultProfileSetting(userID)
	if setting == nil {
		setting = &ProfileSetting{UserID: userID}
	}
	if ValidProfileVisibility(likedVisibility) {
		setting.LikedVisibility = strings.ToLower(strings.TrimSpace(likedVisibility))
	}
	if ValidProfileVisibility(favoriteVisibility) {
		setting.FavoriteVisibility = strings.ToLower(strings.TrimSpace(favoriteVisibility))
	}
	return setting
}

func (s *ProfileSetting) Update(likedVisibility, favoriteVisibility *string) error {
	if likedVisibility == nil && favoriteVisibility == nil {
		return ErrEmptyProfileSettingUpdate
	}
	if likedVisibility != nil {
		value := strings.ToLower(strings.TrimSpace(*likedVisibility))
		if !ValidProfileVisibility(value) {
			return ErrInvalidProfileVisibility
		}
		s.LikedVisibility = value
	}
	if favoriteVisibility != nil {
		value := strings.ToLower(strings.TrimSpace(*favoriteVisibility))
		if !ValidProfileVisibility(value) {
			return ErrInvalidProfileVisibility
		}
		s.FavoriteVisibility = value
	}
	return nil
}

func ValidGender(gender int) bool {
	return gender >= GenderUnspecified && gender <= GenderOther
}

func ValidProfileVisibility(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case ProfileVisibilityPrivate, ProfileVisibilityPublic:
		return true
	default:
		return false
	}
}

func clampCount(value int) int {
	if value < 0 {
		return 0
	}
	return value
}
