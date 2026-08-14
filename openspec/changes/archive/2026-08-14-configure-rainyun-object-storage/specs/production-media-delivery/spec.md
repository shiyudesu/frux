## ADDED Requirements

### Requirement: Environment-Isolated S3-Compatible Deployment
Frux SHALL retain bundled MinIO as the default local Docker Compose media storage and SHALL provide a separate production deployment configuration for Rainyun bucket `frux1` through `https://cn-zj1.rains3.com` with path-style addressing, a non-empty signing region, and separate runtime, browser-presign, and public-delivery address semantics.

#### Scenario: Developer starts the default local stack
- **WHEN** a developer runs the documented default Docker Compose command without Rainyun variables or production overrides
- **THEN** API and Worker use the existing initialized MinIO bucket and local browser upload flow

#### Scenario: Operator selects the production deployment
- **WHEN** an operator explicitly selects the production configuration and supplies all required deployment secrets
- **THEN** production API and Worker use Rainyun without starting or depending on the bundled MinIO initializer

#### Scenario: Production Rainyun credentials are absent
- **WHEN** an operator renders or starts the selected production deployment without both required Rainyun credential values
- **THEN** production deployment fails explicitly while the independent default local MinIO configuration remains usable

### Requirement: Production Browser Direct Upload Policy
The deployment SHALL verify that Rainyun's provider-managed wildcard CORS response permits the methods and headers required by the existing upload-session contract, and Frux SHALL rely on short-lived object-scoped presigned URLs rather than Bucket credentials for browser upload authorization.

#### Scenario: Production browser uploads a signed object
- **WHEN** the production Frux Web application sends the issued presigned PUT with content type, private cache control, SHA-256 checksum, and SHA-256 metadata headers
- **THEN** Rainyun accepts the preflight and object upload and exposes the response headers required for diagnostics

#### Scenario: Rainyun panel has no CORS editor
- **WHEN** the operator configures bucket `frux1`
- **THEN** no nonexistent Bucket CORS setting is required and the operator verifies the gateway OPTIONS response instead

#### Scenario: Signed upload URL leaks
- **WHEN** another browser origin obtains an unexpired presigned PUT URL
- **THEN** the URL is treated as a temporary credential limited to its signed object, headers, checksum, and expiry

### Requirement: Private-Bucket Public Media Redirect
The Rainyun bucket SHALL remain private, and Frux SHALL serve stable public-media application URLs that authorize each promoted object. Frux SHALL serve DASH manifests and HEAD metadata from the stable URL and SHALL redirect media-byte GET requests to a Rainyun presigned GET lasting no more than 60 seconds.

#### Scenario: Public promoted media is requested
- **WHEN** a browser requests a currently eligible promoted `media/*` object through the Frux public media route
- **THEN** Frux returns a no-store temporary redirect to a short-lived signed Rainyun URL and the browser can use Range and ETag behavior

#### Scenario: DASH manifest is requested
- **WHEN** a browser requests an eligible MPD manifest
- **THEN** Frux serves the small authorized manifest from the stable application URL so relative segment requests return through Frux authorization

#### Scenario: Public object metadata is requested
- **WHEN** a browser sends HEAD for an eligible public object
- **THEN** Frux returns authorized content type, content length, ETag, Range support, and bounded cache metadata without exposing a storage URL

#### Scenario: Protected object is requested anonymously
- **WHEN** an unauthenticated caller requests an original upload, protected output, moderation sample, unknown key, or public-ineligible media object
- **THEN** Frux denies the request and issues no signed URL

#### Scenario: Video becomes public-ineligible
- **WHEN** a previously public video's stable media URL is requested after it becomes private, offline, rejected, deleted, or failed
- **THEN** Frux denies a new redirect while previously issued signed URLs expire within their bounded lifetime

### Requirement: Rainyun Provider Contract Verification
The Rainyun rollout SHALL verify the exact S3 operations and metadata semantics required by Frux before the external storage migration is considered complete.

#### Scenario: Direct upload completes
- **WHEN** an authenticated user uploads a valid cover or video through a Rainyun-backed upload session
- **THEN** presigned PUT, checksum metadata, `HeadObject`, upload completion, and durable asset creation all succeed without weakening validation

#### Scenario: Worker processes Rainyun media
- **WHEN** a valid uploaded video enters the durable media processing job
- **THEN** Worker can read the source, write and verify outputs, list and delete scoped objects, and publish a playable baseline

#### Scenario: Provider contract is incompatible
- **WHEN** checksum, custom metadata, signed PUT/GET, listing, deletion, redirect, Range, HEAD, ETag, or cache behavior is incompatible
- **THEN** production rollout fails explicitly without changing or weakening the default local MinIO workflow

### Requirement: Secret-Safe Production Storage Configuration
Rainyun AccessKey and SecretKey values SHALL be supplied only through production deployment secrets and SHALL NOT be required by local development, committed, documented as usable values, or emitted in validation output.

#### Scenario: Repository configuration is inspected
- **WHEN** a contributor reviews tracked local and production Compose, YAML, documentation, and example environment files
- **THEN** only variable names and non-secret Rainyun endpoint, region, and bucket values are present

#### Scenario: Developer works without production secrets
- **WHEN** Rainyun is unavailable, unconfigured, or fails compatibility validation
- **THEN** the documented default local Compose workflow continues to use MinIO without production secrets or application-code changes
