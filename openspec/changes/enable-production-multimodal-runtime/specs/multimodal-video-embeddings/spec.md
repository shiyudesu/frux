## ADDED Requirements

### Requirement: Production opt-in embeds newly published videos
When production multimodal deployment and video jobs are explicitly enabled, newly published eligible
videos SHALL use the existing durable Job → Fact → Projection path. Disabling the profile MUST leave
publication and existing vector reads available.

#### Scenario: Production video is newly published
- **WHEN** Worker has a healthy compatible private Adapter and accepts the publication handoff
- **THEN** the bounded job produces an active-contract Fact and current Projection

#### Scenario: Production Adapter is unavailable
- **WHEN** a durable job cannot reach the Adapter
- **THEN** it remains retryable and the video continues through non-semantic product paths

#### Scenario: Production profile is disabled
- **WHEN** operators turn off the explicit multimodal deployment and video-job flags
- **THEN** no new model calls occur and existing vectors remain stored and readable
