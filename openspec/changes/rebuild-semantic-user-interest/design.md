## Context

The narrowed `project-semantic-user-interest` change creates a live-only shadow projection isolated by user and exact provider/model/revision. It defines `semantic-interest-v1`, event-time text-hash/vector-digest validation, eligible behavior/action/feedback facts, fixed weights, canonical full-user reduction, immutable semantic event identity, and a stable exact-user/model advisory-lock namespace. It deliberately leaves historical facts, disabled intervals, and optional completeness repair to this change.

This change depends explicitly on:

- `integrate-semantic-video-embeddings`, which owns the exact revision-bearing model key, persisted normalized vector contract, and dimension validation;
- `backfill-semantic-video-embeddings`, which can repair historical exact-model video coverage and defines the operational boundary for vectors that are still missing;
- the narrowed `project-semantic-user-interest`, which owns all semantic profile and live projection semantics reused here.

Historical reconstruction is an optional operator capability, not a deployment prerequisite. Small-user installations may skip it and let live semantic events establish profiles naturally. When invoked, it must tolerate interruption, late and out-of-order source facts, incomplete event-time vector coverage, and concurrent live projection. It must never infer embeddings, bind old behavior to newer content, invent author content, publish a silently partial profile, mark a missing-vector fact applied, or replace live state without preserving newer live contributions.

## Goals / Non-Goals

**Goals:**

- Provide an operator-only, bounded, deterministic, resumable command for one exact supported semantic model.
- Select candidate users and their immutable durable facts in stable keyset pages behind a captured multi-source high-water fence.
- Reuse the live projector's eligibility, fixed weights, immutable provider/model/revision/text-hash/vector-digest event identity, canonical ordering, event-time decay, one-final-clamp reducer, schema, and vector validation.
- Finalize one user atomically while preserving every live event applied before lock acquisition and allowing later live events to continue normally.
- Defer users with missing, ambiguous, or invalid event-time embeddings without changing their profile or semantic event ledger.
- Support dry-run, maximum user/event/runtime limits, cancellation, restart, idempotency, exact-model force guards, bounded observability, and safe summaries.
- Remain absent from normal startup and safely skippable when natural profile growth is sufficient.

**Non-Goals:**

- Changing live handoff, leased retry, event classification, weights, decay, profile schema, or live worker behavior.
- Generating or refreshing video embeddings; operators use `backfill-semantic-video-embeddings` for that prerequisite.
- Adding pgvector, ANN indexes or queries, semantic recall, ranking or policy inputs, request-path reads, training, or recommendation behavior changes.
- Projecting follow, unfollow, `reduce_author`, author catalogs, or synthetic author-affinity vectors.
- Adding a public/admin HTTP API, Web UI, scheduler, recurring worker, Kafka flow, or Redis state.
- Requiring every deployment or every user population to run historical rebuild.

## Decisions

### 1. Add a dedicated one-shot Go command with fixed model descriptors

Add `cmd/rebuild-semantic-user-interest` as an optional operations binary. It loads PostgreSQL and recommendation configuration, validates that the selected descriptor is a statically registered model/profile pair, composes the rebuild runner, optionally exposes an internal metrics listener, emits one final summary, and exits. It does not initialize Redis, Kafka, the Python embedding service, or live workers and does not run migrations. No normal Compose service, API startup, or worker startup depends on this command.

The initial accepted identity is the fixed embedding provider plus model `semantic-minilm-l12-v2`, revision `e8f8c211226b894f`, profile schema `semantic-interest-v1`, and dimension 384. The command accepts an exact registered identity but never an arbitrary provider, model, revision, dimension, schema, or dynamic rule set. Implementation is gated by live projection and versioned embedding contracts; historical video backfill is an optional prerequisite only for runs whose missing identities can be reproduced.

Alternative considered: add a rebuild mode to `cmd/worker`. Rejected because a bounded operator mutation must not share automatic startup, cancellation, or dependency lifecycle with live consumers.

### 2. Use durable PostgreSQL run state as the checkpoint

Add rebuild-owned persistence for:

- a run row containing opaque run ID, exact model/schema, options compatibility hash, source high-water fence, user-selection horizon, last completed user cursor, status, lease owner/expiry, safe counters, and timestamps;
- per-run deferred-user rows containing user ID, bounded reason, missing-vector count, attempt count, and next eligibility time;
- exact-model rebuild coverage containing the last successfully reconstructed source fence and committed profile version per user/model/schema.

