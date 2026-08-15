package domainaccount

import (
	"context"
	"time"
)

type ProfileUpdate struct {
	UserID    int64
	Nickname  *string
	AvatarURL *string
	Bio       *string
	Gender    *int
}

type ProfileSettingUpdate struct {
	UserID             int64
	LikedVisibility    *string
	FavoriteVisibility *string
}

// Repository 定义账号领域需要的持久化能力，应用层只依赖这个接口。
type Repository interface {
	// Save 保存新用户，账号重复时返回 ErrAccountAlreadyExists。
	Save(ctx context.Context, user *User) error
	// FindByAccount 用于登录流程通过账号查找用户。
	FindByAccount(ctx context.Context, account string) (*User, error)
	// FindByID 用于根据登录态读取当前用户。
	FindByID(ctx context.Context, id int64) (*User, error)
	// UpdateProfile 只更新用户展示资料字段。
	UpdateProfile(ctx context.Context, update ProfileUpdate) error
	// UpdateProfileAndSetting 在一个持久化事务中更新资料和隐私设置；nil 表示不更新对应部分。
	UpdateProfileAndSetting(ctx context.Context, profile *ProfileUpdate, setting *ProfileSettingUpdate) error
}

type AuthorDisplayReader interface {
	BatchGetAuthorDisplays(ctx context.Context, userIDs []int64) (map[int64]*AuthorDisplay, error)
}

type AdminPrincipalReader interface {
	FindAdminPrincipalByID(ctx context.Context, userID int64) (*AdminPrincipal, error)
}

type ManagedAccountReader interface {
	ListManagedAccounts(ctx context.Context, query ManagedAccountQuery) ([]*ManagedAccount, error)
	GetManagedAccount(ctx context.Context, userID int64) (*ManagedAccount, error)
}

type RefreshSessionRepository interface {
	CreateRefreshSession(ctx context.Context, session *RefreshSession) error
	RotateRefreshSession(ctx context.Context, input RotateRefreshSessionInput) (*RotateRefreshSessionResult, error)
	RevokeRefreshSession(ctx context.Context, sessionID, secretHash, reason string, revokedAt time.Time) error
	ReplacePasswordAndSessions(ctx context.Context, input ReplacePasswordAndSessionsInput) error
	DeleteExpiredRefreshSessions(ctx context.Context, now, revokedBefore time.Time, limit int) (int64, error)
}
