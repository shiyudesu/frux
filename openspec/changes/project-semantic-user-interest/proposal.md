## Why

Frux has durable recommendation facts and pretrained, model-versioned semantic video embeddings, but no separate per-user semantic-interest projection that future session-aware recall and rule ranking can consume. The confirmed low-data route favors deterministic single-user aggregation over group-model training, while keeping current recommendation behavior unchanged.

## What Changes

- Add a separate provider/model/revision-isolated semantic user-interest profile keyed by user and exact pretrained embedding identity, with long-term, recent, and negative vectors, profile schema/dimension metadata, version, and materialization time.
- Add a model-scoped semantic event ledger so one durable source event is aggregated once per user and exact model without colliding with the existing hash-profile ledger.
- Bind every eligible event to the content identity visible for that event: embedding provider, model, revision, canonical text hash, and vector digest. Later content edits do not silently reinterpret old behavior; later events bind the new content identity.
- Store enough immutable event-time embedding evidence for live rematerialization and optional rebuild to use the same vector, rather than rereading whichever embedding happens to be current later.
- Project eligible live completion, sustained progress, LIKE, FAVORITE, early-skip, and video-scoped negative-feedback facts while keeping follow and `reduce_author` exclusively in the existing non-semantic profile.
- Durably hand eligible live facts from the existing profile-outbox flow to a dedicated leased semantic queue, without delaying the originating interaction, view, or feedback API.
- Retry missing exact event-time embeddings durably with bounded leases and capped delays; never call an embedding API or rebind an old event to updated content.
- Recompute only the affected user's profile from its semantic event ledger in one canonical event order, applying fixed signal weights and one deterministic reduction/clamp contract so delivery order cannot change the result.
- Preserve both recent and long-term vectors as explicit inputs for later fusion; recent interest never completely replaces a usable long-term profile.
- Add event-time decay, per-user/model concurrency control, bounded-cardinality projection/lag/queue/retry metrics, migrations, worker composition, tests, and documentation.
- Depend explicitly on `add-semantic-embedding-service` and `integrate-semantic-video-embeddings`, including their fixed model identity, dimension validation, durable semantic video rows, and missing-coverage behavior.
- Exclude historical replay, rebuild, backfill, checkpointing, staging, repair/purge commands, and completeness recovery. Low-volume deployments may skip the optional future `rebuild-semantic-user-interest` capability and let profiles form naturally from new behavior.
- Keep online recommendation recall/ranking unchanged. Semantic-profile absence remains a normal condition that later consumers must handle by preserving the existing hash/non-vector path. Do not add pgvector, ANN queries, a semantic recall provider, policy features, group-model training, or removal/reinterpretation of the existing hash profile.

## Capabilities

### New Capabilities

- `semantic-user-interest`: Defines exact-model per-user profile persistence, immutable event-time embedding identity, model-scoped idempotency, canonical single-user rematerialization, missing-embedding leased retry, decay, concurrency, observability, migration, composition, and verification.

### Modified Capabilities

None. Current `contextual-recommendation` recall, ranking, hash-profile, author-affinity, and fallback requirements remain unchanged; this projection is intentionally not consumed online yet.

## Impact

- Affects recommendation domain/application projection code, PostgreSQL recommendation persistence and migrations, leased profile-outbox processing, worker composition/configuration, metrics, tests, and recommendation documentation.
- Reads live durable playback behavior, accepted like/favorite actions, and explicit negative video feedback through existing fact/outbox patterns; follow and author-scoped feedback remain owned by the existing profile path.
- Reads pretrained semantic video embeddings produced by `integrate-semantic-video-embeddings`, records provider/model/revision/text-hash/vector-digest identity, and validates model/dimension/schema before projection.
- Adds no public API, Web behavior, historical processing command, or completeness guarantee and does not change Feed availability, candidate recall, ranking, or current hash-based recommendation behavior.
