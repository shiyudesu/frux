## 1. Media Domain and Schema

- [ ] 1.1 Define media asset, variant, processing profile/job, upload session, and media status domain entities and errors.
- [ ] 1.2 Add repository interfaces for immutable assets, variants, processing jobs, upload sessions, and cleanup tasks.
- [ ] 1.3 Add PostgreSQL models, migrations, indexes, uniqueness constraints, and legacy-ready backfill.
- [ ] 1.4 Extend video entities and DTOs with media asset reference, processing state, and additive ordered playback sources.
- [ ] 1.5 Add migration and repository tests for legacy compatibility and variant ordering.

## 2. Storage and Delivery Adapters

- [ ] 2.1 Define object-store and delivery-URL interfaces for put, head, delete, presign, and public resolution.
- [ ] 2.2 Implement the local filesystem adapter through the common interfaces.
- [ ] 2.3 Implement an S3-compatible object-storage adapter and configuration validation.
- [ ] 2.4 Add CDN/public base URL and protected signed-URL resolution without exposing credentials.
- [ ] 2.5 Add adapter tests for ownership, checksums, object keys, expiry, and error mapping.

## 3. Upload Sessions

- [ ] 3.1 Add authenticated create/complete upload-session application services with owner, kind, size, checksum, key, and expiry validation.
- [ ] 3.2 Add upload-session HTTP DTOs, routes, idempotency handling, and response contracts.
- [ ] 3.3 Update the Web upload API and page to use direct production uploads with progress while retaining multipart fallback.
- [ ] 3.4 Verify completed objects before attaching them to immutable media assets.
- [ ] 3.5 Add API-flow tests for valid completion, expiry, owner mismatch, checksum mismatch, and replay.

## 4. Processing Pipeline

- [ ] 4.1 Add RabbitMQ media-processing event/queue configuration and idempotent worker consumption.
- [ ] 4.2 Implement versioned ffprobe inspection and processing-profile selection.
- [ ] 4.3 Generate the baseline H.264/AAC faststart MP4 and bounded non-upscaled MP4 renditions.
- [ ] 4.4 Generate and verify the DASH manifest and fragmented media outputs.
- [ ] 4.5 Publish outputs from temporary keys only after checksum and metadata validation.
- [ ] 4.6 Record retryable/terminal failures, attempts, leases, and worker metrics.

## 5. Publication and API Assembly

- [ ] 5.1 Gate public video eligibility on required baseline readiness while preserving legacy-ready video reads.
- [ ] 5.2 Update Feed, detail, profile, recommendation, and preload card assembly with additive playback sources.
- [ ] 5.3 Project a compatible baseline into existing `media_url` and normalized cover into `cover_url`.
- [ ] 5.4 Expose owner-visible processing and failure state in creator content APIs and UI.
- [ ] 5.5 Add cache invalidation when baseline or additional variants become ready.

## 6. Delivery Security and Caching

- [ ] 6.1 Configure public immutable variant and cover responses with Range, HEAD, ETag, and long-lived cache headers.
- [ ] 6.2 Keep originals, private variants, and incomplete assets behind current authorization or short-lived signed URLs.
- [ ] 6.3 Ensure visibility/delete transitions remove public source discovery without granting cross-owner access.
- [ ] 6.4 Add tests for public partial content, CDN headers, private denial, owner access, signed expiry, and legacy local assets.

## 7. Reconciliation and Operations

- [ ] 7.1 Implement reconciliation for expired leases, missing objects, missing database rows, and incomplete variant sets.
- [ ] 7.2 Implement delayed tombstone-based object cleanup for deleted videos and abandoned uploads.
- [ ] 7.3 Add object-storage, processing queue, rendition success, failure, latency, and cleanup metrics.
- [ ] 7.4 Add Compose/local integration support for an S3-compatible development service if selected.

## 8. Verification and Documentation

- [ ] 8.1 Add end-to-end tests from upload session through processing, public playback-source response, Range delivery, and deletion cleanup.
- [ ] 8.2 Update video, playback, architecture, engineering, security, deployment, optimization, and performance documentation.
- [ ] 8.3 Validate the breaking processing-state rollout and rollback path against legacy clients.
- [ ] 8.4 Run targeted Go tests, Web build, Compose validation, media probes, Windows Chrome playback checks, and strict OpenSpec validation.
