## Context

The review backend already provides a stable available queue, immutable evidence/history, version-checked leases, and idempotent decisions. The Web console exposes those storage and concurrency concepts directly, keeps the only usable lease token in component memory, and reads the subject's public `media_url`. A claim therefore removes work from the available queue, a refresh loses the token, and pending protected media cannot be previewed.

The change crosses review persistence/application/HTTP, media access control, and the strict TypeScript admin UI. It must not make pending media public, persist raw lease tokens, weaken case-version checks, or introduce a routing library.

## Goals / Non-Goals

**Goals:**

- Present review work as tasks rather than cases and leases.
- Make protected pending media previewable only to authorized review readers.
- Give reviewers stable available, in-progress, and recently completed views.
- Restore an in-progress task after a refresh without storing a reusable lease token in browser persistence.
- Keep an actively viewed task alive and make release/expiry/conflict states explicit.
- Display provider/model/policy provenance and identify the known seeded test provider honestly.

**Non-Goals:**

- Changing review outcomes, policy thresholds, reason codes, or public video eligibility.
- Adding appeals, multi-reviewer consensus, bulk decisions, or reviewer assignment by managers.
- Making arbitrary evidence references fetchable.
- Persisting plaintext lease credentials or exposing internal provider payloads.

## Decisions

### Extend the existing queue API with explicit scopes

`GET /api/admin/review/cases` gains a required/defaulted `scope` of `available`, `mine`, or `recent`.

- `available` preserves the current `priority DESC, created_at ASC, id ASC` order and only returns currently claimable work.
- `mine` returns pending-human cases actively leased to the current reviewer, ordered by `lease_expires_at ASC, priority DESC, id ASC`.
- `recent` returns cases decided by the current reviewer, ordered by `decided_at DESC, id DESC`, within a bounded retention window and stable cursor.

The cursor binds the scope and all filters. Reusing the current endpoint avoids a second queue representation while preserving backward compatibility through `scope=available`.

Alternative considered: merge available and owned tasks into one result. Rejected because rows would have different ordering keys and actions, making pagination and empty/error states ambiguous.

### Resume rotates the lease token

Add `POST /api/admin/review/cases/{caseId}/lease/resume`. It requires `review.decide`, the current authenticated reviewer, current case/review version, and an unexpired active assignment. The transaction:

1. locks the case and subject;
2. verifies current ownership and reviewability;
3. generates a new 256-bit opaque token;
4. replaces the stored token hash, extends the expiry by the normal bounded duration, increments case version, and records a `resumed` assignment event;
5. returns the new token exactly once.

The old token becomes invalid. The Web client keeps the resumed token only in detail-page memory. This restores work after reload without putting a bearer-equivalent lease secret in `localStorage` or `sessionStorage`.

Alternative considered: make claim idempotently return the old token. Rejected because the database stores only a hash and cannot recover the plaintext token.

### Keep-alive is automatic but visible

While an owned task detail is active, the Web client renews before half the remaining lease duration elapses and displays a server-time-derived countdown labeled “审核占用至”. Renewal pauses when no token is held and stops after decision, release, expiry, permission failure, or version conflict.

The page also exposes “延长审核时间” for an immediate manual retry and “放回待处理” for explicit release. Navigation does not silently release work because browser unload delivery is unreliable; the user chooses whether to retain or release it.

### Review preview uses dedicated short-lived access

Add `GET /api/admin/review/cases/{caseId}/preview-access`. The review service verifies `review.read`, current case/video identity, matching review version, non-deleted subject state, and available media assets before invoking a narrow media-access signer.

The response contains typed media and cover URLs plus `expires_at`, with a maximum five-minute lifetime:

- object storage returns signed protected-object URLs;
- local storage returns an authenticated, expiring admin media URL;
- neither path changes `video.media_url`, promotes protected objects, or grants anonymous access.

The client refreshes preview access shortly before expiry and clears URLs on 401/403/conflict. The detail response remains bounded and does not embed long-lived media credentials.

Alternative considered: let `review.read` call the creator's asset-access endpoint. Rejected because ownership semantics differ from review authorization and would couple admin access to creator APIs.

### Evidence presentation is additive

The detail DTO exposes existing provider, model version, policy version, confidence, label, and evidence references as typed presentation data. The Web client:

- marks the reserved `manual-seed` provider as test data;
- treats all other pre-production rows as “来源未验证” unless the backend marks them as production in the later provider change;
- translates registered labels while preserving the canonical value;
- never turns arbitrary evidence strings into clickable external URLs.

This change does not claim that a non-seed provider is production-generated. Explicit persisted source classification is owned by `integrate-production-moderation-provider`.

### User-facing language is isolated from API/domain names

Backend route and domain names can retain `case`, `claim`, and `lease` for compatibility. The admin UI consistently uses “审核任务”, “视频内容”, “审核记录”, “开始审核”, “审核占用至”, “延长审核时间”, and “放回待处理”. API error mapping translates version/lease conflicts into task-oriented guidance.

## Risks / Trade-offs

- [Signed preview URLs can leak through logs or copied links] → Keep the TTL at five minutes or less, redact query strings from application logs, and require current review authorization before issuance.
- [Automatic renewal can keep abandoned work occupied] → Renew only while the detail page is active, preserve the bounded server lease, and provide explicit release plus normal expiry recovery.
- [Resume could be abused to rotate tokens repeatedly] → Require current ownership/version, rate-limit the mutation, increment version, and record immutable resume history.
- [Three scopes add query/index cost] → Use scope-specific indexed queries and stable bounded pagination; do not combine them with OR-heavy dynamic SQL.
- [Known seed detection is provider-name based] → Label only the reserved seed provider as test and use “unverified” for all others until persisted source classification lands.

## Migration Plan

1. Add the assignment-history event value and any indexes needed by `mine` and `recent`; deploy backward-compatible repository queries.
2. Deploy resume and preview-access APIs while the old Web UI still uses only available/detail/claim.
3. Deploy the new scoped queue and detail UI.
4. Monitor resume, renew, release, preview issuance, and authorization failures.
5. Roll back the Web UI independently if needed; added APIs and history values remain backward compatible.

## Open Questions

None. Production evidence classification and real inference remain in the separate moderation-provider change.
