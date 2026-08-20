## ADDED Requirements

### Requirement: Conditional Future Activation
The recommendation training dataset exporter SHALL remain inactive and outside the current low-data roadmap until a reviewed activation record satisfies every gate.

#### Scenario: Activation is requested
- **WHEN** an owner proposes implementing the exporter
- **THEN** the record names the exact training decision, supplies preregistered numeric row/user/request/split and exposure/label coverage thresholds, records privacy/security approval for deletion and opt-out handling, and assigns database/runtime/storage budgets and owners with no unresolved values

#### Scenario: Any activation gate is missing
- **WHEN** training purpose, evidence threshold, label coverage, privacy approval, or resource budget is absent or unapproved
- **THEN** implementation remains indefinitely deferred and no exporter command, repository, migration, or scheduled job is introduced

#### Scenario: Offline evaluation is planned
- **WHEN** Frux performs low-data scorer replay, human golden-set evaluation, or optional observational diagnosis
- **THEN** that work proceeds without requiring this exporter or treating evaluation approval as exporter activation

### Requirement: Bounded Read-Only Operator Export
After activation, Frux SHALL provide an operator-only command that exports recommendation training rows for a required half-open UTC served-time window and label horizon without modifying source facts, policies, evidence, or retention.

#### Scenario: Operator exports a valid closed window
- **WHEN** the operator supplies valid `from`, `to`, `as-of`, label-horizon, output, split, and HMAC-key inputs within configured maximum bounds
- **THEN** the command reads durable training impressions and linked durable outcomes/behavior facts and produces an offline dataset for impressions with `from <= served_at < to`

#### Scenario: Window is absent, open, or too large
- **WHEN** the operator omits a boundary, supplies a non-UTC or unordered boundary, exceeds the maximum export window or label horizon, or chooses an `as-of` before all requested labels can close
- **THEN** the command fails validation before querying rows or creating a final artifact

#### Scenario: Export command executes
- **WHEN** the exporter reads recommendation sources
- **THEN** it uses read-only database transactions and does not insert, update, delete, claim, lease, acknowledge, or extend retention for any source row

### Requirement: Training-Impression Dependency and Version Gate
The exporter SHALL depend on the durable facts planned by `persist-recommendation-training-impressions` and SHALL export only explicitly supported record, feature-schema, and source-model versions.

#### Scenario: Supported source versions are present
- **WHEN** every selected impression uses a registered record and feature-schema version and its immutable scene/policy configuration resolves to a supported source-model version
- **THEN** the exporter writes those versions into each row and summarizes every distinct source version in the manifest

#### Scenario: Unsupported or unresolved version is encountered
- **WHEN** a selected impression, feature payload, policy, or source-model identifier is not supported by the selected dataset schema version
- **THEN** the command fails explicitly, names only the bounded unsupported version identifiers, and does not publish a dataset

#### Scenario: Dependency schema is unavailable
- **WHEN** the training-impression table or required versioned fields and indexes from `persist-recommendation-training-impressions` are absent
- **THEN** preflight fails with an actionable dependency error before output publication

### Requirement: Deterministic Privacy-Bounded Row Schema
Dataset schema version 1 SHALL emit only an enumerated deterministic row schema with pseudonymous stable account identity, immutable generation identity, and bounded ranking, delivery, behavior, and label fields.

#### Scenario: Row is serialized
- **WHEN** an eligible impression is exported
- **THEN** the row contains dataset/source schema versions, a domain-separated HMAC-SHA-256 user key, a domain-separated request grouping key, immutable generation, video ID, scene, generation-relative absolute rank position, policy and source-model versions, bounded reasons and score components, served/exposed/engagement facts, bounded watch fields, label booleans and times, primary label, and split

#### Scenario: HMAC key is invalid or unavailable
- **WHEN** the required operator key is missing, shorter than 32 bytes, unreadable, or supplied through an unsupported insecure input
- **THEN** the command fails before reading source rows and never writes the key or raw key material to logs, checkpoints, rows, or the manifest

#### Scenario: Sensitive source fields exist
- **WHEN** joined records or policy configuration contain tokens, URLs, raw account IDs, profile vectors, embeddings, arbitrary request context, device metadata, event payloads, or raw errors
- **THEN** none of those fields are serialized into rows, checkpoints, metrics, or the manifest

#### Scenario: Collection ordering is serialized
- **WHEN** reasons or score components are written
- **THEN** reasons preserve their bounded source order, score components use a canonical name-sorted list with finite normalized numbers, JSON fields use a fixed order, and all timestamps use canonical UTC encoding

