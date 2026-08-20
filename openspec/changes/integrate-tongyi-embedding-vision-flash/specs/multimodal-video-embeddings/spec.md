## ADDED Requirements

### Requirement: Supported concrete multimodal development contracts
Frux SHALL resolve each supported Tongyi model profile to a distinct concrete contract. The dated
snapshot SHALL use revision `2026-03-06-res1` and `provider-fusion-v1`; the undated model SHALL use
revision `stable-independent-mean-v1` and `normalized-mean-fusion-v1`. Both SHALL use provider
`alibaba-bailian`, model `tongyi-embedding-vision-flash`, dimension 768, `public-content-v1`,
`representative-frames-v1`, and `rgb-fit-v1`. Changing model behavior, dimension, resolution level,
fusion algorithm, or any other policy MUST create another contract identity.

#### Scenario: Selected contract vector is persisted
- **WHEN** the Tongyi adapter returns a valid vector for either selected profile and current video source
- **THEN** Frux persists it only under the exact selected contract key

#### Scenario: Selected profile changes
- **WHEN** a later deployment changes model profile, dimension, resolution level, or processing policy
- **THEN** it uses a new revision or policy identity and does not compare the resulting vectors with `2026-03-06-res1`

#### Scenario: Deployment switches between supported profiles
- **WHEN** configuration changes between the dated and undated Tongyi Flash profiles
- **THEN** the active contract changes and the projection/retrieval path excludes vectors stored under the other profile
