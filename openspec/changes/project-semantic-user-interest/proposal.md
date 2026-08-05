## Why

Frux has durable recommendation facts and model-versioned semantic video embeddings, but no separate semantic user-interest profile that future semantic recall can consume. This change adds the live shadow projection without expanding into historical reconstruction, so the implementation remains bounded and existing recommendation behavior stays unchanged.

## What Changes

- Add a separate model-versioned semantic user-interest profile keyed by `(user_id, model)` with long-term, recent, and negative vectors, profile schema/dimension metadata, version, and materialization time.
- Add model-scoped applied-event identity so one durable source event can be projected once per semantic model without colliding with the existing hash-profile ledger.
- Project eligible live completion, sustained progress, LIKE, FAVORITE, early-skip, and video-scoped negative-feedback facts while keeping follow and `reduce_author` exclusively in the existing non-semantic profile.
- Durably hand eligible live facts from the existing profile-outbox flow to a dedicated leased semantic queue, without delaying the originating interaction, view, or feedback API.
- Retry missing exact-model video embeddings with bounded leases and capped delays; apply profile and ledger updates atomically once the embedding exists.
- Add event-time decay, per-user/model concurrency control, bounded-cardinality projection/lag/queue/retry metrics, migrations, worker composition, tests, and documentation.
- Depend explicitly on `add-semantic-embedding-service` and `integrate-semantic-video-embeddings`, including their fixed model identity, dimension validation, durable semantic video rows, and missing-coverage behavior.
- Exclude historical replay, rebuild, backfill, checkpointing, staging, repair/purge commands, and completeness recovery. Users whose eligible facts predate live handoff may remain without semantic profiles until the future `rebuild-semantic-user-interest` change.
- Keep online recommendation recall/ranking unchanged. Do not add pgvector, ANN queries, a semantic recall provider, policy features, model training, or removal/reinterpretation of the existing hash profile.

## Capabilities

### New Capabilities

- `semantic-user-interest`: Defines model-versioned semantic profile persistence, model-scoped idempotency, live durable event projection, missing-embedding leased retry, decay, concurrency, observability, migration, composition, and verification.

### Modified Capabilities

None. Current `contextual-recommendation` recall, ranking, hash-profile, author-affinity, and fallback requirements remain unchanged; this projection is intentionally not consumed online yet.

## Impact

- Affects recommendation domain/application projection code, PostgreSQL recommendation persistence and migrations, leased profile-outbox processing, worker composition/configuration, metrics, tests, and recommendation documentation.
- Reads live durable playback behavior, accepted like/favorite actions, and explicit negative video feedback through existing fact/outbox patterns; follow and author-scoped feedback remain owned by the existing profile path.
- Reads semantic video embeddings produced by `integrate-semantic-video-embeddings` under an exact revision-bearing model key and validates model/dimension/schema before projection.
- Adds no public API, Web behavior, historical processing command, or completeness guarantee and does not change Feed availability, candidate recall, ranking, or current hash-based recommendation behavior.
