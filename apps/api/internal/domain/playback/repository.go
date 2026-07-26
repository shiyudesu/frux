package domainplayback

import "context"
import "time"

// Repository 定义播放优化需要的持久化能力。
type Repository interface {
	// FindConfig 按端和网络类型读取播放配置。
	FindConfig(ctx context.Context, platform string, networkType string) (*Config, error)
	// ListPreloadVideos 为兼容客户端按发布时间读取当前视频之后的补充资源，不代表活动 Feed 场景顺序。
	ListPreloadVideos(ctx context.Context, currentVideoID int64, limit int) ([]*PreloadVideo, error)
	// CreateQoSReport 保存播放质量上报；幂等键命中时返回既有记录。
	CreateQoSReport(ctx context.Context, report *QoSReport) (*QoSReport, bool, error)
}

// TelemetryRepository persists bounded telemetry batches independently from legacy QoS reports.
type TelemetryRepository interface {
	CreateTelemetryBatch(ctx context.Context, batch *TelemetryBatch) (*TelemetryBatchWriteResult, error)
}

type TelemetryRetentionRepository interface {
	DeleteTelemetryBefore(ctx context.Context, cutoff time.Time, limit int) (*TelemetryCleanupResult, error)
}
