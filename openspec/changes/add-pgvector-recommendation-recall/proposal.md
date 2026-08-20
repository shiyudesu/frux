## Why

Frux needs a dormant, policy-compatible bridge from per-user pretrained semantic interests to exact/ANN full-catalog search, but the provider must not be actively canaried before shadow evaluation. This change registers the provider and fixes candidate mixing/ranking contracts so a later activation cannot lose semantic candidates to pre-rank `published_at` truncation or recall them only to rank by hash features.

## What Changes

- Add the application-level `semantic_ann` `RecallProvider` token and narrow profile/index interfaces supplied by the prerequisite changes.
- Build one normalized query vector by fixed explicit fusion of available session, recent, and long-term pretrained semantic vectors; usable recent interest never completely replaces usable long-term interest.
- Execute one bounded semantic ANN lookup using the selected policy budget, provider deadline, and bounded session-video exclusions.
- Replace the current cross-provider merge path that globally sorts by `published_at` and truncates before ranking with deterministic provider reservations/mixing, including a separate `semantic_ann` pool reservation and at least one retained baseline provider.
- Emit finite bounded cosine similarity as both the `semantic_ann` recall reason/source score and an explicit `semantic_similarity` ranking component required by any future semantic policy.
- Give semantic recall a separate no-queue capacity slot so it cannot consume the baseline provider pool.
- Extend validation for a future semantic policy to require budget, deadline, semantic reservation, positive semantic ranking weight, and at least one baseline provider; keep bootstrap `recommend/v1` and `recommend/v2` byte-for-byte unchanged.
- Compose and register the provider only behind explicit enablement and available prerequisites. Registration alone does not activate it, and this change creates, selects, or rolls out no semantic policy.
- Require `shadow-semantic-ann-recall` acceptance before any later active gray rollout.
- Explicitly exclude pgvector lifecycle, model training, changes to v1/v2, and active canary/rollout from this change.

## Capabilities

### New Capabilities

- `semantic-ann-recall`: Defines dormant provider registration, fixed session/recent/long-term fusion, separate capacity, deterministic pre-rank provider mixing, explicit semantic ranking input, failure isolation, observability, and future-policy validation.

### Modified Capabilities

- `contextual-recommendation`: Allows a future `semantic_ann` policy only with baseline-provider preservation, semantic reservation, and semantic ranking weight while replacing premature global recency truncation with deterministic pre-rank mixing.

## Impact

- Assumes `enable-pgvector-recommendation-index` provides a validated bounded semantic ANN query interface backed by its projection/index.
- Assumes `project-semantic-user-interest` provides compatible recent and long-term semantic profile reads.
- Affects recommendation domain/application policy validation, provider code, candidate-pool mixing, ranking feature extraction, API composition/configuration, dormant-provider metrics, focused tests, and `docs/modules/recommendation.md`.
- Adds no public API or Web behavior and does not modify persistence schemas, migrations, PostgreSQL deployment, semantic embedding/profile requirements, or bootstrap policy contents; no policy is activated in this change.
