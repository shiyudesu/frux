# production-media-delivery Specification

## Purpose

Defines production-ready media storage, processing, availability, playback delivery, access control, and lifecycle reconciliation.

## Requirements

### Requirement: Pluggable Media Storage
Frux SHALL store media through a common storage contract with a local development implementation and a production object-storage implementation.

#### Scenario: Local development upload completes
- **WHEN** local storage mode accepts a valid authenticated upload
- **THEN** the asset is stored locally with immutable owner metadata and remains usable by existing development flows

#### Scenario: Production direct upload completes
- **WHEN** an authenticated user completes a valid unexpired production upload session with the expected object key, size, and checksum
- **THEN** Frux records an immutable owned media asset and enqueues processing

#### Scenario: Upload session payload does not match
- **WHEN** the completed object does not match the session owner, kind, size, checksum, or key
- **THEN** completion is rejected and the object is not attached to a video

### Requirement: Actionable Upload Intent Validation
Production upload-session creation SHALL validate filename, media kind, content type, size, checksum, and idempotency metadata before issuing an object-storage request, and SHALL distinguish unsupported file constraints from upload-session state conflicts.

#### Scenario: Cover exceeds its size limit
- **WHEN** a client requests a cover upload session whose size exceeds the configured cover limit
- **THEN** Frux rejects the request with an actionable cover-size validation code and creates no upload session

#### Scenario: File type does not match its media kind
- **WHEN** a requested filename or content type is unsupported for the requested video or cover kind
- **THEN** Frux rejects the request with an actionable file-type validation code and creates no upload session

#### Scenario: Completed upload session is replayed
- **WHEN** a client retries the same owner, idempotency key, and upload fingerprint after that file completed
- **THEN** Frux returns the existing completed asset without requiring another object upload

#### Scenario: Paired upload retry uses independent keys
- **WHEN** a client retries only the failed member of a video and cover pair while reusing the completed member's original key
- **THEN** each upload session is resolved independently and the completed member remains reusable

### Requirement: Versioned Media Processing
Frux SHALL process each source asset with an idempotent versioned media profile, SHALL persist the
state and metadata of every generated output, and SHALL create a durable creator notification only
when the required current baseline reaches a terminal failure. New processing SHALL produce exactly
one browser-compatible MP4 baseline at the source resolution, except for the minimal even-dimension
adjustment required by H.264, and SHALL NOT generate selectable renditions or DASH outputs.
Input-duration policy, per-command timeout, and encoder speed preset SHALL remain explicit validated
runtime configuration. Worker SHALL upload a generated video body directly from its local temporary
file to the deterministic final protected key and SHALL NOT use the object store as an intermediate
temporary-file round trip.

#### Scenario: Browser-compatible source is uploaded
- **WHEN** the primary source video is H.264 and its optional audio is AAC
- **THEN** the worker stream-copies the selected streams into one fast-start baseline MP4 without changing the source resolution

#### Scenario: Only source audio requires normalization
- **WHEN** the primary video is H.264 but its audio is not AAC
- **THEN** the worker copies video, encodes audio once to AAC, and produces one source-resolution baseline MP4

#### Scenario: Source video requires normalization
- **WHEN** a valid uploaded source uses another accepted video codec
- **THEN** the worker performs one H.264/AAC normalization pass at the source resolution before public eligibility

#### Scenario: Source dimensions are odd
- **WHEN** H.264 encoding cannot retain an odd source width or height
- **THEN** the worker floors only the affected dimensions to the nearest positive even value

#### Scenario: Final output does not exist
- **WHEN** the deterministic protected output key is absent after local processing succeeds
- **THEN** Worker uploads the local output once, verifies size and checksum, and commits the variant without uploading or downloading an object-store temporary copy

#### Scenario: Matching final output already exists
- **WHEN** a retry finds the deterministic final key with the expected size and checksum
- **THEN** Worker reuses it idempotently without transferring the body again

#### Scenario: Final output metadata conflicts
- **WHEN** the deterministic final key exists with different size or checksum
- **THEN** processing fails explicitly and does not overwrite or advertise the conflicting file

#### Scenario: Worker exits after final PUT
- **WHEN** the final file commits but PostgreSQL finalization does not
- **THEN** the unreferenced deterministic file remains protected and delayed orphan reconciliation removes it only after the configured safety window

#### Scenario: Processing output is exposed
- **WHEN** the single baseline completes and the video's other public gates are satisfied
- **THEN** `media_url` and ordered playback sources expose the same MP4, no new DASH source is returned, and the player shows no quality selector for that single source

#### Scenario: Existing completed adaptive media is read
- **WHEN** a previously completed video already has rendition or DASH variants
- **THEN** those immutable outputs remain readable and are not reprocessed or deleted by this change

#### Scenario: Unfinished legacy-profile job is reclaimed
- **WHEN** a pending or retryable v1 job has no committed ready baseline
- **THEN** the current worker may finish it using the single source-resolution baseline behavior without creating duplicate concurrent output

#### Scenario: Accepted long source processes within configured budget
- **WHEN** a source is within the configured duration limit but processing requires more than 15 minutes on the production host
- **THEN** ffmpeg continues up to the configured command timeout with lease heartbeats remaining active

