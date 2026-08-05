## Context

The active planned change `persist-recommendation-training-impressions` defines a compact durable row for every final readable recommendation card delivered by Feed. Its proposal, design, delta spec, and tasks establish `recommendation_training_impression` as the authoritative long-retention source with user/request/video identity, absolute position, scene, policy version, bounded reasons/components, served time, and explicit record/feature-schema versions. This export change must be implemented after that dependency and must not reconstruct impressions from sampled request logs or short-lived served-candidate evidence.

Validated request-linked outcomes already represent attribution-safe `exposed`, playback, interaction, follow, and feedback facts. `recommendation_behavior_event` retains richer playback fields such as position, cumulative effective watch time, duration, completion, playback session, occurrence time, and recording time. The exporter combines these sources offline without changing them.

The output is intended to become the input contract for the later `evaluate-recommendation-policies-offline` change. It is not a policy evaluator or training pipeline.

## Goals / Non-Goals

**Goals:**

- Add a read-only operator command for a required bounded, closed served-time window.
- Produce a canonical, streaming, resumable gzip JSONL dataset and integrity manifest.
- Export only supported source versions and fail closed on unknown semantics.
- Preserve useful delivery, ranking, watch, interaction, follow, and feedback facts while excluding direct account identity and unrelated sensitive payloads.
- Make state, label precedence, watch aggregation, split assignment, ordering, pagination, cancellation, and cleanup deterministic and testable.
- Use bounded indexed PostgreSQL queries and one-page memory.
- Document output retention and key custody separately from unchanged source retention.

**Non-Goals:**

- Implementing `persist-recommendation-training-impressions` inside this change or backfilling missing historical impressions.
- Model training, feature learning, policy scoring, counterfactual/offline evaluation, propensity estimation, exploration, experiment rollout, or online serving.
- Adding semantic embeddings, pgvector, raw profile vectors, learned weights, or arbitrary context to the export.
- Adding a public HTTP endpoint, browser workflow, scheduler, database write-back, or automatic upload to external storage.
- Changing attribution authorization, recommendation policy state, evidence expiry, or source retention.

## Decisions

### 1. Add a standalone operator binary with strict preflight

Add `apps/api/cmd/recommendation-dataset-export` using the existing config/database construction but no HTTP server, worker, Redis, or RabbitMQ. Its required inputs are:

- `--from`, `--to`, and `--as-of` UTC timestamps;
- `--label-horizon` with a documented default of 24 hours and maximum of 7 days;
- `--output` final `.jsonl.gz` path;
- `--hmac-key-file`, read from a permission-restricted file and containing at least 32 bytes;
- one split strategy and its complete parameters.

The supported served window defaults to no value and is capped at 31 days. Preflight requires `from < to`, `to + label_horizon <= as_of`, `as_of <= now`, valid split controls, nonexisting final output/manifest unless the explicit safe overwrite mode is selected, supported dataset schema version, and availability of the dependency tables/columns/indexes. The database connection is configured read-only and every repository transaction executes as read-only.

The binary exposes a build-injected tool version, with a stable development fallback for tests. It reports only bounded progress counts and coarse timestamps.

Alternative considered: an authenticated internal HTTP endpoint. Rejected because large exports, filesystem/key handling, cancellation, and operator authorization do not belong on the serving path.

Alternative considered: a worker job. Rejected because this is an explicit offline operation and must not claim queues or create durable job state.

### 2. Use an explicit dataset-v1 compatibility registry

Dataset schema `recommendation-training-dataset/v1` registers the supported training record versions, feature-schema versions, score component names/number encoding, label rules, watch bounds, and source-model resolution. The first implementation supports only the version introduced by `persist-recommendation-training-impressions` (expected record version `1` and feature schema `ranking-components-v1`); implementation must use the dependency's final constants rather than duplicate literals.

For each distinct `(scene, policy_version)`, the exporter loads the immutable `recommendation_policy` row. A registry adapter maps the supported feature schema and versioned policy configuration to a bounded source-model identifier such as the current deterministic ranking model contract. The manifest lists every distinct scene/policy, source-model, impression-record, feature-schema, and dataset-schema version. Missing policies, malformed configs, unknown feature components, or unresolved/unsupported model versions fail the export before publication.

This does not require changing the `recommendation-training-impressions` capability: impressions already retain the policy and feature-schema keys needed for resolution. If implementation discovers that the dependency's final policy record cannot resolve a stable source-model identifier, update that active dependency before implementing this exporter rather than guessing or labeling mixed semantics as one model.

