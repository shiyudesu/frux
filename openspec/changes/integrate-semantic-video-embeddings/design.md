## Context

`docs/recommendation-roadmap.md` makes this change step 6 of the recommendation sequence. It must
not be implemented until the trusted-impression, dataset-export, offline-evaluation, learned-weight,
and standalone semantic-service changes are complete and archived.

Separately, `migrate-video-workflows-to-kafka` moves video publication to the retained
`frux.video.published.v1` Kafka topic. Feed and `hash-ngram-v1` processing use independent consumer
groups, while PostgreSQL remains authoritative for long-running media work. This change builds on
that completed transport design rather than preserving the former RabbitMQ delivery topology.

The semantic HTTP service is deterministic but remote and comparatively expensive. A Kafka
publication offset therefore cannot remain uncommitted while inference retries occur. The intake
boundary must first preserve the existing hash embedding and then record semantic work durably in
PostgreSQL.

## Goals / Non-Goals

**Goals:**

- Generate fixed-version semantic embeddings for newly published videos.
- Preserve `hash-ngram-v1` as the mandatory first and fallback representation.
- Use Kafka only for retained publication intake and PostgreSQL for semantic execution state,
  leases, retries, and terminal outcomes.
- Keep duplicate delivery, uncertain offset commits, worker restarts, and service outages safe.
- Validate the semantic service and every returned vector before persistence.
- Keep semantic failures isolated from Feed, hash generation, media processing, and unrelated
  workers.

**Non-Goals:**

- Implementing any recommendation-roadmap step before its predecessors are archived.
- Historical video scanning or backfill.
- Semantic user-interest projection or rebuild.
- pgvector, ANN indexes, recall providers, ranking features, policy changes, or online request-path
  inference.
- RabbitMQ publication, retry, delay, dead-letter, or compatibility queues.
- Media processing, media lifecycle revocation, or object-store cleanup changes.
- Mutable model selection, training, or automatic model upgrades.

## Decisions

### 1. Gate implementation on both roadmap and transport prerequisites

Implementation starts only after recommendation steps 1-5 and
`migrate-video-workflows-to-kafka` are archived. The semantic service contract and the Kafka
publication contract are therefore dependencies, not code that this change may create early.

This gate is stricter than the roadmap's optional parallelization guidance because the requested
execution mode is a single ordered sequence. Planning artifacts may remain active, but no task is
considered implemented until its prerequisites are archived.

### 2. Extend the Kafka hash-embedding intake instead of introducing a retry stream

The existing embedding consumer group reads `frux.video.published.v1`, validates the versioned
envelope and video-ID key, and processes each record independently from Feed. Its durable success
boundary becomes:

1. canonicalize the bounded title and description;
2. look up or persist `hash-ngram-v1`;
3. upsert the semantic job for the fixed semantic model and canonical text hash;
4. allow the Kafka offset to commit.

If either durable write fails, the record remains uncommitted. If the offset commit is uncertain,
redelivery is safe because both the hash fact and semantic job have stable identities. Semantic
inference is never performed in the Kafka handler.

An alternative semantic Kafka consumer group would duplicate canonicalization and could race hash
coverage. Extending the existing hash group gives one explicit hash-first boundary. Kafka retry
topics and uncommitted-record retry loops are rejected because remote inference may remain
unavailable far longer than a consumer session.

### 3. PostgreSQL owns semantic execution and retry state

One semantic job is identified by `(video_id, model)`. It records canonical text hash, bounded
state, attempts, `available_at`, lease owner and expiry, bounded error class, completion metadata,
and timestamps. A publication with the same text hash is idempotent; a different hash resets the
same model job to pending and fences stale completion.

Workers claim bounded batches in stable order with `FOR UPDATE SKIP LOCKED`. Processing uses a
bounded lease and heartbeat. Expired processing leases are reclaimable. Retry delays are 5 seconds,
30 seconds, 2 minutes, 10 minutes, then capped at 30 minutes. Local cancellation or uncertain lease
ownership prevents completion writes.

Transport, timeout, overload, authentication, readiness, and service-contract failures close the
affected replica's local semantic gate and release work retryably. Deterministic invalid input may
be terminally classified. Shared jobs are not rewritten into a cluster-wide suspended state merely
because one replica cannot reach or validate the service.

### 4. Fix model identity and text canonicalization

The only accepted semantic contract is:

