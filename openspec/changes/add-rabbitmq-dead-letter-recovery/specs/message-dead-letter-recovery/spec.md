## ADDED Requirements

### Requirement: Bounded Consumer Delivery
Each protected RabbitMQ consumer SHALL classify failures and SHALL stop normal redelivery after its configured delivery limit.

#### Scenario: Retryable infrastructure error repeats
- **WHEN** a message repeatedly fails with a classified retryable infrastructure error
- **THEN** RabbitMQ redelivers it only up to the configured limit and then routes it to the consumer's dead-letter queue

#### Scenario: Terminal payload error occurs
- **WHEN** a message is malformed, unsupported, or references a terminal domain state
- **THEN** the consumer rejects it without an unbounded requeue loop

### Requirement: Durable Dead-Letter Topology
Critical durable workflows SHALL use versioned quorum source queues, configured dead-letter exchanges, bounded queue lengths, and at-least-once dead-lettering where loss would violate business correctness.

#### Scenario: Dead-letter target is temporarily unavailable
- **WHEN** a critical quorum source queue cannot confirm the dead-letter target
- **THEN** the source retains the message for later dead-letter delivery rather than silently discarding it

#### Scenario: Queue type migration is deployed
- **WHEN** an existing classic queue moves to a quorum topology
- **THEN** Frux uses a new queue name and controlled binding cutover instead of redeclaring the existing queue with another type

### Requirement: Idempotent Redelivery and Replay
Consumer redelivery and operator replay SHALL preserve the original business event ID so existing idempotency rules prevent duplicate business facts.

#### Scenario: Dead-lettered event was partially persisted
- **WHEN** an operator replays an event whose original ID was already durably applied
- **THEN** the consumer treats it as an idempotent duplicate and does not repeat the business mutation

### Requirement: Protected Dead-Letter Inspection
An operator with `governance.execute` SHALL be able to list dead-letter queue summaries and request a bounded head preview through a server-side broker adapter without receiving broker credentials.

#### Scenario: Unauthorized caller inspects a DLQ
- **WHEN** an authenticated principal lacks governance execution permission
- **THEN** Frux returns HTTP 403 and no payload or queue metadata

#### Scenario: Preview is requested
- **WHEN** an authorized operator requests a bounded preview
- **THEN** Frux returns redacted envelope metadata and bounded payload diagnostics while leaving the message dead-lettered

### Requirement: Confirmed Single-Message Replay
Operator replay SHALL validate the original route, republish one unchanged business payload with a new replay ID, wait for publisher confirmation, and acknowledge the DLQ message only after confirmation.

#### Scenario: Replay publish succeeds
- **WHEN** RabbitMQ confirms the republished message
- **THEN** Frux acknowledges the selected dead-letter message and commits a success audit fact containing the queue, original event ID, and replay ID

#### Scenario: Replay publish fails or times out
- **WHEN** the broker does not confirm the replay
- **THEN** the dead-letter message remains available and Frux records a failed replay outcome without reporting success

### Requirement: Dead-Letter Observability
Frux SHALL expose bounded metrics and alerts for retry exhaustion, dead-letter depth, dead-letter routing failure, replay attempts, and replay failures.

#### Scenario: Dead-letter backlog grows
- **WHEN** a configured queue remains above its backlog threshold for the alert window
- **THEN** Prometheus evaluates a firing alert without using event IDs or payload fields as labels
