package infraaccount

import (
	domainaccount "GCFeed/internal/domain/account"
	infrapersistence "GCFeed/internal/infra/persistence"
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Repository struct {
	db *gorm.DB
}

type userWithStatModel struct {
	ID                int64
	Account           string
	Password          string
	Nickname          string
	AvatarURL         string
	Bio               string
	Gender            int
	Status            int
	Role              string
	FollowingCount    int
	FollowerCount     int
	WorkCount         int
	PrivateWorkCount  int
	ReceivedLikeCount int
	CollectionCount   int
}

type authorDisplayModel struct {
	ID        int64
	Nickname  string
	AvatarURL string
}

// New 创建账号仓储实现，db 由路由装配阶段注入。
func New(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// Save 将领域用户转换为 GORM 模型并写入 account 表。
func (r *Repository) Save(ctx context.Context, user *domainaccount.User) error {
	model := UserModel{
		Account:   user.Account,
		Password:  user.Password,
		Nickname:  user.Nickname,
		AvatarURL: user.AvatarURL,
		Bio:       user.Bio,
		Gender:    user.Gender,
		Status:    user.Status,
		Role:      user.Role,
	}

	if err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&model).Error; err != nil {
			return err
		}
		return tx.Create(&ProfileSettingModel{
			UserID:             model.ID,
			LikedVisibility:    domainaccount.ProfileVisibilityPrivate,
			FavoriteVisibility: domainaccount.ProfileVisibilityPrivate,
		}).Error
	}); err != nil {
		if infrapersistence.IsDuplicatedKeyError(err) {
			// account 字段有唯一索引，重复注册会转换成领域错误。
			return domainaccount.ErrAccountAlreadyExists
		}
		return err
	}
	// 数据库自增 ID 写回领域对象，应用层随后把 ID 返回给客户端。
	user.ID = model.ID
	return nil
}

// FindByAccount 根据账号查找用户，登录流程会调用它。
func (r *Repository) FindByAccount(ctx context.Context, account string) (*domainaccount.User, error) {
	var user userWithStatModel
	account = domainaccount.NormalizeAccount(account)
	err := r.db.WithContext(ctx).
		Table("account AS a").
		Select(userWithStatSelect()).
		Joins("LEFT JOIN user_relation_stat AS rs ON rs.user_id = a.id").
		Joins("LEFT JOIN user_content_stat AS cs ON cs.user_id = a.id").
		Joins("LEFT JOIN (SELECT user_id, COUNT(*) AS following_count FROM user_follow WHERE status = 1 GROUP BY user_id) AS active_following ON active_following.user_id = a.id").
		Joins("LEFT JOIN (SELECT target_user_id, COUNT(*) AS follower_count FROM user_follow WHERE status = 1 GROUP BY target_user_id) AS active_followers ON active_followers.target_user_id = a.id").
		Joins("LEFT JOIN (SELECT author_id, COUNT(*) AS work_count FROM video WHERE status = 2 AND visibility = 'public' AND media_status IN ('legacy_ready', 'ready') GROUP BY author_id) AS published_works ON published_works.author_id = a.id").
		Where("a.account = ?", account).
		Take(&user).
		Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domainaccount.ErrUserNotFound
		}
		return nil, err
	}

	return restoreUser(user), nil
}

// FindByID 根据用户 ID 查找用户，鉴权后的个人资料接口会调用它。
func (r *Repository) FindByID(ctx context.Context, id int64) (*domainaccount.User, error) {
	var user userWithStatModel
	err := r.db.WithContext(ctx).
		Table("account AS a").
		Select(userWithStatSelect()).
		Joins("LEFT JOIN user_relation_stat AS rs ON rs.user_id = a.id").
		Joins("LEFT JOIN user_content_stat AS cs ON cs.user_id = a.id").
		Joins("LEFT JOIN (SELECT user_id, COUNT(*) AS following_count FROM user_follow WHERE status = 1 GROUP BY user_id) AS active_following ON active_following.user_id = a.id").
		Joins("LEFT JOIN (SELECT target_user_id, COUNT(*) AS follower_count FROM user_follow WHERE status = 1 GROUP BY target_user_id) AS active_followers ON active_followers.target_user_id = a.id").
		Joins("LEFT JOIN (SELECT author_id, COUNT(*) AS work_count FROM video WHERE status = 2 AND visibility = 'public' AND media_status IN ('legacy_ready', 'ready') GROUP BY author_id) AS published_works ON published_works.author_id = a.id").
		Where("a.id = ?", id).
		Take(&user).
		Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domainaccount.ErrUserNotFound
		}
		return nil, err
	}

	return restoreUser(user), nil
}

func (r *Repository) BatchGetAuthorDisplays(ctx context.Context, userIDs []int64) (map[int64]*domainaccount.AuthorDisplay, error) {
	result := make(map[int64]*domainaccount.AuthorDisplay, len(userIDs))
	if len(userIDs) == 0 {
		return result, nil
	}
	var models []authorDisplayModel
	if err := r.db.WithContext(ctx).
		Table("account").
		Select("id, nickname, avatar_url").
		Where("id IN ?", userIDs).
		Scan(&models).Error; err != nil {
		return nil, err
	}
	for _, model := range models {
		result[model.ID] = domainaccount.RestoreAuthorDisplay(model.ID, model.Nickname, model.AvatarURL)
	}
	return result, nil
}

