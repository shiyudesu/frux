## 1. Governance Domain

- [x] 1.1 Add the closed degradation-control registry with owner, default, process scope, stale age, and failure default.
- [x] 1.2 Add immutable control revision and active selection entities with expiry and optimistic concurrency.
- [x] 1.3 Add domain tests for unknown keys, invalid revisions, expiry, rollback, and process scope.

## 2. Persistence and Mutation APIs

- [x] 2.1 Add PostgreSQL revision and active-control models, indexes, and migration registration.
- [x] 2.2 Implement revision listing, active snapshot reads, create, update, and rollback repository methods.
- [x] 2.3 Implement transactional control mutation with required success audit fact.
- [x] 2.4 Add `governance.execute` protected handlers for query, update, and rollback.
- [x] 2.5 Add persistence and API-flow tests for revision conflicts, expiry, forbidden access, and audit rollback.

## 3. Local Snapshot Evaluation

- [x] 3.1 Implement validated snapshot loading and atomic in-memory swap for API and worker processes.
- [x] 3.2 Add bounded polling, last-known-good retention, maximum stale handling, and clean shutdown.
- [x] 3.3 Define narrow application reader interfaces and integrate one optional capability end to end.
- [x] 3.4 Add unit tests for fresh, missing, invalid, expired, polling-failed, and over-stale evaluation.

## 4. Observability and Documentation

- [x] 4.1 Add low-cardinality metrics for active revision, poll result, snapshot age, invalid control, and evaluation fallback.
- [x] 4.2 Add Prometheus alerts for stale snapshots and repeated poll failure.
- [x] 4.3 Update governance, monitoring, product, architecture, and engineering documentation.
- [x] 4.4 Run targeted governance tests, the full Go suite, Compose config validation, and strict OpenSpec validation.
