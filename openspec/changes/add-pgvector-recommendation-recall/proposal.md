## Why

Frux needs a small, policy-controlled bridge from compatible semantic user profiles to a validated semantic ANN index. The pgvector index lifecycle and semantic-profile projection are separate prerequisites, so this change can focus only on active recall behavior and preserve the existing ranking and bootstrap-policy contracts.

## What Changes

- Add the application-level `semantic_ann` `RecallProvider` token and narrow profile/index interfaces supplied by the prerequisite changes.
- Select a compatible recent semantic profile first, fall back to the compatible long-term profile, and return a healthy empty result when neither exists.
- Execute one bounded semantic ANN lookup using the selected policy budget, provider deadline, and bounded session-video exclusions.
- Emit finite bounded cosine similarity as the `semantic_ann` recall reason and source score, then use the existing merge, visibility recheck, ranking, snapshot, and degradation paths.
- Extend policy validation so a future policy may opt into `semantic_ann` with budget `1..100` and deadline `25..500ms`; keep bootstrap `recommend/v1` and `recommend/v2` unchanged.
- Compose the provider only behind explicit semantic-ANN enablement and available prerequisite interfaces. Registration alone does not activate it.
- Add bounded active-provider metrics, policy-driven rollout/rollback guidance, focused tests, and recommendation documentation.
- Explicitly exclude pgvector deployment, extension/schema/projection/index lifecycle, reconciliation/backfill, and query-plan/performance acceptance to the index prerequisite; defer shadow execution/overlap acceptance to future `shadow-semantic-ann-recall`; exclude ranking feature or weight changes, model training, and automatic policy activation.

## Capabilities

### New Capabilities

- `semantic-ann-recall`: Defines the active semantic ANN provider contract, profile selection, bounded execution, cosine annotations, failure isolation, enablement, observability, and policy-controlled rollout.

### Modified Capabilities

- `contextual-recommendation`: Allows `semantic_ann` as an optional validated recall provider while preserving bootstrap policies, ranking semantics, shared provider bounds, and degraded operation.

## Impact

- Assumes `enable-pgvector-recommendation-index` provides a validated bounded semantic ANN query interface backed by its projection/index.
- Assumes `project-semantic-user-interest` provides compatible recent and long-term semantic profile reads.
- Affects recommendation domain/application policy and provider code, API composition/configuration, active-provider metrics, focused tests, and `docs/modules/recommendation.md`.
- Adds no public API or Web behavior and does not modify persistence schemas, migrations, PostgreSQL deployment, semantic embedding/profile requirements, ranking features, or bootstrap policy contents.
