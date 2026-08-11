package applicationplayback

import (
	"context"
	domainplayback "github.com/shiyudesu/frux/internal/domain/playback"
	"time"
)

const defaultTelemetryCleanupBatchSize = 1000
const maxTelemetryCleanupRunDuration = 5 * time.Second

type TelemetryCleaner struct {
	repo        domainplayback.TelemetryRetentionRepository
	retention   time.Duration
	interval    time.Duration
	batchSize   int
	runDuration time.Duration
	now         func() time.Time
}

func NewTelemetryCleaner(repo domainplayback.TelemetryRetentionRepository, retention time.Duration, interval time.Duration, batchSize int) *TelemetryCleaner {
	if batchSize <= 0 {
		batchSize = defaultTelemetryCleanupBatchSize
	}
	return &TelemetryCleaner{
		repo:        repo,
		retention:   retention,
		interval:    interval,
		batchSize:   batchSize,
		runDuration: maxTelemetryCleanupRunDuration,
		now:         func() time.Time { return time.Now().UTC() },
	}
}

func (cleaner *TelemetryCleaner) Run(
	ctx context.Context,
	onResult func(*domainplayback.TelemetryCleanupResult),
	onError func(error),
) {
	if cleaner == nil || cleaner.repo == nil || cleaner.retention <= 0 || cleaner.interval <= 0 {
		return
	}
	cleaner.cleanup(ctx, onResult, onError)
	ticker := time.NewTicker(cleaner.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cleaner.cleanup(ctx, onResult, onError)
		}
	}
}

func (cleaner *TelemetryCleaner) CleanupOnce(ctx context.Context) (*domainplayback.TelemetryCleanupResult, error) {
	cutoff := cleaner.now().UTC().Add(-cleaner.retention)
	return cleaner.repo.DeleteTelemetryBefore(ctx, cutoff, cleaner.batchSize)
}

func (cleaner *TelemetryCleaner) cleanup(
	ctx context.Context,
	onResult func(*domainplayback.TelemetryCleanupResult),
	onError func(error),
) {
	result, err := cleaner.cleanupRun(ctx)
	if err != nil {
		if onError != nil {
			onError(err)
		}
		return
	}
	if onResult != nil {
		onResult(result)
	}
}

func (cleaner *TelemetryCleaner) cleanupRun(ctx context.Context) (*domainplayback.TelemetryCleanupResult, error) {
	runContext, cancel := context.WithTimeout(ctx, cleaner.runDuration)
	defer cancel()
	total := &domainplayback.TelemetryCleanupResult{}
	for {
		result, err := cleaner.CleanupOnce(runContext)
		if err != nil {
			if runContext.Err() == context.DeadlineExceeded && ctx.Err() == nil {
				return total, nil
			}
			return nil, err
		}
		total.DeletedEvents += result.DeletedEvents
		total.DeletedBatches += result.DeletedBatches
		if result.DeletedEvents < int64(cleaner.batchSize) &&
			result.DeletedBatches < int64(cleaner.batchSize) {
			break
		}
		select {
		case <-runContext.Done():
			if ctx.Err() != nil {
				return total, ctx.Err()
			}
			return total, nil
		default:
		}
	}
	return total, nil
}
