## Context

Frux now has a dormant Session Semantic path with a real active-contract vector runtime, trusted
session facts, Exact recall, sampled request evidence, and Snapshot acceptance. The remaining
evidence gap is relevance quality under repeatable inputs. A personal deployment cannot reach the
traffic or randomized exposure needed for a credible online-lift claim, so the evaluation layer must
combine complementary evidence rather than pretending one small log sample is sufficient.

The roadmap names three independent sources:

1. production scorer replay for implementation parity;
2. versioned human Golden Sets for semantic direction and suppression;
3. MicroLens and KuaiRec for public short-video ranking baselines.

KuaiRec documents a v2 package with `small_matrix.csv`/`big_matrix.csv`, watch ratio, timestamps, and
item categories. MicroLens publishes several releases and processing paths whose raw layouts may
differ. Frux must therefore parse KuaiRec's documented profile directly while requiring a
release-specific canonical manifest for MicroLens instead of guessing an upstream layout.

## Goals / Non-Goals

**Goals:**

- Run fully offline with bounded local files and zero external model calls.
- Keep dataset/release/license/checksum provenance explicit and dataset namespaces isolated.
- Produce deterministic chronological/session evaluation cases without future leakage.
- Compare simple explainable baselines plus precomputed content/session-vector baselines.
- Compute standard top-K, coverage, diversity, repetition, and latency metrics with denominators.
- Preserve exact production replay and blinded human Golden Set evidence as separate tracks.
- Emit byte-stable JSON/Markdown suitable for CI artifacts and interview demonstrations.

**Non-Goals:**

- No raw public-dataset redistribution, automatic download, or license acceptance on the user's behalf.
- No embedding generation, model inference, fine-tuning, weight learning, collaborative-filter model
  training, ANN/HNSW, database mutation, API, Worker, Shadow, or Rollout.
- No cross-dataset identity join or combined headline metric.
- No causal-lift, statistical-significance, or production-promotion claim.

## Decisions

### 1. Use one standalone Go command with three explicit tracks

Add `cmd/recommendation-offline-evaluate` with `public-dataset`, `replay`, and `golden` subcommands.
The command imports Frux domain packages but never loads service configuration or connects to
PostgreSQL, Redis, Kafka, HTTP, S3, or a model provider. Each track writes its own report and may be
run independently.

The public-dataset command accepts a dataset kind, an operator-owned dataset root, a manifest path,
one or more K values, explicit case/item/interaction bounds, and JSON/Markdown output paths. Replay
accepts a frozen production bundle and validated policy files. Golden accepts a versioned annotation
bundle and named ranking outputs.

Alternative considered: Python/pandas. Rejected for the first version because streaming CSV,
deterministic ranking, and the required metrics fit the existing Go toolchain; avoiding a second
runtime makes reports easier to reproduce in CI and on interview machines.

### 2. Keep upstream acquisition outside the evaluator

The evaluator never downloads MicroLens or KuaiRec. A manifest records dataset kind, release,
source URL, citation, license identifier or explicit `license_status=operator_reviewed`, file paths,
SHA-256 hashes, row counts, and schema versions. Inputs outside the configured dataset root,
symlinks escaping the root, hash mismatches, unknown files, or unsupported releases fail closed.

Only synthetic schema fixtures are checked into Frux. Real data roots and generated reports are
ignored. This avoids redistributing upstream material and accommodates MicroLens releases whose
terms/layouts require operator review.

### 3. Parse KuaiRec directly and normalize MicroLens through a manifest profile

`kuairec-v2` consumes the documented interaction fields `user_id`, `video_id`, `play_duration`,
`video_duration`, `timestamp`, and `watch_ratio`, plus `item_categories.csv`. Optional canonical
feature files may provide text/image/multimodal vectors and author keys. The adapter verifies the
declared watch ratio against duration values within tolerance and never invents a missing like label.

`microlens-canonical-v1` consumes manifest-declared canonical files produced from one named official
release: interactions (`user_key`, `video_key`, `occurred_at`, `watch_ratio`), items (categories,
optional author key), and optional precomputed feature vectors. The manifest must record the source
release and normalization recipe/version. This is intentionally not a guessed parser for every raw
MicroLens release.

Both adapters emit the same bounded internal records using dataset-local opaque string keys. Keys
are never parsed as Frux IDs and cannot be merged across datasets.

### 4. Use deterministic chronological session cases and preregistered labels

Rows are sorted by user, occurred time, then stable source ordinal. Duplicate user/item/timestamp
rows or conflicting metadata fail. For each eligible user, the last positive interaction becomes
the target; only earlier rows form history. A bounded recent window forms the session.

Default public profile `short-video-session-v1` defines:

- positive: finite watch ratio `>= 0.8`;
- quick skip: finite watch ratio `<= 0.2`;
- neutral: values between the thresholds;
- minimum earlier history: 3 interactions;
- session window: latest 20 earlier interactions;
- candidate universe: all items with required baseline inputs, excluding prior interacted items;
- target inclusion: required, otherwise the case is excluded with a closed reason.

