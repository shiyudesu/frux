## Why

Frux can persist delivered recommendation evidence, export versioned observational datasets, and compare existing linear policies offline, but it has no reproducible way to learn improved production feature weights from those artifacts. This change adds a fail-closed offline learner while preserving the current linear scorer and keeping candidate activation an explicit separate operator decision.

## What Changes

- Add a deterministic offline tool that depends on `persist-recommendation-training-impressions`, `export-recommendation-training-dataset`, and `evaluate-recommendation-policies-offline`.
- Train only the eight existing bounded score components: content similarity, session similarity, hotness, freshness, author affinity, follow relation, negative penalty, and exposure penalty.
- Accept only supported versioned export manifests/datasets and the evaluator's versioned composite observational label; use exposure-eligible examples and never treat delivered-but-unexposed items as negatives.
- Use deterministic request-local pairwise linear ranking optimization with temporal and pseudonymous-user-safe splits, bounded pair/class sampling, regularization, a fixed seed, and explicit sparse/constant-feature handling.
- Project learned weights onto production constraints: positive components remain nonnegative, penalty components remain nonpositive, and existing per-weight and total absolute-weight bounds remain valid.
- Start from a fully validated baseline `PolicyConfiguration` and emit a complete disabled candidate JSON that changes only `feature_weights`, plus deterministic training metadata and an offline evaluator comparison.
- Refuse candidate publication when integrity, coverage, leakage, convergence, weight, or evaluator regression gates fail.
- Add synthetic/golden recovery, determinism, sign/bound, leakage, failure-gate, and documentation coverage.
- Keep recall budgets, diversity, suppression, retention, rollout, deadlines, activation, online inference, databases, experiments, embeddings, vector stores, neural models, and model serving out of scope.

## Capabilities

### New Capabilities

- `recommendation-policy-weight-learning`: Deterministic, exposure-safe offline optimization of the existing linear recommendation feature weights, constrained candidate generation, evaluator-gated reporting, and fail-closed publication.

### Modified Capabilities

None. The learner consumes the already planned versioned dataset, label, policy-validation, and evaluator report contracts without extending them.

## Impact

- Sequenced after `persist-recommendation-training-impressions`, `export-recommendation-training-dataset`, and `evaluate-recommendation-policies-offline`; it reads only their local versioned artifacts.
- Adds a small standalone Go operator command and offline learning packages under `apps/api`, with no production service or runtime-image dependency.
- Reuses production policy normalization/bounds and evaluator label/report logic, but writes no database or policy state and does not activate the emitted candidate.
- Adds local candidate/report artifacts, fixtures, golden files, focused tests, and recommendation/operator documentation; no public HTTP, frontend, persistence, or main-spec behavior changes.
