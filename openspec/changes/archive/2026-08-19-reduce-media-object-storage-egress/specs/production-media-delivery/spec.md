## MODIFIED Requirements

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
- **THEN** Worker uploads the local output once, verifies size and checksum, and commits the variant
  without uploading or downloading an object-store temporary copy

#### Scenario: Matching final output already exists
- **WHEN** a retry finds the deterministic final key with the expected size and checksum
- **THEN** Worker reuses it idempotently without transferring the body again

#### Scenario: Final output metadata conflicts
- **WHEN** the deterministic final key exists with different size or checksum
- **THEN** processing fails explicitly and does not overwrite or advertise the conflicting file

#### Scenario: Worker exits after final PUT
- **WHEN** the final file commits but PostgreSQL finalization does not
- **THEN** the unreferenced deterministic file remains protected and delayed orphan reconciliation
  removes it only after the configured safety window

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

### Requirement: Public CDN Cache Contract
Ready public variants and covers SHALL use versioned virtual exposure URLs with Range, HEAD, ETag,
and a bounded revalidating public cache. Public eligibility and exposure generation SHALL be stored
in PostgreSQL while the protected object-storage key remains unchanged; lifecycle transitions SHALL
NOT copy or move the media body.

#### Scenario: Browser requests a public byte range
- **WHEN** a browser requests a valid byte range for a currently eligible public exposure
- **THEN** the delivery path resolves the protected storage key, returns correct partial-content
  semantics and cache validators, and permits caching for no longer than 30 minutes with
  `must-revalidate`

#### Scenario: Public exposure is requested repeatedly
- **WHEN** the same currently eligible generation URL is requested within its bounded cache window
- **THEN** browser caching and bounded signed-URL reuse are permitted without another lifecycle
  database mutation or storage-body copy

#### Scenario: Video becomes public-ineligible
- **WHEN** a published video becomes private, offline, rejected, deleted, or media-failed
- **THEN** database eligibility immediately denies new signed URLs, existing redirects and signed
  URLs expire within 30 minutes, and the protected storage file remains unchanged

#### Scenario: Video becomes public again
- **WHEN** an eligible restored video is published again
- **THEN** Frux creates a new exposure generation pointing to the same protected file without
  changing original publication time or copying the body

#### Scenario: Cover becomes public
- **WHEN** a validated ready cover is exposed with its video
- **THEN** its virtual public URL points to the immutable uploaded cover key without creating an
  identical processed or public copy

#### Scenario: Legacy v2 exposure is migrated
- **WHEN** a legacy public variant has a verified protected counterpart
- **THEN** Frux stores the protected key and logical generation, serves a v3 URL, retains the old URL
  for the bounded cache window, and later schedules old public-object cleanup

#### Scenario: Legacy protected counterpart is missing
- **WHEN** migration cannot find the protected counterpart for a legacy public object
- **THEN** reconciliation repairs the protected copy before switching identity and keeps existing
  playback available until repair succeeds

### Requirement: Private-Bucket Public Media Redirect
The Rainyun bucket SHALL remain private, and Frux SHALL serve stable virtual public-media URLs that
authorize each currently eligible exposure while resolving a separate protected storage key.
Frux SHALL redirect media-byte GET requests to a Rainyun presigned GET lasting no more than 30
minutes and SHALL permit browser caching only within the same revocation bound.

#### Scenario: Public v3 media is requested
- **WHEN** a browser requests a currently eligible v3 exposure through the Frux public media route
- **THEN** Frux resolves the protected key, returns a cacheable redirect lasting less than the signed
  URL lifetime, and the signed Rainyun response permits at most 30 minutes of revalidating cache

#### Scenario: Redirect is requested repeatedly
- **WHEN** the same exposure is requested repeatedly during the safe redirect-cache window
- **THEN** Frux may reuse the same signed Rainyun URL so the browser can reuse its cached redirect
  and media ranges

#### Scenario: Public object metadata is requested
- **WHEN** a browser sends HEAD for an eligible virtual exposure
- **THEN** Frux resolves and returns content type, content length, ETag, Range support, and bounded
  cache metadata without disclosing the protected storage key

#### Scenario: Protected object is requested anonymously
- **WHEN** an unauthenticated caller requests an original upload, protected key, moderation sample,
  unknown exposure, or public-ineligible media
- **THEN** Frux denies the request and issues no signed URL

#### Scenario: Video becomes public-ineligible
- **WHEN** a previously public generation URL is requested after the video becomes private, offline,
  rejected, deleted, or failed
- **THEN** Frux denies new redirects while previously cached redirects and signed URLs expire within
  the configured 30-minute maximum

#### Scenario: Owner or reviewer requests protected media
- **WHEN** current owner or reviewer authorization permits protected access
- **THEN** Frux continues to issue separate short-lived `private, no-store` access that never inherits
  public cache behavior

## ADDED Requirements

### Requirement: No-Copy Cover Completion
A valid completed cover upload SHALL become the protected ready cover variant without downloading
and uploading an identical cover body.

#### Scenario: Cover upload completes
- **WHEN** object metadata matches the authenticated cover upload session
- **THEN** Frux records the uploaded key as the ready protected cover variant and performs no
  object-body GET during completion

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
- **THEN** requested full or Range byte estimates may be counted separately while provider billing
  remains the authoritative outbound total
