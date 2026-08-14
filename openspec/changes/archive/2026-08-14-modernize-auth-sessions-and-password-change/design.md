## Context

The current consumer authentication flow issues one HS256 access JWT, stores it in `localStorage`, mirrors it into an HttpOnly `/uploads` asset cookie, and treats logout as a client-side state clear. There is no refresh credential, durable login-session record, per-account credential version, key identifier, issuer validation, or server-side revocation. Consumer and admin tokens have different purpose/audience values but share one signing secret. Consumer login also returns the same response for unknown accounts and wrong passwords without performing the admin flow's dummy bcrypt work, and it does not currently enforce the documented active-account rule.

This change crosses the account domain, JWT infrastructure, PostgreSQL persistence, middleware, rate limiting, protected asset access, Web session architecture, and administrator authentication. PostgreSQL remains the durable source of truth. Redis remains optional coordination for registered rate limits and does not become authoritative for session validity.

## Goals / Non-Goals

**Goals:**

- Provide an authenticated password-change flow that verifies the current password, enforces one shared password policy, handles concurrent changes safely, and keeps the initiating browser signed in.
- Replace persistent access-only consumer login with short-lived access JWTs and durable opaque refresh sessions.
- Make refresh, logout, password change, account disablement at refresh time, and refresh-token replay have explicit server-side semantics.
- Remove consumer bearer credentials from persistent JavaScript storage.
- Separate consumer and admin signing keys, require strict claims, and support bounded key rotation.
- Revoke all prior refresh sessions on password change and invalidate admin credentials immediately through the existing authoritative admin-principal read.
- Preserve protected-media behavior while rotating its short-lived asset credential with consumer access credentials.
- Keep authentication failure handling stable, safe, rate-limited, and testable.

**Non-Goals:**

- Immediate database-backed validation of every ordinary consumer API request.
- Password reset through email, phone, support staff, recovery codes, or an unauthenticated forgotten-password flow.
- Multi-factor authentication, passkeys, OAuth, device naming, or a user-visible session-management page.
- Replacing JWT with opaque access tokens or making Redis the source of truth for authentication.
- Sharing refresh sessions between the consumer and admin applications.

## Decisions

### 1. Use short-lived access JWTs plus opaque PostgreSQL refresh sessions

Consumer access JWTs remain locally verifiable and have a five-minute default TTL with a hard maximum of fifteen minutes. A refresh credential has a fixed thirty-day absolute lifetime and is represented as `session_id.secret`. PostgreSQL stores the session ID and SHA-256 hash of the secret, never the raw refresh credential.

The refresh session table contains the session ID, user ID, token-family ID, current secret hash, optional previous secret hash and grace deadline, account authentication version, absolute expiration, last-used time, revocation time/reason, replacement session reference, and timestamps. Expired and long-revoked rows are deleted by a bounded worker cleanup.

This hybrid keeps high-volume Feed reads from adding an account/session query to every request while making login continuity and logout durable. The alternative of checking `auth_version` or a session row on every consumer request would provide exact immediate access-token revocation but would add a PostgreSQL dependency and round trip to every authenticated request. A Redis version cache was rejected because cache invalidation failure cannot provide an exact security boundary while PostgreSQL remains authoritative.

### 2. Make access-token revocation bounded rather than pretending it is immediate

Password change increments `account.auth_version` and revokes all refresh sessions in the same PostgreSQL transaction. The initiating device receives a newly created refresh session and a new access JWT for the new authentication version. Other devices cannot refresh again, but already issued ordinary access JWTs can remain usable until their five-minute expiration.

Admin JWTs include `auth_version`. The existing admin permission middleware already reloads the current account principal for every protected request, so that read will also return the current authentication version and reject stale admin credentials immediately without adding another query.

This trade-off is explicit in the API and documentation. Exact immediate consumer access-token revocation remains a possible future mode if operational evidence justifies stateful validation on every request.

### 3. Use a shared password policy and compare-and-swap persistence

Registration and password change share one domain validator:

- trim surrounding whitespace consistently with current password semantics;
- require at least eight Unicode code points;
- reject values longer than 72 UTF-8 bytes before calling bcrypt;
- preserve case and internal whitespace;
- reject a new password that authenticates against the existing hash.

