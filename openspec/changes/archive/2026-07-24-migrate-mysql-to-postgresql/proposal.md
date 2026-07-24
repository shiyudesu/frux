## Why

GCFeed is tightly coupled to MySQL across connection setup, GORM models, raw SQL, migration error handling, and deployment manifests. Replacing it with PostgreSQL now establishes a single durable-store baseline before more environments and database-dependent features are added.

## What Changes

- **BREAKING** Replace MySQL with PostgreSQL as the only supported durable database; existing development data is not migrated and local database volumes must be recreated.
- Replace the MySQL `database/sql` and GORM drivers with pgx and the GORM PostgreSQL driver while preserving the shared connection-pool architecture used by the API and worker.
- Make schema initialization safe when API and worker processes start concurrently, and replace MySQL-specific data types, index naming, duplicate-key handling, index discovery, and upsert expressions.
- Normalize account identifiers to lowercase at the domain/application boundary so registration and login remain case-insensitive under PostgreSQL.
- Treat non-account idempotency and event identifiers as trimmed, case-sensitive opaque strings.
- Replace MySQL services, configuration, health checks, volumes, and deployment resources with PostgreSQL equivalents.
- Add real-PostgreSQL verification for migration, repository conflict/upsert behavior, concurrent process startup, and the complete Compose stack.
- Update project, architecture, engineering, optimization, module, deployment, and onboarding documentation to describe PostgreSQL as the source of truth.

## Capabilities

### New Capabilities

- `postgresql-persistence`: Defines PostgreSQL connectivity, schema initialization, repository semantics, deployment integration, and real-database verification.
- `account-identity`: Defines normalized, case-insensitive account registration and lookup behavior.

### Modified Capabilities

None.

## Impact

- Backend dependencies and composition: `apps/api/go.mod`, database configuration/connection code, API router, and worker startup.
- Persistence: GORM models, migration orchestration, duplicate-key mapping, timeline index creation, and exposure/recommendation upserts.
- Runtime environments: Docker Compose, Kubernetes manifests, local configuration, ports, credentials, health checks, and persistent volumes.
- Tests: new PostgreSQL-backed migration/repository coverage plus full-stack startup and API-flow verification.
- Documentation and OpenSpec context: `README.md`, `openspec/project.md`, engineering/architecture/optimization/quick-read documents, and database-referencing module or operational documentation.
- Public HTTP request and response shapes remain unchanged; account matching becomes explicitly lowercase-normalized.