// UpdateProfile 只更新资料字段，账号、密码、角色等字段保持原值。
func (r *Repository) UpdateProfile(ctx context.Context, update domainaccount.ProfileUpdate) error {
	values := profileUpdateValues(update)
	if update.UserID <= 0 {
		return domainaccount.ErrInvalidUserID
	}
	if len(values) == 0 {
		return domainaccount.ErrEmptyProfileUpdate
	}
	result := r.db.WithContext(ctx).
		Model(&UserModel{}).
		Where("id = ?", update.UserID).
		Updates(values)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return domainaccount.ErrUserNotFound
	}
	return nil
}

func (r *Repository) UpdateProfileAndSetting(ctx context.Context, profile *domainaccount.ProfileUpdate, setting *domainaccount.ProfileSettingUpdate) error {
	if profile == nil && setting == nil {
		return domainaccount.ErrEmptyProfileUpdate
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if profile != nil {
			values := profileUpdateValues(*profile)
			if profile.UserID <= 0 {
				return domainaccount.ErrInvalidUserID
			}
			if len(values) == 0 {
				return domainaccount.ErrEmptyProfileUpdate
			}
			result := tx.Model(&UserModel{}).
				Where("id = ?", profile.UserID).
				Updates(values)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected == 0 {
				return domainaccount.ErrUserNotFound
			}
		}
		if setting != nil {
			values := profileSettingUpdateValues(*setting)
			if setting.UserID <= 0 {
				return domainaccount.ErrInvalidUserID
			}
			if len(values) == 0 {
				return domainaccount.ErrEmptyProfileSettingUpdate
			}
			if err := ensureProfileSetting(tx, setting.UserID); err != nil {
				return err
			}
			if err := tx.Model(&ProfileSettingModel{}).
				Where("user_id = ?", setting.UserID).
				Updates(values).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *Repository) GetProfileSetting(ctx context.Context, userID int64) (*domainaccount.ProfileSetting, error) {
	if userID <= 0 {
		return nil, domainaccount.ErrInvalidUserID
	}
	var model ProfileSettingModel
	err := r.db.WithContext(ctx).Where("user_id = ?", userID).Take(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return domainaccount.NewDefaultProfileSetting(userID)
	}
	if err != nil {
		return nil, err
	}
	return domainaccount.RestoreProfileSetting(model.UserID, model.LikedVisibility, model.FavoriteVisibility), nil
}

func (r *Repository) UpdateProfileSetting(ctx context.Context, update domainaccount.ProfileSettingUpdate) error {
	if update.UserID <= 0 {
		return domainaccount.ErrInvalidUserID
	}
	values := profileSettingUpdateValues(update)
	if len(values) == 0 {
		return domainaccount.ErrEmptyProfileSettingUpdate
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := ensureProfileSetting(tx, update.UserID); err != nil {
			return err
		}
		return tx.Model(&ProfileSettingModel{}).
			Where("user_id = ?", update.UserID).
			Updates(values).Error
	})
}

func profileUpdateValues(update domainaccount.ProfileUpdate) map[string]any {
	values := make(map[string]any, 4)
	if update.Nickname != nil {
		values["nickname"] = *update.Nickname
	}
	if update.AvatarURL != nil {
		values["avatar_url"] = *update.AvatarURL
	}
	if update.Bio != nil {
		values["bio"] = *update.Bio
	}
	if update.Gender != nil {
		values["gender"] = *update.Gender
	}
	return values
}

func profileSettingUpdateValues(update domainaccount.ProfileSettingUpdate) map[string]any {
	values := make(map[string]any, 2)
	if update.LikedVisibility != nil {
		values["liked_visibility"] = *update.LikedVisibility
	}
	if update.FavoriteVisibility != nil {
		values["favorite_visibility"] = *update.FavoriteVisibility
	}
	return values
}

func ensureProfileSetting(tx *gorm.DB, userID int64) error {
	return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&ProfileSettingModel{
		UserID:             userID,
		LikedVisibility:    domainaccount.ProfileVisibilityPrivate,
		FavoriteVisibility: domainaccount.ProfileVisibilityPrivate,
	}).Error
}

func EnsureProfileSettings(db *gorm.DB) error {
	return db.Exec(`
		INSERT INTO account_profile_setting (user_id, liked_visibility, favorite_visibility, created_at, updated_at)
		SELECT a.id, 'private', 'private', NOW(), NOW()
		FROM account AS a
		LEFT JOIN account_profile_setting AS s ON s.user_id = a.id
		WHERE s.user_id IS NULL
	`).Error
}

// restoreUser 把数据库模型转换回领域对象，业务逻辑继续操作领域类型。
func restoreUser(user userWithStatModel) *domainaccount.User {
	return domainaccount.RestoreUserWithDashboard(
		user.ID,
		user.Account,
		user.Password,
		user.Nickname,
		user.AvatarURL,
		user.Bio,
		user.Gender,
		user.Status,
		user.Role,
		user.FollowingCount,
		user.FollowerCount,
		user.WorkCount,
		user.PrivateWorkCount,
		user.ReceivedLikeCount,
		user.CollectionCount,
	)
}

func userWithStatSelect() string {
	return "a.id, a.account, a.password, a.nickname, a.avatar_url, a.bio, a.gender, a.status, a.role, COALESCE(active_following.following_count, rs.following_count, 0) AS following_count, COALESCE(active_followers.follower_count, rs.follower_count, 0) AS follower_count, COALESCE(cs.public_work_count, published_works.work_count, 0) AS work_count, COALESCE(cs.private_work_count, 0) AS private_work_count, COALESCE(cs.received_like_count, 0) AS received_like_count, COALESCE(cs.collection_count, 0) AS collection_count"
}
