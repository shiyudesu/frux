## ADDED Requirements

### Requirement: Session-only runtime overrides are explicit and default-preserving
Frux SHALL allow the parent multimodal runtime and Session Semantic recommendation flags to be
overridden by registered environment variables. Missing or blank overrides MUST preserve YAML values,
invalid non-blank values MUST fail startup, and checked-in/default Compose behavior SHALL remain off.

#### Scenario: No override is supplied
- **WHEN** API and Worker load the checked-in configuration without non-blank runtime overrides
- **THEN** multimodal and Session Semantic runtime remain disabled exactly as declared in YAML

#### Scenario: Valid Session-only overrides are supplied
- **WHEN** the parent and session variables are true with a compatible registered profile
- **THEN** API composes Session Semantic Builder/Exact runtime without requiring video jobs, query embedding, Hybrid Search, Adapter endpoint, HMAC, or API key

#### Scenario: Invalid override is supplied
- **WHEN** a registered boolean override contains an unsupported non-blank value
- **THEN** configuration loading fails before API or Worker startup

#### Scenario: Compose session runtime file is used
- **WHEN** the dedicated session-runtime env example is passed to Compose
- **THEN** only profile and the two runtime booleans are injected and native loopback Adapter settings are not required
