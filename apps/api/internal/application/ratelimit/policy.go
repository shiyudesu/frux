package applicationratelimit

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrUnknownPolicy = errors.New("unknown rate-limit policy")
	ErrInvalidPolicy = errors.New("invalid rate-limit policy")
)

type PolicyName string
type IdentityDimension string
type DistributedMode string
type FallbackMode string
type Algorithm string

const (
	PolicyPlaybackTelemetry PolicyName = "playback_telemetry"
	PolicyPublicSearch      PolicyName = "public_search"
	PolicyUploadSession     PolicyName = "upload_session"

	IdentityIP   IdentityDimension = "ip"
	IdentityUser IdentityDimension = "user"

	DistributedLocalOnly DistributedMode = "local_only"
	DistributedRedis     DistributedMode = "redis"

	FallbackLocal      FallbackMode = "local"
	FallbackFailClosed FallbackMode = "fail_closed"

	AlgorithmTokenBucket Algorithm = "token_bucket"
	AlgorithmFixedWindow Algorithm = "fixed_window"
)

const (
	minCapacity           = 1
	maxCapacity           = 100_000
	minRefillPerSecond    = 1.0 / 3600
	maxRefillPerSecond    = 100_000
	minDistributedTimeout = 10 * time.Millisecond
	maxDistributedTimeout = 500 * time.Millisecond
	minRetryAfter         = time.Second
	maxRetryAfter         = time.Hour
)

type Quota struct {
	Capacity        int
	RefillPerSecond float64
	Algorithm       Algorithm
	Window          time.Duration
}

type Profile struct {
	Local       Quota
	Distributed Quota
	Fallback    Quota
}

type Policy struct {
	Name               PolicyName
	EndpointGroup      string
	Identity           IdentityDimension
	Distributed        DistributedMode
	Fallback           FallbackMode
	DistributedTimeout time.Duration
	RetryAfterMinimum  time.Duration
	Normal             Profile
	Emergency          Profile
}

type Registry struct {
	policies map[PolicyName]Policy
}

func NewRegistry(policies []Policy) (*Registry, error) {
	registry := &Registry{policies: make(map[PolicyName]Policy, len(policies))}
	for _, policy := range policies {
		policy.Name = PolicyName(strings.TrimSpace(string(policy.Name)))
		policy.EndpointGroup = strings.TrimSpace(policy.EndpointGroup)
		if err := validatePolicy(policy); err != nil {
			return nil, fmt.Errorf("%w: %s", err, policy.Name)
		}
		if _, exists := registry.policies[policy.Name]; exists {
			return nil, fmt.Errorf("%w: duplicate %s", ErrInvalidPolicy, policy.Name)
		}
		registry.policies[policy.Name] = policy
	}
	return registry, nil
}

