## Context

`persist-recommendation-training-impressions` defines a trusted compact diagnostic contract for actual deliveries, including request/generation/video identity, absolute generation position, policy/schema versions, author/publication metadata, reasons/components, degraded metadata, and served/recorded times. The broad training exporter is future-only and is not a prerequisite here.

Production ranking is a linear sum over eight supported bounded components, followed by descending score, descending publication time, descending video ID, and deterministic author/content diversity. Low-data evaluation uses small frozen replay bundles containing candidate components and expected production order, a privacy-reviewed human golden set, and optional observational aggregates. Observed delivered subsets remain incomplete for full-pool or counterfactual conclusions.

Replay and golden bundles need candidate `published_at`, pseudonymous `author_key`, bounded topic/source identifiers, and degraded/unknown metadata. Raw user or author identity remains excluded. These fields may later be supported by a training export, but this evaluator owns no exporter dependency.

No logged randomized assignment propensity exists. The same logged outcomes therefore cannot establish causal or unbiased counterfactual lift for a reordered candidate list.

## Goals / Non-Goals

**Goals:**

- Validate replay-bundle and golden-set hashes, counts, schemas, annotation provenance, and bounded fields before evaluation.
- Validate baseline and candidate `PolicyConfiguration` files through the same production domain rules.
- Recompute linear scores from frozen components and replay production ordering/diversity over each candidate set.
- Require baseline-order parity before candidate interpretation.
- Compute human semantic relevance, recall coverage, author/topic diversity, and optional observational watch/feedback metrics.
- Emit deterministic canonical JSON and concise Markdown reports with uncertainty only when the available sample supports it.
- Make replay limitations, exclusions, missing-label coverage, and non-causal interpretation impossible to overlook.
- Run with small samples; large user cohorts or clustered bootstrap are never prerequisites.
- Use the existing Go toolchain and standard library.

**Non-Goals:**

- Training, optimizing, or recommending policy weights.
- Creating or activating recommendation policies, changing rollout, or altering online scoring/serving.
- Claiming causal lift, unbiased counterfactual performance, or applying IPS without valid future propensity fields.
- Reconstructing recall pools, recomputing feature values, querying production databases, or joining external catalogs.
- Adding dashboards, schedulers, public APIs, frontend workflows, embeddings, pgvector, or model inference.

## Decisions

### 1. Add a standalone low-data Go evaluator with no runtime service dependencies

Add `apps/api/cmd/recommendation-policy-evaluate`. It accepts:

- one or more `--replay-bundle`;
- one `--golden-set`;
- optional `--observations` conforming to the diagnostic identity/time contract;
- one `--baseline <name>=<path>` and one or more repeatable `--candidate <name>=<path>`;
- `--output-json` and `--output-markdown`;
- a validated sorted `--k` list, defaulting to `1,5,10,20`, with values from 1 through 100;
- optional bounded uncertainty controls, disabled by default;
- small explicit case/row bounds;
- explicit safe overwrite behavior.

The command uses only standard-library JSON, hashing, statistics, and filesystem facilities plus Frux domain packages. It does not load application config or connect to PostgreSQL, Redis, Kafka, HTTP, or model services. It may hold the bounded evaluation cases in memory and fails rather than silently sampling an oversized input.

Alternative considered: Python/pandas. Rejected because the metrics do not require an external numerical stack, adding a second dependency/runtime would weaken reproducibility, and shared Go validation/replay logic is central to compatibility.

### 2. Fail closed on replay, golden-set, and optional observation integrity

Preflight strictly parses canonical replay and golden manifests, verifies file hashes/counts, annotation schema/rubric versions, generation identity, canonical timestamps/numbers, unique case/candidate IDs, sorted inputs, and privacy exclusions. Optional observations additionally validate `(user_key, request_key, generation, video_id)` and occurred/recorded time semantics. Structural failures abort without partial reports.

Compatibility is explicit rather than best-effort. The report records every input hash and version. No input is required to have been produced by `export-recommendation-training-dataset`.

Alternative considered: skip bad rows and report exclusions. Rejected for structural/schema/integrity failures because partial acceptance can silently change policy comparisons. Semantically valid but unlabeled rows remain in replay and are reported through coverage metrics.

### 3. Define a small self-contained replay and golden-set profile

Each replay candidate contains:

- `published_at`: trusted candidate publication time in canonical UTC;
- a pseudonymous `author_key`;
- one bounded normalized `topic_key` and bounded recall-source list;
- `degraded_state`: `healthy`, `degraded`, or `unknown`;
- `degraded_providers`: a bounded, sorted list of normalized provider identifiers when available.

