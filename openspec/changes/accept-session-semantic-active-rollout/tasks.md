## 1. Safe Session Runtime Overrides

- [ ] 1.1 Add strict optional parsing for `FRUX_MULTIMODAL_ENABLED` and `FRUX_MULTIMODAL_SESSION_RECOMMENDATION_ENABLED` before multimodal normalization, preserving YAML for absent/blank values.
- [ ] 1.2 Add unit tests for absent, blank, true/false, invalid, parent/session dependency, profile-only Session runtime, and unchanged checked-in defaults.
- [ ] 1.3 Add the variables to Frux env-file allowlists and development/production Compose, plus a dedicated `.env.session-semantic-runtime.example` without Adapter secrets/endpoints.

## 2. Existing Rollout Policy Acceptance

- [ ] 2.1 Extend acceptance config/report/state with optional existing policy version, policy mode, target/fallback cohort buckets, fallback version, and managed/created distinctions.
- [ ] 2.2 Implement store verification for enabled registered rollout target, active contract, enabled 100% baseline, deterministic target request identity, and deterministic non-target fallback selection.
- [ ] 2.3 Update Runner policy, disable, recovery, and cleanup stages so existing targets are managed/exact-disabled but never deleted, while temporary mode remains backward compatible.
- [ ] 2.4 Add unit and PostgreSQL tests for valid target/fallback, missing/disabled/incompatible target, missing baseline, temporary compatibility, success disable, failure recovery, cleanup non-deletion, and report redaction.

## 3. Real Active Rollout Verification

- [ ] 3.1 Rebuild Docker and start API/Worker with the dedicated session-only env file; verify runtime-ready=1 and no Adapter/video/query/Hybrid features are required.
- [ ] 3.2 Create a dedicated local acceptance user through normal register/login APIs and prepare ignored acceptance configuration using existing videos 12/14/13.
- [ ] 3.3 Generate contract-bound Shadow evidence, explicitly activate v3, and execute existing-policy Session Semantic acceptance with mutation/cleanup gates.
- [ ] 3.4 Verify report policy_version=3, positive semantic similarity, expected target, one first-page Builder/Provider call, zero Snapshot recomputation, zero external model calls, fallback cohort evidence, favorite cleanup, and v3 disabled/not deleted with v1/v2 unchanged.
- [ ] 3.5 Restart default Compose without the session runtime override and verify runtime-ready returns to0 while v3 remains disabled.

## 4. Documentation and Final Validation

- [ ] 4.1 Document environment activation, dedicated account preparation, existing-policy acceptance command, safety recovery, final disabled state, and native Adapter independence.
- [ ] 4.2 Run focused config/env/store/runner/report tests, real PostgreSQL integration tests, `go test ./...`, build API/Worker/acceptance entrypoints, rebuild Docker, validate Compose, and run `openspec validate --all --strict`.
