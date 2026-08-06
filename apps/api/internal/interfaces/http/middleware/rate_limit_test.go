package interfaceshttpmiddleware

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	applicationratelimit "github.com/shiyudesu/frux/internal/application/ratelimit"
	interfaceshttpapierror "github.com/shiyudesu/frux/internal/interfaces/http/apierror"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
)

func TestRateLimitMiddlewareRejectsSpoofedIPAndReturnsStableMetadata(t *testing.T) {
	policy := applicationratelimit.Policy{
		Name: "test", EndpointGroup: "test", Identity: applicationratelimit.IdentityIP,
		Distributed:        applicationratelimit.DistributedLocalOnly,
		Fallback:           applicationratelimit.FallbackLocal,
		DistributedTimeout: 50 * time.Millisecond, RetryAfterMinimum: time.Second,
		Normal: applicationratelimit.Profile{
			Local:    applicationratelimit.Quota{Capacity: 1, RefillPerSecond: 1.0 / 60},
			Fallback: applicationratelimit.Quota{Capacity: 1, RefillPerSecond: 1.0 / 60},
		},
		Emergency: applicationratelimit.Profile{
			Local:    applicationratelimit.Quota{Capacity: 1, RefillPerSecond: 1.0 / 60},
			Fallback: applicationratelimit.Quota{Capacity: 1, RefillPerSecond: 1.0 / 60},
		},
	}
	registry, err := applicationratelimit.NewRegistry([]applicationratelimit.Policy{policy})
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	service := applicationratelimit.NewService(
		registry, applicationratelimit.NewLocalLimiter(100, time.Minute),
		nil, nil, nil, time.Minute,
	)
	resolver, err := NewRateLimitIdentityResolver(nil)
	if err != nil {
		t.Fatalf("resolver: %v", err)
	}
	middleware, err := NewRateLimit(service, "test", resolver)
	if err != nil {
		t.Fatalf("middleware: %v", err)
	}
	var handled atomic.Int64
	router := server.New()
	router.GET("/limited", middleware, func(ctx context.Context, c *app.RequestContext) {
		handled.Add(1)
		c.String(http.StatusOK, "ok")
	})
	first := ut.PerformRequest(router.Engine, http.MethodGet, "/limited", nil, ut.Header{Key: "X-Forwarded-For", Value: "198.51.100.1"})
	if first.Code != http.StatusOK {
		t.Fatalf("first status=%d", first.Code)
	}
	second := ut.PerformRequest(router.Engine, http.MethodGet, "/limited", nil, ut.Header{Key: "X-Forwarded-For", Value: "198.51.100.2"})
	if second.Code != http.StatusTooManyRequests || handled.Load() != 1 {
		t.Fatalf("second status=%d handled=%d", second.Code, handled.Load())
	}
	if second.Header().Get("Retry-After") == "" || second.Header().Get("RateLimit-Policy") != "test" {
		t.Fatalf("missing safe retry headers: %v", second.Header())
	}
	var envelope interfaceshttpapierror.Envelope
	if err := json.Unmarshal(second.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Code != interfaceshttpapierror.CodeRateLimited || envelope.RetryAfterSeconds < 1 {
		t.Fatalf("unexpected rate-limit response: %+v", envelope)
	}
}

func TestRateLimitMiddlewareFailsStartupForUnknownPolicy(t *testing.T) {
	registry, err := applicationratelimit.DefaultRegistry(60, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	service := applicationratelimit.NewService(
		registry, applicationratelimit.NewLocalLimiter(100, time.Minute),
		nil, nil, nil, time.Minute,
	)
	resolver, _ := NewRateLimitIdentityResolver(nil)
	if _, err := NewRateLimit(service, "unknown", resolver); err == nil {
		t.Fatal("expected unknown policy startup failure")
	}
}

func TestRateLimitMiddlewareUsesServerUserIdentityAndExplicitFallbacks(t *testing.T) {
	userPolicy := testMiddlewarePolicy("user", applicationratelimit.IdentityUser, applicationratelimit.DistributedLocalOnly, applicationratelimit.FallbackLocal)
	fallbackPolicy := testMiddlewarePolicy("fallback", applicationratelimit.IdentityIP, applicationratelimit.DistributedRedis, applicationratelimit.FallbackLocal)
	failClosedPolicy := testMiddlewarePolicy("fail_closed", applicationratelimit.IdentityIP, applicationratelimit.DistributedRedis, applicationratelimit.FallbackFailClosed)
	registry, err := applicationratelimit.NewRegistry([]applicationratelimit.Policy{userPolicy, fallbackPolicy, failClosedPolicy})
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	service := applicationratelimit.NewService(
		registry, applicationratelimit.NewLocalLimiter(100, time.Minute),
		failingDistributed{}, alwaysDistributedControls{}, nil, time.Minute,
	)
	resolver, _ := NewRateLimitIdentityResolver(nil)
	userRateLimit, _ := NewRateLimit(service, "user", resolver)
	fallbackRateLimit, _ := NewRateLimit(service, "fallback", resolver)
	failClosedRateLimit, _ := NewRateLimit(service, "fail_closed", resolver)

	router := server.New()
	serverIdentity := func(ctx context.Context, c *app.RequestContext) {
		userID, _ := strconv.ParseInt(string(c.GetHeader("X-Test-Server-User")), 10, 64)
		c.Set(ContextUserIDKey, userID)
		c.Next(ctx)
	}
	router.GET("/user", serverIdentity, userRateLimit, func(_ context.Context, c *app.RequestContext) {
		c.String(http.StatusOK, "ok")
	})
	router.GET("/fallback", fallbackRateLimit, func(_ context.Context, c *app.RequestContext) {
		c.String(http.StatusOK, "ok")
	})
	router.GET("/fail-closed", failClosedRateLimit, func(_ context.Context, c *app.RequestContext) {
		c.String(http.StatusOK, "ok")
	})

	userOne := ut.Header{Key: "X-Test-Server-User", Value: "1"}
	userTwo := ut.Header{Key: "X-Test-Server-User", Value: "2"}
	if response := ut.PerformRequest(router.Engine, http.MethodGet, "/user", nil, userOne); response.Code != http.StatusOK {
		t.Fatalf("user one status=%d", response.Code)
	}
	if response := ut.PerformRequest(router.Engine, http.MethodGet, "/user", nil, userTwo); response.Code != http.StatusOK {
		t.Fatalf("user two status=%d", response.Code)
	}
	if response := ut.PerformRequest(router.Engine, http.MethodGet, "/user", nil, userOne); response.Code != http.StatusTooManyRequests {
		t.Fatalf("user one repeat status=%d", response.Code)
	}

	if response := ut.PerformRequest(router.Engine, http.MethodGet, "/fallback", nil); response.Code != http.StatusOK {
		t.Fatalf("fallback first status=%d", response.Code)
	}
	if response := ut.PerformRequest(router.Engine, http.MethodGet, "/fallback", nil); response.Code != http.StatusTooManyRequests {
		t.Fatalf("fallback second status=%d", response.Code)
	}
	if response := ut.PerformRequest(router.Engine, http.MethodGet, "/fail-closed", nil); response.Code != http.StatusServiceUnavailable {
		t.Fatalf("fail-closed status=%d", response.Code)
	}
}

type failingDistributed struct{}

func (failingDistributed) Allow(
	context.Context,
	applicationratelimit.PolicyName,
	string,
	applicationratelimit.Quota,
	time.Duration,
) (applicationratelimit.DistributedDecision, error) {
	return applicationratelimit.DistributedDecision{}, applicationratelimit.ErrDistributedUnavailable
}

type alwaysDistributedControls struct{}

func (alwaysDistributedControls) DistributedEnabled() bool { return true }
func (alwaysDistributedControls) EmergencyEnabled() bool   { return false }

func testMiddlewarePolicy(
	name applicationratelimit.PolicyName,
	identity applicationratelimit.IdentityDimension,
	distributed applicationratelimit.DistributedMode,
	fallback applicationratelimit.FallbackMode,
) applicationratelimit.Policy {
	profile := applicationratelimit.Profile{
		Local:       applicationratelimit.Quota{Capacity: 10, RefillPerSecond: 1.0 / 60},
		Distributed: applicationratelimit.Quota{Capacity: 10, RefillPerSecond: 1.0 / 60},
		Fallback:    applicationratelimit.Quota{Capacity: 1, RefillPerSecond: 1.0 / 60},
	}
	if distributed == applicationratelimit.DistributedLocalOnly {
		profile.Local.Capacity = 1
	}
	return applicationratelimit.Policy{
		Name: name, EndpointGroup: string(name), Identity: identity,
		Distributed: distributed, Fallback: fallback,
		DistributedTimeout: 50 * time.Millisecond, RetryAfterMinimum: time.Second,
		Normal: profile, Emergency: profile,
	}
}
