## Context

Frux currently authenticates all people through the consumer login endpoint, stores one access token in the shared Web session, and then resolves current account role and permissions for `/api/admin/*`. This prevents stale JWT role claims from granting privileges, but it leaves the admin workspace coupled to the consumer login, registration, storage key, and logout lifecycle.

The first phase needs a separate authentication ceremony and credential boundary without introducing a second identity database, role-management UI, refresh-token system, or external SSO. The existing account password hash, active status, closed role mapping, and per-request permission evaluation remain authoritative.

## Goals / Non-Goals

**Goals:**

- Provide an admin-only login route and API with no registration path.
- Issue a credential that consumer endpoints and admin endpoints can distinguish cryptographically and semantically.
- Let user and admin sessions coexist without overwriting each other.
- Keep current account status/role checks authoritative after credential issuance.
- Fail safely and generically for wrong passwords, disabled accounts, and accounts without admin permissions.

**Non-Goals:**

- Creating a separate administrator account table.
- Adding role editing, invitations, password reset, MFA, refresh tokens, SSO, or global token revocation.
- Trusting a role claim inside the admin token as the final authorization decision.
- Sharing an authenticated admin session automatically across browser tabs.

## Decisions

### Reuse account credentials but issue a distinct admin token

Add `POST /api/admin/auth/login`. It accepts the same bounded account/password credential shape as consumer login but is handled by a dedicated Admin Authentication Service. The service:

1. verifies the account and password through the existing account credential verifier;
2. requires active status and at least one permission from the closed admin role registry;
3. returns the same generic authentication failure for an unknown account, wrong password, inactive account, or non-admin role;
4. issues an `admin_access` JWT only after all checks pass.

The token contains `sub`, `token_type=admin_access`, `aud=frux-admin`, `iat`, and bounded `exp`. Role claims are optional diagnostic snapshots and are never used as current permission facts.

Alternative considered: a separate admin account table. Rejected for this phase because it duplicates password lifecycle and identity ownership without an operator-provisioning or SSO design.

### Admin routes require the expected token type and audience

JWT verification gains an expected token-purpose API. Consumer middleware accepts only `access` with the consumer audience; admin middleware accepts only `admin_access` with `frux-admin`. A valid token of the wrong type is an authentication failure, not a permission failure.

After admin-token verification, the existing current-account resolver loads status and role and applies the closed permission registry for each request. This retains immediate demotion and disable behavior.

The admin login route sits outside the protected admin middleware group but uses the existing layered login rate-limiting capability. Every other `/api/admin/*` route, including `/api/admin/me`, requires an admin token.

Alternative considered: accept both token types during steady state. Rejected because it would preserve the mixed boundary this change exists to remove.

### Use an isolated short-lived browser session

The Web client introduces `AdminSessionProvider`, loaded only for admin routes. It stores the admin token and validated principal under a versioned `sessionStorage` key, while the consumer `SessionProvider` continues using its existing storage.

`sessionStorage` is chosen over `localStorage` to reduce persistence of a privileged bearer token. It survives reload in the current tab, allows consumer/admin coexistence, and intentionally requires a new login in a separate tab or after the browser session ends.

Admin logout clears the admin token, principal, permissions, and cached admin data only. Consumer logout does not mutate the admin key, and admin logout does not mutate consumer state.

Alternative considered: an HttpOnly admin cookie. Deferred because it would require a CSRF token/origin-verification design for every admin mutation; the bearer-token architecture already exists and can be isolated with lower migration risk.

### Route unauthenticated admin users only to `/admin/login`

The typed History API router adds `/admin/login`. Direct navigation to any other `/admin/*` route follows:

```text
no admin token -> /admin/login
admin token -> /api/admin/me
valid principal -> requested admin page
401 -> clear admin session and /admin/login
403 -> render forbidden without exposing cached data
503 -> render authorization unavailable
```

The admin login page has admin branding, account/password fields, bounded errors, and no registration or consumer-login redirect. A successfully authenticated principal returns to the validated originally requested admin route.

### Keep admin authentication stateless in phase one

There is no refresh token or server session table. Admin token lifetime is configurable with a conservative default and hard maximum. Current account checks provide immediate authorization revocation, while logout removes the client credential. A later MFA/SSO/session-revocation change can replace issuance without changing permission middleware.

## Risks / Trade-offs

- [Bearer tokens remain exposed to same-origin XSS] → Keep strict TypeScript/React rendering rules, use `sessionStorage`, shorten admin lifetime, and never place tokens in URLs or logs.
- [A stateless token cannot be individually revoked before expiry] → Re-evaluate account status/role on every request and use a shorter admin TTL.
- [Existing bookmarks open in a new tab without the admin session] → Redirect safely to `/admin/login` and preserve the validated return route.
- [Cutover breaks scripts using consumer tokens against admin APIs] → Treat this as an explicit breaking change, update tests/docs, and deploy backend plus Web client together.
- [Generic login failure obscures role provisioning mistakes] → Keep user-facing failure generic while emitting bounded server metrics/log reasons without credentials.

## Migration Plan

1. Add JWT purpose/audience validation and the admin login service/API without changing existing admin middleware.
2. Add AdminSessionProvider and `/admin/login` behind the new API.
3. Switch `/api/admin/*` middleware to require `admin_access`; deploy the Web change in the same release.
4. Clear any legacy admin principal cached in the consumer session and document that operators must log in again.
5. Rollback may temporarily restore dual token acceptance only as an emergency compatibility gate; it must default off and be removed after cutover.

## Open Questions

MFA, external SSO, dedicated operator provisioning, and server-side session revocation are intentionally deferred to later changes.
