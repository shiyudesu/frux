## 1. Structured Lifecycle Message Model

- [ ] 1.1 Add the `VIDEO_LIFECYCLE` message type and registered lifecycle stage/result/reason domain values with bounded validation.
- [ ] 1.2 Extend message entities, persistence models, migration, internal create DTO, and public DTO with additive structured lifecycle fields and `video_id`.
- [ ] 1.3 Preserve legacy `SYSTEM` review messages and existing comment/like/follow message compatibility.
- [ ] 1.4 Add message-service tests for valid lifecycle creation, unknown values, bounded fields, event/idempotency replay, and legacy reads.

## 2. Transactional Lifecycle Facts

- [ ] 2.1 Define the shared bounded lifecycle notification envelope and stable event-ID constructors for every registered stage.
- [ ] 2.2 Add the video-owned notification outbox model, migration, repository leasing, retry, terminal-state, and retention behavior.
- [ ] 2.3 Extend review notification outbox payloads to represent rejection, approved-but-processing, and combined approved-and-published events.
- [ ] 2.4 Write the submission outbox fact atomically with both production-media and compatibility video creation paths.
- [ ] 2.5 Write safe automated and human review outcome facts atomically with case/video/decision/audit transitions.
- [ ] 2.6 Write terminal media-failure and first-publication facts atomically with the corresponding media/video state transition.
- [ ] 2.7 Write takedown and restoration facts atomically with lifecycle, enforcement/restoration, audit, and side-effect intent changes.
- [ ] 2.8 Enforce one first-publication event per video review version across review, media-ready, reconciliation, visibility, and restoration paths.

## 3. Durable Delivery

- [ ] 3.1 Extend the narrow message writer to create structured lifecycle messages with safe server-generated title/content snapshots.
- [ ] 3.2 Implement the video notification Worker using database leases, `FOR UPDATE SKIP LOCKED`, bounded exponential backoff, terminal validation errors, and message-level deduplication.
- [ ] 3.3 Update the review notification Worker to deliver the structured envelope while continuing to drain legacy rows.
- [ ] 3.4 Register Workers, configuration, migrations, and fixed-label metrics in API/Worker composition roots.
- [ ] 3.5 Add reconciliation for missing first-publication facts without synthesizing historical messages for pre-change videos.

## 4. Message Center Experience

- [ ] 4.1 Extend strict frontend message types and runtime guards for lifecycle stage, result, reason, and target video.
- [ ] 4.2 Render lifecycle-specific labels/icons/copy from structured fields with a safe fallback for unknown or legacy messages.
- [ ] 4.3 Mark lifecycle messages read before navigation and route published/restored targets to video detail when readable.
- [ ] 4.4 Route submitted, processing, rejected, failed, and taken-down targets to the creator works surface without attempting public playback.
- [ ] 4.5 Handle deleted or unavailable video targets without leaking media and without reverting the read state.
- [ ] 4.6 Keep upload checksum and percentage feedback local to `UploadPage` while showing the durable submitted state after video creation.

## 5. Verification and Documentation

- [ ] 5.1 Add transaction tests proving lifecycle changes roll back when required outbox insertion fails and delivery failure never rolls back committed facts.
- [ ] 5.2 Add Worker tests for retries, terminal payloads, duplicate semantic events, review/media publication races, and restart recovery.
- [ ] 5.3 Add API-flow tests for submission, automated/human rejection, approval-before-media, media-before-approval, combined publication, terminal failure, takedown, and restoration.
- [ ] 5.4 Add frontend tests for lifecycle rendering, unread counts, navigation, protected/deleted targets, and legacy compatibility.
- [ ] 5.5 Run targeted Go message/video/review/media tests and the strict Web production build.
- [ ] 5.6 Update `docs/modules/message.md`, `video.md`, and `review.md` with the event matrix, outbox ownership, structured payload, and truthful two-gate semantics.
