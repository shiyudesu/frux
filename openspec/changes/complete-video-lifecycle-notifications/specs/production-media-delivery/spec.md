## MODIFIED Requirements

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
