## RENAMED Requirements

- FROM: `Rainyun Provider Contract Verification`
- TO: `Production Object Storage Contract Verification`

## MODIFIED Requirements

### Requirement: Environment-Isolated S3-Compatible Deployment
Frux SHALL retain bundled MinIO as the default local Docker Compose media storage and SHALL provide
a separate production MinIO configuration with path-style addressing, a non-empty signing region,
private bucket initialization, separate root and application credentials, and separate runtime and
browser-presign endpoints.

#### Scenario: Developer starts the default local stack
- **WHEN** a developer runs the documented default Docker Compose command without production variables or overrides
- **THEN** API and Worker use the existing initialized development MinIO bucket and local browser upload flow

#### Scenario: Operator selects the production deployment
- **WHEN** an operator explicitly selects Prod and supplies all required deployment secrets
- **THEN** production API and Worker use the Prod MinIO service and application identity created by the Prod initializer

#### Scenario: Production storage credentials are absent
- **WHEN** an operator renders or starts Prod without required MinIO root or application credentials
- **THEN** production deployment fails explicitly while the independent default local MinIO configuration remains usable

### Requirement: Production Browser Direct Upload Policy
The deployment SHALL configure the private production MinIO bucket to permit CORS only from the
configured Frux application origin, including its public high port, for the methods and headers
required by the upload-session contract. Frux SHALL rely on short-lived object-scoped presigned URLs
rather than Bucket credentials for browser upload authorization.

#### Scenario: Production browser uploads a signed object
- **WHEN** the Frux Web application sends the issued presigned PUT with content type, private cache control, SHA-256 checksum, and SHA-256 metadata headers
- **THEN** MinIO accepts the exact-origin preflight and object upload and exposes the response headers required for diagnostics

#### Scenario: Unconfigured browser origin sends preflight
- **WHEN** a different browser origin attempts the production upload request
- **THEN** MinIO does not grant that origin CORS access even if it possesses no storage credential

#### Scenario: Signed upload URL leaks
- **WHEN** another client obtains an unexpired presigned PUT URL
- **THEN** the URL remains a temporary credential limited to its signed object, headers, checksum, and expiry

### Requirement: Private-Bucket Public Media Redirect
The production MinIO bucket SHALL remain private, and Frux SHALL serve stable virtual public-media
URLs that authorize each currently eligible exposure while resolving a separate protected storage
key. Frux SHALL redirect media-byte GET requests to a MinIO presigned GET lasting no more than 30
minutes and SHALL permit browser caching only within the same revocation bound.

#### Scenario: Public v3 media is requested
- **WHEN** a browser requests a currently eligible v3 exposure through the Frux public media route
- **THEN** Frux resolves the protected key, returns a cacheable redirect lasting less than the signed URL lifetime, and the signed MinIO response permits at most 30 minutes of revalidating cache

#### Scenario: Redirect is requested repeatedly
- **WHEN** the same exposure is requested repeatedly during the safe redirect-cache window
- **THEN** Frux may reuse the same signed MinIO URL so the browser can reuse its cached redirect and media ranges

#### Scenario: Public object metadata is requested
- **WHEN** a browser sends HEAD for an eligible virtual exposure
- **THEN** Frux resolves and returns content type, content length, ETag, Range support, and bounded cache metadata without disclosing the protected storage key

#### Scenario: Protected object is requested anonymously
- **WHEN** an unauthenticated caller requests an original upload, protected key, moderation sample, unknown exposure, or public-ineligible media
- **THEN** Frux denies the request and issues no signed URL

#### Scenario: Video becomes public-ineligible
- **WHEN** a previously public generation URL is requested after the video becomes private, offline, rejected, deleted, or failed
- **THEN** Frux denies new redirects while previously cached redirects and signed URLs expire within the configured 30-minute maximum

#### Scenario: Owner or reviewer requests protected media
- **WHEN** current owner or reviewer authorization permits protected access
- **THEN** Frux continues to issue separate short-lived `private, no-store` access that never inherits public cache behavior

### Requirement: Production Object Storage Contract Verification
The self-hosted MinIO rollout SHALL verify the exact S3 operations, proxy behavior, and metadata
semantics required by Frux before the deployment is considered ready.

#### Scenario: Direct upload completes
- **WHEN** an authenticated user uploads a valid cover or video through a MinIO-backed upload session
- **THEN** CORS, presigned PUT, checksum metadata, `HeadObject`, upload completion, and durable asset creation all succeed without weakening validation

#### Scenario: Worker processes MinIO media
- **WHEN** a valid uploaded video enters the durable media processing job
- **THEN** Worker can read the source, write and verify outputs, list and delete scoped objects, and publish a playable baseline

#### Scenario: Public playback uses the reverse proxy
- **WHEN** Frux issues a signed MinIO GET through the public S3 hostname
- **THEN** Caddy preserves signature inputs and the browser receives correct redirect, Range, HEAD, ETag, and cache behavior

#### Scenario: Provider contract is incompatible
- **WHEN** checksum, custom metadata, signed PUT/GET, listing, deletion, redirect, Range, HEAD, ETag, CORS, or cache behavior is incompatible
- **THEN** production rollout fails explicitly without weakening the default local MinIO workflow

### Requirement: Secret-Safe Production Storage Configuration
MinIO root credentials and Frux S3 application credentials SHALL be supplied only through production
deployment secrets and SHALL NOT be required by local development, committed, documented as usable
values, emitted in validation output, or shared with each other.

#### Scenario: Repository configuration is inspected
- **WHEN** a contributor reviews tracked local and production Compose, YAML, documentation, and example environment files
- **THEN** only variable names and non-secret endpoint, region, bucket, and port examples are present

#### Scenario: Application container is compromised
- **WHEN** API or Worker storage credentials are disclosed
- **THEN** those credentials do not grant MinIO root administration, Bucket policy/CORS/anonymous-access mutation, root-managed marker access, or object access outside the registered Frux prefixes

#### Scenario: Application Access Key is rotated
- **WHEN** the configured Frux S3 application Access Key changes and initialization succeeds
- **THEN** the previous managed MinIO identity is revoked and can no longer read or write Bucket objects

#### Scenario: Developer works without production secrets
- **WHEN** production MinIO is unavailable, unconfigured, or fails compatibility validation
- **THEN** the documented default local Compose workflow continues to use development MinIO without production secrets or application-code changes

## ADDED Requirements

### Requirement: Separated Runtime and Browser S3 Endpoints
Production API and Worker SHALL use the internal Compose MinIO endpoint while presigned browser
requests SHALL use the dedicated public S3 HTTPS origin containing the allocated public NAT port.

#### Scenario: Worker accesses object storage
- **WHEN** API or Worker performs an unsigned runtime S3 operation
- **THEN** traffic remains on the Compose backend network and does not traverse Caddy or the NAT gateway

#### Scenario: Browser receives a presigned request
- **WHEN** Frux creates a signed upload or download request
- **THEN** the URL contains the public S3 hostname and configured public HTTPS port and is reachable from the browser
