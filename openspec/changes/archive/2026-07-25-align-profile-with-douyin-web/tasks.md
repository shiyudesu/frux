## 1. Persistence and Migration Foundations

- [x] 1.1 Extend the account domain and persistence model with validated gender and add `account_profile_setting` with privacy-first defaults.
- [x] 1.2 Add video visibility to the video domain/model with public migration defaults and public-read guards independent from lifecycle status.
- [x] 1.3 Add `user_content_stat` and transactional helpers for public/private work, received-like, and collection counts.
- [x] 1.4 Add exposure-owned `video_view_history` persistence and indexes for latest user-video watch state.
- [x] 1.5 Create the library domain and persistence model for idempotent `user_watch_later` facts.
- [x] 1.6 Add `video_collection`, `video_collection_item`, and `video_batch_operation` models with ownership, uniqueness, ordering, and idempotency constraints.
- [x] 1.7 Register all new models and module-specific indexes/backfills in the shared migration path used by API and Worker.
- [x] 1.8 Add migration tests proving existing videos become public, profile settings default private, aggregates backfill correctly, and repeated migration is safe.

## 2. Profile Aggregate and Privacy APIs

- [x] 2.1 Extend account profile update rules and DTOs to accept supported gender values without changing existing nickname/avatar/bio behavior.
- [x] 2.2 Implement current/public profile aggregate reads for relation counts, public work count, received-like count, gender, and owner-only settings.
- [x] 2.3 Implement authenticated `GET/PATCH /api/users/me/profile-settings` with partial updates and explicit visibility validation.
- [x] 2.4 Wire account/profile repositories, services, handlers, routes, and response types through the router composition root.
- [x] 2.5 Add account API-flow and package tests for profile fields, privacy defaults, valid updates, invalid values, and public response redaction.

## 3. Creator Work Visibility and Queries

- [x] 3.1 Add video domain methods for owner-authorized public/private transitions while preserving lifecycle status and publication time.
- [x] 3.2 Implement creator video cursor encoding and repository queries for visibility, escaped keyword search, inclusive date range, and `created_at/id` ordering.
- [x] 3.3 Implement `POST /api/users/me/video-queries` DTO parsing, validation, service behavior, response hydration, and routes.
- [x] 3.4 Preserve existing own-video and public-user-video endpoints and add tests proving their response contracts remain compatible.
- [x] 3.5 Enforce public visibility in video detail, public user works, Feed candidates, recommendation hydration, and cached card fallback paths.
- [x] 3.6 Invalidate affected Feed/card/index caches when a video becomes private, public, offline, or deleted.

## 4. Atomic Batch Work Management

- [x] 4.1 Implement batch request normalization, a stable request fingerprint, maximum-100 unique ID validation, and supported action validation.
- [x] 4.2 Implement transactional ownership locking and all-or-nothing `make_public`, `make_private`, and `delete` operations.
- [x] 4.3 Persist batch operation idempotency results and return 409 when the same key is reused for a different payload.
- [x] 4.4 Expose `POST /api/users/me/video-batch-actions` and map validation, permission, conflict, and missing-resource errors.
- [x] 4.5 Add API-flow and repository tests for successful batches, mixed ownership rollback, repeated replay, conflict, and aggregate count updates.

## 5. Creator Video Collections

- [x] 5.1 Implement collection domain entities, visibility/status invariants, title/description limits, and owner-authorized mutations.
- [x] 5.2 Implement collection and membership repositories with stable collection cursors and ordered unique members.
- [x] 5.3 Implement current-user collection create/list/update/delete and add/remove-video services with idempotent creation.
- [x] 5.4 Implement public collection listing that filters private collections and unreadable member videos.
- [x] 5.5 Register collection handlers and routes for current-user management and public-user reads.
- [x] 5.6 Add tests for CRUD, ownership isolation, duplicate membership, member ordering, private visibility, and public filtering.

