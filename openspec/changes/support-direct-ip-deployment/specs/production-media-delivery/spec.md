## MODIFIED Requirements

### Requirement: Production Browser Direct Upload Policy
The deployment SHALL configure the private production MinIO bucket to permit CORS only from the
configured complete Frux application origin for the methods and headers required by the
upload-session contract. Frux SHALL rely on short-lived object-scoped presigned URLs rather than
Bucket credentials for browser upload authorization. The application origin SHALL use HTTPS by
default and MAY use HTTP only in explicitly configured direct-IP mode.

#### Scenario: Production browser uploads a signed object
- **WHEN** the Frux Web application sends the issued presigned PUT with content type, private cache control, SHA-256 checksum, and SHA-256 metadata headers
- **THEN** MinIO accepts the exact-origin preflight and object upload and exposes the response headers required for diagnostics

#### Scenario: Unconfigured browser origin sends preflight
- **WHEN** a different browser origin attempts the production upload request
- **THEN** MinIO does not grant that origin CORS access even if it possesses no storage credential

#### Scenario: Signed upload URL leaks
- **WHEN** another client obtains an unexpired presigned PUT URL
- **THEN** the URL remains a temporary credential limited to its signed object, headers, checksum, and expiry

### Requirement: Production Object Storage Contract Verification
The self-hosted MinIO rollout SHALL verify the exact S3 operations, public endpoint behavior, and
metadata semantics required by Frux before the deployment is considered ready.

#### Scenario: Direct upload completes
- **WHEN** an authenticated user uploads a valid cover or video through a MinIO-backed upload session
- **THEN** CORS, presigned PUT, checksum metadata, `HeadObject`, upload completion, and durable asset creation all succeed without weakening validation

#### Scenario: Worker processes MinIO media
- **WHEN** a valid uploaded video enters the durable media processing job
- **THEN** Worker can read the source, write and verify outputs, list and delete scoped objects, and publish a playable baseline

#### Scenario: Public playback uses Caddy
- **WHEN** Frux issues a signed MinIO GET through the public S3 hostname in Caddy mode
- **THEN** Caddy preserves signature inputs and the browser receives correct redirect, Range, HEAD, ETag, and cache behavior

#### Scenario: Public playback uses a direct IP
- **WHEN** Frux issues a signed MinIO GET through the configured direct-IP S3 port
- **THEN** MinIO receives the original Host, path, query, method, and Range request and the browser receives correct redirect, Range, HEAD, ETag, and cache behavior

#### Scenario: Provider contract is incompatible
- **WHEN** checksum, custom metadata, signed PUT/GET, listing, deletion, redirect, Range, HEAD, ETag, CORS, or cache behavior is incompatible
- **THEN** production rollout fails explicitly without weakening the default local MinIO workflow

### Requirement: Separated Runtime and Browser S3 Endpoints
Production API and Worker SHALL use the internal Compose MinIO endpoint while presigned browser
requests SHALL use the configured public S3 origin. The public S3 origin SHALL use HTTPS by default
and MAY use an HTTP IP literal with a distinct port only in explicitly configured direct-IP mode.

#### Scenario: Worker accesses object storage
- **WHEN** API or Worker performs an unsigned runtime S3 operation
- **THEN** traffic remains on the Compose backend network and does not traverse Caddy, the public host binding, or the NAT gateway

#### Scenario: Browser receives a presigned request in Caddy mode
- **WHEN** Frux creates a signed upload or download request for the default domain deployment
- **THEN** the URL contains the public S3 hostname and configured public HTTPS port and is reachable from the browser

#### Scenario: Browser receives a presigned request in direct-IP mode
- **WHEN** Frux creates a signed upload or download request for an explicit direct-IP deployment
- **THEN** the URL contains the configured IPv4 literal and distinct public S3 HTTP port and is reachable from the browser
