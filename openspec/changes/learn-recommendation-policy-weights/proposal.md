## Why

Frux does not yet have enough validated, privacy-approved, statistically powered evidence to justify learning production policy weights. This change is therefore removed from the active roadmap and deferred indefinitely unless a future preregistered activation review proves adequate sample size, label coverage, power, stability, privacy, and resources.

## What Changes

- Keep all learner implementation inactive until a dated activation record contains the exact learning hypothesis, preregistered sample-size calculation, label/exposure coverage thresholds, power and stability criteria, privacy/security approval, and resource budget.
- State explicitly that deterministic scorer replay, human semantic evaluation, recall coverage, and author/topic diversity work proceed without this change.
- If activated later, add a deterministic offline tool that consumes only compatible, separately approved artifacts from the impression, export, and evaluation changes.
- Train only the eight existing bounded score components: content similarity, session similarity, hotness, freshness, author affinity, follow relation, negative penalty, and exposure penalty.
- Accept only supported versioned export manifests/datasets and the evaluator's versioned composite observational label; use exposure-eligible examples and never treat delivered-but-unexposed items as negatives.
- Use deterministic request-local pairwise linear ranking optimization with temporal and pseudonymous-user-safe splits, bounded pair/class sampling, regularization, a fixed seed, and explicit sparse/constant-feature handling.
- Project learned weights onto production constraints: positive components remain nonnegative, penalty components remain nonpositive, and existing per-weight and total absolute-weight bounds remain valid.
- Start from a fully validated baseline `PolicyConfiguration` and emit a complete disabled candidate JSON that changes only `feature_weights`, plus deterministic training metadata and an offline evaluator comparison.
- Refuse candidate publication unless the candidate demonstrates a preregistered practically meaningful and statistically supported improvement over baseline; tolerated degradation or “non-regression within a margin” is insufficient.
- Add synthetic/golden recovery, determinism, sign/bound, leakage, failure-gate, and documentation coverage.
- Keep recall budgets, diversity, suppression, retention, rollout, deadlines, activation, online inference, databases, experiments, embeddings, vector stores, neural models, and model serving out of scope.
- Preserve the future safety boundary that any emitted candidate is local and inactive and can never automatically activate or roll out.

## Capabilities

### New Capabilities

- `recommendation-policy-weight-learning`: Conditionally activatable future optimization of existing linear weights, gated by preregistered evidence/power/privacy thresholds, strict baseline improvement, inactive candidate generation, and fail-closed publication.

### Modified Capabilities

None. The learner consumes the already planned versioned dataset, label, policy-validation, and evaluator report contracts without extending them.

## Impact

- Indefinitely deferred and not a prerequisite for the current semantic or diagnostic roadmap.
- Only after activation, sequenced after compatible impression/export/evaluation contracts and limited to local versioned artifacts.
- A future implementation may add a small standalone Go operator command and offline learning packages, with no production service or runtime-image dependency.
- Reuses production policy normalization/bounds and evaluator label/report logic, but writes no database or policy state and does not activate the emitted candidate.
- Any future candidate/report remains local and inactive; no public HTTP, frontend, persistence, automatic activation, or main-spec behavior changes.