Alternative considered: silently omit model metadata. Rejected because consumers could combine rows whose features have different meanings.

Alternative considered: export unknown versions as opaque JSON. Rejected because deterministic label/feature semantics cannot be guaranteed.

### 3. Export one canonical row per durable delivered impression

Rows are encoded from a struct, not a map, with a fixed field order:

- `dataset_schema_version`;
- `user_key`, `request_key`, and raw numeric `video_id`;
- `scene`, `served_at`, `absolute_position`, `policy_version`;
- `source_record_schema_version`, `source_feature_schema_version`, `source_model_version`;
- bounded `reasons` and canonical name-sorted `{name,value}` score components;
- `delivery_state`, `exposed`, `engaged`, `negative_label_eligible`, `first_exposed_at`, `first_play_at`, `last_progress_at`, and `last_engaged_at`;
- bounded `effective_watch_ms`, `max_position_ms`, optional `duration_ms`, optional `watch_ratio`, `completed`, `completed_at`, `skipped`, and `skipped_at`;
- independent `liked`, `favorited`, `followed`, `not_interested`, `reduce_author`, and `already_seen` facts plus their first eligible event times;
- `primary_label` and `split`.

All timestamps are UTC RFC3339Nano. Floating-point values must be finite, normalized through the schema-v1 encoder, and emitted without map-order dependence. Reasons preserve their persisted normalized order because provider contribution order can be meaningful; score components are converted from JSON objects to a sorted list.

User keys use lowercase hex `HMAC-SHA-256(key, "frux:dataset:v1:user\x00" || canonical_int64_user_id)`. Request keys use a separate `"frux:dataset:v1:request"` domain and bind user ID plus request ID so the same client string cannot collide across accounts. Split bucketing uses a third domain. The manifest stores only the pseudonymization algorithm/version and a short key identifier derived by HMAC over a constant; it never stores the key, raw user ID, raw request ID, key path, or input filename.

Raw video ID is retained for content grouping and catalog joins by controlled downstream jobs. Author/account IDs, event IDs, playback session IDs, tokens, URLs, device/context payloads, raw profile vectors, embeddings, arbitrary policy JSON, and raw errors are excluded.

Alternative considered: hashing with plain SHA-256. Rejected because low-entropy numeric account IDs are enumerable.

Alternative considered: exporting raw request IDs. Rejected because grouping works with a domain-separated pseudonym and the raw identifier is unnecessary.

### 4. Join only attribution-safe outcomes and bounded rich behavior facts

Each impression page is joined to `recommendation_outcome` by exact `(user_id, request_id, video_id)`. Eligible outcomes satisfy:

- `served_at <= recorded_at`;
- `recorded_at <= min(served_at + label_horizon, as_of)`;
- supported outcome type.

For view outcomes, the exporter joins the matching durable `recommendation_behavior_event` using its stable view outcome/source identity, then verifies the same user/request/video tuple. Rich behavior fields are used only when a validated recommendation outcome exists; raw view events cannot independently turn a delivered row into an exposed or negative example. Interaction, follow, and feedback labels come from validated recommendation outcomes, not mutable current-state projections.

State is:

1. `delivered_unexposed` when neither eligible exposure nor positive engagement exists;
2. `exposed_unengaged` when exposure exists but no later eligible playback, interaction, follow, or feedback exists;
3. `engaged_unexposed` when positive engagement exists without an exposure fact;
4. `engaged_exposed` when both exist.

Skip and negative feedback facts may be retained on an unexposed row for source-quality analysis, but `negative_label_eligible` is false and they cannot produce a negative primary label. Positive engagement may still produce a positive primary label. Independent booleans remain available, while the primary label precedence among eligible labels is:

`not_interested > reduce_author > already_seen > favorite > like > follow > complete > meaningful_watch > skip > exposed_only > unobserved`.

Within one label, earliest time is used for first-event fields, latest time for terminal/latest fields, and ties use `(recorded_at, occurred_at, outcome_type, stable source identity)`. The stable identity is used for deduplication/ties but not exported.

Alternative considered: joining directly to mutable `interaction_action`, `user_follow`, or view-history projections. Rejected because they represent current state, can lose the request linkage, and can leak behavior occurring outside the impression label horizon.

### 5. Define bounded watch calculations in dataset schema v1

