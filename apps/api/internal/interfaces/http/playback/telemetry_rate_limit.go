package interfaceshttpplayback

import (
	"sync"
	"time"
)

const defaultTelemetryBatchesPerMinute = 60
const defaultTelemetryRateLimitEntries = 10_000

type telemetryRateLimitEntry struct {
	count   int
	resetAt time.Time
}

type telemetryRateLimiter struct {
	mu         sync.Mutex
	limit      int
	window     time.Duration
	maxEntries int
	entries    map[int64]telemetryRateLimitEntry
	now        func() time.Time
}

func newTelemetryRateLimiter(limit int, window time.Duration, maxEntries int) *telemetryRateLimiter {
	if limit <= 0 {
		limit = defaultTelemetryBatchesPerMinute
	}
	if window <= 0 {
		window = time.Minute
	}
	if maxEntries <= 0 {
		maxEntries = defaultTelemetryRateLimitEntries
	}
	return &telemetryRateLimiter{
		limit:      limit,
		window:     window,
		maxEntries: maxEntries,
		entries:    make(map[int64]telemetryRateLimitEntry),
		now:        time.Now,
	}
}

func (limiter *telemetryRateLimiter) Allow(userID int64) bool {
	if limiter == nil || userID <= 0 {
		return false
	}
	now := limiter.now()
	limiter.mu.Lock()
	defer limiter.mu.Unlock()

	entry, exists := limiter.entries[userID]
	if exists && now.Before(entry.resetAt) {
		if entry.count >= limiter.limit {
			return false
		}
		entry.count++
		limiter.entries[userID] = entry
		return true
	}
	if !exists && len(limiter.entries) >= limiter.maxEntries {
		for id, candidate := range limiter.entries {
			if !now.Before(candidate.resetAt) {
				delete(limiter.entries, id)
			}
		}
		if len(limiter.entries) >= limiter.maxEntries {
			return false
		}
	}
	limiter.entries[userID] = telemetryRateLimitEntry{count: 1, resetAt: now.Add(limiter.window)}
	return true
}
