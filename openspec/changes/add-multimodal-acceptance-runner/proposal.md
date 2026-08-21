## Why

The real Tongyi acceptance run proved the multimodal path works, but reproducing it required manual
service startup, S3 uploads, review approval, database inspection, metric checks, and temporary flag
changes. A versioned runner is needed so future model, contract, and infrastructure changes can be
validated consistently without exposing credentials or silently incurring model charges.

## What Changes

- Add a standalone Go acceptance command with a non-billable validation mode and an explicit
  billable execution mode.
- Validate API, Worker/metrics, Adapter health, selected contract, S3-backed fixture inputs, and
  required runtime configuration before creating any external-model request.
- Drive two controlled fixture videos through upload, review approval, publication, durable
  multimodal jobs, vector facts, projections, Similar Videos, and Hybrid Search.
- Read acceptance credentials and PostgreSQL access only from environment variables, never command
  arguments, reports, normal logs, or metric labels.
- Emit a bounded JSON report containing stages, durations, closed failure codes, contract identity,
  token usage deltas, vector dimension/norm, retrieval IDs, and created fixture identifiers without
  raw vectors, signed URLs, media bytes, API keys, HMAC secrets, passwords, or bearer tokens.
- Preserve repository defaults and running configuration: the runner SHALL NOT edit YAML, promote
  accounts, start/stop infrastructure, or enable feature flags implicitly.
- Add fake HTTP/database tests, a credential-free dry run, operator documentation, and an optional
  local fixture-retention policy for later Golden Set construction.

## Capabilities

### New Capabilities

- `multimodal-acceptance-runner`: Explicit, bounded, secret-safe orchestration and reporting for
  repeatable end-to-end multimodal acceptance.

### Modified Capabilities

None.

## Impact

- Adds `cmd/multimodal-acceptance` and supporting application/infrastructure code under `apps/api`.
- Uses the existing Frux HTTP APIs, signed provider boundary, PostgreSQL facts, Prometheus metrics,
  MinIO/S3 upload sessions, and review workflow; it adds no production HTTP endpoint.
- Adds acceptance-only environment variables and documentation. Existing multimodal flags remain
  disabled by default, and no real model call occurs without an explicit execution confirmation.
