## Why

The admin workspace currently reuses the consumer login page and consumer access token, which mixes user and operator sessions even though privileged authorization is checked separately. A dedicated admin authentication boundary is needed before the control plane expands further.

## What Changes

- Add a dedicated `/admin/login` Web route and admin-only login API that does not expose registration.
- Issue a distinct `admin_access` credential with an admin audience/token type and independently bounded lifetime.
- Keep admin browser session state, persistence, bootstrap, refresh, and logout separate from the consumer session.
- Continue resolving current account status, role, and permissions from durable account data on every privileged request.
- Allow a consumer session and an admin session to coexist in the same browser without overwriting each other.
- Reuse the current account identity store in the first phase; administrator provisioning and role assignment remain out of band.
- **BREAKING**: `/api/admin/*` no longer accepts ordinary consumer access tokens, including tokens belonging to accounts that currently have an admin role.

## Capabilities

### New Capabilities

- `admin-authentication`: Define dedicated admin login, credential issuance, session isolation, logout, and safe failure behavior.

### Modified Capabilities

- `admin-authorization`: Require an authenticated admin credential before current-account permission evaluation.
- `content-operations-console`: Route unauthenticated admin navigation through the dedicated admin session and login surface.

## Impact

Affected areas include JWT token types and claims, auth application/HTTP composition, admin authorization middleware, API errors, typed Web routing, a new Admin session provider and login page, and backend/frontend auth tests. Existing consumer login and user APIs remain compatible.
