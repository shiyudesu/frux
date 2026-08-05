## Context

Frux already publishes `application/video.PublishedEvent` messages to a dedicated embedding queue. `application/embedding` uses the domain `Vectorizer` interface and the in-process `hash-ngram-v1` implementation, then persists JSON vectors through `domain/embedding.Repository`. PostgreSQL already identifies facts by `(video_id, model)` and can therefore hold multiple model versions for one video.

The active planned change `add-semantic-embedding-service` defines, but has not yet integrated, an authenticated internal service with:

- `GET /internal/v1/model`;
- `POST /internal/v1/embeddings`;
- fixed model `sentence-transformers/paraphrase-multilingual-MiniLM-L12-v2`;
- revision `e8f8c211226b894fcb81acc59f3b34ba3efd5f42`;
- 384 finite L2-normalized components;
- ordered batches of at most 32 title/description items;
- a 15-second service deadline and bounded overload responses.

This proposal depends on that change. It does not redefine the Python service or make semantic vectors part of recommendation behavior. It must preserve hash coverage when the service is absent, slow, overloaded, misconfigured, or returning an invalid contract.

The current video embedding consumer uses a shared RabbitMQ consumer channel and immediately `Nack(requeue=true)` on any handler failure. Extending that path with a remote dependency without isolation and delayed retries would create hot retry loops and could interfere with unrelated video consumers.

Historical coverage is intentionally split out. This change establishes the reusable live-integration contracts, while a future separate change named `backfill-semantic-video-embeddings` will own historical selection and resumable operator workflows.

## Goals / Non-Goals

**Goals:**

- Keep `hash-ngram-v1` available and generated independently of semantic configuration or service health.
- Add a small authenticated Go client that validates the exact planned service contract at startup and on every response.
- Store the semantic revision beside hash vectors under the fixed persistence key `semantic-minilm-l12-v2@e8f8c211226b894f`.
- Give video-published semantic work durable delayed retry semantics without duplicate database facts or unbounded blocking.
- Expose bounded-cardinality request, generation, retry, coverage, and backlog metrics.
- Reuse the existing table and composite identity if migration tests confirm the fixed key and 384-component JSON payload fit.
- Establish stable producer-side contracts that the future `backfill-semantic-video-embeddings` change can consume without coupling this live-event integration to a backfill command or job.

**Non-Goals:**

- Reading semantic vectors from recommendation recall, ranking, profiles, or request paths.
- Adding pgvector columns, vector indexes, ANN queries, similarity providers, or recommendation policy fields.
- Dynamic model selection, online model downloads, model training, fine-tuning, or GPU inference.
- Removing, disabling, or replacing the hash n-gram fallback.
- Changing the semantic service contract defined by `add-semantic-embedding-service`.
- Scanning or embedding existing historical videos, including any backfill command/job, cursor, checkpoint, dry-run, re-embedding control, or backfill-specific retry policy.

## Decisions

### 1. Preserve the local domain vectorizer and add a separate fallible semantic port

`domainembedding.Vectorizer` remains the deterministic, synchronous interface used by `HashNgramVectorizer`. Remote inference needs context cancellation, batching, authentication, and errors, so it will not be forced into that interface.

The application layer will instead define a narrow `SemanticGenerator` port with fixed metadata and bounded batch generation methods. Infrastructure will implement it with HTTP. The application embedding coordinator will own both:

1. the existing hash `Vectorizer`, which is mandatory;
2. an optional semantic generator, enabled only by validated configuration.

The domain package will own the fixed persistence model key, expected dimension, canonical title/description normalization and text hashing, finite/unit-vector validation, and immutable embedding construction. The persistence key is intentionally shorter than the existing `model VARCHAR(64)` while mapping one-to-one to the full model and immutable revision.

Alternative considered: replace `Vectorizer` with the HTTP client. Rejected because a remote, fallible batch API has different semantics and would make the always-available fallback depend on network behavior.

### 2. Use a strictly bounded authenticated HTTP client

When semantic generation is enabled, local configuration validation requires:

- an absolute `http` or `https` base URL with no userinfo, query, fragment, or path outside the service root;
- `internal.enabled=true` and the existing validated strong `internal.token`;
- metadata timeout from 500 milliseconds to 5 seconds, default 3 seconds;
- embedding request timeout from 1 to 20 seconds, default 17 seconds;
- coverage sampling interval from 1 minute to 1 hour, default 5 minutes.

