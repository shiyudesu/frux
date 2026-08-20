## Context

`add-pgvector-recommendation-recall` registers a dormant semantic provider, defines fixed session/recent/long-term fusion, separate semantic capacity, deterministic provider reservations, and an explicit semantic ranking feature, but activates no policy. This shadow change is therefore the mandatory gate before any active gray proposal.

Shadow execution must use production-shaped context without entering response candidates or any production artifact. It must evaluate operational safety and likely retrieval usefulness at low traffic volume, including cases where large statistical samples are unavailable. Observational shadow data cannot prove causal online lift.

## Goals / Non-Goals

**Goals:**

- Default sampling to zero and deterministically select bounded requests when enabled.
- Invoke the registered provider asynchronously with separate no-queue shadow capacity.
- Preserve response latency, production candidates, snapshots, recommendation request logs, evidence, and attribution.
- Measure latency, errors, capacity, profile/index coverage, staleness, and result fill.
- Measure unique semantic contribution, pre-rank pool survival, simulated rank survival, author/topic diversity, and Fresh/Hot displacement.
- Add a small golden semantic-relevance set and human-review procedure that does not require large traffic samples.
- Produce an operator report with denominators, uncertainty, and explicit no-causal-lift language.

**Non-Goals:**

- Any production policy activation, candidate merge, ranking, snapshot/log/evidence/attribution mutation, or response change.
- pgvector/profile projection changes, model training, A/B assignment, or causal online experimentation.
- Persisting per-request shadow candidates, vectors, or identifiers.
- Treating overlap, diversity, or golden relevance as proof of online lift.

## Decisions

### 1. Use a versioned deterministic PPM sampler with default zero

Configuration includes:

- `sample_ppm`: `0..1_000_000`, default `0`;
- `budget`: `1..100`, default `20`;
- `deadline_ms`: `25..500`, default `250`;
- `max_in_flight`: `1..16`, default `2`;
- `comparison_limit`: `1..100`, default `100`;
- `simulated_pool_limit`: `1..500`, default matching the production pre-rank cap;
- `simulated_top_k`: `1..100`, default matching response size.

The sampler hashes a versioned, length-delimited tuple of positive user ID, canonical request ID, and normalized scene with SHA-256. `sample_ppm=0` performs no admission, goroutine, profile read, or query. The tuple/hash is never logged or persisted.

### 2. Copy bounded provider-local inputs and never attach shadow state

Before production merge, the service copies bounded provider-local IDs, scores, author IDs, and allowlisted topic/category IDs for Fresh, Hot, content similarity, followed author, and session continuation. Missing/failed/omitted providers receive fixed unavailable states; shadow never reruns them.

After production recall state is available, sampling and no-queue admission occur. The evaluator receives immutable copied scalars and bounded semantic request context, never mutable candidates, response objects, snapshot builders, request-log builders, evidence writers, attribution inputs, or pooled HTTP objects. It returns only aggregate observations to a shadow metric sink.

### 3. Preserve active capacity and bound cancellation-resistant work

Shadow uses a distinct non-blocking semaphore in addition to the provider's dedicated semantic capacity rules. All permits are acquired before goroutine creation and held until the actual provider call returns. Capacity rejection starts no work. There is no queue or retry.

The evaluator owns a lifecycle context. Shutdown closes admission, cancels cooperative work, and waits only to the caller's deadline. Context-ignoring work remains bounded by `max_in_flight`; shutdown may report incomplete drain without blocking indefinitely.

### 4. Observe coverage and staleness explicitly

Each selected execution records fixed-state observations for:

- session/recent/long-term component availability and fused-query availability;
- semantic profile age buckets from `materialized_at`;
- exact/HNSW query mode and returned-K/budget fill;
- authoritative projection state `current`, `missing`, or `stale`, where stale means provider/model/revision/text-hash/vector-digest equality failed;
- provider terminal result, latency, timeout, cancellation, error, and capacity.

No stale projection may enter returned semantic IDs; staleness observations come from bounded diagnostics, not from relaxing the index query's equality/readability filters.

### 5. Simulate planned pool mixing and semantic ranking in memory

The evaluator canonicalizes bounded unique semantic and baseline candidates and computes:

- unique contribution: semantic IDs absent from the union of available baseline providers;
- pool-truncation survival: semantic IDs surviving the planned reservations and deterministic mixing at `simulated_pool_limit`;
- simulated rank survival: surviving semantic IDs in simulated top-K after applying the planned explicit `semantic_similarity` component to copied bounded features;
- Fresh and Hot displacement: Fresh/Hot IDs present in the baseline simulated top-K but absent after adding semantic candidates;
- author/topic diversity: distinct-author/topic counts and bounded concentration/entropy summaries for semantic output and simulated top-K;
- overlap/intersection/Jaccard with each fixed baseline provider and their union.

