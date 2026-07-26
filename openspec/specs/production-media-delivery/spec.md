# production-media-delivery Specification

## Purpose

Defines production-ready media storage, processing, availability, playback delivery, access control, and lifecycle reconciliation.

## Requirements

### Requirement: Pluggable Media Storage
GCFeed SHALL store media through a common storage contract with a local development implementation and a production object-storage implementation.

#### Scenario: Local development upload completes
- **WHEN** local storage mode accepts a valid authenticated upload
- **THEN** the asset is stored locally with immutable owner metadata and remains usable by existing development flows

#### Scenario: Production direct upload completes
- **WHEN** an authenticated user completes a valid unexpired production upload session with the expected object key, size, and checksum
- **THEN** GCFeed records an immutable owned media asset and enqueues processing

#### Scenario: Upload session payload does not match
- **WHEN** the completed object does not match the session owner, kind, size, checksum, or key
- **THEN** completion is rejected and the object is not attached to a video

### Requirement: Versioned Media Processing
GCFeed SHALL process each source asset with an idempotent versioned media profile and SHALL persist the state and metadata of every generated output.

#### Scenario: Source requires normalization
- **WHEN** a valid uploaded source uses an accepted codec or layout that is not the required browser baseline
- **THEN** the worker generates a browser-compatible baseline MP4 before public eligibility

#### Scenario: Rendition ladder is generated
- **WHEN** the source resolution supports multiple configured renditions
- **THEN** the worker generates only non-upscaled bounded renditions and an adaptive manifest that references verified immutable outputs

#### Scenario: Processing job is delivered twice
- **WHEN** the same asset and processing-profile version are consumed more than once
- **THEN** output publication and database records remain idempotent

#### Scenario: Processing fails
- **WHEN** probing, transcoding, checksum validation, or object publication fails
- **THEN** the asset records a retryable or terminal failure without advertising incomplete outputs

### Requirement: Baseline-Gated Public Availability
Videos backed by the production media pipeline SHALL enter public Feed, detail, recommendation, preload, and public-profile reads only after the required baseline output is ready.

#### Scenario: New video is still processing
- **WHEN** an owner creates a video whose required baseline has not completed
- **THEN** the owner can observe processing state but public reads do not return the video

#### Scenario: Baseline becomes ready
- **WHEN** the processing worker verifies and publishes the required baseline
- **THEN** the video becomes eligible for its normal published visibility and additive renditions can appear later

#### Scenario: Legacy local video is read
- **WHEN** an existing readable local video has not yet been migrated
- **THEN** it remains playable through its compatibility `media_url`

### Requirement: Additive Playback Sources
Video and Feed responses SHALL preserve `media_url` and `cover_url` while optionally returning ordered typed playback sources and media processing state.

#### Scenario: New client receives processed video
- **WHEN** a processed video has multiple ready outputs
- **THEN** the response includes source type, URL, codec, dimensions, bitrate, and quality metadata in a deterministic order

#### Scenario: Existing client receives processed video
- **WHEN** a client only consumes `media_url` and `cover_url`
- **THEN** the compatibility fields resolve to a playable baseline and cover

### Requirement: Public CDN Cache Contract
Ready public variants and covers SHALL use immutable versioned delivery URLs with Range, HEAD, ETag, and long-lived public caching.

#### Scenario: Browser requests a public byte range
- **WHEN** a browser requests a valid byte range for a public immutable variant
- **THEN** the delivery path returns correct partial-content semantics and cache validators

#### Scenario: Public variant is requested repeatedly
- **WHEN** the same immutable public URL is requested before expiry
- **THEN** browser and CDN caching are permitted without requiring per-request application authorization

### Requirement: Protected Media Delivery
Originals, private outputs, and incomplete assets SHALL remain owner-protected and SHALL NOT inherit public immutable caching.

#### Scenario: Non-owner requests a private output
- **WHEN** a user without current read permission requests a private or original asset
- **THEN** the asset is not disclosed

#### Scenario: Owner requests a protected output
- **WHEN** the immutable owner has current permission
- **THEN** GCFeed provides an authorized response or short-lived signed URL without exposing reusable credentials

### Requirement: Media Reconciliation and Cleanup
GCFeed SHALL reconcile stuck processing, missing outputs, database/object mismatches, and deleted-media cleanup outside user request transactions.

#### Scenario: Processing lease expires
- **WHEN** a processing job remains leased beyond its timeout
- **THEN** reconciliation makes it retryable without creating duplicate published variants

#### Scenario: Video is deleted
- **WHEN** a video is deleted
- **THEN** public source discovery is removed immediately and physical objects are queued for delayed safe cleanup
