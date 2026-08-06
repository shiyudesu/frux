## MODIFIED Requirements

### Requirement: Baseline-Gated Public Availability
Videos backed by the production media pipeline SHALL enter public Feed, detail, recommendation, preload, search, public-profile, collection, and media reads only after the required baseline output is ready and the video lifecycle is review-approved and published.

#### Scenario: New video is still processing
- **WHEN** an owner creates a pending-review video whose required baseline has not completed
- **THEN** the owner can observe processing and review state but public reads do not return the video

#### Scenario: Baseline becomes ready before approval
- **WHEN** the processing worker verifies and publishes the required baseline while the video remains pending review
- **THEN** the video remains public-ineligible and additive renditions do not bypass review

#### Scenario: Approval occurs before baseline is ready
- **WHEN** review publishes a video whose required baseline is still processing
- **THEN** public reads continue to omit it until the baseline becomes ready

#### Scenario: Both gates become ready
- **WHEN** the required baseline is ready and the video is review-approved, published, and public
- **THEN** the video becomes publicly eligible and additive renditions can appear later

#### Scenario: Legacy local video is read
- **WHEN** an existing readable local video has not yet been migrated
- **THEN** it remains playable through its compatibility `media_url`

### Requirement: Public CDN Cache Contract
Ready public variants and covers SHALL use versioned exposure URLs with Range, HEAD, ETag, and a bounded revalidating public cache so lifecycle revocation can take effect.

#### Scenario: Browser requests a public byte range
- **WHEN** a browser requests a valid byte range for a currently eligible public variant
- **THEN** the delivery path returns correct partial-content semantics, cache validators, and a public cache lifetime no longer than 60 seconds with revalidation required

#### Scenario: Video becomes public-ineligible
- **WHEN** a published video becomes private, offline, rejected, deleted, or media-failed
- **THEN** its promoted variants are moved back to the protected prefix, public delivery stops after the bounded cache window, and failed object cleanup remains durably retryable

#### Scenario: Video becomes public again
- **WHEN** an eligible restored video is published again
- **THEN** Frux promotes the protected bundle under a new exposure generation without changing its original publication time

#### Scenario: Legacy immutable cache is migrated
- **WHEN** the bounded-revocation delivery policy is first deployed
- **THEN** operators purge legacy `media/*` entries that were previously advertised with year-long immutable caching
