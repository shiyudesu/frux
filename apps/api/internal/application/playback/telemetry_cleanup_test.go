package applicationplayback

import (
	domainplayback "GCFeed/internal/domain/playback"
	"context"
	"testing"
	"time"
)

type telemetryRetentionRepoStub struct {
	cutoff  time.Time
	limit   int
	results []*domainplayback.TelemetryCleanupResult
	calls   int
}

type blockingTelemetryRetentionRepo struct{}

func (blockingTelemetryRetentionRepo) DeleteTelemetryBefore(ctx context.Context, _ time.Time, _ int) (*domainplayback.TelemetryCleanupResult, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (repo *telemetryRetentionRepoStub) DeleteTelemetryBefore(_ context.Context, cutoff time.Time, limit int) (*domainplayback.TelemetryCleanupResult, error) {
	repo.cutoff = cutoff
	repo.limit = limit
	if repo.calls < len(repo.results) {
		result := repo.results[repo.calls]
		repo.calls++
		return result, nil
	}
	repo.calls++
	return &domainplayback.TelemetryCleanupResult{DeletedEvents: 2, DeletedBatches: 1}, nil
}

func TestTelemetryCleanerUsesConfiguredRetentionAndBatchSize(t *testing.T) {
	repo := &telemetryRetentionRepoStub{}
	cleaner := NewTelemetryCleaner(repo, 7*24*time.Hour, time.Hour, 250)
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	cleaner.now = func() time.Time { return now }

	result, err := cleaner.CleanupOnce(context.Background())
	if err != nil {
		t.Fatalf("cleanup telemetry: %v", err)
	}
	if !repo.cutoff.Equal(now.Add(-7*24*time.Hour)) || repo.limit != 250 {
		t.Fatalf("unexpected cleanup request: cutoff=%s limit=%d", repo.cutoff, repo.limit)
	}
	if result.DeletedEvents != 2 || result.DeletedBatches != 1 {
		t.Fatalf("unexpected cleanup result: %+v", result)
	}
}

func TestTelemetryCleanerDrainsMultipleBoundedBatches(t *testing.T) {
	repo := &telemetryRetentionRepoStub{
		results: []*domainplayback.TelemetryCleanupResult{
			{DeletedEvents: 1000, DeletedBatches: 1000},
			{DeletedEvents: 1000, DeletedBatches: 1000},
			{DeletedEvents: 50, DeletedBatches: 10},
		},
	}
	cleaner := NewTelemetryCleaner(repo, 7*24*time.Hour, time.Hour, 1000)

	result, err := cleaner.cleanupRun(context.Background())
	if err != nil {
		t.Fatalf("drain telemetry cleanup: %v", err)
	}
	if repo.calls != 3 || result.DeletedEvents != 2050 || result.DeletedBatches != 2010 {
		t.Fatalf("unexpected cleanup drain: calls=%d result=%+v", repo.calls, result)
	}
}

func TestTelemetryCleanerBoundsSlowDatabaseCalls(t *testing.T) {
	cleaner := NewTelemetryCleaner(blockingTelemetryRetentionRepo{}, 7*24*time.Hour, time.Hour, 1000)
	cleaner.runDuration = 10 * time.Millisecond
	startedAt := time.Now()

	result, err := cleaner.cleanupRun(context.Background())
	if err != nil {
		t.Fatalf("bounded cleanup should stop at its time budget: %v", err)
	}
	if result.DeletedEvents != 0 || time.Since(startedAt) > time.Second {
		t.Fatalf("cleanup did not honor its time budget: result=%+v elapsed=%s", result, time.Since(startedAt))
	}
}
