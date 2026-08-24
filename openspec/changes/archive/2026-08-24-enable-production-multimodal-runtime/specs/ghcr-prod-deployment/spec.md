## ADDED Requirements

### Requirement: Optional multimodal production profile is release-gated
The immutable release deployer SHALL validate the multimodal environment as one closed feature set,
activate the Compose profile only when explicitly enabled, pull the digest-pinned Adapter image, and
include Adapter health in deployment success and rollback.

#### Scenario: Complete multimodal production configuration is supplied
- **WHEN** deployment, runtime, video-job, Session, private-network, full-rollout, Profile, HMAC, and API-key values are valid
- **THEN** the deployer starts Adapter/API/Worker and accepts the release only after Adapter and Worker are healthy

#### Scenario: Multimodal configuration is partial
- **WHEN** the profile gate is true but any required value is missing or inconsistent
- **THEN** deployment stops before Compose mutation

#### Scenario: Adapter is unhealthy after release start
- **WHEN** the startup probe or healthcheck does not succeed within the bounded window
- **THEN** the release is rejected and the prior release is restored with its own configured profile state
