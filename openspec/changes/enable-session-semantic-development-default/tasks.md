## 1. Development Runtime Defaults

- [x] 1.1 Add a strict local/test-only development full-rollout configuration flag and environment override.
- [x] 1.2 Enable the registered Session-only profile/runtime/full-rollout flags by default in development Compose while leaving production off.
- [x] 1.3 Add configuration and Compose tests for default development enablement, explicit disable, invalid values, and production rejection.

## 2. Full Policy Lifecycle

- [x] 2.1 Extend registered policy construction to allow exactly100% only through an explicit full-rollout authorization while preserving normal1-5% bounds.
- [x] 2.2 Implement idempotent development reconciliation for immutable v4 create/activate and exact disable of other semantic rollout targets.
- [x] 2.3 Wire reconciliation after successful Session runtime composition and fail startup on policy conflict or persistence error.
- [x] 2.4 Update existing-policy cohort evidence so100% targets require a retained baseline but omit impossible fallback-cohort evidence.

## 3. Verification

- [x] 3.1 Add application, config, router/reconciler, acceptance, operator, and PostgreSQL tests for full rollout and replay behavior.
- [x] 3.2 Rebuild and restart default development Compose without the special runtime env file.
- [x] 3.3 Verify runtime-ready=1, v4 enabled at100%, v3 disabled, v1/v2 unchanged, and deterministic request buckets all select v4.
- [x] 3.4 Run focused tests, real PostgreSQL integration tests, `go test ./...`, entrypoint builds, Compose validation, Docker build, and strict OpenSpec validation.

## 4. Documentation and Completion

- [x] 4.1 Update recommendation, multimodal, monitoring, module index, environment examples, and roadmap documentation for development-default full rollout and production opt-in behavior.
- [ ] 4.2 Sync delta specs, archive the completed change, and leave the worktree clean with the development stack healthy.