Connection and payload bounds are fixed rather than operator-expandable: at most two connections and two in-flight requests per host, 1-second dial/TLS timeouts, no automatic client retry, disabled compression, 16 KiB metadata responses, 1 MiB embedding responses, and batches of at most 32. The request timeout is slightly longer than the service's 15-second deadline so its bounded timeout response can arrive.

The client sends only `X-Internal-Token`, `Content-Type: application/json`, and bounded items using IDs `video:<id>`. It never logs tokens, request bodies, title/description text, vectors, full response bodies, or raw transport errors. Errors are mapped to bounded classes: `canceled`, `timeout`, `over_capacity`, `auth`, `unavailable`, `contract`, and `internal`.

At worker startup, one metadata probe runs with the metadata deadline. Local invalid configuration fails worker startup. Remote unavailability, authentication rejection, or metadata mismatch does not stop the worker, because doing so would also remove hash generation. Instead, the semantic gate remains closed and a single background validator retries after 5 seconds, 30 seconds, 2 minutes, then every 5 minutes. Events arriving while the gate is closed follow the durable semantic retry path.

Metadata validation requires exact model, revision, dimension 384, `float32`, normalized true, CPU device, and the planned limits. Every embedding response again verifies model, revision, dimension, item count, exact IDs, zero-based indexes, request order, exactly 384 values, finiteness, and unit norm within `1e-4`. Valid vectors are L2-normalized once more in Go before bounded JSON serialization.

Alternative considered: fail the worker when the startup probe fails. Rejected because semantic inference is an optional enhancement and must not suppress hash coverage.

### 3. Make hash persistence the first and independent event step

For every decoded video-published event, application processing is ordered:

1. Canonicalize title and description according to the semantic service's NFKC, whitespace, and `title + "\n" + description` contract and compute the text hash.
2. Find `hash-ngram-v1`; if the same text hash already exists, record `skipped`, otherwise generate and conditionally upsert it.
3. If hash persistence fails, do not call the semantic service; retry the event.
4. If semantic generation is disabled, acknowledge after hash success.
5. If the fixed semantic model already exists with the same canonical text hash, record `skipped` and acknowledge.
6. Otherwise call the semantic generator and conditionally upsert the returned vector under the fixed semantic model key.

Repository writes use `(video_id, model)` conflict handling and only update vector fields when the stored text hash or vector metadata differs. Concurrent duplicate deliveries may perform duplicate computation, but they cannot create duplicate facts or churn `updated_at` for an identical fact. A changed publication event may replace each model's row for the new canonical text.

Alternative considered: attempt semantic generation before hash persistence. Rejected because a semantic outage would delay the fallback that current recommendation code can already use.

### 4. Use dedicated durable delayed retries with exact acknowledgement rules

The embedding queue gets its own supervised RabbitMQ connection/channel and `prefetch=1`; fanout, action, view-event, and media consumers do not share its channel or capacity. The existing primary queue remains the only queue bound to the public `video.published` routing key for embedding work.

Five durable retry queues are declared for 5 seconds, 30 seconds, 2 minutes, 10 minutes, and 30 minutes. They are addressed through RabbitMQ's default exchange by queue name and dead-letter only to the primary embedding queue through the default exchange, so retries cannot republish to the fanout queue. An `x-frux-embedding-attempt` integer header selects delays:

| Failed attempt | Delay |
| --- | --- |
| 1 | 5 seconds |
| 2 | 30 seconds |
| 3 | 2 minutes |
| 4 | 10 minutes |
| 5 and later | 30 minutes |

The 30-minute tier repeats without a terminal attempt limit while semantic integration remains enabled. This retains retryability through long outages without a hot loop. Retry count is never used as a metrics label.

Acknowledgement behavior is exact:

