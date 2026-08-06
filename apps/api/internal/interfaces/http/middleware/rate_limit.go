package interfaceshttpmiddleware

import (
	"context"
	"errors"
	"math"
	"net/http"
	"strconv"
	"time"

	applicationratelimit "github.com/shiyudesu/frux/internal/application/ratelimit"
	interfaceshttpapierror "github.com/shiyudesu/frux/internal/interfaces/http/apierror"

	"github.com/cloudwego/hertz/pkg/app"
)

var ErrInvalidRateLimitMiddleware = errors.New("invalid rate-limit middleware")

type RateLimitOption func(*rateLimitMiddleware)

type rateLimitMiddleware struct {
	service    *applicationratelimit.Service
	policy     applicationratelimit.Policy
	resolver   *RateLimitIdentityResolver
	rejectHook func()
}

func WithRateLimitRejectHook(hook func()) RateLimitOption {
	return func(middleware *rateLimitMiddleware) {
		middleware.rejectHook = hook
	}
}

func NewRateLimit(
	service *applicationratelimit.Service,
	name applicationratelimit.PolicyName,
	resolver *RateLimitIdentityResolver,
	options ...RateLimitOption,
) (app.HandlerFunc, error) {
	if service == nil || resolver == nil {
		return nil, ErrInvalidRateLimitMiddleware
	}
	policy, err := service.Policy(name)
	if err != nil {
		return nil, err
	}
	middleware := &rateLimitMiddleware{service: service, policy: policy, resolver: resolver}
	for _, option := range options {
		if option != nil {
			option(middleware)
		}
	}
	return middleware.handle, nil
}

func (m *rateLimitMiddleware) handle(ctx context.Context, c *app.RequestContext) {
	identity, ok := m.resolver.Resolve(c, m.policy.Identity)
	if !ok {
		if m.policy.Identity == applicationratelimit.IdentityUser {
			interfaceshttpapierror.AbortInvalidAccessToken(c)
			return
		}
		interfaceshttpapierror.Abort(c, http.StatusServiceUnavailable, interfaceshttpapierror.CodeRateLimitUnavailable, "rate limit identity unavailable")
		return
	}
	decision := m.service.Enforce(ctx, m.policy.Name, identity)
	writeRateLimitHeaders(c, decision)
	switch decision.Status {
	case applicationratelimit.StatusAllowed:
		c.Next(ctx)
	case applicationratelimit.StatusRateLimited:
		if m.rejectHook != nil {
			m.rejectHook()
		}
		interfaceshttpapierror.AbortRateLimited(c, retrySeconds(decision.RetryAfter))
	default:
		interfaceshttpapierror.AbortRateLimitUnavailable(c, retrySeconds(decision.RetryAfter))
	}
}

func writeRateLimitHeaders(c *app.RequestContext, decision applicationratelimit.Decision) {
	c.Header("RateLimit-Policy", decision.Group)
	c.Header("RateLimit-Limit", strconv.Itoa(decision.Limit))
	c.Header("RateLimit-Remaining", strconv.Itoa(decision.Remaining))
	if decision.Status != applicationratelimit.StatusAllowed {
		c.Header("Retry-After", strconv.Itoa(retrySeconds(decision.RetryAfter)))
		c.Header("Cache-Control", "no-store")
	}
}

func retrySeconds(duration time.Duration) int {
	seconds := int(math.Ceil(duration.Seconds()))
	if seconds < 1 {
		return 1
	}
	return seconds
}
