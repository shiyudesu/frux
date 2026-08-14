## MODIFIED Requirements

### Requirement: Controlled Compose Update
The deployment agent SHALL preserve persistent volumes and SHALL pull, start, health-check, and
restore API, Web, and Worker as one required Prod release unit.

#### Scenario: Worker container does not exist before deployment
- **WHEN** an approved bundle is deployed to a server without an existing Worker container
- **THEN** deployment creates Worker with the approved API image digest

#### Scenario: Worker is already running
- **WHEN** a new approved bundle is deployed while Worker exists
- **THEN** Worker is recreated with the approved API image digest

#### Scenario: Worker is unhealthy
- **WHEN** Worker fails its container health check or required Kafka workflow readiness
- **THEN** the new bundle is not promoted and the deployment agent restores the previous release
