## ADDED Requirements

### Requirement: Registered Degradation Controls
Frux SHALL support only code-registered degradation keys with documented owner, default value, allowed processes, and failure default.

#### Scenario: Operator creates an unknown key
- **WHEN** an authorized operator submits a control key that is not registered
- **THEN** Frux rejects the request without persisting a generic configuration value

#### Scenario: Process reads an unsupported key
- **WHEN** a process attempts to evaluate a key not allowed for that process
- **THEN** evaluation returns the registered failure default and records a bounded error

### Requirement: Immutable Control Revisions
Every control mutation SHALL create an immutable revision with enabled state, reason, optional expiry, actor, and timestamp, and activation SHALL use optimistic concurrency.

#### Scenario: Operator updates current revision
- **WHEN** an authorized operator supplies the active expected revision
- **THEN** Frux creates and activates a new revision and preserves all earlier revisions

#### Scenario: Two operators update concurrently
- **WHEN** the second update supplies an expected revision that is no longer active
- **THEN** Frux returns HTTP 409 and does not overwrite the newer control

#### Scenario: Operator rolls back
- **WHEN** an authorized operator selects an earlier valid revision
- **THEN** Frux activates that historical value through a new audited revision or pointer change without editing history

### Requirement: Local Runtime Evaluation
API and worker hot paths SHALL evaluate degradation controls from an atomically swapped local snapshot without a synchronous database, Redis, or HTTP control-plane call.

#### Scenario: Fresh revision is observed
- **WHEN** snapshot polling reads and validates a newer active revision
- **THEN** the process atomically applies it and exposes the applied revision metric

#### Scenario: Polling temporarily fails
- **WHEN** the control store is unavailable before maximum stale age
- **THEN** the process continues using its last-known-good snapshot and records poll failure and snapshot age

### Requirement: Expiry and Stale Safety
Expired controls and snapshots older than each key's maximum stale age SHALL resolve to registered defaults rather than remaining enabled indefinitely.

#### Scenario: Temporary incident switch expires
- **WHEN** server time passes the active revision expiry
- **THEN** local evaluation returns the registered normal default after the next evaluation or refresh

#### Scenario: Last-known-good becomes too old
- **WHEN** polling remains unavailable beyond the key's maximum stale age
- **THEN** evaluation returns the registered failure default and emits a stale-control metric

### Requirement: Authorized Audited Control Mutation
Control writes and rollbacks SHALL require `governance.execute`, a reason, and a success audit fact committed with the active-state change.

#### Scenario: Audit write fails
- **WHEN** the control revision is valid but its audit fact cannot commit
- **THEN** the new revision does not become active
