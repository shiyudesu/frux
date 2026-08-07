## Context

Frux has asynchronous upload, media processing, automated/human review, publication, takedown, and restoration. The message module already supports durable idempotent internal creation, and human review writes a transactional notification outbox, but creator notifications cover only final human approval/rejection and use unstructured `SYSTEM` messages. A review approval may occur before the required media baseline is ready, so telling the creator that the video is published at approval time is sometimes false.

The design must preserve module ownership and transaction boundaries: video, review, and media workers own their facts; message owns user messages. Worker failure must not roll back accepted lifecycle changes, and retries must not create duplicate messages.

## Goals / Non-Goals

**Goals:**

- Notify creators durably when a video is submitted, rejected, approved-but-processing, first publicly available, terminally media-failed, taken down, or restored.
- Combine approval and publication into one truthful message when approval completes the final public-eligibility gate.
- Carry structured video lifecycle data instead of requiring clients to parse prose.
- Preserve transactional outbox reliability and message-level deduplication across multiple producer modules.
- Give protected/unavailable targets a safe navigation fallback.

**Non-Goals:**

- Persisting per-byte or per-percent upload progress in the message center.
- Notifying for every retry, automatic human-routing step, cache invalidation, or rendition completion.
- Adding push, email, SMS, notification preferences, or notification deletion.
- Rebuilding historical notifications for old videos.

## Decisions

### Notify at durable business facts, not transient upload state

The first durable creator event is video submission, committed with video creation after an upload asset or compatibility URL has been accepted. Completing an upload session without creating a video does not create a message because no reviewable work exists yet.

Client checksum, direct-upload progress, and local multipart progress remain page state. A terminal media-processing failure creates a durable notification; retryable attempts do not.

### Use a structured lifecycle notification envelope

Producer outboxes store a bounded envelope:

```text
event_id
recipient_user_id
video_id
review_version
stage
result
reason_code?
occurred_at
```

Registered stages are `submitted`, `review`, `media_processing`, `published`, `enforcement`, and `restoration`. Registered results are stage-specific closed values such as `pending`, `approved`, `rejected`, `failed`, `public`, `taken_down`, and `restored`.

Message gains a `VIDEO_LIFECYCLE` type plus nullable structured `lifecycle_stage`, `lifecycle_result`, `reason_code`, and existing `video_id`. Title/content remain bounded server-generated snapshots for compatibility. Unknown stage/result values are rejected by the internal message boundary.

Alternative considered: continue using free-form `SYSTEM` text. Rejected because the Web client could not route, label, or safely evolve lifecycle messages without parsing prose.

### Keep module-owned outboxes with one message contract

Review continues to own `review_notification_outbox` for approval/rejection facts. Video/media operations add a `video_notification_outbox` for submission, terminal media failure, first publication, takedown, and restoration. Both Workers call the same narrow lifecycle message writer and use the same stable envelope.

This avoids a cross-module transaction coordinator. Duplicate semantic events from different transitions are safe because `user_message` deduplicates `user_id + event_id`.

Stable event identities include:

- `video-submitted:{video_id}:{review_version}`
- `video-review-approved:{video_id}:{review_version}`
- `video-review-rejected:{video_id}:{review_version}`
- `video-media-failed:{video_id}:{asset_id}:{profile_version}`
- `video-published:{video_id}:{review_version}`
- `video-taken-down:{video_id}:{enforcement_id}`
- `video-restored:{video_id}:{restoration_id}`

Outbox rows remain retained after delivery so producer uniqueness also prevents duplicate creation.

### Model approval and publication around the two public gates

Public eligibility requires both review-published lifecycle and ready baseline media, plus public visibility. Notification behavior is:

1. Approval while baseline is not ready: emit `review/approved` with copy “审核通过，媒体处理中”.
2. Approval when all public gates are ready: emit only `published/public` with copy “审核通过并已发布”.
3. Baseline becoming ready after approval: emit `published/public`.
4. Rejection: emit `review/rejected`.

The `video-published:{video_id}:{review_version}` event is inserted only on the first public-eligibility edge for that review version. Later private/public toggles do not repeat it. Takedown and restoration have their own messages.

Alternative considered: always emit both approval and publication. Rejected because simultaneous events add noise and can arrive in a misleading order through independent Workers.

### Generate facts in the same transaction as their lifecycle transition

- Video creation writes the submission outbox row.
- Automated/human review transition writes either rejected, approved-but-processing, or published.
- Terminal media failure writes the failure row with the failed media state.
- Baseline-ready transition writes published when it completes the final gate.
- Takedown/restoration writes its corresponding outbox row with enforcement/restoration and audit facts.

Outbox delivery failure never rolls back the lifecycle fact. Workers use lease/attempt/available-at state and bounded exponential backoff, matching existing review/message outbox patterns. Invalid recipient or payload becomes terminal and observable.

### Use safe reason codes and target-aware Web navigation

Messages store registered safe reason codes, not private moderator notes or raw processor errors. The server maps codes to bounded creator-facing text.

The Web client renders lifecycle-specific icons and labels. Activation marks the message read first, then:

- published/restored messages may navigate to public video detail when readable;
- submitted, processing, rejected, failed, or taken-down messages navigate to the creator's works view with the video target;
- missing/deleted targets keep the message readable and show a safe unavailable state.

Legacy `SYSTEM` review messages remain readable and are not reinterpreted.

## Risks / Trade-offs

- [Different producers race to emit publication] → Use the same stable publication event ID and message-level unique constraint.
- [Outbox delivery order differs from lifecycle order] → Suppress a separate approval message when publication is immediate and render each remaining message from its own structured stage.
- [Terminal processing errors expose infrastructure details] → Persist an internal error separately and map only registered safe reason codes into messages.
- [Additional unread messages create noise] → Exclude progress, retries, human-routing, and additive rendition events.
- [Outbox tables grow] → Retain uniqueness while allowing an operational archival policy after a safe deduplication horizon.

## Migration Plan

1. Add lifecycle fields/message type and read them additively; old clients ignore unknown fields and legacy messages remain valid.
2. Add `video_notification_outbox`, producer indexes, and Worker delivery before enabling new fact creation.
3. Extend review notification rows and writer mapping while preserving delivery of existing approval/rejection rows.
4. Enable submission and review events, then processing/publication, then enforcement/restoration.
5. Deploy the Web renderer after APIs return structured fields.
6. Rollback disables new event creation; already-created messages remain readable as bounded lifecycle notifications.

## Open Questions

Notification preferences and non-Web delivery channels remain outside this change.