func DefaultRegistry(playbackBatchesPerMinute int, distributedTimeout time.Duration) (*Registry, error) {
	if playbackBatchesPerMinute <= 0 {
		playbackBatchesPerMinute = 60
	}
	playbackQuota := Quota{
		Capacity:  playbackBatchesPerMinute,
		Algorithm: AlgorithmFixedWindow,
		Window:    time.Minute,
	}
	return NewRegistry([]Policy{
		{
			Name: PolicyPlaybackTelemetry, EndpointGroup: string(PolicyPlaybackTelemetry),
			Identity: IdentityUser, Distributed: DistributedLocalOnly, Fallback: FallbackLocal,
			DistributedTimeout: distributedTimeout, RetryAfterMinimum: time.Second,
			Normal: Profile{Local: playbackQuota, Fallback: playbackQuota},
			Emergency: Profile{
				Local: Quota{
					Capacity:  maxInt(1, playbackBatchesPerMinute/2),
					Algorithm: AlgorithmFixedWindow, Window: time.Minute,
				},
				Fallback: Quota{
					Capacity:  maxInt(1, playbackBatchesPerMinute/2),
					Algorithm: AlgorithmFixedWindow, Window: time.Minute,
				},
			},
		},
		{
			Name: PolicyPublicSearch, EndpointGroup: string(PolicyPublicSearch),
			Identity: IdentityIP, Distributed: DistributedRedis, Fallback: FallbackLocal,
			DistributedTimeout: distributedTimeout, RetryAfterMinimum: time.Second,
			Normal: Profile{
				Local:       Quota{Capacity: 60, RefillPerSecond: 1},
				Distributed: Quota{Capacity: 120, RefillPerSecond: 2},
				Fallback:    Quota{Capacity: 20, RefillPerSecond: 1.0 / 3},
			},
			Emergency: Profile{
				Local:       Quota{Capacity: 15, RefillPerSecond: 0.25},
				Distributed: Quota{Capacity: 30, RefillPerSecond: 0.5},
				Fallback:    Quota{Capacity: 5, RefillPerSecond: 1.0 / 12},
			},
		},
		{
			Name: PolicyUploadSession, EndpointGroup: string(PolicyUploadSession),
			Identity: IdentityUser, Distributed: DistributedRedis, Fallback: FallbackFailClosed,
			DistributedTimeout: distributedTimeout, RetryAfterMinimum: time.Second,
			Normal: Profile{
				Local:       Quota{Capacity: 20, RefillPerSecond: 1.0 / 3},
				Distributed: Quota{Capacity: 30, RefillPerSecond: 0.5},
			},
			Emergency: Profile{
				Local:       Quota{Capacity: 5, RefillPerSecond: 1.0 / 12},
				Distributed: Quota{Capacity: 8, RefillPerSecond: 2.0 / 15},
			},
		},
	})
}

func (r *Registry) Require(name PolicyName) (Policy, error) {
	if r == nil {
		return Policy{}, ErrUnknownPolicy
	}
	policy, ok := r.policies[PolicyName(strings.TrimSpace(string(name)))]
	if !ok {
		return Policy{}, fmt.Errorf("%w: %s", ErrUnknownPolicy, name)
	}
	return policy, nil
}

func validatePolicy(policy Policy) error {
	if policy.Name == "" || policy.EndpointGroup == "" {
		return ErrInvalidPolicy
	}
	if policy.Identity != IdentityIP && policy.Identity != IdentityUser {
		return ErrInvalidPolicy
	}
	if policy.Distributed != DistributedLocalOnly && policy.Distributed != DistributedRedis {
		return ErrInvalidPolicy
	}
	if policy.Fallback != FallbackLocal && policy.Fallback != FallbackFailClosed {
		return ErrInvalidPolicy
	}
	if policy.DistributedTimeout < minDistributedTimeout || policy.DistributedTimeout > maxDistributedTimeout {
		return ErrInvalidPolicy
	}
	if policy.RetryAfterMinimum < minRetryAfter || policy.RetryAfterMinimum > maxRetryAfter {
		return ErrInvalidPolicy
	}
	if !validQuota(policy.Normal.Local) || !validQuota(policy.Emergency.Local) {
		return ErrInvalidPolicy
	}
	if policy.Distributed == DistributedRedis {
		if !validQuota(policy.Normal.Distributed) || !validQuota(policy.Emergency.Distributed) {
			return ErrInvalidPolicy
		}
	}
	if policy.Fallback == FallbackLocal {
		if !validQuota(policy.Normal.Fallback) || !validQuota(policy.Emergency.Fallback) {
			return ErrInvalidPolicy
		}
	}
	return nil
}

func validQuota(quota Quota) bool {
	if quota.Capacity < minCapacity || quota.Capacity > maxCapacity {
		return false
	}
	switch quota.Algorithm {
	case "", AlgorithmTokenBucket:
		return quota.Window == 0 &&
			quota.RefillPerSecond >= minRefillPerSecond &&
			quota.RefillPerSecond <= maxRefillPerSecond
	case AlgorithmFixedWindow:
		return quota.RefillPerSecond == 0 &&
			quota.Window >= time.Second &&
			quota.Window <= time.Hour
	default:
		return false
	}
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}
