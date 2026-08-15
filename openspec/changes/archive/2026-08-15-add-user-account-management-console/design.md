## Context

Frux stores consumer and privileged identities in the shared `account` table, but the requested capability concerns only ordinary accounts whose current role is exactly `user`. Today there is no privileged account list or lifecycle API. Account status already distinguishes normal, frozen, and cancelled records in documentation; password login currently collapses every inactive state into generic invalid credentials; refresh sessions are durable PostgreSQL records; and consumer access tokens expire after five minutes by default.

The Admin Shell currently exposes review and video operations behind the closed permission registry. Successful privileged writes use account-derived principals and immutable audit facts, but account persistence has no transactional API that can update status, increment `auth_version`, revoke all sessions, persist an idempotent result, and append audit in one boundary.

## Goals / Non-Goals

**Goals:**

- Let a narrowly authorized administrator discover and inspect all ordinary consumer accounts, including frozen and cancelled history.
- Support reasoned freeze, unfreeze, and force-sign-out operations with optimistic concurrency, safe retry, immediate refresh-session revocation, and transactional audit.
- Tell a credential holder that an account is frozen only after the submitted password has been verified, without weakening unknown-account and wrong-password indistinguishability.
- Deliver durable, reasoned freeze and unfreeze messages through the existing message center and retain them when no access token remains usable.
- Keep private login identifiers visible only inside this privileged boundary.
- Reuse the current account source of truth, authentication version, refresh-session model, admin middleware, audit infrastructure, typed router, and Admin Shell patterns.
- Preserve clear ownership boundaries between account enforcement and video/content enforcement.

**Non-Goals:**

- Listing, creating, editing, freezing, promoting, or demoting reviewer, operator, admin, or unknown-role identities.
- Role assignment, custom permissions, administrator provisioning, password viewing/reset, profile editing, impersonation, account hard deletion, or cancellation.
- Automatically hiding videos, comments, existing messages, relationships, or profile data when an account is frozen.
- Introducing immediate per-request revocation for already issued consumer access JWTs, MFA, SSO, device metadata, or a general policy engine.
- Adding WebSocket, browser push, email, SMS, or another real-time delivery channel.

## Decisions

### Add a dedicated account-management application boundary

Extend the account domain with explicit status constants, ordinary-account query/detail projections, account-management commands, registered reason codes, operation names, version conflicts, and repository interfaces. Implement account-management orchestration in a dedicated application service under `internal/application/account` rather than expanding HTTP handlers or the generic profile service with privileged branching.

The service accepts an already resolved actor ID, target user ID, expected version, reason, and idempotency key. It creates the validated audit fact and delegates one transactional commit to account infrastructure.

Alternative considered: add account queries and updates directly to the generic admin handler. Rejected because validation, cursor binding, concurrency, session revocation, and audit composition are account use-case rules rather than HTTP concerns.

### Enforce the ordinary-user boundary in every repository query and mutation

List and detail queries include `account.role = 'user'`. Mutations lock the target account and re-check the same role inside the transaction before any status, version, session, idempotency, or audit write. A direct request for a privileged or unknown-role identity returns the same ordinary-user-not-found result as a missing target.

This defense is required even though the Web list excludes privileged roles: direct calls and a concurrent out-of-band role change must not turn the feature into administrator-account management.

Alternative considered: return privileged rows read-only. Rejected because the user explicitly excluded administrator-account management, and exposing those identities would create pressure for unsupported role/status controls.

### Use `account.manage` and grant it only to the compatibility admin role

Register `account.manage` in the closed permission registry. `RoleAdmin` continues to inherit the full registered set; reviewer and operator mappings remain unchanged. All `/api/admin/accounts*` routes declare this permission through shared middleware.

Alternative considered: give account management to `operator`. Rejected because freezing identities and terminating all sessions has broader security impact than content enforcement and should default to the narrowest existing role.

### Use filter-bound HMAC cursor pagination

`GET /api/admin/accounts` supports positive user ID, canonical account/nickname query, status, cursor, and limit. The application normalizes the query, binds the full filter set into a hash, and encodes an HMAC cursor containing version, filter hash, `created_at`, and user ID. Persistence returns `limit + 1` rows ordered by `created_at DESC, id DESC`.

The query always filters role before text matching. Add a composite PostgreSQL index covering `(role, status, created_at DESC, id DESC)` for the common list path. Reuse existing escaped `ILIKE` behavior rather than introducing a search engine or PostgreSQL extension in this change.

Alternative considered: offset pagination. Rejected because concurrent registrations and status changes would cause duplicates or skipped accounts.

### Return a privacy-bounded operational projection

List rows contain user ID, canonical login account, nickname, avatar, status, created/updated times, and bounded aggregate counts. Detail may additionally include bio, gender, public/private work counts, received likes, relationship counts, active refresh-session count, and a `version` field mapped from `auth_version`.