#### Scenario: Source exceeds configured duration
- **WHEN** probing finds a source duration greater than the configured processing limit
- **THEN** the job fails terminally with the stable `duration_limit` reason

#### Scenario: Invalid processing runtime configuration
- **WHEN** command timeout, maximum duration, or encoder preset is empty, out of bounds, or unsupported
- **THEN** API and Worker startup fail explicitly before accepting or claiming media work

#### Scenario: Processing job is delivered twice
- **WHEN** the same asset and processing-profile version is consumed more than once
- **THEN** output publication, database records, and terminal-failure notifications remain idempotent

#### Scenario: Processing fails retryably
- **WHEN** probing, remuxing, transcoding, checksum validation, or object publication fails with attempts remaining
- **THEN** the asset records a retryable failure without advertising incomplete output or notifying the creator of a terminal failure

#### Scenario: Processing fails terminally
- **WHEN** the required baseline exhausts bounded retries or encounters a registered non-retryable failure
- **THEN** the asset records terminal failure and commits one creator notification fact containing only a safe reason code

### Requirement: Baseline-Gated Public Availability
Videos backed by the production media pipeline SHALL enter public Feed, detail, recommendation, preload, search, public-profile, collection, and media reads only after the required baseline output is ready and the video lifecycle is review-approved and published. The transition that first completes all public gates SHALL create the stable first-publication notification unless the approving transaction already created it.

#### Scenario: New video is still processing
- **WHEN** an owner creates a pending-review video whose required baseline has not completed
- **THEN** the owner can observe processing and review state but public reads do not return the video

#### Scenario: Baseline becomes ready
- **WHEN** the processing worker verifies and publishes the required baseline for a review-approved, published, public video
- **THEN** the video becomes publicly eligible, additive renditions can appear later, and one first-publication notification fact is committed

#### Scenario: Baseline becomes ready before approval
- **WHEN** the processing worker verifies and publishes the required baseline while the video remains pending review
- **THEN** the video remains public-ineligible, additive renditions do not bypass review, and no publication notification is created

#### Scenario: Approval occurs before baseline is ready
- **WHEN** review publishes a video whose required baseline is still processing
- **THEN** public reads continue to omit it until the baseline becomes ready and the approval notification states that processing remains

#### Scenario: Both gates become ready in the approval transaction
- **WHEN** the required baseline is ready and the review transition makes the public video published
- **THEN** the video becomes publicly eligible and the approval transaction creates one combined approved-and-published notification

#### Scenario: Publication transition is replayed
- **WHEN** reconciliation or duplicate processing observes a video review version whose stable publication event already exists
- **THEN** public state remains correct and no duplicate publication message is created

#### Scenario: Legacy local video is read
- **WHEN** an existing readable local video has not yet been migrated
- **THEN** it remains playable through its compatibility `media_url` without synthesizing a historical publication message

### Requirement: Additive Playback Sources
Video and Feed responses SHALL preserve `media_url` and `cover_url` while optionally returning ordered typed playback sources and media processing state.

#### Scenario: New client receives processed video
- **WHEN** a processed video has multiple ready outputs
- **THEN** the response includes source type, URL, codec, dimensions, bitrate, and quality metadata in a deterministic order

#### Scenario: Existing client receives processed video
- **WHEN** a client only consumes `media_url` and `cover_url`
- **THEN** the compatibility fields resolve to a playable baseline and cover

### Requirement: Public CDN Cache Contract
Ready public variants and covers SHALL use versioned virtual exposure URLs with Range, HEAD, ETag,
and a bounded revalidating public cache. Public eligibility and exposure generation SHALL be stored
in PostgreSQL while the protected object-storage key remains unchanged; lifecycle transitions SHALL
NOT copy or move the media body.

#### Scenario: Browser requests a public byte range
- **WHEN** a browser requests a valid byte range for a currently eligible public exposure
- **THEN** the delivery path resolves the protected storage key, returns correct partial-content semantics and cache validators, and permits caching for no longer than 30 minutes with `must-revalidate`

#### Scenario: Public exposure is requested repeatedly
- **WHEN** the same currently eligible generation URL is requested within its bounded cache window
- **THEN** browser caching and bounded signed-URL reuse are permitted without another lifecycle database mutation or storage-body copy

#### Scenario: Video becomes public-ineligible
- **WHEN** a published video becomes private, offline, rejected, deleted, or media-failed
- **THEN** database eligibility immediately denies new signed URLs, existing redirects and signed URLs expire within 30 minutes, and the protected storage file remains unchanged

#### Scenario: Video becomes public again
- **WHEN** an eligible restored video is published again
- **THEN** Frux creates a new exposure generation pointing to the same protected file without changing original publication time or copying the body

#### Scenario: Cover becomes public
- **WHEN** a validated ready cover is exposed with its video
- **THEN** its virtual public URL points to the immutable uploaded cover key without creating an identical processed or public copy