| Condition | Broker action |
| --- | --- |
| Invalid JSON/event envelope | `Nack(requeue=false)` |
| Context canceled because the worker is shutting down | leave unacknowledged and close the channel for broker redelivery |
| Hash save succeeds and semantic is disabled, already current, or saved | `Ack` |
| Hash persistence or semantic processing returns a retryable/configuration/contract failure | publish the original body and incremented attempt header to the selected retry queue; `Ack` only after publisher confirmation |
| Retry publication or confirmation fails | `Nack(requeue=true)`, close only the dedicated embedding channel, and reconnect after supervised delays of 1, 2, 4, 8, 16, then 30 seconds |

A crash after confirmed retry publication but before acknowledgement can duplicate delivery; conditional upsert and same-hash skips make that harmless. A crash after hash persistence but before semantic completion causes redelivery to skip hash and retry semantic. No remote call can hold an embedding delivery longer than the fixed 17-second client deadline, and no embedding failure closes unrelated consumer channels.

Alternative considered: immediate `Nack(requeue=true)`. Rejected because RabbitMQ can redeliver immediately and spin during a service outage.

Alternative considered: one per-message-TTL retry queue. Rejected because mixed expiration values can create head-of-line delay; fixed-delay queues provide predictable ordering without requiring the delayed-message plugin.

### 5. Reuse `video_embedding` without a schema change

The existing columns already contain all required facts:

- `video_id`;
- revision-bearing `model`;
- `dimension`;
- normalized vector JSONB;
- canonical `text_hash`;
- timestamps.

The existing composite primary/unique identity allows `hash-ngram-v1` and the semantic model row to coexist. No pgvector type, new vector column, or ANN index is added. Migration work is limited to assertions that:

- the semantic key fits the 64-character model column;
- a 384-component finite normalized JSON vector round-trips;
- one video can store both model rows;
- repeated same-model writes remain one fact.

If those assertions fail during implementation, the change must be revised rather than silently introducing an unrelated vector schema.

### 6. Define the future backfill dependency and consumer boundary

This change owns the producer-side primitives needed by live event processing:

- the immutable semantic model identity and canonical text contract;
- bounded metadata and embedding client behavior;
- domain vector validation and normalization;
- same-text lookup and conditional `(video_id, model)` persistence;
- semantic coverage counts and bounded metrics.

The future `backfill-semantic-video-embeddings` change will depend on this change and `add-semantic-embedding-service`. It may consume those stable primitives, but it will own every historical concern: eligible-video scans, batching policy, cursors, checkpoints, cancellation summaries, dry-run, re-embedding safeguards, backfill-specific retries, command/job composition, and operator documentation.

There is no reverse dependency. This integration does not wait for, invoke, schedule, or expose a backfill component. Existing historical videos without the fixed semantic row remain untouched unless they later emit the normal video-published event handled here.

Alternative considered: retain the command in this change. Rejected because live delivery correctness and historical catalog migration have different operational boundaries, failure modes, rollout controls, and test matrices.

### 7. Add bounded operational metrics

The worker registers:

- `frux_semantic_embedding_client_requests_total{operation,result}`;
- `frux_semantic_embedding_client_request_duration_seconds{operation,result}`;
- `frux_video_embedding_vectors_total{model,source,outcome}`;
- `frux_video_embedding_coverage_videos{model,state}`;
- `frux_video_embedding_backlog_messages{state}`.

Allowed values are fixed:

- `operation`: `metadata`, `embed`;
- client `result`: `success`, `canceled`, `timeout`, `over_capacity`, `auth`, `unavailable`, `contract`, `internal`;
- `model`: `hash`, `semantic`;
- `source`: `event`;
- vector `outcome`: `generated`, `skipped`, `retried`, `failed`;
- coverage `state`: `present`, `missing`;
- backlog `state`: `ready`, `retry`, `inflight`.

Every five minutes by default, the worker counts readable published videos with and without the fixed semantic model. Queue inspection sums ready messages in the primary and five retry queues; a local atomic gauge supplies in-flight work. Database IDs, titles, URLs, raw errors, attempts, and arbitrary model strings are never labels. A failed coverage/backlog sample preserves the previous gauge and increments the existing bounded worker-job error observation.

### 8. Compose enables integration without making hash startup depend on readiness

The local sample configuration keeps semantic generation disabled by default. Compose configuration enables it, uses `http://semantic-embedding:8081`, and reuses `FRUX_INTERNAL_TOKEN`. The worker declares `semantic-embedding` with `condition: service_started`, not `service_healthy`: this establishes startup ordering and service discovery while still allowing the worker to start, validate for at most 3 seconds, and provide hash coverage if model preload later fails.

