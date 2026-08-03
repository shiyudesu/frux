## Context

GCFeed's interaction module currently stores every comment as an independent flat row and exposes create, newest-first list, and permission-aware soft delete APIs. The Web Feed requests one fixed page, discards the returned cursor, supports only root creation, and has no delete, reply, or comment-like controls. The message module can display comment notifications but stores no video or comment target, and message activation only marks the row read.

The change crosses interaction, message, migration, HTTP, router, Feed, video-detail, and browser-test surfaces. It must preserve public-video visibility rules, strict TypeScript, the hand-written History API router, PostgreSQL as the durable source of truth, optional viewer authentication for public reads, stable cursor conventions, and bounded query counts.

## Goals / Non-Goals

**Goals:**

- Deliver complete two-level discussions with root comments, flattened directed replies, comment likes, hot/latest root sorting, reply expansion, pagination, deletion, and accurate counters.
- Preserve additive compatibility for existing root-comment creation and listing clients.
- Make comment, reply, and comment-like notifications durable, idempotent, and actionable.
- Keep public reads anonymous while returning viewer-specific like and permission state when optional authentication is valid.
- Migrate existing flat comments without data loss and maintain deterministic rollback behavior.
- Reuse shared Feed/video/comment components across Feed and the new video-detail route.

**Non-Goals:**

- Arbitrarily deep nested comments, rich text, images, stickers, mentions beyond the selected reply target, or live WebSocket updates.
- Comment reporting, keyword moderation, creator pinning, geographic labels, AI summaries, or external social sharing.
- Moving comment likes onto the Redis/RabbitMQ video-action write-behind pipeline.
- Replacing the current router or introducing a frontend state-management library.

## Decisions

### 1. Keep comments in the interaction module with an explicit two-level model

`interaction_comment` remains the aggregate fact table and gains:

- nullable `root_comment_id`: `NULL` for roots; the owning root ID for replies;
- nullable `reply_to_comment_id`: the direct root or reply selected by the author;
- `reply_count`, `like_count`, and `hot_score` counters;
- an expanded status enum: normal, self-deleted, and moderated;
- `request_fingerprint` for idempotency-key payload binding.

A reply to another reply copies the target's `root_comment_id` and records the target in `reply_to_comment_id`. This supports directed conversation while guaranteeing one root plus one visual reply level.

Alternative considered: an adjacency-list `parent_id` with arbitrary nesting. It was rejected because the product explicitly wants two visual levels, recursion complicates pagination and deletion, and the mobile sheet cannot present deep trees cleanly.

### 2. Store comment likes separately and update counters synchronously

Add `interaction_comment_like` with a unique `(user_id, comment_id)` state row and `interaction_comment_like_idempotency_receipt` binding `(user_id, idempotency_key)` to comment and target active state. Like/unlike operations run in a PostgreSQL transaction that locks the comment and like row, applies a real state transition once, updates `like_count`, and recomputes root hot score when the liked comment is a root.

Comment likes do not reuse `interaction_action`: that model is video-scoped and tightly coupled to Redis state, RabbitMQ action versions, video statistics, recommendation attribution, and library indexes. Reuse would blur domain ownership and require invalid video-action semantics for comment IDs.

### 3. Materialize a simple deterministic root hot score

Root hot score is stored and recomputed as:

```text
hot_score = active_root_like_count * 3 + active_reply_count * 5
```

The weights align with the interaction module's existing relative hot-score treatment while remaining explainable and cheap to reconcile. Root likes and reply transitions update the score transactionally. Likes on replies remain visible but do not bubble into root ranking.

`sort=hot` uses `(hot_score DESC, created_at DESC, id DESC)`. `sort=latest` uses `(created_at DESC, id DESC)`. The opaque cursor includes a version and sort discriminator, so cross-sort reuse is rejected. The API keeps `latest` as its omitted-sort compatibility default; the new Web UI explicitly requests `hot`.

