package infralibrary

import "time"

type WatchLaterModel struct {
	UserID    int64     `gorm:"column:user_id;primaryKey;index:idx_user_watch_later_user_status_updated,priority:1"`
	VideoID   int64     `gorm:"column:video_id;primaryKey;index:idx_user_watch_later_user_status_updated,priority:4"`
	Status    int       `gorm:"column:status;type:smallint;not null;default:1;index:idx_user_watch_later_user_status_updated,priority:2"`
	CreatedAt time.Time `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt time.Time `gorm:"column:updated_at;autoUpdateTime;index:idx_user_watch_later_user_status_updated,priority:3"`
}

func (WatchLaterModel) TableName() string {
	return "user_watch_later"
}