The worker has no host access to a browser route and the semantic service remains internal-only as defined by `add-semantic-embedding-service`. Configuration documentation states that enabling semantic integration requires the service capability to be implemented and deployed, but stopping that container must not stop unrelated worker consumers.

### 9. Verify at unit, contract, persistence, RabbitMQ, and Compose levels

Tests will cover:

- canonical text equivalence, model constants, dimension, finite/unit checks, JSON bounds, and conditional same-hash persistence;
- URL/token/timeout validation and all metadata/embedding response failures using `httptest`;
- exact ID/order/index validation, truncated/oversized bodies, non-finite numbers, wrong dimensions, wrong model/revision, overload, cancellation, and safe error/log behavior;
- hash-first worker behavior, disabled semantic behavior, duplicate events, content changes, semantic success, every retry class, publisher-confirm failure, shutdown, and acknowledgement decisions;
- RabbitMQ retry topology, delay selection, default-exchange return to only the embedding queue, dedicated channel/QoS, reconnect backoff, and backlog inspection;
- PostgreSQL coexistence and idempotency with 128-dimensional hash and 384-dimensional semantic rows;
- a live contract run against the service produced by `add-semantic-embedding-service`;
- Compose rendering and a targeted outage test proving the worker writes hash vectors while the semantic container is unavailable, then eventually fills semantic coverage for the live event after recovery;
- scope tests or assertions proving no historical scan, command/job entry point, cursor/checkpoint behavior, dry-run, re-embedding mode, or backfill-specific retry loop is introduced.

## Risks / Trade-offs

- [The dependent semantic service change is still active and may not yet be implemented] → Treat its accepted spec as the contract, include a live cross-service contract gate, and do not mark integration implementation complete until that service is available.
- [An indefinite semantic outage accumulates durable retries] → Use capped 30-minute retry spacing, dedicated queues/channel, explicit backlog and missing-coverage gauges, and the ability to disable integration while retaining hash generation.
- [Disabling semantic integration acknowledges subsequently delivered retry messages] → Document that disablement intentionally stops live-event retries; resulting gaps remain visible in coverage metrics and are not repaired by this change.
- [A service contract bug can retain poison work indefinitely] → Classify and expose `contract` failures, close the semantic gate until metadata validates again, cap retry frequency, and never block hash writes or unrelated consumers.
- [Worker and Python normalization could drift] → Share committed contract fixtures covering NFKC, Unicode whitespace, empty descriptions, and title/description composition; fail semantic integration tests on hash/input drift.
- [Historical semantic coverage remains incomplete] → Make missing coverage explicit in metrics and documentation; defer remediation to `backfill-semantic-video-embeddings` rather than hiding migration work inside the live worker.
- [JSONB vectors are not efficient for ANN retrieval] → Accept the current storage because this change only generates durable facts; `add-pgvector-recommendation-recall` owns future vector indexing and consumption.

## Migration Plan

1. Complete and validate `add-semantic-embedding-service`, including its fixed model metadata and Compose service.
2. Add domain constants/validation, application ports and orchestration, bounded HTTP client, repository methods, metrics, and configuration with semantic disabled by default.
3. Add dedicated embedding consumer/retry topology and deploy it with semantic still disabled; verify existing hash event behavior and no queue regression.
4. Run migration/persistence tests and confirm no schema DDL is needed.
5. Wire Compose to the internal service with `service_started`, enable semantic integration, and verify startup metadata validation plus hash behavior during a forced semantic outage.
6. Monitor live-event request results, retry backlog, and semantic coverage. Existing historical videos remain unprocessed until the future `backfill-semantic-video-embeddings` change is separately proposed and implemented.

Rollback disables semantic generation. Hash processing continues. Existing semantic rows remain inert, versioned facts and need not be deleted. Retry queues may be drained/acknowledged with integration disabled or retained until re-enable according to the documented operator procedure. No database rollback or recommendation rollback is required.

## Open Questions

None. The dependent service contract, persistence model key, timeout bounds, acknowledgement matrix, live-event retry schedule, Compose dependency semantics, future backfill boundary, and recommendation deferral are fixed by this proposal.
