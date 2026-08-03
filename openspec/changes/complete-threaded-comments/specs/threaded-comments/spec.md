## ADDED Requirements

### Requirement: Two-level comment threads
The system SHALL represent discussions as root comments with replies flattened beneath one root. A reply directed at either a root comment or another reply MUST retain the direct target while remaining in the target's root thread, and the system MUST NOT create a third visual nesting level.

#### Scenario: User replies to a root comment
- **WHEN** an authenticated user submits a valid reply to an active root comment on a readable public video
- **THEN** the reply is created under that root, the root reply count increases once, and the response identifies both the root and direct target

#### Scenario: User replies to another reply
- **WHEN** an authenticated user replies to an active reply
- **THEN** the new reply uses the same root comment, records the selected reply as its direct target, and is displayed at the second visual level

#### Scenario: User targets an unavailable comment
- **WHEN** a reply target is missing, moderated, belongs to an unreadable video, or can no longer accept replies
- **THEN** the system rejects the request without creating a reply or changing any counter

### Requirement: Retry-safe comment and reply creation
Root-comment and reply creation SHALL trim and validate content by Unicode code-point length, SHALL support an `Idempotency-Key` of at most 128 characters, and SHALL bind each non-empty key to the canonical video, target, and content payload.

#### Scenario: Creation request is replayed
- **WHEN** the same user repeats a root-comment or reply request with the same idempotency key and canonical payload
- **THEN** the system returns the originally created comment without incrementing comment, reply, hot-score, or notification state again

#### Scenario: Creation key is reused for another payload
- **WHEN** the same user reuses an idempotency key with different content, video, root, or reply target
- **THEN** the system returns an idempotency conflict and preserves the original comment

#### Scenario: Multilingual content reaches the limit
- **WHEN** comment content contains multibyte characters within the configured code-point limit
- **THEN** the system accepts the content without treating UTF-8 byte length as character length

### Requirement: Root comment sorting and cursor pagination
The root-comment API SHALL support `latest` and `hot` sorting with opaque sort-specific cursors. The Web comment surface SHALL default to hot sorting and allow the user to switch to latest sorting without mixing pages from different sort modes.

#### Scenario: Latest comments are requested
- **WHEN** a client requests root comments using `sort=latest`
- **THEN** active roots and eligible self-deleted tombstones are ordered by `created_at DESC, id DESC` and return a matching next cursor

#### Scenario: Hot comments are requested
- **WHEN** a client requests root comments using `sort=hot`
- **THEN** roots are ordered by materialized hot score followed by `created_at DESC, id DESC` and return a cursor that includes the hot ordering tuple

#### Scenario: Cursor is used with another sort
- **WHEN** a client submits a cursor created for a different sort mode
- **THEN** the system rejects the cursor as invalid rather than returning a mixed or undefined page

### Requirement: Bounded reply previews and reply pagination
Each root-comment page SHALL hydrate at most three active reply previews per root using bounded batch queries. Full replies SHALL be loaded through a separate cursor-paginated endpoint ordered by `created_at ASC, id ASC`.

#### Scenario: Root page contains many threads
- **WHEN** a page contains multiple roots with replies
- **THEN** the response includes no more than three ordered previews per root and obtains profiles, viewer-like state, and previews without per-root query growth

#### Scenario: User expands a thread
- **WHEN** the root reports more replies than its preview contains
- **THEN** the Web UI offers an expand action and appends reply pages without duplicating existing previews or replies

#### Scenario: User collapses a thread
- **WHEN** an expanded thread is collapsed
- **THEN** the root remains visible with its bounded preview and the current root-list position is preserved

### Requirement: Comment likes
Authenticated users SHALL be able to set or clear a like on any active visible root comment or reply. The operation SHALL be retry-safe, SHALL update the comment's like count exactly once per state transition, and SHALL return the viewer's effective state.

#### Scenario: User likes a comment
- **WHEN** an authenticated user sets an unliked active comment to liked
- **THEN** one active like fact is stored, the comment like count increases once, and the response reports `liked=true`

#### Scenario: User repeats the same like state
- **WHEN** the user repeats a like or unlike operation whose target state is already effective
- **THEN** the request succeeds without changing the counter or emitting another notification

#### Scenario: Anonymous viewer lists comments
- **WHEN** comments are listed without valid viewer authentication
- **THEN** public comments remain readable and every viewer-specific `liked` and delete-permission field is false

### Requirement: Comment counters and hot score remain consistent
The video comment count SHALL equal the number of visible active root comments and active replies, excluding deletion tombstones. Root reply counts and materialized hot scores SHALL change transactionally with accepted reply, reply deletion, root-like, and moderation transitions.

#### Scenario: Reply changes thread activity
- **WHEN** a reply is created or removed
- **THEN** the video's comment count, the root's active reply count, and the root's hot score receive the corresponding single transactional delta

#### Scenario: Comment like changes hot ordering
- **WHEN** the like count of an active root changes
- **THEN** its materialized hot score is recomputed in the same transaction before a successful response is returned

#### Scenario: Reconciliation is run
- **WHEN** persisted counters are reconciled from comment and like facts
- **THEN** video comment counts, root reply counts, comment like counts, and hot scores converge without becoming negative

### Requirement: Permission-aware deletion
Comment deletion SHALL remain soft and idempotent while distinguishing commenter self-deletion from video-author or administrator moderation.

#### Scenario: Commenter deletes a root with replies
- **WHEN** the root author deletes their own root and active replies remain
- **THEN** the root becomes a non-counted tombstone with author identity and content hidden while its active replies remain readable

#### Scenario: Commenter deletes a root without replies
- **WHEN** the root author deletes their own root and no active replies remain
- **THEN** the root is omitted from public lists and the video comment count decreases once

#### Scenario: Video author moderates a root thread
- **WHEN** the video author or an administrator deletes a root comment
- **THEN** the root and every reply in that thread become publicly hidden and all active affected comments are removed from video and thread counters exactly once

#### Scenario: A reply is deleted
- **WHEN** the reply author, video author, or administrator deletes one reply
- **THEN** only that reply is hidden, the root remains available, and the video and root reply counts decrease once

### Requirement: Video visibility protects comment operations
Public comment listing, thread context, creation, reply, and like operations SHALL require the parent video to be published, public, and media-ready. Authorized deletion SHALL remain available after later video privacy or lifecycle changes.

#### Scenario: Parent video becomes private or unavailable
- **WHEN** an anonymous or ordinary viewer requests comments or attempts a new interaction after the video is no longer publicly readable
- **THEN** the system returns a not-found response and does not expose historical discussion content

#### Scenario: Existing comment requires moderation
- **WHEN** an authorized commenter, video author, or administrator deletes a comment after the video becomes private or offline
- **THEN** the deletion permission remains effective and the operation updates the stored discussion safely

### Requirement: Existing comments migrate without data loss
Migration SHALL treat all existing normal comments as root comments, preserve their content, authors, timestamps, idempotency keys, and current counts, and keep previously deleted comments hidden unless future active replies require a tombstone.

#### Scenario: Migration runs on existing data
- **WHEN** the upgraded schema is applied to a database containing flat comments
- **THEN** each normal comment remains readable as a root with zero replies and zero comment likes, and existing comment totals remain unchanged

#### Scenario: Migration is repeated
- **WHEN** API and Worker processes run the migration concurrently or the migration is rerun
- **THEN** schema changes and backfills are idempotent and do not duplicate likes, replies, receipts, outbox records, or counters