A fresh non-dry run creates its run and captures its fence atomically. Restart requires the run ID and compatible model, mode, and semantic rules; incompatible or corrupt state fails closed. One command holds a renewable run lease so the same run cannot be advanced concurrently.

The user cursor advances only after every user in the page has a durable terminal page outcome: committed, already current, no eligible facts, or durably deferred. The cursor update and page counters commit atomically. A crash before cursor advancement replays at most one bounded page; one-user idempotency absorbs already committed work. Dry-run creates or mutates no run, coverage, deferred, profile, or ledger rows.

Alternative considered: a local checkpoint file. Rejected because profile writes and deferred records are already in PostgreSQL; durable database state gives stronger atomic compatibility checks and works consistently in containers.

### 3. Capture a multi-source snapshot fence and page users deterministically

At fresh-run creation, one repeatable-read transaction captures the maximum immutable primary-key ID for each eligible source namespace (`behavior`, `action`, and `feedback`) and the greatest candidate user ID visible through those fences. The resulting versioned fence is fixed for the run.

Candidate users are the distinct positive user IDs present in any potentially eligible source row at or below its namespace fence. They are ordered by `user_id ASC`, constrained to the captured user horizon, and paged strictly after the checkpoint cursor. Facts for one user are normalized from all three namespaces and ordered by:

`(occurred_at ASC, source_kind_rank ASC, source_event_id ASC)`,

where source-kind rank is fixed by `semantic-interest-v1`. Fact pages use the same tuple as an opaque internal cursor. The normalizer invokes the shared live eligibility and payload-hash code, so unsupported, inactive, malformed, below-threshold, and author-only facts do not contribute. Each contributing fact must also resolve the provider/model/revision, canonical semantic text hash, and vector digest that represented the video for that event. Current content identity is never used as a substitute for an older unresolved fact.

Source IDs, not processing timestamps, define the high-water fence. Occurrence time controls decay only. This allows a fact recorded late with an old occurrence time to remain deterministically included when its durable ID is within the fence.

Alternative considered: scan current profiles or semantic outbox rows. Rejected because neither is a complete history of disabled intervals, and live outbox retention is intentionally bounded.

### 4. Reconstruct only complete event-time identities and defer ambiguity

The runner first reuses any immutable semantic event rows already bound by live projection. For historical facts not yet in that ledger, it resolves a persisted embedding only when provider, model, revision, canonical event-time text hash, and vector digest can be proven. Reads are bounded and deduplicated by exact embedding identity, not merely video ID. Every source vector must match the selected identity, dimension, finiteness, normalization, and digest contract. The runner uses the shared semantic reducer to compute long-term, recent, and negative vectors from zero in canonical fact order and clamps only after the complete reduction.

If any contributing fact lacks a provable event-time identity or valid exact vector, exceeds the per-user event bound, or cannot be safely normalized, the user is durably deferred with bounded reason `missing_embedding_identity`, `missing_embedding`, `invalid_embedding`, `event_limit`, or `invalid_fact`. No replacement profile, coverage row, or new semantic event row is written for that user. Current embeddings with another text hash/digest are ignored.

After the primary user scan reaches its horizon, the command may revisit deferred users in stable `user_id` order while user/event/runtime budgets remain. Missing rows are never marked applied. Operators first run the video backfill when coverage metrics show a material vector gap.

Alternative considered: publish a partial profile and mark it incomplete. Rejected because online consumers could later mistake it for complete and subsequent recovery would require subtracting/replaying partial contributions.

### 5. Share the canonical semantic reducer rather than duplicate projection rules

Historical classification and reduction call the same application/domain functions used by live projection for:

- completion, sustained progress, LIKE, FAVORITE, early skip, `not_interested`, and `already_seen` eligibility;
- completion precedence, destination vectors, fixed weights, payload hashes, and event-time embedding identity;
- the fixed source ordering and tie-breaker;
- 30-day long-term and 24-hour recent/negative half-lives;
- direct decay to the common materialization anchor, one final component clamp, and dimension checks.

The rebuild materialization time is the maximum occurrence time among included facts, or the existing schema-defined zero state when there are no contributing facts. Processing time, scan page, retry attempt, restart time, and current video content never enter the vectors. Tests compare reconstruction with live rematerialization of the same immutable event rows after canonical, delayed, and shuffled delivery.

Alternative considered: replay arrivals and clamp after each event. Rejected because delivery order and rebuild order can disagree. A shared canonical full-user reduction removes that divergence.