- model: `sentence-transformers/paraphrase-multilingual-MiniLM-L12-v2`;
- revision: `e8f8c211226b894fcb81acc59f3b34ba3efd5f42`;
- dimension: 384;
- dtype: `float32`;
- device: CPU;
- output: finite L2-normalized vectors;
- persistence key: `semantic-minilm-l12-v2@e8f8c211226b894f`.

Go canonicalization matches the standalone service contract exactly. The job and persisted row
carry a canonical text hash so duplicate publications and stale workers cannot overwrite a newer
fact. A future model or revision must use a new persistence key and separate job identity.

### 5. Use a bounded authenticated client with strict response validation

The Go client uses only the configured `X-Internal-Token`, bounded connection and in-flight request
counts, explicit metadata and embedding deadlines, response-size limits, and no automatic retries.
It validates service metadata before opening the local claim gate.

Every response is checked for exact model metadata, item count, IDs, indexes, order, dimensions,
finite components, and unit norm. A complete batch is rejected if any item is missing, reordered,
duplicated, unknown, non-finite, wrongly dimensioned, or wrongly versioned. Valid vectors are
normalized once more before bounded JSON persistence.

Invalid local configuration fails worker startup. Remote unavailability or metadata mismatch does
not fail the process: the local semantic gate remains closed and background probes retry with
bounded delay while other workers continue.

### 6. Persist semantic and hash facts side by side

The existing video embedding identity `(video_id, model)` remains authoritative.
`hash-ngram-v1` and the fixed semantic model therefore coexist as separate rows. Semantic rows
contain model key, dimension 384, canonical text hash, finite normalized bounded JSON, and
timestamps.

Conditional persistence avoids updating an identical `(video_id, model, text_hash)` fact and
requires the active job lease and expected text hash before writing a changed fact. This change
does not add a pgvector column, ANN index, or new recommendation vector table.

### 7. Separate durable handoff from optional execution capacity

Once this integration is deployed, Kafka intake always creates semantic jobs after hash success.
Semantic execution is independently enabled and capacity-limited. A disabled or unready replica
does not claim jobs; shared pending/retry work remains durable and visible for healthy replicas or a
later enablement.

Compose enables execution and points the worker at the internal-only semantic service with the
shared strong token. The worker uses a `service_started` dependency rather than a health-gated
dependency so semantic readiness cannot block unrelated worker startup.

### 8. Bound observability and operational controls

Metrics cover metadata/embedding request count and latency by bounded result, hash and semantic
intake outcomes, semantic job count and oldest age by bounded state, local gate readiness, active
leases, retry outcomes, and readable-video semantic coverage.

Labels never include video IDs, text, URLs, tokens, vectors, raw errors, retry numbers, or arbitrary
model strings. Logs use bounded operation and result classes and never emit request text or
authentication material.

### 9. Roll out without changing recommendation behavior

Rollout order is:

1. verify all roadmap and Kafka migration dependencies are archived;
2. deploy the standalone semantic service and confirm its fixed metadata contract;
3. migrate semantic-job persistence;
4. deploy worker code with execution disabled and verify hash-first job handoff;
5. enable bounded semantic execution and monitor backlog, retries, coverage, and unrelated worker
   health;
6. retain all current recommendation policies unchanged.

Rollback disables semantic job claims first. Kafka hash intake, durable job rows, and existing
semantic facts remain intact. The worker can then be rolled back only to a version whose Kafka
consumer contract is compatible; no RabbitMQ semantic route is restored. Recommendation behavior
is unaffected because no current provider consumes the semantic rows.

## Risks / Trade-offs

- [Semantic backlog grows during an outage] -> Keep claims bounded, expose count/oldest-age metrics,
  cap retry delay, and alert without blocking Kafka intake.
- [Service revision or response shape drifts] -> Require exact metadata and response validation and
  keep the local claim gate closed on mismatch.
- [Kafka redelivery repeats durable work] -> Use stable hash and job identities plus conditional
  persistence.
- [A stale worker overwrites changed text] -> Fence completion by lease ownership and expected
  canonical text hash.
- [Semantic execution consumes worker resources] -> Bound HTTP connections, concurrency, claim
  batches, leases, and database operations independently from other workers.
- [Historical coverage remains incomplete] -> Defer scanning and resumability to
  `backfill-semantic-video-embeddings` and report coverage explicitly.

## Open Questions

None. Model identity, roadmap order, Kafka source, PostgreSQL retry ownership, and future boundaries
are fixed by prerequisite changes and the recommendation roadmap.