#### Scenario: Legacy v2 exposure is migrated
- **WHEN** a legacy public variant has a verified protected counterpart
- **THEN** Frux stores the protected key and logical generation, serves a v3 URL, retains the old URL for the bounded cache window, and later schedules old public-object cleanup

#### Scenario: Legacy protected counterpart is missing
- **WHEN** migration cannot find the protected counterpart for a legacy public object
- **THEN** reconciliation repairs the protected copy before switching identity and keeps existing playback available until repair succeeds

### Requirement: Protected Media Delivery
Originals, private outputs, incomplete assets, and non-public creator previews SHALL remain owner-protected and SHALL NOT inherit public immutable caching. Owner asset access SHALL prefer a ready protected baseline or cover variant when available and SHALL otherwise fall back to the protected original.

#### Scenario: Non-owner requests a private output
- **WHEN** a user without current read permission requests a private or original asset
- **THEN** the asset is not disclosed

#### Scenario: Owner requests a protected output
- **WHEN** the immutable owner has current permission
- **THEN** Frux provides an authorized short-lived response without exposing reusable credentials or changing public eligibility

#### Scenario: Owner requests a ready video asset
- **WHEN** a protected browser baseline is ready for the owned asset
- **THEN** the access response resolves the baseline rather than the original source

#### Scenario: Owner requests a ready cover asset
- **WHEN** a protected ready cover variant exists
- **THEN** the access response resolves that cover variant

#### Scenario: Owner requests an incomplete asset
- **WHEN** no matching ready preview variant exists and the protected original remains available
- **THEN** Frux returns short-lived original access and the client handles possible browser incompatibility truthfully

#### Scenario: Protected preview response is cached
- **WHEN** a browser receives owner-protected preview access
- **THEN** the response and underlying object use private no-store behavior rather than public caching

### Requirement: Reviewer-Protected Media Delivery
Frux SHALL provide authorized review readers with short-lived access to the protected media and cover for the current review subject without changing public media eligibility or disclosing reusable storage credentials.

#### Scenario: Reviewer requests a pending subject preview
- **WHEN** an active principal with `review.read` requests preview access for a current non-deleted review subject
- **THEN** Frux returns typed media access that expires within five minutes and leaves the video's public media projection unchanged

#### Scenario: Public caller reuses the review preview
- **WHEN** a caller without current review authorization attempts to obtain protected preview access
- **THEN** Frux denies the request without revealing the protected object location

#### Scenario: Review subject version is stale
- **WHEN** the requested case no longer matches the video's current review version or reviewable lifecycle
- **THEN** Frux returns a stable conflict or unavailable response and does not issue preview access

#### Scenario: Preview access expires
- **WHEN** the signed review preview lifetime has elapsed
- **THEN** storage or local media delivery rejects the URL and a still-authorized reviewer must request fresh access

### Requirement: Media Reconciliation and Cleanup
Frux SHALL reconcile stuck processing, missing outputs, database/object mismatches, and deleted-media cleanup outside user request transactions.

#### Scenario: Processing lease expires
- **WHEN** a processing job remains leased beyond its timeout
- **THEN** reconciliation makes it retryable without creating duplicate published variants

#### Scenario: Video is deleted
- **WHEN** a video is deleted
- **THEN** public source discovery is removed immediately and physical objects are queued for delayed safe cleanup

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

### Requirement: Separated Runtime and Browser S3 Endpoints
Production API and Worker SHALL use the internal Compose MinIO endpoint while presigned browser
requests SHALL use the dedicated public S3 HTTPS origin containing the allocated public NAT port.

#### Scenario: Worker accesses object storage
- **WHEN** API or Worker performs an unsigned runtime S3 operation
- **THEN** traffic remains on the Compose backend network and does not traverse Caddy or the NAT gateway

#### Scenario: Browser receives a presigned request
- **WHEN** Frux creates a signed upload or download request
- **THEN** the URL contains the public S3 hostname and configured public HTTPS port and is reachable from the browser

### Requirement: No-Copy Cover Completion
A valid completed cover upload SHALL become the protected ready cover variant without downloading
and uploading an identical cover body.

#### Scenario: Cover upload completes
- **WHEN** object metadata matches the authenticated cover upload session
- **THEN** Frux records the uploaded key as the ready protected cover variant and performs no object-body GET during completion

#### Scenario: Cover and asset cleanup reference the same key
- **WHEN** deleting the video schedules cleanup for both the cover asset and cover variant
- **THEN** cleanup deduplication produces one safe physical deletion

### Requirement: Media Outbound Byte Observability
Frux SHALL expose low-cardinality byte counters for application-controlled object-storage reads
without video, asset, user, URL, object-key, or token labels.

#### Scenario: Worker downloads a source
- **WHEN** processing reads source bytes from object storage
- **THEN** exact transferred bytes are counted under the registered source-processing operation

#### Scenario: Legacy exposure repair reads a body
- **WHEN** reconciliation must repair a missing protected legacy object
- **THEN** exact transferred bytes are counted under the registered legacy-repair operation

#### Scenario: Public playback is redirected
- **WHEN** Frux issues a signed public media redirect
- **THEN** requested full or Range byte estimates may be counted separately while provider billing remains the authoritative outbound total