### 6. Finalize each user with a bounded live catch-up under the shared lock

Baseline fact loading and vector reduction occur outside the profile transaction. Finalization then:

1. starts a transaction and acquires the exact shared `(user_id, model)` advisory lock;
2. locks the current semantic profile and reads its current version plus exact-model semantic event rows;
3. captures current per-source maxima as a user catch-up fence;
4. loads all eligible user facts after the run fence through that catch-up fence, plus resolves every existing exact-model event identity not already represented by the baseline;
5. aborts without profile changes when catch-up facts/ledgers exceed the bounded catch-up allowance, conflict by payload hash, cannot be resolved, or require a missing/invalid vector;
6. locks all referenced event-time embedding evidence in stable identity order and verifies provider/model/revision/text hash/vector digest;
7. recomputes the complete profile from zero with baseline plus catch-up facts through the same canonical reducer and one final clamp;
8. conditionally replaces/upserts the profile at `locked_current_version + 1`, upserts matching immutable semantic event rows, records rebuild coverage, clears the user's deferral, and commits atomically.

The current profile is never overwritten from an earlier observed version. Every live application committed before rebuild lock acquisition is either represented by its durable source fact and ledger or causes finalization to abort. A live projector that reaches the lock later waits and then applies normally to the rebuilt profile. Facts committed after the captured catch-up maxima cannot have been live-applied before this transaction because live apply requires the same lock.

Catch-up work is bounded by both the run's remaining maximum-events budget and a configured per-user catch-up cap. A hot user that exceeds it is deferred without mutation and can be retried in a later run.

Alternative considered: disable live projection during rebuild. Rejected because it creates an outage, requires operational coordination, and still leaves a restart race.

### 7. Make replacement, event-ledger population, and coverage idempotent

The transaction validates existing ledger payload hashes and embedding identities before writing. It inserts absent immutable semantic event rows for every included fact and treats matching rows as duplicates; a mismatched payload, text hash, or vector digest is a terminal user conflict. It never deletes semantic event rows.

Coverage records bind a user/model/schema to the committed source and catch-up fences plus resulting profile version. Default mode skips only when a coverage record proves the selected run fence is already included and the current profile version is not older than the recorded version. Otherwise it reconstructs from durable facts.

Replaying a committed user from an unadvanced page checkpoint observes matching coverage and profile state and performs no version or timestamp churn. If newer live projection has advanced the profile, finalization performs bounded catch-up rather than restoring the older covered version.

Alternative considered: truncate all model-scoped ledgers before replacing a profile. Rejected because deletion creates duplicate-live-event races and destroys durable idempotency evidence.

### 8. Guard force mode by exact model and preserve model isolation

Default mode honors valid exact-model coverage and avoids unnecessary replacement. `--force` ignores rebuild coverage for candidate selection but requires `--confirm-model` to equal the complete selected persistence key. Prefixes, aliases, upstream model names, wildcard values, and other model keys are rejected before run creation.

All scans, profile locks, ledger reads/writes, coverage rows, metrics aliases, and finalization predicates are scoped to the selected exact model and schema. Force mode never deletes profiles or ledgers and never reads or mutates hash profiles, author affinities, or another semantic model.

Alternative considered: generic `--overwrite`. Rejected because it does not communicate the exact versioned scope and could be misused across models.

### 9. Enforce bounded controls with no unlimited mode

The command provides validated positive bounds for user page size, fact page size, maximum users, maximum total inspected events, per-user events, catch-up events, maximum runtime, deferred retry passes, progress interval, and optional metrics address. Defaults are deliberately finite; zero, negative, and “unlimited” values are invalid.

Maximum users counts candidate users inspected, including deferred or already-current users. Maximum events counts normalized source facts and catch-up facts inspected, including facts later found to have missing vectors. Before starting a user, the runner reserves enough remaining budget for its bounded page; it never intentionally exceed a configured limit.

OS cancellation and maximum runtime share one context. Cancellation stops new pages, rolls back an in-flight user transaction, releases the run lease, and leaves the last committed checkpoint authoritative. `horizon_complete`, `max_users_reached`, `max_events_reached`, and `max_runtime_reached` are resumable safe stop reasons; invalid state, conflicts, or infrastructure failures use bounded non-zero exit classes.

### 10. Expose coverage and progress without sensitive labels

Metrics include bounded counters/histograms/gauges for:

