## Why

New videos currently enter the published lifecycle immediately and become public as soon as media processing is ready. Content review cannot be added safely until public eligibility is explicitly gated by a review-aware video lifecycle.

## What Changes

- Add explicit pending-review and rejected lifecycle states while preserving draft, published, offline, and deleted behavior.
- Make new video creation enter pending review rather than published.
- Require both ready media and an approved review state before public Feed, detail, search, profile, collection, recommendation, preload, and media delivery.
- Define idempotent transitions for approval, rejection, takedown, and restoration without yet implementing machine or human review queues.
- Migrate existing published content without forcing historical re-review.
- **BREAKING**: newly created videos no longer become publicly eligible immediately after media processing.

## Capabilities

### New Capabilities

- `video-review-lifecycle`: Review-aware video states, transition rules, and public-eligibility gating.

### Modified Capabilities

- `creator-content-management`: Creator queries and public-work statistics must represent pending-review and rejected videos correctly.
- `production-media-delivery`: Ready media becomes public only when its owning video is also review-approved.

## Impact

This affects the video domain, media-publication projection, repositories and migrations, public read filters, Feed/search/recommendation hydration, creator DTOs, frontend status labels, API-flow tests, and video/review documentation.
