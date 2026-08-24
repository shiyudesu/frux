## ADDED Requirements

### Requirement: Public access mode does not expose the model Adapter
Frux SHALL keep public Web/S3 access configuration independent from the private multimodal Adapter.
Whether public access uses Caddy HTTPS or explicit direct-IP HTTP, Adapter and Worker metrics MUST
remain unbound from public host interfaces.

#### Scenario: Direct-IP HTTP mode uses multimodal inference
- **WHEN** an operator explicitly enables both direct-IP public mode and the private multimodal profile
- **THEN** only Web and the S3 API use public ports while Adapter remains reachable solely on the backend network

#### Scenario: Public protocol changes
- **WHEN** the site later moves between direct HTTP and domain HTTPS
- **THEN** Worker continues to use the same private signed Adapter endpoint without changing public routes
