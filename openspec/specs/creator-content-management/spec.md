# creator-content-management Specification

## Purpose

Defines creator-owned video visibility, querying, batch management, collections, aggregate statistics, interaction reliability, and private-content isolation.

## Requirements

### Requirement: Independent Video Visibility
Frux SHALL model video visibility independently from lifecycle status, with public visibility as the migration default for existing videos.

#### Scenario: Author makes published video private
- **WHEN** the video owner changes a published public video to private
- **THEN** its lifecycle status remains published but it is removed from Feed, public profile, and public collection reads

#### Scenario: Author makes private video public
- **WHEN** the owner changes an eligible private published video to public
- **THEN** it becomes publicly readable again without changing its original publication time

### Requirement: Creator Video Query
Frux SHALL provide an authenticated cursor-paginated creator query supporting visibility, keyword, creation-date range, cursor, and limit filters, ordered by `created_at DESC, id DESC`.

#### Scenario: User filters private works
- **WHEN** an authenticated user queries their videos with private visibility
- **THEN** only their non-deleted private videos are returned

#### Scenario: User searches own works
- **WHEN** the user supplies a keyword and date range
- **THEN** matching owned videos within the inclusive validated range are returned using parameterized search

### Requirement: Creator Work Archive Months
Frux SHALL provide an authenticated creator archive-month read that returns the unique UTC creation months containing the owner's non-deleted works for a required public or private visibility. Months SHALL use canonical `YYYY-MM` values, be ordered newest first, and remain independent from keyword and cursor state.

#### Scenario: Creator loads public archive months
- **WHEN** an authenticated creator requests archive months with public visibility
- **THEN** Frux returns each UTC creation month containing at least one of that creator's non-deleted public-visible works exactly once in newest-first order

#### Scenario: Creator loads private archive months
- **WHEN** an authenticated creator requests archive months with private visibility
- **THEN** Frux returns months derived only from that creator's non-deleted private works and does not expose another user's archive

#### Scenario: Visibility is invalid
- **WHEN** an archive-month request supplies a visibility other than public or private
- **THEN** Frux rejects the request as invalid without querying or returning creator work metadata

#### Scenario: Creator has no matching works
- **WHEN** the authenticated creator has no non-deleted works for the requested visibility
- **THEN** Frux returns a successful response with an empty month list

#### Scenario: Archive persistence read fails
- **WHEN** PostgreSQL cannot complete the archive-month query
- **THEN** Frux returns an explicit service error and does not return a success-shaped empty list

### Requirement: Profile Archive Month Query Compatibility
The Web SHALL translate a selected canonical archive month into the inclusive UTC first and last date of that month and SHALL submit those values through the existing creator video's `created_from` and `created_to` filters. The existing range-query API SHALL remain available to compatible clients.

#### Scenario: Creator selects an archive month
- **WHEN** the Web applies archive month `2026-08`
- **THEN** it resets the active creator cursor and queries with `created_from=2026-08-01` and `created_to=2026-08-31`

#### Scenario: Creator clears the archive month
- **WHEN** the creator selects `全部`
- **THEN** the Web queries the active visibility with empty creation-date bounds and preserves the visible keyword filter

#### Scenario: Existing client submits an arbitrary range
- **WHEN** a compatible client submits valid `created_from` and `created_to` values directly
- **THEN** the existing inclusive creator range query continues to validate, filter, order, and paginate as before

### Requirement: Atomic Batch Video Actions
Frux SHALL support idempotent batch actions for making owned videos public, making them private, or deleting them. Each request SHALL contain no more than 100 unique video IDs and SHALL be atomic.

#### Scenario: Valid batch privacy change
- **WHEN** the owner submits a valid batch action with an unused idempotency key
- **THEN** all requested videos change visibility in one transaction and content statistics are updated

#### Scenario: Batch contains unauthorized video
- **WHEN** any requested video is missing or not owned by the authenticated user
- **THEN** the entire operation fails and no requested video is changed

#### Scenario: Idempotent batch replay
- **WHEN** the same user repeats the same batch request with the same idempotency key
- **THEN** the original result is returned without applying changes again

#### Scenario: Idempotency conflict
- **WHEN** the same user reuses an idempotency key for a different batch payload
- **THEN** the API returns 409

### Requirement: Creator Video Collections
Frux SHALL allow an authenticated creator to create, list, update, soft-delete, and manage membership of collections containing their own non-deleted videos.

#### Scenario: Creator creates collection
- **WHEN** an authenticated user submits a valid collection with an idempotency key
- **THEN** a collection owned by that user is created and returned

#### Scenario: Creator adds owned video
- **WHEN** the collection owner adds one of their non-deleted videos
- **THEN** the video becomes a unique ordered member of the collection