Existing accounts with shorter passwords can continue authenticating. They must choose a compliant password when changing it.

Password replacement locks or compare-and-swaps against the expected stored password hash. Two requests that both authenticated the same old password cannot both succeed: the first commits, while the second receives a credential-changed conflict instead of overwriting the first result.

### 4. Rotate refresh secrets with bounded concurrent-tab handling

Refresh validates the session ID, current secret hash, session state, expiration, account state, and account authentication version in one transaction using consistent account-before-session lock ordering. A successful refresh generates a new secret, moves the old hash into a short previous-secret grace slot, updates last-used time, and returns the new secret in the response cookie.

If a concurrent request presents the immediately previous secret during the grace interval, it receives a stable superseded-refresh conflict and does not revoke the token family or clear the newer cookie. The Web coordinator uses bounded exponential backoff across up to four attempts so several tabs can observe the winning cookie response without unbounded retry. Reuse after the grace interval revokes the token family and requires login.

This design avoids storing recoverable refresh secrets while preventing ordinary multi-tab races from being classified as theft. Because session IDs are cryptographically random, any non-current Secret presented for an existing active session after excluding the immediate grace case is treated as replay and revokes the family. This fail-closed rule detects arbitrarily old tokens without an unbounded consumed-secret table; possession of only an access token can at worst force logout of that already-compromised session.

### 5. Keep cookie authentication narrow and CSRF-resistant

The refresh cookie is `HttpOnly`, `Secure` in HTTPS deployments, `SameSite=Strict`, and scoped to `/api/sessions`. Refresh and logout endpoints validate the request origin in addition to SameSite enforcement. Successful logout revokes the durable session but does not clear the shared cookie in its response, preventing a delayed old response from deleting a newer login; a current refresh that proves the cookie invalid performs cleanup. Password change continues to require an access bearer token and the current password; it does not authenticate solely through a cookie.

The `/uploads` asset cookie remains a short-lived access JWT scoped to `/uploads`. Login, refresh, and password change replace it with the newly issued access token. Web logout removes the non-HttpOnly active marker immediately, and the server revokes the refresh session. The asset token can remain cryptographically valid only for the same short access-token TTL and is ignored without the active marker.

### 6. Harden JWT structure without unnecessary asymmetric-key complexity

Consumer and admin JWTs use separate HS256 key rings because signing and verification currently occur in the same API process. Each ring declares one active `kid` and one or more verification keys. New tokens require:

- issuer `frux`;
- audience `frux-consumer` or `frux-admin`;
- purpose `access` or `admin_access`;
- account subject in standard `sub`;
- `jti`, `iat`, `nbf`, and `exp`;
- account authentication version;
- consumer refresh-session ID for consumer access tokens.

Consumer tokens do not contain role data. Admin authorization continues reading current status, role, permissions, and authentication version from PostgreSQL.

Parser configuration accepts only HS256, applies a small bounded clock leeway, rejects unknown `kid` values, and requires every normative claim. RSA or EdDSA was rejected for now because no independent verifier needs a public-only key and asymmetric key operations would add deployment complexity without changing the current trust boundary.

### 7. Migrate old tokens through an explicit bounded compatibility window

The first deployment can verify both new keyed tokens and existing consumer/admin tokens signed with the current legacy secret. Configuration records the independent `legacy_issued_until` instant when old issuance stopped and requires `legacy_accept_until >= legacy_issued_until + maximum old TTL + clock leeway`. An accept deadline may pass naturally without preventing later restarts; runtime parsing then disables legacy verification. No-audience and no-`kid` compatibility is removed after the safe deadline.

Rollback keeps the old secret available until the compatibility deadline and does not remove refresh-session rows. If the new session path must be disabled, users can fall back to a fresh legacy login only while the rollback deployment still understands the old configuration.

### 8. Centralize the Web consumer-session coordinator

A framework-independent consumer-session coordinator owns the in-memory access token, expiration, current profile, refresh single-flight, and authentication state. `SessionProvider` subscribes to it and preserves the existing `useSession` surface where practical.

