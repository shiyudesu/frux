## Context

Frux already has a dormant `semantic_session` Recall Provider. It builds a short-term interest vector
from trusted current/recent behavior, reads only existing active-contract video vectors, executes
bounded PostgreSQL Exact retrieval, and emits `semantic_similarity`; it makes no model call during a
Recommendation request. Real acceptance has verified this path under a temporary policy, but normal
`recommend/v1` and `recommend/v2` requests still omit it.

The next step must measure production-shaped availability and candidate value without changing the
active request. Organic traffic is expected to remain small, so runtime metrics need a deterministic
offline replay companion rather than a statistical A/B framework. PostgreSQL, Redis, Kafka, policy
rows, request logs, delivery evidence, and attribution must remain untouched by Shadow work.

## Goals / Non-Goals

**Goals:**

- Sample first-page Recommendation requests deterministically with a default rate of zero.
- Run the existing Session Semantic builder and Provider asynchronously with a copied bounded input,
  a process-owned context, an independent no-queue admission limit, and a strict deadline.
- Compare semantic candidates with a copied completed active ordering and simulate a registered
  reservation/ranking profile entirely in memory.
- Expose bounded coverage, safety, latency, contribution, overlap, survival, displacement, and
  diversity observations without candidate/vector persistence or high-cardinality labels.
- Provide deterministic local replay fixtures and canonical aggregate reports for low-volume evidence.
- Prove exact invariance of active results and bounded shutdown even when a dependency ignores
  cancellation.

**Non-Goals:**

- Selecting or activating a semantic policy, changing `recommend/v1`/`recommend/v2`, or returning a
  Shadow candidate to a user.
- Calling Tongyi or another external model, generating/backfilling vectors, or changing multimodal
  facts/projections.
- Adding pgvector, HNSW/ANN, a long-term semantic user profile, training data, learned weights, an A/B
  framework, causal-lift claims, a public API, or Web UI.
- Persisting Shadow request rows, candidate IDs, vectors, scores, raw contexts, or user identifiers.

## Decisions

### 1. Put Shadow behind an independent runtime configuration

Add `multimodal.session_shadow` with `enabled`, `sample_ppm`, `budget`, `deadline`, `max_in_flight`,
`comparison_limit`, `simulated_pool_limit`, `simulated_top_k`, `semantic_reservation`,
`semantic_weight`, and `shutdown_timeout`. Checked-in configuration uses `enabled=false` and
`sample_ppm=0`.

Enabling Shadow requires the existing multimodal runtime, active contract, Exact repository, and
Session Semantic runtime to be available. It does not require video jobs, query embeddings, Hybrid
Search, the Adapter, Redis, Kafka, or an active semantic policy.

Alternative considered: store Shadow controls in recommendation policy JSON. Rejected because Shadow
must not mutate, select, or semantically reinterpret active delivery policy rows.

### 2. Sample with a versioned length-delimited SHA-256 bucket

`session-semantic-shadow-sampler-v1` hashes normalized scene plus decimal user ID and canonical request
ID using explicit lengths. The first fixed-width integer maps to `[0,1_000_000)`. Sampling occurs only
for authenticated `scene=recommend` first pages after a request ID exists. Retries and replicas select
the same request; zero and one-million PPM are exact boundaries.

Alternative considered: reuse request-log FNV sampling. Rejected so Shadow has an independently
versioned contract and can change sampling without affecting persisted request-log selection.

### 3. Admit before creating a goroutine and detach from the HTTP context

`SessionSemanticShadowEvaluator.TryEvaluate` performs enablement, input validation, sampling, and a
non-blocking semaphore acquisition synchronously. Only admitted work creates a goroutine. The
goroutine receives immutable clones of recommendation context, active policy, and the completed
active ranked ordering; it derives its deadline from a process-owned root context rather than the
request context.

The semaphore permit is released only when the actual Provider call returns. A timed-out dependency
that ignores cancellation therefore remains counted and prevents new work instead of accumulating
goroutines. `Close(ctx)` rejects new work, cancels the root context, and waits only until the bounded
shutdown context expires.

Alternative considered: use normal recommendation Provider slots. Rejected because diagnostic work
must never reduce production recall capacity.

### 4. Launch only after the active ordering is complete

The service offers a read-only hook after active recall, ranking, diversity, suppression, and cursor
filtering have produced the complete bounded ordering and before response slicing. Shadow receives
clones only and has no reference to Snapshot stores, request-log repositories, delivery evidence,
response DTOs, degradation state, or attribution writers. Launch success, failure, timeout, or
capacity exhaustion cannot alter the active return path.

Snapshot hits and cursor pages do not launch Shadow because their first-page ordering has already
been decided. Repository fallback with no healthy active Provider may still be compared, but its
baseline state is explicitly labeled.

