## 1. Dependency and Configuration Gates

- [ ] 1.1 Confirm the accepted contracts from `enable-pgvector-recommendation-index`, `project-semantic-user-interest`, and `add-pgvector-recommendation-recall`, including exact/stale projection semantics, fixed session/recent/long-term fusion, separate semantic capacity, provider reservations, and explicit semantic ranking.
- [ ] 1.2 Add shadow configuration for `sample_ppm`, budget, deadline, `max_in_flight`, comparison limit, simulated pool limit, and simulated top-K with strict finite bounds, default-zero disablement, and no policy-schema fields.
- [ ] 1.3 Implement the versioned length-delimited SHA-256 PPM sampler over user ID, canonical request ID, and normalized scene, with golden bucket, retry/replica stability, zero/full sampling, and invalid-input tests.
- [ ] 1.4 Add composition validation proving disabled startup needs no shadow prerequisites, enabled startup rejects missing/incompatible dependencies, and active policies/bootstrap serialization remain unchanged.

## 2. Shadow Executor and Lifecycle Safety

- [ ] 2.1 Add shadow admission while preserving both baseline-provider and registered semantic-provider capacity, deadline classification, and active behavior.
- [ ] 2.2 Implement non-blocking no-queue shadow admission acquired before goroutine creation, holding permits until the actual provider returns and recording capacity without retry or provider work.
- [ ] 2.3 Implement the lifecycle-owned asynchronous evaluator with copied bounded request context, configured provider budget/deadline, at-most-once terminal classification, and no return path for candidates or degradation.
- [ ] 2.4 Add admission-close, lifecycle cancellation, bounded drain, and incomplete-drain shutdown behavior so cooperative calls stop and context-ignoring calls remain bounded without blocking process shutdown indefinitely.

## 3. Recall Integration and In-Memory Simulation

- [ ] 3.1 Capture deterministic bounded provider-local IDs, scores, authors, and allowlisted topics from Fresh, Hot, content similarity, followed author, and session continuation before merge, including explicit empty/unavailable states.
- [ ] 3.2 Launch selected shadow work only after production recall state is available, without awaiting completion or passing mutable candidates, snapshot/log/evidence builders, response objects, or pooled HTTP request state.
- [ ] 3.3 Reduce semantic output to bounded unique facts and implement profile/component coverage, profile-age and projection-staleness states, query fill, unique contribution, intersection/Jaccard/coverage, planned reservation/pool survival, simulated semantic-rank survival, Fresh/Hot displacement, and author/topic diversity with defined empty/unavailable behavior.
- [ ] 3.4 Wire the evaluator into API recommendation composition and shutdown hooks with `sample_ppm=0` as the deployed default, no worker changes, and no persistence, migration, policy activation, ranking, training, A/B, or Web code.

## 4. Fixed-Label Observability and Low-Volume Evaluation

- [ ] 4.1 Add selected, terminal result, latency/error/capacity, component/profile coverage, profile-age, projection-state, query-mode/fill, unique-contribution, pool/rank-survival, Fresh/Hot-displacement, diversity, comparator, and overlap metrics using only fixed labels.
- [ ] 4.2 Add metrics/logging tests proving at-most-once terminal observations and exclusion of user, request, session, video, vector, score, model, policy, raw scene, SQL/index, and raw error values.
- [ ] 4.3 Add a versioned golden set and human-review workflow with at least 30 representative contexts, bounded relevance labels, precision/recall/nDCG/unique-relevant/diversity reporting, 20% independent second review, and disagreement adjudication.
- [ ] 4.4 Update recommendation and operations documentation with shadow-first ordering, isolation from response/snapshot/request-log/evidence/attribution, sampling/bounds, capacity, coverage/staleness, simulation metrics, golden/human relevance, low-volume uncertainty, rollout-to-zero rollback, and no-causal-lift exclusions.
- [ ] 4.5 Add copyable aggregate-query/report templates that record exact denominators, operational gates, unique contribution, pool/rank survival, Fresh/Hot displacement, diversity, overlap, golden/human results, and `inconclusive` status without a 10,000-request floor or automatic activation.

## 5. Safety Verification and Strict Validation

- [ ] 5.1 Add evaluator tests for success, empty, no profile, stale projection, invalid result, provider error, timeout, cancellation races, capacity exhaustion, fixed coverage/staleness states, and terminal mapping.
- [ ] 5.2 Add stress and shutdown tests proving admission occurs before goroutine creation, context-ignoring calls never exceed `max_in_flight`, no queue/retry forms, active permits remain available, and bounded shutdown returns.
- [ ] 5.3 Add pure simulation/golden tests for deterministic reservations, pool truncation survival, explicit semantic-rank survival, unique contribution, Fresh/Hot displacement, author/topic diversity, unknown metadata, relevance metrics, and reviewer disagreement handling.
- [ ] 5.4 Add production-invariance tests comparing shadow disabled and fully sampled cases for exact candidates/order/reasons/scores, degraded state, ranking, cursors/snapshots, recommendation request logs, evidence, attribution inputs, and response completion under success/failure/stale/blocking cases.
- [ ] 5.5 Run targeted recommendation application/router/config/metrics tests, compile `./cmd/feed` and `./cmd/worker`, run `cd apps/api && go test ./...`, then run `openspec validate --all --strict` and confirm no main specs, active policy, production merge/ranking, migration/projection, training, A/B, causal-lift, or Web scope was introduced.
