## 1. Human Review Domain

- [ ] 1.1 Add reviewer assignment, lease, reason-code, human decision, and history entities.
- [ ] 1.2 Implement lease claim, renewal, expiry, release, and decision validation methods.
- [ ] 1.3 Add domain tests for ownership, server-time expiry, version conflicts, notes, and idempotency payloads.

## 2. Persistence and Queueing

- [ ] 2.1 Add lease, assignment-history, human-decision, and decision-idempotency persistence fields and models.
- [ ] 2.2 Implement stable priority/age queue queries and cursor binding.
- [ ] 2.3 Implement atomic claim, renewal, and expired-lease recovery repository methods.
- [ ] 2.4 Implement the transaction that commits decision, case, video, audit, and notification outbox together.
- [ ] 2.5 Add persistence tests for concurrent claims, stale versions, expired leases, and rollback on audit failure.

## 3. Application and HTTP APIs

- [ ] 3.1 Add queue, case-detail, claim, renew, and decision application services.
- [ ] 3.2 Add permission-protected admin DTOs and handlers with strict input and stable conflict codes.
- [ ] 3.3 Register review-read and review-decide routes under the admin group.
- [ ] 3.4 Connect committed review decisions to author notification through the existing durable outbox pattern.

## 4. Observability and Verification

- [ ] 4.1 Add metrics for queue age, available cases, claims, lease expiry, decisions, conflicts, and notification outcomes.
- [ ] 4.2 Add application, handler, concurrency, and API-flow tests for queue and decision behavior.
- [ ] 4.3 Update review, admin, message, product, architecture, monitoring, and engineering documentation.
- [ ] 4.4 Run targeted review/message tests, the full Go suite, and strict OpenSpec validation.
