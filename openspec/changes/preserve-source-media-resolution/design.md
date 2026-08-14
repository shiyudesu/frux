## Context

The active processor creates a baseline plus every supported 480p/720p/1080p rendition and then
packages those files into DASH. A 1080p source therefore incurs three full video encodes, several
object uploads, and many DASH segment writes. Prod intentionally has one media slot, so this work
directly becomes creator-visible queue latency.

The API and Web already accept an ordered playback-source list containing only MP4, so adaptive
renditions are not required for compatibility. The protected original remains the source of truth,
while the required public baseline must stay browser-compatible and independently revocable.

## Goals / Non-Goals

**Goals:**

- Produce exactly one ready MP4 per newly processed asset.
- Preserve the uploaded resolution except for flooring odd dimensions to the nearest even value when
  H.264 encoding requires it.
- Avoid video encoding when H.264 can be copied safely into the baseline MP4.
- Remove DASH CPU, storage, object-operation, and playback-source overhead for new outputs.
- Let unfinished legacy-profile jobs recover with the same faster behavior.

**Non-Goals:**

- Preserve the original container or unsupported browser codec as the public baseline.
- Reprocess or delete renditions belonging to already-completed videos.
- Add client-selectable quality or adaptive bitrate playback.
- Introduce GPU encoding or parallel media workers.

## Decisions

### Create one source-resolution baseline

The processor will calculate one output size from the probed source dimensions. It will not construct
a rendition-height list and will not create DASH. The resulting variant remains
`source_type=mp4`, `role=baseline`, and `sort_order=10`, so existing publication and playback code
continues to resolve it.

Alternative: retain 480p as a fallback. Rejected because it still requires a second complete encode
and the user explicitly does not want selectable quality.

### Use stream copy whenever browser compatibility permits

When video is H.264 and audio is AAC or absent, ffmpeg will map the primary video and optional audio
and use stream copy into a fast-start MP4. When only audio is incompatible, video will be copied and
audio normalized to AAC. Other accepted video codecs will be transcoded once to H.264 with the
configured preset while keeping source dimensions.

Alternative: expose the original object directly. Rejected because originals can use incompatible
containers/codecs and are governed by protected-source lifecycle rules rather than public variants.

### Activate profile v2 with legacy retry compatibility

New uploads use `profile_version=v2`, whose contract is the single source-resolution MP4. The
processor keeps accepting unfinished `v1` jobs but applies the same recovery implementation because
those jobs have no committed ready outputs. Existing completed `v1` assets and variants are not
changed.

Alternative: migrate every durable job row from v1 to v2. Rejected because it introduces profile
identity conflicts and unnecessary operational migration for jobs that can safely finish in place.

### Keep a single MP4 playback source

No API shape changes are required. New processed videos return one MP4 in `playback_sources`, and
`media_url` resolves to the same baseline. Existing videos may continue returning their previously
generated DASH and rendition sources. The player hides its quality menu when fewer than two
selectable qualities exist, so new single-output videos do not present a meaningless selector.

## Risks / Trade-offs

- **Large source-resolution files increase delivery bandwidth** → retain the existing upload-size
  limit and private-object delivery; this change explicitly favors source clarity over adaptive
  bandwidth.
- **4K software transcoding can still be slow** → stream-copy compatible H.264 and otherwise perform
  only one encode instead of three.
- **Legacy v1 retries produce fewer outputs than the historical profile description** → only
  unfinished jobs use the recovery behavior; completed v1 outputs remain immutable.
- **Some H.264 streams cannot be copied cleanly into MP4** → treat remux failure as retryable and
  retain actionable ffmpeg diagnostics rather than silently falling back to success.

## Migration Plan

1. Register and activate processing profile `v2`.
2. Deploy API and Worker together so new jobs are created as v2 and both v1/v2 jobs are understood.
3. Existing pending/retryable v1 jobs are claimed normally and finish with one baseline MP4.
4. Existing completed media remains unchanged.
5. Rollback restores v1 job creation; v2 jobs remain durable and require the newer Worker to process.

## Open Questions

None.
