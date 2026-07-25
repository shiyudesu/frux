## Why

GCFeed Web currently reports only exposure events, while watch history and recommendation profiles depend on play and completion behavior. This leaves the recommendation feedback loop effectively empty for normal Web sessions and prevents reliable resume-progress data.

## What Changes

- Define a complete client playback-event lifecycle covering exposed, play, progress, complete, skip, and page-exit behavior.
- Make view-event writes retry-safe with a client event identifier so lifecycle retries do not duplicate facts.
- Update the Web player to report cumulative media position and effective watch duration at bounded intervals and terminal transitions.
- Keep watch-history projection updates transactionally aligned with accepted events and prevent stale or delayed events from regressing progress.
- Publish accepted behavior events for recommendation-profile updates without blocking playback.
- Preserve the existing `/api/video-view-events` contract through additive fields and compatible event handling.

## Capabilities

### New Capabilities

- `view-event-feedback`: Defines trustworthy client playback lifecycle reporting, idempotent event acceptance, recommendation signaling, and unload-safe delivery.

### Modified Capabilities

- `personal-video-library`: Extends durable watch history to consume progress events and retain the newest monotonic playback position and completion state.

## Impact

- Affects `VideoStage`, `useFeed`, Feed API types, exposure Domain/Application/Persistence/HTTP code, RabbitMQ view-event messages, recommendation profile reads, migrations, and API-flow tests.
- Adds a uniqueness boundary for client event IDs and may add progress metadata to view-event and history records.
- Requires synchronized updates to exposure, feed, recommendation, personal-library, engineering, and performance documentation.
