## 1. Event Contract and Persistence

- [x] 1.1 Extend exposure domain event types and validation with `progress`, event identity, playback session, sequence, occurrence time, position, and duration fields.
- [x] 1.2 Add PostgreSQL migration fields, unique event identity constraints, history progress fields, and the view-event outbox table.
- [x] 1.3 Backfill legacy view events and history rows with deterministic event identity and occurrence metadata.
- [x] 1.4 Update exposure repository transactions to deduplicate events and atomically maintain exposure, history, and outbox records.
- [x] 1.5 Add idempotency-conflict and monotonic-history repository tests.

## 2. API and Event Delivery

- [x] 2.1 Extend view-event HTTP DTOs and error mapping while preserving legacy request compatibility.
- [x] 2.2 Implement bounded occurrence-time, sequence, duration, and payload validation.
- [x] 2.3 Add replay responses for identical event IDs and 409 handling for mismatched reuse.
- [x] 2.4 Implement an outbox dispatcher with RabbitMQ publisher confirmation, retry, lease, and deduplication metadata.
- [x] 2.5 Wire the dispatcher into the worker process and expose outbox lag/error metrics.

## 3. Web Playback Lifecycle

- [x] 3.1 Add strict TypeScript playback-session and view-event request types plus ID/sequence helpers.
- [x] 3.2 Track effective foreground watch time, media position, completion threshold, and terminal state in `VideoStage`.
- [x] 3.3 Emit exposed, play, bounded progress, complete, and skip events from active Feed transitions.
- [x] 3.4 Add pause, seek, visibility, scene-change, unmount, and page-exit flush behavior with keepalive/beacon fallback.
- [x] 3.5 Ensure lifecycle reporting does not duplicate events during swipe rendering or React Strict Mode effects.

## 4. Recommendation and History Integration

- [x] 4.1 Extend recommendation behavior-event messages and consumers with stable event identity and progress semantics.
- [x] 4.2 Add progress weighting and duplicate-event protection to recommendation profile reads or projections.
- [x] 4.3 Return media position and effective watch duration in personal-library history metadata.
- [x] 4.4 Update history UI progress rendering to use the new monotonic position field.

## 5. Verification and Documentation

- [x] 5.1 Add API-flow tests for lifecycle events, legacy compatibility, retries, conflicts, delayed ordering, and video visibility validation.
- [x] 5.2 Add Web tests for interval reporting, completion, skip, page-exit, and duplicate suppression.
- [x] 5.3 Add worker tests for outbox retry and duplicate recommendation delivery.
- [x] 5.4 Update exposure, feed, recommendation, personal-library, engineering, architecture, and performance documentation.
- [x] 5.5 Run targeted Go tests, the Web production build, browser lifecycle checks, and strict OpenSpec validation.
