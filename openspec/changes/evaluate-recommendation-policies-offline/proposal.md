## Why

Frux needs a repeatable way to compare existing linear recommendation policies before any learned-weight or online experiment work, using only privacy-bounded exported facts. The planned `persist-recommendation-training-impressions` and `export-recommendation-training-dataset` changes establish the source and file contract; this change turns that contract into deterministic observational evaluation without claiming causal lift.

## What Changes

- Add a Go operator CLI that validates a versioned export manifest and gzip JSONL checksum/schema before reading rows.
- Load one baseline and one or more candidate `PolicyConfiguration` JSON files through the same production feature-name and bound validation, then recompute linear scores from logged score components.
- Replay production score ordering and diversity within each request/session over the available exported candidate set, while explicitly classifying served-subset replay as incomplete for full-pool conclusions.
- Define and version one bounded composite relevance/utility label using watch ratio/effective watch, completion, like, favorite, follow, quick skip, and explicit feedback.
- Produce deterministic observational NDCG@K, watch/interaction rates, coverage/concentration, recall-source, policy/degraded/schema, position-stratified, and sample/join-quality metrics.
- Produce canonical JSON and concise Markdown reports with dataset/policy hashes, definitions, counts, exclusions, warnings, replay limitations, and deterministic bootstrap confidence intervals only where valid.
- Add fixture/golden, corrupted input/config, unsupported-version, repeatability, replay, metric, bootstrap, and documentation coverage.
- Add the minimum evaluator-facing dataset metadata needed for production-equivalent tie-breaking, author grouping, and degraded slices; keep raw author identity excluded.
- Explicitly exclude training weights, activating policies, online serving changes, A/B dashboards, propensity-free IPS, causal-lift claims, embeddings, pgvector, and model inference.
- Establish this evaluator as a later dependency of `learn-recommendation-policy-weights`.

## Capabilities

### New Capabilities

- `recommendation-offline-evaluation`: Deterministic validation, replay, observational metrics, uncertainty reporting, and machine/human-readable reports for existing linear recommendation policies over versioned exports.

### Modified Capabilities

- `recommendation-training-dataset`: Add only the privacy-bounded replay metadata required for exact production tie-breaking and author/degraded slices in supported dataset versions.

## Impact

- Depends on the planned `persist-recommendation-training-impressions` change and then `export-recommendation-training-dataset`; it never queries production facts directly.
- Adds a standalone Go command and offline domain/application packages under `apps/api`, plus fixtures, golden reports, and operator documentation.
- Extends the planned dataset contract with candidate publication time, a domain-separated pseudonymous author grouping key, and bounded degraded/unknown metadata; no raw author ID, public HTTP, frontend, database mutation, or online policy behavior changes.
- `learn-recommendation-policy-weights` may consume the evaluator's validated metrics and report contract later, but training remains out of scope here.
