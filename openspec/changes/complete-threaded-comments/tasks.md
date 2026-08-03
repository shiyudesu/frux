## 1. Comment Domain and Schema

- [x] 1.1 Extend the interaction comment entity with root/direct-target IDs, reply/like/hot counters, normal/self-deleted/moderated statuses, Unicode content validation, viewer state, permissions, and tombstone projection rules.
- [x] 1.2 Extend interaction repository interfaces and domain errors for root pages, reply pages, direct thread context, comment likes, payload-aware creation replay, differentiated deletion, and reconciliation.
- [x] 1.3 Add GORM models for threaded comment fields, comment likes, comment-like idempotency receipts, and the leased comment-notification outbox.
- [x] 1.4 Add advisory-locked migration steps and explicit indexes for root latest/hot ordering, reply ordering, direct targets, likes, receipts, and pending outbox leases.
- [x] 1.5 Backfill existing comments as roots, map legacy deleted rows safely, initialize counters and scores, and add an idempotent persistent migration marker.
- [x] 1.6 Implement snapshot-delta reconciliation for video comment totals, root reply counts, comment like counts, and materialized hot scores.

## 2. Threaded Comment Persistence

- [x] 2.1 Implement payload-aware root-comment creation and replay so matching idempotency requests return the original row and conflicting payloads return a conflict.
- [x] 2.2 Implement reply creation that resolves root/direct targets, prevents unavailable targets, keeps replies at one visual level, and updates video/root/hot counters transactionally.
- [x] 2.3 Implement latest and hot root queries with sort-versioned opaque cursors, eligible self-deleted tombstones, and public-video visibility enforcement.
- [x] 2.4 Implement bounded root-page hydration for at most three reply previews per root, user profiles, direct-target profiles, viewer likes, and server-computed delete permissions without N+1 growth.
- [x] 2.5 Implement oldest-first reply cursor pagination and a direct root-thread context query for notification deep links.
- [x] 2.6 Implement retry-safe comment like/unlike persistence with exact counter transitions and root hot-score updates.
- [x] 2.7 Implement differentiated deletion transactions for self-deleted root tombstones, hidden roots without replies, moderator thread cascades, and isolated reply deletion.
- [x] 2.8 Preserve authorized deletion after parent-video privacy or lifecycle changes while rejecting new reads, replies, and likes for unreadable videos.

## 3. Comment Application and HTTP APIs

- [x] 3.1 Add application inputs/results and cursor codecs for root sorting, replies, direct thread context, comment likes, viewer state, and deletion outcomes.
- [x] 3.2 Add service orchestration for root/reply creation, hot/latest listing, reply expansion, thread context, like transitions, deletion, cache synchronization, hot ranking, and outbox creation.
- [x] 3.3 Extend comment DTOs with root/direct-target metadata, reply previews/counts, likes, viewer permissions, deleted state, and total comment counts while keeping existing fields compatible.
- [x] 3.4 Keep `POST /api/videos/{videoId}/comments` for roots and add reply creation, reply listing, thread context, and comment like/unlike endpoints.
- [x] 3.5 Add optional JWT authentication to public comment and thread reads so anonymous access remains available and authenticated responses include viewer-specific state.
- [x] 3.6 Update interaction error-to-status mapping for sort/cursor errors, reply-target errors, like conflicts, moderated resources, and payload idempotency conflicts.
- [x] 3.7 Wire all new interaction routes and options through the Hertz composition root without importing infrastructure into domain or application packages.

## 4. Durable Comment Notifications

- [x] 4.1 Add `COMMENT_REPLY` and `COMMENT_LIKE` message types plus structured `video_id`, `comment_id`, and `root_comment_id` fields across message domain, persistence, internal DTOs, and public DTOs.
- [x] 4.2 Extend the narrow interaction-to-message writer contract to carry structured targets while preserving existing like, follow, system, and legacy comment messages.
- [x] 4.3 Insert root-comment, reply, and new comment-like outbox events in the same interaction transaction with stable event IDs and self-notification suppression.
- [x] 4.4 Implement leased Worker delivery with bounded batches, exponential backoff, idempotent message creation, terminal error handling, and processing metrics.
- [x] 4.5 Register the comment-notification outbox worker in `cmd/worker` and ensure shutdown, retry, and database-error behavior follows existing supervised worker patterns.
- [x] 4.6 Backfill no synthetic notifications for historical comments and verify existing messages without target metadata remain readable and markable as read.

## 5. Frontend Data and Routing

