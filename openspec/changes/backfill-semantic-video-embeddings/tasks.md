## 1. Dependency Gates and Shared Contracts

- [ ] 1.1 Confirm `add-semantic-embedding-service` is implemented and its authenticated metadata/batch API still matches the fixed model, revision, dimension, normalization, limits, and safe error contract.
- [ ] 1.2 Confirm the narrowed `integrate-semantic-video-embeddings` implementation exposes the fixed persistence key, canonical source hashing, validated bounded client, finite vector construction, conditional repository behavior, and coverage interfaces without historical logic.

## 2. Backfill Domain, Options, and Checkpoint Contract

- [ ] 2.1 Add application-owned backfill candidate, refresh mode, ordering tuple, horizon, outcome, progress, summary, and safe error types that use only the fixed semantic model.
- [ ] 2.2 Implement and test bounded option parsing/validation for page size, batch size, concurrency, maximum rows/runtime, dry-run, progress interval, metrics address, checkpoint path, and exact-model refresh confirmation.
- [ ] 2.3 Implement and test the versioned opaque checkpoint cursor plus mode-0600 sibling write, file/directory flush, atomic replacement, corruption detection, compatibility checks, and dry-run no-write behavior.

## 3. Stable Scan and Transactional Persistence

- [ ] 3.1 Extend the versioned embedding repository contract with fresh-run horizon capture and stable `(published_at, id)` candidate pages for published, public, `legacy_ready`/`ready` videos.
- [ ] 3.2 Implement missing-only selection and exact-model refresh projections without reading, deleting, or updating hash embeddings or other model rows.
- [ ] 3.3 Implement transactional row locking, current canonical hash and eligibility revalidation, refresh-mode compare-and-set checks, and shared exact-model insert/update/no-op persistence outcomes.
- [ ] 3.4 Add PostgreSQL tests for tuple ordering and horizon bounds, equal timestamps, concurrent inserts, source/visibility/lifecycle/media changes, exact-model races, identical no-op timestamps, side-by-side models, and replay idempotency.

## 4. Bounded Backfill Runner

- [ ] 4.1 Implement deterministic page classification for `none`, `stale`, and `force`, including dry-run `would_generate`, already-current, ineligible, and source-changed outcomes.
- [ ] 4.2 Implement consecutive semantic batches with stable `video:<id>` identity/order, configured batch size, at most two concurrent requests, and cancellation-safe goroutine cleanup.
- [ ] 4.3 Add bounded three-attempt retry handling for timeout, overload, and unavailable results with cancellation-aware delays and terminal handling for auth, metadata, contract, input, and local configuration errors.
- [ ] 4.4 Advance checkpoints only after complete durable page prefixes; preserve the prior checkpoint on partial failure, cancellation, or runtime expiry and resume strictly after the stored tuple/horizon.
- [ ] 4.5 Add runner tests for row/runtime limits, horizon completion, partial-page writes, retries, terminal failures, SIGINT/SIGTERM cancellation, restart in every refresh mode, and no service calls or mutations during dry-run.

## 5. Command Composition and Observability

- [ ] 5.1 Add `cmd/backfill-semantic-video-embeddings` to load existing database/semantic configuration, validate service metadata before scanning, compose the runner without Redis/Kafka or migrations, and return bounded completion/error exit classes.
- [ ] 5.2 Add bounded Prometheus collectors and an optional internal health/metrics listener for row outcomes, batch count/duration/results, in-flight work, checkpoint writes, and last progress time with fixed label allowlists.
- [ ] 5.3 Add periodic structured progress and exactly one final summary with safe counts, elapsed time, pages, attempts, last publication time, stop reason, and redaction tests for IDs, text, vectors, hashes, tokens, URLs, paths, cursors, and raw errors.

## 6. Container and Operations

- [ ] 6.1 Build and copy the backfill binary in `apps/api/Dockerfile`, then add a manual Compose profile/service with PostgreSQL and semantic-service dependencies, internal-only metrics, no Redis/Kafka dependency, and a persistent checkpoint mount.
- [ ] 6.2 Add the semantic embedding backfill operator runbook and update relevant embedding/video, monitoring, engineering, architecture, deployment, module-index, and setup/configuration docs for prerequisites, dry-run, bounded rollout, refresh confirmation, progress/metrics, cancellation/restart, verification, and rollback.

## 7. Integration and Final Validation

- [ ] 7.1 Add live semantic-service and command integration tests covering missing, stale, force, dry-run, transient retry, contract failure, source changes during inference, cancellation, atomic checkpoint replacement, and restart.
- [ ] 7.2 Build all three Go entrypoints, run targeted embedding/backfill/config/metrics/PostgreSQL tests, run the complete Go suite, build the container, and validate the manual Compose entrypoint with a strong internal token.
- [ ] 7.3 Confirm no main specs, live Kafka event behavior, pgvector/ANN artifacts, profile rebuild, recommendation provider/policy, public API/Web behavior, or training code changed, then run `openspec validate --all --strict`.
