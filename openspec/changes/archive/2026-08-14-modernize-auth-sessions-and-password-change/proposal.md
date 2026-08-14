## Why

Frux currently issues access-only JWTs that persist in browser storage, cannot be refreshed or revoked server-side, and leave password changes unable to invalidate other login sessions. The authentication boundary should be modernized before adding password changes so login, logout, credential rotation, protected media access, and administrator access have explicit and bounded session semantics.

## What Changes

- Add authenticated user password changes with current-password verification, shared password validation, atomic credential replacement, and concurrent-change protection.
- **BREAKING** Require newly registered and newly selected passwords to contain at least eight Unicode characters and no more than 72 UTF-8 bytes; existing shorter passwords remain valid only for authentication and migration to a compliant password.
- Replace access-only consumer login with short-lived access JWTs plus opaque rotating refresh sessions persisted as hashes in PostgreSQL.
- Keep the current browser signed in after a successful password change while revoking every previously established refresh session for the account.
- Move consumer access credentials out of persistent Web storage and restore browser sessions through an HttpOnly refresh cookie.
- Make consumer logout revoke the current refresh session rather than acting only as a client-side acknowledgement.
- Harden JWT claims and validation with issuer, audience, purpose, subject, session, version, token ID, issued-at, not-before, expiration, bounded TTLs, separate consumer/admin signing keys, and key identifiers for rotation.
- Remove stale authorization data from consumer JWTs and make admin credentials invalid when the account credential version changes.
- Add registered rate-limit policies for consumer login, refresh, and password-change attempts, including safe failure behavior.
- Add stable API errors and Chinese Web messages for password validation, incorrect current passwords, credential conflicts, expired or replayed refresh sessions, and temporary authentication unavailability.
- Preserve the existing protected-media product behavior while rotating the short-lived asset credential together with consumer access credentials.

## Capabilities

### New Capabilities

- `consumer-auth-sessions`: Consumer login, short-lived access JWTs, durable rotating refresh sessions, logout, password changes, credential-version behavior, browser cookie handling, and protected-asset credential synchronization.

### Modified Capabilities

- `admin-authentication`: Require isolated signing keys, strict JWT claims, bounded compatibility migration, and credential-version invalidation for admin access.
- `web-frontend`: Replace persistent consumer bearer-token storage with in-memory access state and refresh-based session bootstrap while preserving typed session distribution.
- `layered-request-rate-limiting`: Add registered consumer login, refresh, and password-change policies with server-derived identities and bounded fallback behavior.
- `user-facing-api-errors`: Add stable authentication-session and password-change error contracts without treating credential mistakes as expired login state.

## Impact

- Backend account domain/application services, JWT infrastructure, session persistence, migrations, authentication middleware, rate-limit registry, HTTP DTOs/handlers/routes, configuration, and API-flow tests.
- Web session provider, account API client, authentication page, profile security UI, protected-asset activation, error catalog, local-storage migration, and frontend tests.
- `account` gains an authentication version; a new refresh-session table stores only bounded metadata and token hashes.
- Deployment configuration changes from one shared JWT secret to separate rotatable consumer and admin key sets, with a bounded migration path for already-issued access tokens.
- Account, architecture, engineering, UI/UX, operations, and current-issues documentation must be synchronized with the new behavior.
