## 1. Configuration and Deterministic Selection

- [x] 1.1 Add bounded `multimodal.session_shadow` configuration structures and checked-in disabled defaults to native, Docker, and production configs.
- [x] 1.2 Validate zero/default behavior, complete enabled prerequisites, PPM/budget/deadline/capacity/pool/top-K/reservation/weight/shutdown bounds, and rejected partial settings.
- [x] 1.3 Implement `session-semantic-shadow-sampler-v1` with length-delimited SHA-256 selection and tests for normalization, retry/replica stability, boundary PPM values, invalid inputs, and first-page eligibility.

## 2. Pure Shadow Comparison and Simulation

- [x] 2.1 Define privacy-bounded Shadow input, closed terminal outcome, aggregate observation, provider-sequence, and simulation result types with defensive cloning/validation.
- [x] 2.2 Implement deterministic active/semantic intersection, unique contribution, finite ratio, top-K displacement, author diversity, and empty-denominator handling.
- [x] 2.3 Build the in-memory `shadow-mix-v1` policy from an active policy and configured active contract without modifying the source policy or bootstrap policies.
- [x] 2.4 Reconstruct bounded Provider sequences from copied active candidates, visibility-revalidate semantic candidates, reuse Quota Merge and ranking for simulation, and test deterministic survival/fallback behavior.

## 3. Asynchronous Evaluator and Production Isolation

- [x] 3.1 Implement `SessionSemanticShadowEvaluator` with pre-goroutine deterministic selection, independent no-queue admission, process-owned context, one actual Provider call, panic containment, and at-most-once terminal observation.
- [x] 3.2 Hold permits until context-ignoring calls actually return; implement close/cancel/drain behavior with bounded shutdown and concurrency tests proving no queue, retry, or replacement goroutine forms.
- [x] 3.3 Add a read-only Service hook after completed active ordering and before response slicing, passing immutable clones and never exposing Snapshot, request-log, evidence, attribution, or response writers.
- [x] 3.4 Add exact production-invariance tests for Shadow disabled/selected success, unique candidates, empty, timeout, error, capacity, panic, Snapshot retry/page, and degraded cursor cases.

## 4. Composition and Observability

- [x] 4.1 Compose Shadow only when the complete multimodal Session Semantic + Exact prerequisites are enabled, while disabled startup requires no Shadow dependency and active provider selection remains policy-controlled.
- [x] 4.2 Register evaluator cancellation/drain with the API shutdown lifecycle and test enabled/disabled composition plus bounded shutdown.
- [x] 4.3 Add fixed-label Prometheus counters/histograms/gauge for selection, admission, terminal result, confidence, candidate counts, overlap/contribution/survival/displacement/diversity, in-flight work, and duration.
- [x] 4.4 Add metric/log allowlist tests proving IDs, request context, candidates, vectors, model/contract strings, SQL details, and raw errors never become labels or normal fields.

## 5. Low-Volume Replay and Canonical Reports

- [x] 5.1 Define and validate a versioned bounded Shadow fixture format with relative provenance, hashes/counts, opaque case-local identities, active/semantic candidates, relevance/author metadata, and no runtime endpoints or credentials.
- [x] 5.2 Implement `cmd/session-semantic-shadow-eval` using the pure comparison/simulation primitives with no PostgreSQL, Redis, Kafka, HTTP, S3, or model dependencies.
- [x] 5.3 Atomically write `0600` deterministic JSON and Markdown reports with aggregate denominators, safety/quality gates, warnings, `external_model_calls: 0`, and `inconclusive` handling.
- [x] 5.4 Add committed fixtures and byte-stability tests for valid, insufficient, malformed, unsafe-path, duplicate, oversized, unavailable, and conflicting-metadata cases.

## 6. Documentation and Verification

- [x] 6.1 Document disabled defaults, Exact-only/no-model behavior, sampling, admission, metrics, replay/report workflow, low-traffic limitations, enable/disable order, and rollback in recommendation and monitoring docs.
- [x] 6.2 Clarify native `.env.multimodal` loopback versus default Docker Compose boundaries so the host endpoint is not injected into containers as a working Adapter address.
- [x] 6.3 Update the recommendation roadmap and module index to mark Session Semantic Shadow implemented but not rolled out, with HNSW, long-term profiles, training, and causal claims still excluded.
- [x] 6.4 Run focused configuration/sampler/comparison/evaluator/service/composition/metrics/replay tests, `go test ./...`, build API/Worker/replay entrypoints, validate Compose, and run `openspec validate --all --strict`.
