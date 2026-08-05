## 1. Domain Authorization Model

- [x] 1.1 Add canonical admin role and permission constants under the account or admin authorization domain.
- [x] 1.2 Implement the closed role-to-permission registry with unknown-role deny behavior.
- [x] 1.3 Add domain tests for reviewer, operator, compatible admin, ordinary user, and unknown roles.

## 2. Current Principal Resolution

- [x] 2.1 Define the narrow admin principal reader interface used by HTTP authorization.
- [x] 2.2 Implement the PostgreSQL-backed reader for current account status and role.
- [x] 2.3 Add repository tests for active, disabled, demoted, missing, and unknown-role accounts.

## 3. HTTP Enforcement

- [x] 3.1 Add shared permission middleware that runs after JWT authentication and stores the resolved principal.
- [x] 3.2 Add stable forbidden API codes and mappings without changing existing 401 behavior.
- [x] 3.3 Wire a protected admin route group in the router with parameterized permission checks.
- [x] 3.4 Replace any new admin handler role comparisons with the shared principal helper.

## 4. Verification and Documentation

- [x] 4.1 Add middleware tests proving revoked JWT role claims do not retain admin authority.
- [x] 4.2 Add API-flow coverage for allowed, unauthenticated, forbidden, disabled, and compatible-admin requests.
- [x] 4.3 Update account, admin, product, architecture, and engineering documentation with the authorization boundary.
- [x] 4.4 Run targeted Go tests, the full Go test suite, and strict OpenSpec validation.
