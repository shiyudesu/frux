## 1. Dependency and Configuration Gates

- [ ] 1.1 Confirm `add-semantic-embedding-service` and `integrate-semantic-video-embeddings` provide the fixed `semantic-minilm-l12-v2@e8f8c211226b894f`, dimension-384, normalized persisted-vector contract.
- [ ] 1.2 Add a static semantic-profile registry mapping provider/model/revision and the fixed persistence key to bounded metric alias, dimension, and `semantic-interest-v1`.
- [ ] 1.3 Add disabled-by-default configuration with bounded batch, poll, lease, retry, cleanup, and per-user rematerialization limits and rejection of arbitrary providers, models, revisions, or dimensions.

## 2. Semantic Profile Domain

- [ ] 2.1 Add semantic profile constants and construction/restoration validation for exact provider/model/revision, schema, dimension, finite recent/long-term/negative vectors, component bounds, version, and materialization time.
- [ ] 2.2 Add semantic source-event identity, stable payload hashing, canonical text-hash/vector-digest identity, immutable event-time vector snapshots, and classification for completion, sustained progress, early skip, active LIKE/FAVORITE, and video-scoped negative feedback.
- [ ] 2.3 Implement the fixed canonical event order, 30-day long-term and 24-hour recent/negative event-time decay to one common anchor, complete-ledger reduction, and one final deterministic clamp.
- [ ] 2.4 Add domain tests for fixed weights, thresholds, completion precedence, follow/`reduce_author` exclusion, identity hashing, content updates, recent/long-term separation, canonical reduction, out-of-order delivery, and bounds.

## 3. PostgreSQL Persistence and Migrations

- [ ] 3.1 Add `recommendation_semantic_user_interest_profile` and immutable `recommendation_semantic_profile_event` models with provider/model/revision-scoped identities, event-time text hash/vector digest/snapshot, and validated metadata.
- [ ] 3.2 Add `recommendation_semantic_profile_outbox` with exact event-time embedding target, unique handoff identity, bounded payload fields, availability, attempts, lease, last result, and dispatch time plus claim/cleanup indexes.
- [ ] 3.3 Register only the profile, ledger, and semantic outbox models in the shared advisory-locked API/worker migration flow and extend account erasure for their user-owned rows.
- [ ] 3.4 Add migration tests for table/index names, composite uniqueness, event-vector and profile 384-vector JSONB round-trip, repeated/concurrent migration, account erasure, and absence of rebuild/staging tables.

## 4. Transactional Semantic Application

- [ ] 4.1 Add repository interfaces for loading only the persisted embedding matching event-time provider/model/revision/text hash and validating dimension, finiteness, norm, and digest without inference.
- [ ] 4.2 Implement transaction-scoped exact-user/model advisory locking, immutable event-ledger insertion, canonical bounded event reads, complete profile rematerialization, and atomic commit.
- [ ] 4.3 Return same-payload-and-embedding duplicates without changing profile version/timestamps; return no-write conflicts for changed payload, text hash, or vector digest under the same semantic identity.
- [ ] 4.4 Add repository tests for first apply, multi-model/revision independence, source-kind namespace reuse, content-update isolation, duplicate/conflict handling, rollback, malformed rows, same-event races, canonical rematerialization, safety-limit deferral, and lost-update prevention.

## 5. Durable Live Handoff

- [ ] 5.1 Add an idempotent semantic handoff writer that captures event-time canonical text hash and exact provider/model/revision into bounded outbox rows after existing outcome/hash-profile work.
- [ ] 5.2 Update behavior, action, and feedback profile processing to persist eligible semantic handoff before source dispatch while preserving current hash/outcome semantics.
- [ ] 5.3 Ensure follow, unfollow, `reduce_author`, unsupported/inactive signals, and disabled models create no semantic row, with no historical replay when a model is later enabled.
- [ ] 5.4 Add handoff tests for insertion failure, crash before source dispatch, duplicate same-payload success, payload/text-hash conflict, content edits before binding, disabled mode, live-only population, and author-only exclusions.

## 6. Leased Worker, Composition, and Metrics

- [ ] 6.1 Implement bounded `FOR UPDATE SKIP LOCKED` claiming with stable owner identity, lease expiry, cancellation, and concurrency limits.
- [ ] 6.2 Implement exact event-time embedding load, capped missing-embedding/persistence retry, transactional event insertion plus canonical rematerialization followed by dispatch marking, duplicate crash recovery, and seven-day dispatched-row cleanup while retaining pending rows.
- [ ] 6.3 Add bounded projection outcome/duration, occurrence-to-application lag, pending/retrying count, oldest-age, and missing-embedding metrics without historical-coverage claims or high-cardinality labels.
- [ ] 6.4 Compose handoff, semantic worker, cleanup, health, and metrics in `cmd/worker` only for enabled supported models, with tests for configuration, leases, retry schedule, cancellation, cleanup, and bounded errors/logs.

## 7. Integration, Documentation, and Validation

- [ ] 7.1 Add integration/outage/regression tests covering persisted pretrained semantic input, event-time identity and content updates, live handoff through canonical per-user rematerialization, deferred missing embeddings, historical users remaining unprofiled, and unchanged current recommendation results.
- [ ] 7.2 Update recommendation, engineering, architecture, and deployment/configuration documentation with exact provider/model/revision/schema, no-training boundary, event-time text-hash/vector-digest semantics, live-only signal flow, fixed weights, canonical reduction, recent/long-term separation, hash fallback, retry, concurrency, metrics, rollout/rollback gaps, and optional `rebuild-semantic-user-interest`.
- [ ] 7.3 Run targeted Go tests, `cd apps/api && go test ./...`, compile `./cmd/feed` and `./cmd/worker`, validate Compose configuration, and run `openspec validate --all --strict`.