Eligible playback facts are deduplicated by stable source identity. For each playback session, schema v1 takes the maximum non-negative cumulative `watch_ms`; session maxima are summed and capped at `21_600_000` ms (6 hours) per impression. Because playback session IDs are not exported, an absent session ID is grouped under one deterministic fallback bucket. Position and duration inputs are clamped to the same six-hour bound. `max_position_ms` is the maximum bounded position. `duration_ms` is the maximum eligible positive bounded duration.

`watch_ratio` is null without a valid duration; otherwise it is `min(1, effective_watch_ms / duration_ms)` and never negative or non-finite. `complete` requires a validated complete outcome. `meaningful_watch` is schema-versioned and initially means exposed plus either at least 10 seconds effective watch or ratio at least 0.5; it does not override explicit labels earlier in the precedence list.

These constants and definitions are written into the manifest so downstream consumers do not infer them from code.

Alternative considered: sum every progress event. Rejected because cumulative progress events and retries would overcount.

Alternative considered: infer completion from ratio. Rejected because duration quality varies and a validated complete fact already exists.

### 6. Offer user or time splitting with explicit leakage controls

User splitting computes `HMAC-SHA-256(key, "frux:dataset:v1:split\x00" || canonical_user_id)`, maps the first 64 bits to 10,000 buckets, and applies operator-supplied integer basis-point ranges totaling 10,000. Every row for a user therefore remains in one split.

Time splitting requires two ordered served-time cutoffs and an embargo at least equal to the label horizon on each boundary. Rows in `[cutoff-embargo, cutoff+embargo)` are excluded and counted. Remaining earlier/middle/later rows become train/validation/test. This ensures no emitted row's label observation interval crosses a later split boundary.

The strategies are mutually exclusive. Split parameters are part of the checkpoint fingerprint and manifest.

Alternative considered: random per-row splitting. Rejected because it is not reproducible and leaks users and nearby sessions across splits.

Alternative considered: time boundaries without an embargo. Rejected because labels for rows near a boundary can observe events in the next partition.

### 7. Use keyset pages and deterministic concatenated gzip members

The repository pages training impressions by `(served_at, id)` using the dependency's cleanup/export index. The query is bounded by the served window and a captured maximum impression ID, and uses a fixed page size (default 1,000; validated range 100-10,000). A page-scoped CTE/temporary `VALUES` relation drives set-based outcome and behavior joins; application code never issues per-impression queries.

Each completed page becomes one deterministic gzip member appended to `<output>.partial`. Gzip mtime, OS byte, filename/comment, compression level, JSON newline, and page size are fixed inputs. After each member is flushed and fsynced, a `0600` checkpoint is atomically replaced with:

- last `(served_at,id)` cursor and captured maximum impression ID;
- committed byte offset and cumulative counts;
- tool/dataset versions;
- source/input/split/compression/page-size fingerprint;
- pseudonymization key identifier.

Concatenated gzip members are standards-compliant and allow resume to truncate to the last committed byte offset before appending. Cancellation stops at a page boundary and preserves resumable partial state. Validation, source-version, query, serialization, I/O, fsync, and checksum failures remove partial/checkpoint/temporary manifest files. Final paths are never visible until completion.

After the final page, the exporter fsyncs the data, computes SHA-256 over the exact compressed bytes, validates count totals, renames the data atomically, writes and syncs a canonical manifest, then removes the checkpoint. Output files are created with `0600` permissions. Safe overwrite, if implemented, writes new sibling temporary files and replaces finals only after both validate.

Alternative considered: one long gzip stream. Rejected because safe cross-process resume cannot identify a fully committed compression boundary.

Alternative considered: offset pagination. Rejected because it becomes slow and can skip/duplicate rows under concurrent inserts.

### 8. Require closed-source controls for repeatability

`as_of` is part of the export identity. Only outcomes with `recorded_at <= as_of` and behavior rows visible under the corresponding bounded source cutoff participate. The command requires the impression window plus label horizon to be closed at `as_of`, and documentation requires operators to choose an `as_of` behind the monitored attribution-worker settle lag.

The first run captures the maximum eligible impression ID and source cutoff metadata in the checkpoint. Resume reuses them. Identical visible source facts and identical inputs produce identical row and gzip bytes. The manifest records `source_snapshot_started_at`, `as_of`, and the captured boundaries so consumers can distinguish separate snapshots.

A single long PostgreSQL snapshot was considered but rejected because it can hold vacuum horizons for large exports and cannot survive process restart. Closed windows, source cutoffs, stable pagination, and explicit snapshot metadata provide bounded operational behavior while making the repeatability assumptions visible.

