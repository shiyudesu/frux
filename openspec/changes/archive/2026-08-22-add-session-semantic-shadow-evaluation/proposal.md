## Why

Frux has already validated dormant Session Semantic recall with real active-contract vectors, but it
still lacks production-shaped evidence that the semantic path remains bounded, useful, and completely
isolated from user-visible recommendation behavior. The next safe step is an Exact-based Shadow path
that works with low traffic and does not require ANN, long-term profiles, training, or request-time
model calls.

## What Changes

- Add disabled-by-default deterministic Shadow sampling for first-page Recommendation requests.
- Reuse the existing `session-semantic-v1` builder, active-contract video facts/projections, Exact
  retrieval, confidence rules, and semantic Provider bounds without calling the external model.
- Run admitted Shadow work asynchronously under an independent no-queue capacity limit and bounded
  lifecycle so it cannot delay responses or consume normal recommendation Provider slots.
- Compare bounded semantic candidates in memory with the completed active result and simulate the
  accepted Quota Merge and `semantic_similarity` ranking rules without merging Shadow candidates.
- Record fixed-label coverage, availability, latency, error, capacity, unique-contribution, overlap,
  simulated-survival, displacement, and diversity metrics without persisting candidate IDs or vectors.
- Add a deterministic replay command and versioned low-volume fixtures so a personal deployment can
  produce useful Shadow evidence without waiting for large organic traffic.
- Produce a deterministic non-causal JSON/Markdown report with explicit denominators, safety gates,
  uncertainty, and an `inconclusive` outcome when evidence is insufficient; never activate a policy.
- Add exact production-invariance and shutdown tests proving Shadow cannot change candidates, order,
  reasons, scores, degradation, Snapshot/cursor behavior, request logs, delivery evidence, attribution,
  or response completion.
- Clarify that native `.env.multimodal` loopback endpoints are not directly injectable into the
  default Docker Compose containers; default Compose remains model-disabled.

## Capabilities

### New Capabilities

- `session-semantic-shadow-evaluation`: Defines bounded sampling, asynchronous no-queue execution,
  in-memory comparison/simulation, low-volume replay, fixed-label observability, deterministic reports,
  lifecycle safety, privacy, and non-causal acceptance for Session Semantic Shadow.

### Modified Capabilities

- `contextual-recommendation`: Requires Shadow execution to remain strictly isolated from production
  recommendation results, persistence, attribution, degradation, latency, and active policy selection.

## Impact

- Affects recommendation application orchestration, API lifecycle/composition, configuration,
  Prometheus metrics, a standalone Go replay/report command, tests, and recommendation/embedding
  documentation.
- Reuses the completed Session Semantic builder/Provider and Exact repository; adds no database
  migration, public API, Web behavior, Kafka consumer, Redis schema, vector generation, or external
  model call.
- Keeps `recommend/v1` and `recommend/v2` unchanged and introduces no active semantic policy, rollout,
  HNSW/ANN dependency, historical backfill, long-term semantic profile, training export, or learned
  weights.
