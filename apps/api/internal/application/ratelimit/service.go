package applicationratelimit

import (
	"context"
	"errors"
	"fmt"
	"time"
)

var ErrDistributedUnavailable = errors.New("distributed rate limiter unavailable")

const (
	LayerLocal       = "local"
	LayerDistributed = "distributed"
	LayerFallback    = "fallback"

	ResultAllow        = "allow"
	ResultReject       = "reject"
	ResultFallback     = "fallback"
	ResultSaturation   = "saturation"
	ResultBackendError = "backend_error"
)

type DistributedDecision struct {
	Allowed    bool
	RetryAfter time.Duration
	Remaining  int
}

type DistributedLimiter interface {
	Allow(ctx context.Context, policy PolicyName, identity string, quota Quota, idleTTL time.Duration) (DistributedDecision, error)
}

type ControlReader interface {
	DistributedEnabled() bool
	EmergencyEnabled() bool
}

type Observer interface {
	Observe(endpointGroup, layer, result string)
}

type DecisionStatus string

const (
	StatusAllowed     DecisionStatus = "allowed"
	StatusRateLimited DecisionStatus = "rate_limited"
	StatusUnavailable DecisionStatus = "unavailable"
)

type Decision struct {
	Status     DecisionStatus
	Group      string
	Limit      int
	Remaining  int
	RetryAfter time.Duration
}

type Service struct {
	registry    *Registry
	local       *LocalLimiter
	distributed DistributedLimiter
	controls    ControlReader
	observer    Observer
	idleTTL     time.Duration
}

func NewService(
	registry *Registry,
	local *LocalLimiter,
	distributed DistributedLimiter,
	controls ControlReader,
	observer Observer,
	idleTTL time.Duration,
) *Service {
	return &Service{
		registry: registry, local: local, distributed: distributed,
		controls: controls, observer: observer, idleTTL: idleTTL,
	}
}

func (s *Service) Policy(name PolicyName) (Policy, error) {
	if s == nil || s.registry == nil {
		return Policy{}, ErrUnknownPolicy
	}
	return s.registry.Require(name)
}

func (s *Service) Enforce(ctx context.Context, name PolicyName, identity string) Decision {
	policy, err := s.Policy(name)
	if err != nil {
		return Decision{Status: StatusUnavailable, RetryAfter: time.Second}
	}
	profile := policy.Normal
	if s.controls != nil && s.controls.EmergencyEnabled() {
		profile = policy.Emergency
	}
	local := s.local.Allow(localKey(policy.Name, LayerLocal, identity), profile.Local)
	if local.Saturated {
		s.observe(policy.EndpointGroup, LayerLocal, ResultSaturation)
	}
	if !local.Allowed {
		s.observe(policy.EndpointGroup, LayerLocal, ResultReject)
		return rejection(policy, profile.Local.Capacity, local.RetryAfter)
	}
	s.observe(policy.EndpointGroup, LayerLocal, ResultAllow)
	if policy.Distributed == DistributedLocalOnly {
		return Decision{
			Status: StatusAllowed, Group: policy.EndpointGroup,
			Limit: profile.Local.Capacity, Remaining: local.Remaining,
		}
	}
	if s.controls != nil && !s.controls.DistributedEnabled() {
		s.observe(policy.EndpointGroup, LayerDistributed, ResultFallback)
		return s.fallback(policy, profile, identity)
	}
	if s.distributed == nil {
		s.observe(policy.EndpointGroup, LayerDistributed, ResultBackendError)
		return s.fallback(policy, profile, identity)
	}
	distributedCtx, cancel := context.WithTimeout(ctx, policy.DistributedTimeout)
	defer cancel()
	distributed, err := s.distributed.Allow(
		distributedCtx, policy.Name, identity, profile.Distributed, s.idleTTL,
	)
	if err != nil {
		s.observe(policy.EndpointGroup, LayerDistributed, ResultBackendError)
		return s.fallback(policy, profile, identity)
	}
	if !distributed.Allowed {
		s.observe(policy.EndpointGroup, LayerDistributed, ResultReject)
		return rejection(policy, profile.Distributed.Capacity, distributed.RetryAfter)
	}
	s.observe(policy.EndpointGroup, LayerDistributed, ResultAllow)
	return Decision{
		Status: StatusAllowed, Group: policy.EndpointGroup,
		Limit: profile.Distributed.Capacity, Remaining: distributed.Remaining,
	}
}

func (s *Service) fallback(policy Policy, profile Profile, identity string) Decision {
	if policy.Fallback == FallbackFailClosed {
		s.observe(policy.EndpointGroup, LayerFallback, ResultReject)
		return Decision{
			Status: StatusUnavailable, Group: policy.EndpointGroup,
			Limit: profile.Distributed.Capacity, RetryAfter: policy.RetryAfterMinimum,
		}
	}
	local := s.local.Allow(localKey(policy.Name, LayerFallback, identity), profile.Fallback)
	if local.Saturated {
		s.observe(policy.EndpointGroup, LayerFallback, ResultSaturation)
	}
	if !local.Allowed {
		s.observe(policy.EndpointGroup, LayerFallback, ResultReject)
		return rejection(policy, profile.Fallback.Capacity, local.RetryAfter)
	}
	s.observe(policy.EndpointGroup, LayerFallback, ResultFallback)
	return Decision{
		Status: StatusAllowed, Group: policy.EndpointGroup,
		Limit: profile.Fallback.Capacity, Remaining: local.Remaining,
	}
}

func (s *Service) observe(group, layer, result string) {
	if s.observer != nil {
		s.observer.Observe(group, layer, result)
	}
}

func rejection(policy Policy, limit int, retry time.Duration) Decision {
	if retry < policy.RetryAfterMinimum {
		retry = policy.RetryAfterMinimum
	}
	return Decision{
		Status: StatusRateLimited, Group: policy.EndpointGroup,
		Limit: limit, Remaining: 0, RetryAfter: retry,
	}
}

func localKey(policy PolicyName, layer, identity string) string {
	return fmt.Sprintf("%s:%s:%s", policy, layer, identity)
}
