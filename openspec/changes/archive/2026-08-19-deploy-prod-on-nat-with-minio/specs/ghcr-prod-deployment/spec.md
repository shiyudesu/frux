## MODIFIED Requirements

### Requirement: Controlled Compose Update
The deployment agent SHALL preserve persistent volumes and SHALL pull, start, dependency-gate,
health-check, and restore API, Web, Worker, MinIO, and MinIO initialization as one required Prod
release unit.

#### Scenario: Worker container does not exist before deployment
- **WHEN** an approved bundle is deployed to a server without an existing Worker container
- **THEN** deployment creates Worker with the approved API image digest

#### Scenario: Worker is already running
- **WHEN** a new bundle is deployed while Worker exists
- **THEN** Worker is recreated with the approved API image digest

#### Scenario: Worker is unhealthy
- **WHEN** Worker fails its container health check or required Kafka workflow readiness
- **THEN** the new bundle is not promoted and the deployment agent restores the previous release

#### Scenario: MinIO is unavailable
- **WHEN** MinIO fails its health check or the initializer cannot create the private bucket and application identity
- **THEN** API and Worker do not become ready and the deployment is rolled back

#### Scenario: Release is rolled back
- **WHEN** the deployment agent restores the previous digest-pinned release
- **THEN** PostgreSQL, Redis, Kafka, MinIO, uploads, and backup volumes are preserved

### Requirement: Health-Gated Bundle Rollback
The deployment agent SHALL switch `current` only after API, Web, Worker, PostgreSQL backup, and their
required Compose dependencies are ready, and SHALL restore the previous bundle if deployment fails.
Host-local route validation SHALL continue through Caddy on local 443 even when the public NAT
origin uses a high port.

#### Scenario: New API is unhealthy
- **WHEN** API or Web fails to become healthy within the timeout
- **THEN** the previous Compose/configuration and digest-pinned images are recreated without deleting volumes

#### Scenario: Public NAT port differs from local Caddy port
- **WHEN** the public HTTPS high port forwards to host-local 443
- **THEN** deployment route checks resolve the configured hostname to `127.0.0.1:443` and do not require the public port to be reachable from the host

#### Scenario: Required storage dependency fails
- **WHEN** MinIO or its initialization prevents API or Worker readiness
- **THEN** the release is rejected before `current` advances

#### Scenario: MinIO becomes unhealthy independently
- **WHEN** the MinIO container health check fails while API process health remains successful
- **THEN** the deployment agent still rejects the release through its explicit MinIO health gate
