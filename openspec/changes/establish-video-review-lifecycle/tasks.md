## 1. Video Lifecycle Domain

- [ ] 1.1 Add pending-review and rejected status constants without changing existing numeric meanings.
- [ ] 1.2 Implement approve, reject, take-offline, restore, and public-eligibility domain methods.
- [ ] 1.3 Add exhaustive transition-table tests including idempotency and terminal deleted behavior.
- [ ] 1.4 Update video restoration and compatibility defaults for all known lifecycle values.

## 2. Persistence and Migration

- [ ] 2.1 Update video model constraints, status conversion, and migration registration.
- [ ] 2.2 Preserve existing published rows and publication timestamps through PostgreSQL migration tests.
- [ ] 2.3 Update lifecycle persistence and content-stat transactions for pending, rejected, offline, and restored states.
- [ ] 2.4 Extend content-stat reconciliation tests for review-aware public-work counts.

## 3. Creation and Public Read Gates

- [ ] 3.1 Change production and compatibility video constructors to create pending-review videos with no publication time.
- [ ] 3.2 Update media-publication behavior so media readiness never publishes a pending video.
- [ ] 3.3 Update Feed, search, recommendation, profile, collection, library, comment, preload, and detail queries to reuse review-aware readability.
- [ ] 3.4 Update local and object-media authorization so pending and rejected assets remain owner-protected.
- [ ] 3.5 Add stale-cache and hydration tests proving pending and rejected IDs are removed from public responses.

## 4. API and Web Compatibility

- [ ] 4.1 Update video DTO status fields and creator query filters for pending-review and rejected content.
- [ ] 4.2 Update Web video status types, labels, creator grids, and upload success state.
- [ ] 4.3 Update API-flow and Web tests for newly created pending videos and review-gated media readiness.

## 5. Documentation and Validation

- [ ] 5.1 Update video, review, product, architecture, UI/UX, and engineering documentation.
- [ ] 5.2 Run targeted video/media/feed tests, the full Go suite, Web tests/build, and strict OpenSpec validation.
