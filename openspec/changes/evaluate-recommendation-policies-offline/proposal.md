## Why

Session Semantic Recommendation has passed deterministic, PostgreSQL, and real API/Snapshot
acceptance, but a personal project cannot obtain enough live users to justify relevance claims from
online outcomes. Frux therefore needs a reproducible offline gate that combines production replay,
human Golden Sets, and isolated public short-video datasets before any Shadow proposal.

## What Changes

- Add a standalone Go evaluator with independent `replay`, `golden`, and `public-dataset` tracks;
  it does not start Frux services, query business databases, mutate policy state, or call a model.
- Add a strict KuaiRec v2 adapter for documented interaction/category inputs and a strict MicroLens
  manifest adapter for release-specific canonical exports. Dataset IDs and item/user namespaces stay
  isolated, and Frux does not redistribute or silently download upstream data.
- Define deterministic chronological/session splits and preregistered watch-ratio rules without
  converting missing or neutral observations into negative labels.
- Compare deterministic Popularity, Recent Interaction, Category, Text-only, Image-only,
  Multimodal, and Multimodal + Session Interest baselines when their required inputs are available.
- Report Recall@K, NDCG@K, HitRate@K, MRR, Catalog Coverage, category/author diversity,
  repeated-item/category runs, feature coverage, deterministic ranking work, and optional
  checksum-covered Exact latency/upstream embedding throughput evidence.
- Retain exact production-scorer replay and versioned blinded Golden Sets as separate evidence. A
  public-dataset result cannot substitute for production parity, and replay cannot substitute for
  semantic relevance labels.
- Emit deterministic canonical JSON and concise Markdown containing dataset/release/checksum
  provenance, split counts, baseline availability, metric denominators, exclusions, limitations,
  and `external_model_calls: 0`.
- Keep training export, learned weights, HNSW, Shadow, Rollout, causal-lift claims, and automatic
  policy recommendations out of scope.

## Capabilities

### New Capabilities

- `recommendation-offline-evaluation`: Reproducible production replay, human Golden Set, and
  MicroLens/KuaiRec baseline evaluation with deterministic reports and zero model calls.

### Modified Capabilities

None.

## Impact

- New offline Domain/Application/Infrastructure packages and `cmd/recommendation-offline-evaluate`
  under `apps/api`.
- Checked-in synthetic schema fixtures and Golden Sets only; real public datasets remain in ignored
  operator-owned directories and require explicit source/license acknowledgement.
- Reuse of production policy validation, ranking/diversity contracts, session semantic fixtures, and
  standard-library CSV/JSON/hash/statistics support; no new runtime dependency or business schema.
- Recommendation documentation and roadmap evidence; no public API, frontend, production flag,
  bootstrap policy, model contract, or online serving change.
