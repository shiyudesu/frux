## 1. Behavior Topic Contracts

- [x] 1.1 Register `frux.interaction.action-changed.v1` with its canonical action-state key, retention, producer, and active/shadow groups.
- [x] 1.2 Register `frux.exposure.view-event-recorded.v1` with its user key, retention, producer, and active/shadow groups.
- [x] 1.3 Add strict envelope codecs and stable key fixtures for both existing Application event payloads.
- [x] 1.4 Add contract tests for malformed IDs, unsupported versions, key/payload mismatch, size limits, and timestamp bounds.

## 2. Action Event Production

- [x] 2.1 Add a transport-aware action publisher that supports Rabbit primary, Rabbit plus Kafka mirror, Kafka plus Rabbit mirror, and Kafka primary modes.
- [x] 2.2 Preserve Redis-assigned versions and publish Kafka action records with acknowledged idempotent production.
- [x] 2.3 Preserve synchronous `PersistAcceptedActionEvent` fallback for failed or uncertain Kafka production.
- [x] 2.4 Preserve conditional Redis rollback when Kafka production and PostgreSQL fallback both fail.
- [x] 2.5 Add tests for Kafka success, uncertain acknowledgement, fallback success, fallback failure, superseding versions, and mirror failure.

## 3. View Event Outbox Publication

- [x] 3.1 Extend the view-event outbox dispatcher to select registered primary and optional mirror transports.
- [x] 3.2 Mark outbox rows dispatched only after every transport required by the selected single/dual mode acknowledges publication.
- [x] 3.3 Preserve accepted HTTP behavior and retry leases when Kafka is unavailable.
- [x] 3.4 Add outbox tests for Kafka cutover, restart, duplicate dispatch, partial dual acknowledgement, and pending-row recovery.

## 4. Active Kafka Consumers

- [x] 4.1 Wire the accepted-action worker to `frux.interaction.persist-action.v1`.
- [x] 4.2 Commit action offsets only after the receipt, winning-version state, aggregates, and downstream outboxes commit.
- [x] 4.3 Wire the recommendation behavior worker to `frux.recommendation.consume-view.v1`.
- [x] 4.4 Commit view offsets after the durable behavior fact and profile/outcome handoff commit, without waiting for later projection dependencies.
- [x] 4.5 Add consumer tests for duplicate delivery, payload conflicts, older versions, terminal videos, behavior duplicates, commit failure, and reassignment.

## 5. Shadow Validation and Cutover

- [x] 5.1 Implement distinct non-mutating shadow groups for action and view events.
- [x] 5.2 Compare shadow records with available durable receipts/facts using bounded parity results.
- [x] 5.3 Add per-stream cutover configuration and startup validation that prevents RabbitMQ and Kafka dual-active mutation.
- [x] 5.4 Document and test the cutover order: view events first, action events second, with independent rollback.
- [x] 5.5 Add integration coverage for records produced around the cutover boundary and prove stable IDs absorb duplicates.

## 6. Metrics and Documentation

- [x] 6.1 Add bounded primary/mirror publication, fallback, shadow parity, duplicate, supersession, lag, and delivery-age metrics.
- [x] 6.2 Update interaction, exposure, recommendation, architecture, engineering, optimization, and monitoring documentation.
- [x] 6.3 Update current OpenSpec descriptions that still identify RabbitMQ as the behavior delivery mechanism.

## 7. Validation

- [x] 7.1 Run targeted interaction, exposure, recommendation, Kafka adapter, and API-flow tests.
- [x] 7.2 Run forced Kafka outage tests proving view HTTP acceptance and action PostgreSQL fallback/Redis rollback behavior.
- [x] 7.3 Run full Go tests, both Go builds, Compose configuration validation, and strict OpenSpec validation.

## 8. Code Review Remediation

- [x] 8.1 Require the active consumer transport acknowledgement for action and view migration pairs.
- [x] 8.2 Apply active Kafka cutover timestamps as durable one-time group offsets without rewinding initialized groups.
- [x] 8.3 Retry missing shadow parity facts with bounded delay while preserving true mismatch classification.
- [x] 8.4 Reject non-canonical action and user key aliases by exact decode/re-encode equality.
- [x] 8.5 Expose consumer supervisor session health and fail visibly on non-retryable active-group initialization/runtime errors.
- [x] 8.6 Update Kafka migration documentation and complete the required validation matrix.

## 9. Final Review Remediation

- [x] 9.1 Require both RabbitMQ and Kafka acknowledgements in dual transition publisher modes, preserving action fallback/rollback and pending view outbox recovery.
- [x] 9.2 Require and validate broker `LogAppendTime` topology for retained event topics and prevent producer clocks from defining timestamp cutover boundaries.
- [x] 9.3 Require the action cutover boundary to be strictly after the view boundary and start view Kafka active/shadow groups before action groups.
- [x] 9.4 Synchronize metrics, OpenSpec artifacts, and operational/module documentation with the corrected semantics.
- [x] 9.5 Run targeted and full validation, Compose config, strict OpenSpec validation, and final diff review.