Alternative considered: await Shadow before returning the first page. Rejected because even a strict
deadline would add latency and make optional diagnostics a product dependency.

### 5. Build a synthetic Shadow policy without changing stored policy rows

The evaluator clones the selected active policy in memory and adds the existing registered
`semantic_session` budget/deadline, pinned active contract, builder settings, and positive
`semantic_similarity` weight. It creates `shadow-mix-v1` using the active Provider order when present,
otherwise the fixed built-in Provider order, followed by `semantic_session`. Baseline reservations
are preserved when present; otherwise they are zero, while the configured semantic reservation is
bounded by its budget and the simulated pool limit.

Active ranked candidates are partitioned by their retained Recall Reasons to reconstruct bounded
baseline Provider sequences. Semantic candidates are visibility-revalidated, added only to the
simulation, passed through the existing deterministic Quota Merge, and reranked through the existing
feature/ranking implementation. Simulation uses a dedicated read-only helper and never writes a
Snapshot, log, evidence row, policy, exposure, or feedback.

Alternative considered: compare semantic cosine directly with active rank score. Rejected because
scores from different components are not comparable without the production ranker and policy weights.

### 6. Record aggregates through an observer boundary

The application evaluator emits a closed `SessionSemanticShadowObservation` containing only enums,
bounded counts, finite ratios, confidence band, and durations. The Prometheus observer maps these to
allowlisted labels and histograms. User/request/session/video IDs, model strings, contract keys,
vectors, raw scores, query text, SQL details, and error text are never labels or normal log fields.

The runtime does not persist per-request observations. Operational evidence comes from Prometheus
aggregates; deterministic development evidence comes from the replay command.

Alternative considered: add a Shadow database table. Rejected because persisted per-request
diagnostics increase privacy and retention cost without helping the current low-volume gate.

### 7. Keep low-volume replay pure and deterministic

`cmd/session-semantic-shadow-eval` reads a versioned bounded JSON fixture containing opaque case-local
candidate identities, provider/relevance metadata, active ordering, semantic ordering/scores, and
expected safety state. It performs no database, HTTP, Redis, Kafka, S3, or model access. The command
uses the same pure comparison/simulation primitives and atomically writes permission-restricted JSON
and Markdown reports containing only aggregate metrics, provenance hashes/counts, denominators,
warnings, and non-causal limitations.

Identical input and options produce byte-identical reports. Insufficient cases or label coverage
produce `inconclusive`, never automatic rollout approval.

Alternative considered: wait for organic Prometheus samples only. Rejected because a personal project
cannot promise useful denominators in a reasonable time.

### 8. Treat native and Compose multimodal endpoints as separate boundaries

The native `.env.multimodal` example keeps the plaintext Adapter on host loopback. Default Docker
Compose remains model-disabled and must not be started with that loopback file as though container
`127.0.0.1` were the host. A future containerized Adapter requires a separate explicit Compose profile
and internal endpoint; this change only corrects documentation.

## Risks / Trade-offs

- **[Shadow work increases PostgreSQL Exact reads]** → Default sampling is zero, admission is no-queue,
  budget/deadline/concurrency are tightly bounded, and capacity rejection is observable.
- **[Context-ignoring calls outlive the deadline]** → Permits remain held until actual return and
  shutdown is bounded; no retry or replacement goroutine is created.
- **[Reconstructed Provider sequences differ from pre-rank raw pools]** → Reports call the result a
  post-active-order simulation, retain explicit denominators, and do not claim causal production lift.
- **[Small fixtures overstate relevance]** → Require versioned provenance, explicit label coverage,
  deterministic metrics, warnings, and `inconclusive` below the configured evidence gate.
- **[Metrics lose per-request debugging detail]** → This is intentional privacy minimization; existing
  sampled active request logs remain unchanged and Shadow fixtures cover deterministic diagnosis.
- **[Shadow configuration accidentally becomes an activation mechanism]** → It owns no policy write,
  policy selection, response merge, or ranking return path; invariance tests compare exact outputs.

## Migration Plan

1. Add configuration, sampler, pure comparison types, and tests with Shadow disabled.
2. Add evaluator lifecycle and service hook, still disabled in every checked-in config.
3. Compose the evaluator only when complete prerequisites are configured and register bounded shutdown.
4. Add Prometheus metrics, replay fixtures/reporting, and documentation.
5. Enable only in a local/dev override with small sampling and confirm active-output invariance plus
   resource gates; rollback is `enabled=false` or `sample_ppm=0` and requires no data migration.

## Open Questions

None for implementation. Any active cohort percentage, rollout reservation, or relevance promotion
threshold belongs to a later independent Rollout change.
