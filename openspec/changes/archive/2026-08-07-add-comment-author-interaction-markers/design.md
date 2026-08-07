## Context

Threaded comments currently join the live `account` row for nickname and avatar, but omit the canonical public `account` identifier. The Web therefore caches a partial synthetic profile when a comment identity is activated. The video-author surface and comment surface also use different fallback images (`image.creator` versus `image.currentUser`), so an account with no avatar appears inconsistent even though both records come from the same account table.

The comment API already hydrates reply previews and viewer-like state in bounded queries. `video.author_id` is immutable for the lifetime of the video, and `interaction_comment_like` already records active likes by `(user_id, comment_id)`, so author markers can be derived without adding new durable columns.

## Goals / Non-Goals

**Goals:**

- Expose one canonical public identity projection for comment authors and direct reply targets.
- Make no-avatar users render consistently across video-author, comment-author, reply-target, and cached public-profile entry points.
- Mark comments written by the video author.
- Mark comments currently liked by the video author.
- Keep list, preview, reply, thread-context, creation, replay, and like-mutation behavior consistent.
- Preserve bounded query counts and existing tombstone privacy.

**Non-Goals:**

- Persisting nickname/avatar/account snapshots on comments.
- Adding creator pins, staff badges, verified badges, mention notifications, or reaction types.
- Changing comment sort weight based on authorship or author likes.
- Backfilling new database columns; all new fields are derived.

## Decisions

### Derive canonical identity from the live account row

Extend the comment read projection with `author.account` and the direct target's account, alongside the existing nickname and avatar. Domain and HTTP comment types gain additive `UserAccount` and `ReplyToUserAccount` fields.

The comment table remains identity-keyed by `user_id`; it does not duplicate mutable profile fields. Activating a comment identity continues to navigate by user ID, while the complete account/nickname/avatar projection provides a correct initial cache until `GET /api/users/{userId}` refreshes it.

Alternative considered: fetch every commenter through the account API in the Web. Rejected because it creates N+1 traffic and visible profile churn.

### Derive author markers on the server

Add additive booleans:

- `is_video_author`: `comment.user_id == video.author_id`;
- `liked_by_video_author`: an active `interaction_comment_like` exists for the video's author and comment.

The repository joins the parent video in the shared comment-row projection and derives the author-like state through a bounded set-based projection or indexed `EXISTS`. This applies uniformly to roots, previews, replies, thread context, and load-by-ID creation/replay paths.

The client MUST NOT infer these markers from the current viewer or nickname text. This keeps anonymous reads correct and avoids confusing “I liked this” with “the video author liked this.”

### Return author-like state from mutations

`CommentLikeResult` and its HTTP response gain `liked_by_video_author`. The transaction already locks and reads the parent video; after applying the requested like state it derives the effective author marker before returning. Idempotent replay returns the same effective marker.

This lets an author liking or unliking a visible comment update “作者赞过” immediately without reloading the thread. Non-author likes leave the marker unchanged.

### Use one shared avatar fallback

Introduce a small Web helper for public user avatars and use the same fallback for feed/video authors, comment authors, direct reply targets, and cached public profiles. Explicit account avatars always win.

Alternative considered: keep separate decorative defaults for creators and commenters. Rejected because those defaults imply different identities for the same account.

### Preserve tombstone privacy

Self-deleted root tombstones continue to clear user ID, account, nickname, avatar, content, and both author markers from the public DTO. Active replies remain independently projected.

## Risks / Trade-offs

- [An indexed author-like lookup adds work to comment reads] → Reuse the shared page query and existing unique/indexed like facts; add PostgreSQL query-count and result tests.
- [Live profile changes alter historical comment appearance] → This is intentional and matches current nickname/avatar behavior; comments identify the account, not a historical snapshot.
- [Cached public profile briefly uses comment data] → Include canonical account and shared avatar fallback, then continue refreshing from the authoritative public profile API.
- [Marker state becomes stale after an optimistic like] → Return `liked_by_video_author` from the mutation and update only the affected entity.

## Migration Plan

1. Deploy additive backend/domain/DTO fields and set-based derivation.
2. Deploy Web types, normalization, profile cache input, shared avatar fallback, and markers.
3. Old clients ignore the new fields; no database backfill is required.
4. Rollback removes rendering while leaving additive response fields harmless.

## Open Questions

None. “作者赞过” means the current active like state of the video's immutable author, not a historical endorsement snapshot.
