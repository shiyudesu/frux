## 1. Admin Video Operations API

- [ ] 1.1 Add bounded admin video query input, stable `(created_at, id)` cursor, and result DTOs.
- [ ] 1.2 Implement PostgreSQL admin video search filters for lifecycle, author, identifier, keyword, and time range.
- [ ] 1.3 Add reasoned version-checked takedown and eligible restoration application services.
- [ ] 1.4 Commit enforcement state, cache invalidation intent, and success audit facts atomically.
- [ ] 1.5 Add permission-protected handlers and API-flow tests for search, takedown, restore, conflicts, and forbidden access.

## 2. Typed Admin Web Foundation

- [ ] 2.1 Extend session and API types with bounded admin permissions, review cases, evidence, leases, and admin video results.
- [ ] 2.2 Add typed `/admin/reviews`, `/admin/reviews/:reviewId`, and `/admin/videos` routes and normalization tests.
- [ ] 2.3 Add lazy-loaded admin shell layout and permission-filtered navigation without adding a router dependency.
- [ ] 2.4 Add separate typed review and admin-video API modules through `apiRequest<T>`.

## 3. Review Workspace

- [ ] 3.1 Build the review queue page with independent loading, error, empty, pagination, and refresh states.
- [ ] 3.2 Build the case-detail evidence and immutable history views.
- [ ] 3.3 Add claim, lease-renewal, approve, and reject interactions with busy and conflict recovery.
- [ ] 3.4 Add Web tests for permission filtering, lease expiry, stale versions, and successful decisions.

## 4. Content Operations Workspace

- [ ] 4.1 Build the admin video search page with typed filters and independent cursor state.
- [ ] 4.2 Add takedown and restore confirmation dialogs with reason code, note, and expected version.
- [ ] 4.3 Add Web tests for filter-bound cursors, forbidden responses, conflicts, and truthful action outcomes.

## 5. Documentation and Validation

- [ ] 5.1 Update admin, review, video, product, UI/UX, architecture, and engineering documentation.
- [ ] 5.2 Run targeted Go tests, Web tests, the Web production build, the full Go suite, and strict OpenSpec validation.