Authenticated API requests capture the current session epoch and token from the coordinator. On `AUTH_INVALID_ACCESS_TOKEN`, they reuse an already-refreshed token or perform one shared refresh only while the initiating epoch remains current; account switches and logout prevent replay under another identity. Refresh failures that prove the session invalid clear consumer state; current-password mistakes and password validation errors never trigger session clearing.

On startup, the coordinator deletes the legacy access-token local-storage key, treats any cached profile only as untrusted display data, and attempts refresh-cookie bootstrap before declaring the user unauthenticated. Login, password change, logout, bootstrap, and refresh serialize through a process-local queue plus the browser-wide Web Locks API so competing cross-tab `Set-Cookie` responses cannot overwrite a newer identity. Each request captures a session epoch; refresh that reveals another account advances the epoch and cancels the initiating retry. Explicit login and password change atomically replace the in-memory access token, cached profile, asset active marker, and refresh cookie response. Cross-tab logout uses a browser broadcast signal and generation invalidation so tabs clear memory and discard in-flight refreshes even though access credentials are no longer persisted.

The admin session remains isolated in `sessionStorage` and does not use the consumer refresh cookie.

### 9. Register authentication-specific rate limits

The shared rate-limit registry adds:

- consumer login: trusted client IP, distributed coordination with stricter local fallback;
- refresh: trusted client IP, distributed coordination with stricter local fallback;
- password change: authenticated user ID, distributed coordination and fail-closed behavior.

The router orders consumer authentication before the user-scoped password-change limiter. Unknown accounts still perform dummy bcrypt comparison, so response body, status, code, and major computational path match wrong-password attempts.

## Risks / Trade-offs

- [A stolen access JWT can act until expiration after logout or password change] → Default to five minutes, hard-cap consumer TTL at fifteen minutes, revoke refresh immediately, and document the bounded window.
- [Refresh rotation races across tabs] → Use one Web refresh single-flight, an exclusive credential-mutation guard, session epochs, a short previous-secret grace state, and bounded superseded backoff.
- [Refresh-session storage grows indefinitely] → Index expiration/revocation fields and let each hourly Worker run drain up to ten bounded batches.
- [Signing succeeds or fails outside the password transaction] → Generate random material before commit, sign immediately after the durable transaction, surface signing failure explicitly, and leave the user able to authenticate with the new password if response delivery fails.
- [Cookie deployment behind proxies marks Secure incorrectly] → Reuse trusted forwarded-protocol handling and cover production proxy behavior in API tests and deployment documentation.
- [Key rotation misconfiguration rejects all credentials] → Validate active key IDs, unique IDs, key lengths, TTL bounds, and compatibility deadlines at startup.
- [Removing localStorage token changes many request call sites] → Centralize migration through one coordinator and authenticated request path rather than adding page-specific refresh logic.
- [New password minimum rejects previously accepted registrations] → Preserve existing-password login, clearly describe the new policy in registration and change-password UI, and test Unicode/code-point versus bcrypt-byte boundaries.

## Migration Plan

1. Add the account authentication version and refresh-session schema, repository methods, cleanup indexes, and worker cleanup while retaining the old login response behavior.
2. Add separate consumer/admin key-ring configuration, strict new claims, legacy verification with an explicit deadline, and startup validation.
3. Add refresh-session creation, rotation, logout revocation, password change, rate limits, and API tests; keep legacy consumer tokens accepted during the overlap.
4. Deploy backend support before the Web migration so old clients continue using their unexpired access token while new clients can establish refresh sessions.
5. Deploy the Web coordinator, refresh bootstrap, memory-only access storage, password UI, and cross-tab logout; remove the legacy local-storage token on startup.
6. Wait at least the maximum legacy consumer/admin access TTL, then remove legacy no-audience/no-`kid` verification and the old shared-secret configuration path.
7. Mark issue 43 resolved and synchronize account, architecture, engineering, UI/UX, operations, and OpenSpec documentation.

Rollback before step 6 restores the previous Web bundle and backend login path while retaining additive database columns/tables. Rollback after strict legacy removal requires restoring the previous key configuration and compatibility verifier before serving old tokens; refresh rows remain harmless additive data.

## Open Questions

No blocking product questions remain. Exact numeric rate-limit capacities and the refresh previous-secret grace duration can be selected from existing registry bounds during implementation and locked by tests.
