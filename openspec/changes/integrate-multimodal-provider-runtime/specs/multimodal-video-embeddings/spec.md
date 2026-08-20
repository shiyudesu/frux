## MODIFIED Requirements

### Requirement: Environment-neutral multimodal embedding contract
Frux SHALL access multimodal embeddings through an application-owned provider contract with one
bounded operation for public-video content and one bounded operation for normalized public search
query text. The two operations SHALL return finite L2-normalized vectors in the same immutable
model space, and no domain or application type SHALL expose a provider SDK, process model,
hardware, language, deployment topology, or transport-specific payload. When an inference-dependent
feature is enabled, its process SHALL validate the live runtime's complete contract and required
operation capability before accepting dependent work.

#### Scenario: Public video content is embedded
- **WHEN** an eligible job supplies normalized public title/description text and a bounded set of prepared cover or keyframe images
- **THEN** the provider returns exactly one validated vector and its complete immutable contract identity

#### Scenario: Search query text is embedded
- **WHEN** semantic public-video search supplies a valid normalized query under its configured deadline and admission bound
- **THEN** the provider returns one vector compatible with video vectors from the active contract

#### Scenario: Provider implementation changes
- **WHEN** operators replace a local, remote, or otherwise packaged provider implementation with another implementation of the same accepted contract
- **THEN** recommendation, search, video, and embedding domain/application interfaces require no provider-specific type or topology change

#### Scenario: Enabled runtime is incompatible
- **WHEN** startup readiness reports a different protocol, operation set, or immutable model contract
- **THEN** the dependent process fails startup before claiming video jobs or serving semantic queries

### Requirement: Durable newly published video embedding jobs
After the existing `hash-ngram-v1` publication intake is durably safe, Frux SHALL idempotently hand
eligible newly published videos to a PostgreSQL multimodal job with explicit `pending`, `leased`,
`retry`, `succeeded`, and `terminal` states, bounded attempts/backoff, database-time lease,
heartbeat/reclaim, fencing, manual requeue, and cleanup semantics. Kafka source progress SHALL NOT
wait for provider inference after durable handoff. When video-job execution is enabled, the Worker
SHALL construct the validated provider runtime and run the job executor under the process lifecycle.

#### Scenario: Newly published video is handed off
- **WHEN** the embedding publication consumer validates an eligible first-publication fact and the existing hash vector is safe
- **THEN** it creates or reuses the exact-contract multimodal job before allowing the source record to commit

#### Scenario: Provider is unavailable after handoff
- **WHEN** the job encounters a retryable timeout, admission rejection, rate limit, or provider failure
- **THEN** the job retains a bounded retry time while video publication, search fallback, Feed, and Kafka source progress remain available

#### Scenario: Worker loses its lease
- **WHEN** a Worker completes after its lease expired or was reclaimed
- **THEN** fencing prevents it from persisting a result or terminal transition over the current owner

#### Scenario: Development-era video predates the feature
- **WHEN** an existing readable video has no job or active multimodal vector because it predates this change
- **THEN** Frux treats semantic coverage as absent and does not require an automatic historical scan or backfill

#### Scenario: Enabled Worker starts its executor
- **WHEN** the Worker has video jobs enabled and completes the provider compatibility handshake
- **THEN** it claims and executes durable multimodal jobs using the configured media, lease, admission, deadline, retry, and shutdown bounds
