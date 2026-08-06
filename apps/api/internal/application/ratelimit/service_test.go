package applicationratelimit

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type testDistributedLimiter struct {
	mu             sync.Mutex
	calls          int
	decision       DistributedDecision
	err            error
	waitForContext bool
}

func (l *testDistributedLimiter) Allow(ctx context.Context, _ PolicyName, _ string, _ Quota, _ time.Duration) (DistributedDecision, error) {
	l.mu.Lock()
	l.calls++
	l.mu.Unlock()
	if l.waitForContext {
		<-ctx.Done()
		return DistributedDecision{}, ctx.Err()
	}
	return l.decision, l.err
}

type testControls struct {
	distributed bool
	emergency   bool
}

func (c testControls) DistributedEnabled() bool { return c.distributed }
func (c testControls) EmergencyEnabled() bool   { return c.emergency }

func TestServiceIsLocalFirstAndFallsBackExplicitly(t *testing.T) {
	registry, err := NewRegistry([]Policy{testPolicy(FallbackLocal, 2)})
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	distributed := &testDistributedLimiter{err: errors.New("redis down")}
	service := NewService(
		registry, NewLocalLimiter(100, time.Minute), distributed,
		testControls{distributed: true}, nil, time.Minute,
	)
	if decision := service.Enforce(context.Background(), "test", "identity"); decision.Status != StatusAllowed {
		t.Fatalf("fallback should allow first request: %+v", decision)
	}
	if decision := service.Enforce(context.Background(), "test", "identity"); decision.Status != StatusRateLimited {
		t.Fatalf("fallback quota should reject second request: %+v", decision)
	}
	if decision := service.Enforce(context.Background(), "test", "identity"); decision.Status != StatusRateLimited {
		t.Fatalf("local quota should reject third request: %+v", decision)
	}
	if distributed.calls != 2 {
		t.Fatalf("local rejection called Redis: calls=%d", distributed.calls)
	}
}

func TestServiceFailClosedAndDeadline(t *testing.T) {
	policy := testPolicy(FallbackFailClosed, 10)
	policy.DistributedTimeout = 10 * time.Millisecond
	registry, err := NewRegistry([]Policy{policy})
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	distributed := &testDistributedLimiter{waitForContext: true}
	service := NewService(
		registry, NewLocalLimiter(100, time.Minute), distributed,
		testControls{distributed: true}, nil, time.Minute,
	)
	start := time.Now()
	decision := service.Enforce(context.Background(), "test", "identity")
	if decision.Status != StatusUnavailable {
		t.Fatalf("expected fail closed, got %+v", decision)
	}
	if time.Since(start) > 250*time.Millisecond {
		t.Fatal("distributed limiter did not honor short deadline")
	}
}

func TestServiceUsesOnlyPredeclaredEmergencyProfile(t *testing.T) {
	registry, err := NewRegistry([]Policy{testPolicy(FallbackLocal, 3)})
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	service := NewService(
		registry, NewLocalLimiter(100, time.Minute), nil,
		testControls{distributed: false, emergency: true}, nil, time.Minute,
	)
	if !isAllowed(service.Enforce(context.Background(), "test", "identity")) ||
		isAllowed(service.Enforce(context.Background(), "test", "identity")) {
		t.Fatal("emergency profile capacity was not applied")
	}
}

func isAllowed(decision Decision) bool { return decision.Status == StatusAllowed }

func testPolicy(fallback FallbackMode, localCapacity int) Policy {
	return Policy{
		Name: "test", EndpointGroup: "test", Identity: IdentityIP,
		Distributed: DistributedRedis, Fallback: fallback,
		DistributedTimeout: 50 * time.Millisecond, RetryAfterMinimum: time.Second,
		Normal: Profile{
			Local:       Quota{Capacity: localCapacity, RefillPerSecond: 1.0 / 60},
			Distributed: Quota{Capacity: 10, RefillPerSecond: 1},
			Fallback:    Quota{Capacity: 1, RefillPerSecond: 1.0 / 60},
		},
		Emergency: Profile{
			Local:       Quota{Capacity: 1, RefillPerSecond: 1.0 / 60},
			Distributed: Quota{Capacity: 2, RefillPerSecond: 1.0 / 60},
			Fallback:    Quota{Capacity: 1, RefillPerSecond: 1.0 / 60},
		},
	}
}
