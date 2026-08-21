## 1. Contracts and safe command

- [x] 1.1 Define versioned report, stage, prerequisite, policy, semantic evidence, metric delta, fixture, and cleanup contracts with closed result/failure values.
- [x] 1.2 Add strict environment loading/validation for endpoints, PostgreSQL, dedicated account, profile, three distinct video IDs, timeouts, and optional adapter metrics without value leakage.
- [x] 1.3 Implement the two mutation gates and validation-only plan that performs no login, policy, behavior, Feed, or cleanup mutation.
- [x] 1.4 Add `cmd/session-semantic-acceptance`, atomic `0600` report writing, ignored environment example, and command tests.

## 2. Fixture and policy evidence

- [x] 2.1 Implement bounded read-only PostgreSQL checks for readable videos, current Fact/Projection identity, positive seed-to-target Exact similarity, favorite absence, and request-log evidence.
- [x] 2.2 Add a scoped GORM policy manager that chooses a unique version, creates a Domain-valid one-percent semantic policy, derives a matching cohort request ID, disables exact runner policy IDs, and optionally deletes only disabled runner policies.
- [x] 2.3 Expose/test the stable Domain policy cohort helper without changing selection behavior.
- [x] 2.4 Add isolated PostgreSQL tests for fixture bounds, contract mismatch, policy lifecycle, existing-policy preservation, cohort identity, request-log validation, cancellation, and cleanup.

## 3. Authenticated product workflow

- [x] 3.1 Extend the bounded acceptance HTTP client with consumer login, `/api/users/me`, view-event, favorite/unfavorite, and Feed query methods that never expose bearer tokens or cursors.
- [x] 3.2 Implement run-scoped positive completion/favorite and negative early-skip facts with stable event, playback, request, session, and idempotency identities.
- [x] 3.3 Execute first-page Feed with positive current/negative recent context, require expected target evidence and signed continuation, then request the Snapshot page.
- [x] 3.4 Add fake API tests for success, idempotent replay, authentication, malformed/oversize bodies, missing cursor, target absence, timeout, cancellation, and redirect rejection.

## 4. Metrics, runner, and cleanup

- [x] 4.1 Collect API Session Semantic and Snapshot counter baselines/deltas plus optional adapter operation deltas with new-series/reset handling.
- [x] 4.2 Implement the bounded stage machine from preflight through policy, facts, first page, evidence, Snapshot, metrics, policy disable, and optional cleanup.
- [x] 4.3 Guarantee deferred exact-policy disable on every post-creation failure and retain bounded run identities for manual recovery.
- [x] 4.4 Verify first-page builder/provider increments, zero Snapshot-page semantic increments, Snapshot create/hit evidence, and zero optional adapter operation deltas.
- [x] 4.5 Add runner tests for stage stop/skip, failure classification, cleanup failure, policy-disable failure, report redaction, and complete success.

## 5. Documentation and verification

- [ ] 5.1 Document preparation of dedicated account and three existing vectorized videos, session-only runtime configuration, execution/cleanup commands, immutable behavior retention, and manual scoped policy recovery.
- [ ] 5.2 Update the recommendation roadmap to place this runner before public-dataset evaluation without describing it as Shadow or Rollout.
- [ ] 5.3 Run command dry-run tests, targeted race tests, complete Go tests/vet/build, real isolated PostgreSQL tests, Compose validation, and strict OpenSpec validation.
- [ ] 5.4 Confirm checked-in flags/bootstrap policies remain unchanged, the runner has no upload/embed/provider method, reports `external_model_calls: 0`, and secrets/cursors/raw vectors never enter output.
