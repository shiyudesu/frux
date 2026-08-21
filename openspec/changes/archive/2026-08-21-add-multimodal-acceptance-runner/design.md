## Context

Frux has now passed one real Tongyi run covering startup, S3-backed video publication, durable jobs,
768-dimensional normalized vector facts, projections, Similar Videos, and Hybrid Search. The run also
found a publication-outbox field-loss bug that deterministic provider tests did not expose. Repeating
the same evidence currently requires many manual shell, API, SQL, and metric steps.

The runner is an operator/development command, not a production API. It must work through the same
public and admin HTTP workflows as the product, observe PostgreSQL read-only, preserve existing
security boundaries, and require an explicit acknowledgement before any billable provider call.

## Goals / Non-Goals

**Goals:**

- Provide a credential-free validation mode and an explicitly billable execution mode.
- Exercise two S3-backed fixtures through login, direct upload sessions, video creation, human review,
  publication, multimodal jobs, projection, Similar, and Hybrid.
- Verify exact contract identity, job fencing outcome, vector dimension/norm, projection digest,
  retrieval results, latency, and provider token deltas.
- Produce a stable secret-free JSON report suitable for local evidence and future CI environments with
  externally supplied credentials.
- Leave feature flags, service lifecycle, account roles, and infrastructure configuration unchanged.

**Non-Goals:**

- Starting Docker, API, Worker, Adapter, PostgreSQL, MinIO, Redis, or Kafka.
- Creating or promoting an administrator, bypassing review, or writing vector facts directly.
- Enabling multimodal flags, editing configuration files, or changing the active model profile.
- Evaluating semantic quality from synthetic fixtures or replacing the human Golden Set.
- Running in production automatically, deleting arbitrary data, or exposing secrets for debugging.

## Decisions

### 1. Add a standalone Go command with two gates

`cmd/multimodal-acceptance` uses existing packages and `net/http`. The default mode performs local
validation only: it parses configuration, validates fixture files, checks that required credentials
are present without printing them, and describes the planned stages.

Execution requires both `--execute` and `FRUX_ACCEPTANCE_ALLOW_BILLABLE=true`. Requiring a CLI action
and an environment acknowledgement prevents an alias, copied command, or CI discovery step from
silently invoking the external model. A single flag was rejected as too easy to trigger accidentally.

### 2. Treat the runtime as an explicit prerequisite

The runner checks API health, Adapter health, API/Worker metrics availability, PostgreSQL connectivity,
S3 upload-session mode, and configured fixture paths. It does not start services or alter YAML. If
video jobs, Similar, Query Embedding, Hybrid, S3 media, or provider readiness are unavailable, the
runner fails with a closed prerequisite code before uploading fixtures where possible.

This keeps lifecycle authority with existing development/deployment tooling and ensures the report
describes the configuration actually under test.

### 3. Use existing HTTP workflows and read PostgreSQL only

The runner logs in with a dedicated regular acceptance account and a pre-existing admin/reviewer
account supplied through environment variables. It creates direct upload sessions, uploads fixture
bytes to the returned presigned URLs, completes assets, creates two public videos with unique
idempotency keys, waits for review cases, claims and approves them through the admin API, then calls
Similar and Hybrid endpoints.

PostgreSQL is used only to observe review cases, multimodal jobs, facts, projections, contract fields,
vector length/norm, and source identity. Direct inserts, role changes, state transitions, or vector
writes were rejected because they would bypass the system being accepted.

### 4. Use a bounded stage machine

The run has named stages with independent timeouts and monotonic timing:

1. `preflight`
2. `login`
3. `upload_fixture_a` / `upload_fixture_b`
4. `create_video_a` / `create_video_b`
5. `approve_video_a` / `approve_video_b`
6. `wait_embedding_a` / `wait_embedding_b`
7. `verify_fact_projection`
8. `similar`
9. `hybrid`
10. `metrics`

Polling uses bounded intervals and deadlines. Every failure maps to a closed code and stops later
billable stages. HTTP clients reject redirects for authenticated requests and bound response bodies.

### 5. Keep secrets out of arguments and reports

The runner reads PostgreSQL DSN, user/admin account passwords, and optional bearer material only from
environment variables. Command arguments contain paths, timeouts, query text, and report location but
no secret values. Existing environment values are never echoed.

Reports include endpoint roles but not endpoint URLs containing queries, presigned upload URLs,
credentials, tokens, raw HTTP bodies, raw vectors, media bytes, passwords, HMAC material, or API keys.
Normal errors expose closed codes and bounded stage context.

### 6. Report evidence, not quality claims

The JSON report records schema version, run ID, timestamps, stage outcomes/durations, active contract,
video/asset/job IDs, attempts, vector dimension/norm/digest equality, Similar/Hybrid result IDs,
provider operation deltas, token deltas, and retained-fixture status.

Synthetic or duplicated fixture media proves orchestration and cross-modal compatibility only. The
runner does not calculate or claim Recall/NDCG/MRR; those remain the responsibility of a separately
labelled Golden Set.

### 7. Make fixture retention explicit

The default execution retains created fixture videos and reports their IDs so they can seed a Golden
Set. `--cleanup` performs only authenticated deletion of the two videos created by the current run
after retrieval checks complete. It never deletes accounts, media objects directly, historical vector
facts, unrelated videos, or prior-run fixtures. Cleanup failure is reported without hiding acceptance
results.

## Risks / Trade-offs

- [Billable calls and external availability] → Require two execution gates, print the planned number
  of calls in validation mode, and bound every stage.
- [Acceptance credentials have meaningful authority] → Require dedicated accounts, environment-only
  secrets, no value logging, and existing admin permission enforcement.
- [A partial run leaves fixtures] → Use unique run-scoped idempotency keys and report every created ID;
  offer narrow authenticated cleanup for current-run videos only.
- [Database schema coupling] → Keep queries read-only and limited to stable multimodal/review columns;
  cover them with PostgreSQL integration tests when a test DSN is available.
- [Synthetic fixtures can look like relevance evidence] → Label the report `technical_acceptance` and
  explicitly exclude quality claims.
- [Metrics can reset between baseline and result] → Detect counter regression and report metrics as
  unavailable rather than manufacturing a delta.

## Migration Plan

1. Implement the report/domain model and non-billable validation mode.
2. Add bounded HTTP clients for health, login, upload, review, Similar, and Hybrid.
3. Add read-only PostgreSQL observation and provider metric deltas.
4. Add fake-server tests plus optional isolated PostgreSQL tests.
5. Document dedicated acceptance accounts, fixture preparation, execution, retention, and cleanup.
6. Run the command first in validation mode; execute a real run only with explicit operator approval.

Rollback removes the acceptance command and documentation. It changes no production schema or API,
and existing multimodal operation is unaffected.

## Open Questions

- Golden Set ranking collection remains a separate follow-up because acceptance fixtures do not carry
  human relevance labels.
- CI execution with real credentials remains disabled until a dedicated secret store and billing
  budget are explicitly approved.
