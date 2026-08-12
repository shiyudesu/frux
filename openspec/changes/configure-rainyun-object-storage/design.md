## Context

The default Docker Compose environment is a local development stack. It mounts `apps/api/configs/config.docker.yaml`, starts the bundled MinIO service, initializes bucket `frux-media`, and allows the local Web origins. That workflow should remain self-contained and unchanged.

Rainyun is intended only for a future production deployment. It exposes the S3-compatible endpoint `https://cn-zj1.rains3.com`; bucket `frux1` exists and currently rejects anonymous bucket access. Frux already implements the required S3 operations with AWS SDK v2, path-style addressing, presigned browser PUT, checksum metadata validation, protected presigned GET, public URL resolution, Worker list/delete, and media processing.

The production Web origin and CDN/custom media domain do not exist yet. AccessKey and SecretKey are available to the operator but MUST NOT be placed in the repository, OpenSpec artifacts, command output, or logs.

## Goals / Non-Goals

**Goals:**

- Preserve the existing local Compose, MinIO, `minio-init`, local credentials, and local browser CORS behavior.
- Add a separate production configuration for Rainyun bucket `frux1`.
- Add a separate production Compose definition that selects the production configuration and requires deployment secrets.
- Preserve separate server, browser-presign, and public-media URL semantics.
- Keep every Rainyun object private while delivering eligible public media through authorized signed redirects.
- Verify Rainyun support for the exact checksum, metadata, HEAD, listing, deletion, Range, and cache behavior Frux uses before production rollout.

**Non-Goals:**

- Using Rainyun from the default local development stack.
- Supporting Rainyun through SFTP or WebDAV.
- Changing media APIs, object-key layout, database schema, upload limits, or processing profiles.
- Committing live credentials or automating Rainyun account administration.
- Inventing a production Web origin, CDN domain, database, Redis, or Kafka deployment before those values exist.

## Decisions

### Keep local MinIO as the default development storage

`apps/docker-compose.yml` and `apps/api/configs/config.docker.yaml` remain the default local path. API and Worker continue to depend on `minio-init`, and the existing MinIO endpoint, bucket, public URL, credentials, and local CORS remain intact.

Rainyun outages, credentials, policies, or network access therefore cannot break ordinary local development.

### Use the existing S3 implementation in production

The production media configuration will use endpoint and presign endpoint `https://cn-zj1.rains3.com`, bucket `frux1`, non-empty signing region `us-east-1`, and `use_path_style: true`. Rainyun documents the region as arbitrary, while Frux validation requires a value; `us-east-1` is the conventional S3-compatible fallback.

SFTP and WebDAV are rejected because they do not support Frux's existing browser presigned PUT contract and would force uploads through the API or require a new provider adapter.

### Add isolated production configuration

A tracked production configuration template contains no usable credentials and references deployment environment variables for secrets. The separate production Compose stack implemented by `add-single-server-production-compose` mounts that configuration into API and Worker and requires the Rainyun credential variables.

The selected server definition includes PostgreSQL, Redis, one internal Kafka broker, loopback API/Web ports for the existing host Caddy, and PostgreSQL backup without inheriting the local MinIO dependency. The local Compose file remains unchanged and does not require Rainyun variables.

Production PostgreSQL, Redis, Kafka, JWT, internal token, and related security values remain environment-injected rather than copying local defaults into production.

### Separate upload, presign, and public delivery addresses

The Rainyun server and browser-presign endpoints remain `https://cn-zj1.rains3.com`. The production public media base is `https://${FRUX_DOMAIN}/media`; the resolver appends promoted object keys and produces stable application URLs such as `/media/media/v2/...`.

Storage URLs remain private implementation details. A future CDN can replace the signed redirect target without changing object keys or public application URLs.

### Accept and verify Rainyun gateway CORS

Rainyun does not expose Bucket-level CORS controls in its current panel. The `cn-zj1.rains3.com` gateway answers preflight with wildcard origin, methods, request headers, and exposed headers.

No provider-side CORS mutation is required. Deployment documentation records the observed behavior and a repeatable OPTIONS check. Browser upload security continues to rely on the authenticated Frux API issuing short-lived, single-object presigned URLs with signed checksum and metadata headers.

### Keep the whole bucket private and redirect eligible public reads

Rainyun's published management API exposes only a Bucket-wide anonymous-access toggle. It does not expose prefix-scoped public access. Frux therefore does not enable anonymous access on `frux1`.

Production `public_base_url` points to `https://${FRUX_DOMAIN}/media`. Public object URLs remain stable Frux URLs. For GET or HEAD, the API:

1. validates the object key and `media/` prefix;
2. asks the video repository whether that exact promoted object is currently public;
3. serves DASH MPD manifests and HEAD metadata from the stable Frux URL so relative segment URLs remain on the Frux origin;
4. creates a Rainyun presigned GET lasting no more than 60 seconds for MP4 and DASH segment GET requests;
5. returns a no-store `307 Temporary Redirect`, preserving Range headers.

The signed Rainyun GET overrides response caching to `private, no-store`. The browser downloads MP4 and segment bytes from Rainyun, so the VPS handles authorization, small manifests, HEAD metadata, and redirects rather than video bandwidth. Originals, protected outputs, moderation samples, and unknown public keys never receive redirects.

### Validate production independently from local development

Local validation continues to use MinIO and must remain green. Rainyun compatibility validation uses the production configuration and operator-supplied deployment secrets without printing credentials or signed URLs.

The production browser flow must receive a direct upload session, pass CORS preflight, complete the signed PUT with checksum and custom metadata, and complete the session after `HeadObject`. Worker validation must confirm source read, processed object write, list/delete permissions, and baseline readiness. Public playback validation must traverse the stable Frux URL, public authorization, 307 redirect, signed Rainyun GET, Range, HEAD, and ETag behavior.

## Risks / Trade-offs

- [Rainyun wildcard CORS permits requests from any browser origin] → Keep presigned URLs short-lived and object-scoped, never expose storage credentials, and treat signed URL leakage as credential leakage.
- [Rainyun may not support `x-amz-checksum-sha256` exactly as AWS S3 does] → Run a real production-config upload-session test before rollout and do not weaken checksum verification silently.
- [Every public media object request requires API authorization] → Keep the check indexed and lightweight; Rainyun still serves MP4 and segment bytes and Range traffic.
- [A separate production configuration can drift from the local template] → Document shared fields and validate both configurations whenever configuration structure changes.
- [Both production API and Worker receive one shared credential] → Use the narrowest bucket-scoped credential Rainyun supports; split credentials later if provider and deployment configuration allow it.
- [Signed Rainyun URLs appear in browser network logs] → Keep TTL short, issue them only after current public authorization, and treat them as temporary credentials.

## Migration Plan

1. Leave the existing local Compose and MinIO configuration unchanged.
2. Add a secret-free production configuration template and separate production Compose definition.
3. Supply Rainyun and other production secrets only through the deployment environment.
4. Keep `frux1` private and configure the public base URL to the Frux `/media` route.
5. Verify the Rainyun gateway preflight response for Frux's signed PUT headers.
6. Validate Rainyun direct upload, processing, protected access, authorized public redirect, and playback.
7. If any provider contract fails, do not deploy the production definition; local MinIO development remains unaffected.

## Open Questions

- Does Rainyun preserve `x-amz-meta-sha256` and expose the checksum fields required by Frux `HeadObject` validation?
- Does Rainyun's signed GET preserve Range, HEAD, ETag, and object Cache-Control behavior through browser redirects?
