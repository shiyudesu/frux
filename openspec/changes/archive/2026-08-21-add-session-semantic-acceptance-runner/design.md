## Context

The Session Semantic implementation is dormant by default and already has deterministic in-memory
Golden Set coverage, real isolated PostgreSQL adapter tests, full Recommendation service tests, and a
zero-model offline report. Those layers do not prove that one separately running API process has the
feature flag enabled, selects a semantic policy, consumes normal HTTP behavior facts, writes sampled
request evidence, and reuses the Redis Snapshot ordering on a cursor page.

The existing multimodal technical runner is intentionally responsible for media upload, review,
embedding Job/Fact/Projection, Similar, and Hybrid. Reusing it would couple recommendation acceptance
to paid vector creation and duplicate unrelated orchestration. Session acceptance instead requires
three existing readable active-contract videos: a positive seed, a negative seed, and an expected
semantic target. Their vectors can come from retained multimodal acceptance fixtures or any explicit
development fixture.

Recommendation policy creation has a Domain/Application/Repository contract but no public admin HTTP
endpoint. User behavior and Feed access do have normal authenticated HTTP APIs. The runner therefore
uses the narrow policy repository only for a runner-owned development policy, uses HTTP for user
facts and Feed, and uses PostgreSQL read evidence for verification.

## Goals / Non-Goals

**Goals:**

- Prove one real API runtime executes `session-semantic-v1` from normal user facts and existing real
  vectors.
- Prove the expected target carries `semantic_session` and positive `semantic_similarity` in sampled
  request evidence.
- Prove Confidence, policy identity, contract identity, quota diagnostics, and Snapshot reuse are
  observable and bounded.
- Prove Recommendation performs no model call and does not need an online multimodal adapter.
- Leave the runner-owned policy inactive after every execution path and limit optional cleanup to
  runner-owned reversible state.

**Non-Goals:**

- No media upload, review, embedding generation, Backfill, ANN, Shadow, or Rollout.
- No public policy-management API or production policy mutation workflow.
- No deletion of immutable view facts or unrelated request logs.
- No claim of relevance lift from three operator-selected videos.
- No automatic discovery of fixture videos; operators choose exact existing IDs.

## Decisions

### 1. Default to validation and require two mutation gates

The command defaults to configuration/fixture validation. Execution requires both `--execute` and
`FRUX_SESSION_SEMANTIC_ACCEPTANCE_ALLOW_MUTATION=true`. Validation reports planned stages and makes no
login, behavior, policy, Feed, or database mutation.

This mirrors the safety posture of the multimodal acceptance runner even though this workflow is not
billable. Policy activation, favorite state, view facts, request logs, and Snapshot creation are real
state changes and deserve an explicit independent acknowledgement.

### 2. Require pre-existing vectors and make zero model calls structural

Configuration supplies positive seed, negative seed, and expected target IDs plus an immutable
multimodal profile. Preflight uses read-only PostgreSQL evidence to require each video to be currently
readable and to have matching Fact and Projection identity. It also checks the expected target is
positive under Exact retrieval from the positive seed vector.

The runner has no query embedder, media upload, provider client, or embedding Job method. Optional
adapter metrics may be scraped before/after; if supplied, video/query/startup operation counters must
not increase. The report always declares the structural external model call count as zero.

### 3. Install a unique one-percent cohort policy and select it deterministically

The runner connects to PostgreSQL through GORM, restores the Recommendation repository, chooses the
next unused version, and constructs a normal Domain-validated policy:

- existing Providers plus `semantic_session`;
- complete budget/deadline/quota order and reservation;
- positive `semantic_similarity` weight;
- `session-semantic-v1`, active contract key, bounded lookback/seeds/confidence;
- `sampling_rate_ppm=1_000_000`;
- `rollout_percentage=1`;
- exposure hard suppression disabled so seed videos do not collapse a small fixture pool.

The Domain exposes the stable cohort-percent helper used by normal selection. The runner searches a
bounded sequence of run-scoped request IDs until it finds one in the one-percent cohort for the
authenticated user. This avoids making every local request select the temporary policy.

The policy is created enabled because policy selection reads enabled versions on each request. A
deferred scoped update always disables exactly the created policy ID. Existing policy enabled states
and configuration are never rewritten. `--cleanup` may delete only that already-disabled runner policy
after evidence has been collected; otherwise it remains disabled for inspection.