### 9. Add export-specific composite indexes and query-plan tests

The dependency supplies `recommendation_training_impression(served_at, id)` plus request linkage. Add only the indexes required for page-scoped joins:

- `recommendation_outcome(user_id, request_id, video_id, recorded_at, outcome_type) INCLUDE (id, occurred_at)`;
- `recommendation_behavior_event(user_id, request_id, video_id, recorded_at, event_type) INCLUDE (event_id, playback_session_id, occurred_at, position_ms, watch_ms, duration_ms, completed)`.

Final column order may be adjusted from measured PostgreSQL plans, but it must preserve exact tuple lookup followed by bounded time range. Migration remains additive and uses the repository's explicit index naming/concurrent-migration conventions.

PostgreSQL integration tests load realistic skew: many unrelated users/requests, multiple videos per request, duplicate/out-of-order playback events, mixed outcome types, and several pages. `EXPLAIN (FORMAT JSON)` assertions verify index/keyset access and reject an unbounded sequential scan on the outcome/behavior fact tables. Benchmarks are informative; query-plan shape and bounded query count are normative.

Alternative considered: load all outcomes for the full time window. Rejected because memory and latency would scale with unrelated activity rather than the current impression page.

### 10. Keep manifest and documentation as the downstream contract

The canonical manifest includes:

- dataset schema and exporter tool versions;
- data filename, SHA-256, compressed bytes, and JSONL row count;
- requested/effective served window, `as_of`, label horizon, source snapshot time/boundaries;
- state, primary-label, independent-label, split, and excluded/embargo counts;
- complete label precedence, meaningful-watch threshold, watch caps, ratio formula, and timestamp semantics;
- split strategy and parameters;
- distinct scene/policy, source-model, impression-record, and feature-schema versions;
- pseudonymization algorithm/version and non-secret key identifier.

Documentation under the recommendation/module, engineering/configuration, and operator guidance explains that exported files are a new privacy artifact outside database retention. Operators must protect the HMAC key, keep files permission-restricted, define a bounded external retention/deletion policy, and avoid combining exports made with different keys unless intentionally unlinkable.

`evaluate-recommendation-policies-offline` will later validate this manifest and consume these rows. It must not bypass the export by reading production facts directly.

## Risks / Trade-offs

- [Rich joins can overload PostgreSQL] → Require closed bounded windows, keyset pages, set-based page joins, composite indexes, explain-plan integration tests, cancellation, and bounded page sizes.
- [A weak or reused key can make pseudonyms reversible or over-linkable] → Require at least 32 bytes, domain separation, restricted key-file permissions, documented rotation, and no raw key/path output.
- [Unknown source versions can silently corrupt feature meaning] → Use a dataset compatibility registry and fail before publication.
- [Outcome worker lag can make two snapshots differ] → Require closed horizons behind observed settle lag and record explicit `as_of`/snapshot boundaries.
- [Concatenated gzip adds implementation complexity] → Keep one member per fixed page, checkpoint only fsynced offsets, and cover cancel/resume/checksum behavior with integration tests.
- [Time splits discard boundary data] → Count embargo exclusions in the manifest; prefer user splits when temporal generalization is not required.
- [Raw video IDs permit content linkage] → Retain them only because grouping/evaluation needs stable content identity; exclude creator/profile/media metadata and document dataset access controls.
- [Exported files outlive database cleanup] → Make output retention an explicit operator obligation and provide deletion guidance; never change source retention automatically.

## Migration Plan

1. Complete and deploy `persist-recommendation-training-impressions`, including its fact table, versions, retention, and required base indexes.
2. Add the export domain/application interfaces, dataset-v1 registry/encoder, PostgreSQL read repository, composite indexes, and standalone command.
3. Run migration/index integration tests and inspect representative PostgreSQL plans before enabling operators to export production-sized windows.
4. Validate deterministic fixtures, privacy exclusions, unsupported-version failures, cancellation/resume, output cleanup, checksum/manifest, and split leakage controls.
5. Document key custody and external output retention; initially run a small closed window and verify manifest counts against read-only SQL summaries.
6. Rollback by removing access to the operator binary. Additive indexes may remain; no source rows or retention settings require rollback, and incomplete local files can be deleted safely.

## Open Questions

None. Implementation must use the final version constants and policy/model metadata contract from `persist-recommendation-training-impressions`; if that dependency cannot provide stable model resolution, revise the dependency before implementation rather than weakening export version checks.
