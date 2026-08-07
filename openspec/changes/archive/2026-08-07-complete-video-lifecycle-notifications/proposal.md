## Why

Creators currently receive only part of the review outcome through the message center and cannot reliably distinguish submission, approval, media failure, and actual public publication. Durable lifecycle notifications are needed so asynchronous upload, review, processing, and enforcement outcomes are visible and truthful.

## What Changes

- Add durable creator notifications for review submission, media-processing failure, review approval, review rejection, final public publication, takedown, and restoration.
- Keep client-side checksum and upload-percentage progress on the upload page rather than producing noisy durable messages.
- Distinguish “review approved but media still processing” from “video is publicly available.”
- Store structured video lifecycle metadata with each message, including video ID, lifecycle stage, result, and safe reason code.
- Emit stable event identities through transactional outboxes and deduplicate Worker retries in the message service.
- Define safe user-facing text and navigation when a target video is rejected, protected, offline, deleted, or otherwise unreadable.

## Capabilities

### New Capabilities

- `video-lifecycle-notifications`: Define creator-facing event coverage, structured message payloads, idempotency, and truthful Web behavior across the video lifecycle.

### Modified Capabilities

- `video-review-lifecycle`: Emit durable lifecycle facts for submission, review outcomes, takedown, and restoration.
- `production-media-delivery`: Emit processing-failure and final-publication facts at the media availability gates.

## Impact

Affected areas include video and review transactions, media-processing and enforcement Workers, lifecycle notification outboxes, message entities/DTOs and idempotent creation, message-center rendering/navigation, unread counts, migrations, and API-flow/Worker/frontend tests.
