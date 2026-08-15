## 1. Dependency and Dataset Contract

- [ ] 1.1 Verify `persist-recommendation-training-impressions` is implemented with final record/feature version constants, immutable scene/policy metadata, request linkage, and the `(served_at, id)` export index; block exporter implementation if the dependency schema or a stable source-model version cannot be resolved without guessing.
- [ ] 1.2 Add `recommendationdataset` domain types and unit-tested validation for dataset-v1 rows/manifests, source-version sets, delivery states, labels, splits, event times, watch bounds, cursors, typed errors, half-open UTC windows, the 31-day window limit, the 7-day label-horizon limit, closed `as-of`, page-size bounds, output/resume/overwrite modes, and dataset schema selection.
- [ ] 1.3 Implement and test domain-separated HMAC-SHA-256 user/request/split derivation and non-secret key identifiers, including stability, cross-domain and cross-user isolation, minimum 32-byte keys, and exclusion of raw identity or key material from errors.

## 2. Read-Only Persistence and Query Plans

- [ ] 2.1 Define narrow application repository contracts for dependency preflight, source-boundary capture, policy/version loading, and keyset-paged impression/outcome/behavior reads without exposing GORM types.
- [ ] 2.2 Implement the PostgreSQL read repository with read-only transactions, captured maximum impression ID, `(served_at, id)` pagination, page-scoped set-based joins on exact user/request/video identity, `as-of` and label-horizon filters, stable event tie data, cancellation, and bounded query count.
- [ ] 2.3 Add the measured covering composite indexes for request-linked outcome and behavior reads, registering them through the shared advisory-locked migration conventions.
- [ ] 2.4 Add PostgreSQL repository, migration, realistic skewed-fixture, and `EXPLAIN (FORMAT JSON)` tests covering boundaries and later pages, equal timestamps, snapshot limits, unsupported facts, rich-event linkage, duplicates, cancellation, read-only enforcement, keyset/index access, page-scoped joins, and absence of application N+1 queries or unbounded sequential scans.

## 3. Version Registry and Deterministic Rows

- [ ] 3.1 Implement the dataset-v1 compatibility registry from the dependency's final constants, supported score-component schema, immutable policy configuration, and bounded source-model identifiers; fail on missing policy, malformed configuration, unknown components, or unsupported record/feature/model versions.
- [ ] 3.2 Implement canonical row assembly and encoding with fixed JSON field order, UTC RFC3339Nano timestamps, preserved bounded reason order, name-sorted finite score components, explicit delivery/engagement times and facts, and no arbitrary source payloads.
- [ ] 3.3 Implement deterministic outcome/behavior deduplication, delivery-state derivation, exposure-gated negative eligibility, primary-label precedence, stable event tie rules, and bounded per-session watch aggregation with six-hour caps, nullable ratios, validated completion, and meaningful-watch semantics.
- [ ] 3.4 Add table-driven aggregation fixtures and serialized-artifact privacy tests covering all delivery states, playback lifecycles and sessions, duplicates/out-of-order/equal-time events, invalid durations and cap overflow, conflicting labels, later-page rank gaps, supported/unsupported versions, and exclusion of raw identities, secrets, URLs, vectors, embeddings, arbitrary context, event/session IDs, policy JSON, and raw errors from rows, manifests, checkpoints, progress, and failures.

## 4. Leakage-Safe Split Assignment

- [ ] 4.1 Implement deterministic pseudonymous-user splitting over 10,000 HMAC buckets and deterministic time splitting with ordered cutoffs, embargoes at least as large as the label horizon, boundary exclusion, and manifest exclusion counts; reject mixed, incomplete, or unsafe configurations.
- [ ] 4.2 Add split tests proving repeatability, exactly one train/validation/test assignment per emitted row, no user crossing for user splits, no emitted label window crossing a later time split, exact cutoff/embargo handling, and validation of basis-point totals and boundaries.

## 5. Streaming Output, Resume, and Publication

- [ ] 5.1 Implement the canonical JSONL encoder and deterministic one-member-per-page gzip writer with fixed headers/settings/newlines, `0600` files, bounded buffers, fsynced page boundaries, streaming counts, and checkpoints containing cursor, committed offset, source boundaries, counts, versions, fingerprint, page/compression settings, and HMAC key identifier.
- [ ] 5.2 Implement resume validation and truncation to the last committed gzip-member boundary, plus page-boundary cancellation that retains private partial state only when explicit resume is enabled.
- [ ] 5.3 Implement non-resumable failure cleanup, final data sync, compressed-byte SHA-256 and size calculation, count reconciliation, canonical manifest generation, atomic publication, safe overwrite behavior, and checkpoint removal while preserving existing final files on failure.
- [ ] 5.4 Add filesystem integration tests for empty and multi-page exports, deterministic and resumed byte equality, concatenated-gzip readability, checksum/size/count accuracy, existing destinations, cancellation, fingerprint/key mismatch, injected query/encode/write/sync/checksum failures, cleanup, and final-file atomicity.

## 6. Operator Command

- [ ] 6.1 Add `apps/api/cmd/recommendation-dataset-export` with build-injected tool version, existing config/PostgreSQL setup, read-only connection/session safeguards, signal cancellation, bounded identity-safe progress, and no HTTP, Redis, Kafka, worker, or source-write wiring.
- [ ] 6.2 Add strict CLI parsing and preflight for required UTC window, `as-of`, label horizon, output, permission-restricted HMAC key file, dataset schema, page size, exactly one split strategy, resume, and safe overwrite, ensuring preflight finishes before final output creation.
- [ ] 6.3 Add command and end-to-end PostgreSQL tests covering help/usage, malformed flags, insecure or short key files, dependency/version failures, split validation, existing outputs, signals and exit codes, identity-safe stderr, and a repeatable mixed-user/request/video export whose decompressed rows, labels, splits, privacy bounds, ordering, checksum, and manifest are verified.

## 7. Documentation and Downstream Contract

- [ ] 7.1 Document prerequisites, closed-window and settle-lag selection, flags/examples, secure key custody and rotation, `0600` artifacts, resume/cleanup, checksum verification, unsupported-version/query failures, source joins, dataset-v1 fields/labels, indexes/query expectations, and operator responsibility for exported-file transfer, retention, and deletion without changing source retention or evidence.
- [ ] 7.2 Document the implementation sequence on `persist-recommendation-training-impressions` and preserve the versioned JSONL/manifest contract for the future `evaluate-recommendation-policies-offline` consumer, explicitly keeping training, policy scoring/evaluation, embeddings, pgvector, learned weights, exploration, and online serving out of scope.

## 8. Validation

- [ ] 8.1 Run the targeted recommendation-dataset domain/application, PostgreSQL repository/query-plan, migration, filesystem, command, privacy, split, and end-to-end fixture tests.
- [ ] 8.2 Run `cd apps/api && go test ./...`, `cd apps/api && go build ./cmd/feed ./cmd/worker ./cmd/recommendation-dataset-export`, and `openspec validate --all --strict`; confirm proposal, design, spec, dependency statements, downstream-consumer contract, and the consolidated task list remain consistent without modifying application code or main specs.
