## Why

Privileged actions such as review decisions, forced takedowns, configuration changes, and dead-letter replay need durable attribution before those actions are implemented. Frux has no shared append-only audit fact that later admin modules can depend on.

## What Changes

- Add an append-only admin audit record containing actor, permission, action, target, request correlation, outcome, and bounded structured detail.
- Provide a narrow application interface that privileged use cases call in the same transaction when their durable state changes.
- Add cursor-paginated audit querying protected by the audit-read permission.
- Define redaction and payload limits so credentials, tokens, media contents, and unbounded request bodies are never stored.
- Exclude business event sourcing, user activity history, log aggregation, and mutable audit correction.

## Capabilities

### New Capabilities

- `admin-audit-trail`: Durable, queryable, privacy-bounded audit facts for privileged operations.

### Modified Capabilities

None.

## Impact

This adds a small admin audit domain, PostgreSQL model and repository, application port, HTTP query endpoint, migration registration, metrics, tests, and documentation. It depends on `add-admin-authorization-foundation`.
