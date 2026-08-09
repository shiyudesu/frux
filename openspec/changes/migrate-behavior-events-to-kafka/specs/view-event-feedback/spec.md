## MODIFIED Requirements

### Requirement: Reliable Recommendation Event Publication
Accepted playback behavior events SHALL be published from the transactional PostgreSQL outbox to a registered Kafka event stream through retryable, idempotent delivery that does not block the user-facing playback request on Kafka availability.

#### Scenario: Kafka is temporarily unavailable
- **WHEN** a view event is committed while Kafka publication cannot be acknowledged
- **THEN** the event remains pending in the PostgreSQL outbox for retry and the accepted HTTP result is not lost

#### Scenario: One view transition transport fails
- **WHEN** either RabbitMQ or Kafka does not acknowledge a view event in a dual transition mode
- **THEN** the structured publication failure records the acknowledged side, the outbox row remains pending, and stable event IDs absorb retry duplicates

#### Scenario: Kafka consumer receives a duplicate delivery
- **WHEN** the same playback event is delivered more than once because an offset was not committed or a consumer group replays it
- **THEN** the recommendation behavior fact and downstream projection handoff apply the event at most once by event ID

#### Scenario: Kafka delivery is durably handed off
- **WHEN** the recommendation consumer commits the raw behavior fact and its leased profile/outcome outbox
- **THEN** it may commit the Kafka offset without waiting for embedding generation or later projection work