No query selects password hashes, refresh secret hashes, previous hashes, refresh-session IDs, access tokens, cookies, or signed asset credentials. The private account identifier is permitted by the existing privileged account-identity boundary but remains absent from consumer-facing projections and audit detail.

### Model lifecycle operations as versioned idempotent commands

Expose:

- `POST /api/admin/accounts/:userId/freeze`
- `POST /api/admin/accounts/:userId/unfreeze`
- `POST /api/admin/accounts/:userId/sessions/revoke`

Each request requires `Idempotency-Key` with the existing 128-character limit and carries `expected_version` plus a registered `reason_code`. The account module adds `account_management_operation` with actor ID, idempotency key, request fingerprint, target user ID, operation, and serialized result. `(actor_id, idempotency_key)` is unique.

Inside one transaction, persistence:

1. loads an existing operation for replay and rejects fingerprint mismatch;
2. locks the target account and verifies `role='user'` plus the expected `auth_version`;
3. validates the allowed status transition;
4. increments `auth_version` for every committed operation;
5. changes status when applicable;
6. revokes all active refresh sessions for freeze and force-sign-out;
7. appends the validated audit fact;
8. appends a durable account lifecycle notification outbox row for freeze or unfreeze;
9. stores the idempotency result; and
10. commits before reporting success and recording committed audit metrics.

The result stores only the target ID, resulting status/version, revoked-session count, operation, and occurrence time. The Web refreshes list/detail data after success rather than treating the idempotency row as a profile cache.

Alternative considered: use only `expected_version`. Rejected because a transport timeout after commit would make a safe client retry appear as a conflict. Alternative considered: use the audit table itself as an idempotency ledger. Rejected because it has no actor/key uniqueness or complete replay-result contract.

### Define explicit status transitions without content side effects

Freeze accepts only normal `user` accounts and changes status to frozen. Unfreeze accepts only frozen `user` accounts and changes status to normal. Cancelled accounts remain visible but read-only. Force-sign-out accepts normal or frozen `user` accounts and preserves status.

All three operations increment `auth_version`. Freeze and force-sign-out revoke active refresh sessions with distinct registered revocation reasons. Unfreeze creates no replacement session and leaves prior revocations intact, so the user must log in again.

The account transaction does not import video, interaction, relation, message, or media infrastructure. It owns only an account notification outbox fact and later calls message Application through a narrow writer interface. Existing content remains governed by its own lifecycle; the account page links to the existing video operations filter when an operator needs a separate content decision.

Alternative considered: cascade freeze into video takedown. Rejected because account access and content eligibility are different domain facts, a cascade would create a large cross-module transaction, and restoration semantics would be ambiguous.

### Reveal frozen status only after password proof

Consumer login keeps the dominant password-check path unchanged:

1. normalize and load the account, using dummy bcrypt work for an unknown account;
2. compare the submitted password;
3. return generic `AUTH_INVALID_CREDENTIALS` for an unknown account or wrong password;
4. after a successful comparison, return HTTP 423 with `AUTH_ACCOUNT_FROZEN` when status is frozen;
5. keep cancelled and unsupported account states on the generic invalid-credentials result; and
6. create no Refresh Session, Access Token, or asset cookie for a frozen result.

The Web login page maps only `AUTH_ACCOUNT_FROZEN` to “该账号已被冻结，请查看账号消息或联系管理员”.
This reveals status only to a caller who already proved knowledge of the current password.

Alternative considered: include the freeze reason in the login error. Rejected because the durable message is the authoritative user-facing explanation, while the login endpoint should return a stable bounded result without copying lifecycle history into an authentication response.

### Deliver freeze and unfreeze through an account-owned outbox

Add `account_notification_outbox` with stable event ID, recipient user ID, operation
(`freeze` or `unfreeze`), registered reason code, occurred time, delivery state, attempts, availability,
lease owner/until, last error, delivered time, and timestamps. The event ID is derived from
`user_id + operation + new_auth_version`, so a committed lifecycle transition has one notification identity.

Freeze and unfreeze append the outbox row in the same PostgreSQL transaction as account status/version,
Refresh Session changes, audit, and idempotency result. Force-sign-out does not append one. A same-key
idempotent replay returns the stored operation result and creates neither a second outbox row nor a second message.

The Worker polls bounded batches, claims with `FOR UPDATE SKIP LOCKED`, uses a stable lease owner, applies
exponential retry and terminal classification, and calls a narrow `AccountLifecycleMessageWriter`. The message
Application validates operation/reason and creates an idempotent `SYSTEM` message with:

