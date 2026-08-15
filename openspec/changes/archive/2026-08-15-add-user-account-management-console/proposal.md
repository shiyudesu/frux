## Why

Frux has no privileged interface for finding and managing ordinary consumer accounts, so operators cannot inspect account state, freeze abusive users, restore mistakenly frozen users, or terminate durable login sessions without direct database access. The admin workspace should provide this operational boundary while explicitly excluding administrator-account, role, and permission management.

## What Changes

- Add an `/admin/accounts` workspace for authorized operators to search and filter ordinary `user` accounts and inspect bounded identity, profile, status, registration, and aggregate-stat information.
- Add privileged account APIs for stable paginated listing, account detail, freeze, unfreeze, and force-sign-out operations.
- Restrict the capability to accounts whose current role is exactly `user`; reviewer, operator, admin, unknown privileged roles, role assignment, and permission configuration remain outside this change.
- Define frozen-account behavior: after correct password verification, consumer login returns a dedicated frozen-account result without issuing credentials; refresh is rejected, all durable refresh sessions are revoked, and already issued consumer access tokens remain usable only until their existing short expiration.
- Persist durable freeze and unfreeze notification outbox facts in the account transaction and asynchronously create idempotent `SYSTEM` messages containing the registered safe reason. A still-valid short access token can read a freeze message immediately, while missed messages remain available after a later successful login.
- Record successful freeze, unfreeze, and force-sign-out mutations as immutable audit facts in the same PostgreSQL transaction.
- Keep account enforcement separate from content enforcement: freezing an account does not hide, delete, or take down its existing videos.
- Add permission-aware Web states for loading, empty results, validation, conflict, forbidden, unavailable, and successful mutations.

## Capabilities

### New Capabilities

- `user-account-management-console`: Privileged discovery and lifecycle management of ordinary consumer accounts, excluding administrator identities and content enforcement.

### Modified Capabilities

- `admin-authorization`: Register and enforce the permission required by ordinary-user account management routes.
- `admin-audit-trail`: Add privacy-bounded transactional audit facts for account freeze, unfreeze, and force-sign-out operations.
- `consumer-auth-sessions`: Define durable refresh-session revocation and bounded residual access-token behavior after privileged account enforcement.

## Impact

The change affects the account domain and application service, PostgreSQL account, refresh-session, management-operation, and account-notification-outbox persistence, the message Application writer boundary, Worker composition, admin audit action registry, admin authorization permission registry, consumer-login and admin HTTP handlers/router/error codes, API-flow and persistence tests, the typed Web router and Admin Shell, login/message UX, account API types/client code, and account/admin/message/product/architecture/engineering/UI documentation. It adds no external dependency and does not introduce administrator account management, custom roles, password inspection/reset, profile editing, hard deletion, automatic content takedown, or a real-time push channel.
