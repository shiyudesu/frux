## Context

The current roadmap prioritizes trusted diagnostic impressions, deterministic production-scorer replay, and blinded human semantic evaluation. Broad training export and learned weights are not required for that route. Existing observational data is delivered-card-only, position-biased, sparse, and not yet proven adequate for stable optimization.

This change is an indefinitely deferred conditional design. It becomes implementable only after a separate dated activation record proves, with preregistered numeric thresholds and approvals, that the evidence is sufficient and that learning addresses a decision the low-data route cannot answer.

Production ranking is already a linear sum over eight normalized components: content similarity, session similarity, hotness, freshness, author affinity, follow relation, negative penalty, and exposure penalty. Production policy validation currently caps each absolute feature weight at `MaxFeatureWeight` and total absolute weight at `MaxTotalFeatureWeight`. The first six weights are beneficial signals; the final two are penalties. The learner must improve only these coefficients without changing recall, feature generation, diversity, suppression, retention, rollout, deadlines, or serving.

The exported data is observational and includes only delivered cards. A delivered card without validated exposure is not evidence of rejection, and alternative positions have no counterfactual outcomes. Learning and evaluator gates must therefore remain exposure-safe, held-out, deterministic, and explicitly non-causal.

## Goals / Non-Goals

**Goals:**

- Keep implementation inactive until sample-size, label-coverage, power, stability, privacy, and resource gates are approved with no unresolved values.
- Learn only the eight existing linear feature weights from compatible versioned export artifacts.
- Reuse production policy validation and the evaluator's `observational-utility/v1` label and replay/metric semantics.
- Use deterministic request-local pairwise optimization with bounded data, sampling, memory, iterations, and output.
- Enforce feature signs and all current production weight bounds throughout optimization.
- Verify time- or user-safe split integrity, keep test data untouched until final comparison, and fail on leakage.
- Handle sparse and constant features explicitly, report coverage, and fail when aggregate evidence is inadequate.
- Emit a complete candidate `PolicyConfiguration` that differs from the baseline only in `feature_weights`.
- Publish candidate and report atomically only after convergence, integrity, coverage, and strict evaluator improvement gates pass.
- Use the existing Go toolchain and standard library with no production runtime dependency changes.

**Non-Goals:**

- Acting as a prerequisite for semantic relevance, recall coverage, author/topic diversity, or deterministic scorer replay.
- Learning or changing recall budgets, provider deadlines, diversity, half-lives, exposure windows, suppression, fallback, rollout, sampling, retention, snapshot settings, or any other policy field.
- Writing a policy to PostgreSQL, enabling it, assigning a version, activating it, scheduling it, or implementing an A/B system.
- Causal-lift claims, propensity estimators, exploration, or imputing outcomes for unobserved positions.
- Semantic embeddings, pgvector, two-tower or sequence models, neural inference, model registries, or model servers.
- Querying production databases or services, modifying export facts, or adding public APIs/frontend behavior.
- Introducing Python or a numerical dependency; if a future algorithm requires Python, it needs a separate change with a pinned isolated tooling environment.

## Decisions

### 0. Require preregistered activation before any implementation

The change remains inactive unless a reviewed activation record includes:

- a precise learning hypothesis, primary metric, minimum practically meaningful effect, and why human/replay diagnostics are insufficient;
- a prospective sample-size calculation with significance level at most 0.05, power at least 0.80, independent-unit definition, train/validation/test counts, and multiplicity handling;
- minimum validated exposure coverage, positive and negative label counts/rates, maximum missing-label rate, and per-feature variation thresholds;
- stability requirements across at least two non-overlapping time windows, relevant user/content slices, and multiple fixed seeds or resamples;
- privacy/security approval for training purpose, deletion, opt-out, pseudonymization, retention, access, and artifact disposal;
- bounded compute/memory/runtime/storage budgets, owners, and abort thresholds.

Thresholds must be numeric and preregistered before inspecting the activation dataset. If any gate fails, the deferral remains indefinite. Offline semantic evaluation proceeds independently.

### 1. After activation, add a small standalone Go learner after the prerequisite changes

Add `apps/api/cmd/recommendation-policy-learn`. Its required inputs are:

- `--manifest` and `--dataset`;
- `--baseline`;
- `--output-candidate` and `--output-report`;
- bounded learning, coverage, convergence, sampling, and evaluator-gate flags;
- `--seed`, default `1`;
- explicit safe overwrite behavior.

