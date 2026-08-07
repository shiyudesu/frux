## 1. Provenance and Rollout Model

- [x] 1.1 Add registered machine-result source kinds and generated time to review domain entities, restore paths, DTOs, and persistence models.
- [x] 1.2 Add migrations that classify reserved `manual-seed` rows as `test_seed` and all other legacy rows conservatively as `legacy_unknown`.
- [x] 1.3 Add versioned moderation rollout configuration for `disabled`, `observe`, `approve_only`, and `enforce` with hard-bound validation.
- [x] 1.4 Persist the effective rollout mode with automated routing history and ensure mode changes are not retroactive.
- [x] 1.5 Update machine-result validation, internal API tests, review detail DTOs, and Web evidence rendering for explicit production/test/recovery/legacy provenance.

## 2. Durable Moderation Jobs

- [x] 2.1 Add the moderation-job domain model, states, deterministic result identity, attempt/lease rules, and stale-subject cancellation behavior.
- [x] 2.2 Add PostgreSQL job models, uniqueness/indexes, migration registration, and repository mapping.
- [x] 2.3 Create provider-enabled jobs atomically with review intake and reconcile missing jobs for current reviewable cases.
- [x] 2.4 Implement database-time lease claiming with `FOR UPDATE SKIP LOCKED`, bounded retry scheduling, completion, terminal, cancellation, and expired-lease recovery.
- [x] 2.5 Add repository tests for duplicate intake, concurrent claims, lease expiry, stale versions, retry bounds, and deterministic result IDs.

## 3. Protected Input Preparation

- [x] 3.1 Define a narrow moderation-input preparer interface and versioned input profile.
- [x] 3.2 Implement deterministic duration-based extraction of at most 12 JPEG frames, 512-pixel longest edge, and 8 MiB aggregate budget using existing media tooling.
- [x] 3.3 Include only bounded title/description metadata and persist a bounded input manifest of timestamps and hashes.
- [x] 3.4 Store samples under a protected temporary prefix and issue gateway-only short-lived access without exposing the original media.
- [x] 3.5 Add durable sample cleanup after accepted results or retention expiry.
- [x] 3.6 Add tests for short/long videos, extraction determinism, size/count limits, corrupt media, stale subjects, URL expiry, and cleanup idempotency.

## 4. Production Gateway Adapter

- [x] 4.1 Add validated configuration for endpoint, HMAC secret, timeout, concurrency, provider-config version, and local insecure-transport exception.
- [x] 4.2 Implement the canonical HTTP gateway request/response types with strict JSON bounds and unknown-field rejection.
- [x] 4.3 Implement timestamped HMAC request signing, stable request IDs, HTTPS enforcement, response-size limits, and secret/URL log redaction.
- [x] 4.4 Validate provider/model identifiers, generation time, labels, confidence, evidence timestamps, and signal limits before application submission.
- [x] 4.5 Build a contract-compatible HTTP fixture for success, timeout, 429/5xx, malformed response, duplicate response, and signature assertions.

## 5. Worker, Routing, and Failure Safety

- [x] 5.1 Implement the moderation Worker loop that claims jobs, prepares/reuses inputs, calls the gateway, and submits normalized results through the existing review service.
- [x] 5.2 Ensure response uncertainty and Worker restarts reuse the same job/request/result identity and cannot duplicate evidence or decisions.
- [x] 5.3 Implement `observe` force-human routing and `approve_only` suppression of automated rejects while retaining the active policy thresholds.
- [x] 5.4 Implement `disabled` and terminal-failure recovery results with `recovery` provenance and unknown `moderation_unavailable` evidence.
- [x] 5.5 Keep every provider/extraction/delivery failure review-gated and route exhausted current work to the human queue without a fabricated model outcome.
- [x] 5.6 Register Worker composition, configuration validation, fixed-label metrics, reconciliation, and graceful shutdown.

## 6. Verification and Deployment Documentation

- [x] 6.1 Add application tests for all rollout modes, unknown labels, stale results, source classification, recovery routing, and policy/version provenance.
- [x] 6.2 Add end-to-end Worker tests from media-ready intake through production evidence, human fallback, automated approval, and automated rejection.
- [x] 6.3 Run targeted Go review/media/Worker/API-flow tests and the strict Web production build.
- [x] 6.4 Update `docs/modules/review.md`, engineering/configuration docs, metrics documentation, and deployment examples for the gateway contract and secrets.
- [x] 6.5 Document the operational promotion checklist from disabled to observe, approve-only, and enforce, including human-agreement evidence and rollback.
- [ ] 6.6 Validate a real configured inference gateway in observe mode and confirm the admin UI labels its output as production evidence rather than test data.

  Deferred by operator decision on 2026-08-07. The deployment remains in safe `disabled` mode until a third-party provider is configured.
