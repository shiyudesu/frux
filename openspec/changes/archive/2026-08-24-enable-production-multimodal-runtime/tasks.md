## 1. Production Runtime Configuration

- [x] 1.1 Add strict private-network provider and production full-rollout flags with environment-scoped validation and mutual exclusion.
- [x] 1.2 Generalize immutable v4 reconciliation for explicit development or production full rollout without changing policy contents.
- [x] 1.3 Add config/runtime tests for exact private hostname acceptance, arbitrary HTTP rejection, environment scope, and v4 replay/conflict behavior.

## 2. Production Compose and Secret Isolation

- [x] 2.1 Add a profile-gated production Adapter service using the digest-pinned API image, backend-only networking, startup healthcheck, and no published port.
- [x] 2.2 Split API/Worker multimodal environments so only Worker receives video-job Endpoint/HMAC and only Adapter receives `DASHSCOPE_API_KEY`.
- [x] 2.3 Add production environment template values for profile selection, private transport, Adapter bounds, video jobs, Session runtime, and production v4 reconciliation.
- [x] 2.4 Validate rendered Caddy/direct-IP configurations with profile off/on and assert Adapter/API/Worker port and secret boundaries.

## 3. Deployment Lifecycle

- [x] 3.1 Add pre-mutation validation for the complete production multimodal feature set and reject partial or unsafe values.
- [x] 3.2 Make pull/up/readiness/rollback profile-aware and require Adapter health before Worker readiness when enabled.
- [x] 3.3 Extend release/CI checks for the optional Adapter service without changing the signed bundle file set.

## 4. Verification and Documentation

- [x] 4.1 Run config/unit/deploy-script tests, `go test ./...`, builds, production Compose validation for both public modes and multimodal states, Docker smoke checks, and strict OpenSpec validation.
- [x] 4.2 Document direct-IP/ICP limitations, private Adapter HTTP, exact server environment values, activation verification, cost, and rollback.
- [x] 4.3 Sync delta specs, archive the change, and provide the remaining server-side secret/deployment actions.
