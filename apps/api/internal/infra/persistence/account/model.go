package infraaccount

import "time"

// UserModel 映射 account 表，保存用户登录凭证和展示资料。
type UserModel struct {
	ID      int64  `gorm:"column:id;primaryKey;autoIncrement"`
	Account string `gorm:"column:account;size:64;not null;uniqueIndex:uk_account_account"`
	// Password 保存 bcrypt 哈希值，登录时用于校验用户输入密码。
	Password    string    `gorm:"column:password;size:255;not null"`
	Nickname    string    `gorm:"column:nickname;size:128;not null"`
	AvatarURL   string    `gorm:"column:avatar_url;size:512"`
	Bio         string    `gorm:"column:bio;size:255"`
	Gender      int       `gorm:"column:gender;type:smallint;not null;default:0"`
	Status      int       `gorm:"column:status;type:smallint;not null;default:1"`
	Role        string    `gorm:"column:role;size:32;not null"`
	AuthVersion int64     `gorm:"column:auth_version;not null;default:1"`
	CreatedAt   time.Time `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt   time.Time `gorm:"column:updated_at;autoUpdateTime"`
}

type RefreshSessionModel struct {
	ID                    string     `gorm:"column:id;size:64;primaryKey"`
	FamilyID              string     `gorm:"column:family_id;size:64;not null;index:idx_account_refresh_session_family"`
	UserID                int64      `gorm:"column:user_id;not null;index:idx_account_refresh_session_user"`
	SecretHash            string     `gorm:"column:secret_hash;size:64;not null"`
	PreviousSecretHash    string     `gorm:"column:previous_secret_hash;size:64"`
	PreviousSecretValidTo *time.Time `gorm:"column:previous_secret_valid_to"`
	AuthVersion           int64      `gorm:"column:auth_version;not null"`
	ExpiresAt             time.Time  `gorm:"column:expires_at;not null;index:idx_account_refresh_session_cleanup,priority:1"`
	LastUsedAt            time.Time  `gorm:"column:last_used_at;not null"`
	RevokedAt             *time.Time `gorm:"column:revoked_at;index:idx_account_refresh_session_cleanup,priority:2"`
	RevocationReason      string     `gorm:"column:revocation_reason;size:32"`
	ReplacedBySessionID   string     `gorm:"column:replaced_by_session_id;size:64"`
	CreatedAt             time.Time  `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt             time.Time  `gorm:"column:updated_at;autoUpdateTime"`
}

type ProfileSettingModel struct {
	UserID             int64     `gorm:"column:user_id;primaryKey"`
	LikedVisibility    string    `gorm:"column:liked_visibility;size:16;not null;default:private"`
	FavoriteVisibility string    `gorm:"column:favorite_visibility;size:16;not null;default:private"`
	CreatedAt          time.Time `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt          time.Time `gorm:"column:updated_at;autoUpdateTime"`
}

func (ProfileSettingModel) TableName() string {
	return "account_profile_setting"
}

// TableName 指定数据库表名，避免 GORM 使用默认复数表名。
func (UserModel) TableName() string {
	return "account"
}

func (RefreshSessionModel) TableName() string {
	return "account_refresh_session"
}
