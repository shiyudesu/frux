## Context

Recommendation currently reads up to 500 public candidates ordered by aggregate interaction hot score, loads hash n-gram vectors, derives a user vector from up to 200 positive view events, applies fixed weights, and interleaves adjacent authors. The cursor contains score/time/video ID, so later pages recompute against changing candidates and user state. Feed request context currently carries only a request ID from the Web client.

The design must improve relevance and evaluation without requiring an external feature store or online ML platform.

## Goals / Non-Goals

**Goals:**

- Combine multiple bounded recall sources with explainable candidate reasons.
- Use session and device context without accepting unbounded client metadata.
- Include positive, negative, relational, and time-decayed signals.
- Version ranking policy and preserve stable pagination within a recommendation request.
- Record enough request/outcome data for evaluation.
- Keep local vectorization as an operational fallback.

**Non-Goals:**

- Training or serving a large neural recommendation model.
- Building a general-purpose feature store.
- Guaranteeing globally optimal recommendations.
- Exposing internal scores or sensitive profile features to clients.

## Decisions

### 1. Use typed bounded recommendation context

`FeedQueryRequest.context` is replaced internally by a typed structure while the HTTP JSON remains additive. Accepted fields include request/session ID, refresh index, recently viewed video IDs, current video ID, network class, save-data, viewport class, and supported playback capabilities. Lists and strings have strict limits; unknown fields are ignored or rejected consistently.

The server derives authenticated user, time, interaction facts, follow graph, and durable exposure state rather than trusting client claims.

### 2. Introduce composable recall providers

Application-level recall providers return candidate ID, author, recall reason, and raw source score:

- fresh public content,
- recent hot content,
- content-vector similarity,
- followed-author continuation,
- session continuation around recently engaged topics.

The service runs providers concurrently with per-provider deadlines, merges and deduplicates candidates, filters current visibility and recent exposure, and enforces a total pool bound. Provider failure degrades to the remaining sources.

### 3. Materialize user interest instead of scanning raw events per request

A recommendation-profile worker consumes idempotent behavior and interaction events into `user_interest_profile`:

- long-term content vector,
- recent/session vector,
- author affinities,
- negative topic/author weights,
- version and update time.

Signals use time decay. Completion, sustained progress, like, favorite, follow, and repeat watch are positive; early skip and explicit not-interested feedback are negative. Request-time fallback can rebuild from bounded facts when no profile exists.

### 4. Add an explicit negative-feedback contract

`POST /api/recommendation-feedback` accepts video ID, request ID, feedback type, and idempotency key. Initial types are `not_interested`, `reduce_author`, and `already_seen`. The action does not delete historical facts; it updates recommendation preferences and suppresses affected candidates for a bounded period.

### 5. Version ranking policy and normalize features

`recommendation_policy` stores scene, version, enabled state, feature weights, recall budgets, freshness decay, exposure window, diversity rules, and rollout percentage. Ranking computes normalized features such as content similarity, session similarity, hot score, freshness, author affinity, follow relation, negative penalty, and repeated-exposure penalty.

Results sort by score, publish time, and video ID before diversity constraints. Each candidate retains reason and score components internally for logs. Policy validation prevents unknown features, invalid bounds, or weight overflow.

### 6. Snapshot ordered results in Redis for stable pagination

The first page builds an ordered candidate snapshot keyed by user, scene, request ID, and policy version with a short TTL. The cursor contains snapshot ID, offset, and integrity signature. Later pages read the same snapshot and revalidate current visibility before card assembly.

If Redis is unavailable, the service falls back to deterministic score cursors and marks the request as degraded. This preserves availability while making the normal path stable across user/profile changes.

### 7. Record sampled evaluation data

`recommendation_request_log` stores request ID, user ID, scene, policy version, degraded flags, bounded context, and candidate IDs/reasons/scores as a compact payload. Exposure and behavior events already carry request ID, enabling joins for click/watch/completion evaluation. Sampling and retention are configurable to control storage.

### 8. Keep embedding providers replaceable

The current hash n-gram vectorizer remains the default local provider. Interfaces and model/version fields allow a later semantic embedding implementation and side-by-side backfill without changing ranking or Feed APIs.

## Risks / Trade-offs

- [More recalls increase latency] -> Run concurrently with deadlines, budgets, caching, and degraded fallback.
- [Redis snapshots consume memory] -> Bound candidates, TTL, and per-user active sessions.
- [Negative feedback can over-filter] -> Use expiry, reason-specific scope, and minimum fallback pools.
- [Policy configuration can destabilize ranking] -> Validate, version, stage rollout, and retain instant rollback.
- [Profile worker lag reduces personalization] -> Expose lag and retain request-time fallback.
- [Evaluation logs grow quickly] -> Sample, compact, partition, and expire them.

## Migration Plan

1. Add typed context, policy/profile/log models, and disabled configuration.
2. Build recall providers around existing repositories and preserve current ranker as policy version 1.
3. Add profile projection worker fed by reliable behavior events.
4. Enable Redis snapshots and stable cursors.
5. Add negative-feedback endpoint and Web action.
6. Roll out a new policy to a small cohort, compare outcome metrics, then expand.
7. Roll back by selecting the previous policy version and disabling new providers; Feed response compatibility remains.

## Open Questions

- Initial policy weights, sampling rate, and snapshot TTL.
- Whether followed-author recall belongs in recommendation or should remain exclusive to the following scene.