Replay bundles freeze candidate features, expected baseline order, publication time, grouping keys, sources/topics, and degraded state from a privacy-reviewed fixture or diagnostic capture. Golden cases add a blinded context summary, candidate presentation payload, independent 0-3 semantic relevance judgments, annotator count, adjudicated label, and rubric version. The evaluator reports unknown metadata and never guesses.

Grouping keys are opaque and local to the bundle. Raw user/author identity, profiles, free-form private context, and key material remain excluded.

Alternative considered: use video ID as a proxy for author. Rejected because it cannot reproduce `MaxPerAuthor` or author concentration. Alternative considered: query the live catalog during evaluation. Rejected because reports would no longer be self-contained or repeatable.

### 4. Share production policy validation and reject non-replayable policies by default

Factor the existing recommendation domain normalization into an exported validation entry point returning a normalized clone of `PolicyConfiguration`; `NewPolicy` continues to call the same function. The evaluator decodes each file strictly, rejects trailing JSON and unknown fields, supplies a bounded report name separately from the raw configuration, and validates all supported feature names, finite weight bounds, total absolute-weight bound, recall/deadline maps, half-lives, diversity, rollout, retention, suppression, and other existing production constraints.

Reports include both the exact input-file SHA-256 and a canonical normalized-configuration SHA-256. Duplicate names or duplicate normalized hashes are rejected to avoid ambiguous comparisons.

Only `FeatureWeights` and `Diversity` can be replayed from frozen components and candidate metadata. Differences in recall budgets, provider deadlines, feature generation, exposure/suppression, fallback, rollout, sampling, retention, or snapshot settings are non-replayable.

The default command fails before metrics when any candidate has a non-replayable difference. An explicit diagnostic-only override may render the difference inventory, but it omits comparative policy metrics and cannot declare a winner or promotion recommendation.

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

The report classifies every replay case:

- `served_subset_replay`: valid replay over a diagnostic delivered-card subset, with full-pool replay unavailable;
- `incomplete_metadata`: excluded because required candidate metadata is missing;
- `invalid_group`: rejected input when identity/position invariants are contradictory.

Captured full-pool fixtures may declare full-pool replay only when their manifest proves a complete frozen pool. Diagnostic delivered subsets set `full_pool_replay_available=false`. Absolute-position gaps, generation counts, candidate counts, and baseline-order agreement are reported. Canonical production fixtures require 100% exact baseline order; any mismatch invalidates the evaluator build or policy comparison.

Alternative considered: use logged absolute position as the candidate tie-break. Rejected because candidate weights can create new ties and production uses publication time/video ID.

### 6. Make blinded human semantic relevance the primary low-data label

The golden rubric scores each candidate from 0 to 3:

- `0`: irrelevant or clearly undesirable for the stated context;
- `1`: weakly related but unlikely to satisfy;
- `2`: relevant and plausibly useful;
- `3`: highly relevant and strongly matched.

Candidates are presented without policy name or rank. Each case requires at least two independent judgments; disagreements of two or more points require adjudication. Reports include raw judge counts, adjudicated labels, agreement rate, and weighted agreement statistics when defined. Semantic NDCG@K, precision/recall at preregistered relevance thresholds, and pairwise preference accuracy use only adjudicated labels.

### 7. Retain `observational-utility/v1` only for optional observed samples

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

Explicit negative terms apply only when an optional observation says negative labels are eligible. `delivered_unexposed` rows without positive engagement are unlabeled, not zero-relevance negatives. The report includes the formula, constants, quick-skip definition, eligibility rule, and label version.

NDCG@K is computed only for request-policy evaluations whose replayed top K (or all candidates when fewer than K) all have eligible labels. IDCG uses the same observed candidate set and graded label with gain `2^relevance - 1` and logarithmic discount. Requests with no positive gain produce NDCG `0` and remain counted. Missing-label exclusions and complete-label coverage are reported per K.

Alternative considered: reuse an observational bundle's categorical primary-label precedence as the primary relevance score. Rejected because low-data evaluation should be anchored in blinded human semantic judgments, while behavior remains optional diagnosis.

### 8. Compute replay, semantic, recall, diversity, and conditional observational metrics

For baseline and every candidate, at each K:

- baseline-order parity and deterministic score/order diagnostics;
- semantic NDCG@K, precision/recall, and pairwise preference accuracy over the golden set;
- recall coverage of adjudicated relevant items overall and by recall source;
- distinct author and topic coverage, author/topic concentration, largest-group share, and repeated-author/topic runs;
- optional observational utility and watch metrics only for eligible observed rows;
- average composite utility;
- average effective watch milliseconds and watch ratio with separate known denominators;
- completion, quick-skip, not-interested, reduce-author, already-seen, like, favorite, follow, and combined negative-feedback rates only when the corresponding eligible denominator is greater than zero.

