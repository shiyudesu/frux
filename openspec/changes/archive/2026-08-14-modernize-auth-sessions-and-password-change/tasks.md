## 1. Account Domain Contracts

- [x] 1.1 Add shared password normalization and validation for registration and password replacement, including eight-code-point minimum, 72-byte maximum, unchanged-password detection, and domain unit tests.
- [x] 1.2 Add `auth_version` to account restoration and admin-principal projections while preserving existing accounts at version 1.
- [x] 1.3 Define refresh-session entities, states, revocation reasons, expiration rules, secret-hash comparison, and concurrent previous-secret grace behavior in the account domain.
- [x] 1.4 Extend account repository contracts with refresh-session creation/rotation/revocation, authentication-version reads, atomic password replacement, and bounded expired-session cleanup.

## 2. PostgreSQL Persistence and Migration

- [x] 2.1 Add the `account.auth_version` column and refresh-session GORM models with unique, lookup, expiration, revocation, and cleanup indexes.
- [x] 2.2 Register the additive schema in shared API/Worker migration and backfill existing accounts to authentication version 1.
- [x] 2.3 Implement refresh-session creation and lookup without persisting raw refresh tokens or access credentials.
- [x] 2.4 Implement row-locked refresh rotation with current-secret compare, previous-secret grace conflict, replay family revocation, account status/version checks, and deterministic result classification.
- [x] 2.5 Implement idempotent current-session logout and bulk account-session revocation.
- [x] 2.6 Implement atomic password replacement that verifies the expected stored hash, increments authentication version, revokes prior sessions, and creates the initiating browser's replacement session.
- [x] 2.7 Implement bounded deletion of expired and sufficiently old revoked refresh-session rows.
- [x] 2.8 Add persistence tests for hashes-at-rest, rotation races, replay revocation, logout replay, password compare-and-swap, transaction rollback, and cleanup ordering.

## 3. JWT Configuration and Infrastructure

- [x] 3.1 Replace the shared JWT secret configuration with validated consumer/admin HS256 key rings, active key IDs, bounded access TTLs, issuer, clock leeway, and explicit legacy compatibility deadline.
- [x] 3.2 Update repository configuration files, environment overrides, production validation, and startup logging without exposing key material.
- [x] 3.3 Refactor the JWT manager to issue strict consumer claims containing subject, session ID, authentication version, purpose, audience, issuer, `kid`, `jti`, `iat`, `nbf`, and `exp`, without consumer role data.
- [x] 3.4 Refactor admin JWT issuance to use the isolated admin key ring and strict authentication-version claims.
- [x] 3.5 Implement strict key-ID lookup, issuer/audience/purpose validation, bounded leeway, required-claim checks, and separate consumer/admin parsers.
- [x] 3.6 Implement deadline-bound legacy shared-secret/no-key-ID verification that never issues another legacy token and becomes unavailable after the configured migration deadline.
- [x] 3.7 Add JWT and configuration tests for cross-purpose rejection, unknown keys, key rotation overlap, malformed or missing claims, TTL bounds, clock behavior, and legacy cutoff.

## 4. Consumer Session Application Flows

- [x] 4.1 Add cryptographically secure refresh session/family/secret generation and SHA-256 hashing behind narrow application interfaces.
- [x] 4.2 Harden consumer login with active-account enforcement, dummy bcrypt work for unknown accounts, shared credential failures, refresh-session creation, and strict access-token issuance.
- [x] 4.3 Implement refresh orchestration with cookie credential parsing, transactional rotation, superseded-race handling, replay classification, new access issuance, and explicit unavailable failures.
- [x] 4.4 Implement durable idempotent logout using the refresh cookie even when the access token is missing or expired.
- [x] 4.5 Implement authenticated password change with current-password verification, shared new-password policy, unchanged-password rejection, atomic credential/session replacement, and replacement access issuance.
- [x] 4.6 Add a bounded Worker cleanup loop for expired and old revoked refresh sessions with supervised shutdown and metrics/logging consistent with existing database-owned maintenance work.
- [x] 4.7 Add application unit tests for login timing paths, inactive accounts, refresh results, logout idempotency, password errors, signing failures, and cleanup batches.

## 5. HTTP, Cookies, and Authentication Middleware

