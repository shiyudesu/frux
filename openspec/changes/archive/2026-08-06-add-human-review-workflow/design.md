## Context

Automated review produces cases that require human judgment. Reviewers need stable work selection, but a long database transaction cannot be held while a person inspects content. Assignment therefore needs an expiring lease and optimistic decision checks. Final decisions must update review, video, and audit state together.

## Goals / Non-Goals

**Goals:**

- Provide a prioritized, stable, permission-protected human queue.
- Prevent silent concurrent decisions through leases and case versions.
- Make approve/reject decisions idempotent and atomically enforced.
- Preserve complete automated and human decision history.

**Non-Goals:**

- Web UI, appeals, consensus review, reviewer scoring, scheduling, or payroll.
- Editing machine evidence or prior decisions.
- Bulk decisions.

## Decisions

### Use priority and age ordering

Cases order by `priority DESC, created_at ASC, id ASC`. The signed cursor binds filters and the complete sort tuple. Priority is bounded to `1..100` for pending-human cases and is derived deterministically from the confidence of the signal that triggered human routing; default-human routing uses priority `1`. It is persisted with the routing transition and is never supplied by the browser.

### Use expiring leases with opaque tokens

Claiming or assigning a case records reviewer ID, lease token hash, lease expiry, and case version. A decision requires the matching reviewer, unexpired token, and expected case version. Expired leases return the case to the available queue through reads and reconciliation.

Queue reads treat `lease_expires_at <= clock_timestamp()` as available directly, so pagination does not depend on a bounded recovery batch. Claiming such a row records the expired history event before the new claim.

Queue reads also require the joined video to remain pending review at the case review version. Claim locks both case and video; a terminal subject cancels the case and a newer review version supersedes it, with one immutable history event, before returning the existing subject-state/version conflict.

Alternative: permanent assignee only. Rejected because abandoned work would require manual repair and concurrent tabs would remain unsafe.

### Keep decision payloads narrow

The first decision set is approve or reject with a registered reason code and optional bounded note. Idempotency keys are scoped to reviewer and case and store a normalized payload hash.

### Commit decision, video transition, and audit together

The review repository transaction locks the case and video, validates the lease and current state, inserts an immutable human decision, applies the video transition, and inserts the success audit fact. Conflicts return `409`; expired leases return a stable conflict code.

### Notify after durable commitment

An outbox event is written in the same transaction for later author notification. Notification failure never rolls back the review decision.

## Risks / Trade-offs

- [Reviewer clocks differ] -> Lease validity is evaluated using database/server time only.
- [Long reviews expire] -> Support bounded lease renewal by the current holder.
- [A case is approved after content changes] -> Match the case review version to the video's current review version inside the transaction.
- [Queue starvation] -> Expose age and priority metrics and reserve policy changes for a later change.

## Migration Plan

1. Add lease and human-decision fields/tables while automated cases continue to wait.
2. Add queue, claim, renew, and decision APIs behind permissions.
3. Enable author notification outbox consumption.
4. Start with a small lease duration in staging and validate conflict behavior.
5. Roll back routes first; leased cases expire and remain pending human review.

`cancelled`/`superseded` case states and assignment-history events fit the existing bounded string columns, so this correction does not require a table-shape migration.

## Open Questions

- Whether later high-risk categories require two independent reviewer decisions.