## 6. Likes, Favorites, and Profile Library Aggregation

- [x] 6.1 Add interaction action cursors and repository methods for active LIKE/FAVORITE facts ordered by `updated_at/video_id`.
- [x] 6.2 Add a video-catalog batch adapter that hydrates ordered personal-library IDs and filters deleted, offline, and unauthorized private content.
- [x] 6.3 Implement the library application service for current-user liked and favorite video pages, including bounded candidate replenishment.
- [x] 6.4 Implement privacy-checked public liked-video listing while keeping favorites owner-only.
- [x] 6.5 Expose typed liked/favorite list endpoints with cursor validation, authentication, and privacy error mapping.
- [x] 6.6 Add repository, service, and API-flow tests for ordering, canceled actions, inaccessible videos, pagination, privacy, and anonymous access.

## 7. Watch History and Watch Later

- [x] 7.1 Update exposure persistence so play, complete, and skip events upsert watch history in the same transaction while exposed-only events do not.
- [x] 7.2 Implement watch-history cursor listing, single-item deletion, and clear-all projection deletion without deleting raw events.
- [x] 7.3 Implement library watch-later set/remove/list behavior with natural idempotency and readable-video validation.
- [x] 7.4 Expose watch-history and watch-later handlers and routes with typed responses and stable cursors.
- [x] 7.5 Add tests for history upserts, progress/completion updates, exposed-only exclusion, deletion semantics, watch-later replay, and inaccessible video filtering.

## 8. Content Statistics Consistency

- [x] 8.1 Update video create, visibility, delete, and collection transactions to maintain `user_content_stat` without negative counts.
- [x] 8.2 Update durable interaction write paths and Worker action consumption to adjust the video author's received-like aggregate exactly once.
- [x] 8.3 Add reconciliation helpers/tests that rebuild content statistics from video, video_stat, and collection facts.
- [x] 8.4 Add concurrency tests for visibility changes, repeated interaction events, and aggregate clamping.

## 9. Typed Web Data Layer

- [x] 9.1 Extend strict frontend profile, settings, creator-query, batch-action, collection, library-video, history, and watch-later types.
- [x] 9.2 Add typed account/profile-setting API functions while preserving existing account API call signatures.
- [x] 9.3 Add typed creator-management and personal-library API modules over `apiRequest<T>`.
- [x] 9.4 Implement `useCreatorContent` with independent published/private/collection pages, filters, pagination, retry, and batch mutation refresh.
- [x] 9.5 Implement `useProfileLibrary` with isolated likes, favorites, history, and watch-later state plus optimistic watch-later removal.
- [x] 9.6 Reuse the existing recommendation Feed API for the profile Recommend tab without duplicating recommendation endpoints.

## 10. Douyin-Style Profile Interface

- [x] 10.1 Refactor own/public profile composition into reusable `ProfileHero`, primary-tabs, work-tabs, toolbar, grid, collection-grid, and empty-state components.
- [x] 10.2 Rebuild the own-profile header as a full-width banner with 112px avatar, editable identity, inline relation/work/received-like statistics, account identifier, and gender.
- [x] 10.3 Implement real Works, Recommend, Likes, Favorites, Watch History, and Watch Later primary tabs, omitting Short Drama and Appointments.
- [x] 10.4 Implement Published, Private Works, and Collections secondary views with keyword/date filters and batch-management selection mode.
- [x] 10.5 Restyle video cards to the measured six-column-capable 3:4 layout with like overlay, optional pinned/status treatment, and single-line caption while preserving work viewing.
- [x] 10.6 Rebuild profile editing as the measured centered dark dialog with avatar camera affordance, character counts, gender input, cancel/save states, focus management, and upload behavior.
- [x] 10.7 Update the public profile to share the banner/grid system and conditionally expose public liked videos and collections without owner-only controls.
- [x] 10.8 Implement loading skeletons and specific empty/error states for every primary and secondary view without invented content.
- [x] 10.9 Update semantic CSS tokens and profile styles for 1440px wide desktop and 901-1279px compact desktop while preserving the existing usable mobile layout.
- [x] 10.10 Add accessible tab semantics, keyboard operation, visible focus, dialog focus trapping, reduced motion, and minimum touch targets.

