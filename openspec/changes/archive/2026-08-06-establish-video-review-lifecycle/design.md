## Context

`Video.NewProcessing` and the legacy constructor currently create published videos immediately. Public reads are gated by `StatusPublished`, public visibility, and ready media. Review must become another independent eligibility condition without overloading visibility or media status, and existing published data must remain readable.

## Goals / Non-Goals

**Goals:**

- Introduce explicit pending-review and rejected lifecycle states.
- Prevent all public discovery and media delivery before review approval.
- Preserve creator visibility controls and existing published content.
- Centralize valid lifecycle transitions in the video domain.

**Non-Goals:**

- Review task persistence, automated moderation, human assignment, appeals, or policy taxonomy.
- Re-reviewing historical published videos.
- Allowing creators to bypass review by changing visibility.

## Decisions

### Add new numeric states without reinterpreting existing values

Keep draft `1`, published `2`, offline `3`, and deleted `4`; add pending review `5` and rejected `6`. This avoids changing the meaning of persisted rows.

### New videos start pending review

Both production-media and legacy-compatible creation paths set pending review and leave `published_at` empty. Approval moves the video to published and sets `published_at` once. Rejection moves it to rejected without a publication timestamp.

Alternative: add a separate review-status column while leaving lifecycle published. Rejected because too many existing queries equate published with public eligibility and would be easy to miss.

### Keep media and review as independent gates

Approval may occur before or after media processing. A video is publicly readable only when lifecycle is published, visibility is public, and the media baseline is ready. Media readiness never changes review state.

### Define explicit transition methods

The domain exposes approve, reject, take-offline, and restore operations with source-state validation. Deleted is terminal. Restoring offline content requires prior approval and does not change the original publication time.

### Treat old published rows as approved

Migration adds no review task for historical rows. Their existing published status remains the compatibility proof of approval.

### Use bounded revocation for public media caches

Public media objects use versioned exposure-generation URLs and a 60-second revalidating cache rather than year-long immutable caching. Private, offline, rejected, failed-media, and deleted transitions synchronously demote promoted variants to the protected prefix, while local `/media` reads also re-check current database eligibility. Variant moves use compare-and-swap persistence and retain the protected copy so promotion/demotion races cannot delete the only object. A successful revocation may therefore have a short cache window, but a failed demotion returns an error and idempotent retries continue the revocation. The first rollout requires an operational purge of legacy `media/*` entries that were previously advertised with one-year caching.

Alternative: permanently public immutable URLs. Rejected because saved URLs would remain playable after a governance action.

Alternative: authorization on every production media segment request. Deferred because it would replace the existing CDN/public-prefix delivery architecture and exceeds this lifecycle change.

## Risks / Trade-offs

- [A public query misses the new gate] -> Reuse `IsPubliclyReadable` semantics and audit every direct status predicate through targeted tests.
- [Creator counters drift during transitions] -> Update lifecycle and content statistics in one transaction and extend reconciliation.
- [Approval races media completion] -> Keep independent idempotent projections; either event may arrive first.
- [Clients assume every created video is published] -> Return explicit status and update Web status labels before changing creation behavior.
- [CDN caches delay revocation] -> Bound public caching to 60 seconds, require revalidation, demote objects synchronously, and retry failed revocations idempotently.

## Migration Plan

1. Add constants, transition tests, DTO labels, and database constraints.
2. Update all public filters and media authorization to exclude pending/rejected content.
3. Deploy the migration while constructors still use published.
4. Switch new constructors to pending review and update API-flow expectations.
5. Roll back by stopping new creation first; existing pending/rejected rows remain non-public until explicitly migrated.

## Open Questions

- Whether a later metadata edit should increment a review subject version and return an approved video to pending review.
