## ADDED Requirements

### Requirement: Production Adapter is private and health-gated
The production Adapter SHALL reuse the digest-pinned API image, run only on the Compose backend
network, publish no host port, complete its real startup probe before health, and restart under the
same release lifecycle as API/Worker.

#### Scenario: Production profile starts successfully
- **WHEN** valid Profile, HMAC, and DashScope credentials are configured
- **THEN** Adapter becomes healthy before Worker is considered ready and exposes no public listener

#### Scenario: Adapter startup fails
- **WHEN** its model probe or configuration fails
- **THEN** deployment health fails and the release is rolled back without Worker claiming new video jobs