Alternative considered: use `RollbackPolicy`. Rejected because rollback intentionally disables every
other policy in the scene and changes the target rollout to 100%, which would not restore a pre-run
multi-policy state exactly.

### 4. Use normal authenticated behavior and Feed APIs

The HTTP workflow is:

1. consumer login and `/api/users/me` identity;
2. positive seed `complete` view event under scene `recommend`;
3. positive seed `favorite` with run-scoped idempotency and recommendation request headers;
4. negative seed `skip` with an early bounded ratio;
5. first `/api/feed-queries` request with positive current and negative recent context;
6. cursor continuation using the same request/session context.

The runner uses stable event/playback/idempotency IDs derived from `run_id`. Direct request retries are
safe. It does not submit `not_interested`, because that endpoint correctly requires prior server-issued
candidate evidence and the configured negative fixture is not guaranteed to have been delivered by a
previous request.

### 5. Verify authoritative request-log evidence, not public score fields

Public Feed DTOs intentionally hide internal Provider reasons and score components. The runner polls
the sampled `recommendation_request_log` by user/request and validates its compact JSON:

- exact temporary policy version;
- `session_semantic.result=success`;
- builder version and contract key;
- finite positive Confidence and non-none band;
- positive/negative/compatible counts;
- expected target candidate with `semantic_session` reason;
- expected target `semantic_similarity > 0`;
- bounded semantic quota diagnostics when quota merge is configured;
- no raw vector or raw event keys.

It separately confirms the first Feed response is non-empty and contains the expected target either
on the first page or in the full logged ranked pool. Relevance assertions beyond that remain the
offline/public-dataset evaluator's responsibility.

### 6. Use metric deltas to prove first-page execution and Snapshot reuse

API metrics are collected before the first page, after the first page, and after cursor continuation.
The first page must add exactly one successful builder and one successful provider operation for the
temporary request. The cursor page must add zero Session Semantic operations. Snapshot metrics must
show a created/hit path or the run fails rather than silently accepting a degraded score cursor as a
Snapshot proof.

If the configured fixture pool cannot produce `has_more + next_cursor`, the runner fails with a closed
snapshot prerequisite code and reports the first-page evidence; operators must add another readable
video rather than weaken the assertion.

### 7. Make cleanup narrow and failure-safe

Deferred cleanup always disables the runner-owned policy. Optional cleanup additionally sends the
normal unfavorite endpoint and deletes only that disabled policy row. Immutable view events, request
logs, served-candidate evidence, and Snapshots follow their existing retention; the report lists the
run/request/policy identities required to inspect them.

Secrets, bearer tokens, DSNs, passwords, raw JSON bodies, signed cursors, and raw vectors never enter
the report or normal error text. The report file uses mode `0600`.

## Risks / Trade-offs

- [Fixture target is not actually close to the positive seed] → Preflight performs Exact evidence and
  requires the configured target in the bounded positive result before any mutation.
- [Small fixture pool cannot prove cursor reuse] → Fail with a closed prerequisite and require another
  readable video; do not substitute a first-page retry for cursor continuation.
- [Temporary policy affects other local traffic] → Use a one-percent stable cohort, a unique highest
  version, a short bounded run, and unconditional exact-ID disable.
- [Runner crashes after policy creation] → Report the policy ID/version as soon as created; policy uses
  one-percent rollout, and the command documents a scoped cleanup invocation. Normal failures execute
  deferred disable.
- [Favorite cleanup changes pre-existing user state] → Require a dedicated acceptance account whose
  fixture favorite is initially absent; preflight rejects an already-favorited positive seed.
- [Adapter metrics are unavailable] → The run may still prove zero-call architecture with no adapter
  configured, but reports adapter evidence unavailable instead of fabricating a delta.
- [Immutable acceptance behavior accumulates] → Use a dedicated user, stable run IDs, normal retention,
  and explicit report identities; never delete audit/history facts directly.

## Migration Plan

1. Add report/config contracts, environment example, validation-only CLI, and fake tests.
2. Add read-only fixture/request-log evidence and scoped policy lifecycle with isolated PostgreSQL tests.
3. Add authenticated behavior/Feed workflow, metrics assertions, cleanup, and complete integration tests.
4. Keep checked-in Session Semantic flags false; execute only against an explicitly prepared local
   runtime and dedicated account.
5. Roll back by removing the command/package. It adds no business schema and changes no active policy.

## Open Questions

No implementation-blocking question remains. Actual fixture IDs and runtime credentials are operator
inputs and intentionally stay outside OpenSpec and source control.
