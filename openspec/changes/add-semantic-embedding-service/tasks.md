## 1. Recommendation Roadmap Gate

- [ ] 1.1 Verify the four prerequisite recommendation changes are implemented, accepted, and archived.
- [ ] 1.2 Add a preflight proving this provider-adapter change cannot be applied before those gates.

## 2. Neutral Contract and Canonicalizer

- [ ] 2.1 Define the narrow Go `SemanticEmbedder` port, neutral batch/result types, fixed contract identity, and bounded error taxonomy without provider SDK types.
- [ ] 2.2 Implement `semantic-text-v1` NFKC/whitespace/control handling, composition, exact bounds, and versioned SHA-256 text hashing.
- [ ] 2.3 Add contract constants/config validation for fixed provider, model, immutable revision, dimension, canonicalizer, timeout, batch, QPS, concurrency, pricing revision, quota, and budget.
- [ ] 2.4 Add tests proving requests cannot select another provider/model/revision/dimension/canonicalizer and any identity change requires a new rebuild scope.

## 3. Privacy and Secret Boundary

- [ ] 3.1 Define an eligibility boundary that allows only currently published/public title and description to become `CanonicalText`.
- [ ] 3.2 Ensure outbound payloads contain only canonical text and fixed model selection, with no user/video/business/request/trace IDs, behavior data, URLs, drafts, tokens, or arbitrary metadata.
- [ ] 3.3 Load provider credentials only from approved secret/config injection and add tests proving no credential/header/derived value enters jobs, vectors, cache, Kafka, checkpoints, logs, traces, metrics, or errors.
- [ ] 3.4 Disable provider SDK payload/debug logging and add redaction tests for all success and failure paths.

## 4. Provider Adapter, Cache, and Validation

- [ ] 4.1 Implement one configured provider adapter with bounded transport and exactly one network attempt per call.
- [ ] 4.2 Add full-contract text-hash batch deduplication and a narrow cache port that stores no raw text or credentials.
- [ ] 4.3 Validate response bounds, complete positional order/count, exact dimension, finite values, positive norm, deterministic L2 normalization, and unit tolerance atomically.
- [ ] 4.4 Add provider sandbox/fixture tests for valid, partial, extra, reordered, malformed, wrong-model, wrong-dimension, NaN, infinity, zero-vector, cancellation, and oversized responses.

## 5. Rate Limits, Gate, Cost, and Quota

- [ ] 5.1 Enforce timeout, maximum batch, in-flight, QPS, and burst limits before payload construction.
- [ ] 5.2 Parse bounded `Retry-After` and classify timeout/network/429/5xx as retryable while auth/input/model/contract/config failures remain operator-actionable.
- [ ] 5.3 Implement the replica-local circuit/gate with bounded open/half-open behavior and tests proving it affects asynchronous semantic work only.
- [ ] 5.4 Implement local billable-unit/cost estimation bound to a pricing revision, actual usage capture when available, quota/budget gates, and bounded-cardinality metrics.

## 6. Asynchronous-Only Composition and Documentation

- [ ] 6.1 Wire the adapter only for durable semantic job and resumable backfill callers; add dependency tests preventing API, publication, Feed, ranking, profile, and Kafka-handler inference calls.
- [ ] 6.2 Document the managed-provider contract, privacy boundary, secret handling, canonicalizer, identity/rebuild policy, cache, retry ownership, cost/quota operations, and permanent `hash-ngram-v1` fallback.
- [ ] 6.3 Remove all obsolete local Python/MiniLM, PyTorch, model artifact, inference process, CPU worker, model container, and Compose model-service requirements.

## 7. Validation

- [ ] 7.1 Run targeted Go tests for canonicalization, identity, config, adapter, cache, validation, redaction, metrics, circuit, cost, and quota behavior.
- [ ] 7.2 Run provider contract fixtures without real credentials in committed test data and verify no synchronous product path depends on provider availability.
- [ ] 7.3 Build the Go entrypoints affected by shared interfaces, run the complete Go suite, and run `openspec validate --all --strict`.
