## 1. Admin Video Operations API

- [x] 1.1 Add bounded admin video query input, stable `(created_at, id)` cursor, and result DTOs.
- [x] 1.2 Implement PostgreSQL admin video search filters for lifecycle, author, identifier, keyword, and time range.
- [x] 1.3 Add reasoned version-checked takedown and eligible restoration application services.
- [x] 1.4 Commit enforcement state, cache invalidation intent, and success audit facts atomically.
- [x] 1.5 Add permission-protected handlers and API-flow tests for search, takedown, restore, conflicts, and forbidden access.
- [x] 1.6 Lease and retry the durable enforcement intent in the Worker, marking delivery only after cache and media side effects succeed.

## 2. Typed Admin Web Foundation

- [x] 2.1 Extend session and API types with bounded admin permissions, review cases, evidence, leases, and admin video results.
- [x] 2.2 Add typed `/admin/reviews`, `/admin/reviews/:reviewId`, and `/admin/videos` routes and normalization tests.
- [x] 2.3 Add lazy-loaded admin shell layout and permission-filtered navigation without adding a router dependency.
- [x] 2.4 Add separate typed review and admin-video API modules through `apiRequest<T>`.

## 3. Review Workspace

- [x] 3.1 Build the review queue page with independent loading, error, empty, pagination, and refresh states.
- [x] 3.2 Build the case-detail evidence and immutable history views.
- [x] 3.3 Add claim, lease-renewal, approve, and reject interactions with busy and conflict recovery.
- [x] 3.4 Add Web tests for permission filtering, lease expiry, stale versions, and successful decisions.
- [x] 3.5 Retain pending decision idempotency across response loss and clear cached queue rows on authoritative forbidden responses.

## 4. Content Operations Workspace

- [x] 4.1 Build the admin video search page with typed filters and independent cursor state.
- [x] 4.2 Add takedown and restore confirmation dialogs with reason code, note, and expected version.
- [x] 4.3 Add Web tests for filter-bound cursors, forbidden responses, conflicts, and truthful action outcomes.
- [x] 4.4 Include the current minute in the default `created_to` bound and cover it with a Web regression test.

## 5. Documentation and Validation

- [x] 5.1 Update admin, review, video, product, UI/UX, architecture, and engineering documentation.
- [x] 5.2 Run targeted Go tests, Web tests, the Web production build, the full Go suite, and strict OpenSpec validation.
- [x] 5.3 Re-run targeted and full validation after the correctness fixes and inspect the final diff.
