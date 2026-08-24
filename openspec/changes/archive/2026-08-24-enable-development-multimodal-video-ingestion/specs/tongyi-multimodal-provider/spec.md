## ADDED Requirements

### Requirement: Development Adapter service is health-gated
The development Compose Adapter service SHALL bind only to the private Compose network, complete the
existing real model probe before listening, expose a bounded liveness healthcheck, and gate Worker
startup until healthy. Restarting the Adapter MUST NOT require or expose its upstream key in API or
Worker.

#### Scenario: Startup probe succeeds
- **WHEN** the overlay supplies a supported profile, Frux HMAC, and valid DashScope key
- **THEN** the Adapter becomes healthy and Worker completes the signed video-capability handshake

#### Scenario: Startup probe fails
- **WHEN** credentials, model access, upstream response, or timeout is invalid
- **THEN** the Adapter remains unavailable and Worker does not begin video-job execution

#### Scenario: Container environments are inspected
- **WHEN** operators inspect API, Worker, and Adapter environment names
- **THEN** `DASHSCOPE_API_KEY` exists only in Adapter and is never printed by health, logs, metrics, or reports
