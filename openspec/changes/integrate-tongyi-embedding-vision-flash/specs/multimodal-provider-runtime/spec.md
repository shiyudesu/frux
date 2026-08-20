## ADDED Requirements

### Requirement: Concrete adapter proves upstream model access
A concrete external-model adapter SHALL verify access to its configured immutable model and a valid
output vector before reporting the existing provider protocol as ready. The Frux API and Worker SHALL
continue to depend only on the signed provider protocol and SHALL NOT receive the upstream API key.

#### Scenario: Concrete adapter starts successfully
- **WHEN** its bounded upstream probe authenticates and returns a vector compatible with the reported contract
- **THEN** the adapter may serve signed readiness and embedding operations to Frux processes

#### Scenario: Upstream credentials are invalid
- **WHEN** the adapter cannot authenticate its startup probe
- **THEN** it fails startup and Frux processes cannot mistake configuration-only readiness for model availability

#### Scenario: Shared model profile resolves a different contract
- **WHEN** the selected allowlisted profile changes in both adapter and Frux runtime configuration
- **THEN** readiness is checked against the newly resolved exact contract and vectors from the previous contract remain isolated
