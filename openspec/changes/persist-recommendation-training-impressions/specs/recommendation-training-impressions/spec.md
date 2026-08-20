## ADDED Requirements

### Requirement: Durable Delivered-Card Diagnostic Facts
Frux SHALL persist a compact diagnostic impression for 100% of final hydrated and readable recommendation cards actually delivered by Feed, without asserting that the retained population is currently suitable for training.

#### Scenario: Ranked card is delivered
- **WHEN** Feed completes card hydration and readability filtering for a recommendation page
- **THEN** each returned card produces a durable fact containing stable user, request, generation, and video identity, zero-based absolute rank position within the generation, trusted author ID and publication time, normalized scene, policy/version, degraded state/providers, served and recorded times, and explicit record and feature-schema versions

#### Scenario: Ranked candidate is not delivered
- **WHEN** a ranked or snapshotted candidate is missing a card, unreadable, suppressed, or otherwise omitted from the final Feed response
- **THEN** Frux does not create a diagnostic impression for that candidate and preserves any resulting gaps in absolute rank positions

#### Scenario: Later page is delivered
- **WHEN** a snapshot or deterministic degraded-cursor page returns candidates from later in the same ranked session
- **THEN** each fact retains its generation and position in the complete post-ranking and post-diversity ordering rather than restarting rank at the page boundary

#### Scenario: Ranking is recomputed
- **WHEN** Feed creates a new complete ordering rather than continuing the prior snapshot generation
- **THEN** Frux assigns a new immutable generation and positions remain unique and interpretable within that generation

### Requirement: Bounded Diagnostic Payload
Diagnostic impressions SHALL retain only enumerated, bounded server-derived delivery and ranking facts needed for diagnosis and possible future approved reuse.

#### Scenario: Candidate has ranking explanations
- **WHEN** a delivered candidate carries recall reasons and score components from recommendation ranking
- **THEN** the impression stores at most 8 normalized recall reasons of at most 64 characters, at most 8 finite supported score components, and bounded normalized degraded metadata under versioned schemas

#### Scenario: Delivery is recorded
- **WHEN** Frux creates the durable handoff
- **THEN** it excludes request context, session identifiers other than the bounded recommendation request ID, device metadata, client tokens, signed media URLs, arbitrary client metadata, embeddings, and outcome payloads

#### Scenario: Client submits recommendation data
- **WHEN** a client supplies Feed context, feedback, exposure, or playback fields
- **THEN** those fields cannot directly create or alter a diagnostic impression because impression contents come only from server ranking state and final Feed assembly

### Requirement: Atomic Trusted Handoff
Frux SHALL commit the short-lived served-candidate evidence and a durable training-impression outbox item in the same database transaction for each newly delivered card.

#### Scenario: Delivery transaction succeeds
- **WHEN** the trusted delivery recorder appends new served-candidate evidence
- **THEN** matching outbox items referencing those persisted evidence rows are committed atomically before Feed reports success

#### Scenario: Delivery transaction fails
- **WHEN** either the served-candidate evidence write or training-impression handoff cannot commit
- **THEN** neither partial state is committed and the recommendation page is not reported as successfully delivered

#### Scenario: Worker is unavailable
- **WHEN** the outbox transaction commits but the training-impression worker is delayed or offline
- **THEN** the Feed response remains valid and the durable pending item is available for later replay

### Requirement: Idempotent Leased Persistence
A bounded leased worker SHALL persist diagnostic impressions idempotently from the durable handoff.

#### Scenario: Worker processes a pending item
- **WHEN** an available outbox item is claimed
- **THEN** the worker inserts one training fact keyed by the stable source served-candidate row and marks the handoff dispatched

#### Scenario: Worker or process restarts
- **WHEN** a lease expires before an item is marked dispatched
- **THEN** another worker can reclaim it and complete persistence without producing a duplicate fact

#### Scenario: Persistence is retried after a fact commit
- **WHEN** the same source item is replayed after its training fact already exists
- **THEN** the worker treats the existing identical fact as success and completes the handoff idempotently

#### Scenario: Persistence repeatedly fails
- **WHEN** a claimed item cannot be persisted
- **THEN** Frux retains it, clears or expires the lease, records a bounded error, and retries with capped backoff without allowing one item to make a worker run unbounded

### Requirement: Delivery, Exposure, and Security Separation
Diagnostic impressions SHALL NOT replace or extend served-candidate evidence for feedback or outcome authorization.

#### Scenario: Card is delivered without exposure
- **WHEN** Feed returns a card but no validated exposure outcome exists
- **THEN** the diagnostic fact records delivery only and any future consumer MUST treat the card as unobserved rather than as a negative example

#### Scenario: Short-lived evidence expires
- **WHEN** a diagnostic impression remains after its `recommendation_served_candidate` evidence expires
- **THEN** feedback and outcome attribution continue to fail unless the existing served-candidate validity checks independently succeed

#### Scenario: Training row is present
- **WHEN** an attribution request matches a user, request, and video stored in the training table
- **THEN** Frux still validates only against `recommendation_served_candidate` and its unchanged `served_at <= recorded_at < expires_at` interval

