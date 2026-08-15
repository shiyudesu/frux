## 1. Dependency and Shared-Semantics Gates

- [ ] 1.1 Confirm `integrate-semantic-video-embeddings`, `backfill-semantic-video-embeddings`, and the narrowed `project-semantic-user-interest` are implemented and strictly valid with the fixed model, vector, profile, ledger, decay, and advisory-lock contracts required by this change.
- [ ] 1.2 Expose or refactor the live semantic event classifier, payload hashing, canonical ordering, weights, decay, clamping, and vector validation into shared domain/application primitives, with regression tests proving live projection behavior is unchanged.
- [ ] 1.3 Add the static rebuild descriptor and bounded option validation for exact model/schema, page sizes, user/event/runtime limits, catch-up limits, retry passes, dry-run, metrics, run resume, and exact-model force confirmation.

## 2. Rebuild Persistence and Atomic Checkpointing

- [ ] 2.1 Add PostgreSQL models and migrations for rebuild runs, per-run deferred users, and per-user/model/schema coverage with bounded fields, required uniqueness/indexes, retention support, and account-erasure handling.
- [ ] 2.2 Implement fresh-run creation that captures the multi-source high-water fence and user horizon in one repeatable-read transaction, plus compatible resume loading and fail-closed state validation.
- [ ] 2.3 Implement renewable single-run leasing and atomic page checkpoint/counter advancement after durable terminal user outcomes, including safe lease release/expiry during cancellation.
- [ ] 2.4 Add persistence tests for concurrent run creation/resume, lease exclusion/expiry, fence consistency, compatible and incompatible restart, atomic cursor advancement, replay before checkpoint, cleanup, and account erasure.

## 3. Stable Historical Selection and Baseline Reconstruction

- [ ] 3.1 Implement repository queries for stable `user_id` keyset pages and normalized per-user behavior/action/feedback fact pages ordered by `(occurred_at, source_kind_rank, source_event_id)` behind the captured namespace fences.
- [ ] 3.2 Implement bounded exact-model embedding batch reads with video deduplication, model/dimension/finiteness/norm validation, and stable vector digests for finalization revalidation.
- [ ] 3.3 Implement baseline reconstruction from zero through the shared semantic reducer, including event budgets, per-user limits, complete-user classification, durable missing/invalid-vector deferral, and no partial profile or ledger writes.
- [ ] 3.4 Add selection and reducer tests for equal timestamps, page boundaries, facts inserted beyond the fence, late-recorded old occurrences, unsupported/author-only facts, delayed and shuffled delivery, decay equivalence, missing vectors, and deterministic restart values.

## 4. Transactional Finalization and Live-Race Safety

- [ ] 4.1 Extend the semantic profile repository with shared `(user_id, model)` advisory locking, locked current profile/ledger reads, per-source catch-up fence capture, bounded post-fence fact loading, and existing-ledger source resolution.
- [ ] 4.2 Implement one-user atomic finalization that stable-order locks and revalidates embedding rows, recomputes baseline plus catch-up, conditionally replaces/upserts at `locked_version + 1`, upserts matching applied identities, records coverage, clears deferral, and rolls back on any failure.
- [ ] 4.3 Implement idempotent coverage skips, replay without version/timestamp churn, payload-conflict and unresolved-ledger fail-closed behavior, catch-up/event-limit deferral, force-mode reconstruction, and strict isolation from other models, hash profiles, and author affinities.
- [ ] 4.4 Add PostgreSQL concurrency tests for live apply before and behind the shared lock, newer profile versions, catch-up overflow, embedding refresh races, duplicate/conflicting ledgers, transaction rollback, missing vectors later appearing, account deletion, model isolation, and crash/restart around user commit.

## 5. Bounded Runner, Command, and Observability

- [ ] 5.1 Implement the cancellation-aware runner with user/event/runtime accounting, stable primary and deferred passes, resumable stop reasons, lease renewal, complete-page checkpointing, and dry-run execution with no database mutation.
- [ ] 5.2 Add `cmd/rebuild-semantic-user-interest` to compose PostgreSQL-only dependencies, validate exact model/schema and resume state, avoid migrations/Redis/Kafka/embedding-service calls, handle OS signals, and return bounded success, cancellation, configuration, conflict, and infrastructure exit classes.
- [ ] 5.3 Add bounded Prometheus metrics plus periodic and exactly one final safe summary for scan progress, baseline/catch-up work, committed/current/deferred/conflicted users, available/missing/invalid vectors, checkpoints, lease state, and coverage completeness.
- [ ] 5.4 Add runner and command tests for every configured limit, dry-run, guarded force, cancellation during scans and transactions, deferred retry passes, lease loss, checkpoint replay, stop/exit classes, metric label allowlists, and summary redaction.

## 6. Operations, Documentation, and Validation

- [ ] 6.1 Build and copy the rebuild binary in the API image and add a manual Compose profile/entrypoint with PostgreSQL only, internal metrics, bounded flags, no automatic startup, and configuration tests.
- [ ] 6.2 Add the semantic user-interest rebuild runbook and update recommendation, monitoring, engineering, architecture, deployment/configuration, and module-index documentation for prerequisites, exact model, dry-run, video-backfill coverage repair, limits, force guard, live-race protocol, cancellation/restart, metrics, verification, rollout, and rollback.
- [ ] 6.3 Run targeted domain/repository/runner/command/PostgreSQL/concurrency tests, the complete Go suite, builds for feed/worker/rebuild entrypoints, container and Compose validation, confirm no excluded live/pgvector/ANN/ranking/policy/training/author-affinity/public API/Web/main-spec changes, and run `openspec validate --all --strict`.