- users and facts scanned by fixed result class;
- users committed, already current, deferred, conflicted, and dry-run eligible;
- exact-model contributing facts with available, missing, or invalid vectors;
- baseline and catch-up event counts and durations;
- checkpoint advances and failures;
- captured eligible users, completed users, deferred users, and missing-vector facts for the run;
- last successful progress time and run lease state.

Labels use only fixed model aliases, source kinds, phases, and allowlisted outcomes. They never include user/video/event IDs, persisted model strings, vectors, titles, URLs, hashes, cursor/fence values, run IDs, paths, tokens, or raw errors.

Periodic and final summaries include mode, elapsed time, bounded counts, completed pages, safe stop reason, coverage numerator/denominator, and missing/invalid-vector counts. They do not claim complete coverage while any user is deferred and do not print sensitive identifiers or raw database errors.

### 11. Verify the boundary at domain, persistence, race, restart, and command levels

Unit tests cover shared eligibility/weight use, canonical ordering, decay equivalence, options, force confirmation, run compatibility, budgets, cancellation, summary redaction, and metric label allowlists. PostgreSQL tests cover snapshot fences, equal occurrence times, stable user/fact pagination, late/out-of-order facts, atomic cursor advancement, deferred persistence, one-user replacement, applied-ledger upsert/conflict, replay idempotency, model isolation, and account deletion.

Concurrency tests pause live projection before and after advisory-lock acquisition and prove that rebuild either includes the committed live fact, defers safely, or commits before the live projector applies it; no newer profile version is overwritten. Restart tests interrupt before/after user commit and page checkpoint, then verify at-most-one vector contribution. Dependency tests cover missing vectors later supplied by video backfill without invoking the embedding service.

The binary may be added to the existing API image with a manual Compose profile/entrypoint and no automatic service startup. Deployments that do not need historical coverage may omit the profile and command entirely.

## Risks / Trade-offs

- [A user has too many historical or catch-up facts] → Enforce per-user and global event caps, defer without mutation, and support a later explicitly bounded run.
- [Missing or unknowable event-time identity prevents profile publication] → Report bounded identity/vector gaps, retain a durable deferral, and use historical backfill only where the exact content revision can be reproduced.
- [Long finalization transactions block one user's live projection] → Precompute outside the transaction, bound catch-up and unique-video locks, use stable lock order, and defer oversized users.
- [Historical source rows are deleted before reconstruction] → Treat unresolved existing ledgers as a safe conflict and document source-retention prerequisites; never overwrite an unprovable live profile.
- [Embedding rows change during reconstruction] → Verify and share-lock exact-model rows before commit; retry on digest change.
- [A crash after user commit but before page checkpoint repeats work] → Coverage, ledger hashes, and profile-version checks make replay a no-op.
- [Force mode causes avoidable database load] → Require full exact-model confirmation, finite limits, dry-run, and no wildcard model selection.
- [Run-state tables grow] → Retain active/deferred runs, clean completed operational rows in bounded age-based batches, and preserve compact coverage only.

## Migration Plan

1. Complete and strictly validate `integrate-semantic-video-embeddings` and `project-semantic-user-interest`; validate `backfill-semantic-video-embeddings` only when the intended run needs reproducible missing historical identities.
2. Add rebuild run/deferred/coverage persistence, indexes, migration registration, retention, and shared repository contracts without exposing the command.
3. Refactor or expose the live semantic classifier/reducer as shared code and prove unchanged live behavior with regression tests.
4. Add snapshot scans, bounded runner, one-user finalization/catch-up, checkpointing, metrics, and the dedicated command.
5. Optionally build the binary into the API image and add a manual Compose profile plus operator documentation; do not make it part of normal startup.
6. Run a bounded dry-run, repair reported video coverage with the dependent backfill, then run a small non-force reconstruction and exercise cancellation/restart.
7. Expand bounded runs only while live projection lag, database lock time, deferred users, and missing-vector metrics remain acceptable.

Rollback stops the one-shot command and disables its manual entrypoint. Live projection continues unchanged. Successfully committed profiles and semantic event rows remain valid; active run state remains resumable. Rebuild-owned operational rows may be cleaned later only after confirming no restart is required.

## Open Questions

None. Optional deployment status, exact model/schema, event-time identity, source fences, canonical ordering, shared final-clamp reduction, missing-vector behavior, live-race protocol, force guard, bounds, checkpoint semantics, observability, and exclusions are fixed by this proposal.