## 11. Documentation and Verification

- [x] 11.1 Update product scope and module documentation for account, video, interaction, exposure, library, collections, and the expanded Web personal profile.
- [x] 11.2 Update architecture, engineering, UI/UX, quick-read, and module index documentation for new tables, module wiring, APIs, privacy, and user flows.
- [x] 11.3 Run targeted Go package and API-flow tests for each implemented module, then run `go test ./...`.
- [x] 11.4 Run the strict frontend production build with `pnpm -C apps/web run build`.
- [x] 11.5 Validate Compose configuration if migration or service wiring changes affect startup.
- [x] 11.6 Capture and compare own/public profile screenshots at 1440px and compact desktop through Windows Chrome DevTools, checking header geometry, tabs, filters, grids, modal, privacy, and empty states.
- [x] 11.7 Run `openspec validate --all --strict` and reconcile all proposal, design, specs, tasks, and documentation with the implemented behavior.

## 12. Code Review Remediation

- [x] 12.1 Protect local video and cover delivery by lifecycle, visibility, ownership, and authenticated Web asset cookies.
- [x] 12.2 Stabilize session actions and profile effect dependencies to prevent repeated profile/following requests.
- [x] 12.3 Reset public-profile state by user ID and reject stale profile, works, relation, likes, and collection responses.
- [x] 12.4 Count public works only when videos are both published and public across lifecycle transitions.
- [x] 12.5 Reconcile content statistics with snapshot deltas so concurrent online updates are preserved.
- [x] 12.6 Order watch-history projection updates by `(created_at, event_id)` to reject older events.
- [x] 12.7 Hydrate collection PATCH membership and touch collection ordering only for real membership changes.
- [x] 12.8 Preserve collection-create idempotency keys across same-payload retries and rotate them on change or success.
- [x] 12.9 Restore only failed Watch Later optimistic removals.
- [x] 12.10 Add independent collection-editor work search and pagination.
- [x] 12.11 Add focused backend regression coverage and rerun project validation.

## 13. Final High-Confidence Remediation

- [x] 13.1 Add immutable protected local-upload ownership, migration backfill, authenticated recording, publish validation, and ownership-aware delivery.
- [x] 13.2 Make the Web immediately clear local state plus the client-controlled asset-active marker even when logout is offline.
- [x] 13.3 Add a transactional combined profile/privacy update boundary while preserving separate profile-settings endpoints.
- [x] 13.4 Remove public-favorites controls and claims from the Web editor while retaining backend compatibility.
- [x] 13.5 Run full Go tests/build, strict Web build, Compose validation, OpenSpec validation, and final diff review.

## 14. Final Review Remediation

- [x] 14.1 Stop per-response asset-cookie refresh, require a client-controlled active marker for Cookie identity, and cover late-response behavior.
- [x] 14.2 Make local logout authoritative offline and keep the stateless server logout response free of stale Cookie mutations.
- [x] 14.3 Require owned `video` and `cover` upload kinds for local publishing and reject bypass paths.
- [x] 14.4 Persist profile and setting partial updates by supplied column so concurrent requests preserve unrelated fields.
- [x] 14.5 Preserve open profile-editor drafts across background parent rerenders.
- [x] 14.6 Preserve collection metadata and search drafts across membership-driven collection refreshes.

## 15. Remaining Confirmed Regression Fixes