Simulation uses pure copied data and versioned rules from `add-pgvector-recommendation-recall`. It cannot write production candidate state. Undefined ratios are omitted; unavailable comparators remain explicit.

### 6. Emit only fixed-label aggregate telemetry

Metrics cover selection, terminal results, latency, capacity, profile/component coverage, profile-age bucket, projection state, query mode, fill ratio, unique contribution, pool survival, simulated rank survival, Fresh/Hot displacement, author/topic diversity, and comparator overlap.

Labels use fixed enums only. User/request/session/video/author/topic IDs, vectors, scores, model strings, policy versions, raw scenes, SQL/index details, candidate lists, and raw errors never appear in labels or normal logs. Recommendation request logs receive no shadow fields. Each selected request finalizes at most one terminal observation.

### 7. Add small golden and human semantic relevance evaluation

Operators maintain a versioned low-volume golden set of at least 30 representative session/profile contexts with bounded judged candidate labels. It covers cold/absent profile, session-dominant, recent-shift, stable long-term, negative-feedback, sparse-topic, and Fresh/Hot-heavy cases. Labels include relevant, partially relevant, irrelevant, and unsafe/unreadable; unsafe/unreadable must never be retrieved.

The report computes precision@K, recall@K where judgments are complete, nDCG@K, unique relevant contribution, and author/topic diversity. At least 20% of contexts receive independent second review, with disagreements and adjudication reported. A bounded manual review may inspect ephemeral candidate text in an authorized operator workflow, but IDs/text are not copied into metrics, request logs, evidence, or long-lived per-request shadow storage.

The golden set and human review establish semantic plausibility at small scale; they do not prove causal engagement lift.

### 8. Make acceptance manual, low-volume-capable, and non-causal

The operator report records exact denominators and confidence/uncertainty rather than requiring 10,000 requests or another large traffic floor. Production-shaped observations should cover at least one representative peak window when available; deterministic load/cancellation tests and the golden set remain required even when traffic is sparse.

Operational checks include terminal coverage, latency versus configured deadline, provider/invalid-result errors, timeout, capacity, query fill, current/missing/stale projection coverage, and shutdown behavior. Retrieval checks include unique contribution, pool/rank survival, Fresh/Hot displacement, diversity, overlap, and golden/human relevance.

Insufficient observations are reported as `inconclusive`, not passed by assuming zero failures. Passing the report authorizes only a separate rollout proposal; it never creates/selects a policy and explicitly states that shadow/golden evidence does not prove causal online lift.

## Risks / Trade-offs

- [Shadow adds database load] → Default zero, bound PPM/budget/deadline/in-flight work, preserve active permits, and increase sampling only after capacity checks.
- [Context-ignoring work leaks goroutines] → Acquire before launch, retain permits until actual return, never queue/retry, and bound shutdown.
- [Simulation drifts from future activation] → Version and share pure reservation/ranking rules with the dormant provider change.
- [Topic metadata is sparse] → Report unknown coverage explicitly and never invent semantic topics.
- [Low sample sizes overstate certainty] → Report denominators/uncertainty, require deterministic tests plus golden/human review, and allow `inconclusive`.
- [Observational relevance is mistaken for lift] → State repeatedly that causal lift requires a separate accepted online experiment.

## Migration Plan

1. Complete and validate the index, profile, and dormant provider-registration changes.
2. Add sampler, configuration, lifecycle, no-queue admission, diagnostics, simulation, metrics, and tests with `sample_ppm=0`.
3. Wire bounded copied baseline inputs without any production artifact interface.
4. Add the versioned golden set workflow, human-review guidance, and report template.
5. Deploy disabled and prove production outputs/artifacts are invariant.
6. Enable a small bounded PPM when capacity allows; produce operational, simulation, diversity, and golden/human evidence.
7. Keep every production policy unchanged. Any active gray requires a separate accepted proposal.

Rollback sets `sample_ppm=0`; no schema, policy, snapshot, log, evidence, or attribution rollback is needed.

## Open Questions

None. Default-zero sampling, shadow-first ordering, production isolation, capacity, coverage/staleness, simulation metrics, low-volume golden/human relevance, uncertainty reporting, and no-causal-lift scope are fixed.