The command will reuse the export integrity reader, production policy validator, evaluator label derivation, replay, and metrics as Go packages rather than shelling out or duplicating formulas. It will not load service configuration or initialize database, cache, broker, HTTP, embeddings, or model clients.

Alternative considered: Python with NumPy/scikit-learn. Rejected because an eight-dimensional convex linear objective does not justify a second runtime, dependency lock, or production-image ambiguity. Go permits direct reuse of production validation and deterministic evaluator code.

### 2. Fail closed on artifact integrity and exact compatibility

Preflight performs the evaluator/export checks before allocating training state: canonical manifest parsing, compressed filename/size/SHA-256, manifest SHA-256, row count and line bounds, gzip completeness, canonical JSONL fields and ordering, duplicate identity checks, and reconciliation of all manifest counts. A registry identifies the exact compatible dataset, impression record, feature, label, source-model, pseudonymization, evaluator-label, replay, and report versions.

Dataset rows must contain all eight registered components exactly once with finite values in `[0,1]`. Unknown, missing, duplicate, or non-finite components are structural incompatibilities, not imputable missing data. The report records all source version sets and hashes.

Alternative considered: skip incompatible rows. Rejected because silent row removal changes feature coverage, pair composition, and learned weights in a way that cannot be safely compared across runs.

### 3. Admit only exposed rows to optimization

The evaluator's `observational-utility/v1` formula is the sole relevance target. The learner derives it through the evaluator package rather than maintaining a second formula. Optimization eligibility additionally requires a validated exposure. Both `delivered_unexposed` and `engaged_unexposed` rows are excluded from every pair, including the lower side, even if a durable signal exists. This stricter rule avoids treating delivery or unverified visibility as an opportunity to engage.

Within each request and split, row A is preferred to row B only when:

```text
utility(A) - utility(B) >= min_label_gap
```

The default `min_label_gap` is `0.05`, bounded to `(0,1]`. Pair weight is `clamp(utility(A)-utility(B), min_label_gap, 1)`. Ties and near-ties are counted but omitted.

Alternative considered: pointwise regression to utility. Rejected because the production task is within-request ordering, utility calibration is observational, and pairwise differences cancel request-level offsets. Alternative considered: use unexposed positive rows only as preferred examples. Rejected because exposure eligibility would then differ across pair sides and complicate the safety interpretation.

### 4. Revalidate the export's leakage-safe split before constructing pairs

The learner supports the exporter-defined user or time strategy:

- User strategy: every `user_key`, request, row, and pair occurs in exactly one of train, validation, or test.
- Time strategy: boundaries are strictly ordered, embargo is at least the label horizon, every label cutoff closes inside its assigned split, and manifest embargo exclusions reconcile with rows and counts.

Requests may not cross splits under either strategy. Pairs are always request-local and split-local. Train creates gradients; validation selects a checkpoint and convergence; test is read only after selection for one final evaluator comparison. The report records split counts for rows, exposed examples, users, requests, pairs, and exclusions.

Alternative considered: reshuffle exporter splits inside the learner. Rejected because it would weaken dataset reproducibility and could violate the export's HMAC user grouping or temporal embargo contract.

### 5. Bound and balance examples and pairs deterministically

Rows are canonicalized by `(split, user_key, request_key, video_id)`. Eligible rows in each request are assigned to fixed utility strata `[0,.25)`, `[.25,.5)`, `[.5,.75)`, and `[.75,1]`. Defaults and hard bounds are:

- at most 64 eligible rows per request;
- at most 16 rows per request/utility stratum;
- at most 256 pairs per request;
- at most 250,000 train pairs and 100,000 validation or test pairs;
- at most 1,000,000 parsed rows, consistent with the evaluator bound.

When a cap applies, priority is SHA-256 over the tool sampling version, seed, split, user key, request key, video ID or pair IDs, and stratum. The lowest hashes win, followed by canonical identity tie-breaks. No map iteration or source row order affects selection. Stratum caps prevent one dense utility class from consuming all per-request capacity; pair caps prevent quadratic growth.

Alternative considered: PRNG shuffling. Rejected because hash priority is simpler to resume, inspect, golden-test, and keep stable when unrelated rows are reordered.

### 6. Optimize a convex signed pairwise objective with projected gradient descent

For preferred/lower feature vectors `x+` and `x-`, `d = x+ - x-`, pair weight `q`, candidate weights `w`, and baseline `w0`, training minimizes:

```text
sum(q * log(1 + exp(-dot(w, d)))) / sum(q)
+ lambda_l2 * ||w||²
+ lambda_anchor * ||w - w0||²
```

