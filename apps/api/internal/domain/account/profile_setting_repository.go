package domainaccount

import "context"

type ProfileSettingRepository interface {
	GetProfileSetting(ctx context.Context, userID int64) (*ProfileSetting, error)
	UpdateProfileSetting(ctx context.Context, update ProfileSettingUpdate) error
}
