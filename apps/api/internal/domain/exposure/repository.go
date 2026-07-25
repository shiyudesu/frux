package domainexposure

import "context"

// Repository 定义曝光模块需要的持久化能力。
type Repository interface {
	SaveViewEvent(ctx context.Context, event *ViewEvent) (*ViewEvent, *Exposure, error)
}

type HistoryRepository interface {
	ListHistory(ctx context.Context, userID int64, cursor *HistoryCursor, limit int) ([]*ViewHistory, error)
	DeleteHistory(ctx context.Context, userID, videoID int64) error
	ClearHistory(ctx context.Context, userID int64) error
}
