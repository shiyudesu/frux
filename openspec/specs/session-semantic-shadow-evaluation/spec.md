# Session Semantic Shadow Evaluation Specification

## Purpose

Define deterministic, bounded, privacy-safe Session Semantic Shadow execution and low-volume replay
that remain strictly isolated from active recommendation delivery and make no request-time model call.

## Requirements

### Requirement: Shadow selection is deterministic and disabled by default
Frux SHALL select only authenticated first-page Recommendation requests through the versioned
`session-semantic-shadow-sampler-v1` contract, and checked-in configuration SHALL disable selection
with both `enabled=false` and `sample_ppm=0`.

#### Scenario: Same request reaches another replica
- **WHEN** the normalized scene, user ID, and canonical request ID are identical
- **THEN** every replica produces the same parts-per-million Shadow selection result

#### Scenario: Sampling is disabled
- **WHEN** Shadow is disabled or `sample_ppm` is zero
- **THEN** no Shadow goroutine, Session Semantic build, Exact query, or Shadow metric beyond bounded skip accounting is created

#### Scenario: Cursor page is served
- **WHEN** a request reads an existing Snapshot or supplies a subsequent-page cursor
- **THEN** it does not launch another Shadow evaluation for that recommendation ordering

### Requirement: Shadow admission and lifecycle are independently bounded
Shadow evaluation SHALL use an independent non-blocking no-queue admission limit acquired before
goroutine creation, a process-owned cancellable context, one bounded deadline, and bounded shutdown.
It MUST NOT consume normal recommendation Provider slots.

#### Scenario: Shadow capacity is full
- **WHEN** all Shadow permits are held
- **THEN** the selected request records bounded capacity rejection without creating a goroutine or calling the Provider

#### Scenario: Dependency ignores cancellation
- **WHEN** a Shadow Provider call continues after its deadline
- **THEN** its permit remains held until the actual call returns and no retry, queue, or replacement call is created

#### Scenario: API shuts down
- **WHEN** shutdown begins with admitted Shadow work in flight
- **THEN** new work is rejected, the lifecycle context is cancelled, and shutdown waits only until the configured bounded deadline

### Requirement: Shadow reuses existing Session Semantic Exact behavior without model calls
Admitted Shadow work SHALL reuse the registered active-contract Session Semantic builder and Provider,
trusted bounded context, existing video vector facts/projections, and PostgreSQL Exact retrieval. It
MUST NOT invoke query embedding, video embedding, Tongyi, or another external model.

#### Scenario: Compatible trusted session evidence exists
- **WHEN** the copied request context resolves to sufficient active-contract signals and vectors
- **THEN** Shadow executes at most one bounded Exact query and obtains the same deterministic semantic candidate prefix as the registered Provider contract

#### Scenario: Semantic evidence is unavailable
- **WHEN** trusted signals, compatible vectors, confidence, or Exact retrieval are unavailable
- **THEN** Shadow records a closed empty, timeout, capacity, or error outcome without changing the active request

#### Scenario: External Adapter is offline
- **WHEN** active-contract vector facts already exist but the multimodal Adapter is unavailable
- **THEN** Shadow remains able to execute and records zero external model calls

### Requirement: Shadow comparison and simulation are bounded and deterministic
Frux SHALL compare the bounded semantic prefix with a copied completed active ordering, calculate
explicit overlap and unique-contribution denominators, reconstruct bounded Provider sequences from
retained Recall Reasons, and run the registered `shadow-mix-v1` reservation and ranking simulation in
memory only.

#### Scenario: Semantic candidates overlap the active ordering
- **WHEN** a semantic candidate already exists in the copied active ordering
- **THEN** it contributes to intersection/overlap metrics and does not count as a unique semantic candidate

#### Scenario: Semantic candidate is unique and readable
- **WHEN** a semantic candidate is absent from the active ordering and passes current readability validation
- **THEN** it may enter only the simulated pool and its pool/top-K survival is counted with explicit denominators

#### Scenario: Identical copied input is replayed
- **WHEN** active candidates, semantic candidates, policy, configuration, and evaluation time are identical
- **THEN** overlap, contribution, reservation survival, rank survival, displacement, and diversity outputs are identical

### Requirement: Shadow observations are aggregate and privacy-safe
Runtime Shadow observations SHALL contain only registered enums, bounded counts, finite ratios,
confidence bands, and durations. Metrics and normal logs MUST NOT contain user, request, session,
video, candidate, vector, model, contract, query, SQL, or raw-error values as labels or normal fields.

#### Scenario: Shadow succeeds
- **WHEN** an admitted evaluation completes comparison and simulation
- **THEN** fixed-label metrics record selection, terminal result, confidence band, candidate counts, overlap, unique contribution, survival, displacement, diversity, and duration

#### Scenario: Shadow fails
- **WHEN** building, Exact retrieval, validation, comparison, ranking, or lifecycle work fails
- **THEN** one closed terminal result is recorded without persisting candidates or exposing raw error text

#### Scenario: Runtime is inspected
- **WHEN** PostgreSQL, request logs, Snapshots, delivery evidence, and normal application logs are inspected
- **THEN** they contain no Shadow-specific per-request row, candidate list, vector, or raw context

### Requirement: Low-volume replay and reports are deterministic and non-causal
Frux SHALL provide a bounded standalone replay command that reads only versioned local fixtures,
performs no runtime or model access, and atomically writes permission-restricted canonical JSON and
Markdown aggregate reports.

#### Scenario: Valid fixtures are evaluated twice
- **WHEN** identical versioned inputs and options are evaluated to the same output paths
- **THEN** both JSON and Markdown report bytes are identical and declare `external_model_calls: 0`

#### Scenario: Evidence is insufficient
- **WHEN** case count, relevance labels, or required denominators are below the registered gate
- **THEN** the report result is `inconclusive` and does not recommend or activate a policy

#### Scenario: Fixture attempts runtime access
- **WHEN** input or options request PostgreSQL, Redis, Kafka, HTTP, S3, credentials, or a model provider
- **THEN** validation fails before evaluation and no partial report is emitted
