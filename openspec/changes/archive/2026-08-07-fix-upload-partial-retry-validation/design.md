## Context

UploadPage currently validates metadata and file presence, then starts video and cover uploads concurrently under one attempt identifier. The production API validates each upload session independently. A deterministic rejection on one file can therefore race with successful completion of the other file. The page does not retain the successful result separately, and replacing one file resets the shared attempt, so retrying can upload an unchanged successful file again.

The latest failure has exactly that shape: PostgreSQL contains a newly completed video session but no matching cover session, while the page reported `UPLOAD_SESSION_VALIDATION_FAILED`. The current error contract also collapses size, MIME, and filename failures into one generic message.

## Goals / Non-Goals

**Goals:**

- Reject unsupported or oversized selected files before either upload starts.
- Tell the user whether the video or cover is invalid and why.
- Preserve the completed result and idempotency identity of each unchanged selected file.
- Retry only the failed or replaced side of a video/cover pair.
- Preserve both uploaded results across a retryable video-creation failure.
- Keep backend validation authoritative and return actionable validation codes to non-Web clients.

**Non-Goals:**

- Resuming a partially transferred object-storage PUT.
- Persisting upload sessions across a page reload or browser restart.
- Making the cover optional or generating a cover automatically.
- Changing media processing, review, visibility, or public eligibility.

## Decisions

### Preflight the complete pair before network work

The Web will validate both selected files as one preflight step after metadata and presence validation. It will enforce the existing upload contract: video extensions MP4/MOV/WebM up to 512 MiB and cover extensions JPEG/PNG/WebP up to 20 MiB, with the corresponding MIME family when the browser provides one.

No checksum or upload-session request starts unless both files pass. Backend validation remains authoritative and uses specific error categories for unsupported filename/type and per-kind size limits.

Alternative considered: upload sequentially and validate the cover only after the video. Rejected because it still performs avoidable network work before a deterministic failure.

### Track upload identity and completion per selected file

UploadPage will keep separate in-memory records for video and cover. Each record contains the exact selected `File`, a stable random idempotency seed, and an optional completed `MediaUploadResult`.

Selecting or replacing one file creates a new record only for that kind. The unchanged counterpart keeps its seed and completed result. A submit uploads only records without a completed result, and stores each fulfillment immediately rather than waiting for the paired `Promise.all` result.

Alternative considered: keep one pair-level attempt identifier and reset it on any file change. Rejected because changing an invalid cover unnecessarily invalidates a successfully uploaded video.

### Separate media upload replay from video creation replay

The final `POST /api/videos` receives its own stable creation key for the current selected media pair. Changing the video or cover selection invalidates that creation key; editing metadata does not, because a response lost after successful creation must not permit a duplicate video on retry. Media selection changes still preserve completed uploads for the unchanged file.

If video creation fails transiently or metadata validation is corrected, retry reuses both completed assets and the same creation key. The backend creates no idempotency record for a rejected request, while a request that already committed safely replays the original video.

### Keep progress truthful for reused results

A completed cached side reports 100 percent immediately on retry. The failed side resets its own progress and uploads again. The page status distinguishes local validation, media upload, and work creation errors through existing safe user-facing error handling.

## Risks / Trade-offs

- [Web and backend constraints drift] → Define the same explicit limits and formats in tests and document them in the video module and OpenSpec scenarios.
- [A selected `File` object is replaced with an equivalent file] → Treat it as a new selection and new upload identity; correctness is preferred over content-based cross-selection reuse.
- [A completed orphan asset remains when the user abandons the page] → Preserve the existing delayed cleanup/reconciliation behavior; browser persistence is explicitly out of scope.
- [One promise fulfills after its counterpart rejects] → Store each result inside its own upload operation and guard it against a later file replacement.

## Migration Plan

1. Deploy additive backend validation codes and tests.
2. Deploy the Web paired preflight and per-file retry state.
3. Existing upload sessions remain replayable because idempotency-key format and API shapes stay compatible.
4. Rollback restores the shared-attempt UI; backend validation codes remain safe API additions.

## Open Questions

None. Cover selection remains required, and successful unchanged media should always be reused within the current page session.
