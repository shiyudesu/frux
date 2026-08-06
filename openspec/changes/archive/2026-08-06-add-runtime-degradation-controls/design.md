## Context

Frux has optional Redis and RabbitMQ behavior in the API process and several natural degraded paths, but no versioned runtime control. A per-request remote governance call would make reliability controls depend on another service. Both API and worker already connect to PostgreSQL, allowing a small control plane with local snapshots.

## Goals / Non-Goals

**Goals:**

- Manage a closed set of boolean degradation controls with revisions, expiry, and rollback.
- Evaluate controls locally in API and worker hot paths.
- Preserve a last-known-good snapshot across temporary control-plane failures.
- Audit every control mutation.

**Non-Goals:**

- Feature experimentation, arbitrary expressions, rate limits, deployment orchestration, or generic application configuration.
- A Web UI.
- Turning mandatory correctness dependencies into optional ones.

## Decisions

### Register controls in code

Each key declares owner, description, default, allowed processes, and failure default. Unknown keys cannot be created through the API. Initial keys target optional work such as recommendation enrichment, preloading, or nonessential projections.

### Store immutable revisions and one active pointer

Every update creates a revision containing enabled state, reason, expiry, actor, and creation time. An active table points to the selected revision using optimistic concurrency. Rollback selects an earlier valid revision rather than editing history.

### Poll into an atomic local snapshot

API and worker poll by revision at a bounded interval and swap a validated in-memory snapshot atomically. Hot paths perform no database or Redis call. Expired revisions resolve to the registered default.

Alternative: Redis Pub/Sub only. Rejected because missed notifications need a durable source of truth and API Redis is optional.

### Use last-known-good with explicit staleness

If polling fails, processes retain the last valid snapshot and emit age/error metrics. Each key declares a maximum stale age; after it is exceeded, evaluation returns the registered failure default.

### Protect writes with permission, expected revision, and audit

Mutations require `governance.execute`, an expected active revision, reason, and optional expiry. State and audit commit together.

## Risks / Trade-offs

- [Polling delays emergency changes] -> Use a short configurable interval and expose applied revision metrics per process.
- [Last-known-good masks control-plane failure] -> Alert on snapshot age and poll failures.
- [Wrong defaults worsen an incident] -> Require each key to document and test normal, enabled, missing, expired, and stale behavior.
- [Control count grows into generic configuration] -> Keep a code review gate and reject unregistered keys.

## Migration Plan

1. Add storage, registry, and read-only snapshot loading with no consumers.
2. Add protected mutation APIs and audit.
3. Integrate one optional capability and validate revision propagation.
4. Add remaining registered controls incrementally.
5. Roll back consumers first; stored revisions are inert without evaluation calls.

## Open Questions

- Whether a future scale stage needs push notification in addition to polling.
