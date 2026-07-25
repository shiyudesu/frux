## Context

Authenticated uploads are stored under local `./uploads`, validated by ffprobe, and rewritten with MP4 faststart. Video and cover URLs are stored directly on `video`. Protected referenced assets pass through authorization and receive `Cache-Control: no-store`, including public videos. There is no object-storage abstraction, rendition model, processing state, direct upload, or CDN cache contract.

The production path must preserve local development and existing `media_url` consumers while introducing asynchronous processing and immutable delivery.

## Goals / Non-Goals

**Goals:**

- Support local storage and S3-compatible production object storage behind narrow interfaces.
- Normalize uploaded media into browser-compatible playback outputs and covers.
- Deliver public variants through immutable CDN URLs with Range and cache validation.
- Protect originals, private assets, and incomplete processing.
- Preserve additive compatibility for existing video and Feed clients.
- Make processing retryable, observable, and reconcilable.

**Non-Goals:**

- Implementing a vendor-specific DRM system.
- Migrating every existing local file to object storage in one deployment.
- Replacing content moderation or video lifecycle rules.
- Building the final Web adaptive player in this change.

## Decisions

### 1. Separate logical video records from physical media assets

Add:

- `media_asset`: owner, kind, storage backend, object key, content type, size, checksum, probe metadata, state, and timestamps.
- `media_variant`: asset/video linkage, format, codec, width, height, bitrate, object key, manifest role, readiness, and checksum.
- `media_processing_job`: requested profile version, state, attempts, error code, lease, and timestamps.

`video` keeps `media_url` and `cover_url` as compatibility projections and gains `media_asset_id` plus `media_status`. New responses add ordered `playback_sources`.

### 2. Use pluggable ingest and delivery interfaces

Infrastructure provides `MediaObjectStore` for put/head/delete/presign and `MediaURLResolver` for public CDN and protected signed URLs. Local development implements the same interfaces over `./uploads`. Production uses an S3-compatible endpoint and configured CDN base URL.

Object keys are immutable and versioned by asset ID, processing profile, and checksum. Public variants never require per-request database authorization; private/original URLs are short-lived signed URLs or remain behind the API asset handler.

### 3. Add direct production upload sessions with multipart fallback

Production Web uploads use:

1. `POST /api/upload-sessions` to validate intent and receive a presigned object upload.
2. Direct object upload.
3. `POST /api/upload-sessions/{id}/complete` to verify size, checksum, owner, and object metadata.

The existing multipart `/api/uploads` remains for local mode and compatibility. Both paths create immutable ownership records.

### 4. Process media asynchronously with a versioned profile

Completing a video asset enqueues a media-processing job. The worker probes the original, then produces:

- a baseline H.264/AAC faststart MP4,
- bounded 480p, 720p, and 1080p MP4 renditions when the source supports them,
- a DASH manifest and fragmented MP4 segments from the same normalized renditions,
- a normalized cover when required.

Renditions never upscale beyond the source. Bitrate, frame rate, and audio settings live in a versioned processing profile. Jobs are idempotent by `(asset_id, profile_version)`, write to temporary keys, and publish immutable outputs only after checksum verification.

Alternative: serve originals and let browsers choose. This was rejected because accepted codecs such as HEVC, VP9, and AV1 are not uniformly playable and originals provide no bitrate ladder.

### 5. Gate public availability on a required baseline

A video is eligible for public Feed/detail/recommendation only when the required baseline variant is ready. Additional renditions can become available additively. Existing videos with readable local media are marked `legacy_ready` and continue using `media_url` until migrated.

New object-storage uploads return the video plus `media_status=processing`; they do not enter public Feed until baseline processing succeeds. Processing failure is visible to the owner and retryable.

### 6. Define explicit cache and authorization policy

- Public immutable variants/covers: content-addressed URL, `Cache-Control: public, max-age=31536000, immutable`, ETag, Range/HEAD, CDN caching.
- Manifests: short cache with versioned variant references.
- Private/original assets: signed short-lived URL or authorized API response, `private/no-store`.
- Deleted or privacy-changed videos: public variant discovery is removed immediately; immutable objects are garbage-collected after a safety delay.

### 7. Use reconciliation instead of trusting queue completion

A periodic reconciler finds stuck jobs, missing objects, variant/database mismatches, and orphaned objects. Deletion writes a tombstone and cleanup task rather than synchronously deleting many objects inside the user request.

## Risks / Trade-offs

- [Transcoding is CPU and storage intensive] -> Bound profiles, worker concurrency, retries, and rendition count; expose queue and processing metrics.
- [Public immutable URLs cannot be revoked instantly from caches] -> Use unguessable versioned URLs, remove discovery, and reserve CDN purge for governance emergencies.
- [Direct upload can be abused] -> Bind sessions to owner, kind, size, checksum, expiration, and exact object key.
- [Dual local/object modes diverge] -> Share domain contracts and run repository/handler tests against both adapters where practical.
- [Processing changes publish latency] -> Preserve legacy-ready content and expose explicit owner processing state.

## Migration Plan

1. Add storage interfaces, models, configuration, and local adapters without changing current responses.
2. Register legacy local assets as `legacy_ready` and backfill video asset references where ownership is unambiguous.
3. Add processing queue/worker and generate variants for new opt-in uploads.
4. Enable object-storage upload sessions and additive `playback_sources`.
5. Gate new production uploads on baseline readiness, then migrate existing assets in batches.
6. Enable CDN public delivery after Range/cache/security validation.
7. Roll back new ingestion to local multipart mode; existing additive records and compatibility URLs remain readable.

## Open Questions

- Initial object-storage/CDN provider and whether local integration tests use MinIO.
- Exact bitrate ladder and DASH segment duration after representative content testing.
- Retention policy for originals after all required variants are healthy.
