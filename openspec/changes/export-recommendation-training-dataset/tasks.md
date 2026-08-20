## 1. Activation Gate

- [ ] 1.1 Keep this change inactive until a reviewed activation record names the exact training use and explains why low-data replay and human evaluation are insufficient.
- [ ] 1.2 Require preregistered numeric minimum rows, users, requests, per-split counts, validated exposure coverage, positive/negative label coverage, and maximum missing-label rate; reject `TBD` thresholds.
- [ ] 1.3 Obtain privacy/security approval for allowed fields, HMAC custody, deletion, training opt-out, export retention/transfer, and incident ownership.
- [ ] 1.4 Approve PostgreSQL query, maximum window/page/runtime, output storage/retention, operator ownership, and abort budgets. Do not begin sections 2-9 unless tasks 1.1-1.4 are complete.

## 2. Dependency and Dataset Contract

- [ ] 2.1 Verify `persist-recommendation-training-impressions` is implemented with final record/feature versions, immutable `(user_id, request_id, generation, video_id)` identity, generation-relative position, author/publication/policy/degraded metadata, served/recorded semantics, privacy boundaries, and export/watermark indexes.
- [ ] 2.2 Add `recommendationdataset` domain types and validation for dataset-v1 rows/manifests, source-version and watermark sets, delivery states, labels, splits, occurred/recorded event times, watch bounds, cursors, half-open UTC windows, label horizons, closed `as-of`, page-size bounds, output/resume/overwrite modes, and dataset schema selection.
- [ ] 2.3 Implement and test domain-separated HMAC-SHA-256 user/request/split derivation and non-secret key identifiers, including stability, isolation, minimum 32-byte keys, and exclusion of raw identity or key material from errors.

## 3. Read-Only Persistence, Watermarks, and Query Plans

- [ ] 3.1 Define narrow application repository contracts for dependency preflight, complete source-watermark capture, policy/video/privacy version loading, and keyset-paged impression/outcome/behavior reads without exposing GORM types.
- [ ] 3.2 Implement read-only PostgreSQL queries with frozen impression, outcome, behavior, privacy, policy, and video metadata watermarks; paginate `(served_at, id)` and join exact user/request/generation/video identity.
- [ ] 3.3 Apply `occurred_at` to behavioral ordering/horizons and `recorded_at` plus `as-of`/watermarks to snapshot visibility; cover late-arriving in-window events explicitly.
- [ ] 3.4 Add measured covering indexes and PostgreSQL query-plan tests for bounded page-scoped joins without N+1 queries or unbounded sequential scans.

## 4. Version Registry and Deterministic Rows

- [ ] 4.1 Implement the dataset-v1 compatibility registry from final dependency constants, supported score components, immutable policy configuration, and bounded source-model identifiers; fail on missing or unsupported semantics.
- [ ] 4.2 Implement canonical rows with fixed JSON order, generation identity, UTC timestamps, preserved reasons, sorted finite score components, explicit occurred/recorded facts, and no arbitrary payloads.
- [ ] 4.3 Implement deterministic deduplication, exposure-gated negative eligibility, label precedence, stable event ties, and bounded watch aggregation.
- [ ] 4.4 Add aggregation, late-arrival, generation, unsupported-version, and serialized-artifact privacy fixtures.

## 5. Leakage-Safe Split Assignment

- [ ] 5.1 Implement deterministic pseudonymous-user splitting and deterministic time splitting with ordered cutoffs, embargoes at least as large as the label horizon, boundary exclusion, and manifest counts.
- [ ] 5.2 Prove repeatability, exactly one split per row, no user crossing, and no emitted label window crossing a later time split.

## 6. Streaming Output, Resume, and Publication

- [ ] 6.1 Implement canonical deterministic gzip JSONL with `0600` files, bounded buffers, fsynced page boundaries, and checkpoints containing cursors, committed offsets, complete source watermarks, counts, versions, fingerprint, and HMAC key identifier.
- [ ] 6.2 Implement resume validation and page-boundary cancellation.
- [ ] 6.3 Reconcile privacy, every source watermark, counts, checksum, and size before atomically publishing dataset and manifest together; preserve existing finals on failure.
- [ ] 6.4 Add filesystem tests for deterministic/resumed bytes, watermark accuracy, privacy races, cancellation, cleanup, and atomicity.

## 7. Operator Command

- [ ] 7.1 After activation, add `apps/api/cmd/recommendation-dataset-export` with read-only PostgreSQL, signal cancellation, bounded identity-safe progress, and no HTTP, Redis, Kafka, worker, or source-write wiring.
- [ ] 7.2 Add strict CLI and preflight for UTC window, `as-of`, label horizon, output, restricted HMAC key, dataset schema, page size, one split strategy, resume, and overwrite.
- [ ] 7.3 Add command and PostgreSQL end-to-end tests covering malformed inputs, dependency/version/watermark failures, privacy changes, signals, and deterministic mixed-user/request/generation/video output.

## 8. Documentation and Future Consumer Contract

- [ ] 8.1 Document activation prerequisites, closed-window/settle-lag selection, occurred/recorded semantics, all-source watermarks, key custody, `0600` artifacts, resume/cleanup, checksum verification, privacy handling, and output retention/deletion.
- [ ] 8.2 Document that low-data offline evaluation does not depend on this exporter and that any future training consumer needs separate approval.

## 9. Validation

- [ ] 9.1 Run targeted dataset, PostgreSQL/query-plan, migration, filesystem, command, privacy, generation, occurred/recorded, watermark, split, and end-to-end tests.
- [ ] 9.2 Run the relevant Go suite/builds and `openspec validate --all --strict`; confirm future-only status, activation gates, identity/time contract, all-source watermarks, atomic manifest, and evaluation independence remain coherent.
