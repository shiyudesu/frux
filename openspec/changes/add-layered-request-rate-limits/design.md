## Context

Playback telemetry has a bounded in-process fixed-window limiter, but other endpoints have no shared protection. Frux runs multiple API replicas in production-oriented deployments and already has optional Redis. The limiter must protect the process even when Redis is unavailable and must not call a central HTTP decision service.

## Goals / Non-Goals

**Goals:**

- Define registered endpoint groups and typed quota policies.
- Apply a cheap bounded local guard before optional distributed coordination.
- Produce stable `429` responses and low-cardinality metrics.
- Preserve current playback telemetry limits through migration.

**Non-Goals:**

- Billing quotas, arbitrary runtime expressions, adaptive concurrency control, service mesh installation, or per-video Prometheus labels.
- Guaranteeing an exact global count during partitions.

## Decisions

### Use two enforcement layers

Every protected request first passes a bounded in-memory token bucket keyed by normalized IP, user, or endpoint group. Policies that require cross-instance coordination then execute one Redis Lua token-bucket operation.

### Register policies in typed configuration

Policies define endpoint group, identity dimension, capacity, refill rate, distributed requirement, fallback mode, and retry metadata. Routes reference policy names; browsers cannot supply descriptors.

### Use explicit Redis failure modes

Public reads and telemetry fall back to the local bucket. Expensive authenticated writes use a stricter local fallback. Security-sensitive endpoints may fail closed only when the policy explicitly declares it. No failure mode becomes unlimited traffic.

### Bound memory and normalize client identity

The local store caps entries and expires idle buckets. IP identity uses the existing trusted proxy boundary; untrusted forwarding headers are ignored. Authenticated policies prefer user ID over IP.

### Integrate degradation controls narrowly

A registered governance control may disable distributed coordination or tighten a predeclared emergency profile. It cannot inject arbitrary rates.

## Risks / Trade-offs

- [Local fallback permits aggregate overage across replicas] -> Set conservative fallback rates and expose fallback metrics.
- [Redis becomes a latency source] -> Use one bounded Lua call with a short deadline after the local guard.
- [Attackers create unbounded IP keys] -> Cap entries, expire idle state, and reject new keys conservatively at capacity.
- [Proxy misconfiguration groups or spoofs clients] -> Document trusted proxy configuration and test direct and forwarded requests.

## Migration Plan

1. Implement policy validation and local buckets beside the existing telemetry limiter.
2. Migrate telemetry to the shared local policy and compare behavior.
3. Add Redis coordination for one endpoint group with fallback metrics.
4. Roll out remaining groups and alerts incrementally.
5. Roll back distributed policies to local-only before removing Redis scripts.

## Open Questions

- Which production ingress will own coarse anonymous IP limits before traffic reaches Hertz.
