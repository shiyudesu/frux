## 1. Domain Authorization Model

- [ ] 1.1 Add canonical admin role and permission constants under the account or admin authorization domain.
- [ ] 1.2 Implement the closed role-to-permission registry with unknown-role deny behavior.
- [ ] 1.3 Add domain tests for reviewer, operator, compatible admin, ordinary user, and unknown roles.

## 2. Current Principal Resolution

- [ ] 2.1 Define the narrow admin principal reader interface used by HTTP authorization.
- [ ] 2.2 Implement the PostgreSQL-backed reader for current account status and role.
- [ ] 2.3 Add repository tests for active, disabled, demoted, missing, and unknown-role accounts.

## 3. HTTP Enforcement

- [ ] 3.1 Add shared permission middleware that runs after JWT authentication and stores the resolved principal.
- [ ] 3.2 Add stable forbidden API codes and mappings without changing existing 401 behavior.
- [ ] 3.3 Wire a protected admin route group in the router with parameterized permission checks.
- [ ] 3.4 Replace any new admin handler role comparisons with the shared principal helper.

## 4. Verification and Documentation

- [ ] 4.1 Add middleware tests proving revoked JWT role claims do not retain admin authority.
- [ ] 4.2 Add API-flow coverage for allowed, unauthenticated, forbidden, disabled, and compatible-admin requests.
- [ ] 4.3 Update account, admin, product, architecture, and engineering documentation with the authorization boundary.
- [ ] 4.4 Run targeted Go tests, the full Go test suite, and strict OpenSpec validation.