#### Scenario: Creator attempts cross-owner membership
- **WHEN** a user attempts to add another user's video or edit another user's collection
- **THEN** the API rejects the operation without changing membership

### Requirement: Collection Visibility
Collections SHALL have public or private visibility. Public collection responses SHALL include only public, published member videos; private collections SHALL be owner-only.

#### Scenario: Visitor opens public collections
- **WHEN** a visitor lists a user's public collections
- **THEN** only active public collections and publicly readable member cards are returned

#### Scenario: Member becomes private
- **WHEN** a public collection member video becomes private
- **THEN** that member is omitted from public collection reads while remaining visible to its owner

#### Scenario: Public collection profile preview is bounded
- **WHEN** an anonymous visitor requests up to 100 public collections
- **THEN** each collection returns at most 3 hydrated readable member cards in `position ASC, video_id ASC` order and reports the total readable `member_count`

#### Scenario: Owner collection listing supports editing
- **WHEN** the owner lists their collections for management
- **THEN** every non-deleted member remains available in stable membership order even though public profile previews are capped

#### Scenario: Collection metadata update returns members
- **WHEN** the owner PATCHes collection metadata
- **THEN** the response contains the collection's current hydrated membership items

#### Scenario: Concurrent collection metadata updates
- **WHEN** concurrent PATCH requests supply different title, description, or visibility fields from stale snapshots
- **THEN** each request persists only its supplied fields and does not overwrite unrelated metadata

#### Scenario: Membership retry is a no-op
- **WHEN** the owner repeats an already-applied add or remove membership request
- **THEN** membership and the collection `updated_at` remain unchanged

### Requirement: Content Statistics
Frux SHALL maintain per-user public-work, private-work, received-like, and collection counts as persistent aggregate statistics and SHALL expose non-negative values through profile responses.

#### Scenario: Visibility change updates counts
- **WHEN** a video changes between public and private visibility
- **THEN** public and private work counts are adjusted in the same transaction without becoming negative

#### Scenario: Public work lifecycle changes
- **WHEN** a public video becomes offline, returns to published, or is deleted
- **THEN** `public_work_count` counts it exactly when it is both published and public

#### Scenario: Reconciliation overlaps an online delta
- **WHEN** startup reconciliation runs while another transaction commits a newer content-stat delta
- **THEN** reconciliation repairs the prior discrepancy without overwriting the newer delta

#### Scenario: Like state updates received likes
- **WHEN** the durable like count for a user's video changes
- **THEN** the author's received-like aggregate is adjusted consistently with the durable interaction result

#### Scenario: Accepted interaction is consumed after privacy change
- **WHEN** a new interaction is accepted while a video is published and public, and the video becomes private before its asynchronous event is consumed
- **THEN** the event is durably persisted exactly once without making the private video publicly readable

#### Scenario: New interaction targets private video
- **WHEN** a user submits a new synchronous interaction request after the video becomes private
- **THEN** the request is rejected as not found and no event is accepted

#### Scenario: Accepted interaction event is redelivered
- **WHEN** Kafka redelivers or replays the same accepted action event
- **THEN** its event receipt, action fact, video count, and author received-like aggregate remain exactly-once

#### Scenario: Publish and fallback persistence both fail
- **WHEN** Redis accepts an action mutation, no broker durably acknowledges it, and synchronous PostgreSQL fallback fails
- **THEN** the API conditionally rolls back only that still-current Redis version, and a retry emits a higher persistable version instead of silently succeeding with `delta=0`

#### Scenario: Publish acknowledgement is uncertain
- **WHEN** the broker may have accepted an event but publisher confirmation is unavailable or times out
- **THEN** synchronous fallback may persist the same version and any later broker delivery is an exactly-once duplicate

#### Scenario: Active action acknowledgement survives fallback failure
- **WHEN** fallback fails after the active primary transport durably acknowledges the stable action event
- **THEN** the API reports failure, confirms the Redis handoff, and does not roll back state that the active broker can later persist

#### Scenario: Mirror-only action acknowledgement remains retryable
- **WHEN** fallback fails after only the non-active mirror transport acknowledges the stable action event
- **THEN** the API reports failure and preserves Redis without confirming the handoff so an idempotent retry republishes to the active transport

#### Scenario: Failed mutation is superseded
- **WHEN** a newer Redis action version replaces a failed mutation before its recovery rollback
- **THEN** recovery does not roll back the newer state

#### Scenario: Older interaction event arrives after newer state
- **WHEN** a newer unlike or unfavorite event is durably applied before an older delayed like or favorite event for the same user, video, and action type
- **THEN** the older event is acknowledged without changing the action state, video count, or author received-like aggregate

