## Context

Prod uses one Worker on a small single-server Compose stack. Media jobs are durable and recoverable,
but the runtime previously treated Worker as optional during deployment. The ffmpeg processor also
hard-codes a 10-minute input limit, a 15-minute timeout for each command, and the `medium` x264
preset while serially creating the configured rendition ladder. Real production evidence shows
accepted videos reaching the timeout at less than realtime encoding speed, DASH packaging failures
whose terminal stderr is not retained, and expired attempts returning to the queue without a reason.

The current `v1` profile identifies the browser-facing output topology and codecs. Existing pending
and retryable `v1` jobs must benefit from the fix without a data migration or a second job identity.

## Goals / Non-Goals

**Goals:**

- Always deploy and health-gate Prod Worker.
- Let existing retryable jobs complete under safer single-server processing budgets.
- Make input duration, command timeout, and x264 speed preset validated runtime configuration.
- Preserve actionable timeout, DASH, and expired-lease diagnostics.
- Reproduce DASH packaging against multi-rendition media with optional audio.

**Non-Goals:**

- Persisting frame-level percentage progress or streaming raw ffmpeg output to the Web client.
- Parallelizing one asset across multiple hosts.
- Changing public playback response shapes or the durable job identity.
- Replacing ffmpeg or introducing a managed transcoding provider.

## Decisions

### Keep the existing profile identity and configure runtime policy separately

`profile_version=v1` continues to define the output ladder, codecs, bitrates, and DASH layout.
`max_duration`, `command_timeout`, and `ffmpeg_preset` become validated processing runtime settings.
This allows existing `v1` retries to use the repaired runtime without rewriting job identities.

Alternative: introduce `v2` and migrate every active job. Rejected because it requires superseding
old durable jobs safely and delays recovery of the current production backlog.

### Use single-server-safe Prod defaults

Prod will allow videos up to 180 minutes, use a 360-minute timeout per ffmpeg invocation, retain one
media worker slot, and use the allowlisted `veryfast` x264 preset. The rendition topology remains
bounded and non-upscaling. Local Docker configuration uses the same explicit fields so behavior is
testable and configuration drift is visible.

Alternative: increase Worker concurrency. Rejected as the primary fix because concurrent x264 jobs
on the same VPS compete for CPU and memory and can make every job slower.

### Distinguish command timeout from arbitrary process failure

The command runner will inspect its derived context when `exec.CommandContext` returns. Deadline
expiry becomes a stable timeout error, while parent cancellation and ordinary ffmpeg exit failures
remain distinct. Processing error codes will therefore identify timeout rather than persisting only
`signal: killed`.

### Retain diagnostic tails

ffmpeg normally prints the actionable error at the end of stderr. Durable job error truncation will
retain the final bounded bytes, not the beginning. Expired lease recovery will set a safe
`lease_expired` code and message while keeping the job retryable.

### Verify DASH packaging with production-shaped inputs

DASH generation will continue to package verified MP4 renditions, but stream mapping and adaptation
sets will be covered by an integration fixture containing multiple video renditions and optional
audio. The implementation will use explicit stream mapping accepted by the packaged ffmpeg version
and verify the generated manifest and referenced files.

### Keep Worker mandatory in Prod

Worker will not use an optional Compose profile. Deployment pulls, starts, health-checks, restores,
and documents API, Web, and Worker as one required release unit.

## Risks / Trade-offs

- **Longer commands can occupy the only Worker slot for longer** → retain lease heartbeats, bounded
  retries, metrics, and one-slot execution; use `veryfast` to reduce wall time.
- **A 180-minute source can occupy a small VPS for several hours** → expose explicit runtime
  settings and document that this stack is personal/pre-production rather than realtime encoding.
- **Changing x264 preset changes compression efficiency for retries under the same profile** → codec,
  bitrate, dimensions, and playback contract remain unchanged; existing completed outputs are not
  reprocessed.
- **DASH behavior can vary by ffmpeg release** → run the integration test in the API container's
  packaged ffmpeg environment and retain the terminal diagnostic tail on failure.

## Migration Plan

1. Deploy the configuration and processor changes with Worker mandatory.
2. Worker polling automatically claims existing pending and retryable `v1` jobs.
3. Existing terminal `duration_limit` jobs remain terminal; operators can explicitly reset them if
   they want the newly configured duration policy to reprocess those assets.
4. Rollback restores the prior image and configuration. Durable jobs and variants require no schema
   rollback.

## Open Questions

None.
