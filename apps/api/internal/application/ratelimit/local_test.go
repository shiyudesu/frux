package applicationratelimit

import (
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestLocalLimiterRefillRejectExpiryAndSaturation(t *testing.T) {
	now := time.Date(2026, 8, 6, 8, 0, 0, 0, time.UTC)
	limiter := NewLocalLimiter(1, time.Minute, WithLocalLimiterClock(func() time.Time { return now }))
	quota := Quota{Capacity: 2, RefillPerSecond: 1}

	if !limiter.Allow("first", quota).Allowed || !limiter.Allow("first", quota).Allowed {
		t.Fatal("expected initial capacity")
	}
	rejected := limiter.Allow("first", quota)
	if rejected.Allowed || rejected.RetryAfter != time.Second {
		t.Fatalf("unexpected rejection: %+v", rejected)
	}
	saturated := limiter.Allow("second", quota)
	if saturated.Allowed || !saturated.Saturated || len(limiter.entries) != 1 {
		t.Fatalf("expected conservative bounded saturation: %+v entries=%d", saturated, len(limiter.entries))
	}
	now = now.Add(time.Second)
	if !limiter.Allow("first", quota).Allowed {
		t.Fatal("expected one refilled token")
	}
	now = now.Add(time.Minute)
	if !limiter.Allow("second", quota).Allowed || len(limiter.entries) != 1 {
		t.Fatalf("expected idle expiry reclamation, entries=%d", len(limiter.entries))
	}
}

func TestLocalLimiterFixedWindowDoesNotRefillMidWindow(t *testing.T) {
	now := time.Date(2026, 8, 6, 8, 0, 0, 0, time.UTC)
	limiter := NewLocalLimiter(10, time.Minute, WithLocalLimiterClock(func() time.Time { return now }))
	quota := Quota{Capacity: 2, Algorithm: AlgorithmFixedWindow, Window: time.Minute}

	if !limiter.Allow("telemetry", quota).Allowed || !limiter.Allow("telemetry", quota).Allowed {
		t.Fatal("expected configured fixed-window capacity")
	}
	if limiter.Allow("telemetry", quota).Allowed {
		t.Fatal("expected request above fixed-window capacity to be rejected")
	}
	now = now.Add(30 * time.Second)
	if limiter.Allow("telemetry", quota).Allowed {
		t.Fatal("fixed window refilled before its reset boundary")
	}
	now = now.Add(30 * time.Second)
	if !limiter.Allow("telemetry", quota).Allowed {
		t.Fatal("expected a new fixed window at the exact reset boundary")
	}
}

func TestLocalLimiterIndexedExpiryReclaimsOneEntryAndStaysBounded(t *testing.T) {
	now := time.Date(2026, 8, 6, 8, 0, 0, 0, time.UTC)
	const capacity = 10_000
	limiter := NewLocalLimiter(capacity, time.Minute, WithLocalLimiterClock(func() time.Time { return now }))
	quota := Quota{Capacity: 1, RefillPerSecond: 1}

	if !limiter.Allow("oldest", quota).Allowed {
		t.Fatal("expected oldest identity to be admitted")
	}
	now = now.Add(30 * time.Second)
	for index := 1; index < capacity; index++ {
		if !limiter.Allow(strconv.Itoa(index), quota).Allowed {
			t.Fatalf("identity %d was not admitted", index)
		}
	}
	now = now.Add(31 * time.Second)
	if !limiter.Allow("replacement", quota).Allowed {
		t.Fatal("expected indexed reclamation of the one expired identity")
	}
	if len(limiter.entries) != capacity || len(limiter.expiries) != capacity {
		t.Fatalf("capacity changed: entries=%d expiries=%d", len(limiter.entries), len(limiter.expiries))
	}
	if _, exists := limiter.entries["oldest"]; exists {
		t.Fatal("expired identity was not reclaimed")
	}
}

func TestLocalLimiterConcurrentCapacity(t *testing.T) {
	limiter := NewLocalLimiter(10, time.Minute)
	quota := Quota{Capacity: 25, RefillPerSecond: 1.0 / 60}
	var allowed atomic.Int64
	var wait sync.WaitGroup
	for range 100 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if limiter.Allow("same", quota).Allowed {
				allowed.Add(1)
			}
		}()
	}
	wait.Wait()
	if allowed.Load() != 25 {
		t.Fatalf("allowed=%d, want 25", allowed.Load())
	}
}

func TestLocalLimiterConcurrentUniqueIdentitiesNeverExceedCapacity(t *testing.T) {
	const capacity = 64
	limiter := NewLocalLimiter(capacity, time.Minute)
	quota := Quota{Capacity: 1, RefillPerSecond: 1}
	var allowed atomic.Int64
	var wait sync.WaitGroup
	for index := range 1_000 {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			if limiter.Allow(strconv.Itoa(index), quota).Allowed {
				allowed.Add(1)
			}
		}(index)
	}
	wait.Wait()
	if allowed.Load() != capacity {
		t.Fatalf("allowed=%d, want %d", allowed.Load(), capacity)
	}
	if len(limiter.entries) != capacity || len(limiter.expiries) != capacity {
		t.Fatalf("capacity exceeded: entries=%d expiries=%d", len(limiter.entries), len(limiter.expiries))
	}
}