#### Scenario: Training persistence is delayed
- **WHEN** the training worker has not yet materialized a fact
- **THEN** existing feedback and outcome attribution behavior is unchanged

### Requirement: Retention and Cleanup
Frux SHALL retain diagnostic impressions longer than security evidence under an independently configured bounded retention policy and SHALL clean facts and completed handoffs in bounded batches.

#### Scenario: Impression reaches retention cutoff
- **WHEN** a diagnostic impression's trusted served time is older than the configured retention period
- **THEN** a cleanup worker deletes it in bounded ordered batches without modifying outcomes, behavior events, request logs, or served-candidate evidence

#### Scenario: Handoff is complete
- **WHEN** an outbox item has been dispatched beyond the configured operational replay period
- **THEN** cleanup may delete the completed handoff while retaining the training fact until its own retention cutoff

#### Scenario: Handoff is still pending
- **WHEN** an outbox item is undispatched regardless of age
- **THEN** routine cleanup retains it for recovery and exposes the backlog through metrics

### Requirement: Privacy Deletion and Training Opt-Out
Frux SHALL provide bounded, auditable privacy handling for account deletion and SHALL define a durable exclusion boundary for any future training use.

#### Scenario: Account deletion is accepted
- **WHEN** a user deletion request reaches the recommendation privacy boundary
- **THEN** Frux idempotently deletes or schedules deletion of that user's pending handoffs and materialized diagnostic facts and reconciles until no account-linked row remains

#### Scenario: User opts out of training
- **WHEN** a user has an effective training opt-out
- **THEN** operational diagnostic recording may continue within its approved retention, but every future export or learner MUST exclude the user's facts using a captured privacy watermark

#### Scenario: Privacy state changes after delivery
- **WHEN** deletion or opt-out becomes effective after an impression was recorded
- **THEN** the later privacy state supersedes row presence and prevents future training eligibility

### Requirement: Unified Identity and Time Contract
Frux SHALL expose one immutable identity and time contract to all future diagnostic consumers.

#### Scenario: Fact identity is read
- **WHEN** a downstream consumer identifies an impression
- **THEN** it uses `(user_id, request_id, generation, video_id)` with zero-based generation-relative absolute position, while `source_served_candidate_id` remains the persistence idempotency key

#### Scenario: Fact times are interpreted
- **WHEN** delivery and later outcome facts are reconciled
- **THEN** `served_at`/`occurred_at` determine event ordering and behavioral windows, while `recorded_at` determines durable snapshot watermarks and late-arrival completeness

### Requirement: Operational Observability
Frux SHALL expose bounded operational metrics for diagnostic-impression handoff, persistence, lag, replay, failure, and cleanup.

#### Scenario: Worker handles a batch
- **WHEN** the worker persists, replays, retries, or rejects work
- **THEN** counters use only bounded result labels and gauges report pending count and oldest pending age without user, request, video, or error text labels

#### Scenario: Cleanup runs
- **WHEN** training facts or completed handoffs are deleted
- **THEN** Frux records bounded deletion counts and worker duration/success using the existing worker observability conventions

### Requirement: Rollout Acceptance Thresholds
Frux SHALL block broad rollout until compactness, Feed latency, backlog recovery, and reconciliation thresholds pass on representative load.

#### Scenario: Storage is measured
- **WHEN** representative impression fixtures and indexes are measured
- **THEN** p95 logical payload is at most 2 KiB and table-plus-index storage is at most 4 KiB per materialized fact

#### Scenario: Feed latency is compared
- **WHEN** the same release is load-tested with diagnostic handoff enabled and disabled
- **THEN** enabled Feed delivery p99 increases by no more than both 5 ms and 5%

#### Scenario: Backlog behavior is tested
- **WHEN** steady load and a simulated 10-minute worker outage are exercised
- **THEN** 99.99% of committed handoffs materialize within 5 minutes during steady load, oldest pending age stays below 15 minutes, and the outage backlog drains within 60 minutes

#### Scenario: Daily reconciliation runs
- **WHEN** committed delivered evidence is reconciled against pending handoffs and facts
- **THEN** 100% has exactly one recoverable handoff or fact within 24 hours and there are zero unexplained missing or duplicate downstream identities

### Requirement: Additive API-Compatible Migration
The capability SHALL use additive persistence changes without altering public Feed or feedback request and response schemas.

#### Scenario: Schema is migrated
- **WHEN** API or worker startup runs the shared migration
- **THEN** the training-impression and handoff models and required unique, claim, request-linkage, and retention indexes exist safely under concurrent migration

#### Scenario: Existing deployment is upgraded
- **WHEN** the new schema and workers are deployed
- **THEN** new deliveries become eligible for durable impressions without attempting to reconstruct historical impressions from sampled request logs or short-lived evidence

#### Scenario: Existing API client continues requests
- **WHEN** a client uses the current Feed, feedback, exposure, or playback contracts
- **THEN** request and response payloads remain compatible and no client-visible training endpoint is introduced
