## MODIFIED Requirements

### Requirement: Health-Gated Bundle Rollback
The deployment agent SHALL switch `current` only after API, Web, Worker, PostgreSQL backup, and their
required Compose dependencies are ready, and SHALL restore the previous bundle if deployment fails.
Host-local route validation SHALL use Caddy on local 443 in Caddy mode and the Web-published host
port through loopback in direct-IP mode.

#### Scenario: New API is unhealthy
- **WHEN** API or Web fails to become healthy within the timeout
- **THEN** the previous Compose/configuration and digest-pinned images are recreated without deleting volumes

#### Scenario: Public NAT port differs from local Caddy port
- **WHEN** the public HTTPS high port forwards to host-local 443
- **THEN** deployment route checks resolve the configured hostname to `127.0.0.1:443` and do not require the public port to be reachable from the host

#### Scenario: Direct-IP mode is selected
- **WHEN** Web publishes the configured direct application port on the host
- **THEN** deployment route checks request the application, health, and protected-media paths through loopback on that host port without requiring hairpin public routing

#### Scenario: Required storage dependency fails
- **WHEN** MinIO or its initialization prevents API or Worker readiness
- **THEN** the release is rejected before `current` advances

#### Scenario: MinIO becomes unhealthy independently
- **WHEN** the MinIO container health check fails while API process health remains successful
- **THEN** the deployment agent still rejects the release through its explicit MinIO health gate
