## Why

Frux currently uses the bundled MinIO service for local Docker Compose development but does not have a separate production object-storage configuration. Production deployment should use the external Rainyun S3-compatible bucket `frux1` without changing the local MinIO workflow, committing credentials, or exposing protected upload, processing, or moderation objects.

## What Changes

- Add an explicit production API and Worker media configuration for the Rainyun endpoint `https://cn-zj1.rains3.com` using path-style S3 access and bucket `frux1`.
- Add a production Compose override that injects Rainyun credentials through environment variables and mounts the production configuration.
- Preserve the existing default Compose configuration, bundled MinIO services, MinIO initialization, local credentials, and local browser CORS unchanged.
- Document and verify Rainyun's provider-wide wildcard CORS behavior for browser direct upload.
- Keep the bucket private and document a prefix-scoped public-read policy for promoted `media/*` objects only.
- Validate the production override, direct upload, checksum metadata, media processing, protected access, and public playback independently from the existing local MinIO workflow.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `production-media-delivery`: Define a production-only external S3-compatible deployment, credential, browser-origin, public-prefix, and verification contract while retaining MinIO for local development.

## Impact

- Affected configuration: a new production API configuration and production Compose override; existing local Docker configuration remains unchanged.
- Affected operations: Rainyun gateway CORS verification and prefix-scoped anonymous read policy.
- Affected runtime systems: production API direct upload, production Worker media processing and cleanup, and production browser playback.
- No HTTP API or database schema changes are expected.
