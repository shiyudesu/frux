## Context

Frux creates review cases and has an authenticated, provider-neutral machine-result boundary that persists provenance and applies versioned routing policy. No component currently samples media or calls a real inference service; the manually seeded review rows are test fixtures. The Worker already requires RabbitMQ and Redis, while PostgreSQL is the durable source of truth and object storage protects non-public media.

This change must integrate real inference without importing a vendor SDK into domain/application code, exposing original media publicly, trusting callbacks, or allowing provider failure to publish content. A deployment may use any real model behind a contract-compatible inference gateway.

## Goals / Non-Goals

**Goals:**

- Create durable, idempotent moderation work for each current reviewable video version.
- Produce deterministic bounded visual inputs from protected media.
- Exchange those inputs with an authenticated production inference gateway.
- Persist explicit source, provider, model, generation time, labels, confidence, and evidence timestamps.
- Roll out conservatively from human-only operation to policy enforcement.
- Recover retryable failures and route terminal/disabled cases to human review without pretending the recovery fact is model evidence.

**Non-Goals:**

- Hosting or training a model inside Frux.
- Choosing a specific cloud moderation vendor in core code.
- Sending full original videos to arbitrary third parties.
- Adding audio extraction, ASR, OCR, live-stream moderation, appeals, or reviewer consensus in the first version.
- Allowing the provider to write video/case state directly.

## Decisions

### Integrate through an authenticated HTTP inference gateway

Add a narrow application `ModerationProvider` interface with a production HTTP implementation. The gateway contract accepts canonical bounded inputs and returns canonical review signals:

```text
request:
  job_id, case_id, video_id, review_version
  requested_policy_version
  metadata title/description
  sampled frames: timestamp_ms, sha256, short-lived protected URL

response:
  provider, model_version, generated_at
  signals: canonical-or-unknown label, confidence, frame timestamps
```

Requests use HTTPS outside explicitly allowed local development, a timestamp, stable request ID, and HMAC signature from a dedicated secret. The response body, signal count, label length, confidence, timestamps, and model/provider identifiers are strictly bounded before conversion to the existing machine-result command.

The gateway, not Frux core, owns vendor SDKs, vendor label mapping, asynchronous vendor polling, and model-specific credentials. This keeps the provider replaceable while still making the configured production exchange real.

Alternative considered: add one cloud-vendor SDK directly to the Worker. Rejected because it couples review application code, secrets, retries, and response types to one vendor and makes self-hosted inference harder.

### Create moderation jobs with review intake

Add `review_moderation_job`, uniquely keyed by `(case_id, review_version, provider_config_version)`, with status `pending`, `leased`, `retry_wait`, `submitted`, `terminal`, or `cancelled`; attempt count; `available_at`; lease owner/expiry; input profile version; deterministic result ID; bounded error code; and timestamps.

Case intake creates the job in the same transaction when the configured rollout mode calls a provider. Reconciliation creates missing jobs for current reviewable cases, expires abandoned leases by database time, and cancels jobs whose case/video version is stale.

Workers claim jobs using `FOR UPDATE SKIP LOCKED`, a bounded lease, and deterministic ordering. The maximum attempts, backoff, call timeout, and concurrent jobs are configured within hard limits. Result submission uses a deterministic provider result identity so a response timeout or Worker restart cannot duplicate signals or decisions.

### Use deterministic bounded frame sampling

The first input profile uses the verified browser baseline or protected source and extracts at most 12 JPEG frames distributed deterministically across duration, with the longest edge at most 512 pixels and a total encoded budget at most 8 MiB. Title and description are included within their existing bounds. Audio, comments, user profile data, and unrelated metadata are excluded.

Frames are stored under a protected temporary moderation prefix with owner/case metadata and short retention. The gateway receives URLs expiring within the job-call window. Persisted evidence references contain video timestamps and hashes, not reusable object URLs. A cleanup task deletes samples after result acceptance or retention expiry.

