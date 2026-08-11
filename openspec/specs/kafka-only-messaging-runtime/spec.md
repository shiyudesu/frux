# kafka-only-messaging-runtime Specification

## Purpose

Defines the final Kafka-only runtime, deployment, recovery compatibility, retirement gates, and
documentation requirements after the legacy broker is removed.

## Requirements

### Requirement: RabbitMQ Retirement Gates
Frux SHALL remove RabbitMQ only after every registered stream is Kafka-active through an observation window, RabbitMQ source queues are drained, RabbitMQ DLQ records have an audited disposition, and Kafka plus durable-job recovery signals satisfy documented thresholds.

#### Scenario: RabbitMQ queue still contains records
- **WHEN** any allowlisted source queue has ready or unacknowledged records
- **THEN** retirement is blocked

#### Scenario: RabbitMQ DLQ has unresolved records
- **WHEN** a dead-letter record has not been replayed, waived, or exported through the documented audited procedure
- **THEN** retirement is blocked

### Requirement: Kafka-Only Broker Runtime
The final Frux runtime SHALL contain no RabbitMQ publisher, consumer, topology, management client, AMQP dependency, credential, URL, migration mode, or startup requirement.

#### Scenario: API starts after retirement
- **WHEN** valid Kafka, PostgreSQL, Redis, and other required configuration is present without any AMQP configuration
- **THEN** the API starts and wires only Kafka or database-owned asynchronous capabilities

#### Scenario: Worker starts after retirement
- **WHEN** Kafka, PostgreSQL, Redis, and required worker dependencies are healthy
- **THEN** the Worker starts without connecting to RabbitMQ

### Requirement: Kafka Recovery API Replacement
Kafka topic/partition/offset inspection and replay SHALL be available before RabbitMQ queue-based dead-letter endpoints are removed.

#### Scenario: Client calls a retired RabbitMQ dead-letter endpoint
- **WHEN** the RabbitMQ recovery API has been removed
- **THEN** Frux does not emulate queue-head or destructive acknowledgement semantics and the client must use the documented Kafka recovery API

#### Scenario: Historical replay audit is queried
- **WHEN** an operator reads audit history created before RabbitMQ retirement
- **THEN** the immutable historical facts remain available even though the RabbitMQ endpoint and broker no longer exist

### Requirement: RabbitMQ-Free Deployment
Compose and supported deployment documentation SHALL provision Kafka as the only message broker and SHALL contain no RabbitMQ service, management port, volume, health dependency, credential, alert, or dashboard.

#### Scenario: Compose is rendered
- **WHEN** an operator validates the final Compose file
- **THEN** API and Worker dependencies reference Kafka and no RabbitMQ resource is present

### Requirement: Final Messaging Documentation
Current architecture, engineering, deployment, optimization, monitoring, and module documentation SHALL distinguish retained Kafka events, short-lived Kafka wakeup commands, and PostgreSQL durable jobs without describing RabbitMQ as an active component.

#### Scenario: Developer adds asynchronous work
- **WHEN** a developer consults current engineering documentation
- **THEN** the guidance directs replayable domain events to Kafka and long-running retry state to a durable PostgreSQL job rather than a RabbitMQ-style queue abstraction
