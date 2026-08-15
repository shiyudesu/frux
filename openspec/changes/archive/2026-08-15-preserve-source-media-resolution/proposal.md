## Why

The current media profile decodes and encodes the full source up to three times and then packages a
DASH bundle, making a single upload CPU- and network-intensive on the one-Worker Prod server.
Creators prefer preserving the uploaded resolution over selectable rendition quality.

## What Changes

- Replace the 480p/720p/1080p rendition ladder with exactly one browser-compatible MP4 at the source
  resolution, except for the minimal even-dimension adjustment required by H.264.
- Remove DASH manifest and segment generation for newly processed or retried media.
- Fast-remux H.264 video with AAC or no audio instead of re-encoding when the source streams are
  already browser-compatible.
- When normalization is required, transcode only once at the source resolution to H.264/AAC.
- Introduce a new active processing profile while allowing unfinished legacy-profile jobs to use the
  same single-output recovery behavior.
- Preserve existing completed videos and their already-published playback sources.
- Update processing tests and media/product documentation for the single-output contract.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `production-media-delivery`: Change new media processing from a multi-rendition adaptive ladder to
  one source-resolution MP4 with a stream-copy fast path.

## Impact

Affected areas include the ffmpeg processor, processing-profile registration and configuration,
media integration tests, output/storage volume, playback source generation, and media/optimization
documentation. Existing API fields remain compatible but newly processed videos normally expose one
MP4 playback source and no DASH source.
