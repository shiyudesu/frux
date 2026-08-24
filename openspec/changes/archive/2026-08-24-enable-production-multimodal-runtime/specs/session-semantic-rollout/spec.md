## ADDED Requirements

### Requirement: Production full rollout is explicit and environment-scoped
Frux SHALL support a production full-rollout flag that is valid only in staging/production, requires
the parent and Session runtimes, and is mutually exclusive with the development full-rollout flag.
When enabled, API startup SHALL idempotently reconcile the same immutable compatible v4=100% policy.

#### Scenario: Production full rollout is enabled
- **WHEN** Session runtime composes under production with the explicit production full-rollout flag
- **THEN** API ensures v4 exists and is enabled, exact-disables other semantic targets, and retains v1/v2

#### Scenario: Production flag is used locally
- **WHEN** local/test configuration enables the production full-rollout flag
- **THEN** configuration fails before startup

#### Scenario: Development and production flags are both set
- **WHEN** both environment-scoped full-rollout flags are true
- **THEN** configuration fails without policy mutation
