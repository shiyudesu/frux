## Why

The review console exposes backend concurrency terminology, cannot preview protected pending videos, and hides work after it is claimed. Reviewers need a task-oriented workflow that preserves the existing concurrency guarantees without making internal leases and case mechanics the primary user experience.

## What Changes

- Replace user-visible case, subject, immutable-history, claim, and lease terminology with review-task language.
- Add permission-checked, short-lived review preview access for protected video and cover assets without restoring public media URLs.
- Add available, in-progress, and recently completed review queue scopes for the current reviewer.
- Let a reviewer resume, automatically keep alive, explicitly retain, or release in-progress work while preserving opaque lease-token security.
- Show bounded machine-evidence provenance and distinguish test, production-provider, and recovery evidence.
- Preserve version conflicts, single-reviewer ownership, immutable assignment history, and idempotent decisions behind the revised workflow.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `human-review-workflow`: Add reviewer-owned work retrieval and secure resume behavior while retaining lease and decision invariants.
- `production-media-delivery`: Authorize short-lived protected media access for eligible review principals.
- `content-operations-console`: Replace infrastructure-oriented review UX with task-oriented queue, preview, provenance, keep-alive, resume, and release behavior.

## Impact

Affected areas include review application and persistence queries, admin review HTTP DTOs and handlers, protected media authorization/signing, review queue/detail Web pages, typed review API clients, and review API-flow and frontend tests. No public media authorization is relaxed.
