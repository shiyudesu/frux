## 1. Context and Contracts

- [x] 1.1 Define bounded typed recommendation context, validation, normalization, and backward-compatible Feed request mapping.
- [x] 1.2 Extend Web Feed request types with session ID, refresh index, recent video IDs, and normalized playback capability context.
- [x] 1.3 Add recommendation feedback domain types, idempotency rules, HTTP DTOs, and route contract.
- [x] 1.4 Add API tests for valid context, excessive fields, anonymous restrictions, and feedback replay/conflict.

## 2. Policy, Profile, and Logging Storage

- [x] 2.1 Add recommendation policy, user-interest profile, applied profile event, feedback, and sampled request-log models and migrations.
- [x] 2.2 Implement policy validation, version activation, staged rollout, and rollback repository operations.
- [x] 2.3 Implement idempotent profile event application with long-term, recent, author, and negative components.
- [x] 2.4 Implement retention and sampling controls for recommendation request logs.
- [x] 2.5 Add repository and migration tests for policy versions, deduplication, and cleanup.

## 3. Multi-Source Recall

- [x] 3.1 Define recall-provider interfaces, candidate reason metadata, budgets, and per-provider deadlines.
- [x] 3.2 Implement fresh-content and hot-content recall providers.
- [x] 3.3 Implement content-similarity recall through the versioned embedding interface and hash-vector fallback.
- [x] 3.4 Implement followed-author and session-continuation recall providers through narrow adapters.
- [x] 3.5 Implement concurrent provider execution, degradation, deduplication, visibility filtering, and pool bounds.
- [x] 3.6 Add provider tests for ordering, duplicates, timeout degradation, and unreadable candidates.

## 4. Profile Projection

- [x] 4.1 Consume idempotent progress, completion, skip, interaction, follow, and feedback events into the recommendation profile worker.
- [x] 4.2 Implement configurable time decay and bounded positive/negative signal weighting.
- [x] 4.3 Add fallback profile reconstruction from bounded durable facts when no materialized profile exists.
- [x] 4.4 Expose profile worker lag, duplicate, failure, and update metrics.

## 5. Ranking and Diversity

- [x] 5.1 Implement normalized ranking features and policy-driven score calculation.
- [x] 5.2 Apply exposure suppression, negative penalties, author affinity, follow relation, freshness, hotness, and content/session similarity.
- [x] 5.3 Replace adjacent-author-only interleaving with configurable author/content diversity while preserving deterministic tie-breaking.
- [x] 5.4 Retain internal candidate reasons and score components for sampled evaluation records.
- [x] 5.5 Add ranker tests for policy changes, negative feedback, diversity, cold start, and deterministic ordering.

## 6. Stable Recommendation Sessions

- [x] 6.1 Implement Redis recommendation snapshots keyed by user, scene, request ID, and policy version.
- [x] 6.2 Add signed snapshot cursors with offset, expiry, and integrity validation.
- [x] 6.3 Revalidate current visibility during snapshot page assembly and handle filtered gaps.
- [x] 6.4 Implement deterministic degraded cursor fallback when Redis is unavailable.
- [x] 6.5 Add pagination tests for changing profiles, visibility changes, expiry, invalid signatures, and Redis degradation.

## 7. Feedback and Evaluation

- [x] 7.1 Implement feedback persistence and bounded suppression for not-interested, reduce-author, and already-seen actions.
- [x] 7.2 Add a truthful Web recommendation feedback action to the Feed more menu with authenticated error handling.
- [x] 7.3 Record sampled request context, policy, ordered candidates, reasons, scores, and degraded flags.
- [x] 7.4 Link exposure and downstream behavior outcomes by recommendation request ID for offline queries.
- [x] 7.5 Add metrics for recall contribution, degraded requests, snapshot hit rate, policy version, and outcome aggregates.

## 8. Verification and Documentation

- [x] 8.1 Add end-to-end recommendation API-flow tests covering context, recalls, policy, snapshots, feedback, and outcome linkage.
- [x] 8.2 Update recommendation, Feed, exposure, interaction, relation, architecture, optimization, monitoring, and engineering documentation.
- [x] 8.3 Define initial policy version, rollout cohort, snapshot TTL, log sampling, and rollback runbook.
- [x] 8.4 Run targeted Go tests, Web build, recommendation load checks, Windows Chrome behavior checks, and strict OpenSpec validation.
