## Context

The exposure backend accepts `exposed`, `play`, `complete`, and `skip`, and watch history already projects the latest non-exposure event. The Web Feed only emits `exposed`; `VideoStage` separately reports coarse QoS on pause or unmount. Recommendation interest vectors therefore see few or no positive watch signals, while watch-history progress depends on callers that the Web client does not provide.

The change spans the Web player, exposure persistence, RabbitMQ delivery, recommendation projection, and personal-library history. It must tolerate duplicate lifecycle delivery, page shutdown, delayed requests, multiple tabs, and anonymous Feed scenes without blocking playback.

## Goals / Non-Goals

**Goals:**

- Emit a complete and testable playback lifecycle from the active Web player.
- Make retries and page-exit delivery safe through stable client event identifiers.
- Preserve monotonic watch-history state under duplicate, delayed, and concurrent events.
- Deliver accepted events reliably to recommendation consumers.
- Keep existing clients and the current endpoint compatible.

**Non-Goals:**

- Replacing playback QoS telemetry; that is handled by `expand-playback-observability`.
- Building the richer ranking pipeline; that is handled by `evolve-contextual-recommendation`.
- Tracking unauthenticated users in durable personal history.
- Sending an event on every `timeupdate`.

## Decisions

### 1. Use a versioned playback-session event envelope

Each activation of a Feed item creates a `playback_session_id`. Events include a client-generated `event_id`, monotonically increasing `sequence`, bounded `occurred_at`, `position_ms`, cumulative `watch_ms`, optional `duration_ms`, scene, Feed `request_id`, and event type:

- `exposed`: item became the active visible candidate.
- `play`: media first entered playing state in this activation.
- `progress`: active watch time or media position crossed the reporting interval.
- `complete`: media ended or reached the configured completion threshold.
- `skip`: the item deactivated before completion.

The existing request fields remain valid. Missing additive fields identify a legacy request.

Alternative: repeatedly overload `play` with progress. This was rejected because it obscures lifecycle meaning and makes downstream weighting and history diagnostics ambiguous.

### 2. Use bounded progress reporting and terminal flushes

The client reports progress after 10 seconds of effective foreground playback or a 10-second position advance, whichever occurs first. It also flushes on pause, seek completion, Feed deactivation, scene change, `visibilitychange`, and `pagehide`. Terminal delivery uses `fetch(..., {keepalive: true})` when possible and a bounded `sendBeacon` fallback.

Only active, visible playback contributes to `watch_ms`; waiting time and background time do not. A completion event is emitted once when playback ends or reaches both 95% and the final two seconds. Existing loop behavior may continue after the completion fact is emitted.

Alternative: report every media `timeupdate`. This was rejected because the event volume is browser-dependent and unnecessarily high.

### 3. Deduplicate by authenticated user and event ID

`video_view_events` gains `event_id`, `playback_session_id`, `sequence`, `occurred_at`, `position_ms`, and optional `duration_ms`. Authenticated writes use a unique `(user_id, event_id)` constraint. Replaying the same event returns the previously accepted result; reusing an event ID with a different normalized payload returns 409.

Legacy requests without `event_id` keep current non-idempotent behavior until all supported clients migrate.

### 4. Order history by bounded occurrence time and deterministic identity

The server validates `occurred_at` against a bounded clock-skew window and falls back to acceptance time when absent. The history projection stores `last_occurred_at`, `last_event_id`, `last_position_ms`, cumulative watch metadata, and completion state. It updates only when `(occurred_at, event_id)` is newer; older events may initialize `first_watched_at` but cannot regress the latest position or completion state.

Within one playback session, duplicate or decreasing sequences are accepted as immutable facts but do not replace a newer sequence from that session.

### 5. Persist recommendation delivery through an outbox

The event fact, watch-history projection, exposure projection, and a compact `view_event_outbox` record are committed in one PostgreSQL transaction. A worker publishes outbox rows to RabbitMQ with retry and marks them dispatched after publisher confirmation. Recommendation consumers deduplicate on `event_id`.

Alternative: continue ignoring publish errors in the HTTP request. This was rejected because the primary purpose of the change is to close the feedback loop.

### 6. Keep the HTTP path bounded and privacy-safe

The endpoint validates ID lengths, non-negative durations, maximum media duration, sequence range, allowed event transitions, and readable video state. It does not accept arbitrary device fingerprints or free-form metadata. Batching is deferred to the observability change; behavior events remain individually auditable facts.

## Risks / Trade-offs

- [Lifecycle events increase write volume] -> Use a 10-second progress interval, indexed append-only writes, and bounded retention planning.
- [Client clocks are inaccurate] -> Clamp occurrence time and retain deterministic event identity; use server acceptance time when absent.
- [Page-exit delivery is not guaranteed] -> Use terminal flushes before unmount, keepalive/beacon fallback, and idempotent retry on the next opportunity.
- [Outbox backlog delays profiles] -> Expose lag metrics and keep recommendation fallback behavior when new signals are delayed.
- [Multiple tabs create competing history] -> Order by occurrence time and never let older events regress the latest projection.

## Migration Plan

1. Add nullable event-envelope columns, idempotency storage, history fields, and outbox tables.
2. Backfill legacy rows with deterministic `legacy-{id}` event IDs, `occurred_at=created_at`, and `position_ms=watch_ms`.
3. Deploy compatible backend acceptance and outbox worker before enabling new Web events.
4. Enable Web lifecycle reporting behind a configuration flag and observe event volume, duplicates, outbox lag, and projection correctness.
5. Add `progress` to recommendation weighting only after production traffic confirms semantics.
6. Roll back by disabling the Web flag and outbox consumption; additive columns and legacy endpoint behavior remain valid.

## Open Questions

- Final retention period for raw progress events versus compact history and recommendation projections.
- Whether future mobile clients should share the same completion threshold or provide platform-specific policy.