- [x] 5.1 Extend strict TypeScript comment and message types for roots, replies, previews, likes, permissions, tombstones, sorting, thread context, and structured message targets.
- [x] 5.2 Extend `src/api/social.ts` with typed root-page, reply-page, thread-context, reply-create, comment-like, and comment-delete calls using retry-safe idempotency keys.
- [x] 5.3 Replace the flat `useComments` state with normalized per-video/per-sort root pages, per-root reply pages, deduplicated entities, expanded roots, focused targets, and independent operation states.
- [x] 5.4 Add per-video drafts, reply-target selection/cancelation, Unicode character counts, create replay handling, optimistic comment-like updates with rollback, and permission-aware delete updates.
- [x] 5.5 Extend the hand-written typed router with `/videos/${number}` and validated comment/highlight search parameters without adding a routing library.
- [x] 5.6 Add a video-detail API/page flow that loads a readable video, reuses shared player/comment components, opens a requested thread, scrolls to the target, and presents an unavailable-discussion state safely.

## 6. Threaded Comment and Message UI

- [x] 6.1 Refactor the Feed details panel into reusable details and threaded-comment components shared by Feed and Video Detail.
- [x] 6.2 Add hot/latest controls, root loading-more, bounded reply previews, expand/collapse, reply loading-more, direct-target labels, and stable list deduplication.
- [x] 6.3 Add root/reply like controls, like counts, reply actions, permission-aware delete menus, moderator cascade confirmation, and self-delete tombstone presentation.
- [x] 6.4 Replace the single-line disabled composer with an accessible multiline composer that shows reply context, character count, login action, submitting state, local failure, and cancel behavior.
- [x] 6.5 Preserve panel focus return, keyboard shortcut suppression, mobile touch targets, scroll position, per-video draft state, expanded threads, and focused-target highlighting.
- [x] 6.6 Update wide-desktop panel, compact drawer, and mobile bottom-sheet styles so sorting, nested replies, action rows, composer, and error states remain reachable without overflow.
- [x] 6.7 Update the message center to render root-comment/reply/comment-like labels and icons, mark actionable messages read, and navigate to the structured video discussion target.
- [x] 6.8 Keep legacy messages and unavailable targets functional with read-only behavior or explicit unavailable-discussion feedback.

## 7. Backend and Frontend Automated Tests

- [x] 7.1 Add interaction domain/application tests for Unicode limits, payload fingerprints, root resolution, reply-to-reply flattening, hot-score deltas, and all deletion modes.
- [x] 7.2 Extend interaction API-flow tests for compatible root creation, hot/latest cursors, reply pages, preview bounds, optional viewer state, comment likes, permissions, idempotency conflicts, and hidden-video behavior.
- [x] 7.3 Add PostgreSQL repository and migration tests for backfill, indexes, stable ordering, bounded hydration query counts, concurrent counter reconciliation, cascade moderation, and repeated migration.
- [x] 7.4 Add message/outbox tests for root, reply, and comment-like events, self-suppression, transient retry, stable deduplication, structured targets, and legacy messages.
- [x] 7.5 Add frontend API and threaded-comment state tests for page merging, sort switching, reply expansion, draft isolation, optimistic rollback, creation, deletion, and authentication transitions.
- [x] 7.6 Add component tests for desktop panel, mobile sheet, tombstones, moderator confirmation, reply targeting, character limits, focus restoration, and unavailable discussion.
- [x] 7.7 Add router and message-navigation tests for valid typed video targets, malformed search parameters, root/reply highlighting, reload persistence, and removed targets.

## 8. Documentation and End-to-End Validation

- [ ] 8.1 Update `docs/modules/interaction.md` with the threaded schema, APIs, sorting, counters, likes, deletion rules, visibility, idempotency, outbox, and test matrix.
- [ ] 8.2 Update `docs/modules/message.md`, `docs/product.md`, `docs/uiux.md`, and `docs/engineering.md` for actionable targets, message types, typed video routing, responsive threaded UI, and module boundaries.
- [ ] 8.3 Run targeted Go interaction, message, migration, and Worker tests, then run `go test ./...` if targeted validation exposes shared-package risk.
- [x] 8.4 Run the existing frontend unit tests and `pnpm -C apps/web run build` with strict TypeScript.
- [ ] 8.5 Validate desktop and mobile root/reply creation, likes, pagination, sorting, deletion semantics, and message deep links in Windows Chrome through the mounted Windows executable.
- [ ] 8.6 Capture required threaded-comment desktop/mobile screenshots, inspect console/network failures, and verify the video-detail target survives direct reload.
- [ ] 8.7 Run `openspec validate --all --strict` and reconcile proposal, specs, design, tasks, and affected product documentation with the delivered behavior.
