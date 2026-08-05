## 1. Audit Domain and Storage

- [ ] 1.1 Add the validated immutable audit fact entity, action identifiers, target identifiers, and detail limits.
- [ ] 1.2 Add PostgreSQL audit model, indexes, migration registration, and domain conversion.
- [ ] 1.3 Implement append and stable filtered cursor-query repository methods with no update or delete surface.
- [ ] 1.4 Add domain and persistence tests for validation, ordering, filters, and cursor stability.

## 2. Transactional Integration Contract

- [ ] 2.1 Add the shared infrastructure helper that inserts a validated audit fact inside an existing GORM transaction.
- [ ] 2.2 Define application builders for success and denied-attempt audit facts without exposing GORM types.
- [ ] 2.3 Add a representative transaction test proving an audit insert failure rolls back its protected mutation.
- [ ] 2.4 Add best-effort denied-attempt recording with explicit failure metrics and logging.

## 3. Audit Query API

- [ ] 3.1 Add bounded audit query DTOs, filter validation, and signed or encoded cursor handling.
- [ ] 3.2 Add the `audit.read` protected admin endpoint and stable error mapping.
- [ ] 3.3 Add API-flow tests for pagination, filters, forbidden access, invalid ranges, and redacted detail.

## 4. Observability and Documentation

- [ ] 4.1 Add low-cardinality metrics for success audit writes, denied attempts, and write failures.
- [ ] 4.2 Update admin, product, architecture, engineering, and module documentation.
- [ ] 4.3 Run targeted Go tests, the full Go test suite, and strict OpenSpec validation.
