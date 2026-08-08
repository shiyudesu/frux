## 1. Behavior Topic Contracts

- [ ] 1.1 Register `frux.interaction.action-changed.v1` with its canonical action-state key, retention, producer, and active/shadow groups.
- [ ] 1.2 Register `frux.exposure.view-event-recorded.v1` with its user key, retention, producer, and active/shadow groups.
- [ ] 1.3 Add strict envelope codecs and stable key fixtures for both existing Application event payloads.
- [ ] 1.4 Add contract tests for malformed IDs, unsupported versions, key/payload mismatch, size limits, and timestamp bounds.

## 2. Action Event Production

- [ ] 2.1 Add a transport-aware action publisher that supports Rabbit primary, Rabbit plus Kafka mirror, Kafka plus Rabbit mirror, and Kafka primary modes.
- [ ] 2.2 Preserve Redis-assigned versions and publish Kafka action records with acknowledged idempotent production.
- [ ] 2.3 Preserve synchronous `PersistAcceptedActionEvent` fallback for failed or uncertain Kafka production.
- [ ] 2.4 Preserve conditional Redis rollback when Kafka production and PostgreSQL fallback both fail.
- [ ] 2.5 Add tests for Kafka success, uncertain acknowledgement, fallback success, fallback failure, superseding versions, and mirror failure.

## 3. View Event Outbox Publication

- [ ] 3.1 Extend the view-event outbox dispatcher to select registered primary and optional mirror transports.
- [ ] 3.2 Mark outbox rows dispatched only after the primary transport acknowledges publication and observe mirror results separately.
- [ ] 3.3 Preserve accepted HTTP behavior and retry leases when Kafka is unavailable.
- [ ] 3.4 Add outbox tests for Kafka cutover, restart, duplicate dispatch, mirror gaps, and pending-row recovery.

## 4. Active Kafka Consumers

- [ ] 4.1 Wire the accepted-action worker to `frux.interaction.persist-action.v1`.
- [ ] 4.2 Commit action offsets only after the receipt, winning-version state, aggregates, and downstream outboxes commit.
- [ ] 4.3 Wire the recommendation behavior worker to `frux.recommendation.consume-view.v1`.
- [ ] 4.4 Commit view offsets after the durable behavior fact and profile/outcome handoff commit, without waiting for later projection dependencies.
- [ ] 4.5 Add consumer tests for duplicate delivery, payload conflicts, older versions, terminal videos, behavior duplicates, commit failure, and reassignment.

## 5. Shadow Validation and Cutover

- [ ] 5.1 Implement distinct non-mutating shadow groups for action and view events.
- [ ] 5.2 Compare shadow records with available durable receipts/facts using bounded parity results.
- [ ] 5.3 Add per-stream cutover configuration and startup validation that prevents RabbitMQ and Kafka dual-active mutation.
- [ ] 5.4 Document and test the cutover order: view events first, action events second, with independent rollback.
- [ ] 5.5 Add integration coverage for records produced around the cutover boundary and prove stable IDs absorb duplicates.

## 6. Metrics and Documentation

- [ ] 6.1 Add bounded primary/mirror publication, fallback, shadow parity, duplicate, supersession, lag, and delivery-age metrics.
- [ ] 6.2 Update interaction, exposure, recommendation, architecture, engineering, optimization, and monitoring documentation.
- [ ] 6.3 Update current OpenSpec descriptions that still identify RabbitMQ as the behavior delivery mechanism.

## 7. Validation

- [ ] 7.1 Run targeted interaction, exposure, recommendation, Kafka adapter, and API-flow tests.
- [ ] 7.2 Run forced Kafka outage tests proving view HTTP acceptance and action PostgreSQL fallback/Redis rollback behavior.
- [ ] 7.3 Run full Go tests, both Go builds, Compose configuration validation, and strict OpenSpec validation.
