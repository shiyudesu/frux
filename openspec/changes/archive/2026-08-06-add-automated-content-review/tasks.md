## 1. Review Domain and Policy

- [x] 1.1 Add review case, machine signal, automated decision, status, and error entities.
- [x] 1.2 Add video review-version behavior and validate current subject versions.
- [x] 1.3 Implement the closed moderation taxonomy and typed versioned threshold policy.
- [x] 1.4 Add domain tests for normalization, bounds, precedence, unknown labels, and routing outcomes.

## 2. Persistence and Migration

- [x] 2.1 Add PostgreSQL models and indexes for review cases, signals, decisions, policies, and idempotent result identities.
- [x] 2.2 Implement case creation, case locking, evidence insertion, policy reads, and atomic outcome repository methods.
- [x] 2.3 Register migrations and bootstrap a conservative initial policy without overwriting operator state.
- [x] 2.4 Add PostgreSQL tests for duplicate intake, duplicate results, stale versions, and transactional outcomes.

## 3. Intake and Result Processing

- [x] 3.1 Add the application service that creates or returns a review case for a ready pending video.
- [x] 3.2 Connect durable media-ready or video-ready events to idempotent case intake.
- [x] 3.3 Add the internal-token machine-result endpoint with strict bounded JSON binding.
- [x] 3.4 Implement policy routing to approve, reject, or pending-human and apply video transitions atomically.
- [x] 3.5 Add reconciliation for ready pending videos that have no active review case.

## 4. Observability and Verification

- [x] 4.1 Add bounded metrics for intake, provider result, routing outcome, duplicate, invalid, retry, and reconciliation results.
- [x] 4.2 Add service, handler, worker, and API-flow tests for all routing and failure paths.
- [x] 4.3 Update review, video, product, architecture, monitoring, and engineering documentation.
- [x] 4.4 Run targeted review/video tests, the full Go suite, and strict OpenSpec validation.