Defaults are `lambda_l2=0.001`, `lambda_anchor=0.01`, initial learning rate `0.05`, maximum `500` epochs, minimum `20` epochs, tolerance `1e-7`, and patience `25`; every option has validated finite operational bounds. Accumulation uses canonical pair order, the fixed eight-feature registry, compensated summation, and stable logistic-loss branches. The baseline is epoch zero.

Each full-batch step is projected onto:

- `[0, MaxFeatureWeight]` for the six positive features;
- `[-MaxFeatureWeight, 0]` for the two penalty features;
- `sum(abs(w)) <= MaxTotalFeatureWeight`.

Projection first maps weights to nonnegative magnitudes in their fixed sign direction, then performs deterministic Euclidean projection onto the capped L1 simplex by fixed-iteration bisection, and finally restores signs. Production validation is still rerun on every selectable checkpoint and the final candidate.

The best checkpoint is the lowest finite validation pairwise loss. Ties within `1e-12` choose the earliest epoch. Convergence requires a valid checkpoint, finite train/validation losses, projected-gradient norm at or below tolerance or validation improvement below tolerance for the full patience window after the minimum epoch, and no later invalid value. Exhausting maximum epochs without the configured convergence condition fails closed.

Alternative considered: unconstrained optimization followed by final clamping. Rejected because it evaluates and selects infeasible intermediate models and can distort the optimum. Alternative considered: stochastic Adam. Rejected because fixed-order full-batch projected descent is easier to make byte-deterministic in only eight dimensions.

### 7. Freeze sparse or constant features and gate preregistered coverage

Every feature receives training coverage statistics: finite count, zero/nonzero count, minimum, maximum, mean, and pairwise nonzero-difference count. A feature is trainable only when it has at least 100 nonzero rows, at least 1% nonzero coverage, range greater than `1e-9`, and at least 100 training pairs with a nonzero difference. Otherwise it remains exactly at the normalized baseline weight and its freeze reason is recorded.

Any activated implementation must use the stricter of the preregistered power/coverage plan and these operational floors:

- train: at least 1,000 exposed labeled rows, 100 requests, 30 users, 5,000 pairs;
- validation: at least 250 exposed labeled rows, 30 requests, 30 users, 1,000 pairs;
- test: at least 250 exposed labeled rows, 30 requests, 30 users, 1,000 pairs;
- at least four trainable features, including at least one penalty feature.

Production candidate publication tests may not lower activation thresholds. Unit fixtures may use explicitly test-only thresholds. Every effective threshold is recorded, and structural absence of a component is never treated as sparsity.

Alternative considered: zero-impute or drop sparse features from the candidate. Rejected because rows are required to have complete components and the output must remain a complete baseline-derived production configuration.

### 8. Emit a baseline-derived candidate and prove non-weight equality

The baseline is strictly decoded with unknown/trailing JSON rejection, hashed as input bytes, normalized through the shared production validator, and hashed again as canonical normalized JSON. It must contain exactly the eight registered weights.

Candidate assembly deep-clones the normalized baseline and replaces only `FeatureWeights`. The learner canonicalizes and compares all other fields, revalidates the full candidate through production rules, and verifies all feature signs and bounds. The output is exactly one complete `PolicyConfiguration` JSON object with canonical field order and one trailing newline.

The file is not a policy record: it contains no ID, version, `enabled` flag, activation request, or persistence instruction. Its existence has no online effect. The report marks it `candidate_only` and `activation_state: inactive`; a future reviewed change must define any activation workflow.

Alternative considered: set rollout to zero to signal disabled. Rejected because that would violate the invariant that only feature weights differ from the baseline. Inactivity is guaranteed by the local artifact boundary and absence of all persistence/activation behavior.

### 9. Require genuine held-out improvement over baseline

After checkpoint selection, the learner uses the evaluator packages to replay the normalized baseline and candidate over exactly the held-out test request groups. It inherits `served_subset_replay`, `observational-utility/v1`, production tie-breaking/diversity, complete-label metric eligibility, user-cluster bootstrap, and non-causal warnings.

The preregistered primary metric must show both:

- a point improvement at least as large as the preregistered minimum practically meaningful effect; and
- a multiplicity-adjusted 95% confidence lower bound strictly greater than zero.

Every preregistered guardrail must be no worse than baseline with a confidence bound on the safe side of zero; quick-skip and explicit-negative rates therefore require an upper bound at or below zero, not a positive tolerance. Improvement must repeat in the required non-overlapping time windows and stability slices, and candidate direction must remain stable across the preregistered seeds/resamples.

