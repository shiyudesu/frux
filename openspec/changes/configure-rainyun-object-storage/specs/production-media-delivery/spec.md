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
The Rainyun bucket CORS policy SHALL permit Frux direct-upload requests only from the configured production Web origin with the signed methods and headers required by the existing upload-session contract and SHALL NOT default to wildcard or local-development origins.

#### Scenario: Production browser uploads a signed object
- **WHEN** the production Frux Web application sends the issued presigned PUT with content type, private cache control, SHA-256 checksum, and SHA-256 metadata headers
- **THEN** Rainyun accepts the preflight and object upload and exposes the response headers required for diagnostics

#### Scenario: Unlisted browser origin attempts direct upload
- **WHEN** a browser origin other than the configured production Web origin attempts to use the bucket direct-upload path
- **THEN** the storage CORS policy does not grant that origin browser access

#### Scenario: Production Web origin is not configured
- **WHEN** the production Web origin is still unknown
- **THEN** Rainyun rollout remains incomplete and the deployment does not substitute `*`, `http://127.0.0.1:5173`, or `http://localhost:5173`

### Requirement: Prefix-Scoped Public Media Access
The Rainyun bucket SHALL remain private by default and SHALL grant anonymous object reads only for promoted objects under `media/*`.

#### Scenario: Public promoted media is requested
- **WHEN** a browser requests an eligible promoted object through `https://cn-zj1.rains3.com/frux1/media/...`
- **THEN** Rainyun returns the object with Range, HEAD, ETag, and bounded revalidation cache behavior

#### Scenario: Protected object is requested anonymously
- **WHEN** an unauthenticated caller requests an original upload, protected processed output, moderation sample, or any object outside `media/*`
- **THEN** Rainyun denies access without revealing storage credentials

#### Scenario: Provider cannot enforce prefix policy
- **WHEN** Rainyun cannot apply or verify anonymous read restricted to `media/*`
- **THEN** production deployment SHALL NOT enable bucket-wide public read or proceed until a safe delivery design exists

### Requirement: Rainyun Provider Contract Verification
The Rainyun rollout SHALL verify the exact S3 operations and metadata semantics required by Frux before the external storage migration is considered complete.

#### Scenario: Direct upload completes
- **WHEN** an authenticated user uploads a valid cover or video through a Rainyun-backed upload session
- **THEN** presigned PUT, checksum metadata, `HeadObject`, upload completion, and durable asset creation all succeed without weakening validation

#### Scenario: Worker processes Rainyun media
- **WHEN** a valid uploaded video enters the durable media processing job
- **THEN** Worker can read the source, write and verify outputs, list and delete scoped objects, and publish a playable baseline

#### Scenario: Provider contract is incompatible
- **WHEN** checksum, custom metadata, signed request, listing, deletion, Range, HEAD, ETag, or cache behavior is incompatible
- **THEN** production rollout fails explicitly without changing or weakening the default local MinIO workflow

### Requirement: Secret-Safe Production Storage Configuration
Rainyun AccessKey and SecretKey values SHALL be supplied only through production deployment secrets and SHALL NOT be required by local development, committed, documented as usable values, or emitted in validation output.

#### Scenario: Repository configuration is inspected
- **WHEN** a contributor reviews tracked local and production Compose, YAML, documentation, and example environment files
- **THEN** only variable names and non-secret Rainyun endpoint, region, and bucket values are present

#### Scenario: Developer works without production secrets
- **WHEN** Rainyun is unavailable, unconfigured, or fails compatibility validation
- **THEN** the documented default local Compose workflow continues to use MinIO without production secrets or application-code changes
