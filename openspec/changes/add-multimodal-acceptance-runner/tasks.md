## 1. Acceptance Contract and Configuration

- [x] 1.1 Define versioned technical-acceptance report, stage result, closed failure code, fixture,
  vector evidence, retrieval evidence, metric delta, and cleanup result types.
- [x] 1.2 Add strict environment configuration for API/Adapter/metrics endpoints, PostgreSQL DSN,
  dedicated user/admin credentials, fixture paths, query text, polling, deadlines, and body limits.
- [x] 1.3 Implement the two-gate billable-execution policy and a default validation plan that reports
  maximum model calls without making remote or state-changing requests.

## 2. Bounded HTTP Workflow Client

- [x] 2.1 Implement one reusable non-redirecting bounded JSON client with bearer handling, response
  limits, closed status mapping, and no raw-body or secret errors.
- [x] 2.2 Implement API/Adapter health and Prometheus baseline collection with closed metric names and
  counter-reset-safe delta calculation.
- [x] 2.3 Implement consumer/admin login, S3 upload-session creation, presigned PUT, completion, and
  video creation using run-scoped idempotency keys.
- [x] 2.4 Implement review-case polling, claim, approval, Similar Videos, Hybrid Search, and optional
  current-run video deletion through existing APIs.

## 3. Read-only PostgreSQL Evidence

- [ ] 3.1 Implement bounded read-only queries for review cases, multimodal jobs, vector facts,
  projections, contract identity, vector dimension/norm, attempts, and digest equality.
- [ ] 3.2 Reject incompatible contracts, terminal jobs, non-finite/non-unit vectors, missing
  projections, and source/fact mismatches with closed evidence codes.
- [ ] 3.3 Add optional isolated PostgreSQL tests that skip explicitly when `FRUX_POSTGRES_TEST_DSN`
  is absent and never mutate a non-test schema.

## 4. Stage Orchestration and Reporting

- [ ] 4.1 Implement the fixed bounded stage machine from preflight through two fixtures, review,
  embedding, evidence, Similar, Hybrid, metrics, and optional cleanup.
- [ ] 4.2 Stop dependent stages on failure, retain all current-run identifiers, and emit one report
  for success, validation-only execution, partial failure, cancellation, or cleanup failure.
- [x] 4.3 Add atomic optional report-file writing with restrictive permissions while keeping stdout
  JSON free of secrets, raw vectors, signed URLs, media bytes, and raw provider responses.

## 5. Command and Operator Experience

- [x] 5.1 Add `cmd/multimodal-acceptance` with validation-only defaults, `--execute`, `--cleanup`,
  report path, query, and bounded timeout options but no secret-bearing flags.
- [ ] 5.2 Add an ignored acceptance environment example and document dedicated accounts, S3/runtime
  prerequisites, dry run, confirmed execution, expected billable calls, report interpretation, and cleanup.
- [x] 5.3 Update the recommendation roadmap to place the runner before real Golden Set collection and
  preserve disabled-by-default multimodal behavior.

## 6. Verification

- [ ] 6.1 Add fake API, Adapter, metrics, presigned-upload, cancellation, timeout, redirect, malformed
  response, secret-redaction, partial-run, and cleanup tests.
- [ ] 6.2 Run command dry-run tests, targeted race tests, complete Go tests/vet/build, Compose validation,
  and strict OpenSpec validation.
- [ ] 6.3 Confirm repository defaults remain disabled and no credentials, bearer tokens, signed URLs,
  fixture media, raw vectors, generated reports, or acceptance database DSNs are committed.
