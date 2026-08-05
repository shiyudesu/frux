## Why

Automated moderation cannot safely resolve ambiguous or sampled content without a bounded human workflow. Frux needs a review queue with ownership and concurrency rules before an operator-facing console can be built.

## What Changes

- Add a stable cursor-paginated queue for cases awaiting human review, ordered by priority and age.
- Support explicit assignment and expiring reviewer leases so two reviewers cannot unknowingly decide the same case.
- Allow authorized reviewers to submit idempotent approve or reject decisions with required reason codes and optional bounded notes.
- Apply the final decision to the review case and video lifecycle atomically and record the privileged action through the shared audit port.
- Expose complete machine evidence and decision history while preventing mutation of prior facts.
- Exclude appeals, multi-reviewer consensus, quality sampling, workforce scheduling, and Web UI.

## Capabilities

### New Capabilities

- `human-review-workflow`: Queueing, assignment, leases, decisions, and atomic enforcement for human review.

### Modified Capabilities

None.

## Impact

This extends the review domain, repository, service, admin HTTP endpoints, notification adapters, metrics, migrations, tests, and documentation. It depends on `add-admin-authorization-foundation`, `add-admin-audit-trail`, and `add-automated-content-review`.