### Requirement: Delivered, Exposed, and Engaged State Semantics
The dataset SHALL distinguish delivered, exposed, and engaged states and SHALL treat a delivered impression without a validated exposure as unlabeled rather than negative.

#### Scenario: Delivered card has no exposure or engagement
- **WHEN** no validated `exposed` outcome and no eligible positive engagement exist in the label horizon
- **THEN** the row state is `delivered_unexposed`, negative-label eligibility is false, and the primary label is `unobserved`

#### Scenario: Card is exposed without engagement
- **WHEN** a validated exposure exists but no eligible playback, interaction, follow, or feedback signal exists
- **THEN** the row state is `exposed_unengaged` and the primary label is `exposed_only`

#### Scenario: Card has eligible post-delivery behavior
- **WHEN** at least one eligible playback, interaction, follow, or feedback signal is linked to the same user, request, and video after delivery and within the label horizon
- **THEN** the row state distinguishes `engaged_exposed` from `engaged_unexposed` and records the applicable independent facts and event times

#### Scenario: Unexposed card has a negative signal
- **WHEN** skip or negative feedback is linked to a delivered card but no validated exposure exists
- **THEN** the row may retain the observed bounded signal for auditability, negative-label eligibility remains false, and the primary label is not negative

#### Scenario: Outcome predates delivery or exceeds the horizon
- **WHEN** an outcome or behavior fact occurs before `served_at` or after the impression's bounded label cutoff
- **THEN** it does not affect state, watch aggregates, labels, or event times for that row

### Requirement: Deterministic Label and Watch Aggregation
Dataset schema version 1 SHALL define deterministic aggregation, label precedence, and bounded watch calculations over validated outcomes and their linked rich behavior facts.

#### Scenario: Outcome time is evaluated
- **WHEN** an outcome has both `occurred_at` and `recorded_at`
- **THEN** `occurred_at` determines behavioral ordering and inclusion in the delivery label horizon, while `recorded_at` determines visibility under `as_of` and the captured source watermark

#### Scenario: Late fact is recorded
- **WHEN** an event occurred inside the label horizon but was recorded after `as_of` or above the captured source watermark
- **THEN** the event is excluded from this deterministic snapshot and the manifest exposes the applicable watermark and late-arrival semantics

#### Scenario: Multiple label classes are present
- **WHEN** a row has several eligible outcomes
- **THEN** independent booleans are retained and the primary label uses this precedence among eligible labels: `not_interested`, `reduce_author`, `already_seen`, `favorite`, `like`, `follow`, `complete`, `meaningful_watch`, `skip`, `exposed_only`, then `unobserved`, with negative entries eligible only after validated exposure

#### Scenario: Multiple events have equal precedence
- **WHEN** more than one eligible event can supply the same label or event time
- **THEN** aggregation uses stable earliest/latest rules and a deterministic `(recorded_at, occurred_at, outcome_type, stable source identity)` tie-break without exporting the source identity

#### Scenario: Watch facts are aggregated
- **WHEN** validated play, progress, complete, or skip outcomes have linked behavior facts
- **THEN** effective watch time, media position, and duration use non-negative bounded values, effective watch time is capped by the dataset-schema maximum, and watch ratio is `clamp(effective_watch_ms / duration_ms, 0, 1)` when a valid bounded duration exists

#### Scenario: Duration is absent or invalid
- **WHEN** no eligible positive duration exists
- **THEN** bounded effective watch time remains available, watch ratio is null, and completion is derived only from a validated complete outcome rather than an inferred ratio

#### Scenario: Duplicate or out-of-order facts exist
- **WHEN** durable facts contain retries, duplicate projections, multiple playback sessions, or occurrence order differs from recording order
- **THEN** stable source identities deduplicate events and schema-defined min/max aggregation produces the same row independent of database return order

### Requirement: Leakage-Safe Deterministic Dataset Splits
The exporter SHALL assign every emitted row to exactly one deterministic `train`, `validation`, or `test` split using a validated time-based or pseudonymous-user-based strategy.

#### Scenario: User-based splitting is selected
- **WHEN** the operator supplies validated train/validation/test bucket percentages
- **THEN** a domain-separated HMAC bucket assigns all rows for one pseudonymous user to one split without exposing the raw user ID

#### Scenario: Time-based splitting is selected
- **WHEN** the operator supplies ordered UTC split boundaries and an embargo at least as large as the label horizon
- **THEN** rows are assigned by served time, embargo rows are excluded and counted, and no row's label horizon crosses into a later split

#### Scenario: Split inputs are unsafe
- **WHEN** percentages do not total 100, time boundaries fall outside the export window, an embargo is too short, or strategies are mixed ambiguously
- **THEN** validation fails before source reads or output creation

