## 1. Registered Rollout Policy

- [x] 1.1 Define `session-semantic-rollout-v1` options, fixed semantic budget/deadline/reservation/weight/builder bounds, 1-5% rollout validation, and deterministic configuration digest.
- [x] 1.2 Implement baseline-cloning policy construction with fixed Provider order/Quota Merge, active contract binding, disabled-first output, defensive maps, and exact source/bootstrap immutability tests.
- [x] 1.3 Add stable synthetic cohort probes and tests proving target matches fall through deterministically to an enabled 100% baseline for non-matching requests.

## 2. Policy Lifecycle and Kill Switch

- [x] 2.1 Extend policy repository contracts with exact idempotent `DisablePolicy(scene, version)` and implement PostgreSQL row locking/validation without changing other policies.
- [x] 2.2 Harden exact activation preflight to require another enabled 100% baseline policy while preserving existing low-level repository compatibility for non-rollout callers.
- [x] 2.3 Add application rollout service actions for plan/create/activate/status/disable with target compatibility, existing-target replay/conflict, baseline fallback, and no policy deletion.
- [ ] 2.4 Add unit and PostgreSQL integration tests for create replay/conflict, activate replay, stable selection, exact disable replay, wrong target rejection, concurrent mutation, and v1/v2 preservation.

## 3. Evidence and Runtime Gates

- [x] 3.1 Add strict loading/validation for canonical `session-semantic-shadow-report/v1`, file SHA-256, schema/tool/status, zero model calls, minimum cases, contribution/survival, and relevance parity.
- [x] 3.2 Add `frux_recommendation_session_semantic_runtime_ready` and set it only after complete Builder/Provider/contract/Exact API composition; add enabled/disabled composition tests.
- [x] 3.3 Implement bounded metrics-endpoint readiness probing that requires exactly one ready gauge and rejects missing, duplicate, malformed, non-finite, or non-ready results.
- [x] 3.4 Ensure disable remains available without Shadow report or runtime readiness while plan/create/activate fail closed on incompatible prerequisites.

## 4. Operator Command and Reports

- [x] 4.1 Add ignored-local-env discovery and a committed `.env.session-semantic-rollout.example` with DSN, metrics endpoint, evidence path, source/target versions, rollout percentage, report path, and mutation gate.
- [x] 4.2 Implement `cmd/session-semantic-rollout` actions `plan/create/activate/status/disable`, bounded options/timeouts, and double mutation acknowledgement with no credential-bearing flags.
- [x] 4.3 Generate `0600` JSON operator reports containing fixed preflight outcomes, evidence/config digests, bounded policy diff, synthetic cohort summary, replay/mutation result, and exact recovery command.
- [x] 4.4 Add command/report tests for dry run, each action, single-gate rejection, malformed config/evidence/readiness, replay/conflict, secret/path/error redaction, atomic replacement, and deterministic read-only fields.

## 5. Observability, Verification, and Operations

- [x] 5.1 Add fixed-label rollout operation metrics for plan/create/activate/status/disable success/replay/blocked/error without scene/version/path/contract/error labels.
- [ ] 5.2 Document evidence gates, disabled-first lifecycle, runtime readiness, 1% cohort, observation checklist, exact Kill Switch, broad rollback distinction, recovery, and non-causal limits.
- [ ] 5.3 Run a real local `plan` against Docker PostgreSQL and API metrics, prove activation is blocked while Session Semantic runtime readiness is 0, and record a secret-free operator report.
- [ ] 5.4 Run focused policy/evidence/readiness/service/repository/command/metrics tests, real PostgreSQL integration tests, `go test ./...`, build API/Worker/rollout entrypoints, rebuild Docker, validate Compose, and run `openspec validate --all --strict`.
