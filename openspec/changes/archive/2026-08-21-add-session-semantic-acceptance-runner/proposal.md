## Why

Session Semantic Recommendation has deterministic Golden Set, real PostgreSQL adapter, race, and
full-service tests, but it has not yet been proven through one running API using existing real
active-contract vectors, normal user behavior endpoints, policy selection, sampled evidence, and
Snapshot continuation. A dedicated runner closes that gap without generating another embedding or
turning a local acceptance run into a production rollout.

## What Changes

- Add a default validation-only `session-semantic-acceptance` command with an independent explicit
  mutation gate; it makes no state change unless both confirmations are present.
- Require a dedicated acceptance user and configured positive seed, negative seed, and expected
  semantic target video IDs that are already readable and have current active-contract Fact and
  Projection rows. The runner never uploads media, creates embedding jobs, or calls a model.
- Install one unique, low-cohort, development-only Recommendation policy through the existing Domain
  policy contract and repository, derive a request ID that deterministically selects that cohort,
  and disable or remove only that runner-owned policy during cleanup.
- Use normal authenticated HTTP APIs to record completion/early-skip and favorite facts, then call
  `/api/feed-queries` with bounded Recommendation context and follow the signed Snapshot cursor.
- Verify PostgreSQL request-log evidence for policy version, builder/contract identity, Confidence,
  `semantic_session` reason, positive `semantic_similarity`, expected target participation, quota
  diagnostics, and absence of raw vectors.
- Verify API metrics show one first-page Builder/Provider execution and no additional Session Semantic
  execution on the Snapshot page. Optional adapter metrics, when configured, MUST remain unchanged.
- Emit a versioned privacy-safe JSON report with closed stages, created policy identity, bounded
  video/request IDs, evidence counts, metric deltas, cleanup result, and `external_model_calls: 0`.
- Revert runner-created favorite state and policy activation through narrow cleanup. Immutable view
  facts remain on the dedicated acceptance account and are reported rather than deleted directly.

## Capabilities

### New Capabilities

- `session-semantic-acceptance-runner`: Safe real-runtime orchestration and evidence for dormant
  Session Semantic Recommendation using pre-existing vectors and zero external model calls.

### Modified Capabilities

None.

## Impact

- New command and acceptance application/infrastructure code under `apps/api`.
- Reuse of Recommendation Domain policy validation, PostgreSQL recommendation/embedding repositories,
  bounded HTTP/metrics utilities, and existing environment-file loading conventions.
- New ignored example environment for dedicated acceptance identity and existing fixture video IDs.
- Recommendation documentation and roadmap status; no public API, business schema, model contract,
  bootstrap policy, production flag, or external dependency change.