Thresholds and bounds are versioned in the report and may only change through a new profile version.
Neutral rows may contribute recency/category/content history but are not positive relevance targets or
negative session signals. Missing watch ratio is excluded rather than coerced to zero.

### 5. Register explainable baselines with explicit availability

The deterministic baseline registry contains:

- `popularity`: descending positive training count;
- `recent_interaction`: descending latest eligible interaction time;
- `category`: cosine over normalized user/category frequency and item category indicators;
- `text`: cosine between positive-session centroid and precomputed text vectors;
- `image`: the same over precomputed image vectors;
- `multimodal`: the same over precomputed multimodal vectors;
- `multimodal_session`: normalized positive centroid minus bounded negative quick-skip centroid,
  followed by Exact cosine.

All ties use dataset-local item key ascending. Missing required fields make a baseline unavailable for
the affected case; vectors are never zero-filled. Reports show eligible cases, feature coverage, and
exclusion reasons per baseline. No baseline is described as the production policy.

### 6. Compute deterministic metrics per dataset and baseline

For sorted unique K values from 1 through 100, compute Recall@K, NDCG@K, HitRate@K, and reciprocal
rank using the single held-out target. Aggregate Catalog Coverage against the eligible item universe,
distinct category/author coverage when metadata exists, largest category/author share, repeated-item
and repeated-primary-category runs, and per-case ranking latency (`count`, total, min, max, p50, p95)
using integer nanoseconds. Optional upstream embedding throughput is copied only from a signed or
checksum-covered declared measurement file and is never measured through provider calls.

Dataset reports remain separate. A summary may place them side-by-side but cannot average MicroLens
and KuaiRec into one score.

### 7. Keep production replay strict and orthogonal

Factor the existing policy normalization into an exported normalized-clone validator shared by
`NewPolicy` and replay. Replay uses frozen score components, publication time, pseudonymous author,
recall reasons, and expected production order. It reproduces fixed feature-order summation,
production tie-breaking, and diversity. Canonical fixtures require exact baseline parity.

Only score weights and diversity fields are replayable. Differences in recall, provider deadlines,
feature generation, suppression, fallback, sampling, rollout, retention, contracts, or Snapshot
behavior fail comparative replay. A diagnostic-only flag may list differences without policy metrics
or a recommendation.

### 8. Keep Golden Sets blinded and versioned

Golden cases cover Query → Relevant Videos, Source Video → Similar Videos, Session Facts → Expected
Interest Direction, and Negative Feedback → Expected Suppression. Candidate judgments use a 0-3
rubric, at least two independent blinded annotations, and adjudication when judgments differ by two
or more points. Reports include agreement, label coverage, semantic NDCG, direction accuracy, and
suppression accuracy.

Golden metrics never consume public-dataset IDs, and public labels never become Frux Golden labels.

### 9. Publish privacy-safe deterministic reports atomically

Reports contain schema/tool/profile versions, file hashes/counts, source/citation/license metadata,
baseline availability, split/exclusion counts, metrics with numerators/denominators, latency, warnings,
limitations, and `external_model_calls: 0`. They exclude raw rows, vector components, user histories,
credentials, absolute operator paths, and wall-clock timestamps.

JSON uses stable struct ordering and normalized numeric rendering. Markdown is rendered from the same
report value. Both outputs use sibling temporary files, `0600`, fsync, and atomic rename; a partial
failure preserves existing final outputs.

## Risks / Trade-offs

- [MicroLens release layouts or terms vary] → Require an operator-reviewed canonical manifest and do
  not claim universal raw-format support.
- [KuaiRec watch ratio is not explicit preference] → Version thresholds, report neutral/missing rows,
  and avoid causal or production-lift claims.
- [Precomputed features came from different models] → Record feature source/dimension/normalization
  per channel and compare only compatible cases.
- [Simple baselines understate stronger research models] → Present them as engineering baselines, not
  state of the art; training remains a separate gated change.
- [Chronological holdout has one target] → Report exact denominators and complement it with Golden
  Sets rather than manufacturing multiple labels.
- [Large CSVs exceed local memory] → Stream interactions, enforce manifest bounds, and retain only
  bounded per-user histories, item aggregates, and final cases.
- [Latency varies by machine] → Record operation/count statistics as engineering evidence, not a
  cross-machine benchmark.

## Migration Plan

1. Replace the outdated evaluation artifacts with the three-track contract and commit the plan.
2. Add common contracts, strict manifests, KuaiRec/MicroLens adapters, and synthetic fixtures.
3. Add deterministic cases, baselines, metrics, reports, and CLI.
4. Add production replay and Golden Set integration, documentation, and complete verification.
5. Run checked-in fixtures in CI; real dataset runs remain explicit operator actions.
6. Roll back by removing the offline command/packages and specs; there is no business migration.

## Open Questions

No implementation-blocking question remains. Exact upstream file acquisition and MicroLens release
normalization are operator inputs because Frux must not silently accept terms or redistribute data.
