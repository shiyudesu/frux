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

### Requirement: Versioned Media Processing
Frux SHALL process each source asset with an idempotent versioned media profile, SHALL persist the state and metadata of every generated output, and SHALL create a durable creator notification only when the required current baseline reaches a terminal failure.

#### Scenario: Source requires normalization
- **WHEN** a valid uploaded source uses an accepted codec or layout that is not the required browser baseline
- **THEN** the worker generates a browser-compatible baseline MP4 before public eligibility

#### Scenario: Rendition ladder is generated
- **WHEN** the source resolution supports multiple configured renditions
- **THEN** the worker generates only non-upscaled bounded renditions and an adaptive manifest that references verified immutable outputs

#### Scenario: Processing job is delivered twice
- **WHEN** the same asset and processing-profile version are consumed more than once
- **THEN** output publication, database records, and terminal-failure notifications remain idempotent

#### Scenario: Processing fails retryably
- **WHEN** probing, transcoding, checksum validation, or object publication fails with attempts remaining
- **THEN** the asset records a retryable failure without advertising incomplete outputs or notifying the creator of a terminal failure

#### Scenario: Processing fails terminally
- **WHEN** the required current baseline exhausts bounded retries or encounters a registered non-retryable failure
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
Ready public variants and covers SHALL use versioned exposure URLs with Range, HEAD, ETag, and a bounded revalidating public cache so lifecycle revocation can take effect.

#### Scenario: Browser requests a public byte range
- **WHEN** a browser requests a valid byte range for a currently eligible public variant
- **THEN** the delivery path returns correct partial-content semantics, cache validators, and a public cache lifetime no longer than 60 seconds with `must-revalidate`

#### Scenario: Public variant is requested repeatedly
- **WHEN** the same currently eligible versioned exposure URL is requested within its bounded cache window
- **THEN** browser and CDN caching are permitted without per-request application authorization, but revalidation is required no later than 60 seconds

#### Scenario: Video becomes public-ineligible
- **WHEN** a published video becomes private, offline, rejected, deleted, or media-failed
- **THEN** its promoted variants are moved back to the protected prefix, public delivery stops after the bounded cache window, and failed object cleanup remains durably retryable

#### Scenario: Video becomes public again
- **WHEN** an eligible restored video is published again
- **THEN** Frux promotes the protected bundle under a new exposure generation without changing its original publication time

#### Scenario: Legacy immutable cache is migrated
- **WHEN** the bounded-revocation delivery policy is first deployed
- **THEN** operators purge legacy `media/*` entries that were previously advertised with year-long immutable caching

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
