## Context

GCFeed currently opens a MySQL `database/sql` pool and passes it to the GORM MySQL dialect in both the API router and worker. Both processes execute `AutoMigrate`, and the migration package retries selected MySQL duplicate-object error numbers to tolerate concurrent startup.

The persistence layer is mostly expressed through GORM, but several details are MySQL-specific:

- MySQL drivers and DSN syntax are embedded in connection and composition code.
- Status fields use the unsupported PostgreSQL type `tinyint`.
- Explicit index names such as `idx_user_created`, `uk_user_idempotency`, and `uk_user_video` are reused by different tables; PostgreSQL index names share the schema namespace.
- Timeline index discovery queries MySQL `information_schema.statistics`.
- Exposure upserts use MySQL `VALUES(column)` expressions.
- Duplicate-key mapping depends on MySQL error 1062 or string inspection.
- MySQL's default case-insensitive collation currently makes account matching case-insensitive without an explicit domain rule.

The user has accepted rebuilding development data. There is no production-data copy, dual-write period, or zero-downtime cutover requirement.

## Goals / Non-Goals

**Goals:**

- Make PostgreSQL the only durable database used by API, worker, Compose, and Kubernetes manifests.
- Preserve repository transactions, idempotency, stable pagination, row-locking, counters, and exposure aggregation.
- Make schema initialization deterministic under concurrent API and worker startup.
- Make account case-insensitivity explicit and independent of database collation.
- Add verification that executes database-specific behavior against real PostgreSQL.
- Keep the existing `database/sql` pool reuse and four-layer dependency direction.

**Non-Goals:**

- Migrating or preserving existing MySQL development data.
- Supporting MySQL and PostgreSQL simultaneously.
- Introducing a generic database-provider abstraction.
- Replacing GORM or converting `AutoMigrate` into a full versioned migration framework.
- Changing public HTTP routes or response shapes.
- Adding PostgreSQL-specific search, vector, replication, or high-availability features.

## Decisions

### 1. Replace MySQL rather than add multi-dialect support

The codebase will remove MySQL drivers, DSN handling, error types, services, and documentation. PostgreSQL-specific runtime behavior will be allowed where it improves correctness, while repository code will continue using portable GORM APIs where practical.

Maintaining conditional drivers and SQL branches was rejected because there is no compatibility requirement and it would multiply schema and test paths.

### 2. Use pgx through the existing shared connection pool

`internal/infra/database` will open a pgx `database/sql` connection using host, port, user, password, database name, `sslmode`, and `TimeZone`. The router and worker will pass that pool to `gorm.io/driver/postgres`, preserving one pool per process.

Local and Compose configurations will use `sslmode: disable` and UTC. The configuration field remains explicit so deployed environments can require TLS without code changes. PostgreSQL will use a dedicated application user rather than a database superuser.

Letting GORM create a separate pool was rejected because the current architecture deliberately centralizes pool ownership, ping validation, and shutdown.

### 3. Serialize all schema initialization with a PostgreSQL advisory transaction lock

Both API and worker will retain startup migration, but the complete migration sequence will execute in one PostgreSQL transaction after acquiring a stable `pg_advisory_xact_lock` key:

```text
API ─────┐
         ├── advisory transaction lock
Worker ──┘        │
                  ├── AutoMigrate models
                  ├── ensure video_stat rows
                  └── ensure Timeline index
```

PostgreSQL transactional DDL means a failed initializer rolls back before the lock is released. Other processes wait and then re-evaluate the completed schema.

Designating only the API as migrator was rejected because workers and additional API replicas could still start independently in Kubernetes. Retrying duplicate-object SQLSTATE values was rejected because it treats races after they occur and does not protect post-migration setup.

### 4. Make the GORM schema PostgreSQL-native and namespace-safe

- Status fields will use `smallint` rather than `tinyint`.
- Embedding JSON will use `jsonb`, preserving database validation while leaving vector decoding in the application.
- Every explicit index will use a table-prefixed name. This fixes the current cross-table collisions and prevents future PostgreSQL namespace conflicts.
- The Timeline index will be declared or created through GORM's migrator rather than MySQL information-schema SQL. Its logical columns remain `(status, published_at, id)`; PostgreSQL can scan the B-tree in reverse for the all-descending query.
- Existing nullable idempotency columns remain nullable so multiple requests without a key do not conflict.
- Existing auto-incrementing `int64` keys will map to PostgreSQL identity/sequence-backed columns through GORM.

