## 1. Video Lifecycle Domain

- [x] 1.1 Add pending-review and rejected status constants without changing existing numeric meanings.
- [x] 1.2 Implement approve, reject, take-offline, restore, and public-eligibility domain methods.
- [x] 1.3 Add exhaustive transition-table tests including idempotency and terminal deleted behavior.
- [x] 1.4 Update video restoration and compatibility defaults for all known lifecycle values.

## 2. Persistence and Migration

- [x] 2.1 Update video model constraints, status conversion, and migration registration.
- [x] 2.2 Preserve existing published rows and publication timestamps through PostgreSQL migration tests.
- [x] 2.3 Update lifecycle persistence and content-stat transactions for pending, rejected, offline, and restored states.
- [x] 2.4 Extend content-stat reconciliation tests for review-aware public-work counts.

## 3. Creation and Public Read Gates

- [x] 3.1 Change production and compatibility video constructors to create pending-review videos with no publication time.
- [x] 3.2 Update media-publication behavior so media readiness never publishes a pending video.
- [x] 3.3 Update Feed, search, recommendation, profile, collection, library, comment, preload, and detail queries to reuse review-aware readability.
- [x] 3.4 Update local and object-media authorization so pending and rejected assets remain owner-protected.
- [x] 3.5 Add stale-cache and hydration tests proving pending and rejected IDs are removed from public responses.

## 4. API and Web Compatibility

- [x] 4.1 Update video DTO status fields and creator query filters for pending-review and rejected content.
- [x] 4.2 Update Web video status types, labels, creator grids, and upload success state.
- [x] 4.3 Update API-flow and Web tests for newly created pending videos and review-gated media readiness.

## 5. Documentation and Validation

- [x] 5.1 Update video, review, product, architecture, UI/UX, and engineering documentation.
- [x] 5.2 Run targeted video/media/feed tests, the full Go suite, Web tests/build, and strict OpenSpec validation.
