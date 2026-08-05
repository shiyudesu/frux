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
