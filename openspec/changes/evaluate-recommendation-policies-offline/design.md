## Context

The active planned change `persist-recommendation-training-impressions` creates durable facts for final delivered recommendation cards, including request/video identity, absolute position, policy/schema versions, recall reasons, and normalized score components. The active planned change `export-recommendation-training-dataset` converts those facts and linked outcomes into a privacy-bounded, checksummed, deterministic gzip JSONL dataset and manifest. This change is sequenced after both and consumes only their files.

Production ranking is a linear sum over eight supported bounded components, followed by descending score, descending publication time, descending video ID, and deterministic author/content diversity. The export currently plans served impressions rather than every recalled candidate, so an evaluator can replay only the observed candidate subset and must not describe it as complete full-pool counterfactual evaluation.

The export needs three additional privacy-bounded fields before its dataset contract is finalized: candidate `published_at` for the production tie-break, a domain-separated pseudonymous `author_key` for author diversity/coverage, and bounded degraded/unknown metadata for degraded slices. Raw author identity remains excluded.

No logged randomized assignment propensity exists. The same logged outcomes therefore cannot establish causal or unbiased counterfactual lift for a reordered candidate list.

## Goals / Non-Goals

**Goals:**

- Validate the manifest, compressed-file checksum/size, row count, schema versions, and bounded row contract before evaluation.
- Validate baseline and candidate `PolicyConfiguration` files through the same production domain rules.
- Recompute linear scores from logged components and replay production ordering/diversity over each available request candidate set.
- Define one explicit versioned bounded observational relevance/utility label.
- Compute deterministic ranking, watch, interaction, coverage, concentration, recall-source, slice, and data-quality metrics.
- Emit deterministic canonical JSON and concise Markdown reports with paired uncertainty where statistically defensible.
- Make replay limitations, exclusions, missing-label coverage, and non-causal interpretation impossible to overlook.
- Use the existing Go toolchain and standard library.

**Non-Goals:**

- Training, optimizing, or recommending policy weights.
- Creating or activating recommendation policies, changing rollout, or altering online scoring/serving.
- Claiming causal lift, unbiased counterfactual performance, or applying IPS without valid future propensity fields.
- Reconstructing recall pools, recomputing feature values, querying production databases, or joining external catalogs.
- Adding dashboards, schedulers, public APIs, frontend workflows, embeddings, pgvector, or model inference.

## Decisions

### 1. Add a standalone Go evaluator with no runtime service dependencies

Add `apps/api/cmd/recommendation-policy-evaluate`. It accepts:

- `--manifest` and `--dataset`;
- one `--baseline <name>=<path>` and one or more repeatable `--candidate <name>=<path>`;
- `--output-json` and `--output-markdown`;
- a validated sorted `--k` list, defaulting to `1,5,10,20`, with values from 1 through 100;
- `--bootstrap-replicates`, default 2,000 and bounded from 100 through 10,000;
- `--max-rows`, default 250,000 and bounded through 1,000,000;
- explicit safe overwrite behavior.

The command uses only standard-library JSON, gzip, hashing, statistics, and filesystem facilities plus Frux domain packages. It does not load application config or connect to PostgreSQL, Redis, RabbitMQ, HTTP, or model services. Manifest row count is checked against `--max-rows` before decompression; the implementation may hold the bounded dataset grouped by request in memory and fails rather than silently sampling an oversized input.

Alternative considered: Python/pandas. Rejected because the metrics do not require an external numerical stack, adding a second dependency/runtime would weaken reproducibility, and shared Go validation/replay logic is central to compatibility.

### 2. Fail closed on dataset integrity and compatibility

Preflight parses the canonical manifest, requires the explicitly registered dataset schema and label/source versions, compares the dataset basename, byte size, SHA-256 over compressed bytes, and declared row count, then streams every gzip member and JSONL row. It rejects checksum/size mismatch, malformed or oversized lines, truncated gzip, trailing invalid data, duplicate `(user_key, request_key, video_id)` rows, invalid canonical timestamps/numbers, unknown fields or enum values, unsupported source versions, unsorted input, and manifest/count reconciliation failures.

The evaluator registry initially supports only the final schema produced by `export-recommendation-training-dataset` after its evaluator metadata is added. Compatibility is explicit rather than best-effort. The report records manifest hash, compressed dataset hash, dataset/tool/schema versions, and all source version sets.

Alternative considered: skip bad rows and report exclusions. Rejected for structural/schema/integrity failures because partial acceptance can silently change policy comparisons. Semantically valid but unlabeled rows remain in replay and are reported through coverage metrics.

### 3. Finalize the export profile with minimal replay metadata

The planned dataset row adds:

- `published_at`: trusted candidate publication time in canonical UTC;
- `author_key`: lowercase hex HMAC-SHA-256 using the export key and a new `"frux:dataset:v1:author"` domain over canonical author ID;
- `degraded_state`: `healthy`, `degraded`, or `unknown`;
- `degraded_providers`: a bounded, sorted list of normalized provider identifiers when available.