- [x] 5.1 Add typed login/refresh/password request and token response DTOs without returning refresh credentials in JSON.
- [x] 5.2 Add scoped refresh-cookie helpers with HttpOnly, SameSite=Strict, HTTPS Secure handling, bounded Max-Age, safe expiration, and request-origin validation for cookie-authenticated session endpoints.
- [x] 5.3 Add `POST /api/sessions/current/refresh`, update login and `DELETE /api/sessions/current`, and add `PUT /api/users/me/password`.
- [x] 5.4 Extend consumer authentication context with strict subject, refresh-session ID, authentication version, and access expiration while preserving optional protected-asset authentication.
- [x] 5.5 Rotate the `/uploads` asset JWT cookie on login, refresh, and password change without refreshing it on ordinary authenticated responses.
- [x] 5.6 Return stable password-change, refresh-invalid, refresh-replayed, refresh-superseded, conflict, and authentication-unavailable codes with safe status mappings.
- [x] 5.7 Extend admin principal reads and permission middleware to reject stale admin authentication versions before protected handlers execute.
- [x] 5.8 Add HTTP/middleware tests for cookie flags and paths, origin rejection, absent/expired-cookie logout, response ordering, strict JWT claims, admin invalidation, and protected-asset behavior.

## 6. Authentication Rate Limits

- [x] 6.1 Register consumer-login and refresh IP policies plus a user-scoped fail-closed password-change policy with normal and emergency profiles.
- [x] 6.2 Wire rate-limit middleware in the correct router order so password changes derive identity only after JWT authentication.
- [x] 6.3 Add registry, middleware, fallback, Redis-failure, header, and endpoint rejection tests for all authentication policies.

## 7. Web Session Architecture

- [x] 7.1 Add typed login, refresh, logout, and password-change API contracts and update the account API module to rely on credentialed same-origin cookie requests where required.
- [x] 7.2 Create a framework-independent consumer-session coordinator for in-memory access credentials, expiration, profile state, refresh single-flight, and subscriptions.
- [x] 7.3 Add one-time authenticated-request retry after shared refresh while preventing refresh recursion, duplicate rotation, and retries for non-session credential errors.
- [x] 7.4 Refactor `SessionProvider` to bootstrap through refresh, delete the legacy localStorage bearer key, treat cached profile data as non-authoritative, and preserve `useSession` behavior.
- [x] 7.5 Add cross-tab consumer logout propagation while keeping the separate admin `sessionStorage` session untouched.
- [x] 7.6 Keep the protected-asset active marker synchronized with login, refresh bootstrap, password replacement, local logout, and invalid-refresh cleanup.
- [x] 7.7 Update login/registration UI for the shared password rule and correct `current-password` versus `new-password` autocomplete semantics.
- [x] 7.8 Add an own-profile account-security entry and dedicated password-change dialog with current, new, and confirmation fields, accessible focus handling, busy/error/success states, and no credential mixing with profile edits.
- [x] 7.9 Add safe Chinese messages for all new stable password/session codes and ensure only invalid access/refresh outcomes clear authentication.
- [x] 7.10 Add Web tests for reload bootstrap, token non-persistence, legacy-key removal, refresh single-flight, retry-once behavior, superseded races, cross-tab logout, password validation, successful credential adoption, and admin-session isolation.

## 8. End-to-End Authentication Verification

- [x] 8.1 Extend account API-flow tests for compliant and legacy-short registration/login behavior, dummy unknown-account work, inactive-account rejection, and strict login cookies.
- [x] 8.2 Add API-flow tests covering refresh rotation, concurrent previous-secret conflicts, replay family revocation, expiration, malformed cookies, account-version mismatch, and idempotent logout.
- [x] 8.3 Add API-flow tests proving password change rejects wrong/unchanged/invalid passwords, permits exactly one concurrent replacement, rolls back atomically, accepts only the new password afterward, and keeps the initiating browser signed in.
- [x] 8.4 Add integration coverage proving password change revokes other refresh sessions, bounds old consumer access by TTL, and immediately invalidates existing admin credentials.
- [x] 8.5 Add key migration tests proving old tokens work only before the explicit deadline and strict new tokens continue across active-key rotation.

## 9. Documentation and Rollout

- [x] 9.1 Update `docs/modules/account.md` with password policy, refresh-session table, endpoint contracts, cookie behavior, access-token revocation bounds, errors, and tests.
- [x] 9.2 Update architecture, engineering, UI/UX, rate-limiting, and protected-media documentation for the new trust boundaries and frontend coordinator.
- [x] 9.3 Update development, Docker, production, deployment, environment, and operations configuration examples for separate rotatable key rings and the compatibility deadline.
- [x] 9.4 Document staged backend-first/Web-second deployment, legacy-token cutoff, rollback constraints, and refresh-session cleanup operations.
- [x] 9.5 Mark item 43 in `docs/当前问题.md` resolved only after backend, Web, and rollout behavior are verified.

## 10. Validation

- [x] 10.1 Run gofmt and targeted account, persistence, JWT, middleware, rate-limit, admin-auth, protected-asset, and API-flow Go tests.
- [x] 10.2 Run `cd apps/api && go test ./...` and compile both `./cmd/feed` and `./cmd/worker`.
- [x] 10.3 Run targeted Vitest session/account/UI tests, `pnpm -C apps/web run lint`, and `pnpm -C apps/web run build`.
- [x] 10.4 Run `cd apps && docker compose config` and `openspec validate --all --strict`.