### Requirement: Streaming Dataset and Integrity Manifest
The exporter SHALL stream canonical JSON Lines through deterministic gzip encoding and SHALL publish a manifest only after the dataset file is complete and verified.

#### Scenario: Export completes
- **WHEN** all pages are written successfully
- **THEN** the command closes and syncs the gzip JSONL file, computes its SHA-256 checksum and byte size, reconciles privacy and every source watermark, and atomically publishes the dataset plus a canonical manifest containing dataset schema/tool versions, requested and effective windows, label definitions and bounds, split policy, pseudonymization version, row/state/label/split counts, excluded counts, source policy/model/schema versions, complete source watermarks, file checksum, and file size

#### Scenario: Same snapshot and inputs are exported again
- **WHEN** source facts, exporter/tool version, HMAC key, page size, compression settings, and command inputs are identical
- **THEN** row ordering, JSON bytes, gzip bytes, checksum, counts, and split assignments are identical

#### Scenario: Output path already contains final files
- **WHEN** the operator has not explicitly selected the supported overwrite behavior
- **THEN** the command fails without replacing or appending to the existing dataset or manifest

### Requirement: Bounded Pagination, Resume, Cancellation, and Cleanup
The exporter SHALL use stable keyset pagination, bounded memory, cancellation-aware queries and writes, and a validated local checkpoint protocol.

#### Scenario: More rows exist than one page
- **WHEN** an export spans multiple pages
- **THEN** impressions are read in repeatable `(served_at, id)` order with a fixed bounded page size and no offset pagination, duplicate row, or skipped boundary row

#### Scenario: Operator cancels between committed pages
- **WHEN** cancellation is observed
- **THEN** the command stops new reads, closes resources, leaves no final artifact, and may retain only an explicitly resumable private partial file and checkpoint at the last fsynced page boundary

#### Scenario: Operator resumes an interrupted export
- **WHEN** the partial file and checkpoint match the configuration fingerprint, tool/dataset versions, HMAC key identifier, source window, complete source-watermark set, split configuration, page size, and committed byte offset
- **THEN** the exporter truncates to the committed offset, continues after the saved keyset cursor, and produces the same ordered final artifact without duplicates

#### Scenario: Export fails validation, query, serialization, write, sync, or checksum
- **WHEN** the failure is not an operator cancellation eligible for resume
- **THEN** incomplete output, checkpoint, and manifest files are removed, final paths remain untouched, and the error contains no raw source payload or identity

### Requirement: Indexed Query Plan and Bounded Resource Use
The export implementation SHALL provide indexes and query-plan verification for the impression page scan and request-linked outcome/behavior joins.

#### Scenario: Export query runs
- **WHEN** the repository reads a bounded impression page and its label facts
- **THEN** PostgreSQL can use the training-impression `(served_at, id)` index and composite user/request/video/time indexes for outcomes and behavior facts rather than scanning an unbounded source table

#### Scenario: Query-plan integration test runs
- **WHEN** realistic skewed fixtures are loaded and `EXPLAIN` is evaluated
- **THEN** the tested export query uses bounded index/keyset access, keeps joins within the current impression page, and avoids per-row application queries

#### Scenario: Large bounded window is exported
- **WHEN** row count approaches the supported operational maximum
- **THEN** memory remains proportional to one configured page plus bounded aggregates, and progress reporting uses only counts and coarse time ranges without identity labels

### Requirement: Privacy, Retention, and Consumer Documentation
Frux SHALL document the dataset contract, privacy boundary, retention responsibility, operational use, and downstream dependency without expanding this change into training or evaluation.

#### Scenario: Privacy state is captured
- **WHEN** an export snapshot is created
- **THEN** the exporter captures a privacy deletion/opt-out watermark, excludes ineligible users, and aborts publication if a later privacy state invalidates selected rows before atomic publication

#### Scenario: Operator reviews export documentation
- **WHEN** preparing an export
- **THEN** documentation explains key custody, pseudonym stability, enumerated fields, source and output retention, secure file permissions, transfer/deletion responsibilities, unsupported-version failure, cancellation/resume behavior, and that source retention is unchanged

#### Scenario: Implementation is validated
- **WHEN** automated coverage runs
- **THEN** CLI validation, version rejection, deterministic serialization/checksum, label precedence, unexposed handling, watch bounds, split leakage controls, pagination/resume, cancellation/cleanup, privacy exclusions, indexed PostgreSQL joins, and realistic mixed-event fixtures are tested

#### Scenario: Future training consumes an export
- **WHEN** a separately activated training capability uses this versioned export
- **THEN** it validates generation identity, occurred/recorded semantics, all source watermarks, privacy eligibility, and the atomic manifest without adding training behavior to this exporter
