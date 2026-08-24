## ADDED Requirements

### Requirement: Development overlay automatically embeds new public videos
When the billable development multimodal overlay is active, Frux SHALL enable the existing durable
video-job handoff and executor for newly published eligible videos. Successful jobs SHALL persist the
authoritative active-contract Fact and current Projection used by Session Semantic recommendation.

#### Scenario: New eligible video is published
- **WHEN** the publication consumer durably accepts a public media-ready video under the active profile
- **THEN** it creates or reuses the exact-contract job, the Worker calls the Adapter within existing bounds, and a valid result becomes a Fact and Projection

#### Scenario: New video is not yet vectorized
- **WHEN** the Adapter is temporarily unavailable after durable handoff
- **THEN** the job remains retryable and the video stays available through non-semantic Feed providers

#### Scenario: Same publication is replayed
- **WHEN** Kafka redelivers the same source event or Worker retries an already completed source hash
- **THEN** Frux reuses durable state and does not create another authoritative vector or unnecessary provider call

#### Scenario: Overlay is disabled
- **WHEN** development returns to base Compose
- **THEN** new videos remain normally publishable without multimodal vectors and existing Fact/Projection rows remain readable
