## ADDED Requirements

### Requirement: First concrete multimodal development contract
Frux SHALL define its first concrete development contract as provider `alibaba-bailian`, model
`tongyi-embedding-vision-flash`, revision `2026-03-06-res1`, dimension 768, and the existing
`public-content-v1`, `representative-frames-v1`, `rgb-fit-v1`, and `provider-fusion-v1` policies.
Changing the upstream snapshot, dimension, resolution level, or any policy MUST create another
contract identity rather than overwrite vectors produced under this contract.

#### Scenario: Selected contract vector is persisted
- **WHEN** the Tongyi adapter returns a valid vector for the selected profile and current video source
- **THEN** Frux persists it only under the exact selected contract key

#### Scenario: Selected profile changes
- **WHEN** a later deployment changes model snapshot, dimension, resolution level, or processing policy
- **THEN** it uses a new revision or policy identity and does not compare the resulting vectors with `2026-03-06-res1`
