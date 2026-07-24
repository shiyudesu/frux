## 1. PostgreSQL Connection Baseline

- [x] 1.1 Replace the MySQL Go drivers with pgx and `gorm.io/driver/postgres`, then tidy the backend module dependencies.
- [x] 1.2 Extend `DatabaseConfig` and both YAML configurations with PostgreSQL SSL mode and UTC time-zone settings, using PostgreSQL host, port, database, and application-user defaults.
- [x] 1.3 Replace the MySQL DSN and `sql.Open` logic with a pgx `database/sql` pool while preserving ping validation, pool limits, ownership, and shutdown behavior.
- [x] 1.4 Switch API router and worker GORM initialization to the PostgreSQL dialect and enable GORM translated errors.

## 2. Canonical Account Identity

- [x] 2.1 Add one shared account normalization function that trims and lowercases account identifiers without changing nickname or password values.
- [x] 2.2 Apply account normalization consistently during user creation, restoration, and repository account lookup.
- [x] 2.3 Add domain and API-flow coverage for mixed-case registration, case-variant login, canonical returned accounts, and duplicate case variants.

## 3. PostgreSQL Schema and Migration Safety

- [x] 3.1 Replace MySQL-only `tinyint` tags with PostgreSQL-compatible status types and store embedding payloads as `jsonb`.
- [x] 3.2 Rename every explicit GORM index with a table-prefixed schema-unique name, including the current cross-table collisions.
- [x] 3.3 Express the Timeline `(status, published_at, id)` index through model metadata or GORM migrator APIs and remove MySQL information-schema discovery.
- [x] 3.4 Replace MySQL migration error-number retries with one PostgreSQL advisory transaction lock covering `AutoMigrate`, video-stat backfill, and Timeline index setup.
- [x] 3.5 Add migration tests that assert clean initialization, idempotent restart, concurrent initialization, required tables/indexes, supported column types, and complete `video_stat` rows.

## 4. Repository SQL and Conflict Semantics

- [x] 4.1 Replace MySQL error structs and duplicate-message substring checks with shared `gorm.ErrDuplicatedKey` handling in account, video, interaction, exposure, message, and playback repositories.
- [x] 4.2 Rewrite exposure and recommendation conflict updates without MySQL `VALUES(column)` expressions while preserving atomic count increments and incoming last-exposure fields.
- [x] 4.3 Verify embedding upserts, relation conflict-ignore inserts, `FOR UPDATE` locking, nullable idempotency keys, generated IDs, and stable cursor queries under PostgreSQL.
- [x] 4.4 Add real-PostgreSQL repository tests for account conflicts, exact-case opaque keys, exposure aggregation, representative idempotent writes, and concurrent counter updates.

## 5. Runtime and Deployment

- [x] 5.1 Replace the Compose MySQL service, health check, dependency names, port, credentials, and volume with a pinned PostgreSQL Alpine service using `pg_isready`.
- [x] 5.2 Replace Kubernetes MySQL Secret data, PVC, Deployment, Service, probes, ports, mount paths, and application configuration with PostgreSQL equivalents.
- [x] 5.3 Validate Compose and Kubernetes configuration and document the one-time development reset using `docker compose down -v`.

## 6. End-to-End Verification

- [x] 6.1 Add a documented external PostgreSQL test configuration so database integration tests run against an isolated real database and skip clearly when it is absent.
- [x] 6.2 Run the complete Go test suite and compile both `cmd/feed` and `cmd/worker`.
- [x] 6.3 Start the stack from clean volumes and verify API/worker health plus account, video, Feed, interaction, exposure, message, and asynchronous persistence flows.
- [x] 6.4 Confirm the repository and runtime dependency graph contains no remaining MySQL driver, DSN, SQL dialect, service, port, health-check, or volume references.

## 7. Documentation and Specification Synchronization

- [x] 7.1 Update `README.md`, `openspec/project.md`, engineering, architecture, quick-read, optimization, deployment, and relevant module documentation to make PostgreSQL the durable source of truth.
- [x] 7.2 Update operational, interview, performance, and resume-oriented documentation where MySQL-specific commands or architecture claims would become inaccurate.
- [x] 7.3 Run strict OpenSpec validation and ensure the implemented behavior satisfies every `postgresql-persistence` and `account-identity` scenario.
