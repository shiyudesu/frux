package applicationplayback

import (
	domainplayback "github.com/shiyudesu/frux/internal/domain/playback"
	"context"
	"errors"
	"strings"
	"time"
)

var ErrLoadPlaybackFailed = errors.New("failed to load playback data")
var ErrSaveQoSReportFailed = errors.New("failed to save playback qos report")
var ErrSaveTelemetryBatchFailed = errors.New("failed to save playback telemetry batch")

type Service struct {
	repo              domainplayback.Repository
	telemetryRepo     domainplayback.TelemetryRepository
	telemetryObserver TelemetryObserver
	now               func() time.Time
}

type ConfigResult struct {
	Config *domainplayback.Config
}

type PreloadResult struct {
	Items []*domainplayback.PreloadVideo
}

type QoSReportResult struct {
	Report  *domainplayback.QoSReport
	Created bool
}

type TelemetryBatchResult struct {
	Summary *domainplayback.TelemetryBatchWriteResult
}

type Option func(*Service)

type TelemetryObserver interface {
	RecordTelemetryBatch(batch *domainplayback.TelemetryBatch, summary *domainplayback.TelemetryBatchWriteResult, receivedAt time.Time)
	RecordTelemetryRejection(eventCount int)
}

func New(repo domainplayback.Repository, options ...Option) *Service {
	service := &Service{
		repo: repo,
		now:  func() time.Time { return time.Now().UTC() },
	}
	for _, option := range options {
		option(service)
	}
	return service
}

func WithTelemetryRepository(repo domainplayback.TelemetryRepository) Option {
	return func(service *Service) {
		service.telemetryRepo = repo
	}
}

func WithTelemetryObserver(observer TelemetryObserver) Option {
	return func(service *Service) {
		service.telemetryObserver = observer
	}
}

func WithNow(now func() time.Time) Option {
	return func(service *Service) {
		if now != nil {
			service.now = now
		}
	}
}

// GetConfig 查询端侧播放配置，配置缺失时返回领域默认值。
func (s *Service) GetConfig(ctx context.Context, platform string, networkType string) (*ConfigResult, error) {
	platform = domainplayback.NormalizePlatform(platform)
	networkType = domainplayback.NormalizeNetworkType(networkType)
	if len(platform) > domainplayback.MaxPlatformLength {
		return nil, domainplayback.ErrInvalidPlatform
	}
	if len(networkType) > domainplayback.MaxNetworkTypeLength {
		return nil, domainplayback.ErrInvalidNetworkType
	}

	config, err := s.repo.FindConfig(ctx, platform, networkType)
	if err != nil {
		return nil, ErrLoadPlaybackFailed
	}
	if config == nil && networkType != domainplayback.NetworkDefault {
		config, err = s.repo.FindConfig(ctx, platform, domainplayback.NetworkDefault)
		if err != nil {
			return nil, ErrLoadPlaybackFailed
		}
	}
	if config == nil {
		config = domainplayback.DefaultConfig(platform, networkType)
	}
	return &ConfigResult{Config: normalizeConfig(config, platform, networkType)}, nil
}

// ListPreloadVideos 为兼容客户端查询按发布时间排列的补充资源。
func (s *Service) ListPreloadVideos(ctx context.Context, currentVideoID int64, limit int) (*PreloadResult, error) {
	if currentVideoID < 0 {
		return nil, domainplayback.ErrInvalidVideoID
	}
	limit = normalizeLimit(limit)

	items, err := s.repo.ListPreloadVideos(ctx, currentVideoID, limit)
	if err != nil {
		return nil, ErrLoadPlaybackFailed
	}
	return &PreloadResult{Items: items}, nil
}

// CreateQoSReport 写入播放质量流水。
func (s *Service) CreateQoSReport(ctx context.Context, userID int64, videoID int64, firstFrameMs *int, stutterCount int, watchMs int, idempotencyKey string) (*QoSReportResult, error) {
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	report, err := domainplayback.NewQoSReport(userID, videoID, firstFrameMs, stutterCount, watchMs, idempotencyKey)
	if err != nil {
		return nil, err
	}

	created, inserted, err := s.repo.CreateQoSReport(ctx, report)
	if err != nil {
		return nil, ErrSaveQoSReportFailed
	}
	return &QoSReportResult{Report: created, Created: inserted}, nil
}

func (s *Service) CreateTelemetryBatch(ctx context.Context, input domainplayback.NewTelemetryBatchInput) (*TelemetryBatchResult, error) {
	batch, err := domainplayback.NewTelemetryBatch(input)
	if err != nil {
		s.recordTelemetryRejection(len(input.Events))
		return nil, err
	}
	now := s.now().UTC().Truncate(time.Microsecond)
	if batch.ClientSentAt.Before(now.Add(-domainplayback.MaxTelemetryPastSentAtSkew)) ||
		batch.ClientSentAt.After(now.Add(domainplayback.MaxTelemetryFutureSentAtSkew)) {
		s.recordTelemetryRejection(len(batch.Events))
		return nil, domainplayback.ErrTelemetrySentAtOutOfRange
	}
	if s.telemetryRepo == nil {
		s.recordTelemetryRejection(len(batch.Events))
		return nil, ErrSaveTelemetryBatchFailed
	}
	summary, err := s.telemetryRepo.CreateTelemetryBatch(ctx, batch)
	if err != nil {
		s.recordTelemetryRejection(len(batch.Events))
		if errors.Is(err, domainplayback.ErrTelemetryBatchConflict) ||
			errors.Is(err, domainplayback.ErrTelemetryEventConflict) {
			return nil, err
		}
		return nil, ErrSaveTelemetryBatchFailed
	}
	if s.telemetryObserver != nil {
		s.telemetryObserver.RecordTelemetryBatch(batch, summary, now)
	}
	return &TelemetryBatchResult{Summary: summary}, nil
}

func (s *Service) recordTelemetryRejection(eventCount int) {
	if s.telemetryObserver != nil {
		s.telemetryObserver.RecordTelemetryRejection(eventCount)
	}
}

func normalizeConfig(config *domainplayback.Config, platform string, networkType string) *domainplayback.Config {
	normalized := *config
	normalized.Platform = domainplayback.NormalizePlatform(normalized.Platform)
	normalized.NetworkType = domainplayback.NormalizeNetworkType(normalized.NetworkType)
	if normalized.Platform == "" {
		normalized.Platform = platform
	}
	if normalized.NetworkType == "" {
		normalized.NetworkType = networkType
	}
	if normalized.PreloadCount <= 0 {
		normalized.PreloadCount = domainplayback.DefaultPreloadCount
	}
	if normalized.PreloadCount > domainplayback.MaxPreloadLimit {
		normalized.PreloadCount = domainplayback.MaxPreloadLimit
	}
	if normalized.BufferMs <= 0 {
		normalized.BufferMs = domainplayback.DefaultBufferMs
	}
	return &normalized
}

func normalizeLimit(limit int) int {
	if limit <= 0 {
		return domainplayback.DefaultPreloadCount
	}
	if limit > domainplayback.MaxPreloadLimit {
		return domainplayback.MaxPreloadLimit
	}
	return limit
}
