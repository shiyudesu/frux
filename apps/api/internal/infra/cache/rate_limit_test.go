package infracache

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	applicationratelimit "github.com/shiyudesu/frux/internal/application/ratelimit"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestRedisRateLimiterCoordinatesAcrossInstancesAndExpires(t *testing.T) {
	server, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis: %v", err)
	}
	t.Cleanup(server.Close)
	clientA := redis.NewClient(&redis.Options{Addr: server.Addr()})
	clientB := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() {
		_ = clientA.Close()
		_ = clientB.Close()
	})
	limiters := []*RedisRateLimiter{NewRedisRateLimiter(clientA), NewRedisRateLimiter(clientB)}
	quota := applicationratelimit.Quota{Capacity: 10, RefillPerSecond: 1.0 / 60}
	var allowed atomic.Int64
	var wait sync.WaitGroup
	for index := range 40 {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			decision, callErr := limiters[index%2].Allow(
				context.Background(), applicationratelimit.PolicyPublicSearch,
				"shared-client", quota, time.Minute,
			)
			if callErr != nil {
				t.Errorf("allow: %v", callErr)
				return
			}
			if decision.Allowed {
				allowed.Add(1)
			}
		}(index)
	}
	wait.Wait()
	if allowed.Load() != 10 {
		t.Fatalf("allowed=%d, want 10", allowed.Load())
	}

	server.FastForward(time.Minute)
	if len(server.Keys()) != 0 {
		t.Fatalf("expected bucket expiry, keys=%v", server.Keys())
	}
}

func TestRedisRateLimiterReturnsScriptErrors(t *testing.T) {
	limiter := NewRedisRateLimiter(errorEvaler{})
	_, err := limiter.Allow(
		context.Background(), applicationratelimit.PolicyPublicSearch, "identity",
		applicationratelimit.Quota{Capacity: 1, RefillPerSecond: 1}, time.Minute,
	)
	if err == nil {
		t.Fatal("expected script error")
	}
}

func TestRedisRateLimiterExecutesMutatingScriptOncePerAllow(t *testing.T) {
	evaler := &countingEvaler{}
	limiter := NewRedisRateLimiter(evaler)
	decision, err := limiter.Allow(
		context.Background(), applicationratelimit.PolicyPublicSearch, "identity",
		applicationratelimit.Quota{Capacity: 1, RefillPerSecond: 1}, time.Minute,
	)
	if err != nil {
		t.Fatalf("allow: %v", err)
	}
	if !decision.Allowed || evaler.calls.Load() != 1 {
		t.Fatalf("decision=%+v calls=%d", decision, evaler.calls.Load())
	}
}

type errorEvaler struct{}

func (errorEvaler) Eval(context.Context, string, []string, ...any) *redis.Cmd {
	command := redis.NewCmd(context.Background())
	command.SetErr(errors.New("script failure"))
	return command
}

type countingEvaler struct {
	calls atomic.Int64
}

func (e *countingEvaler) Eval(context.Context, string, []string, ...any) *redis.Cmd {
	e.calls.Add(1)
	command := redis.NewCmd(context.Background())
	command.SetVal([]any{int64(1), int64(0), int64(0)})
	return command
}
