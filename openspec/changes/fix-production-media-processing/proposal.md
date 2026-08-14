## Why

Production uploads can remain processing indefinitely or repeatedly fail because the deployment did
not always start Worker, the hard-coded H.264 ladder can exceed its fixed ffmpeg timeout on the
single-server Prod host, and persisted command errors can omit the diagnostic tail needed to repair
DASH failures.

## What Changes

- Require the Prod Compose deployment to start and health-check Worker with API and Web.
- Make media processing limits and encoding controls explicit configuration rather than fixed
  implementation constants.
- Use a single-server-safe processing profile that avoids deterministic timeouts while preserving a
  browser-compatible baseline and bounded adaptive outputs.
- Distinguish command timeout from process termination and retain the actionable tail of ffmpeg
  diagnostics in durable job state.
- Keep expired-lease recovery retryable while preserving a safe reason that explains why the attempt
  returned to the queue.
- Add regression coverage for long-running transcoding, DASH packaging, retry diagnostics, and Prod
  Worker startup.
- Synchronize media, deployment, and operations documentation with the corrected behavior.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `durable-media-work-jobs`: Require actionable retry diagnostics and timeout-safe durable recovery.
- `production-media-delivery`: Make processing limits/profile behavior configurable and require
  viable processing for accepted video durations.
- `ghcr-prod-deployment`: Make Worker a required, health-gated service in every Prod release.

## Impact

Affected areas include Prod Compose and deployment tests, media configuration, the ffmpeg processor,
durable media-job recovery, media integration/unit tests, and deployment/media documentation. No
public API compatibility break is intended; accepted upload behavior may become more explicit when a
configured duration limit is exceeded.
