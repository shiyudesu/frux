package applicationratelimit

import (
	"errors"
	"testing"
	"time"
)

func TestRegistryRejectsInvalidAndUnknownPolicies(t *testing.T) {
	_, err := NewRegistry([]Policy{{
		Name: "invalid", EndpointGroup: "invalid", Identity: IdentityIP,
		Distributed: DistributedRedis, Fallback: FallbackLocal,
		DistributedTimeout: time.Second, RetryAfterMinimum: time.Second,
		Normal:    Profile{Local: Quota{Capacity: 1, RefillPerSecond: 1}},
		Emergency: Profile{Local: Quota{Capacity: 1, RefillPerSecond: 1}},
	}})
	if !errors.Is(err, ErrInvalidPolicy) {
		t.Fatalf("expected invalid policy, got %v", err)
	}
	registry, err := DefaultRegistry(60, 75*time.Millisecond)
	if err != nil {
		t.Fatalf("default registry: %v", err)
	}
	if _, err := registry.Require("client-supplied"); !errors.Is(err, ErrUnknownPolicy) {
		t.Fatalf("expected unknown policy, got %v", err)
	}
}

func TestDefaultRegistryPreservesPlaybackBurstQuota(t *testing.T) {
	registry, err := DefaultRegistry(17, 75*time.Millisecond)
	if err != nil {
		t.Fatalf("default registry: %v", err)
	}
	policy, err := registry.Require(PolicyPlaybackTelemetry)
	if err != nil {
		t.Fatalf("require playback policy: %v", err)
	}
	if policy.Normal.Local.Capacity != 17 ||
		policy.Normal.Local.Algorithm != AlgorithmFixedWindow ||
		policy.Normal.Local.Window != time.Minute {
		t.Fatalf("unexpected playback quota: %+v", policy.Normal.Local)
	}
}

func TestDefaultRegistryUsesRefillableAdminLoginQuotas(t *testing.T) {
	registry, err := DefaultRegistry(60, 75*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := registry.Require(PolicyAdminLogin)
	if err != nil {
		t.Fatal(err)
	}
	for _, quota := range []Quota{
		policy.Normal.Local,
		policy.Normal.Distributed,
		policy.Normal.Fallback,
		policy.Emergency.Local,
		policy.Emergency.Distributed,
		policy.Emergency.Fallback,
	} {
		if quota.Algorithm == AlgorithmFixedWindow || quota.RefillPerSecond <= 0 {
			t.Fatalf("admin login quota is not refillable: %+v", quota)
		}
	}
}

func TestDefaultRegistryAuthenticationPolicies(t *testing.T) {
	registry, err := DefaultRegistry(60, 75*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []PolicyName{PolicyConsumerLogin, PolicySessionRefresh} {
		policy, err := registry.Require(name)
		if err != nil {
			t.Fatal(err)
		}
		if policy.Identity != IdentityIP || policy.Fallback != FallbackLocal ||
			policy.Distributed != DistributedRedis {
			t.Fatalf("%s policy = %+v", name, policy)
		}
	}
	password, err := registry.Require(PolicyPasswordChange)
	if err != nil {
		t.Fatal(err)
	}
	if password.Identity != IdentityUser ||
		password.Fallback != FallbackFailClosed ||
		password.Normal.Local.Algorithm != AlgorithmFixedWindow ||
		password.Normal.Local.Window != 15*time.Minute {
		t.Fatalf("password policy = %+v", password)
	}
}