- [x] 15.1 Run watch-history raw-event backfill once behind a durable migration marker and cover delete/clear followed by startup rerun.
- [x] 15.2 Prevent stale logout responses from clearing a newer login while preserving immediate local private-asset denial.
- [x] 15.3 Replenish Watch History and Watch Later pages across unreadable candidates within a bounded number of rounds.
- [x] 15.4 Persist only supplied collection PATCH columns and cover partial, concurrent, and hydrated-response behavior.
- [x] 15.5 Run full Go tests/build, strict Web build, Compose validation, OpenSpec strict validation, and final diff review.

## 16. Final Privacy and Profile-Stat Regressions

- [x] 16.1 Require anonymous comment listing to verify the parent video is published and public, preserving authorized comment mutations.
- [x] 16.2 Refetch the current profile after batch work and collection create/delete mutations through a stable race-safe callback.
- [x] 16.3 Add visibility-transition regressions and rerun full Go, Web, Compose, OpenSpec, and diff validation.

## 17. Async Interaction Queue Remediation

- [x] 17.1 Separate synchronous public-video interaction validation from accepted asynchronous event persistence.
- [x] 17.2 Add durable event-ID receipts, exact duplicate handling, and received-like aggregate correctness across privacy changes.
- [x] 17.3 Classify invalid, conflicting, missing, and deleted action events as terminal so RabbitMQ does not requeue them indefinitely.
- [x] 17.4 Add focused service, repository, and Worker regressions and rerun full Go, Web, Compose, OpenSpec, and diff validation.

## 18. Async Interaction Event Ordering

- [x] 18.1 Persist and atomically enforce latest `(occurred_at, event_id)` ordering per user, video, and action type, treating duplicate and stale events as successful no-ops.
- [x] 18.2 Backfill existing action rows and preserve accepted-event fallback, terminal-event, and retryable database-error behavior.
- [x] 18.3 Add focused PostgreSQL, service, and Worker regressions for delayed older events, equal timestamps, duplicate delivery, and aggregate stability.
- [x] 18.4 Update interaction/OpenSpec documentation and run full Go, Web, Compose, OpenSpec, and diff validation.

## 19. Library Pagination Round Bound

- [x] 19.1 Apply the existing bounded replenishment-round guard to Watch History and Watch Later while preserving resumable cursor pagination.
- [x] 19.2 Cover many unreadable candidates, round limiting, and readable older items across the continuation cursor.
- [x] 19.3 Update library/OpenSpec documentation and run full Go, Web, Compose, OpenSpec, and diff validation.

## 20. Final Reliability Pass

- [x] 20.1 Add publisher confirms, retry-safe event replay, conditional same-version Redis rollback, and failure-injection coverage for publish/persistence failure, acknowledgement uncertainty, and a concurrent superseding mutation.
- [x] 20.2 Allocate atomic per-action Redis versions, persist them in events/action rows/receipts, make version the primary Worker order, and safely backfill legacy rows as version zero.
- [x] 20.3 Add a direct authenticated single-target follow-state API and prevent pending relationship reads from overwriting successful mutations.
- [x] 20.4 Guard relation-modal pagination by request generation, active tab, and open state so stale pages cannot cross tabs or append after close/reset.
- [x] 20.5 Invalidate active history requests before clear and block new history pages during clear so stale results cannot resurrect entries.
- [x] 20.6 Run full Go/PostgreSQL tests and builds, strict Web build, Compose validation, OpenSpec strict validation, and final diff review.

## 21. Public Collection Listing Performance

- [x] 21.1 Replace per-collection membership and video hydration with fixed-count batched repository queries while preserving collection and member ordering.
- [x] 21.2 Cap public profile collection previews at three readable member cards, expose readable `member_count`, and keep owner collection lists complete for editing.
- [x] 21.3 Add PostgreSQL query-count coverage for 100 collections, ordering, unreadable filtering, owner completeness, and the public preview cap.
- [x] 21.4 Update collection DTO/Web/docs/OpenSpec behavior and run full Go, Web, Compose, OpenSpec, and diff validation.