The exporter obtains publication time and author ID through the same bounded page-scoped video metadata join used to assemble export rows. It emits degraded state only from a trusted durable source available to the export, such as a matching retained request record; otherwise it emits `unknown` and no provider list. The evaluator reports known-state coverage and never treats unknown as healthy or infers state from missing providers.

`author_key` is grouping-only and cannot be reversed without the operator key. The raw author ID, author profile, publication metadata other than time, and key material remain excluded. The manifest enumerates these fields and pseudonymization version.

Alternative considered: use video ID as a proxy for author. Rejected because it cannot reproduce `MaxPerAuthor` or author concentration. Alternative considered: query the live catalog during evaluation. Rejected because reports would no longer be self-contained or repeatable.

### 4. Share production policy validation without changing online behavior

Factor the existing recommendation domain normalization into an exported validation entry point returning a normalized clone of `PolicyConfiguration`; `NewPolicy` continues to call the same function. The evaluator decodes each file strictly, rejects trailing JSON and unknown fields, supplies a bounded report name separately from the raw configuration, and validates all supported feature names, finite weight bounds, total absolute-weight bound, recall/deadline maps, half-lives, diversity, rollout, retention, suppression, and other existing production constraints.

Reports include both the exact input-file SHA-256 and a canonical normalized-configuration SHA-256. Duplicate names or duplicate normalized hashes are rejected to avoid ambiguous comparisons.

Only `FeatureWeights` and `Diversity` can be replayed from logged components and candidate metadata. Differences in recall budgets, provider deadlines, feature-generation half-lives, exposure/suppression, fallback pool, rollout, sampling, retention, or snapshot settings are listed as non-replayable policy differences. They do not alter the offline candidate set or logged components.

Alternative considered: validate only weights/diversity. Rejected because the input is a production `PolicyConfiguration`, and accepting a file production would reject creates a misleading promotion path.

### 5. Use a versioned deterministic score and production ordering contract

For each candidate, evaluation version `linear-replay/v1`:

1. Requires every registered score component exactly once, with finite values in `[0,1]`.
2. Multiplies normalized component values by normalized policy weights.
3. Sums in the registry's fixed feature order to avoid Go map iteration nondeterminism.
4. Rejects a non-finite result.
5. Sorts by score descending, `published_at` descending, then `video_id` descending.
6. Applies the production diversity algorithm: author cap, author gap, content-bucket gap, gap-relaxed retry, then stable remainder fallback when caps are infeasible.

The content bucket is the lexicographically smallest recall provider, matching production. Baseline replay is compared with logged absolute positions before metric computation. Mismatches are counted and warned because source suppression, absent full-pool candidates, historical arithmetic, or metadata defects can prevent equality.

The report classifies every request:

- `served_subset_replay`: valid replay over exported delivered cards, with full-pool replay unavailable;
- `incomplete_metadata`: excluded because required candidate metadata is missing;
- `invalid_group`: rejected input when identity/position invariants are contradictory.

Because the prerequisite source persists delivered cards, `full_pool_replay_available` is false for dataset v1. Absolute-position gaps, multiple delivered pages, candidate counts, and baseline-order agreement are reported. No candidate-policy result is called a full-policy or full-recall replay.

Alternative considered: use logged absolute position as the candidate tie-break. Rejected because candidate weights can create new ties and production uses publication time/video ID.

### 6. Define `observational-utility/v1`

Each row derives:

- `watch_ratio_term = clamp(watch_ratio, 0, 1)` when present, otherwise `0`;
- `effective_watch_term = clamp(effective_watch_ms / 30000, 0, 1)`;
- `quick_skip = exposed && skipped && effective_watch_ms <= 3000 && (watch_ratio is null || watch_ratio <= 0.10)`.

The composite relevance/utility label is:

```text
clamp(
  0.35 * watch_ratio_term
  + 0.15 * effective_watch_term
  + 0.15 * completed
  + 0.10 * liked
  + 0.15 * favorited
  + 0.10 * followed
  - 0.20 * quick_skip
  - 0.35 * eligible_not_interested
  - 0.25 * eligible_reduce_author
  - 0.15 * eligible_already_seen,
  0, 1)
```

Explicit negative terms apply only when the exported row says negative labels are eligible. `delivered_unexposed` rows without positive engagement are unlabeled, not zero-relevance negatives. The report includes the formula, constants, quick-skip definition, eligibility rule, and label version.

NDCG@K is computed only for request-policy evaluations whose replayed top K (or all candidates when fewer than K) all have eligible labels. IDCG uses the same observed candidate set and graded label with gain `2^relevance - 1` and logarithmic discount. Requests with no positive gain produce NDCG `0` and remain counted. Missing-label exclusions and complete-label coverage are reported per K.

Alternative considered: reuse the export's categorical primary-label precedence as a numeric relevance score. Rejected because it does not explicitly combine watch depth and simultaneous independent signals as required for policy ranking evaluation.

### 7. Compute explicitly observational metrics and slices

For baseline and every candidate, at each K:

