# postgresql-persistence Specification

## Purpose
Define PostgreSQL connectivity, schema initialization, persistence semantics, runtime provisioning, and verification requirements for Frux.

## Requirements

### Requirement: PostgreSQL Database Connectivity
The API and worker SHALL use PostgreSQL as the only durable database through a shared `database/sql` connection pool and the GORM PostgreSQL dialect. Database configuration SHALL include host, port, user, password, database name, SSL mode, and time zone settings.

#### Scenario: API starts with valid PostgreSQL configuration
- **WHEN** the feed API starts with a reachable PostgreSQL database and valid credentials
- **THEN** it initializes the shared connection pool, registers the GORM repositories, and serves requests without requiring a MySQL driver

#### Scenario: Worker starts with valid PostgreSQL configuration
- **WHEN** the worker starts with a reachable PostgreSQL database and valid credentials
- **THEN** it initializes the shared connection pool and starts its persistence-backed consumers without requiring a MySQL driver

#### Scenario: Database connection is invalid
- **WHEN** either process starts with unreachable PostgreSQL settings or rejected credentials
- **THEN** startup fails with an explicit database initialization error

### Requirement: Concurrent Schema Initialization
Schema initialization SHALL be safe when multiple API or worker processes start against the same PostgreSQL database. The migration flow SHALL serialize schema changes and SHALL create all required tables, constraints, and indexes without duplicate-object failures.

#### Scenario: API and worker start against an empty database
- **WHEN** the API and worker run schema initialization concurrently against an empty PostgreSQL database
- **THEN** one initializer performs the migration while the others wait, and all processes continue after the complete schema is available

#### Scenario: Processes restart against an initialized database
- **WHEN** schema initialization runs against a database that already contains the current schema
- **THEN** it completes without destructive changes or duplicate table, column, constraint, or index errors

#### Scenario: Post-migration setup is required
- **WHEN** schema initialization completes
- **THEN** every video has a `video_stat` row and the stable Timeline query index exists before the process begins serving work

### Requirement: PostgreSQL-Compatible Persistence Semantics
Repository operations SHALL preserve Frux transaction, idempotency, conflict, row-locking, and upsert behavior using PostgreSQL-compatible SQL and GORM clauses.

#### Scenario: Unique account or idempotency constraint is violated
- **WHEN** PostgreSQL reports a unique-constraint violation for an account, event identifier, or idempotency key
- **THEN** the repository maps it to the same domain conflict or existing-result behavior exposed by the current API

#### Scenario: Exposure aggregation is upserted
- **WHEN** an exposure already exists for the same user and video
- **THEN** the existing row atomically increments `exposure_count` and adopts the incoming last-exposure time, scene, and update time

#### Scenario: Interaction counters are updated concurrently
- **WHEN** concurrent interaction operations update the same video statistics
- **THEN** PostgreSQL row locking preserves committed increments or decrements and persisted counters do not become negative

#### Scenario: Opaque identifiers differ only by case
- **WHEN** two non-account idempotency or event identifiers differ by letter case
- **THEN** PostgreSQL persistence treats them as distinct trimmed values

### Requirement: PostgreSQL-Compatible Schema
The generated schema SHALL use PostgreSQL-supported data types and schema-wide unique object names while preserving current field ranges, nullability, uniqueness, and stable cursor ordering.

#### Scenario: Schema is created from GORM models
- **WHEN** GORM creates the schema in an empty PostgreSQL database
- **THEN** status fields use a supported small integer type, embedding JSON uses a PostgreSQL JSON type, and every explicitly named index is unique within the schema

#### Scenario: Timeline page is queried
- **WHEN** the repository reads published videos ordered by `published_at DESC, id DESC`
- **THEN** PostgreSQL can use an index beginning with `status`, `published_at`, and `id` while preserving stable cursor pagination

#### Scenario: Timestamps cross process boundaries
- **WHEN** API and worker processes write or read publication, creation, update, or event times
- **THEN** the database connection and application represent those timestamps consistently in UTC

### Requirement: PostgreSQL Runtime Environments
The project SHALL provision PostgreSQL in local Compose and Kubernetes deployment manifests with persistent storage, health checks, explicit credentials, and dependency ordering for the API and worker.

#### Scenario: Complete Compose stack starts
- **WHEN** a developer recreates volumes and runs the documented Compose startup command
- **THEN** PostgreSQL becomes healthy before the API and worker start and the complete Frux stack reaches its healthy state

#### Scenario: Kubernetes manifests are applied
- **WHEN** the deployment manifests are applied to the `frux` namespace
- **THEN** PostgreSQL is exposed to the API and worker on port 5432 with persistent storage and readiness/liveness checks

### Requirement: Real PostgreSQL Verification
Database-specific behavior SHALL be verified against a real PostgreSQL instance rather than only through in-memory repositories.

#### Scenario: Persistence integration tests run
- **WHEN** the PostgreSQL integration test configuration is provided
- **THEN** tests cover clean migration, concurrent initialization, duplicate-key mapping, exposure upsert behavior, and required indexes

#### Scenario: Full-stack API flow runs
- **WHEN** the Compose stack is started from recreated volumes
- **THEN** account, video, Feed, interaction, exposure, message, and worker-backed flows persist through PostgreSQL without MySQL runtime dependencies
