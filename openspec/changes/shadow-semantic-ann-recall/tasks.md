## 1. Dependency and Configuration Gates

- [ ] 1.1 Confirm the accepted contracts from `enable-pgvector-recommendation-index`, `project-semantic-user-interest`, and the narrowed `add-pgvector-recommendation-recall`, including the existing provider interface, fixed result classes, budget/deadline bounds, and active permit count.
- [ ] 1.2 Add semantic ANN shadow configuration for `sample_ppm`, budget, deadline, `max_in_flight`, and comparison limit with design defaults, strict bounds, default-zero disablement, and no policy-schema fields.
- [ ] 1.3 Implement the versioned length-delimited SHA-256 PPM sampler over user ID, canonical request ID, and normalized scene, with golden bucket, retry/replica stability, zero/full sampling, and invalid-input tests.
- [ ] 1.4 Add composition validation proving disabled startup needs no shadow prerequisites, enabled startup rejects missing/incompatible dependencies, and active policies/bootstrap serialization remain unchanged.

## 2. Shadow Executor and Lifecycle Safety

- [ ] 2.1 Refactor provider execution capacity into active and shadow admission classes while preserving the existing active permit count, deadline classification, and active behavior.
- [ ] 2.2 Implement non-blocking no-queue shadow admission acquired before goroutine creation, holding permits until the actual provider returns and recording capacity without retry or provider work.
- [ ] 2.3 Implement the lifecycle-owned asynchronous evaluator with copied bounded request context, configured provider budget/deadline, at-most-once terminal classification, and no return path for candidates or degradation.
- [ ] 2.4 Add admission-close, lifecycle cancellation, bounded drain, and incomplete-drain shutdown behavior so cooperative calls stop and context-ignoring calls remain bounded without blocking process shutdown indefinitely.

## 3. Recall Integration and In-Memory Evaluation

- [ ] 3.1 Capture deterministic bounded unique ID sets from successful active `content_similarity` and `session_continuation` results before merge, including explicit empty and unavailable comparator states.
- [ ] 3.2 Launch selected shadow work only after production recall state is available, without awaiting completion or passing mutable candidates, snapshot/log/evidence builders, response objects, or pooled HTTP request state.
- [ ] 3.3 Reduce semantic ANN output to bounded unique positive IDs and implement intersection, Jaccard, comparator coverage, and union calculations with defined empty/unavailable behavior and no candidate score/vector retention.
- [ ] 3.4 Wire the evaluator into API recommendation composition and shutdown hooks with `sample_ppm=0` as the deployed default, no worker changes, and no persistence, migration, policy activation, ranking, training, A/B, or Web code.

## 4. Fixed-Label Observability and Acceptance Runbook

- [ ] 4.1 Add selected, terminal result/profile availability, latency, candidate-count, comparator-state, intersection, Jaccard, and comparator-coverage metrics using only the fixed labels and histogram bounds defined by the design.
- [ ] 4.2 Add metrics/logging tests proving at-most-once terminal observations and exclusion of user, request, session, video, vector, score, model, policy, raw scene, SQL/index, and raw error values.
- [ ] 4.3 Update `docs/modules/recommendation.md` and directly related configuration/operations documentation with shadow isolation, sampling/bounds, capacity, cancellation/shutdown, rollout-to-zero rollback, and explicit exclusions.
- [ ] 4.4 Add the copyable Prometheus-query acceptance report template covering the 24-hour window, 10,000 selected and 1,000 profile-available minimums, 99% terminal and 10% profile coverage, p95/error/timeout/capacity gates, and overlap distributions without relevance or activation claims.

## 5. Safety Verification and Strict Validation

- [ ] 5.1 Add executor tests for success, empty, no profile, invalid result, provider error, timeout, cancellation races, capacity exhaustion, and fixed terminal/profile result mapping.
- [ ] 5.2 Add stress and shutdown tests proving admission occurs before goroutine creation, context-ignoring calls never exceed `max_in_flight`, no queue/retry forms, active permits remain available, and bounded shutdown returns.
- [ ] 5.3 Add production-invariance tests comparing shadow disabled and fully sampled cases for exact candidates/order/reasons/scores, degraded state, ranking, cursors/snapshots, request logs, evidence, attribution inputs, and response completion under success/failure/blocking cases.
- [ ] 5.4 Run targeted recommendation application/router/config/metrics tests, compile `./cmd/feed` and `./cmd/worker`, run `cd apps/api && go test ./...`, then run `openspec validate --all --strict` and confirm no application code was added outside implementation, no main specs were edited, and no excluded migration/projection/scoring/policy/ranking/training/A-B/Web scope was introduced.