- freeze title `账号已被冻结`;
- unfreeze title `账号已解冻`;
- safe Chinese content mapped from the closed reason registry; and
- the outbox event ID as both event and idempotency identity.

The account module does not write `user_message` directly and the message module does not read account tables.
If the pre-freeze Access Token remains valid, existing unread polling can expose the freeze message before token
expiry. If delivery or reading occurs later, the message remains durable and is visible after unfreeze and a new
password login. No real-time push guarantee is claimed.

Alternative considered: synchronously insert `user_message` inside the account transaction. Rejected because it
breaks message ownership and couples account availability to message persistence behavior.

### Preserve bounded residual access-token behavior

The current consumer middleware verifies short-lived JWTs without loading account state on every request. Therefore freeze and force-sign-out prevent all future refresh immediately, but an already issued access token can remain usable until its existing expiration, normally no more than five minutes and never more than the configured fifteen-minute cap.

The Admin UI confirmation and success copy must state this bounded behavior rather than claiming instantaneous device logout. That residual window also lets the existing message center fetch a newly delivered freeze notification. Adding a database/Redis revocation check to every consumer request is deferred.

Alternative considered: add immediate access-token revocation. Rejected for this change because it changes the latency and availability boundary of every consumer-authenticated API.

### Extend immutable audit schemas for account enforcement

Register actions `account.freeze`, `account.unfreeze`, and `account.sessions_revoke`, target type `user_account`, permission `account.manage`, exact methods/routes, allowed status/version fields, bounded numeric revoked-session count, and operation-specific reason registries.

Audit target ID is the numeric user ID. Detail excludes account, nickname, profile fields, credential material, refresh-session IDs, and raw idempotency keys. The shared builder stores only the idempotency-key SHA-256 digest. Audit validation or insertion failure rolls back the entire account operation.

### Add a typed Admin Shell destination and complete states

Add `/admin/accounts` to the `Route` and `AdminProtectedRoute` unions, normalize and validate the route without a routing library, and add an Admin Shell destination gated by `account.manage`. Replace the current two-route permission conditional with an explicit route-to-permission mapping so new destinations do not silently inherit `review.read`.

The page uses one filter form and stable load-more pagination, a table with status badges and bounded account facts, and a detail/action panel. Freeze, unfreeze, and force-sign-out use confirmation dialogs, registered reason selectors, fresh expected versions, generated idempotency keys, disabled submitting states, and conflict-triggered refresh. A 403 clears account page data; an admin-authentication 401 continues to clear only the admin session.

## Risks / Trade-offs

- [The feature accidentally touches a privileged account after a concurrent role change] → Include `role='user'` in the locked mutation predicate and return ordinary-user-not-found before any write.
- [Freeze is interpreted as immediate JWT revocation] → Explain the bounded residual access window in UI copy and API documentation; revoke refresh sessions and increment version transactionally.
- [Frozen login status enables account enumeration] → Return it only after successful password comparison; unknown accounts and wrong passwords retain dummy bcrypt and the generic response.
- [The old token expires before the notification is delivered or read] → Persist an account-owned Outbox and idempotent message; immediate visibility is best effort, while later visibility is durable.
- [Message persistence failure blocks account enforcement] → Commit the Outbox rather than the message; Worker retries independently after the account transaction succeeds.
- [A timed-out write is repeated] → Require actor-scoped idempotency keys and persist the replay result in the same transaction.
- [Account search leaks private identifiers] → Require `account.manage`, omit privileged identities, keep account values out of audit detail, and never reuse this projection in public APIs or caches.
- [Freeze is mistaken for content takedown] → Keep content untouched, state that behavior in confirmation copy, and link separately to video operations.
- [Aggregate joins or text filters become expensive] → Use bounded limits, role/status pagination indexes, set-based aggregate queries, escaped filters, and query-count/performance tests.
- [Audit schema growth accepts contradictory facts] → Bind every new action to one permission, target, route, method, transition, reason registry, and exact detail-key set.

## Migration Plan

1. Add the frozen-login domain result, account notification Outbox model, repository methods, Worker, narrow message writer, reason mappings, and tests.
2. Add the `AUTH_ACCOUNT_FROZEN` HTTP/Web mapping while preserving generic unknown-account and wrong-password responses.
3. Deploy API and Worker before relying on lifecycle notifications; older Web clients safely fall back to their generic error handling.
4. Deploy the updated login copy, message rendering compatibility, Admin documentation, and account-management documentation.
5. Rollback may stop producing or delivering new account notifications while retaining additive Outbox rows and already-created immutable messages. A rollback MUST NOT restore revoked credentials, delete audit evidence, or convert frozen accounts to normal.

## Open Questions

None for this revision. Administrator identities, role management, password reset, account cancellation, immediate access-token revocation, automatic content enforcement, and real-time push remain explicit follow-up changes.
