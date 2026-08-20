## Why

Frux needs a trustworthy way to diagnose and compare recommendation policies before enough user data exists for training or causal inference. The current route therefore centers on deterministic production-scorer replay, a small privacy-reviewed human golden set, and optional observational metrics rather than a large training export.

## What Changes

- Add a Go operator CLI that validates small versioned replay bundles and blinded human golden sets; optional observational inputs use the shared diagnostic identity/time contract but do not require `export-recommendation-training-dataset`.
- Load one baseline and one or more candidate `PolicyConfiguration` files through production validation, then deterministically replay the production scorer, tie-breaking, and diversity logic.
- Require exact baseline-order parity on canonical replay fixtures and report parity on diagnostic cases before interpreting candidate results.
- Reject policies with recall, feature-generation, suppression, rollout, or other non-replayable differences by default; an explicit diagnostic-only mode may list those differences but cannot rank or recommend the policy.
- Make human semantic relevance the primary low-data evidence through a versioned 0-3 rubric, blinded annotations, adjudication, and agreement reporting.
- Report recall coverage and source contribution plus author and topic diversity over the frozen candidate pools.
- Report quick-skip and explicit-negative feedback only when an eligible observed sample exists; otherwise emit `unavailable` with zero denominator rather than a zero rate.
- Produce deterministic JSON and concise Markdown with hashes, counts, exclusions, parity, golden-set metrics, optional observational metrics, and prominent non-causal limitations.
- Keep uncertainty optional and sample-appropriate; no large user-cluster bootstrap is required to run or accept the evaluator.
- Explicitly exclude training weights, activating policies, online serving changes, A/B dashboards, propensity-free IPS, causal-lift claims, embeddings, pgvector, and model inference.
- Keep this evaluator independent of the deferred `learn-recommendation-policy-weights` change and usable for the semantic roadmap without it.

## Capabilities

### New Capabilities

- `recommendation-offline-evaluation`: Low-data deterministic production replay, blinded human golden-set scoring, optional observational diagnostics, and machine/human-readable policy reports.

### Modified Capabilities

- `recommendation-training-dataset`: Define optional future interoperability with the evaluator's identity/time and replay metadata; the dataset exporter is not an evaluator prerequisite.

## Impact

- May consume small privacy-reviewed diagnostic bundles aligned with `persist-recommendation-training-impressions`, but does not depend on the future-only dataset exporter and never queries production facts directly.
- Adds a standalone Go command and offline domain/application packages under `apps/api`, plus fixtures, golden reports, and operator documentation.
- Uses frozen publication time, pseudonymous author grouping, bounded topic/source/degraded metadata, and human annotations; no raw author ID, public HTTP, frontend, database mutation, or online behavior changes.
- Weight learning is conditionally deferred and is neither required by nor automatically enabled through this evaluator.