An external schema generated by pgloader was rejected because no data is being retained and it could drift from the GORM model contract.

### 5. Use translated GORM errors and PostgreSQL-compatible upserts

GORM will enable `TranslateError`, and repository conflict helpers will depend on `errors.Is(err, gorm.ErrDuplicatedKey)` rather than MySQL error structs or substring matching.

Exposure and recommendation upserts will use GORM conflict assignments for incoming values and an explicit atomic `exposure_count + 1` expression. Raw MySQL `VALUES(column)` expressions will be removed. Embedding upserts and relation `DoNothing` inserts already use compatible GORM conflict clauses.

Row-locking transactions will continue using `clause.Locking{Strength: "UPDATE"}`, which maps to PostgreSQL `FOR UPDATE`.

### 6. Normalize account identity in shared domain behavior

A shared account-normalization function will trim and lowercase account identifiers. Registration, login lookup, and restoration will use it, while nickname and password handling remain unchanged.

PostgreSQL `citext` was rejected because application-level normalization is visible, testable, does not require an extension, and preserves the invariant across persistence implementations. A functional `lower(account)` index was rejected because lookup code could still bypass it and expose mixed-case values.

Other event and idempotency identifiers remain trimmed but case-sensitive because they are opaque keys, not user identities.

### 7. Replace runtime environments and reset development storage

Compose will replace the MySQL service and volume with a pinned PostgreSQL Alpine image, port 5432, `POSTGRES_DB`/`POSTGRES_USER`/`POSTGRES_PASSWORD`, and `pg_isready` health checks. API and worker dependencies will point to that service.

Kubernetes resources will replace the MySQL Secret key, PVC, Deployment, Service, ports, probes, and data mount with PostgreSQL equivalents. Application configuration will use the PostgreSQL service name and dedicated credentials.

Because data preservation is out of scope, developers will be instructed to run `docker compose down -v` before the first PostgreSQL startup.

### 8. Verify dialect behavior with an external PostgreSQL test configuration

Database integration tests will accept an explicit test PostgreSQL DSN/configuration and skip only when it is absent. The tests will create isolated schema state and cover:

- clean and concurrent schema initialization;
- schema/index presence and PostgreSQL-compatible types;
- duplicate account and idempotency mapping;
- exposure upsert increments and incoming-field updates;
- account case normalization;
- representative row-locking/counter behavior.

The implementation will not add a testcontainers dependency. Validation will start the repository's PostgreSQL service, run the integration tests against it, and then exercise the complete Compose stack and API flows.

## Risks / Trade-offs

- [Existing development data is discarded] → Document the required volume reset and keep the old volume untouched unless the developer explicitly removes it.
- [Concurrent migration code holds a database-wide application lock] → Use one stable, narrowly scoped advisory key and keep only schema initialization inside the transaction.
- [Renaming indexes can leave obsolete indexes in a reused database] → The supported cutover uses an empty PostgreSQL volume; migration tests assert only the expected names.
- [UTC conversion changes displayed offsets if callers relied on local database time] → Configure PostgreSQL and pgx for UTC and keep API serialization tests around cursor and timestamp fields.
- [Lowercasing accounts is a visible behavior change] → Specify it explicitly, return the canonical value, and test registration, duplicate registration, and login variants.
- [Integration tests require an external service] → Keep unit/API tests fast by default and provide one documented command that starts PostgreSQL and runs the database suite.
- [GORM AutoMigrate remains less controlled than versioned migrations] → Limit this change to a clean-schema development baseline and treat adoption of versioned migrations as a separate future change.

## Migration Plan

1. Replace dependencies, database configuration, connection setup, and GORM dialect wiring.
2. Make models, index names, migration orchestration, duplicate handling, and upserts PostgreSQL-compatible.
3. Add account normalization and its domain/application tests.
4. Add PostgreSQL-backed integration coverage and run it against a clean database.
5. Replace Compose and Kubernetes database resources and validate configuration.
6. Recreate local volumes, start the complete stack, and exercise representative API and worker flows.
7. Update all technical and operational documentation and validate OpenSpec artifacts.

Rollback consists of reverting the code and manifests and recreating the previous MySQL development environment. PostgreSQL data created after the switch is not migrated back.

## Open Questions

None. PostgreSQL-only support, development data reset, UTC timestamps, application-level lowercase account normalization, and advisory-lock migration serialization are established decisions.
