## Why

GCFeed serves uploaded originals from local disk and marks referenced protected assets `no-store`, which prevents effective browser/CDN reuse and provides no normalized bitrate variants. This is suitable for local development but not for production-scale playback cost, reliability, or adaptive delivery.

## What Changes

- Introduce storage and delivery abstractions that support local development storage and production object storage/CDN backends.
- Persist immutable media assets, processing state, normalized metadata, covers, and ordered playback variants.
- Process published uploads asynchronously into browser-compatible MP4 renditions and an adaptive manifest, while retaining an original or baseline fallback.
- **BREAKING**: New production object-storage uploads remain owner-visible in a processing state and do not enter public Feed or detail reads until the required baseline rendition is ready; legacy/local-ready media remains compatible.
- Return additive playback-source metadata without removing the existing `media_url` and `cover_url` compatibility fields.
- Apply public immutable caching, ETag/Range behavior, and CDN-origin separation for public variants; keep private or unprocessed assets authorization-protected.
- Define failure, retry, reconciliation, cleanup, and publication rules so videos are not advertised with incomplete required media.

## Capabilities

### New Capabilities

- `production-media-delivery`: Defines media ingestion, asynchronous rendition processing, object storage, CDN caching, protected originals, and compatible playback-source delivery.

### Modified Capabilities

## Impact

- Affects upload handling, video entities and persistence, migrations, worker queues, ffmpeg processing, Feed/video DTOs, asset authorization, configuration, Compose deployment, and media tests.
- Adds object-storage/CDN configuration and a production deployment dependency while preserving local-disk development mode.
- Requires updates to video, playback, architecture, engineering, security, deployment, and performance documentation.