Any missing estimate, inadequate power/coverage, instability, baseline parity failure, integrity error, new exclusion category, or tolerated degradation blocks candidate and report publication. Passing is still observational evidence, not causal lift or activation approval.

Alternative considered: gate only on training/validation loss. Rejected because surrogate pairwise loss can improve while replay metrics regress. Alternative considered: allow small degradation margins to produce more candidates. Rejected because a deferred optional learner has no roadmap justification unless it demonstrates real held-out improvement over the production baseline.

### 10. Produce deterministic atomic artifacts

The success report is canonical JSON with fixed structs/sorted arrays, normalized finite numbers, no wall-clock timestamp, and one trailing newline. It includes:

- tool, optimizer, sampler, dataset, record, feature, label, replay, evaluator, and report versions;
- manifest, compressed dataset, baseline input, normalized baseline, candidate, and report-relevant configuration hashes;
- split policy and reconciled counts;
- seed and all hyperparameters/bounds;
- class/pair sampling and exclusion counts;
- convergence reason, selected epoch, losses, projected-gradient norm, and bounded trace summary;
- per-feature coverage, freeze state, baseline weight, learned weight, and constraint checks;
- held-out evaluator comparison, gates, warnings, and non-causal/served-subset limitations.

Candidate and report are written to permission-restricted sibling partial files, synced, cross-hashed, and atomically renamed only after both are complete. Existing files require explicit safe overwrite and remain untouched on failure. No final success report is emitted without its candidate; bounded failure categories go to stderr without source rows or identities.

Alternative considered: include `generated_at`. Rejected because it would break byte determinism without improving reproducibility.

### 11. Test with synthetic recovery and golden failure fixtures

A seeded synthetic generator creates request groups from known signed weights, complete bounded components, exposure-safe labels, and valid user/time splits. Tests verify ordering recovery and expected weight directions within tolerance rather than claiming exact coefficient identifiability. Golden candidate/report fixtures cover a small deterministic run.

Additional tests cover row-order invariance, repeated-run bytes, hash pair caps, sign and L1/box projection, sparse/constant freezing, baseline non-weight equality, user/time/request leakage, embargo and label-cutoff violations, test isolation, unsupported versions, inadequate coverage, non-convergence, evaluator regressions, corrupted gzip/manifest, permission-restricted atomic publication, write/sync/rename failures, and preservation of existing outputs.

## Risks / Trade-offs

- [Observational labels are position-biased and served-subset-only] → Train only within observed exposed request groups, retain evaluator warnings, use held-out observational gates, and make no causal or online-lift claim.
- [Exposure filtering can substantially reduce sample size] → Report every exclusion and enforce explicit split-level coverage gates rather than silently relaxing eligibility.
- [Pair counts grow quadratically] → Bound rows per stratum/request and pairs per request/split using deterministic hash priority.
- [Feature collinearity can make exact weights non-identifiable] → Anchor to the baseline, regularize, freeze unsupported features, and test recovered ordering/direction rather than exact coefficients.
- [Sparse penalty features may prevent safe learning] → Require at least one trainable penalty feature and retain baseline values for frozen components.
- [Floating-point changes across implementations can affect bytes] → Use Go standard-library arithmetic, fixed feature/pair order, compensated sums, fixed projection iterations, canonical number normalization, and golden tests on supported platforms.
- [An evaluator gate can be overfit if test data is reused repeatedly] → The tool accesses test only after checkpoint selection and records hashes; operator documentation treats repeated manual iteration against one test export as invalid practice.
- [A local candidate file could be mistaken for an active policy] → Emit only `PolicyConfiguration`, mark the report inactive, perform no DB/API operation, and document that activation requires a separate future workflow.

## Migration Plan

1. Keep the change deferred while the low-data semantic and diagnostic route proceeds.
2. Create and approve the preregistered activation record before implementing or inspecting an activation dataset.
3. If activated, validate compatible impression, export, and evaluator contracts and their privacy boundaries.
4. Add the learner command, deterministic optimizer/sampler, strict-improvement gates, artifact writer, fixtures, tests, and operator documentation.
5. Trial only on copied local artifacts; review inactive candidates without activation.
6. Roll back by removing access to the standalone learner binary and deleting local candidate/report files. No database, policy, export, service, or runtime state requires migration or rollback.

## Open Questions

Activation thresholds and approvals are intentionally unresolved. That is the reason this change remains indefinitely deferred. They must be preregistered and approved before implementation; prerequisite contract mismatches remain fail-closed after activation.
