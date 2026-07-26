package interfaceshttpplayback

import (
	"testing"
	"time"
)

func TestTelemetryRateLimiterBoundsPerUserAndEntries(t *testing.T) {
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	limiter := newTelemetryRateLimiter(2, time.Minute, 1)
	limiter.now = func() time.Time { return now }

	if !limiter.Allow(1) || !limiter.Allow(1) || limiter.Allow(1) {
		t.Fatal("expected two allowed batches followed by a rejection")
	}
	if limiter.Allow(2) {
		t.Fatal("expected bounded entry map to reject a new user")
	}

	now = now.Add(time.Minute)
	if !limiter.Allow(2) {
		t.Fatal("expected expired entry cleanup to admit a new user")
	}
}
