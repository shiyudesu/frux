## 1. Review Work Queries and Lease Resume

- [ ] 1.1 Add typed `available`, `mine`, and `recent` queue scopes and scope-bound cursor payloads to the review application boundary.
- [ ] 1.2 Extend the review repository interface with current-reviewer active-work and recently-decided queries using the specified stable orders.
- [ ] 1.3 Implement scope-specific PostgreSQL queries and supporting indexes without changing available-queue claimability semantics.
- [ ] 1.4 Add the `resumed` assignment-history event and migration compatibility for existing history rows.
- [ ] 1.5 Implement the resume transaction with ownership/version checks, database-time expiry validation, token rotation, bounded extension, and immutable history.
- [ ] 1.6 Add unit and repository tests for scoped pagination, cursor/filter conflicts, reviewer isolation, expired work, concurrent resume, and stale versions.

## 2. Protected Review Preview

- [ ] 2.1 Define a narrow review-media access interface and typed preview response containing bounded media/cover access plus expiration.
- [ ] 2.2 Implement review subject/version/lifecycle authorization before any preview URL is issued.
- [ ] 2.3 Implement object-storage signing for protected review media with a maximum five-minute lifetime.
- [ ] 2.4 Implement expiring authenticated local-media preview access without changing public `/uploads` or `/media` authorization.
- [ ] 2.5 Add preview tests for authorized readers, missing permission, stale review version, deleted/unavailable subjects, expiration, and anonymous reuse.

## 3. Admin Review HTTP Surface

- [ ] 3.1 Extend queue request/response DTOs with validated scope and scope-specific rows while preserving `scope=available` compatibility.
- [ ] 3.2 Add the lease-resume endpoint with stable permission, version, ownership, expiry, and conflict error mapping.
- [ ] 3.3 Add the preview-access endpoint and ensure credentials/signed query data are not logged or embedded in ordinary case detail.
- [ ] 3.4 Extend detail presentation fields for provider, model, policy, labels, confidence, bounded evidence text, and reserved seed-provider test classification.
- [ ] 3.5 Add API-flow tests covering all queue scopes, resume/release/renew/decision transitions, forbidden access, and protected preview.

## 4. Review Console Web Workflow

- [ ] 4.1 Update typed review API functions and domain types for scoped pages, resume, preview access, and structured provenance.
- [ ] 4.2 Replace user-visible “案件/主体/不可变历史/领取案件/租约/续租” copy with the approved task-oriented terminology.
- [ ] 4.3 Implement “待我处理 / 我正在审核 / 最近完成” tabs with independent loading, error, empty, pagination, refresh, and 403 cache-clearing states.
- [ ] 4.4 Implement start-review navigation so claimed work appears in “我正在审核” immediately.
- [ ] 4.5 Add protected video/cover preview loading, expiry refresh, unavailable state, and URL cleanup on permission or version failure.
- [ ] 4.6 Add automatic bounded keep-alive, visible server-derived occupancy countdown, manual “延长审核时间”, and stop conditions.
- [ ] 4.7 Resume an owned task after reload through token rotation while keeping the opaque token only in component memory.
- [ ] 4.8 Wire “放回待处理”, retain-on-navigation guidance, expired/conflict recovery, and recently completed movement after decision.
- [ ] 4.9 Render translated registered labels and provider/model/policy details while marking `manual-seed` as test evidence and unknown sources as unverified.

## 5. Verification and Documentation

- [ ] 5.1 Add frontend behavior tests for tab isolation, terminology, claim-to-mine movement, reload resume, keep-alive, release, expiry, preview denial, and seeded evidence labels.
- [ ] 5.2 Run targeted Go review/media/API-flow tests and the strict Web production build.
- [ ] 5.3 Update `docs/modules/review.md`, `docs/modules/admin.md`, `docs/modules/video.md`, and related operator/user guidance with the revised workflow and preview security boundary.
- [ ] 5.4 Perform an authenticated browser review flow with two reviewer sessions and confirm pending media remains anonymously inaccessible.