- complete-label NDCG@K;
- average composite utility;
- average effective watch milliseconds and watch ratio with separate known denominators;
- completion, quick-skip, not-interested, reduce-author, already-seen, like, favorite, follow, and combined negative-feedback rates;
- distinct content/video and author coverage against the observed candidate-set universe;
- content and author HHI concentration and largest-author share;
- fractional recall-source mix, assigning each selected item `1/n` to each of its `n` reasons, plus multi-source item rate.

Metrics are also emitted for bounded slices by source policy version, degraded/healthy state, dataset schema, source record schema, feature schema, source model, and logged absolute-position bands `0`, `1-4`, `5-9`, and `10+`. Small slices are retained with counts rather than hidden.

Data-quality sections include manifest/parsed counts, request/user/item counts, duplicate and invalid counts, label-eligible coverage, known-duration/watch-ratio coverage, exposure/engagement states, absolute-position gaps, baseline-order agreement, source-version distribution, request-size distribution, and exclusions by reason.

Candidate-minus-baseline deltas are paired on identical request groups. Every metric is labeled `observational`; reports state that reordering logged served subsets changes which observed labels appear at K but does not estimate outcomes for items at unobserved positions.

### 8. Use deterministic clustered bootstrap only where valid

For additive request/item means, rates, NDCG, and paired candidate-minus-baseline deltas, the evaluator uses a user-cluster bootstrap so requests from one pseudonymous user remain together. The PRNG seed is derived from manifest hash, normalized policy hashes, label version, metric key, slice key, and replicate count; results are identical across runs.

The report emits percentile 95% intervals only when at least 30 distinct eligible user clusters and non-degenerate finite samples exist. It reports a machine-readable unavailable reason otherwise. Global unique coverage, HHI/concentration, raw counts, data-quality diagnostics, and slices without adequate clusters do not receive misleading confidence intervals.

These intervals describe sampling variability of the observed export only. They are not causal intervals and do not correct position bias. Results are position-stratified, and no IPS/SNIPS/doubly-robust estimator is implemented. A future dataset with validated randomized propensities requires a new schema and capability change before such estimators can be added.

### 9. Produce canonical atomic JSON and Markdown reports

The JSON report uses structs and sorted arrays rather than maps where ordering matters, finite normalized numbers, fixed field order, no wall-clock generation timestamp, and one trailing newline. It contains input hashes, normalized policies, replay scope, warnings, metric definitions, label definition, sample/exclusion counts, slices, estimates, confidence intervals, and limitations.

The Markdown report is rendered solely from the JSON report model and contains a short dataset/policy summary, prominent observational/full-pool warnings, top-line baseline/candidate tables, sample coverage, exclusions, and metric definitions. It is concise and deterministic.

Both outputs are written to permission-restricted sibling partial files, synced, and atomically renamed only after both render successfully. Existing files remain untouched unless safe overwrite is explicit. Failures remove partial files without modifying the input dataset or manifest.

### 10. Make downstream training dependency one-way

Documentation states that future `learn-recommendation-policy-weights` work may invoke or consume this evaluator's versioned report to compare learned candidates against a baseline. That later change must not move training, optimization, activation, or model inference into this evaluator.

## Risks / Trade-offs

- [Served impressions omit unserved recall candidates and outcomes under alternative ranks] → Mark full-pool replay unavailable, report absolute-position gaps and label coverage, position-stratify metrics, and prohibit causal/counterfactual claims.
- [Candidate policies can change non-replayable recall or feature-generation settings] → Validate the full configuration but enumerate non-replayable differences and limit claims to logged-component score/diversity replay.
- [Degraded state may be unavailable for unsampled or expired request records] → Preserve an explicit `unknown` slice, report known-state coverage, and never infer healthy state from absence.
- [Unobserved delivered cards can be mistaken for negatives] → Treat unexposed/unengaged rows as missing labels and restrict NDCG to complete-label top-K groups.
- [Large exports can exceed evaluator memory] → Preflight manifest row count against a bounded operator limit and fail without partial metrics.
- [Bootstrap intervals can imply stronger evidence than exists] → Cluster by user, require minimum samples, omit invalid intervals, and repeat observational/non-causal warnings.
- [Golden reports can be brittle across intentional schema changes] → Version label, replay, metric, and report schemas and require explicit golden updates.

## Migration Plan

1. Finalize and implement `persist-recommendation-training-impressions` with the source contracts required by the planned exporter.
2. Finalize and implement `export-recommendation-training-dataset` with `published_at`, pseudonymous `author_key`, degraded/unknown fields, manifest enumeration, and deterministic fixtures.
3. Add the shared policy-configuration validation entry point without changing online policy semantics.
4. Implement evaluator domain types, integrity reader, replay, labels, metrics, bootstrap, reporting, and CLI.
5. Validate golden fixtures against small exports before evaluating operator datasets.
6. Roll back by removing access to the standalone evaluator binary and deleting its local reports; no database, policy, export, or serving state is changed.

## Open Questions

None. Implementation must block if the prerequisite changes cannot provide publication/author replay metadata; degraded state may remain explicitly unknown but must never be guessed.
