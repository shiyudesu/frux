package applicationratelimit

import (
	"container/heap"
	"math"
	"strings"
	"sync"
	"time"
)

type LocalDecision struct {
	Allowed    bool
	RetryAfter time.Duration
	Remaining  int
	Saturated  bool
}

type localEntry struct {
	key        string
	tokens     float64
	refilledAt time.Time
	lastSeen   time.Time
	expiresAt  time.Time
	windowUsed int
	windowEnds time.Time
	heapIndex  int
}

type localExpiryHeap []*localEntry

func (h localExpiryHeap) Len() int { return len(h) }

func (h localExpiryHeap) Less(left, right int) bool {
	return h[left].expiresAt.Before(h[right].expiresAt)
}

func (h localExpiryHeap) Swap(left, right int) {
	h[left], h[right] = h[right], h[left]
	h[left].heapIndex = left
	h[right].heapIndex = right
}

func (h *localExpiryHeap) Push(value any) {
	entry := value.(*localEntry)
	entry.heapIndex = len(*h)
	*h = append(*h, entry)
}

func (h *localExpiryHeap) Pop() any {
	old := *h
	last := len(old) - 1
	entry := old[last]
	old[last] = nil
	entry.heapIndex = -1
	*h = old[:last]
	return entry
}

type LocalLimiter struct {
	mu         sync.Mutex
	maxEntries int
	idleTTL    time.Duration
	entries    map[string]*localEntry
	expiries   localExpiryHeap
	now        func() time.Time
}

type LocalLimiterOption func(*LocalLimiter)

func WithLocalLimiterClock(now func() time.Time) LocalLimiterOption {
	return func(limiter *LocalLimiter) {
		if now != nil {
			limiter.now = now
		}
	}
}

func NewLocalLimiter(maxEntries int, idleTTL time.Duration, options ...LocalLimiterOption) *LocalLimiter {
	limiter := &LocalLimiter{
		maxEntries: maxEntries,
		idleTTL:    idleTTL,
		entries:    make(map[string]*localEntry),
		now:        time.Now,
	}
	for _, option := range options {
		if option != nil {
			option(limiter)
		}
	}
	return limiter
}

func (l *LocalLimiter) Allow(key string, quota Quota) LocalDecision {
	if l == nil || strings.TrimSpace(key) == "" || l.maxEntries <= 0 || l.idleTTL <= 0 || !validQuota(quota) {
		return LocalDecision{RetryAfter: time.Second}
	}
	now := l.now().UTC()
	l.mu.Lock()
	defer l.mu.Unlock()

	entry, exists := l.entries[key]
	if exists && !now.Before(entry.expiresAt) {
		l.remove(entry)
		exists = false
	}
	if !exists {
		if len(l.entries) >= l.maxEntries {
			l.reclaimExpired(now, 1)
		}
		if len(l.entries) >= l.maxEntries {
			return LocalDecision{RetryAfter: l.idleTTL, Saturated: true}
		}
		entry = &localEntry{
			key: key, tokens: float64(quota.Capacity), refilledAt: now,
			lastSeen: now, expiresAt: now.Add(l.idleTTL), heapIndex: -1,
		}
		l.entries[key] = entry
		heap.Push(&l.expiries, entry)
	}

	var decision LocalDecision
	if quota.Algorithm == AlgorithmFixedWindow {
		decision = allowFixedWindow(entry, quota, now)
	} else {
		decision = allowTokenBucket(entry, quota, now)
	}
	l.touch(entry, now)
	return decision
}

func allowTokenBucket(entry *localEntry, quota Quota, now time.Time) LocalDecision {
	elapsed := now.Sub(entry.refilledAt).Seconds()
	if elapsed > 0 {
		entry.tokens = math.Min(float64(quota.Capacity), entry.tokens+elapsed*quota.RefillPerSecond)
		entry.refilledAt = now
	}
	if entry.tokens > float64(quota.Capacity) {
		entry.tokens = float64(quota.Capacity)
	}
	entry.lastSeen = now
	if entry.tokens < 1 {
		missing := 1 - entry.tokens
		retry := time.Duration(math.Ceil(missing/quota.RefillPerSecond*1000)) * time.Millisecond
		if retry < time.Millisecond {
			retry = time.Millisecond
		}
		return LocalDecision{RetryAfter: retry, Remaining: 0}
	}
	entry.tokens--
	return LocalDecision{Allowed: true, Remaining: int(math.Floor(entry.tokens))}
}

func allowFixedWindow(entry *localEntry, quota Quota, now time.Time) LocalDecision {
	if entry.windowEnds.IsZero() || !now.Before(entry.windowEnds) {
		entry.windowUsed = 0
		entry.windowEnds = now.Add(quota.Window)
	}
	if entry.windowUsed >= quota.Capacity {
		return LocalDecision{RetryAfter: entry.windowEnds.Sub(now)}
	}
	entry.windowUsed++
	return LocalDecision{Allowed: true, Remaining: quota.Capacity - entry.windowUsed}
}

func (l *LocalLimiter) touch(entry *localEntry, now time.Time) {
	entry.lastSeen = now
	entry.expiresAt = now.Add(l.idleTTL)
	heap.Fix(&l.expiries, entry.heapIndex)
}

func (l *LocalLimiter) reclaimExpired(now time.Time, limit int) {
	for reclaimed := 0; reclaimed < limit && l.expiries.Len() > 0; reclaimed++ {
		entry := l.expiries[0]
		if now.Before(entry.expiresAt) {
			return
		}
		l.remove(entry)
	}
}

func (l *LocalLimiter) remove(entry *localEntry) {
	delete(l.entries, entry.key)
	heap.Remove(&l.expiries, entry.heapIndex)
}
