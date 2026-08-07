## Why

Frux persists and routes machine-review results but does not currently generate them; the existing review data used for manual testing is seeded rather than produced by a model. A durable production-provider pipeline is required to make machine evidence real while keeping provider failures review-safe.

## What Changes

- Add a configurable production moderation-provider contract implemented through an authenticated HTTP inference gateway, avoiding direct vendor coupling in domain and application packages.
- Create durable, leased moderation jobs for reviewable video versions with bounded retry, terminal failure, reconciliation, and idempotent result delivery.
- Generate bounded review inputs from protected media, beginning with deterministic video frame samples and optional existing text metadata; raw media remains protected.
- Normalize provider labels, confidence, model identity, evidence references, and source classification into the existing machine-result ingestion path.
- Add disabled-human-fallback, observe/force-human, approve-only, and full-policy rollout modes.
- Keep videos review-gated when input extraction, provider calls, response validation, or result delivery fails.
- Expose truthful evidence provenance so seeded/test results cannot be mistaken for production-model output.

## Capabilities

### New Capabilities

- `production-moderation-provider`: Define durable inference work, protected input preparation, authenticated provider exchange, rollout controls, and failure recovery.

### Modified Capabilities

- `automated-content-review`: Require explicit evidence source classification and accept production-provider results through the existing provenanced, idempotent routing boundary.

## Impact

Affected areas include review domain/application capability interfaces, new persistence models and migrations for moderation jobs and samples, Worker composition, object-storage access, configuration and secrets, metrics, machine-result provenance, admin evidence presentation, and integration tests using a contract-compatible HTTP provider fixture. Production deployment must configure a real inference gateway; disabled mode remains safe.
