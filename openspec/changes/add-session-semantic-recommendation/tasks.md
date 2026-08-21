## 1. Domain contract and policy registration

- [x] 1.1 Add closed session-semantic signal, result, confidence-band, evidence, builder-version, and configuration types with strict bounds and defensive cloning.
- [x] 1.2 Register `semantic_session` and `semantic_similarity` without changing the meaning or defaults of existing Provider and feature names.
- [x] 1.3 Extend optional Recommendation policy configuration with a complete session-semantic block, contract-key matching, Provider/feature cross-validation, serialization, cloning, and backward-compatible normalization.
- [x] 1.4 Add domain tests for unknown versions/signals, partial configuration, invalid bounds/contracts, existing policy JSON, and unchanged `recommend/v1`/`recommend/v2` behavior.

## 2. Session signal and vector builder

- [x] 2.1 Define narrow application interfaces for bounded trusted session facts and active-contract vector batches; keep user identity, provider clients, and persistence models outside the builder.
- [x] 2.2 Implement canonical signal selection for current, complete, sustained, like, favorite, early skip, `not_interested`, `already_seen`, and existing author suppression precedence with duplicate/order stability.
- [x] 2.3 Implement registered v1 weights, time decay, positive-evidence requirement, capped negative subtraction, exclusions, finite/dimension checks, L2 normalization, and canonical input digest.
- [x] 2.4 Implement deterministic confidence coverage/strength/coherence/freshness terms, confidence bands, eligibility gate, confidence-scaled output bound, and closed healthy-unavailable results.
- [x] 2.5 Add table-driven builder and versioned Golden Set tests for reinforcing, contradictory, negative override, exclusion-only, missing-vector, contract-mismatch, near-zero, and replay-determinism cases.

## 3. PostgreSQL evidence adapters

- [x] 3.1 Implement one bounded batch query over context seed IDs that verifies recent server encounter/behavior evidence and projects only closed canonical playback signal facts.
- [x] 3.2 Join or batch-load current LIKE/FAVORITE state and accepted recommendation feedback without treating `already_seen` or `reduce_author` as negative topic directions.
- [x] 3.3 Implement bounded active-contract multimodal vector loading with exact source/contract/current-state checks and no historical fallback or provider call.
- [x] 3.4 Add isolated PostgreSQL integration tests for user scoping, arbitrary client IDs, duplicate/out-of-order playback, feedback precedence, mixed vector coverage, contract rotation, bounds, and cancellation.

## 4. Semantic Recall and ranking integration

- [x] 4.1 Implement `SemanticSessionProvider` using the builder plus existing `ExactMultimodalSearch`, seed/hard-suppression exclusions, finite positive scores, deterministic confidence scaling, and bounded output.
- [x] 4.2 Feed `semantic_session` candidates through existing visibility revalidation, deduplication, Provider-local normalization, quota reservation/underfill, round-robin fill, and common Recall deadline/capacity controls.
- [x] 4.3 Populate registered `semantic_similarity` only from valid semantic Provider evidence while preserving hash `session_similarity`, existing feature weights, suppression, diversity, tie-breaking, and fallback.
- [x] 4.4 Propagate closed semantic result/degradation metadata through first-page Recall execution without changing public Feed DTOs or Snapshot cursor contracts.
- [x] 4.5 Add Provider, Ranker, quota, cancellation, timeout, overlap, underfill, visibility, suppression, and no-external-model-call tests, including race coverage for concurrent requests.

## 5. Runtime configuration and composition

- [x] 5.1 Add `multimodal.session_recommendation_enabled` and bounded session-semantic runtime settings to local, Docker, production, and sample configuration with every checked-in default false.
- [x] 5.2 Extend multimodal configuration normalization so enabled session recommendation requires a complete active contract and Exact retrieval but does not require query embedding or an upstream provider endpoint.
- [x] 5.3 Reorder/reuse multimodal PostgreSQL repository assembly and conditionally register the semantic Provider only after complete dependency validation.
- [x] 5.4 Add startup/configuration tests for disabled defaults, complete runtime, partial dependencies, policy/runtime contract mismatch, and enabled runtime with a policy that omits semantic recall.

## 6. Evidence, metrics, and Snapshot stability

- [x] 6.1 Extend sampled request-log JSON with a bounded optional session-semantic summary and compaction/validation that excludes raw vectors, raw events, query text, credentials, and unbounded identifiers.
- [x] 6.2 Add fixed-cardinality builder/Provider metrics and histograms for result, closed reason, confidence band/value, coverage, signal/vector/candidate counts, underfill, and duration.
- [x] 6.3 Verify first-page semantic evidence and score components enter the existing Snapshot ordering and that later pages never rebuild the session vector or rerun Exact retrieval.
- [x] 6.4 Add request-log backward compatibility, privacy, size-bound, Snapshot continuation/fallback, and metrics-label tests.

## 7. End-to-end verification and documentation

- [x] 7.1 Add recommendation flow tests proving positive semantic direction, negative suppression, missing-vector fallback, Exact timeout degradation, stable pagination, and coexistence with every existing Provider.
- [x] 7.2 Add a reproducible development acceptance fixture/report for `session-semantic-v1` that makes zero external model calls and labels results as technical/offline evidence only.
- [x] 7.3 Update recommendation module, architecture/engineering guidance, product capability status, and the recommendation roadmap without describing dormant semantic recall as rolled out.
- [x] 7.4 Run targeted race tests, complete Go tests/vet/build, optional PostgreSQL tests, Compose validation, and strict OpenSpec validation.
- [x] 7.5 Confirm active bootstrap policies and checked-in flags remain unchanged/disabled and no raw vectors, behavior payloads, user/request/session/video IDs in metric labels, provider calls, Backfill, ANN, or training dependencies were introduced.
