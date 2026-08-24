## MODIFIED Requirements

### Requirement: Session-only runtime overrides are explicit and default-preserving
Frux SHALL allow the parent multimodal runtime, Session Semantic recommendation, and development full
rollout flags to be overridden by registered environment variables. Development Compose SHALL select
a registered profile and enable the Session-only runtime by default. Production SHALL remain off
unless explicitly configured, and MUST reject the development full-rollout flag.

#### Scenario: Development Compose starts without multimodal overrides
- **WHEN** API and Worker load the checked-in development Compose configuration
- **THEN** the parent and Session Semantic runtimes are enabled with the registered default profile and no Adapter endpoint, HMAC, or API key is required

#### Scenario: Development operator explicitly disables the runtime
- **WHEN** either registered runtime boolean is explicitly set to false
- **THEN** the override is honored and configuration dependency validation remains fail-closed

#### Scenario: Invalid override is supplied
- **WHEN** a registered boolean override contains an unsupported non-blank value
- **THEN** configuration loading fails before API or Worker startup

#### Scenario: Production configuration is loaded
- **WHEN** staging or production configuration omits explicit runtime enablement
- **THEN** Session Semantic remains disabled and development full-rollout reconciliation cannot run

#### Scenario: Production requests development full rollout
- **WHEN** the development full-rollout flag is true in staging or production
- **THEN** configuration loading fails before service startup