Trade-off: scores can change between pages. The client deduplicates by comment ID, and the API preserves deterministic ordering for each observed score tuple. A snapshot ranking service is intentionally out of scope.

### 4. Use bounded root hydration and a separate reply endpoint

Root listing performs a bounded sequence:

1. Validate the public, published, media-ready video.
2. Query one root page.
3. Batch load up to three reply previews per root with a PostgreSQL window function ordered by `(created_at ASC, id ASC)`.
4. Batch load viewer likes when optional identity is present.
5. Join or batch hydrate user profiles and direct reply targets.

Replies use a separate cursor endpoint ordered oldest-first. A direct thread-context endpoint loads one root plus its first reply page for notification deep links without scanning hot/latest pages.

Alternative considered: return all replies with every root page. It was rejected because large threads would make response size and query work unbounded.

### 5. Apply differentiated deletion semantics without erasing audit facts

- Root author self-delete: mark the root self-deleted. If active replies remain, list a tombstone DTO with no author identity or content; otherwise omit it.
- Video author or administrator root delete: mark the root moderated and batch-mark every still-visible reply moderated in one transaction.
- Reply deletion: hide only that reply, whether self-deleted or moderated.
- Repeated deletion returns the current result without another counter delta.

Video `comment_count` counts normal roots and normal replies only. Tombstones are not counted. Root `reply_count` counts normal replies only. Moderating a thread decrements all currently counted rows atomically.

Stored content and actor IDs remain in PostgreSQL for audit and idempotency, but tombstone/public DTOs do not expose them.

### 6. Make creation idempotency payload-aware

The existing unique `(user_id, idempotency_key)` identity remains, but every new root or reply stores a canonical request fingerprint containing video, root, direct target, and normalized content. A replay with the same fingerprint returns the original comment; a different fingerprint returns 409. Existing rows with no fingerprint retain legacy replay behavior and are not rewritten.

Content limits use Unicode code points rather than UTF-8 byte count. The initial maximum remains 1000 code points to avoid an unnecessary product-limit change.

### 7. Deliver notifications through a durable interaction outbox

Add `interaction_comment_notification_outbox` with event ID, recipient, actor, message type, content snapshot, video ID, root ID, target comment ID, state, attempt count, next-attempt time, and lease fields. The same transaction that creates a comment/reply or performs a new comment-like transition inserts its outbox event unless actor and recipient are the same.

The Worker drains the outbox with leasing and bounded exponential backoff, calls the existing narrow message writer extended with structured targets, and marks delivery complete only after message persistence succeeds. `user_id + event_id` continues to deduplicate message creation.

Message types become:

- `COMMENT` for a new root comment sent to the video author;
- `COMMENT_REPLY` for a reply sent to the direct target author;
- `COMMENT_LIKE` for a new like sent to the comment author.

Unlike/re-like uses a stable actor/comment event identity, preventing notification spam across repeated toggles.

Alternative considered: keep best-effort post-commit message writes. It was rejected because a successful interaction could permanently lose its notification during a transient message failure.

### 8. Add structured message targets additively

`user_message` gains nullable `video_id`, `comment_id`, and `root_comment_id`. Domain, internal API, persistence, and public DTOs carry these fields. Existing messages remain valid with zero/omitted targets, and old clients ignore the additive JSON fields.

The message center first marks an activated message read, then navigates only when target metadata is valid. Missing legacy targets retain read-only behavior.

### 9. Add a typed video-detail route with validated focus parameters

The Route union gains `/videos/${number}`. The hand-written router gains a typed navigation target that can include validated search parameters:

```text
comment=<root_comment_id>
highlight=<target_comment_id>
```

`VideoDetailPage` loads the existing public video detail API and shared threaded-comment surface. When a root target is supplied it uses the direct thread-context endpoint, expands the replies, scrolls the target into view, and applies a temporary highlight. Missing, malformed, deleted, or hidden targets produce an explicit unavailable-discussion state while the video remains readable when allowed.