#### Scenario: Concurrent workers receive opposing action versions
- **WHEN** workers concurrently persist like and unlike events whose timestamps conflict with their Redis-assigned versions
- **THEN** the greatest version is primary and determines the durable state

#### Scenario: Compatibility events have the same version and timestamp
- **WHEN** distinct compatibility action events for the same fact have equal versions and occurrence timestamps
- **THEN** their event IDs provide a deterministic final tie-break

#### Scenario: Accepted interaction references terminal video
- **WHEN** an action event is invalid or its video is missing or deleted
- **THEN** the Worker classifies it as terminal and the consumer does not requeue it indefinitely

### Requirement: Review-Aware Creator Management
Creator management responses and aggregate statistics SHALL represent pending-review and rejected videos without treating them as public works or allowing visibility changes to bypass lifecycle review.

#### Scenario: Creator queries pending review works
- **WHEN** an authenticated creator queries their non-deleted videos
- **THEN** pending-review and rejected owned videos can be returned with truthful lifecycle status

#### Scenario: Pending public-visible video is counted
- **WHEN** a video has public visibility but remains pending review
- **THEN** it is excluded from `public_work_count`

#### Scenario: Creator changes pending video visibility
- **WHEN** the owner changes a pending-review video between public and private visibility
- **THEN** the lifecycle remains pending review and the video does not become publicly readable

#### Scenario: Rejected video is made public
- **WHEN** the owner requests public visibility for a rejected video
- **THEN** Frux rejects the operation or retains the rejected public-ineligible state without publishing it

### Requirement: Existing Video API Compatibility
Existing simple own-video and public-user-video list endpoints SHALL remain available while the new creator query and collection endpoints are introduced.

#### Scenario: Existing client requests own videos
- **WHEN** an existing client calls `GET /api/users/me/videos`
- **THEN** the endpoint remains valid and returns the existing compatible response shape

### Requirement: Owner-Protected Work Preview
An authenticated creator SHALL be able to open every owned non-deleted work from the creator management surface, including pending-review, processing, rejected, private, and offline states, using owner-authorized temporary media access without changing public eligibility.

#### Scenario: Creator opens a pending work
- **WHEN** the owner activates a pending-review work whose public media URL is blank
- **THEN** the Web requests short-lived access for its media and cover assets and opens the protected WorkViewer

#### Scenario: Creator opens a processed non-public work
- **WHEN** a ready baseline exists for an owned private, pending, rejected, or offline video
- **THEN** the viewer plays the protected baseline and does not expose it through public detail, Feed, or anonymous media routes

#### Scenario: Work is still processing
- **WHEN** only the protected uploaded source or cover is available
- **THEN** the viewer shows the available owner preview with truthful processing state and does not claim that the video is published

#### Scenario: Browser cannot decode the protected source
- **WHEN** the current protected source uses an unsupported browser codec
- **THEN** the viewer retains the cover and metadata, reports that preview playback is unavailable while processing, and offers retry

#### Scenario: Another user requests preview
- **WHEN** a non-owner requests protected asset access for the work
- **THEN** Frux denies access without returning a reusable media location

#### Scenario: Creator closes the viewer
- **WHEN** the owner closes WorkViewer or selects another work
- **THEN** stale protected-access responses cannot reopen or overwrite the current viewer

### Requirement: Private Content Isolation
Private videos and private collections MUST NOT appear in anonymous video details, public profiles, Feed candidates, recommendation hydration, public liked lists, or another user's personal library.

#### Scenario: Private video is requested anonymously
- **WHEN** an anonymous caller requests a private video or receives its stale ID from a cache
- **THEN** the API does not return its media or private metadata

#### Scenario: Hidden video comments are requested anonymously
- **WHEN** an anonymous caller requests comments for a private, offline, deleted, or missing video
- **THEN** the API returns no comments and treats the parent video as not found; comments become readable again only if the video returns to published public state

#### Scenario: Stale local media URL is requested
- **WHEN** an anonymous caller requests a local `/uploads` video or cover belonging to a private, offline, or deleted video
- **THEN** the asset request returns no media, while the owner may read non-deleted media through authenticated Web delivery

#### Scenario: Attacker re-references another upload
- **WHEN** a user attempts to publish another user's protected local video or cover URL
- **THEN** video creation is rejected and the attacker's reference cannot make the asset publicly readable

#### Scenario: Local upload kind does not match the published field
- **WHEN** a user publishes local media from `file`, `avatar`, or `cover`, publishes a cover from another kind, or references an unowned local path
- **THEN** video creation is rejected; local media requires an owned `video` upload and local cover requires an owned `cover` upload

#### Scenario: Existing protected asset is migrated
- **WHEN** migration finds an existing protected local URL referenced by exactly one author
- **THEN** immutable ownership is backfilled to that author and delivery continues to follow public/owner readability rules