Metrics are also emitted for bounded available slices by source policy version, degraded/healthy state, input/record/feature/source-model schema, and logged absolute-position bands `0`, `1-4`, `5-9`, and `10+`. Small slices are retained with counts rather than hidden.

Data-quality sections include manifest/parsed counts, request/user/item counts, duplicate and invalid counts, label-eligible coverage, known-duration/watch-ratio coverage, exposure/engagement states, absolute-position gaps, baseline-order agreement, source-version distribution, request-size distribution, and exclusions by reason.

Candidate-minus-baseline semantic deltas are paired on identical golden cases. Optional behavior deltas are labeled `observational`; reports state that they do not estimate causal lift or outcomes at unobserved positions. A zero denominator yields `unavailable`, never numeric zero.

### 9. Make uncertainty optional and sample-appropriate

Point estimates, denominators, and case counts are always available when inputs exist. The evaluator does not require a minimum user population or bootstrap to run.

When preregistered and statistically defensible, deterministic case-level bootstrap or exact/binomial intervals may be emitted with explicit independence assumptions and minimum case counts. User-cluster bootstrap is allowed only for optional observations with sufficient independent users; it is never an acceptance prerequisite. Otherwise the report gives an unavailable reason.

These intervals describe only the sampled golden cases or optional observations. They are not causal intervals and do not correct position bias. No IPS/SNIPS/doubly-robust estimator is implemented. A future input with validated randomized propensities requires a new schema and capability change before such estimators can be added.

### 10. Produce canonical atomic JSON and Markdown reports

The JSON report uses structs and sorted arrays rather than maps where ordering matters, finite normalized numbers, fixed field order, no wall-clock generation timestamp, and one trailing newline. It contains input hashes, normalized policies, replay scope, warnings, metric definitions, label definition, sample/exclusion counts, slices, estimates, confidence intervals, and limitations.

The Markdown report is rendered solely from the JSON report model and contains a short input/policy summary, prominent replay/observational/full-pool warnings, top-line baseline/candidate tables, sample coverage, annotation agreement, exclusions, and metric definitions. It is concise and deterministic.

Both outputs are written to permission-restricted sibling partial files, synced, and atomically renamed only after both render successfully. Existing files remain untouched unless safe overwrite is explicit. Failures remove partial files without modifying any input bundle or manifest.

### 11. Keep training independent and deferred

Documentation states that `learn-recommendation-policy-weights` is conditionally deferred and not a prerequisite for semantic evaluation. A future activated learner may consume a versioned report, but cannot move training, optimization, activation, or model inference into this evaluator.

## Risks / Trade-offs

- [Served impressions omit unserved recall candidates and outcomes under alternative ranks] → Mark full-pool replay unavailable, report absolute-position gaps and label coverage, position-stratify metrics, and prohibit causal/counterfactual claims.
- [Candidate policies can change non-replayable recall or feature-generation settings] → Validate the full configuration but enumerate non-replayable differences and limit claims to logged-component score/diversity replay.
- [Degraded state may be unavailable for unsampled or expired request records] → Preserve an explicit `unknown` slice, report known-state coverage, and never infer healthy state from absence.
- [Unobserved delivered cards can be mistaken for negatives] → Treat unexposed/unengaged rows as missing labels and never report quick-skip or negative rates without eligible samples.
- [Oversized replay or observation bundles can exceed evaluator memory] → Preflight manifest counts against bounded operator limits and fail without partial metrics.
- [Small golden sets can imply stronger evidence than exists] → Report counts, agreement, missing coverage, and optional sample-appropriate intervals without making causal claims.
- [Golden reports can be brittle across intentional schema changes] → Version label, replay, metric, and report schemas and require explicit golden updates.

## Migration Plan

1. Finalize the compact diagnostic identity/time contract and create privacy-reviewed replay fixtures; do not wait for the future training exporter.
2. Define the human rubric, annotation instructions, blinded workflow, adjudication, and a small representative golden set.
3. Add shared policy validation without changing online policy semantics.
4. Implement evaluator input validation, production replay/parity, semantic/recall/diversity metrics, optional observational metrics, reporting, and CLI.
5. Validate canonical baseline parity and golden reports before evaluating operator bundles.
6. Roll back by removing access to the standalone evaluator binary and deleting local reports; no database, policy, export, or serving state is changed.

## Open Questions

The first golden-set rubric and minimum case coverage must be preregistered before annotation. Missing publication/author/topic replay metadata remains a hard input failure; degraded state may remain explicitly unknown but must never be guessed.