Alternative considered: send the original video URL to the gateway. Rejected because it expands disclosure, transfer cost, processing time, and credential lifetime.

### Submit normalized results through the existing review service

The Worker converts a valid gateway response into the same application command used by the internal machine-result HTTP handler. It does not update case or video rows directly.

Machine results gain:

- `source_kind`: `production_provider`, `test_seed`, `recovery`, or `legacy_unknown`;
- `generated_at`;
- existing provider, model version, policy version, result identity, signals, and evidence.

The production Worker always sets `production_provider`. Manual fixtures must set `test_seed`. System fallback sets `recovery`. Existing rows migrate to `legacy_unknown` except the reserved manual seed provider, which migrates to `test_seed`.

### Gate automated outcomes by rollout mode

Configuration is versioned and exposes:

- `disabled`: do not call the gateway; submit a recovery fact that routes the case to human review.
- `observe`: call the real gateway and persist production evidence, but force every valid result to human review.
- `approve_only`: allow policy-qualified approval; any policy reject or human result goes to human review.
- `enforce`: apply the complete active approve/reject/human policy.

The effective rollout mode and policy version are persisted with the decision/routing history. Moving to a more permissive mode affects only new results; existing decisions are immutable.

Alternative considered: let deployment flags change policy behavior without provenance. Rejected because an auditor must be able to explain why the same evidence was routed differently.

### Route terminal provider failures to human review

Retryable transport, timeout, 429, 5xx, extraction, or malformed-response failures stay on the durable job with bounded backoff. When attempts are exhausted, or when mode is disabled, Frux submits one deterministic recovery result using:

- provider `frux-moderation-recovery`;
- source `recovery`;
- unknown label `moderation_unavailable`;
- bounded registered error evidence.

The existing unknown-label safety rule routes the case to human review. The video remains pending and non-public. The UI distinguishes the recovery fact from production-model evidence.

This provides liveness without manufacturing a safe/unsafe model judgment.

### Observe the pipeline without high-cardinality labels

Add fixed-label metrics for job creation, claim, extraction, provider call, result submission, retry, terminal fallback, and cancellation. Provider/model/video/job IDs are excluded from metric labels. Logs include job/case IDs and bounded error codes but redact signatures, signed URLs, response bodies, and secrets.

## Risks / Trade-offs

- [Gateway output quality or label mapping is wrong] → Start in observe mode, preserve unknown labels, compare human outcomes, and require explicit promotion to approve-only/enforce.
- [Frame sampling misses short harmful segments] → Use deterministic distributed samples, keep claims limited to sampled evidence, and defer richer temporal/audio coverage rather than overstating accuracy.
- [Protected samples leak] → Use temporary private objects, short-lived URLs, minimal metadata, redacted logs, and durable cleanup.
- [Provider outage grows the queue] → Bound concurrency/retries and route exhausted work to human review with explicit recovery provenance.
- [Observe mode increases human workload] → Measure provider/human agreement before enabling automated decisions and retain operator-controlled rollout.
- [Generic gateway still requires deployment work] → Provide a contract fixture and configuration validation; production enablement is impossible without a real configured gateway.

## Migration Plan

1. Add source/generation fields, job table, indexes, and cleanup metadata; backfill existing source classification conservatively.
2. Deploy the Worker with mode `disabled`, which keeps new videos human-reviewable without external calls.
3. Configure a contract-compatible real gateway and enable `observe`; compare production signals with human decisions.
4. Promote to `approve_only` only after measured agreement and threshold review.
5. Promote to `enforce` only after explicit operational approval and reject-threshold validation.
6. Roll back by returning to `disabled`; active jobs cancel or resolve to recovery/human routing, and no video is auto-published because of provider unavailability.

## Open Questions

The concrete gateway deployment and upstream model/vendor are environment decisions. The Frux contract and safety behavior do not depend on that choice.