Alternative considered: inject a target video into the timeline Feed. It was rejected because Feed cursor/ranking state is not a stable deep-link destination and notification navigation must survive reloads.

### 10. Normalize frontend comment state by video and thread

Replace the single flat `useComments` state with a typed threaded-comment controller containing:

- root pages keyed by video and sort;
- reply pages keyed by root ID;
- deduplicated entity maps;
- per-video drafts and selected reply target;
- independent create, like, delete, root-load, and reply-load busy/error state;
- expanded-root state and focused target;
- session-aware permission and optimistic-like handling with rollback.

The Feed panel and Video Detail page consume the same controller and presentation components. Anonymous users can read comments; participation controls navigate to authentication instead of presenting a dead disabled input.

### 11. Preserve visibility, authorization, and constant-query boundaries

All public discussion reads and new root/reply/like writes require the parent video to remain published, public, and media-ready. Comment deletion keeps current author/video-author/admin permissions after later video privacy changes.

Viewer-specific `liked` and `can_delete` fields are computed server-side under optional JWT identity. This avoids duplicating administrator and tombstone rules in Web code. Root pages with up to 100 items use bounded batch queries rather than per-thread repository calls.

### 12. Indexes and reconciliation

Add indexes supporting:

- root latest ordering by video/status/root/created/id;
- root hot ordering by video/status/root/hot-score/created/id;
- replies by root/status/created/id;
- direct targets by `reply_to_comment_id`;
- comment likes by comment/status and unique user/comment;
- idempotency receipts and pending outbox leases.

Add reconciliation that derives video comment counts, root reply counts, comment like counts, and root hot scores from facts. As with existing aggregate reconciliation, updates use snapshot deltas rather than absolute overwrites when concurrent writes can occur.

## Risks / Trade-offs

- [Mutable hot scores can move comments between pages] -> Embed sort and tuple values in cursors, deduplicate client-side, and keep latest sorting available for strictly chronological browsing.
- [Moderating a large root thread can lock many rows] -> Keep replies two-level, index `root_comment_id`, issue set-based updates, and cap transaction duration with existing request cancellation.
- [Reply previews can create N+1 queries] -> Require window-function batch hydration and assert bounded query counts in PostgreSQL tests.
- [Outbox growth during message outages] -> Lease in bounded batches, use exponential backoff, expose metrics, and retain terminal error state for inspection.
- [Deep links can expose deleted or private discussion context] -> Revalidate video and comment visibility on every context request and return an unavailable state without tombstone-hidden identity/content.
- [Existing deleted rows have only one legacy status] -> Map them to self-deleted during migration; because they have no replies they remain omitted, matching current behavior.
- [Old Web builds do not recognize new message types] -> Keep message fields additive and ensure the existing unknown-type icon/body fallback remains safe during staggered rollout.

## Migration Plan

1. Add nullable comment-thread columns, counters, fingerprint, new statuses, comment-like tables, like receipts, notification outbox, message target columns, and indexes through the shared advisory-locked migration.
2. Backfill existing comments as roots with zero reply/like/hot counters; preserve normal rows and map legacy deleted rows to self-deleted.
3. Deploy backend APIs and Worker outbox processing while the old Web continues using root creation and latest root listing.
4. Reconcile all comment, reply, like, hot-score, and video-comment counters and record a persistent migration marker.
5. Deploy the new Web threaded surface, typed video route, and actionable message navigation.
6. Run API-flow, PostgreSQL migration/repository, frontend unit/build, and Windows Chrome desktop/mobile smoke coverage.

Rollback uses application rollback rather than destructive schema rollback. The old backend and Web ignore additive columns/tables; new notification types remain readable through fallback presentation. Newly created replies are not representable in the old flat Web, so backend rollback after enabling replies requires temporarily disabling reply and comment-like write routes while retaining data until the new backend is restored.

## Open Questions

- None. Product decisions are fixed for two visual levels, hot-default/latest sorting, differentiated deletion, and direct notification navigation.
